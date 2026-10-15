package api

// Home fork: what Mediarium TV's guide shows about a program, as
// NostalgiaTV's guide does: the title's logo and a wide picture, the episode,
// its description, when it first aired, its age rating and score, and
// whether it's HD or 4K. From the library where it can, TMDB for the rest
// (remembered for a while).

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/internal/tv"
)

type tvProgramInfo struct {
	Kind     string  `json:"kind"`
	Title    string  `json:"title"`
	Episode  string  `json:"episode,omitempty"` // "S1E34 - The Dump"
	Overview string  `json:"overview,omitempty"`
	AirDate  string  `json:"airDate,omitempty"` // first aired / released, "2019-04-08"
	Rating   string  `json:"rating,omitempty"`  // TV-G, PG-13
	Score    float64 `json:"score,omitempty"`   // TMDB's, out of 10
	Quality  string  `json:"quality,omitempty"` // SD, HD, 4K
	Logo     string  `json:"logo,omitempty"`
	Backdrop string  `json:"backdrop,omitempty"`
	Poster   string  `json:"poster,omitempty"`
}

var tvInfoCache sync.Map // channel:start -> tvInfoEntry

type tvInfoEntry struct {
	info tvProgramInfo
	at   time.Time
}

// GET /api/tv/channels/{id}/info?at=<unix ms>: the program on at that
// moment (now when left out), described.
func (s *Server) handleTVInfo(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a channel.")
		return
	}
	at := time.Now()
	if ms, err := strconv.ParseInt(r.URL.Query().Get("at"), 10, 64); err == nil && ms > 0 {
		at = time.UnixMilli(ms)
	}
	p, ok, err := s.tvRepo().At(id, at)
	if err != nil || !ok {
		writeError(w, http.StatusNotFound, "Nothing is on then.")
		return
	}
	key := strconv.FormatInt(id, 10) + ":" + strconv.FormatInt(p.Start.UnixMilli(), 10)
	if v, ok := tvInfoCache.Load(key); ok && time.Since(v.(tvInfoEntry).at) < 30*time.Minute {
		writeJSON(w, http.StatusOK, map[string]any{"program": p, "info": v.(tvInfoEntry).info})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	info := s.describeProgram(ctx, p)
	tvInfoCache.Store(key, tvInfoEntry{info, time.Now()})
	writeJSON(w, http.StatusOK, map[string]any{"program": p, "info": info})
}

// episodeLine turns "S01E34 · The Dump" into "S1E34 - The Dump".
func episodeLine(sub string) string {
	code, name, _ := strings.Cut(sub, " · ")
	var season, ep int
	if n, _ := sscanEpisode(code, &season, &ep); n == 2 {
		code = "S" + strconv.Itoa(season) + "E" + strconv.Itoa(ep)
	}
	if name == "" {
		return code
	}
	return code + " - " + name
}

func sscanEpisode(code string, season, ep *int) (int, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if !strings.HasPrefix(code, "S") {
		return 0, nil
	}
	s, e, ok := strings.Cut(code[1:], "E")
	if !ok {
		return 0, nil
	}
	a, err1 := strconv.Atoi(s)
	b, err2 := strconv.Atoi(e)
	if err1 != nil || err2 != nil {
		return 0, nil
	}
	*season, *ep = a, b
	return 2, nil
}

func qualityBadge(q string) string {
	q = strings.ToLower(q)
	switch {
	case strings.Contains(q, "2160"), strings.Contains(q, "4k"):
		return "4K"
	case strings.Contains(q, "1080"), strings.Contains(q, "720"):
		return "HD"
	case q == "":
		return ""
	}
	return "SD"
}

func (s *Server) describeProgram(ctx context.Context, p tv.Program) tvProgramInfo {
	info := tvProgramInfo{Kind: p.Kind, Title: p.Title}
	tmdbID := 0
	kind := "tv"
	switch p.Kind {
	case "live":
		info.Episode = p.Subtitle
		return info
	case "movie":
		kind = "movie"
		var overview, release, quality sql.NullString
		var tmdb sql.NullInt64
		_ = s.db.QueryRowContext(ctx, `SELECT tmdb_id, overview, release_date, quality FROM movies WHERE id = ?`, p.ItemID).
			Scan(&tmdb, &overview, &release, &quality)
		tmdbID = int(tmdb.Int64)
		info.Overview, info.AirDate, info.Quality = overview.String, release.String, qualityBadge(quality.String)
		info.Episode = p.Subtitle
	case "episode":
		info.Episode = episodeLine(p.Subtitle)
		var overview, aired, quality sql.NullString
		_ = s.db.QueryRowContext(ctx, `SELECT overview, air_date, quality FROM episodes WHERE id = ?`, p.ItemID).
			Scan(&overview, &aired, &quality)
		var tmdb sql.NullInt64
		_ = s.db.QueryRowContext(ctx, `SELECT tmdb_id FROM series WHERE id = ?`, p.SeriesID).Scan(&tmdb)
		tmdbID = int(tmdb.Int64)
		info.Overview, info.AirDate, info.Quality = overview.String, aired.String, qualityBadge(quality.String)
		if info.Quality == "" {
			info.Quality = qualityBadge(p.FilePath)
		}
	case "stream":
		info.Episode = episodeLine(p.Subtitle)
		info.Quality = "HD" // StreamNZB's 1080p stream
		id, season, ep, ok := tv.ParseStreamPath(p.FilePath)
		if ok {
			tmdbID = int(id)
			if eps, err := s.TMDB().GetSeason(ctx, tmdbID, season); err == nil {
				for _, e := range eps {
					if e.Episode == ep {
						info.Overview, info.AirDate = e.Overview, e.AirDate
					}
				}
			}
		}
	}
	if tmdbID <= 0 || s.TMDB() == nil {
		return info
	}
	if kind == "movie" {
		if d, err := s.TMDB().GetMovieDetail(ctx, tmdbID); err == nil {
			info.Rating, info.Score = d.Certification(), d.VoteAverage
			info.Backdrop, info.Poster = metadata.ImageURL("w1280", d.BackdropPath), metadata.ImageURL("w342", d.PosterPath)
			if info.Overview == "" {
				info.Overview = d.Overview
			}
		}
	} else if d, err := s.TMDB().GetShowFull(ctx, tmdbID); err == nil {
		info.Rating, info.Score = d.ContentRating(), d.VoteAverage
		info.Backdrop, info.Poster = metadata.ImageURL("w1280", d.BackdropPath), metadata.ImageURL("w342", d.PosterPath)
		if info.Overview == "" {
			info.Overview = d.Overview
		}
	}
	if logo, err := s.TMDB().TitleLogo(ctx, kind, tmdbID); err == nil {
		info.Logo = metadata.ImageURL("w500", logo)
	}
	return info
}

// ---- badges in the guide grid ----

// guideProgram is a program as the guide grid shows it: with its age rating
// and HD/4K, as NostalgiaTV's guide does.
type guideProgram struct {
	tv.Program
	Rating  string `json:"rating,omitempty"`
	Quality string `json:"quality,omitempty"`
}

var (
	tvRatings     sync.Map // "tv:<tmdb>" or "movie:<tmdb>" -> rating ("" when none)
	tvRatingsBusy sync.Map
)

// guideBadges looks badges up for one guide answer, remembering what it has
// asked the database already.
type guideBadges struct {
	s       *Server
	ctx     context.Context
	series  map[int64]int64   // library show -> tmdb id
	quality map[string]string // "episode:<id>"/"movie:<id>" -> quality
	movies  map[int64]int64   // library movie -> tmdb id
}

func (s *Server) newGuideBadges(ctx context.Context) *guideBadges {
	return &guideBadges{s: s, ctx: ctx, series: map[int64]int64{}, quality: map[string]string{}, movies: map[int64]int64{}}
}

func (g *guideBadges) of(p tv.Program) guideProgram {
	out := guideProgram{Program: p}
	switch p.Kind {
	case "episode":
		key := "episode:" + strconv.FormatInt(p.ItemID, 10)
		q, ok := g.quality[key]
		if !ok {
			var v sql.NullString
			_ = g.s.db.QueryRowContext(g.ctx, `SELECT quality FROM episodes WHERE id = ?`, p.ItemID).Scan(&v)
			q = qualityBadge(v.String)
			if q == "" {
				q = qualityBadge(p.FilePath)
			}
			g.quality[key] = q
		}
		out.Quality = q
		tmdb, ok := g.series[p.SeriesID]
		if !ok {
			var v sql.NullInt64
			_ = g.s.db.QueryRowContext(g.ctx, `SELECT tmdb_id FROM series WHERE id = ?`, p.SeriesID).Scan(&v)
			tmdb = v.Int64
			g.series[p.SeriesID] = tmdb
		}
		out.Rating = g.s.cachedRating("tv", int(tmdb))
	case "movie":
		key := "movie:" + strconv.FormatInt(p.ItemID, 10)
		q, ok := g.quality[key]
		tmdb := g.movies[p.ItemID]
		if !ok {
			var v sql.NullString
			var t sql.NullInt64
			_ = g.s.db.QueryRowContext(g.ctx, `SELECT quality, tmdb_id FROM movies WHERE id = ?`, p.ItemID).Scan(&v, &t)
			q, tmdb = qualityBadge(v.String), t.Int64
			g.quality[key], g.movies[p.ItemID] = q, tmdb
		}
		out.Quality = q
		out.Rating = g.s.cachedRating("movie", int(tmdb))
	case "stream":
		out.Quality = "HD"
		if id, _, _, ok := tv.ParseStreamPath(p.FilePath); ok {
			out.Rating = g.s.cachedRating("tv", int(id))
		}
	}
	return out
}

// cachedRating is a title's age rating if known; otherwise it's looked up in
// the background for next time, so the guide never waits on TMDB.
func (s *Server) cachedRating(kind string, tmdbID int) string {
	if tmdbID <= 0 || s.TMDB() == nil {
		return ""
	}
	key := kind + ":" + strconv.Itoa(tmdbID)
	if v, ok := tvRatings.Load(key); ok {
		return v.(string)
	}
	if _, busy := tvRatingsBusy.LoadOrStore(key, true); busy {
		return ""
	}
	go func() {
		defer tvRatingsBusy.Delete(key)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if kind == "movie" {
			if d, err := s.TMDB().GetMovieDetail(ctx, tmdbID); err == nil {
				tvRatings.Store(key, d.Certification())
			}
		} else if d, err := s.TMDB().GetShowFull(ctx, tmdbID); err == nil {
			tvRatings.Store(key, d.ContentRating())
		}
	}()
	return ""
}
