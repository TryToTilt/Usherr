package api

// Home fork: Mediarium TV as a live TV tuner, so its channels and guide
// show up in Jellyfin's Live TV (and any IPTV app). Jellyfin can't sign in,
// so the feeds live behind a secret key in their address, like the
// calendar feed:
//
//   /api/tvfeed/{key}/channels.m3u     the channel list
//   /api/tvfeed/{key}/xmltv.xml        the guide
//   /api/tvfeed/{key}/stream/{n}.ts    channel n, live
//
// A channel's stream is one continuous MPEG-TS: ffmpeg plays the program on
// air from the right second at real-time speed, then the next, and so on.
// Video is copied as it is (no re-encoding); sound is turned into AAC
// stereo so every program sounds alike. Timestamps carry on across programs.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rdborg/mediarium/internal/mediaservers"
	"github.com/rdborg/mediarium/internal/tv"
)

const (
	keyTVFeedKey  = "home.tv_feed_key"
	keyTVFeedBase = "home.tv_feed_base" // how Jellyfin reaches Mediarium
	defaultTVBase = "http://localhost:8264"
	maxTVStreams  = 4
	tvStartBurst  = 10 * time.Second
)

var tvStreams atomic.Int32

// tvFeedKey is the secret in the feeds' address, made on first use.
func (s *Server) tvFeedKey() string {
	if k, _ := s.Settings.Get(keyTVFeedKey); strings.TrimSpace(k) != "" {
		return strings.TrimSpace(k)
	}
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	k := hex.EncodeToString(b)
	_ = s.Settings.Set(keyTVFeedKey, k, false)
	return k
}

func (s *Server) tvFeedOK(r *http.Request) bool {
	k := s.tvFeedKey()
	return k != "" && r.PathValue("key") == k
}

// tvFeedBase is the address the feeds give out: the one the request came in
// on, so Jellyfin gets links it can reach.
func tvFeedBase(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if r.Host == "" {
		return defaultTVBase
	}
	return scheme + "://" + r.Host
}

func (s *Server) registerTVFeedRoutes(public *http.ServeMux) {
	public.HandleFunc("GET /api/tvfeed/{key}/channels.m3u", s.handleTVM3U)
	public.HandleFunc("GET /api/tvfeed/{key}/xmltv.xml", s.handleTVXMLTV)
	public.HandleFunc("GET /api/tvfeed/{key}/stream/{file}", s.handleTVStream)
}

// GET /api/tvfeed/{key}/channels.m3u
func (s *Server) handleTVM3U(w http.ResponseWriter, r *http.Request) {
	if !s.tvFeedOK(r) {
		http.NotFound(w, r)
		return
	}
	chans, err := s.tvRepo().Channels()
	if err != nil {
		http.Error(w, "unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "audio/x-mpegurl; charset=utf-8")
	key := r.PathValue("key")
	_, _ = io.WriteString(w, tvM3U(s.feedChannels(chans, tvFeedBase(r), key), tvFeedBase(r), key))
}

func tvM3U(chans []tv.Channel, origin, key string) string {
	base := origin + "/api/tvfeed/" + key
	var b strings.Builder
	fmt.Fprintf(&b, "#EXTM3U url-tvg=\"%s/xmltv.xml\"\n", base)
	for _, c := range chans {
		if !c.Enabled {
			continue
		}
		name := strings.ReplaceAll(c.Name, ",", " ")
		fmt.Fprintf(&b, "#EXTINF:-1 tvg-id=\"mediarium.%d\" tvg-chno=\"%d\" tvg-name=\"%s\" group-title=\"Mediarium TV\"", c.Number, c.Number, name)
		if c.LogoURL != "" {
			fmt.Fprintf(&b, " tvg-logo=\"%s\"", c.LogoURL)
		}
		if c.Kind == "live" {
			fmt.Fprintf(&b, ",%s\n%s/api/tvlive/%s/%d/index.m3u8\n", name, origin, key, c.Number)
			continue
		}
		fmt.Fprintf(&b, ",%s\n%s/stream/%d.ts\n", name, base, c.Number)
	}
	return b.String()
}

// ---- XMLTV ----

type xmltvDoc struct {
	XMLName    xml.Name         `xml:"tv"`
	Generator  string           `xml:"generator-info-name,attr"`
	Channels   []xmltvChannel   `xml:"channel"`
	Programmes []xmltvProgramme `xml:"programme"`
}

type xmltvChannel struct {
	ID      string `xml:"id,attr"`
	Name    string `xml:"display-name"`
	Number  string `xml:"lcn,omitempty"`
	IconSrc *struct {
		Src string `xml:"src,attr"`
	} `xml:"icon,omitempty"`
}

type xmltvProgramme struct {
	Start      string   `xml:"start,attr"`
	Stop       string   `xml:"stop,attr"`
	Channel    string   `xml:"channel,attr"`
	Title      string   `xml:"title"`
	SubTitle   string   `xml:"sub-title,omitempty"`
	Categories []string `xml:"category,omitempty"`
	EpisodeNum *struct {
		System string `xml:"system,attr"`
		Value  string `xml:",chardata"`
	} `xml:"episode-num,omitempty"`
}

func xmltvTime(t time.Time) string { return t.Format("20060102150405 -0700") }

// GET /api/tvfeed/{key}/xmltv.xml: three days of guide.
func (s *Server) handleTVXMLTV(w http.ResponseWriter, r *http.Request) {
	if !s.tvFeedOK(r) {
		http.NotFound(w, r)
		return
	}
	repo := s.tvRepo()
	chans, err := repo.Channels()
	if err != nil {
		http.Error(w, "unavailable", http.StatusInternalServerError)
		return
	}
	now := time.Now()
	progs := map[int64][]tv.Program{}
	for _, c := range chans {
		progs[c.ID], _ = repo.Programs(c.ID, now.Add(-2*time.Hour), now.Add(tv.Horizon))
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	_, _ = io.WriteString(w, xml.Header)
	enc := xml.NewEncoder(w)
	enc.Indent("", " ")
	_ = enc.Encode(tvXMLTV(s.feedChannels(chans, tvFeedBase(r), r.PathValue("key")), progs))
}

func tvXMLTV(chans []tv.Channel, progs map[int64][]tv.Program) xmltvDoc {
	doc := xmltvDoc{Generator: "Mediarium TV"}
	for _, c := range chans {
		if !c.Enabled {
			continue
		}
		id := fmt.Sprintf("mediarium.%d", c.Number)
		xc := xmltvChannel{ID: id, Name: c.Name, Number: strconv.Itoa(c.Number)}
		if c.LogoURL != "" {
			xc.IconSrc = &struct {
				Src string `xml:"src,attr"`
			}{c.LogoURL}
		}
		doc.Channels = append(doc.Channels, xc)
		for _, p := range progs[c.ID] {
			xp := xmltvProgramme{Start: xmltvTime(p.Start), Stop: xmltvTime(p.End), Channel: id, Title: p.Title}
			if p.Kind == "live" {
				xp.SubTitle = p.Subtitle
			} else if p.Kind == "movie" {
				xp.Categories = []string{"Movie"}
				xp.SubTitle = p.Subtitle
			} else {
				code, title, _ := strings.Cut(p.Subtitle, " · ")
				xp.SubTitle = title
				xp.Categories = []string{"Series"}
				if code != "" {
					xp.EpisodeNum = &struct {
						System string `xml:"system,attr"`
						Value  string `xml:",chardata"`
					}{"onscreen", code}
				}
			}
			doc.Programmes = append(doc.Programmes, xp)
		}
	}
	return doc
}

// ---- the live stream ----

// tvFFmpegArgs plays one program from offset for at most d, as MPEG-TS
// whose timestamps start at tsOffset (seconds into the stream).
// burst is how much to send at full speed before settling to real time: the
// first program of a connection gets a head start so players fill their
// buffer at once instead of waiting.
//
// Tuning in part-way seeks to the key frame before offset and starts sound
// there too (-noaccurate_seek), so the stream opens with picture and sound
// together: players such as Jellyfin's check the first seconds for both,
// and give up when the sound comes late. audio is which sound track to play.
func tvFFmpegArgs(file string, offset, d, tsOffset, burst time.Duration, audio int) []string {
	sec := func(x time.Duration) string { return strconv.FormatFloat(x.Seconds(), 'f', 3, 64) }
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin"}
	if offset > time.Second {
		args = append(args, "-noaccurate_seek", "-ss", sec(offset))
	}
	if strings.HasPrefix(file, "http") {
		// A streamed episode: carry on through a dropped connection.
		args = append(args, "-reconnect", "1", "-reconnect_streamed", "1", "-reconnect_delay_max", "5")
	}
	// Some files (and streams) leave the first frames' timestamps out, which
	// the MPEG-TS muxer refuses: fill them in.
	args = append(args, "-fflags", "+genpts", "-readrate", "1")
	if burst > 0 {
		args = append(args, "-readrate_initial_burst", sec(burst))
	}
	args = append(args,
		"-i", file,
		"-t", sec(d),
		"-map", "0:v:0", "-map", fmt.Sprintf("0:a:%d?", audio),
		"-c:v", "copy",
		"-c:a", "aac", "-b:a", "192k", "-ac", "2",
		"-output_ts_offset", sec(tsOffset),
		"-muxdelay", "0", "-muxpreload", "0",
		"-f", "mpegts", "pipe:1",
	)
	return args
}

var tvAudioPick sync.Map // file → sound track to play

// tvAudioTrack is the English sound track of a file (counting sound tracks
// only), or its first when it has none marked English.
func tvAudioTrack(ctx context.Context, file string) int {
	if v, ok := tvAudioPick.Load(file); ok {
		return v.(int)
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-select_streams", "a",
		"-show_entries", "stream_tags=language", "-of", "csv=p=0", file).Output()
	if err != nil {
		return 0
	}
	pick := pickAudioTrack(strings.Split(strings.TrimSpace(string(out)), "\n"))
	tvAudioPick.Store(file, pick)
	return pick
}

func pickAudioTrack(langs []string) int {
	for i, l := range langs {
		switch strings.ToLower(strings.TrimSpace(l)) {
		case "eng", "en", "english":
			return i
		}
	}
	return 0
}

// GET /api/tvfeed/{key}/stream/{n}.ts
func (s *Server) handleTVStream(w http.ResponseWriter, r *http.Request) {
	if !s.tvFeedOK(r) {
		http.NotFound(w, r)
		return
	}
	number, err := strconv.Atoi(strings.TrimSuffix(r.PathValue("file"), ".ts"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	repo := s.tvRepo()
	chans, err := repo.Channels()
	if err != nil {
		http.Error(w, "unavailable", http.StatusInternalServerError)
		return
	}
	var ch *tv.Channel
	for i := range chans {
		if chans[i].Number == number && chans[i].Enabled {
			ch = &chans[i]
		}
	}
	if ch == nil {
		http.NotFound(w, r)
		return
	}
	if ch.Kind == "live" {
		http.Redirect(w, r, s.liveStreamPath(ch.Number), http.StatusFound)
		return
	}
	if tvStreams.Add(1) > maxTVStreams {
		tvStreams.Add(-1)
		http.Error(w, "too many channels playing at once", http.StatusServiceUnavailable)
		return
	}
	defer tvStreams.Add(-1)

	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	// clock is how far into the schedule the stream has got. The first
	// program's head start puts it a little ahead of the wall clock, and it
	// stays that far ahead, so the next program picks up exactly where the
	// last one stopped.
	started := time.Now()
	clock := started
	burst := tvStartBurst
	ctx := r.Context()
	fw := flushWriter{w: w}
	for ctx.Err() == nil {
		now := clock
		if wall := time.Now(); wall.After(now) {
			now = wall
		}
		p, ok, err := repo.At(ch.ID, now)
		if err != nil || !ok {
			// Nothing scheduled right now: wait a little for the schedule.
			if !sleepCtx(ctx, 5*time.Second) {
				return
			}
			continue
		}
		offset, remaining := now.Sub(p.Start), p.End.Sub(now)
		if remaining < time.Second {
			if !sleepCtx(ctx, remaining+100*time.Millisecond) {
				return
			}
			continue
		}
		s.tvPrewarmBefore(ctx, ch.ID, p)
		var args []string
		src := p.FilePath
		if p.Kind == "stream" {
			if st, err := s.tvStreamSource(ctx, p.FilePath); err == nil {
				src = st.url
			} else {
				slog.Info("tv: off air", "channel", ch.Name, "program", p.Title, "err", err)
				args = tvSlateArgs(remaining, now.Sub(started))
			}
		}
		if args == nil {
			args = tvFFmpegArgs(src, offset, remaining, now.Sub(started), burst, tvAudioTrack(ctx, src))
		}
		burst = 0
		err = s.runTVFFmpeg(ctx, fw, args)
		if err == nil {
			clock = p.End
			continue
		}
		if ctx.Err() != nil {
			return
		}
		slog.Info("tv: stream", "channel", ch.Name, "program", p.Title, "err", err)
		// This program can't play: off air until the next one, so the
		// player keeps its picture rather than giving up.
		at := clock
		if wall := time.Now(); wall.After(at) {
			at = wall
		}
		if rest := p.End.Sub(at); rest > time.Second {
			if s.runTVFFmpeg(ctx, fw, tvSlateArgs(rest, at.Sub(started))) != nil && !sleepCtx(ctx, rest) {
				return
			}
		}
		clock = p.End
	}
}

func (s *Server) runTVFFmpeg(ctx context.Context, w io.Writer, args []string) error {
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Stdout = w
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := firstLine(stderr.String()); msg != "" {
			return fmt.Errorf("%w: %s", err, msg)
		}
		return err
	}
	return nil
}

type flushWriter struct{ w http.ResponseWriter }

func (f flushWriter) Write(p []byte) (int, error) {
	n, err := f.w.Write(p)
	if fl, ok := f.w.(http.Flusher); ok {
		fl.Flush()
	}
	return n, err
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

// ---- Jellyfin ----

// GET /api/tv/tuner: the feed addresses (admin), for setting up an IPTV app
// by hand.
func (s *Server) handleTVTunerInfo(w http.ResponseWriter, r *http.Request) {
	base := s.feedAddress(r)
	feed := strings.TrimRight(base, "/") + "/api/tvfeed/" + s.tvFeedKey()
	writeJSON(w, http.StatusOK, map[string]any{"m3u": feed + "/channels.m3u", "xmltv": feed + "/xmltv.xml"})
}

// POST /api/tv/jellyfin: add Mediarium TV to Jellyfin's Live TV (a tuner
// and a guide), once, and refresh Jellyfin's guide.
func (s *Server) handleTVJellyfin(w http.ResponseWriter, r *http.Request) {
	srv, ok := s.jellyfinServer()
	if !ok {
		writeError(w, http.StatusPreconditionFailed, connectServerFirst)
		return
	}
	base := s.feedAddress(r)
	if base == "" {
		writeError(w, http.StatusPreconditionFailed, "Set Mediarium's address on your network under Settings > Extras first, so "+serverLabel(srv)+" can reach it.")
		return
	}
	feed := strings.TrimRight(base, "/") + "/api/tvfeed/" + s.tvFeedKey()
	m3u, guide := feed+"/channels.m3u", feed+"/xmltv.xml"
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	var cfg struct {
		TunerHosts []struct {
			ID  string `json:"Id"`
			URL string `json:"Url"`
		} `json:"TunerHosts"`
		ListingProviders []struct {
			Path string `json:"Path"`
		} `json:"ListingProviders"`
	}
	if err := jfJSON(ctx, srv, http.MethodGet, "/System/Configuration/livetv", nil, &cfg); err != nil {
		writeError(w, http.StatusBadGateway, "Couldn't read "+serverLabel(srv)+"'s Live TV settings: "+err.Error())
		return
	}
	added := []string{}
	hasGuide := false
	tunerID := ""
	for _, t := range cfg.TunerHosts {
		if strings.Contains(t.URL, "/api/tvfeed/") {
			tunerID = t.ID
		}
	}
	for _, l := range cfg.ListingProviders {
		hasGuide = hasGuide || strings.Contains(l.Path, "/api/tvfeed/")
	}
	// Added once, then kept up to date (same Id). TunerCount 0 is no limit
	// in Jellyfin: Mediarium limits its own channels, and Jellyfin's count
	// also holds on to streams a failed start never gave back.
	body := map[string]any{"Type": "m3u", "Url": m3u, "FriendlyName": "Mediarium TV", "TunerCount": 0, "ImportFavoritesOnly": false, "AllowHWTranscoding": true, "EnableStreamLooping": false}
	if tunerID != "" {
		body["Id"] = tunerID
	}
	if err := jfJSON(ctx, srv, http.MethodPost, "/LiveTv/TunerHosts", body, nil); err != nil {
		writeError(w, http.StatusBadGateway, serverLabel(srv)+" didn't take the tuner: "+err.Error())
		return
	}
	if tunerID == "" {
		added = append(added, "tuner")
	}
	if !hasGuide {
		body := map[string]any{"Type": "xmltv", "Path": guide, "EnableAllTuners": true}
		if err := jfJSON(ctx, srv, http.MethodPost, "/LiveTv/ListingProviders?validateListings=false&validateLogin=false", body, nil); err != nil {
			writeError(w, http.StatusBadGateway, serverLabel(srv)+" didn't take the guide: "+err.Error())
			return
		}
		added = append(added, "guide")
	}
	refreshJellyfinGuide(ctx, srv)
	writeJSON(w, http.StatusOK, map[string]any{"added": added, "message": "Mediarium TV is in " + serverLabel(srv) + "'s Live TV. The guide fills in within a few minutes."})
}

// refreshJellyfinGuide starts Jellyfin's "Refresh Guide" task.
func refreshJellyfinGuide(ctx context.Context, srv mediaservers.Server) {
	var tasks []struct {
		ID  string `json:"Id"`
		Key string `json:"Key"`
	}
	if err := jfJSON(ctx, srv, http.MethodGet, "/ScheduledTasks", nil, &tasks); err != nil {
		return
	}
	for _, t := range tasks {
		if t.Key == "RefreshGuide" {
			_ = jfJSON(ctx, srv, http.MethodPost, "/ScheduledTasks/Running/"+t.ID, nil, nil)
		}
	}
}
