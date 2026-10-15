package api

// Home fork: thumbs up and down. Each person can vote on any film, show,
// artist, album, book or comic series, in the library or not. Votes are the
// person's own: a thumbs down hides the title from their Discover, and the
// votes are what the taste profile learns from.

import (
	"context"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/metadata"
)

var voteKinds = map[string]bool{"movie": true, "tv": true, "artist": true, "album": true, "book": true, "comic": true}

type votePayload struct {
	Kind      string `json:"kind"`
	Ref       string `json:"ref"`
	Vote      int    `json:"vote"`
	Title     string `json:"title,omitempty"`
	Year      int    `json:"year,omitempty"`
	Image     string `json:"image,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

func (s *Server) votesOf(ctx context.Context, uid int64, kind string, limit int) []votePayload {
	q := `SELECT kind, ref, vote, title, year, image, updated_at FROM home_votes WHERE user_id = ?`
	args := []any{uid}
	if kind != "" {
		q += ` AND kind = ?`
		args = append(args, kind)
	}
	q += ` ORDER BY updated_at DESC`
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	out := []votePayload{}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var v votePayload
		if rows.Scan(&v.Kind, &v.Ref, &v.Vote, &v.Title, &v.Year, &v.Image, &v.UpdatedAt) == nil {
			out = append(out, v)
		}
	}
	return out
}

// GET /api/home/votes?kind=: your votes, newest first.
func (s *Server) handleVotes(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	if kind != "" && !voteKinds[kind] {
		writeError(w, http.StatusBadRequest, "That kind can't be voted on.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"votes": s.votesOf(r.Context(), userID(r), kind, 0)})
}

// PUT /api/home/votes {"kind","ref","vote":1|-1|0,"title","year","image"}:
// vote, or take a vote back with 0.
func (s *Server) handlePutVote(w http.ResponseWriter, r *http.Request) {
	var in votePayload
	if err := decodeJSON(r, &in); err != nil || !voteKinds[in.Kind] || strings.TrimSpace(in.Ref) == "" || len(in.Ref) > 80 || in.Vote < -1 || in.Vote > 1 {
		writeError(w, http.StatusBadRequest, "That vote couldn't be read.")
		return
	}
	ctx, me := r.Context(), userID(r)
	var err error
	if in.Vote == 0 {
		_, err = s.db.ExecContext(ctx, `DELETE FROM home_votes WHERE user_id = ? AND kind = ? AND ref = ?`, me, in.Kind, in.Ref)
	} else {
		title := strings.TrimSpace(in.Title)
		if len([]rune(title)) > 200 {
			title = string([]rune(title)[:200])
		}
		_, err = s.db.ExecContext(ctx, `INSERT INTO home_votes (user_id, kind, ref, vote, title, year, image) VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (user_id, kind, ref) DO UPDATE SET vote = excluded.vote, title = excluded.title, year = excluded.year,
			image = excluded.image, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')`,
			me, in.Kind, in.Ref, in.Vote, title, in.Year, strings.TrimSpace(in.Image))
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The vote couldn't be saved.")
		return
	}
	tasteChanged(me)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// GET /api/home/votes/deck?kind=movie|tv: about 20 titles to vote on quickly,
// none voted on yet: half from what the house has, half well-known titles.
func (s *Server) handleVoteDeck(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	if kind != "tv" {
		kind = "movie"
	}
	ctx := r.Context()
	voted := map[int]bool{}
	for _, v := range s.votesOf(ctx, userID(r), kind, 0) {
		if id, err := strconv.Atoi(v.Ref); err == nil {
			voted[id] = true
		}
	}
	rnd := rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), rand.Uint64()))
	h := s.have(ctx)
	sets := h.Movies
	if kind == "tv" {
		sets = h.Shows
	}
	var mine []int
	for _, l := range [][]int{sets.Disk, sets.Streamed} {
		for _, id := range l {
			if !voted[id] {
				mine = append(mine, id)
			}
		}
	}
	rnd.Shuffle(len(mine), func(i, j int) { mine[i], mine[j] = mine[j], mine[i] })
	if len(mine) > 10 {
		mine = mine[:10]
	}
	tm := s.displayTMDB()
	v := url.Values{"include_adult": {"false"}, "sort_by": {"vote_count.desc"}, "page": {strconv.Itoa(1 + rnd.IntN(10))}}
	var movies []metadata.Movie
	var shows []metadata.Show
	seen := map[int]bool{}
	if kind == "tv" {
		for _, id := range mine {
			if d, err := tm.GetShow(ctx, id); err == nil && !seen[id] {
				seen[id] = true
				shows = append(shows, d.Show)
			}
		}
		if p, err := tm.DiscoverTVWith(ctx, v); err == nil {
			for _, sh := range p.Results {
				if !voted[sh.TMDBID] && !seen[sh.TMDBID] && len(shows) < 20 {
					seen[sh.TMDBID] = true
					shows = append(shows, sh)
				}
			}
		}
		rnd.Shuffle(len(shows), func(i, j int) { shows[i], shows[j] = shows[j], shows[i] })
		writeJSON(w, http.StatusOK, map[string]any{"items": s.showPagePayload(ctx, &metadata.ShowPage{Page: 1, TotalPages: 1, Results: shows}).Results})
		return
	}
	for _, id := range mine {
		if m, err := tm.GetMovie(ctx, id); err == nil && !seen[id] {
			seen[id] = true
			movies = append(movies, *m)
		}
	}
	if p, err := tm.DiscoverMoviesWith(ctx, v); err == nil {
		for _, m := range p.Results {
			if !voted[m.TMDBID] && !seen[m.TMDBID] && len(movies) < 20 {
				seen[m.TMDBID] = true
				movies = append(movies, m)
			}
		}
	}
	rnd.Shuffle(len(movies), func(i, j int) { movies[i], movies[j] = movies[j], movies[i] })
	writeJSON(w, http.StatusOK, map[string]any{"items": s.moviePagePayload(ctx, &metadata.MoviePage{Page: 1, TotalPages: 1, Results: movies}).Results})
}
