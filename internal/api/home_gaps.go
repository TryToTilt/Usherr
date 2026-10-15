package api

// Home fork: "Complete your library", after CinePlete. A scan (nightly, or
// Rescan) looks at every film the household has (on disk, streamed, or in
// the media server) and works out what's missing: films from the series you
// have part of, from directors you collect, popular films of actors you
// collect, acclaimed classics, and the films your own library recommends
// most. Each film's people and recommendations are read from TMDB once and
// kept, so later scans only read new films. Each person can ignore a series,
// a director or an actor.

import (
	"context"
	"encoding/json"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/metadata"
)

const (
	gapsEvery           = 24 * time.Hour
	gapsDirectorMin     = 3   // films of theirs you have, to count as collected
	gapsActorMin        = 4   //
	gapsActorMinVotes   = 500 // only an actor's well-known films
	gapsDirectorMinVote = 50  // leaves out shorts and student films nobody saw
	gapsPerPerson       = 25
	gapsActorPer        = 10
	gapsClassicsPages   = 6
	gapsSuggestMin      = 2
	gapsSuggestMax      = 100
	gapsSuggestLook     = 600 // candidates read, to find 100 well-known ones
	gapsSuggestVotes    = 500 // leaves out little-known TV films that recommend each other
	gapsPeopleRefresh   = 180 // days before a film's people and recommendations are read again
)

type gapFilm struct {
	TMDBID      int     `json:"tmdbId"`
	Title       string  `json:"title"`
	Year        int     `json:"year"`
	PosterURL   string  `json:"posterUrl,omitempty"`
	Rating      float64 `json:"rating,omitempty"`
	VoteCount   int     `json:"voteCount,omitempty"`
	ReleaseDate string  `json:"releaseDate,omitempty"`
	Matches     int     `json:"matches,omitempty"` // suggestions: how many of your films recommend it
}

func gapFilmOf(m metadata.Movie) gapFilm {
	return gapFilm{TMDBID: m.TMDBID, Title: m.Title, Year: m.Year(), PosterURL: metadata.PosterURL(m.PosterPath), Rating: m.VoteAverage, VoteCount: m.VoteCount, ReleaseDate: m.ReleaseDate}
}

type gapGroup struct {
	ID      int       `json:"id"`
	Name    string    `json:"name"`
	Have    int       `json:"have"`
	Total   int       `json:"total"` // franchises: released parts; people: have + missing shown
	Missing []gapFilm `json:"missing"`
}

type gapScore struct {
	Have  int `json:"have"`
	Total int `json:"total"`
	Pct   int `json:"pct"`
}

func scoreOf(have, total int) gapScore {
	s := gapScore{Have: have, Total: total}
	if total > 0 {
		s.Pct = have * 100 / total
	}
	return s
}

type gapsResult struct {
	BuiltAt     string     `json:"builtAt"`
	Owned       int        `json:"owned"`
	Scores      gapScores  `json:"scores"`
	Franchises  []gapGroup `json:"franchises"`
	Directors   []gapGroup `json:"directors"`
	Actors      []gapGroup `json:"actors"`
	Classics    []gapFilm  `json:"classics"`
	Suggestions []gapFilm  `json:"suggestions"`
}

type gapScores struct {
	Franchises gapScore `json:"franchises"`
	Directors  gapScore `json:"directors"`
	Classics   gapScore `json:"classics"`
	Global     int      `json:"global"`
}

// The scan's progress, for the page.
var (
	gapsRunMu   sync.Mutex
	gapsRunning bool
	gapsStep    int
	gapsLabel   string
	gapsDone    int
	gapsOfN     int
)

const gapsSteps = 7

func gapsProgress(step int, label string, done, of int) {
	gapsRunMu.Lock()
	gapsStep, gapsLabel, gapsDone, gapsOfN = step, label, done, of
	gapsRunMu.Unlock()
}

// gapsJob is the nightly scan (the automation job).
func (s *Server) gapsJob(ctx context.Context) {
	if !s.TMDB().HasAPIKey() {
		return
	}
	s.runGapsScan(ctx)
}

func (s *Server) runGapsScan(ctx context.Context) {
	gapsRunMu.Lock()
	if gapsRunning {
		gapsRunMu.Unlock()
		return
	}
	gapsRunning = true
	gapsRunMu.Unlock()
	defer func() {
		gapsRunMu.Lock()
		gapsRunning = false
		gapsRunMu.Unlock()
	}()
	start := time.Now()
	res, err := s.scanGaps(ctx)
	if err != nil {
		slog.Warn("complete your library: scan failed", "err", err)
		return
	}
	b, _ := json.Marshal(res)
	_, _ = s.db.ExecContext(ctx, `INSERT OR REPLACE INTO home_gaps (id, data, built_at) VALUES (1, ?, ?)`, string(b), res.BuiltAt)
	slog.Info("complete your library: scanned", "films", res.Owned, "took", time.Since(start).Round(time.Second))
}

func (s *Server) scanGaps(ctx context.Context) (*gapsResult, error) {
	tm := s.displayTMDB()
	gapsProgress(1, "Reading your films", 0, 0)
	ownedMu.Lock()
	ownedMovs = nil // a fresh look
	ownedMu.Unlock()
	have := s.ownedMovies(ctx)
	res := &gapsResult{BuiltAt: time.Now().UTC().Format(time.RFC3339), Owned: len(have), Franchises: []gapGroup{}, Directors: []gapGroup{}, Actors: []gapGroup{}, Classics: []gapFilm{}, Suggestions: []gapFilm{}}
	today := time.Now().Format("2006-01-02")

	// 2. Film series.
	gapsProgress(2, "Finding the film series", 0, 0)
	s.fillMovieCollections(ctx)
	ownedIn := map[int]int{}
	if rows, err := s.db.QueryContext(ctx, `SELECT tmdb_id, collection_id FROM home_movie_collections WHERE collection_id > 0`); err == nil {
		for rows.Next() {
			var id, col int
			if rows.Scan(&id, &col) == nil && have[id] {
				ownedIn[col]++
			}
		}
		rows.Close()
	}
	colIDs := make([]int, 0, len(ownedIn))
	for id := range ownedIn {
		colIDs = append(colIDs, id)
	}
	cols := make([]*metadata.Collection, len(colIDs))
	parallel(len(colIDs), 8, func(i int) {
		if c, err := tm.GetCollection(ctx, colIDs[i]); err == nil {
			cols[i] = c
		}
		gapsProgress(2, "Finding the film series", i+1, len(colIDs))
	})
	var fHave, fTotal int
	for _, col := range cols {
		if col == nil {
			continue
		}
		g := gapGroup{ID: col.ID, Name: col.Name, Missing: []gapFilm{}}
		for _, p := range col.Parts {
			if p.ReleaseDate == "" || p.ReleaseDate > today {
				continue
			}
			g.Total++
			if have[p.TMDBID] {
				g.Have++
			} else {
				g.Missing = append(g.Missing, gapFilm{TMDBID: p.TMDBID, Title: p.DisplayTitle(), Year: yearOfDate(p.ReleaseDate), PosterURL: metadata.PosterURL(p.PosterPath), Rating: p.VoteAverage, ReleaseDate: p.ReleaseDate})
			}
		}
		if g.Total < 2 || g.Have == 0 {
			continue
		}
		fHave += g.Have
		fTotal += g.Total
		sort.SliceStable(g.Missing, func(i, j int) bool { return g.Missing[i].ReleaseDate < g.Missing[j].ReleaseDate })
		if len(g.Missing) > 0 {
			res.Franchises = append(res.Franchises, g)
		}
	}
	sort.SliceStable(res.Franchises, func(i, j int) bool {
		a, b := res.Franchises[i], res.Franchises[j]
		if len(a.Missing) != len(b.Missing) {
			return len(a.Missing) < len(b.Missing)
		}
		if a.Have != b.Have {
			return a.Have > b.Have
		}
		return a.Name < b.Name
	})
	res.Scores.Franchises = scoreOf(fHave, fTotal)

	// 3. Who made and leads each film, and what it recommends (read once).
	people := s.fillFilmPeople(ctx, have)

	// 4. Directors you collect.
	dirCount, dirName := map[int]int{}, map[int]string{}
	actCount, actName := map[int]int{}, map[int]string{}
	recCount := map[int]int{}
	for id, fp := range people {
		if !have[id] {
			continue
		}
		for _, d := range fp.Directors {
			dirCount[d.ID]++
			dirName[d.ID] = d.Name
		}
		for _, a := range fp.Cast {
			actCount[a.ID]++
			actName[a.ID] = a.Name
		}
		for _, r := range fp.Recs {
			recCount[r]++
		}
	}
	collected := func(counts map[int]int, min int) []int {
		var ids []int
		for id, n := range counts {
			if n >= min {
				ids = append(ids, id)
			}
		}
		sort.Slice(ids, func(i, j int) bool {
			return counts[ids[i]] > counts[ids[j]] || (counts[ids[i]] == counts[ids[j]] && ids[i] < ids[j])
		})
		return ids
	}
	dirs := collected(dirCount, gapsDirectorMin)
	dirGroups := make([]*gapGroup, len(dirs))
	parallel(len(dirs), 6, func(i int) {
		gapsProgress(4, "Looking at the directors you collect", i+1, len(dirs))
		directed, _, err := tm.PersonFilms(ctx, dirs[i])
		if err != nil {
			return
		}
		g := &gapGroup{ID: dirs[i], Name: dirName[dirs[i]], Missing: []gapFilm{}}
		sort.SliceStable(directed, func(a, b int) bool { return directed[a].Popularity > directed[b].Popularity })
		for _, m := range directed {
			if m.ReleaseDate == "" || m.ReleaseDate > today || m.VoteCount < gapsDirectorMinVote {
				continue
			}
			if have[m.TMDBID] {
				g.Have++
				continue
			}
			if len(g.Missing) < gapsPerPerson {
				g.Missing = append(g.Missing, gapFilmOf(m))
			}
		}
		g.Total = g.Have + len(g.Missing)
		dirGroups[i] = g
	})
	var dHave, dTotal int
	for _, g := range dirGroups {
		if g == nil || g.Have == 0 {
			continue
		}
		dHave += g.Have
		dTotal += g.Total
		if len(g.Missing) > 0 {
			res.Directors = append(res.Directors, *g)
		}
	}
	res.Scores.Directors = scoreOf(dHave, dTotal)

	// 5. Actors you collect: their well-known films.
	acts := collected(actCount, gapsActorMin)
	actGroups := make([]*gapGroup, len(acts))
	parallel(len(acts), 6, func(i int) {
		gapsProgress(5, "Looking at the actors you collect", i+1, len(acts))
		_, acted, err := tm.PersonFilms(ctx, acts[i])
		if err != nil {
			return
		}
		g := &gapGroup{ID: acts[i], Name: actName[acts[i]], Have: actCount[acts[i]], Missing: []gapFilm{}}
		sort.SliceStable(acted, func(a, b int) bool { return acted[a].Popularity > acted[b].Popularity })
		seen := map[int]bool{}
		for _, m := range acted {
			if seen[m.TMDBID] || have[m.TMDBID] || m.Order > 8 || m.VoteCount < gapsActorMinVotes || m.ReleaseDate == "" || m.ReleaseDate > today || hasGenreID(m.GenreIDs, 99) {
				continue
			}
			seen[m.TMDBID] = true
			g.Missing = append(g.Missing, gapFilmOf(m.Movie))
			if len(g.Missing) == gapsActorPer {
				break
			}
		}
		g.Total = g.Have + len(g.Missing)
		actGroups[i] = g
	})
	for _, g := range actGroups {
		if g != nil && len(g.Missing) > 0 {
			res.Actors = append(res.Actors, *g)
		}
	}

	// 6. Classics: TMDB's best rated, well known.
	var cHave, cTotal int
	for page := 1; page <= gapsClassicsPages; page++ {
		gapsProgress(6, "Checking the classics", page, gapsClassicsPages)
		p, err := tm.DiscoverMoviesWith(ctx, url.Values{"sort_by": {"vote_average.desc"}, "vote_count.gte": {"5000"}, "vote_average.gte": {"8"}, "include_adult": {"false"}, "page": {strconv.Itoa(page)}})
		if err != nil || len(p.Results) == 0 {
			break
		}
		for _, m := range p.Results {
			cTotal++
			if have[m.TMDBID] {
				cHave++
			} else {
				res.Classics = append(res.Classics, gapFilmOf(m))
			}
		}
		if page >= p.TotalPages {
			break
		}
	}
	res.Scores.Classics = scoreOf(cHave, cTotal)

	// 7. What your library recommends most.
	type rec struct{ id, n int }
	var recs []rec
	for id, n := range recCount {
		if n >= gapsSuggestMin && !have[id] {
			recs = append(recs, rec{id, n})
		}
	}
	sort.Slice(recs, func(i, j int) bool {
		return recs[i].n > recs[j].n || (recs[i].n == recs[j].n && recs[i].id < recs[j].id)
	})
	if len(recs) > gapsSuggestLook {
		recs = recs[:gapsSuggestLook]
	}
	sugg := make([]*gapFilm, len(recs))
	parallel(len(recs), 8, func(i int) {
		gapsProgress(7, "Gathering what your library recommends", i+1, len(recs))
		m, err := tm.GetMovie(ctx, recs[i].id)
		if err != nil || m.ReleaseDate == "" || m.ReleaseDate > today || m.VoteCount < gapsSuggestVotes {
			return
		}
		f := gapFilmOf(*m)
		f.Matches = recs[i].n
		sugg[i] = &f
	})
	// Most recommended first, weighed by how widely known the film is, so a
	// cluster of similar TV films can't fill the list on its own.
	var picked []gapFilm
	for _, f := range sugg {
		if f != nil {
			picked = append(picked, *f)
		}
	}
	weight := func(f gapFilm) float64 { return float64(f.Matches) * math.Log10(float64(max(f.VoteCount, 10))) }
	sort.SliceStable(picked, func(i, j int) bool { return weight(picked[i]) > weight(picked[j]) })
	if len(picked) > gapsSuggestMax {
		picked = picked[:gapsSuggestMax]
	}
	res.Suggestions = append(res.Suggestions, picked...)

	n := 0
	sum := 0
	for _, sc := range []gapScore{res.Scores.Franchises, res.Scores.Directors, res.Scores.Classics} {
		if sc.Total > 0 {
			sum += sc.Pct
			n++
		}
	}
	if n > 0 {
		res.Scores.Global = sum / n
	}
	return res, nil
}

func hasGenreID(ids []int, g int) bool {
	for _, x := range ids {
		if x == g {
			return true
		}
	}
	return false
}

func yearOfDate(d string) int {
	if len(d) >= 4 {
		y, _ := strconv.Atoi(d[:4])
		return y
	}
	return 0
}

// parallel runs fn(0..n-1), at most k at a time.
func parallel(n, k int, fn func(i int)) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, k)
	for i := 0; i < n; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			fn(i)
		}()
	}
	wg.Wait()
}

// fillFilmPeople reads, for each film you have that hasn't been read (or was
// read long ago), its directors, leading cast and recommendations; and
// returns them all.
func (s *Server) fillFilmPeople(ctx context.Context, have map[int]bool) map[int]metadata.FilmPeople {
	out := map[int]metadata.FilmPeople{}
	fresh := map[int]bool{}
	cut := time.Now().AddDate(0, 0, -gapsPeopleRefresh).UTC().Format(time.RFC3339)
	if rows, err := s.db.QueryContext(ctx, `SELECT tmdb_id, directors, leads, recs, fetched_at FROM home_film_people`); err == nil {
		for rows.Next() {
			var id int
			var d, c, r, at string
			if rows.Scan(&id, &d, &c, &r, &at) != nil {
				continue
			}
			var fp metadata.FilmPeople
			_ = json.Unmarshal([]byte(d), &fp.Directors)
			_ = json.Unmarshal([]byte(c), &fp.Cast)
			_ = json.Unmarshal([]byte(r), &fp.Recs)
			out[id] = fp
			fresh[id] = at > cut
		}
		rows.Close()
	}
	var todo []int
	for id := range have {
		if !fresh[id] {
			todo = append(todo, id)
		}
	}
	var mu sync.Mutex
	done := 0
	parallel(len(todo), 6, func(i int) {
		if ctx.Err() != nil {
			return
		}
		fp, err := s.TMDB().GetFilmPeople(ctx, todo[i])
		mu.Lock()
		done++
		gapsProgress(3, "Reading who made your films", done, len(todo))
		mu.Unlock()
		if err != nil {
			return
		}
		d, _ := json.Marshal(fp.Directors)
		c, _ := json.Marshal(fp.Cast)
		r, _ := json.Marshal(fp.Recs)
		_, _ = s.db.ExecContext(ctx, `INSERT OR REPLACE INTO home_film_people (tmdb_id, directors, leads, recs, fetched_at) VALUES (?, ?, ?, ?, ?)`, todo[i], string(d), string(c), string(r), time.Now().UTC().Format(time.RFC3339))
		mu.Lock()
		out[todo[i]] = *fp
		mu.Unlock()
		time.Sleep(60 * time.Millisecond) // gentle with TMDB
	})
	return out
}

// ---- the page ----

type gapIgnore struct {
	Kind string `json:"kind"`
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func (s *Server) gapIgnores(ctx context.Context, userID int64) []gapIgnore {
	out := []gapIgnore{}
	rows, err := s.db.QueryContext(ctx, `SELECT kind, ref_id, name FROM home_gap_ignores WHERE user_id = ? ORDER BY kind, name`, userID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var g gapIgnore
		if rows.Scan(&g.Kind, &g.ID, &g.Name) == nil {
			out = append(out, g)
		}
	}
	return out
}

// GET /api/home/gaps: the latest findings, less what you ignore, and the scan's progress.
func (s *Server) handleGaps(w http.ResponseWriter, r *http.Request) {
	var raw string
	_ = s.db.QueryRowContext(r.Context(), `SELECT data FROM home_gaps WHERE id = 1`).Scan(&raw)
	var res *gapsResult
	if raw != "" {
		res = &gapsResult{}
		if json.Unmarshal([]byte(raw), res) != nil {
			res = nil
		}
	}
	ignores := s.gapIgnores(r.Context(), userID(r))
	if res != nil {
		skip := map[string]bool{}
		for _, g := range ignores {
			skip[g.Kind+":"+strconv.Itoa(g.ID)] = true
		}
		keep := func(kind string, list []gapGroup) []gapGroup {
			out := list[:0:0]
			for _, g := range list {
				if !skip[kind+":"+strconv.Itoa(g.ID)] {
					out = append(out, g)
				}
			}
			return out
		}
		res.Franchises = keep("franchise", res.Franchises)
		res.Directors = keep("director", res.Directors)
		res.Actors = keep("actor", res.Actors)
	}
	gapsRunMu.Lock()
	status := map[string]any{"running": gapsRunning, "step": gapsStep, "steps": gapsSteps, "label": gapsLabel, "done": gapsDone, "of": gapsOfN}
	gapsRunMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"result": res, "ignored": ignores, "scan": status})
}

// POST /api/home/gaps/scan: scan now.
func (s *Server) handleGapsScan(w http.ResponseWriter, r *http.Request) {
	if !s.TMDB().HasAPIKey() {
		writeError(w, http.StatusBadRequest, "Mediarium needs its TMDB key for this.")
		return
	}
	s.background(func() { s.runGapsScan(context.Background()) })
	writeJSON(w, http.StatusOK, map[string]string{"message": "Scanning your library."})
}

// POST /api/home/gaps/ignore {"kind","id","name","undo"}: ignore a series, director or actor (or stop).
func (s *Server) handleGapsIgnore(w http.ResponseWriter, r *http.Request) {
	var in struct {
		gapIgnore
		Undo bool `json:"undo"`
	}
	if err := decodeJSON(r, &in); err != nil || in.ID <= 0 || (in.Kind != "franchise" && in.Kind != "director" && in.Kind != "actor") {
		writeError(w, http.StatusBadRequest, "Choose a film series, director or actor.")
		return
	}
	if in.Undo {
		_, _ = s.db.ExecContext(r.Context(), `DELETE FROM home_gap_ignores WHERE user_id = ? AND kind = ? AND ref_id = ?`, userID(r), in.Kind, in.ID)
	} else {
		_, _ = s.db.ExecContext(r.Context(), `INSERT OR REPLACE INTO home_gap_ignores (user_id, kind, ref_id, name) VALUES (?, ?, ?, ?)`, userID(r), in.Kind, in.ID, strings.TrimSpace(in.Name))
	}
	writeJSON(w, http.StatusOK, map[string]any{"ignored": s.gapIgnores(r.Context(), userID(r))})
}

// GET /api/home/person/{id}: someone's page: who they are, and the films and
// shows they're best known for (directed, wrote, or starred in).
func (s *Server) handlePerson(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.PathValue("id"))
	if id <= 0 || !s.TMDB().HasAPIKey() {
		writeError(w, http.StatusNotFound, "That person couldn't be found.")
		return
	}
	ctx := r.Context()
	tm := s.displayTMDB()
	p, err := tm.GetPerson(ctx, id)
	if err != nil {
		writeUpstreamError(w, "read the person", err)
		return
	}
	movies, shows, err := tm.PersonWork(ctx, id)
	if err != nil {
		writeUpstreamError(w, "read their work", err)
		return
	}
	if len(movies) > 60 {
		movies = movies[:60]
	}
	if len(shows) > 30 {
		shows = shows[:30]
	}
	mp := s.moviePagePayload(ctx, &metadata.MoviePage{Page: 1, TotalPages: 1, Results: movies})
	sp := s.showPagePayload(ctx, &metadata.ShowPage{Page: 1, TotalPages: 1, Results: shows})
	writeJSON(w, http.StatusOK, map[string]any{
		"person":    map[string]any{"id": p.ID, "name": p.Name, "biography": p.Biography, "birthday": p.Birthday, "deathday": p.Deathday, "place": p.Place, "known": p.Known, "image": metadata.ProfileURL(p.ProfilePath)},
		"following": s.isFollowing(ctx, userID(r), id),
		"movies":    mp.Results,
		"shows":     sp.Results,
	})
}
