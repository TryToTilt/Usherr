package api

// Home fork: Mediarium TV's streamed shows, shows a channel plays without
// having them. Their episodes come from TMDB (aired ones, with TMDB's
// runtimes until a real length is known), and each plays through StreamNZB
// when it comes on: the 1080p stream by default. A couple of minutes before
// a streamed episode starts on a channel someone is watching, Mediarium asks
// StreamNZB for it and reads its first bytes, so it starts on time. When
// nothing can be found the channel shows a dark "off air" screen until the
// next program.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/tv"
)

const keyTVStreamQuality = "home.tv_stream_quality" // StreamNZB stream for streamed shows

// tvLibrary is the library plus streamed shows (negative ids).
type tvLibrary struct {
	tv.DBLibrary
	s *Server
}

func (l tvLibrary) Episodes(id int64) ([]tv.Item, error) {
	if id >= 0 {
		return l.DBLibrary.Episodes(id)
	}
	return l.s.streamedEpisodes(-id)
}

type streamedShow struct {
	items []tv.Item
	at    time.Time
}

var streamedShows sync.Map // tmdb id -> streamedShow

// streamedEpisodes lists a streamed show's aired episodes from TMDB
// (remembered for a day).
func (s *Server) streamedEpisodes(tmdbID int64) ([]tv.Item, error) {
	if v, ok := streamedShows.Load(tmdbID); ok && time.Since(v.(streamedShow).at) < 24*time.Hour {
		return v.(streamedShow).items, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	show, eps, err := s.TMDB().GetShowEpisodes(ctx, int(tmdbID))
	if err != nil {
		return nil, fmt.Errorf("look up show %d on TMDB: %w", tmdbID, err)
	}
	today := time.Now().Format("2006-01-02")
	sort.Slice(eps, func(i, j int) bool {
		if eps[i].Season != eps[j].Season {
			return eps[i].Season < eps[j].Season
		}
		return eps[i].Episode < eps[j].Episode
	})
	var known []int
	for _, e := range eps {
		if e.Runtime > 0 {
			known = append(known, e.Runtime)
		}
	}
	typical := 30
	if len(known) > 0 {
		sort.Ints(known)
		typical = known[len(known)/2]
	}
	var items []tv.Item
	for _, e := range eps {
		if e.Season <= 0 || e.AirDate == "" || e.AirDate > today {
			continue // specials, and what hasn't aired
		}
		mins := e.Runtime
		if mins <= 0 {
			mins = typical
		}
		sub := fmt.Sprintf("S%02dE%02d", e.Season, e.Episode)
		if e.Name != "" {
			sub += " · " + e.Name
		}
		items = append(items, tv.Item{
			Kind: "stream", SeriesID: -tmdbID, Title: show.Name, Subtitle: sub,
			Path: tv.StreamPath(tmdbID, e.Season, e.Episode), Runtime: time.Duration(mins) * time.Minute,
		})
	}
	streamedShows.Store(tmdbID, streamedShow{items: items, at: time.Now()})
	return items, nil
}

// tvProber measures library files with ffprobe; streamed episodes take the
// length learned when one last played, or TMDB's runtime.
func tvProber(repo *tv.Repo) tv.Prober {
	files := tv.FFProbe(repo)
	return func(it tv.Item) (time.Duration, error) {
		if it.Kind != "stream" {
			return files(it)
		}
		if secs, ok := repo.Duration(it.Path, 0); ok {
			return time.Duration(secs * float64(time.Second)), nil
		}
		if it.Runtime <= 0 {
			return 0, errors.New("no runtime")
		}
		return it.Runtime, nil
	}
}

// ---- finding a stream ----

var errOffAir = errors.New("nothing to stream for this episode right now")

type tvStreamSrc struct {
	url     string // as Mediarium reaches it
	homeURL string // as a device at home reaches it
	release string
	at      time.Time
	// browserOK is whether a web browser can play it as it is; old AVI and
	// XviD copies can't, and are converted for the browser instead.
	browserOK bool
	// copyVideo: the picture plays as it is and only its box (AVI, TS)
	// doesn't, so a conversion keeps the picture.
	copyVideo bool
}

var (
	tvStreamSrcs sync.Map // stream path -> tvStreamSrc
	tvMissing    sync.Map // stream path -> time it couldn't be found
	tvIMDb       sync.Map // tmdb id -> imdb id
)

// tvStreamSource finds a streamed episode's stream (remembered for a few
// hours; a miss is remembered for a few minutes so a channel doesn't ask
// again and again).
func (s *Server) tvStreamSource(ctx context.Context, path string) (tvStreamSrc, error) {
	if v, ok := tvStreamSrcs.Load(path); ok && time.Since(v.(tvStreamSrc).at) < 3*time.Hour {
		return v.(tvStreamSrc), nil
	}
	if v, ok := tvMissing.Load(path); ok && time.Since(v.(time.Time)) < 5*time.Minute {
		return tvStreamSrc{}, errOffAir
	}
	tmdbID, season, episode, ok := tv.ParseStreamPath(path)
	if !ok {
		return tvStreamSrc{}, errOffAir
	}
	// Usherr: the engine chosen in Settings > Extras goes first. Usherr's own
	// engine also takes over when StreamNZB has nothing that plays (a daily
	// show it can't search by date, or no StreamNZB at all).
	if s.streamEngine() == engineUsherr {
		if own, oerr := s.tvStreamOwn(ctx, path, tmdbID, season, episode); oerr == nil {
			tvStreamSrcs.Store(path, own)
			return own, nil
		} else if !errors.Is(oerr, errNoRelease) {
			slog.Info("tv: own stream", "episode", path, "err", oerr)
		}
		if src, err := s.tvStreamNZB(ctx, path, tmdbID, season, episode); err == nil {
			return src, nil
		}
		tvMissing.Store(path, time.Now())
		return tvStreamSrc{}, errOffAir
	}
	src, err := s.tvStreamNZB(ctx, path, tmdbID, season, episode)
	if err == nil {
		return src, nil
	}
	if own, oerr := s.tvStreamOwn(ctx, path, tmdbID, season, episode); oerr == nil {
		tvStreamSrcs.Store(path, own)
		return own, nil
	} else if !errors.Is(oerr, errNoRelease) {
		slog.Info("tv: own stream", "episode", path, "err", oerr)
	}
	tvMissing.Store(path, time.Now())
	return tvStreamSrc{}, errOffAir
}

// tvStreamNZB finds an episode's stream on StreamNZB.
func (s *Server) tvStreamNZB(ctx context.Context, path string, tmdbID int64, season, episode int) (tvStreamSrc, error) {
	c, err := s.streamNZB()
	if err != nil {
		return tvStreamSrc{}, err
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
	defer cancel()
	profiles, err := c.profiles(ctx)
	if err != nil {
		return tvStreamSrc{}, err
	}
	quality, _ := s.Settings.Get(keyTVStreamQuality)
	var p snzbProfile
	for _, q := range []string{strings.TrimSpace(quality), "1080p", "720p", "4K"} {
		if pp, ok := profileFor(profiles, q); q != "" && ok {
			p = pp
			break
		}
	}
	if p.Token == "" {
		return tvStreamSrc{}, errors.New("StreamNZB has no stream set up for TV channels")
	}
	imdb := ""
	if v, ok := tvIMDb.Load(tmdbID); ok {
		imdb = v.(string)
	} else {
		if imdb, err = s.imdbFor(ctx, "series", int(tmdbID)); err != nil {
			return tvStreamSrc{}, err
		}
		tvIMDb.Store(tmdbID, imdb)
	}
	list, err := c.titleStreams(ctx, p.Token, "series", fmt.Sprintf("%s:%d:%d", imdb, season, episode))
	if err != nil {
		return tvStreamSrc{}, err
	}
	// StreamNZB's first stream isn't always the episode: some are short
	// placeholder clips. Take the first that really lasts.
	repo := s.tvRepo()
	for i, st := range list {
		if i >= 4 {
			break
		}
		u := c.internalURL(st.URL)
		pr, err := probeStream(ctx, u)
		if err != nil || pr.secs < 60 {
			slog.Info("tv: skip stream", "episode", path, "seconds", pr.secs, "err", err)
			continue
		}
		secs := pr.secs
		src := tvStreamSrc{url: u, homeURL: st.URL, release: releaseLine(st.Title + "\n" + st.Description), at: time.Now(), browserOK: pr.browserOK(), copyVideo: pr.copyVideo()}
		tvStreamSrcs.Store(path, src)
		repo.SetDuration(path, 0, secs) // its next showing gets its real length
		return src, nil
	}
	return tvStreamSrc{}, errOffAir
}

type streamProbe struct {
	secs   float64
	format string // ffprobe's format names: "matroska,webm", "avi", ...
	video  string // the first video stream's codec
}

// copyVideo is whether the picture can be kept as it is when the stream is
// converted (it's H.264, only in a box browsers don't open).
func (p streamProbe) copyVideo() bool { return p.video == "h264" }

// browserOK is whether web browsers play this as it is.
func (p streamProbe) browserOK() bool {
	switch p.video {
	case "h264", "vp9", "av1", "hevc":
	default:
		return false
	}
	return strings.Contains(p.format, "matroska") || strings.Contains(p.format, "mp4") || strings.Contains(p.format, "mov")
}

// probeStream measures a stream: how long it plays and what it is.
func probeStream(ctx context.Context, u string) (streamProbe, error) {
	return probeStreamWithin(ctx, u, 45*time.Second)
}

// probeStreamTwice is probeStream that looks once more when the first look
// failed: the first read of a stream that was only just opened from Usenet can
// be slow or fail once (the log showed "exit status 1" and "killed" for streams
// that played a minute later).
func probeStreamTwice(ctx context.Context, u string) (streamProbe, error) {
	p, err := probeStreamWithin(ctx, u, 40*time.Second)
	if err == nil || ctx.Err() != nil {
		return p, err
	}
	slog.Info("tv: probe failed, looking again", "err", err)
	if !sleepCtx(ctx, 2*time.Second) {
		return p, err
	}
	return probeStreamWithin(ctx, u, 60*time.Second)
}

// probeStreamWithin is probeStream with its own time limit. A failure says
// why, in ffprobe's words, and a read that stalls gives up before the limit.
func probeStreamWithin(ctx context.Context, u string, limit time.Duration) (streamProbe, error) {
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-rw_timeout", "30000000", "-select_streams", "v:0",
		"-show_entries", "format=duration,format_name:stream=codec_name", "-of", "default=noprint_wrappers=1", u)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return streamProbe{}, fmt.Errorf("ffprobe didn't answer in %s", limit.Round(time.Second))
		}
		if why := strings.TrimSpace(firstLine(strings.TrimSpace(stderr.String()))); why != "" {
			return streamProbe{}, fmt.Errorf("ffprobe: %s", why)
		}
		return streamProbe{}, err
	}
	var p streamProbe
	for _, line := range strings.Split(string(out), "\n") {
		k, v, _ := strings.Cut(strings.TrimSpace(line), "=")
		switch k {
		case "duration":
			p.secs, _ = strconv.ParseFloat(v, 64)
		case "format_name":
			p.format = v
		case "codec_name":
			p.video = v
		}
	}
	return p, nil
}

// releaseLine picks the release name out of a StreamNZB stream's text: the
// line that reads like one (has a resolution, or dots), else the first.
func releaseLine(text string) string {
	first := ""
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if first == "" {
			first = line
		}
		if parser.Parse(line).Resolution != "" || strings.Count(line, ".") >= 3 {
			return line
		}
	}
	return first
}

// ---- converting for the browser ----

// tvBrowserArgs plays a stream from offset as fragmented MP4 that any web
// browser plays, at real-time speed after a short head start. Only what has
// to be converted is: the picture is kept when copyVideo (else it becomes
// H.264 at its own size), and the sound becomes AAC with its channels.
func tvBrowserArgs(u string, offset time.Duration, copyVideo bool) []string {
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin",
		"-reconnect", "1", "-reconnect_streamed", "1", "-reconnect_delay_max", "5"}
	if offset > time.Second {
		args = append(args, "-ss", strconv.FormatFloat(offset.Seconds(), 'f', 3, 64))
	}
	args = append(args, "-fflags", "+genpts", "-readrate", "1", "-readrate_initial_burst", "8", "-i", u,
		"-map", "0:v:0", "-map", "0:a:0?")
	if copyVideo {
		args = append(args, "-c:v", "copy")
	} else {
		args = append(args, "-c:v", "libx264", "-preset", "veryfast", "-crf", "21", "-pix_fmt", "yuv420p")
	}
	args = append(args, aacArgs(6)...)
	return append(args, "-movflags", "frag_keyframe+empty_moov+default_base_moof", "-f", "mp4", "pipe:1")
}

// GET /api/stream/{id}/browser?at=<seconds>: a stream session's video
// converted for the browser, from at seconds in.
func (s *Server) handleStreamBrowser(w http.ResponseWriter, r *http.Request) {
	ss, ok := streams.get(r.PathValue("id"), requestUserID(r))
	if !ok {
		writeError(w, http.StatusNotFound, "This stream has ended. Start it again.")
		return
	}
	at, _ := strconv.ParseFloat(r.URL.Query().Get("at"), 64)
	cmd := exec.CommandContext(r.Context(), "ffmpeg", tvBrowserArgs(ss.target, time.Duration(at*float64(time.Second)), r.URL.Query().Get("v") == "copy")...)
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Cache-Control", "no-store")
	cmd.Stdout = flushWriter{w: w}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	stop := make(chan struct{})
	go func() { // keep the session alive while it plays
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
	err := cmd.Run()
	close(stop)
	if err != nil && r.Context().Err() == nil {
		slog.Info("tv: browser copy", "title", ss.title, "err", err, "ffmpeg", firstLine(stderr.String()))
	}
}

// tvPrewarm gets a streamed episode ready: finds its stream and reads the
// first bytes, so StreamNZB has it going when it comes on.
func (s *Server) tvPrewarm(path string) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	src, err := s.tvStreamSource(ctx, path)
	if err != nil {
		slog.Info("tv: get ready", "episode", path, "err", err)
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src.url, nil)
	if err != nil {
		return
	}
	req.Header.Set("Range", "bytes=0-2097151")
	if resp, err := streamRelayClient.Do(req); err == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}

var tvWarming sync.Map // stream path -> when it was warmed

// tvPrewarmBefore gets the program after p ready about two minutes before
// p ends, when it's a streamed one. ctx is the viewer: nothing is fetched
// once they've gone.
func (s *Server) tvPrewarmBefore(ctx context.Context, channelID int64, p tv.Program) {
	next, ok, _ := s.tvRepo().At(channelID, p.End.Add(time.Millisecond))
	if !ok || next.Kind != "stream" {
		return
	}
	if v, ok := tvWarming.Load(next.FilePath); ok && time.Since(v.(time.Time)) < 30*time.Minute {
		return
	}
	go func() {
		if !sleepCtx(ctx, time.Until(p.End.Add(-2*time.Minute))) {
			return
		}
		if _, loaded := tvWarming.LoadOrStore(next.FilePath, time.Now()); loaded {
			return
		}
		s.tvPrewarm(next.FilePath)
	}()
}

// tvSlateArgs is the "off air" screen: dark picture and silence for d.
func tvSlateArgs(d, tsOffset time.Duration) []string {
	sec := func(x time.Duration) string { return strconv.FormatFloat(x.Seconds(), 'f', 3, 64) }
	return []string{"-hide_banner", "-loglevel", "error", "-nostdin",
		"-re", "-f", "lavfi", "-i", "color=c=0x101418:s=1280x720:r=25",
		"-re", "-f", "lavfi", "-i", "anullsrc=r=48000:cl=stereo",
		"-t", sec(d),
		"-c:v", "libx264", "-preset", "ultrafast", "-tune", "stillimage", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-b:a", "64k",
		"-output_ts_offset", sec(tsOffset),
		"-muxdelay", "0", "-muxpreload", "0",
		"-f", "mpegts", "pipe:1",
	}
}
