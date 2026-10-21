package api

// Usherr: choosing the sound track and subtitles in the player. A file's
// tracks are read with ffprobe; subtitle files next to it (Bazarr's .srt)
// count too. Text subtitles reach the browser as WebVTT; picture subtitles
// (Blu-ray PGS, DVD) can only be burned into a converted picture.

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// playTrack is one sound or subtitle track of a title.
type playTrack struct {
	Index    int    `json:"index"`          // the stream's index in the file (as Jellyfin counts); -1 for a file next to it
	Key      string `json:"key,omitempty"`  // subtitles: "e<index>" in the file, "f<n>" a file next to it
	Kind     string `json:"kind"`           // audio, text (subtitles as text), image (picture subtitles)
	Codec    string `json:"codec"`          // aac, eac3, subrip, hdmv_pgs_subtitle…
	Lang     string `json:"lang,omitempty"` // eng, spa…
	Title    string `json:"title,omitempty"`
	Channels int    `json:"channels,omitempty"`
	Default  bool   `json:"default,omitempty"`
	Forced   bool   `json:"forced,omitempty"`
	path     string // a file next to the video
}

var (
	tracksMu   sync.Mutex
	trackCache = map[codecsKey][]playTrack{}
)

// imageSubs are subtitle codecs that are pictures, not text.
var imageSubs = map[string]bool{"hdmv_pgs_subtitle": true, "dvd_subtitle": true, "dvb_subtitle": true, "xsub": true}

// probeTracks lists file's sound tracks and subtitles.
func probeTracks(ctx context.Context, file string) ([]playTrack, error) {
	fi, err := os.Stat(file)
	if err != nil {
		return nil, err
	}
	key := codecsKey{file, fi.Size(), fi.ModTime()}
	tracksMu.Lock()
	if v, ok := trackCache[key]; ok {
		tracksMu.Unlock()
		return withSidecars(file, v), nil
	}
	tracksMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-show_entries",
		"stream=index,codec_type,codec_name,channels:stream_tags=language,title:stream_disposition=default,forced", "-of", "json", file).Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe: %w", err)
	}
	var res struct {
		Streams []struct {
			Index       int               `json:"index"`
			Type        string            `json:"codec_type"`
			Codec       string            `json:"codec_name"`
			Channels    int               `json:"channels"`
			Tags        map[string]string `json:"tags"`
			Disposition map[string]int    `json:"disposition"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return nil, err
	}
	var list []playTrack
	for _, st := range res.Streams {
		tr := playTrack{Index: st.Index, Codec: st.Codec, Channels: st.Channels, Default: st.Disposition["default"] == 1, Forced: st.Disposition["forced"] == 1}
		for k, v := range st.Tags {
			switch strings.ToLower(k) {
			case "language":
				if v != "und" {
					tr.Lang = strings.ToLower(v)
				}
			case "title":
				tr.Title = v
			}
		}
		switch st.Type {
		case "audio":
			tr.Kind = "audio"
		case "subtitle":
			tr.Kind = "text"
			if imageSubs[st.Codec] {
				tr.Kind = "image"
			}
			tr.Key = "e" + strconv.Itoa(st.Index)
		default:
			continue
		}
		list = append(list, tr)
	}
	tracksMu.Lock()
	trackCache[key] = list
	tracksMu.Unlock()
	return withSidecars(file, list), nil
}

// sidecarPart is a language in "Movie.en.forced.srt", among the parts after
// the video's own name.
var sidecarPart = regexp.MustCompile(`^[a-z]{2,3}(-[a-z]{2})?$`)

// withSidecars adds the subtitle files next to file (same name, then a
// language): Movie.en.srt, Movie.eng.forced.srt.
func withSidecars(file string, list []playTrack) []playTrack {
	out := append([]playTrack(nil), list...)
	dir, base := filepath.Dir(file), strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	var names []string
	for _, e := range entries {
		n := e.Name()
		switch strings.ToLower(filepath.Ext(n)) {
		case ".srt", ".vtt", ".ass", ".ssa":
		default:
			continue
		}
		if !e.IsDir() && strings.HasPrefix(strings.ToLower(n), strings.ToLower(base)) {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	for i, n := range names {
		tr := playTrack{Index: -1, Key: "f" + strconv.Itoa(i), Kind: "text", Codec: strings.TrimPrefix(strings.ToLower(filepath.Ext(n)), "."), path: filepath.Join(dir, n)}
		mid := strings.TrimSuffix(n[len(base):], filepath.Ext(n))
		for _, p := range strings.Split(strings.ToLower(mid), ".") {
			switch {
			case p == "forced":
				tr.Forced = true
			case p == "sdh" || p == "cc" || p == "hi":
				tr.Title = "SDH"
			case sidecarPart.MatchString(p) && tr.Lang == "":
				tr.Lang = p
			}
		}
		out = append(out, tr)
	}
	return out
}

// sameLang compares languages written as "en", "eng" or "en-US".
func sameLang(a, b string) bool {
	a, b = langBase(a), langBase(b)
	return a != "" && a == b
}

var lang2to3 = map[string]string{"en": "eng", "es": "spa", "fr": "fre", "de": "ger", "it": "ita", "pt": "por", "nl": "dut", "sv": "swe", "da": "dan", "no": "nor", "nb": "nor", "fi": "fin", "pl": "pol", "cs": "cze", "ru": "rus", "ja": "jpn", "ko": "kor", "zh": "chi", "ar": "ara", "hi": "hin", "tr": "tur", "el": "gre", "he": "heb", "hu": "hun"}
var lang3alias = map[string]string{"fra": "fre", "deu": "ger", "nld": "dut", "ces": "cze", "zho": "chi", "ell": "gre", "nob": "nor"}

func langBase(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if i := strings.IndexAny(s, "-_"); i > 0 {
		s = s[:i]
	}
	if v, ok := lang2to3[s]; ok {
		return v
	}
	if v, ok := lang3alias[s]; ok {
		return v
	}
	return s
}

// pickAudio is the sound track to play: the one asked for, else the first in
// the language wanted (when the file's first track is another language),
// else the file's first.
func pickAudio(tracks []playTrack, asked *int, lang string) (playTrack, bool) {
	var first *playTrack
	for i := range tracks {
		if tracks[i].Kind != "audio" {
			continue
		}
		if first == nil {
			first = &tracks[i]
		}
		if asked != nil && tracks[i].Index == *asked {
			return tracks[i], true
		}
	}
	if first == nil {
		return playTrack{Index: -1}, false
	}
	if lang != "" && first.Lang != "" && !sameLang(first.Lang, lang) {
		for _, tr := range tracks {
			if tr.Kind == "audio" && sameLang(tr.Lang, lang) {
				return tr, true
			}
		}
	}
	return *first, true
}

// GET /api/play/tracks/{kind}/{id}: a title's sound tracks and subtitles.
func (s *Server) handlePlayTracks(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	file, _, err := s.playFile(r, r.PathValue("kind"), id)
	if err != nil || file == "" {
		writeError(w, http.StatusNotFound, "This title has no file to play.")
		return
	}
	tracks, err := probeTracks(r.Context(), file)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "This file's tracks couldn't be read.")
		return
	}
	out := map[string][]playTrack{"audio": {}, "subtitles": {}}
	for _, tr := range tracks {
		if tr.Kind == "audio" {
			out["audio"] = append(out["audio"], tr)
		} else {
			out["subtitles"] = append(out["subtitles"], tr)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// GET /api/play/subs/{kind}/{id}?key=e3&shift=<seconds>: a text subtitle as
// WebVTT, its times moved back by shift (for a copy that starts part-way).
// Subtitles inside a big file take a while the first time: until the whole
// file has been read, X-Subs-Complete is 0 and what's read so far comes back.
func (s *Server) handlePlaySubs(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	file, _, err := s.playFile(r, r.PathValue("kind"), id)
	if err != nil || file == "" {
		writeError(w, http.StatusNotFound, "This title has no file to play.")
		return
	}
	tracks, err := probeTracks(r.Context(), file)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "This file's tracks couldn't be read.")
		return
	}
	key := r.URL.Query().Get("key")
	var tr *playTrack
	for i := range tracks {
		if tracks[i].Key == key && tracks[i].Kind == "text" {
			tr = &tracks[i]
		}
	}
	if tr == nil {
		writeError(w, http.StatusNotFound, "That subtitle isn't there, or is a picture subtitle.")
		return
	}
	vtt, complete, err := subtitleVTT(r.Context(), file, *tr, tracks)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "That subtitle couldn't be read.")
		return
	}
	if shift, _ := strconv.ParseFloat(r.URL.Query().Get("shift"), 64); shift > 0 {
		vtt = shiftVTT(vtt, time.Duration(shift*float64(time.Second)))
	}
	w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if complete {
		w.Header().Set("X-Subs-Complete", "1")
	} else {
		w.Header().Set("X-Subs-Complete", "0")
	}
	_, _ = w.Write(vtt)
}

// subtitleVTT converts a text subtitle to WebVTT, kept on disk afterwards.
// Subtitles inside a file are spread through all of it: a big 4K file takes
// minutes to read through. All its text subtitles are read in one pass, in
// the background; until it's done, what has been read so far comes back,
// with complete false, and the player asks again.
func subtitleVTT(ctx context.Context, file string, tr playTrack, all []playTrack) ([]byte, bool, error) {
	if tr.path != "" {
		if strings.EqualFold(filepath.Ext(tr.path), ".vtt") {
			b, err := os.ReadFile(tr.path)
			return b, true, err
		}
		cached, err := subsCachePath(tr.path, tr.Key)
		if err != nil {
			return nil, false, err
		}
		if b, err := os.ReadFile(cached); err == nil {
			return b, true, nil
		}
		ctx, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()
		out, err := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error", "-nostdin", "-i", tr.path, "-f", "webvtt", "pipe:1").Output()
		if err != nil {
			return nil, false, err
		}
		_ = os.WriteFile(cached, out, 0o600)
		return out, true, nil
	}
	cached, err := subsCachePath(file, tr.Key)
	if err != nil {
		return nil, false, err
	}
	if b, err := os.ReadFile(cached); err == nil {
		return b, true, nil
	}
	startSubsJob(file, all)
	b, _ := os.ReadFile(cached + ".part")
	if i := bytes.LastIndex(b, []byte("\n\n")); i >= 0 {
		b = b[:i+2] // only whole cues
	} else {
		b = []byte("WEBVTT\n\n")
	}
	if _, err := os.Stat(cached); err == nil { // finished meanwhile
		if full, err := os.ReadFile(cached); err == nil {
			return full, true, nil
		}
	}
	return b, false, nil
}

// subsCachePath is where a subtitle of src is kept as WebVTT.
func subsCachePath(src, key string) (string, error) {
	fi, err := os.Stat(src)
	if err != nil {
		return "", err
	}
	h := sha1.Sum([]byte(fmt.Sprintf("%s|%d|%d|%s", src, fi.Size(), fi.ModTime().Unix(), key)))
	dir := filepath.Join(os.TempDir(), "usherr-subs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, hex.EncodeToString(h[:])+".vtt"), nil
}

var subsJobs sync.Map // file -> struct{}: a pass reading its subtitles is on

// startSubsJob reads every text subtitle in file in one pass, unless one is
// already doing so.
func startSubsJob(file string, all []playTrack) {
	if _, busy := subsJobs.LoadOrStore(file, struct{}{}); busy {
		return
	}
	go func() {
		defer subsJobs.Delete(file)
		args := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-i", file}
		var outs []string
		for _, t := range all {
			if t.Kind != "text" || t.path != "" {
				continue
			}
			p, err := subsCachePath(file, t.Key)
			if err != nil {
				return
			}
			if _, err := os.Stat(p); err == nil {
				continue
			}
			args = append(args, "-map", "0:"+strconv.Itoa(t.Index), "-flush_packets", "1", "-f", "webvtt", p+".part")
			outs = append(outs, p)
		}
		if len(outs) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		args = append([]string{"-y"}, args...)
		if err := exec.CommandContext(ctx, "ffmpeg", args...).Run(); err != nil {
			slog.Info("play: subtitles", "file", filepath.Base(file), "err", err)
			for _, p := range outs {
				_ = os.Remove(p + ".part")
			}
			return
		}
		for _, p := range outs {
			_ = os.Rename(p+".part", p)
		}
	}()
}

var vttTime = regexp.MustCompile(`^((?:\d+:)?\d{2}:\d{2}\.\d{3}) --> ((?:\d+:)?\d{2}:\d{2}\.\d{3})(.*)$`)

// shiftVTT moves every cue back by d, dropping the ones over before 0.
func shiftVTT(vtt []byte, d time.Duration) []byte {
	var out bytes.Buffer
	sc := bufio.NewScanner(bytes.NewReader(vtt))
	sc.Buffer(make([]byte, 64*1024), 4<<20)
	skip := false
	for sc.Scan() {
		line := sc.Text()
		if m := vttTime.FindStringSubmatch(line); m != nil {
			a, b := parseVTTTime(m[1])-d, parseVTTTime(m[2])-d
			if b <= 0 {
				skip = true
				continue
			}
			skip = false
			out.WriteString(formatVTTTime(max(a, 0)) + " --> " + formatVTTTime(b) + m[3] + "\n")
			continue
		}
		if skip {
			if line == "" {
				skip = false
				out.WriteString("\n")
			}
			continue
		}
		out.WriteString(line + "\n")
	}
	return out.Bytes()
}

func parseVTTTime(s string) time.Duration {
	parts := strings.Split(s, ":")
	var h, m int
	var sec float64
	if len(parts) == 3 {
		h, _ = strconv.Atoi(parts[0])
		parts = parts[1:]
	}
	m, _ = strconv.Atoi(parts[0])
	sec, _ = strconv.ParseFloat(parts[1], 64)
	return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute + time.Duration(sec*float64(time.Second))
}

func formatVTTTime(d time.Duration) string {
	ms := d.Milliseconds()
	return fmt.Sprintf("%02d:%02d:%02d.%03d", ms/3600000, ms/60000%60, ms/1000%60, ms%1000)
}
