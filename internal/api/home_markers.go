package api

// Usherr: where a video's intro and credits are, for the player's "Skip
// intro" button and an early "Up next" at the credits. Two sources, both
// cheap: the file's own chapters when they are named so ("Intro",
// "Opening", "OP", "Credits", "Ending", "ED"), and Jellyfin's media segments
// (found by its Intro Skipper plugin) for a library file it knows, or Plex's
// markers (Plex Pass finds them) for a film or episode it has. Times are
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

	"github.com/rdborg/mediarium/internal/mediaservers"
)

type playMarkers struct {
	IntroStart   float64 `json:"introStart,omitempty"`
	IntroEnd     float64 `json:"introEnd,omitempty"` // 0: no intro known
	CreditsStart float64 `json:"creditsStart,omitempty"`
	Source       string  `json:"source,omitempty"` // "chapters", "jellyfin" or "plex"
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
	m := s.markersFor(r.Context(), file, true)
	if m.Source == "" {
		if pm := s.plexMarkers(r.Context(), kind, id); pm.Source != "" {
			markersCache.Store(file, pm)
			m = pm
		}
	}
	writeJSON(w, http.StatusOK, m)
}

// plexMarkers asks Plex for a library film's or episode's intro and
// credits, finding it there by its TMDB id (and, for an episode, its place).
func (s *Server) plexMarkers(ctx context.Context, kind string, id int64) playMarkers {
	servers := s.plexServers()
	if len(servers) == 0 || s.mediaFinder == nil {
		return playMarkers{}
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tmdb, season, episode := 0, 0, 0
	media := mediaservers.MediaMovie
	switch kind {
	case "movie":
		m, err := s.MovieRepo.Get(id)
		if err != nil {
			return playMarkers{}
		}
		tmdb = m.TMDBID
	case "episode":
		ep, err := s.MovieRepo.GetEpisodeByID(id)
		if err != nil {
			return playMarkers{}
		}
		sr, err := s.MovieRepo.GetSeries(ep.SeriesID)
		if err != nil {
			return playMarkers{}
		}
		tmdb, season, episode, media = sr.TMDBID, ep.Season, ep.Episode, mediaservers.MediaTV
	}
	if tmdb <= 0 {
		return playMarkers{}
	}
	for _, srv := range servers {
		item, ok := s.mediaFinder.Item(ctx, srv, media, tmdb)
		if !ok {
			continue
		}
		key := item.ID
		if kind == "episode" {
			if key, ok = s.mediaClient.PlexEpisodeKey(ctx, srv, item.ID, season, episode); !ok {
				continue
			}
		}
		mk, err := s.mediaClient.PlexMarkers(ctx, srv, key)
		if err != nil || (mk.IntroEnd == 0 && mk.CreditsStart == 0) {
			continue
		}
		return playMarkers{IntroStart: mk.IntroStart, IntroEnd: mk.IntroEnd, CreditsStart: mk.CreditsStart, Source: "plex"}
	}
	return playMarkers{}
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
