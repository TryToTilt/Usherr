package api

// Usherr: the episodes of streamed shows on the calendar. A show that is
// only in the streamed library (played through StreamNZB) has no episodes in
// Mediarium's own library, so its air dates come from TMDB instead: for each
// streamed show still running, the season out now and the next one, looked up
// in the background about twice a day.

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/metadata"
)

const streamedCalendarFor = 12 * time.Hour

var streamedCalendar struct {
	mu       sync.Mutex
	at       time.Time
	building bool
	entries  []calendarEntryPayload
}

// streamedCalendarEntries is what is known of streamed shows' episodes in the
// calendar's window. It starts a lookup when the last one is old, and answers
// with what it has meanwhile (nothing, the first time).
func (s *Server) streamedCalendarEntries() []calendarEntryPayload {
	c := &streamedCalendar
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.building && time.Since(c.at) > streamedCalendarFor && s.TMDB().HasAPIKey() {
		c.building = true
		s.background(func() {
			entries, err := s.buildStreamedCalendar(context.Background())
			c.mu.Lock()
			defer c.mu.Unlock()
			c.building = false
			if err != nil {
				slog.Info("calendar: streamed shows", "err", err)
				c.at = time.Now().Add(-streamedCalendarFor + 30*time.Minute) // try again in half an hour
				return
			}
			c.entries, c.at = entries, time.Now()
			slog.Info("calendar: streamed shows looked up", "episodes", len(entries))
		})
	}
	from := time.Now().UTC().Add(-calendarEpisodeLookback).Format("2006-01-02")
	to := time.Now().UTC().Add(calendarEpisodeLookahead).Format("2006-01-02")
	out := make([]calendarEntryPayload, 0, len(c.entries))
	for _, e := range c.entries {
		if e.ReleaseDate >= from && e.ReleaseDate <= to {
			out = append(out, e)
		}
	}
	return out
}

func (s *Server) buildStreamedCalendar(ctx context.Context) ([]calendarEntryPayload, error) {
	// Streamed shows that aren't also in the library (those are on the calendar already).
	rows, err := s.db.QueryContext(ctx, `SELECT t.id, t.tmdb_id, t.title, t.poster_path, t.anime FROM streamed_titles t
		WHERE t.kind = 'series' AND t.tmdb_id > 0 AND NOT EXISTS (SELECT 1 FROM series s WHERE s.tmdb_id = t.tmdb_id)`)
	if err != nil {
		return nil, err
	}
	type show struct {
		id     int64
		tmdb   int
		title  string
		poster string
		anime  bool
	}
	var shows []show
	for rows.Next() {
		var sh show
		var anime int
		if rows.Scan(&sh.id, &sh.tmdb, &sh.title, &sh.poster, &anime) == nil {
			sh.anime = anime == 1
			shows = append(shows, sh)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	tm := s.displayTMDB()
	from := time.Now().UTC().Add(-calendarEpisodeLookback).Format("2006-01-02")
	to := time.Now().UTC().Add(calendarEpisodeLookahead).Format("2006-01-02")
	var out []calendarEntryPayload
	for _, sh := range shows {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		d, err := tm.GetShow(ctx, sh.tmdb)
		time.Sleep(150 * time.Millisecond) // gentle on TMDB
		if err != nil || d.Status == "Ended" || d.Status == "Canceled" {
			continue
		}
		// The seasons with an episode in the window: the latest out and the next due.
		seasons := map[int]bool{}
		for _, ep := range []*metadata.EpisodeInfo{d.LastEpisodeToAir, d.NextEpisodeToAir} {
			if ep != nil && ep.AirDate >= from && ep.AirDate <= to {
				seasons[ep.Season] = true
			}
		}
		poster := metadata.PosterURL(sh.poster)
		if poster == "" {
			poster = metadata.PosterURL(d.PosterPath)
		}
		for season := range seasons {
			eps, err := tm.GetSeason(ctx, sh.tmdb, season)
			time.Sleep(150 * time.Millisecond)
			if err != nil {
				continue
			}
			for _, ep := range eps {
				if ep.AirDate < from || ep.AirDate > to {
					continue
				}
				label := fmt.Sprintf("S%02dE%02d", ep.Season, ep.Episode)
				if ep.Name != "" {
					label += " · " + ep.Name
				}
				out = append(out, calendarEntryPayload{
					Kind: "episode", ID: -(sh.id*100000 + int64(ep.Season)*1000 + int64(ep.Episode)), TMDBID: sh.tmdb,
					Season: ep.Season, Episode: ep.Episode, Title: sh.title, Subtitle: label, ReleaseDate: ep.AirDate,
					Status: "streams", PosterURL: poster, Anime: sh.anime,
				})
			}
		}
	}
	return out, nil
}
