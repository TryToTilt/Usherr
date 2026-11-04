package api

// Usherr: Continue watching. The films and episodes you stopped part way
// through in Usherr's own player, newest first, from home_positions (one row
// per account, so it is only ever your own). The dashboard shows them as a
// row; each opens the title's page, where Play picks up where you left off.

import (
	"net/http"
	"strconv"
	"strings"
)

type continueItem struct {
	Item      string  `json:"item"`
	Kind      string  `json:"kind"` // movie or tv
	Title     string  `json:"title"`
	Year      int     `json:"year,omitempty"`
	Episode   string  `json:"episode,omitempty"` // S02E05 · its name
	PosterURL string  `json:"posterUrl,omitempty"`
	Link      string  `json:"link"`
	Position  float64 `json:"position"`
	Duration  float64 `json:"duration"`
}

const continueMax = 12

// GET /api/home/continue (member): what you're part way through.
func (s *Server) handleContinue(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rows, err := s.db.QueryContext(ctx, `SELECT item, position, duration FROM home_positions
		WHERE user_id = ? AND (item LIKE 'movie:%' OR item LIKE 'episode:%' OR item LIKE 'title:%')
		ORDER BY updated_ms DESC LIMIT 60`, requestUserID(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Couldn't read where you got to.")
		return
	}
	type pos struct {
		item     string
		pos, dur float64
	}
	var all []pos
	for rows.Next() {
		var p pos
		if rows.Scan(&p.item, &p.pos, &p.dur) == nil {
			all = append(all, p)
		}
	}
	_ = rows.Close()

	out := []continueItem{}
	shows := map[string]bool{} // one episode per show: the latest
	for _, p := range all {
		if len(out) >= continueMax {
			break
		}
		c, ok := s.continueOf(r, p.item)
		if !ok || (c.Kind == "tv" && shows[c.Link]) {
			continue
		}
		if c.Kind == "tv" {
			shows[c.Link] = true
		}
		c.Item, c.Position, c.Duration = p.item, p.pos, p.dur
		out = append(out, c)
	}
	writeJSON(w, http.StatusOK, out)
}

// continueOf names a kept place: its title, poster and page. False for one
// whose title is gone from the library (or anything else).
func (s *Server) continueOf(r *http.Request, item string) (continueItem, bool) {
	ctx := r.Context()
	parts := strings.Split(item, ":")
	num := func(i int) int64 {
		if i >= len(parts) {
			return 0
		}
		n, _ := strconv.ParseInt(parts[i], 10, 64)
		return n
	}
	ep := func(season, episode int64, name string) string {
		c := "S" + pad2(season) + "E" + pad2(episode)
		if name != "" {
			c += " · " + name
		}
		return c
	}
	var c continueItem
	var poster string
	switch {
	case parts[0] == "movie" && len(parts) == 2:
		var tmdb int
		if s.db.QueryRowContext(ctx, `SELECT tmdb_id, title, year, poster_path FROM movies WHERE id = ?`, num(1)).Scan(&tmdb, &c.Title, &c.Year, &poster) != nil || tmdb <= 0 {
			return c, false
		}
		c.Kind, c.Link = "movie", "/title/"+strconv.Itoa(tmdb)
	case parts[0] == "episode" && len(parts) == 2:
		var seriesID, season, episode int64
		var name string
		if s.db.QueryRowContext(ctx, `SELECT s.id, s.title, s.year, s.poster_path, e.season, e.episode, e.title
			FROM episodes e JOIN series s ON s.id = e.series_id WHERE e.id = ?`, num(1)).Scan(&seriesID, &c.Title, &c.Year, &poster, &season, &episode, &name) != nil {
			return c, false
		}
		c.Kind, c.Link, c.Episode = "tv", "/series/"+strconv.FormatInt(seriesID, 10), ep(season, episode, name)
	case parts[0] == "title" && len(parts) >= 3 && (parts[1] == "movie" || parts[1] == "tv"):
		tmdb := num(2)
		kind, stored := parts[1], parts[1]
		if kind == "tv" {
			stored = "series" // how streamed_titles names a show
		}
		if tmdb <= 0 || s.db.QueryRowContext(ctx, `SELECT title, year, poster_path FROM streamed_titles WHERE kind = ? AND tmdb_id = ?`, stored, tmdb).Scan(&c.Title, &c.Year, &poster) != nil {
			return c, false
		}
		c.Kind = kind
		if kind == "movie" {
			c.Link = "/title/" + strconv.FormatInt(tmdb, 10)
		} else {
			c.Link = "/show/" + strconv.FormatInt(tmdb, 10)
			if len(parts) == 5 {
				c.Episode = ep(num(3), num(4), "")
			}
		}
	default:
		return c, false
	}
	if poster != "" {
		c.PosterURL = tmdbPoster(poster)
	}
	return c, true
}

func pad2(n int64) string {
	if n < 10 && n >= 0 {
		return "0" + strconv.FormatInt(n, 10)
	}
	return strconv.FormatInt(n, 10)
}
