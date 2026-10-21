package api

// Usherr: the sound of a stream on its own, for a browser that plays the
// picture but not the sound (Dolby, Atmos, TrueHD or DTS in an MKV: Chrome
// plays those silently). The picture keeps playing as it is, in 4K and with
// seeking; the player plays this beside it, in step, and asks again from the
// new place after a seek. Only the sound is converted (stereo AAC), at the
// speed it's heard, so it reads the same part of the file the picture just
// did and costs next to nothing.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// streamAudio is one sound track in a stream.
type streamAudio struct {
	index    int
	lang     string
	channels int
	codec    string
	title    string
	def      bool
	// lead is how long after the start of the film this track's sound
	// begins (some releases start it a moment after the picture).
	lead float64
}

var streamAudioCache sync.Map // target URL -> []streamAudio

// streamAudioTracks lists a stream's sound tracks, once per stream.
func streamAudioTracks(ctx context.Context, target string) ([]streamAudio, error) {
	if v, ok := streamAudioCache.Load(target); ok {
		return v.([]streamAudio), nil
	}
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-rw_timeout", "30000000", "-select_streams", "a",
		"-show_entries", "format=start_time:stream=index,codec_name,channels,start_time:stream_tags=language,title:stream_disposition=default", "-of", "json", target).Output()
	if err != nil {
		return nil, err
	}
	var res struct {
		Format struct {
			Start string `json:"start_time"`
		} `json:"format"`
		Streams []struct {
			Start       string            `json:"start_time"`
			Index       int               `json:"index"`
			Codec       string            `json:"codec_name"`
			Channels    int               `json:"channels"`
			Tags        map[string]string `json:"tags"`
			Disposition map[string]int    `json:"disposition"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return nil, err
	}
	list := make([]streamAudio, 0, len(res.Streams))
	start, _ := strconv.ParseFloat(res.Format.Start, 64)
	for _, st := range res.Streams {
		a := streamAudio{index: st.Index, channels: st.Channels, codec: st.Codec, def: st.Disposition["default"] == 1}
		if s, err := strconv.ParseFloat(st.Start, 64); err == nil && s > start {
			a.lead = s - start
		}
		for k, v := range st.Tags {
			switch {
			case strings.EqualFold(k, "language") && v != "und":
				a.lang = strings.ToLower(v)
			case strings.EqualFold(k, "title"):
				a.title = v
			}
		}
		list = append(list, a)
	}
	streamAudioCache.Store(target, list)
	return list, nil
}

// pickStreamAudio is the track asked for (asked >= 0), else the one in the
// language wanted, else the first; its index is -1 when there is no sound.
func pickStreamAudio(list []streamAudio, asked int, lang string) streamAudio {
	if len(list) == 0 {
		return streamAudio{index: -1}
	}
	for _, a := range list {
		if asked >= 0 && a.index == asked {
			return a
		}
	}
	if lang != "" && list[0].lang != "" && !sameLang(list[0].lang, lang) {
		for _, a := range list {
			if sameLang(a.lang, lang) {
				return a
			}
		}
	}
	return list[0]
}

// streamSoundArgs reads target's track from at seconds in and gives AAC with
// the track's channels (5.1 stays 5.1), as ADTS, which a browser plays as it
// arrives: a few seconds ahead, then at the speed it's heard.
func streamSoundArgs(target string, track streamAudio, at float64) []string {
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin",
		"-reconnect", "1", "-reconnect_streamed", "1", "-reconnect_delay_max", "5"}
	if at > 0.05 {
		args = append(args, "-ss", strconv.FormatFloat(at, 'f', 3, 64))
	}
	args = append(args, "-readrate", "1", "-readrate_initial_burst", "8", "-i", target,
		"-map", "0:"+strconv.Itoa(track.index), "-vn", "-sn", "-dn")
	if gap := track.lead - at; gap > 0.01 {
		// The sound begins a moment into the film: silence until then, so
		// it starts in step with the picture.
		args = append(args, "-af", fmt.Sprintf("adelay=%d:all=1", int(gap*1000+0.5)))
	}
	args = append(args, aacArgs(track.channels)...)
	return append(args, "-f", "adts", "pipe:1")
}

// GET /api/stream/{id}/sound?at=<seconds>&track=<index>&lang=<language>: a
// stream's sound on its own, from at seconds in: the track picked in the
// player, else the one in the language wanted.
func (s *Server) handleStreamSound(w http.ResponseWriter, r *http.Request) {
	ss, ok := streams.get(r.PathValue("id"), requestUserID(r))
	if !ok {
		writeError(w, http.StatusNotFound, "This stream has ended. Start it again.")
		return
	}
	streams.mu.Lock()
	target := ss.target
	streams.mu.Unlock()
	list, err := streamAudioTracks(r.Context(), target)
	if err != nil {
		if r.Context().Err() == nil {
			writeError(w, http.StatusBadGateway, "The stream's sound couldn't be read.")
		}
		return
	}
	asked := -1
	if v := r.URL.Query().Get("track"); v != "" {
		asked, _ = strconv.Atoi(v)
	}
	track := pickStreamAudio(list, asked, r.URL.Query().Get("lang"))
	if track.index < 0 {
		writeError(w, http.StatusNotFound, "This stream has no sound.")
		return
	}
	at, _ := strconv.ParseFloat(r.URL.Query().Get("at"), 64)
	cmd := exec.CommandContext(r.Context(), "ffmpeg", streamSoundArgs(target, track, max(at, 0))...)
	w.Header().Set("Content-Type", "audio/aac")
	w.Header().Set("Cache-Control", "no-store")
	cmd.Stdout = flushWriter{w: w}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	stop := make(chan struct{})
	go func() { // the session stays alive while its sound plays
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				streams.touch(ss.id)
			}
		}
	}()
	err = cmd.Run()
	close(stop)
	if err != nil && r.Context().Err() == nil {
		slog.Info("stream: sound", "title", ss.title, "err", err, "ffmpeg", firstLine(stderr.String()))
	}
}

// GET /api/stream/{id}/tracks: a stream's sound tracks, for picking the
// language in the player. (Its subtitles aren't offered: reading them means
// reading the whole film from Usenet.)
func (s *Server) handleStreamTracks(w http.ResponseWriter, r *http.Request) {
	ss, ok := streams.get(r.PathValue("id"), requestUserID(r))
	if !ok {
		writeError(w, http.StatusNotFound, "This stream has ended. Start it again.")
		return
	}
	streams.mu.Lock()
	target := ss.target
	streams.mu.Unlock()
	list, err := streamAudioTracks(r.Context(), target)
	if err != nil {
		if r.Context().Err() == nil {
			writeError(w, http.StatusBadGateway, "The stream's sound tracks couldn't be read.")
		}
		return
	}
	audio := make([]playTrack, 0, len(list))
	for _, a := range list {
		audio = append(audio, playTrack{Index: a.index, Kind: "audio", Codec: a.codec, Lang: a.lang, Title: a.title, Channels: a.channels, Default: a.def})
	}
	writeJSON(w, http.StatusOK, map[string]any{"audio": audio, "subtitles": []playTrack{}})
}
