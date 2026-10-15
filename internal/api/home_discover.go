package api

// Home fork: three more Discover rails.
//
//   - "Because you watched" (list=watched): from what a person actually
//     watched in Jellyfin (or Emby), recent watching counting most, TMDB's
//     recommendations for each, leaving out what they have seen or started.
//     Each Jellyfin user has their own, so people without a Mediarium
//     account (who watch through Jellyfin) get suggestions too.
//   - "Complete the collection" (list=collections): the films missing from
//     film series you have some of, whatever holds them (the media server
//     sees the whole library, streamed titles included).
//   - The season's rail (list=season): Halloween in October, the holidays
//     in December, and so on, from TMDB keywords.

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/metadata"
)

const (
	listWatched     = "watched"
	listCollections = "collections"
	listSeason      = "season"
	listForYou      = "foryou" // the taste engine (home_taste.go)
)

// homeDiscoverList answers the fork's lists of /api/discover/list; false
// for any other list.
func (s *Server) homeDiscoverList(w http.ResponseWriter, r *http.Request, kind, list string, page int) bool {
	switch list {
	case listWatched:
		s.listPage(w, r, kind, page, func(ctx context.Context) ([]similarCandidate, error) {
			return s.watchedSuggestions(ctx, kind, s.discoverPerson(ctx, r))
		})
	case listForYou:
		s.listPage(w, r, kind, page, func(ctx context.Context) ([]similarCandidate, error) {
			list, _ := s.forYou(ctx, s.myTaste(ctx, kind))
			return list, nil
		})
	case listCollections:
		s.listPage(w, r, kind, page, func(ctx context.Context) ([]similarCandidate, error) {
			if kind != "movie" {
				return nil, nil
			}
			return s.collectionGaps(ctx)
		})
	case listSeason:
		s.seasonPage(w, r, kind, page)
	case "anime-airing", "anime-popular", "anime-top", "anime-new":
		if !s.TMDB().HasAPIKey() || kind != "tv" {
			writeJSON(w, http.StatusOK, browsePayload{Page: page, TotalPages: 1, Results: []browseItem{}})
			return true
		}
		res, err := s.displayTMDB().DiscoverAnime(r.Context(), strings.TrimPrefix(list, "anime-"), page)
		if err != nil {
			writeUpstreamError(w, "load the anime", err)
			return true
		}
		writeJSON(w, http.StatusOK, s.showPagePayload(r.Context(), res))
	default:
		return false
	}
	return true
}

// listPage serves one page of a ranked list, 20 to a page.
func (s *Server) listPage(w http.ResponseWriter, r *http.Request, kind string, page int, rank func(context.Context) ([]similarCandidate, error)) {
	empty := browsePayload{Page: page, TotalPages: 1, Results: []browseItem{}}
	if !s.TMDB().HasAPIKey() {
		writeJSON(w, http.StatusOK, empty)
		return
	}
	ranked, err := rank(r.Context())
	if err != nil {
		writeUpstreamError(w, "load the suggestions", err)
		return
	}
	total := len(ranked)
	pages := max(1, (total+similarPageSize-1)/similarPageSize)
	start := (page - 1) * similarPageSize
	if start >= total {
		empty.TotalResults, empty.TotalPages = total, pages
		writeJSON(w, http.StatusOK, empty)
		return
	}
	end := min(start+similarPageSize, total)
	ctx := r.Context()
	if kind == "tv" {
		shows := make([]metadata.Show, 0, end-start)
		for _, c := range ranked[start:end] {
			shows = append(shows, *c.show)
		}
		writeJSON(w, http.StatusOK, s.showPagePayload(ctx, &metadata.ShowPage{Page: page, TotalPages: pages, TotalResults: total, Results: shows}))
		return
	}
	movies := make([]metadata.Movie, 0, end-start)
	for _, c := range ranked[start:end] {
		movies = append(movies, *c.movie)
	}
	writeJSON(w, http.StatusOK, s.moviePagePayload(ctx, &metadata.MoviePage{Page: page, TotalPages: pages, TotalResults: total, Results: movies}))
}

// ---- the media server's side ----

type jfItem struct {
	ID             string            `json:"Id"`
	Name           string            `json:"Name"`
	Type           string            `json:"Type"`
	ProductionYear int               `json:"ProductionYear"`
	SeriesID       string            `json:"SeriesId"`
	SeriesName     string            `json:"SeriesName"`
	ProviderIDs    map[string]string `json:"ProviderIds"`
	UserData       struct {
		LastPlayedDate string `json:"LastPlayedDate"`
	} `json:"UserData"`
}

func (it jfItem) tmdbID() int {
	for k, v := range it.ProviderIDs {
		if strings.EqualFold(k, "Tmdb") {
			id, _ := strconv.Atoi(v)
			return id
		}
	}
	return 0
}

type jfPerson struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	IsAdmin bool   `json:"-"`
}

// jfPeople lists the media server's users.
func (s *Server) jfPeople(ctx context.Context) ([]jfPerson, error) {
	srv, ok := s.jellyfinServer()
	if !ok {
		return nil, nil
	}
	var users []struct {
		ID     string `json:"Id"`
		Name   string `json:"Name"`
		Policy struct {
			IsAdministrator bool `json:"IsAdministrator"`
			IsDisabled      bool `json:"IsDisabled"`
			IsHidden        bool `json:"IsHidden"`
		} `json:"Policy"`
	}
	if err := jfJSON(ctx, srv, http.MethodGet, "/Users", nil, &users); err != nil {
		return nil, err
	}
	var out []jfPerson
	for _, u := range users {
		if !u.Policy.IsDisabled {
			out = append(out, jfPerson{ID: u.ID, Name: u.Name, IsAdmin: u.Policy.IsAdministrator})
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

// personFor is the media server user that matches a Mediarium account: same
// name (user name, first name, or the part of the e-mail address before the
// @), else the server's first administrator.
func personFor(people []jfPerson, u *auth.User) string {
	if u == nil || len(people) == 0 {
		return ""
	}
	if id := personMatch(people, u); id != "" {
		return id
	}
	for _, p := range people {
		if p.IsAdmin {
			return p.ID
		}
	}
	return people[0].ID
}

// discoverPerson is whose watching "Because you watched" is built from:
// ?who= for administrators (anyone in the household), else the account's own.
func (s *Server) discoverPerson(ctx context.Context, r *http.Request) string {
	people, err := s.jfPeople(ctx)
	if err != nil || len(people) == 0 {
		return ""
	}
	u := auth.UserFromContext(r.Context())
	if who := r.URL.Query().Get("who"); who != "" && u != nil && u.IsAdmin {
		for _, p := range people {
			if p.ID == who {
				return who
			}
		}
	}
	return personFor(people, u)
}

// watching is what a person has watched, as the rails need it.
type watching struct {
	recentMovies []int          // movies finished, most recent first
	recentShows  []int          // shows with an episode watched, most recent first
	names        map[int]string // a title's name, by TMDB id
	seen         map[int]bool   // movies finished and shows started
	at           time.Time
}

var (
	watchMu    sync.Mutex
	watchCache = map[string]*watching{}
)

// watchingOf reads a person's watch history from the media server, kept for
// half an hour.
func (s *Server) watchingOf(ctx context.Context, person string) (*watching, error) {
	watchMu.Lock()
	if wa, ok := watchCache[person]; ok && time.Since(wa.at) < 30*time.Minute {
		watchMu.Unlock()
		return wa, nil
	}
	watchMu.Unlock()
	srv, ok := s.jellyfinServer()
	if !ok || person == "" {
		return &watching{names: map[int]string{}, seen: map[int]bool{}}, nil
	}
	wa := &watching{names: map[int]string{}, seen: map[int]bool{}, at: time.Now()}
	base := "/Users/" + url.PathEscape(person) + "/Items?Recursive=true&Filters=IsPlayed&SortBy=DatePlayed&SortOrder=Descending&EnableImages=false&Fields=ProviderIds"
	var movies struct {
		Items []jfItem `json:"Items"`
	}
	if err := jfJSON(ctx, srv, http.MethodGet, base+"&IncludeItemTypes=Movie&Limit=2000", nil, &movies); err != nil {
		return nil, err
	}
	for _, it := range movies.Items {
		if id := it.tmdbID(); id > 0 && !wa.seen[id] {
			wa.seen[id] = true
			wa.recentMovies = append(wa.recentMovies, id)
			wa.names[id] = it.Name
		}
	}
	var eps struct {
		Items []jfItem `json:"Items"`
	}
	if err := jfJSON(ctx, srv, http.MethodGet, base+"&IncludeItemTypes=Episode&Limit=3000", nil, &eps); err != nil {
		return nil, err
	}
	// The shows, in the order their latest episode was watched.
	var seriesOrder []string
	seriesName := map[string]string{}
	for _, it := range eps.Items {
		if it.SeriesID != "" && seriesName[it.SeriesID] == "" {
			seriesOrder = append(seriesOrder, it.SeriesID)
			seriesName[it.SeriesID] = it.SeriesName
		}
	}
	// Their TMDB ids, a batch at a time.
	tmdbOf := map[string]int{}
	for i := 0; i < len(seriesOrder); i += 100 {
		batch := seriesOrder[i:min(i+100, len(seriesOrder))]
		var shows struct {
			Items []jfItem `json:"Items"`
		}
		if err := jfJSON(ctx, srv, http.MethodGet, "/Users/"+url.PathEscape(person)+"/Items?EnableImages=false&Fields=ProviderIds&Ids="+strings.Join(batch, ","), nil, &shows); err != nil {
			return nil, err
		}
		for _, it := range shows.Items {
			tmdbOf[it.ID] = it.tmdbID()
		}
	}
	for _, sid := range seriesOrder {
		if id := tmdbOf[sid]; id > 0 && !wa.seen[-id] {
			wa.seen[-id] = true // shows are kept negative, apart from movies
			wa.recentShows = append(wa.recentShows, id)
			wa.names[-id] = seriesName[sid]
		}
	}
	watchMu.Lock()
	watchCache[person] = wa
	watchMu.Unlock()
	return wa, nil
}

const (
	watchedSeeds  = 12   // recent titles asked about
	watchedDecay  = 0.18 // each older one counts a little less
	watchedWeight = 3.0  // how much "you watched something like it" outweighs the rest
)

// watchedSuggestions ranks what TMDB relates to a person's recent watching.
func (s *Server) watchedSuggestions(ctx context.Context, kind, person string) ([]similarCandidate, error) {
	wa, err := s.watchingOf(ctx, person)
	if err != nil {
		return nil, err
	}
	recent, sign := wa.recentMovies, 1
	if kind == "tv" {
		recent, sign = wa.recentShows, -1
	}
	if len(recent) == 0 {
		return nil, nil
	}
	seeds := recent[:min(watchedSeeds, len(recent))]
	perSeed, err := s.suggestions(ctx, kind, seeds)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	weight := map[int]float64{}
	because := map[int][]string{}
	byID := map[int]similarCandidate{}
	var order []int
	for i, list := range perSeed {
		w := 1 / (1 + watchedDecay*float64(i))
		counted := map[int]bool{}
		for _, c := range list {
			if c.ID <= 0 || counted[c.ID] || wa.seen[sign*c.ID] {
				continue
			}
			counted[c.ID] = true
			if _, ok := byID[c.ID]; !ok {
				c.Votes = 0
				byID[c.ID] = c
				order = append(order, c.ID)
			}
			cc := byID[c.ID]
			cc.Votes++
			byID[c.ID] = cc
			weight[c.ID] += w
			if len(because[c.ID]) < 2 {
				because[c.ID] = append(because[c.ID], wa.names[sign*seeds[i]])
			}
		}
	}
	rememberWhy(person, kind, because)
	out := make([]similarCandidate, 0, len(order))
	for _, id := range order {
		if c := byID[id]; c.Year > 0 {
			out = append(out, c)
		}
	}
	score := func(c similarCandidate) float64 { return watchedWeight*weight[c.ID] + 0.4*similarScore(c, now) }
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.HasPoster != b.HasPoster {
			return a.HasPoster
		}
		return score(a) > score(b)
	})
	return out[:min(len(out), similarMaxResults)], nil
}

// GET /api/home/discover/people: the household (the media server's users),
// and whose watching the rail shows by default. Only administrators may
// look at someone else's.
func (s *Server) handleDiscoverPeople(w http.ResponseWriter, r *http.Request) {
	people, err := s.jfPeople(r.Context())
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"people": []jfPerson{}, "me": ""})
		return
	}
	u := auth.UserFromContext(r.Context())
	me := personFor(people, u)
	// Pick for the Room lists everyone: who's on the sofa, not their watching.
	if (u == nil || !u.IsAdmin) && r.URL.Query().Get("room") != "1" {
		var mine []jfPerson
		for _, p := range people {
			if p.ID == me {
				mine = append(mine, p)
			}
		}
		people = mine
	}
	if people == nil {
		people = []jfPerson{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"people": people, "me": me})
}

// GET /api/home/discover/watched-seeds?kind=&who=: the titles the rail is
// built from, most recent first ("Because you watched The Bear, Severance…").
func (s *Server) handleWatchedSeeds(w http.ResponseWriter, r *http.Request) {
	kind, ok := discoverKind(r)
	if !ok {
		writeError(w, http.StatusBadRequest, `kind must be "movie" or "tv"`)
		return
	}
	wa, err := s.watchingOf(r.Context(), s.discoverPerson(r.Context(), r))
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"titles": []string{}})
		return
	}
	recent, sign := wa.recentMovies, 1
	if kind == "tv" {
		recent, sign = wa.recentShows, -1
	}
	titles := []string{}
	for _, id := range recent[:min(4, len(recent))] {
		titles = append(titles, wa.names[sign*id])
	}
	writeJSON(w, http.StatusOK, map[string]any{"titles": titles})
}

// ---- complete the collection ----

var (
	ownedMu   sync.Mutex
	ownedAt   time.Time
	ownedMovs map[int]bool
)

// ownedMovies is every film the household has: the media server's movies
// (the whole library, streamed titles included), Mediarium's library and
// its streamed titles. Kept for an hour.
func (s *Server) ownedMovies(ctx context.Context) map[int]bool {
	ownedMu.Lock()
	defer ownedMu.Unlock()
	if ownedMovs != nil && time.Since(ownedAt) < time.Hour {
		return ownedMovs
	}
	have := map[int]bool{}
	if srv, ok := s.jellyfinServer(); ok {
		people, _ := s.jfPeople(ctx)
		// Read through an administrator, who sees every library.
		viewer := ""
		for _, p := range people {
			if p.IsAdmin && viewer == "" {
				viewer = p.ID
			}
		}
		if viewer == "" && len(people) > 0 {
			viewer = people[0].ID
		}
		if viewer != "" {
			var res struct {
				Items []jfItem `json:"Items"`
			}
			p := "/Users/" + url.PathEscape(viewer) + "/Items?Recursive=true&IncludeItemTypes=Movie&EnableImages=false&Fields=ProviderIds"
			if err := jfJSON(ctx, srv, http.MethodGet, p, nil, &res); err == nil {
				for _, it := range res.Items {
					if id := it.tmdbID(); id > 0 {
						have[id] = true
					}
				}
			}
		}
	}
	if list, err := s.MovieRepo.List(); err == nil {
		for _, m := range list {
			have[m.TMDBID] = true
		}
	}
	if rows, err := s.db.QueryContext(ctx, `SELECT tmdb_id FROM streamed_titles WHERE kind = 'movie'`); err == nil {
		for rows.Next() {
			var id int
			if rows.Scan(&id) == nil {
				have[id] = true
			}
		}
		rows.Close()
	}
	ownedMovs, ownedAt = have, time.Now()
	return have
}

var collectionFillRunning sync.Mutex

// fillMovieCollections looks up, for each film the household has that hasn't
// been looked up yet, which film series it belongs to (TMDB), at a gentle
// pace, keeping the answers.
func (s *Server) fillMovieCollections(ctx context.Context) {
	if !collectionFillRunning.TryLock() {
		return
	}
	defer collectionFillRunning.Unlock()
	known := map[int]bool{}
	if rows, err := s.db.QueryContext(ctx, `SELECT tmdb_id FROM home_movie_collections`); err == nil {
		for rows.Next() {
			var id int
			if rows.Scan(&id) == nil {
				known[id] = true
			}
		}
		rows.Close()
	}
	n := 0
	for id := range s.ownedMovies(ctx) {
		if known[id] || ctx.Err() != nil {
			continue
		}
		m, err := s.TMDB().GetMovie(ctx, id)
		if err != nil {
			continue
		}
		col := 0
		if m.BelongsToCollection != nil {
			col = m.BelongsToCollection.ID
		}
		_, _ = s.db.ExecContext(ctx, `INSERT OR REPLACE INTO home_movie_collections (tmdb_id, collection_id) VALUES (?, ?)`, id, col)
		n++
		time.Sleep(120 * time.Millisecond)
	}
	if n > 0 {
		slog.Info("discover: looked up film series", "films", n)
	}
}

// collectionGaps lists the released films missing from series the household
// has part of: the nearly complete series first.
var (
	gapsMu sync.Mutex
	gapsAt time.Time
	gapsOf []similarCandidate
)

// collectionGaps is kept for an hour: working it out reads every film series
// you have part of from TMDB.
func (s *Server) collectionGaps(ctx context.Context) ([]similarCandidate, error) {
	gapsMu.Lock()
	defer gapsMu.Unlock()
	if gapsOf != nil && time.Since(gapsAt) < time.Hour {
		return gapsOf, nil
	}
	out, err := s.workOutCollectionGaps(ctx)
	if err == nil && len(out) > 0 {
		gapsOf, gapsAt = out, time.Now()
	}
	return out, err
}

func (s *Server) workOutCollectionGaps(ctx context.Context) ([]similarCandidate, error) {
	s.background(func() { s.fillMovieCollections(context.Background()) })
	have := s.ownedMovies(ctx)
	owned := map[int]int{} // collection -> films had
	rows, err := s.db.QueryContext(ctx, `SELECT tmdb_id, collection_id FROM home_movie_collections WHERE collection_id > 0`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, col int
		if rows.Scan(&id, &col) == nil && have[id] {
			owned[col]++
		}
	}
	rows.Close()
	type gap struct {
		col     *metadata.Collection
		have    int
		missing []metadata.CollectionPart
	}
	var gaps []gap
	today := time.Now().Format("2006-01-02")
	// The series from TMDB, eight at a time.
	ids := make([]int, 0, len(owned))
	for colID := range owned {
		ids = append(ids, colID)
	}
	cols := make([]*metadata.Collection, len(ids))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, colID := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if c, err := s.displayTMDB().GetCollection(ctx, colID); err == nil {
				cols[i] = c
			}
		}()
	}
	wg.Wait()
	for i, colID := range ids {
		col := cols[i]
		if col == nil {
			continue
		}
		n := owned[colID]
		g := gap{col: col, have: n}
		for _, p := range col.Parts {
			if !have[p.TMDBID] && p.ReleaseDate != "" && p.ReleaseDate <= today {
				g.missing = append(g.missing, p)
			}
		}
		if len(g.missing) > 0 {
			gaps = append(gaps, g)
		}
	}
	// Nearly complete series first, then the ones you have most of.
	sort.SliceStable(gaps, func(i, j int) bool {
		a, b := gaps[i], gaps[j]
		if len(a.missing) != len(b.missing) {
			return len(a.missing) < len(b.missing)
		}
		if a.have != b.have {
			return a.have > b.have
		}
		return a.col.Name < b.col.Name
	})
	var out []similarCandidate
	for _, g := range gaps {
		for _, p := range g.missing {
			m := metadata.Movie{TMDBID: p.TMDBID, Title: p.DisplayTitle(), Overview: p.Overview, ReleaseDate: p.ReleaseDate, PosterPath: p.PosterPath, VoteAverage: p.VoteAverage}
			out = append(out, movieCandidate(m))
			if len(out) >= similarMaxResults {
				return out, nil
			}
		}
	}
	return out, nil
}

// ---- the season's rail ----

// discoverSeason is a time of year with a rail of its own.
type discoverSeason struct {
	Key      string   `json:"key"`
	keywords []string // TMDB keywords, any of them
	genres   []int    // movie genres, any of them (optional)
}

func seasonOn(d time.Time) *discoverSeason {
	m, day := d.Month(), d.Day()
	switch {
	case m == time.October:
		// Movies only in the genres Halloween is watched in (horror, family,
		// animation, mystery): "ghost" alone also finds blockbusters.
		return &discoverSeason{Key: "halloween", keywords: []string{"halloween", "haunted house", "trick or treating", "witch", "ghost", "vampire", "zombie", "werewolf"}, genres: []int{27, 10751, 16, 9648}}
	case m == time.November && day >= 15, m == time.December && day <= 26:
		return &discoverSeason{Key: "holidays", keywords: []string{"christmas", "santa claus", "holiday", "christmas party"}}
	case m == time.February && day <= 14:
		return &discoverSeason{Key: "valentine", keywords: []string{"valentine's day", "romantic comedy", "love"}}
	case m >= time.June && m <= time.August:
		return &discoverSeason{Key: "summer", keywords: []string{"summer", "summer vacation", "beach", "road trip"}}
	}
	return nil
}

// GET /api/home/discover/season: the season's rail now ({"key": ""} when none).
func (s *Server) handleDiscoverSeason(w http.ResponseWriter, r *http.Request) {
	if se := seasonOn(time.Now()); se != nil {
		writeJSON(w, http.StatusOK, se)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"key": ""})
}

func (s *Server) seasonPage(w http.ResponseWriter, r *http.Request, kind string, page int) {
	empty := browsePayload{Page: page, TotalPages: 1, Results: []browseItem{}}
	se := seasonOn(time.Now())
	if se == nil || !s.TMDB().HasAPIKey() {
		writeJSON(w, http.StatusOK, empty)
		return
	}
	ctx := r.Context()
	var ids []int
	for _, k := range se.keywords {
		if id, err := s.TMDB().KeywordID(ctx, k); err == nil && id > 0 {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		writeJSON(w, http.StatusOK, empty)
		return
	}
	q := metadata.KeywordQuery{Keywords: ids, Genres: se.genres, MinVotes: 150, Page: page}
	if kind == "tv" {
		q.Genres, q.MinVotes = nil, 50
		p, err := s.displayTMDB().DiscoverTVByKeyword(ctx, q)
		if err != nil {
			writeUpstreamError(w, "load the season's shows", err)
			return
		}
		writeJSON(w, http.StatusOK, s.showPagePayload(ctx, p))
		return
	}
	p, err := s.displayTMDB().DiscoverMoviesByKeyword(ctx, q)
	if err != nil {
		writeUpstreamError(w, "load the season's movies", err)
		return
	}
	writeJSON(w, http.StatusOK, s.moviePagePayload(ctx, p))
}
