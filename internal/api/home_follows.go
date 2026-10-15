package api

// Home fork: follow a person (an actor, director or writer). Each person in
// the household keeps their own list. What the people you follow have just
// made or have coming shows as a Discover row ("From people you follow") and
// on the Following tab of Upcoming, with a row per person. Their work is read
// from TMDB and kept for a few hours.

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/metadata"
)

const (
	followMax       = 200
	followWorkTTL   = 6 * time.Hour
	followNewMonths = 12 // "new" is out within this many months
	followPerPerson = 30
	followRowMax    = 60
)

type followed struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Image string `json:"image"`
	Known string `json:"known"`
}

func (s *Server) follows(ctx context.Context, uid int64) []followed {
	out := []followed{}
	rows, err := s.db.QueryContext(ctx, `SELECT person_id, name, image, known FROM home_follows WHERE user_id = ? ORDER BY name COLLATE NOCASE`, uid)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var f followed
		if rows.Scan(&f.ID, &f.Name, &f.Image, &f.Known) == nil {
			out = append(out, f)
		}
	}
	return out
}

func (s *Server) isFollowing(ctx context.Context, uid int64, personID int) bool {
	var n int
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM home_follows WHERE user_id = ? AND person_id = ?`, uid, personID).Scan(&n)
	return n > 0
}

// GET /api/home/follows: who you follow.
func (s *Server) handleFollows(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"people": s.follows(r.Context(), userID(r))})
}

// PUT /api/home/follows/{id} {"name","image","known"}: follow someone.
func (s *Server) handleFollow(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.PathValue("id"))
	var in followed
	if err := decodeJSON(r, &in); err != nil || id <= 0 || strings.TrimSpace(in.Name) == "" {
		writeError(w, http.StatusBadRequest, "Choose someone to follow.")
		return
	}
	ctx := r.Context()
	if !s.isFollowing(ctx, userID(r), id) && len(s.follows(ctx, userID(r))) >= followMax {
		writeError(w, http.StatusBadRequest, "You follow as many people as you can. Unfollow someone first.")
		return
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO home_follows (user_id, person_id, name, image, known) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (user_id, person_id) DO UPDATE SET name = excluded.name, image = excluded.image, known = excluded.known`,
		userID(r), id, strings.TrimSpace(in.Name), strings.TrimSpace(in.Image), strings.TrimSpace(in.Known))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Couldn't follow them.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"people": s.follows(ctx, userID(r))})
}

// DELETE /api/home/follows/{id}: stop following someone.
func (s *Server) handleUnfollow(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.PathValue("id"))
	_, _ = s.db.ExecContext(r.Context(), `DELETE FROM home_follows WHERE user_id = ? AND person_id = ?`, userID(r), id)
	writeJSON(w, http.StatusOK, map[string]any{"people": s.follows(r.Context(), userID(r))})
}

// A person's work as read from TMDB, kept for followWorkTTL.
type followWork struct {
	movies []metadata.Movie
	shows  []metadata.Show
	at     time.Time
}

var followCache sync.Map // person id -> followWork

func (s *Server) personWork(ctx context.Context, id int) (followWork, error) {
	if v, ok := followCache.Load(id); ok && time.Since(v.(followWork).at) < followWorkTTL {
		return v.(followWork), nil
	}
	movies, shows, err := s.displayTMDB().PersonWork(ctx, id)
	if err != nil {
		return followWork{}, err
	}
	fw := followWork{movies: movies, shows: shows, at: time.Now()}
	followCache.Store(id, fw)
	return fw, nil
}

// freshFirst keeps what's dated from `since` on and orders it: what's coming
// (soonest first), then what's out (newest first).
func freshFirst[T any](list []T, date func(T) string, since, today string) []T {
	var coming, out []T
	for _, x := range list {
		d := date(x)
		switch {
		case d == "" || d < since:
		case d > today:
			coming = append(coming, x)
		default:
			out = append(out, x)
		}
	}
	sort.SliceStable(coming, func(i, j int) bool { return date(coming[i]) < date(coming[j]) })
	sort.SliceStable(out, func(i, j int) bool { return date(out[i]) > date(out[j]) })
	return append(coming, out...)
}

func movieDate(m metadata.Movie) string { return m.ReleaseDate }
func showDate(sh metadata.Show) string  { return sh.FirstAirDate }

type followRow struct {
	followed
	Movies []browseItem `json:"movies"`
	Shows  []browseItem `json:"shows"`
	Fresh  int          `json:"fresh"` // how many of them are new or coming
}

// GET /api/home/follows/work: what the people you follow have new or coming,
// all together (movies and shows, each with who it's from) and per person.
func (s *Server) handleFollowWork(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	people := s.follows(ctx, userID(r))
	empty := map[string]any{"movies": []browseItem{}, "shows": []browseItem{}, "who": map[string]string{}, "people": []followRow{}}
	if len(people) == 0 || !s.TMDB().HasAPIKey() {
		writeJSON(w, http.StatusOK, empty)
		return
	}
	works := make([]followWork, len(people))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i, p := range people {
		wg.Add(1)
		go func(i, id int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			works[i], _ = s.personWork(ctx, id)
		}(i, p.ID)
	}
	wg.Wait()

	now := time.Now()
	today := now.Format("2006-01-02")
	since := now.AddDate(0, -followNewMonths, 0).Format("2006-01-02")
	var allMovies []metadata.Movie
	var allShows []metadata.Show
	who := map[string][]string{} // "movie:id" -> names
	rows := make([]followRow, 0, len(people))
	for i, p := range people {
		fm := freshFirst(works[i].movies, movieDate, since, today)
		fs := freshFirst(works[i].shows, showDate, since, today)
		for _, m := range fm {
			k := "movie:" + strconv.Itoa(m.TMDBID)
			if len(who[k]) == 0 {
				allMovies = append(allMovies, m)
			}
			who[k] = append(who[k], p.Name)
		}
		for _, sh := range fs {
			k := "tv:" + strconv.Itoa(sh.TMDBID)
			if len(who[k]) == 0 {
				allShows = append(allShows, sh)
			}
			who[k] = append(who[k], p.Name)
		}
		// Their row: what's new or coming, then their best-known work.
		rm, rs := fm, fs
		seenM, seenS := map[int]bool{}, map[int]bool{}
		for _, m := range fm {
			seenM[m.TMDBID] = true
		}
		for _, sh := range fs {
			seenS[sh.TMDBID] = true
		}
		for _, m := range works[i].movies {
			if len(rm) >= followPerPerson {
				break
			}
			if !seenM[m.TMDBID] {
				rm = append(rm, m)
			}
		}
		for _, sh := range works[i].shows {
			if len(rs) >= followPerPerson {
				break
			}
			if !seenS[sh.TMDBID] {
				rs = append(rs, sh)
			}
		}
		if len(rm) > followPerPerson {
			rm = rm[:followPerPerson]
		}
		if len(rs) > followPerPerson {
			rs = rs[:followPerPerson]
		}
		rows = append(rows, followRow{
			followed: p,
			Movies:   s.moviePagePayload(ctx, &metadata.MoviePage{Page: 1, TotalPages: 1, Results: rm}).Results,
			Shows:    s.showPagePayload(ctx, &metadata.ShowPage{Page: 1, TotalPages: 1, Results: rs}).Results,
			Fresh:    len(fm) + len(fs),
		})
	}
	// People with something new or coming first.
	sort.SliceStable(rows, func(i, j int) bool { return (rows[i].Fresh > 0) && rows[j].Fresh == 0 })
	allMovies = freshFirst(allMovies, movieDate, since, today)
	allShows = freshFirst(allShows, showDate, since, today)
	if len(allMovies) > followRowMax {
		allMovies = allMovies[:followRowMax]
	}
	if len(allShows) > followRowMax {
		allShows = allShows[:followRowMax]
	}
	names := map[string]string{}
	for k, list := range who {
		if len(list) > 2 {
			list = append(list[:2:2], "…")
		}
		names[k] = strings.Join(list, ", ")
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"movies": s.moviePagePayload(ctx, &metadata.MoviePage{Page: 1, TotalPages: 1, Results: allMovies}).Results,
		"shows":  s.showPagePayload(ctx, &metadata.ShowPage{Page: 1, TotalPages: 1, Results: allShows}).Results,
		"who":    names,
		"people": rows,
	})
}
