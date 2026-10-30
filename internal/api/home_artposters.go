package api

// Usherr: Re-Posterr's posters in Usherr too. A poster Re-Posterr puts on a
// title in Jellyfin (picked or made in the style) is kept in the artwork
// folder, and Usherr's own pages show it instead of TMDB's: every poster
// Usherr shows comes through metadata.PosterURL, which swaps a title's TMDB
// poster path for this one. Putting the old one back takes the swap away.

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/metadata"
)

func (s *Server) artPosterFile(id int64) string {
	return filepath.Join(s.artDir(), "posters", strconv.FormatInt(id, 10)+".jpg")
}

// artSavePoster keeps the poster a change put on, as Jellyfin got it.
func (s *Server) artSavePoster(id int64, data []byte) {
	f := s.artPosterFile(id)
	if os.MkdirAll(filepath.Dir(f), 0o755) == nil {
		_ = os.WriteFile(f, data, 0o644)
	}
}

var artSwapsTimer struct {
	mu sync.Mutex
	t  *time.Timer
}

// artSwapsSoon works the swaps out again shortly (once for a run of changes).
func (s *Server) artSwapsSoon() {
	artSwapsTimer.mu.Lock()
	defer artSwapsTimer.mu.Unlock()
	if artSwapsTimer.t != nil {
		artSwapsTimer.t.Stop()
	}
	artSwapsTimer.t = time.AfterFunc(3*time.Second, func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		s.artPosterSwaps(ctx)
	})
}

// artPosterSwaps works out which titles show Re-Posterr's poster: each
// title's latest poster change that's on, by the TMDB poster paths Usherr
// has for it (the library's, the streamed copy's, the collection's).
func (s *Server) artPosterSwaps(ctx context.Context) {
	rows, err := s.db.QueryContext(ctx, `SELECT kind, tmdb_id, MAX(id) FROM art_changes WHERE type = 'poster' AND state = 'done' GROUP BY kind, tmdb_id`)
	if err != nil {
		return
	}
	type on struct {
		kind string
		tmdb int
		id   int64
	}
	var list []on
	for rows.Next() {
		var o on
		if rows.Scan(&o.kind, &o.tmdb, &o.id) == nil {
			list = append(list, o)
		}
	}
	rows.Close()
	paths := map[string]string{
		"movie":      `SELECT poster_path FROM movies WHERE tmdb_id = ?1 UNION SELECT poster_path FROM streamed_titles WHERE kind = 'movie' AND tmdb_id = ?1`,
		"tv":         `SELECT poster_path FROM series WHERE tmdb_id = ?1 UNION SELECT poster_path FROM streamed_titles WHERE kind = 'series' AND tmdb_id = ?1`,
		"collection": `SELECT poster_path FROM collections WHERE tmdb_id = ?1`,
	}
	m := map[string]string{}
	for _, o := range list {
		q := paths[o.kind]
		if q == "" {
			continue
		}
		prow, err := s.db.QueryContext(ctx, q, o.tmdb)
		if err != nil {
			continue
		}
		for prow.Next() {
			var p sql.NullString
			if prow.Scan(&p) == nil && p.String != "" {
				m[p.String] = fmt.Sprintf("/api/home/art/poster/%d", o.id)
			}
		}
		prow.Close()
	}
	metadata.SetPosterSwaps(m)
}

// GET /api/home/art/poster/{id} (members): the poster a change put on.
// Pictures from before Usherr kept them are made again: the one picked is
// downloaded, one in the style is drawn in today's style.
func (s *Server) handleArtPoster(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	f := s.artPosterFile(id)
	data, err := os.ReadFile(f)
	if err != nil {
		list, lerr := s.artChanges(r.Context(), `WHERE c.id = ? AND c.type = 'poster'`, id)
		if lerr != nil || len(list) == 0 {
			http.NotFound(w, r)
			return
		}
		c := list[0]
		ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
		defer cancel()
		if c.Style != "" {
			styles, _ := s.artStylesNow()
			var tvdb, year int
			_ = s.db.QueryRowContext(ctx, `SELECT tvdb_id, year FROM art_titles WHERE kind = ? AND tmdb_id = ?`, c.Kind, c.TMDBID).Scan(&tvdb, &year)
			data, err = s.artStyledPoster(ctx, c, styles.Poster, tvdb, year)
		} else {
			data, _, err = artFetch(ctx, c.URL, "poster")
		}
		if err != nil {
			http.NotFound(w, r)
			return
		}
		s.artSavePoster(id, data)
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable") // a new poster is a new address
	_, _ = w.Write(data)
}

// artStyledPoster draws a title's poster in the style, full size.
func (s *Server) artStyledPoster(ctx context.Context, c artChange, st artStyle, tvdb, year int) ([]byte, error) {
	art, logo, err := s.artPosterArt(ctx, c.Kind, c.TMDBID, tvdb, c.URL, st, "poster")
	if err != nil {
		return nil, err
	}
	return artJPEG(artDraw(art, artPosterW, artPosterH, st, artPosterWords(st, c.Kind, c.Title, year, c.Words), logo, artExtras{dir: s.artDir()}), 88)
}
