package api

// Usherr: where a video's intro and credits are, for the player's "Skip
// intro" button and an early "Up next" at the credits. Two sources, both
// cheap: the file's own chapters when they are named so ("Intro",
// "Opening", "OP", "Credits", "Ending", "ED"), and Jellyfin's media segments
// (found by its Intro Skipper plugin) for a library file it knows. Times are
// seconds into the file. Remembered per file or stream.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type playMarkers struct {
	IntroStart   float64 `json:"introStart,omitempty"`
	IntroEnd     float64 `json:"introEnd,omitempty"` // 0: no intro known
	CreditsStart float64 `json:"creditsStart,omitempty"`
	Source       string  `json:"source,omitempty"` // "chapters" or "jellyfin"
}

var (
	markersCache sync.Map // file or stream -> playMarkers
	introTitle   = regexp.MustCompile(`(?i)^\s*(intro|introduction|opening( credits| titles)?|op\d*|cold open)\s*$`)
	creditsTitle = regexp.MustCompile(`(?i)^\s*(credits|end credits|closing credits|ending( credits)?|ed\d*|outro)\s*$`)
)

// chapterMarkers reads the intro and credits from a file's chapters.
func chapterMarkers(ctx context.Context, source string) playMarkers {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-rw_timeout", "20000000", "-show_chapters", "-of", "json", source).Output()
	if err != nil {
		return playMarkers{}
	}
	var res struct {
		Chapters []struct {
			Start string            `json:"start_time"`
			End   string            `json:"end_time"`
			Tags  map[string]string `json:"tags"`
		} `json:"chapters"`
	}
	if json.Unmarshal(out, &res) != nil {
		return playMarkers{}
	}
	var m playMarkers
	for _, c := range res.Chapters {
		title := c.Tags["title"]
		start, _ := strconv.ParseFloat(c.Start, 64)
		end, _ := strconv.ParseFloat(c.End, 64)
		switch {
		case m.IntroEnd == 0 && introTitle.MatchString(title) && end > start && end-start < 300:
			m.IntroStart, m.IntroEnd = start, end
		case m.CreditsStart == 0 && creditsTitle.MatchString(title) && start > 60:
			m.CreditsStart = start
		}
	}
	if m.IntroEnd > 0 || m.CreditsStart > 0 {
		m.Source = "chapters"
	}
	return m
}

// jellyfinMarkers asks Jellyfin for a library file's intro and credits.
func (s *Server) jellyfinMarkers(ctx context.Context, file string) playMarkers {
	srv, ok := s.jellyfinServer()
	if !ok {
		return playMarkers{}
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	item, err := s.findJellyfinItem(ctx, srv, file)
	if err != nil || item == "" {
		return playMarkers{}
	}
	var res struct {
		Items []struct {
			Type       string `json:"Type"`
			StartTicks int64  `json:"StartTicks"`
			EndTicks   int64  `json:"EndTicks"`
		} `json:"Items"`
	}
	if jfJSON(ctx, srv, http.MethodGet, "/MediaSegments/"+url.PathEscape(item), nil, &res) != nil {
		return playMarkers{}
	}
	var m playMarkers
	for _, it := range res.Items {
		start, end := float64(it.StartTicks)/1e7, float64(it.EndTicks)/1e7
		switch strings.ToLower(it.Type) {
		case "intro":
			if m.IntroEnd == 0 && end > start {
				m.IntroStart, m.IntroEnd = start, end
			}
		case "outro":
			if m.CreditsStart == 0 && start > 60 {
				m.CreditsStart = start
			}
		}
	}
	if m.IntroEnd > 0 || m.CreditsStart > 0 {
		m.Source = "jellyfin"
	}
	return m
}

// markersFor is a file's or stream's intro and credits: its chapters first,
// else Jellyfin's (library files only).
func (s *Server) markersFor(ctx context.Context, source string, library bool) playMarkers {
	if v, ok := markersCache.Load(source); ok {
		return v.(playMarkers)
	}
	m := chapterMarkers(ctx, source)
	if m.Source == "" && library {
		m = s.jellyfinMarkers(ctx, source)
	}
	if ctx.Err() == nil {
		markersCache.Store(source, m)
	}
	return m
}

// GET /api/play/markers/{kind}/{id}: a library movie's or episode's intro
// and credits.
func (s *Server) handlePlayMarkers(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	kind := r.PathValue("kind")
	if err != nil || (kind != "movie" && kind != "episode") {
		writeJSON(w, http.StatusOK, playMarkers{})
		return
	}
	file, _, err := s.playFile(r, kind, id)
	if err != nil || file == "" {
		writeJSON(w, http.StatusOK, playMarkers{})
		return
	}
	writeJSON(w, http.StatusOK, s.markersFor(r.Context(), file, true))
}

// GET /api/stream/{id}/markers: a stream's intro and credits, from its
// chapters.
func (s *Server) handleStreamMarkers(w http.ResponseWriter, r *http.Request) {
	ss, ok := streams.get(r.PathValue("id"), requestUserID(r))
	if !ok {
		writeJSON(w, http.StatusOK, playMarkers{})
		return
	}
	streams.mu.Lock()
	target := ss.target
	streams.mu.Unlock()
	writeJSON(w, http.StatusOK, s.markersFor(r.Context(), target, false))
}
