package api

// Home fork: Discover recipes, after Reciperr. A recipe is a saved set of
// filters (genres, years, a rating with enough votes behind it, runtime,
// language, people, age rating, keywords, the services you pay for) with a
// name; each one is its own Discover row that keeps itself up to date. Each
// person keeps their own, and can share one with the household. Also here:
// the streaming services each person pays for ("On my services").

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/metadata"
)

const (
	keyWatchRegion   = "home.watch_region"
	watchRegionDflt  = "US"
	recipeNameMax    = 60
	recipeFiltersMax = 4000
)

// RecipeFilters is what a recipe asks for. Everything is optional.
type RecipeFilters struct {
	Genres        []int         `json:"genres,omitempty"`
	AllGenres     bool          `json:"allGenres,omitempty"` // every genre, not any of them
	ExcludeGenres []int         `json:"excludeGenres,omitempty"`
	YearFrom      int           `json:"yearFrom,omitempty"`
	YearTo        int           `json:"yearTo,omitempty"`
	LastMonths    int           `json:"lastMonths,omitempty"` // released in the last N months (instead of years)
	MinRating     float64       `json:"minRating,omitempty"`  // TMDB, out of 10
	MinVotes      int           `json:"minVotes,omitempty"`
	MaxVotes      int           `json:"maxVotes,omitempty"` // little known: at most this many votes
	RuntimeMin    int           `json:"runtimeMin,omitempty"`
	RuntimeMax    int           `json:"runtimeMax,omitempty"`
	Language      string        `json:"language,omitempty"` // original language, "ko"; "!en" is anything but English
	People        []recipeNamed `json:"people,omitempty"`   // films only: any of them in the cast or crew
	Certs         []string      `json:"certs,omitempty"`    // films only: US age ratings, any of them
	Keywords      []recipeNamed `json:"keywords,omitempty"`
	MyServices    bool          `json:"myServices,omitempty"` // only what's on the services you pay for
	Providers     []int         `json:"providers,omitempty"`  // or on these services
	Anime         bool          `json:"anime,omitempty"`      // Japanese animation (the Anime tab)
	Sort          string        `json:"sort,omitempty"`       // popular (default), rating, newest, votes, revenue (films)
	// Scores from MDBList (home_scores.go); titles without them are left out.
	MinIMDb       float64 `json:"minImdb,omitempty"`
	MinTomatoes   int     `json:"minTomatoes,omitempty"`
	MinMetacritic int     `json:"minMetacritic,omitempty"`
	MaxAge        int     `json:"maxAge,omitempty"` // Common Sense Media's age, at most
}

func (f RecipeFilters) wantsScores() bool {
	return f.MinIMDb > 0 || f.MinTomatoes > 0 || f.MinMetacritic > 0 || f.MaxAge > 0
}

// passes says whether a title's scores meet the recipe.
func (f RecipeFilters) passes(sc titleScores, ok bool) bool {
	if !ok {
		return false
	}
	if f.MinIMDb > 0 && sc.IMDb < f.MinIMDb {
		return false
	}
	if f.MinTomatoes > 0 && sc.Tomatoes < f.MinTomatoes {
		return false
	}
	if f.MinMetacritic > 0 && sc.Metacritic < f.MinMetacritic {
		return false
	}
	if f.MaxAge > 0 && (sc.CommonSense == 0 || sc.CommonSense > f.MaxAge) {
		return false
	}
	return true
}

// keepScored leaves out the titles whose scores don't meet the recipe.
func (s *Server) keepScored(ctx context.Context, kind string, f RecipeFilters, p browsePayload) browsePayload {
	if !f.wantsScores() || s.mdblistKey() == "" {
		return p
	}
	ids := make([]int, len(p.Results))
	for i, r := range p.Results {
		ids[i] = r.TMDBID
	}
	scores := s.scoresFor(ctx, kind, ids)
	kept := p.Results[:0:0]
	for _, r := range p.Results {
		sc, ok := scores[r.TMDBID]
		if f.passes(sc, ok) {
			kept = append(kept, r)
		}
	}
	p.Results = kept
	return p
}

type recipeNamed struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// notEnglish as a recipe's language: anything but English. TMDB can't leave a
// language out, so it's asked for the main film languages other than English
// (any of them, joined by "|"); English titles are also left out of each page.
const (
	notEnglish     = "!en"
	worldLanguages = "ko|ja|fr|es|it|de|hi|zh|cn|sv|da|no|pt|tr|th|fa|pl|nl|ru|ta|te|ml|ar|he|id|fi|is"
)

func keepIf[T any](list []T, ok func(T) bool) []T {
	out := list[:0:0]
	for _, x := range list {
		if ok(x) {
			out = append(out, x)
		}
	}
	return out
}

func joinInts(ids []int, sep string) string {
	s := make([]string, len(ids))
	for i, v := range ids {
		s[i] = strconv.Itoa(v)
	}
	return strings.Join(s, sep)
}

// tmdbParams turns a recipe into TMDB discover parameters. providers are the
// services to keep to (the person's own when MyServices is on).
func (f RecipeFilters) tmdbParams(kind string, providers []int, region string, page int, now time.Time) url.Values {
	tv := kind != "movie"
	v := url.Values{"include_adult": {"false"}, "page": {strconv.Itoa(max(1, min(page, 500)))}}
	dateField := "primary_release_date"
	if tv {
		dateField = "first_air_date"
	}
	genres := f.Genres
	if f.Anime && !containsInt(genres, 16) {
		genres = append([]int{16}, genres...)
	}
	if len(genres) > 0 {
		sep := "|"
		if f.AllGenres || f.Anime {
			sep = ","
		}
		v.Set("with_genres", joinInts(genres, sep))
	}
	if len(f.ExcludeGenres) > 0 {
		v.Set("without_genres", joinInts(f.ExcludeGenres, ","))
	}
	if f.Anime {
		v.Set("with_original_language", "ja")
	} else if f.Language == notEnglish {
		v.Set("with_original_language", worldLanguages)
	} else if f.Language != "" {
		v.Set("with_original_language", f.Language)
	}
	switch {
	case f.LastMonths > 0:
		v.Set(dateField+".gte", now.AddDate(0, -f.LastMonths, 0).Format("2006-01-02"))
		v.Set(dateField+".lte", now.Format("2006-01-02"))
	default:
		if f.YearFrom > 0 {
			v.Set(dateField+".gte", strconv.Itoa(f.YearFrom)+"-01-01")
		}
		if f.YearTo > 0 {
			v.Set(dateField+".lte", strconv.Itoa(f.YearTo)+"-12-31")
		}
	}
	if f.MinRating > 0 {
		v.Set("vote_average.gte", strconv.FormatFloat(f.MinRating, 'f', 1, 64))
	}
	minVotes := f.MinVotes
	if f.MinRating > 0 && minVotes == 0 {
		minVotes = 100 // a high score from a handful of votes says little
	}
	if minVotes > 0 {
		v.Set("vote_count.gte", strconv.Itoa(minVotes))
	}
	if f.MaxVotes > 0 {
		v.Set("vote_count.lte", strconv.Itoa(f.MaxVotes))
	}
	if f.RuntimeMin > 0 {
		v.Set("with_runtime.gte", strconv.Itoa(f.RuntimeMin))
	}
	if f.RuntimeMax > 0 {
		v.Set("with_runtime.lte", strconv.Itoa(f.RuntimeMax))
	}
	if !tv && len(f.People) > 0 {
		ids := make([]int, len(f.People))
		for i, p := range f.People {
			ids[i] = p.ID
		}
		v.Set("with_people", joinInts(ids, "|"))
	}
	if !tv && len(f.Certs) > 0 {
		v.Set("certification_country", "US")
		v.Set("certification", strings.Join(f.Certs, "|"))
	}
	if len(f.Keywords) > 0 {
		ids := make([]int, len(f.Keywords))
		for i, k := range f.Keywords {
			ids[i] = k.ID
		}
		v.Set("with_keywords", joinInts(ids, "|"))
	}
	if len(providers) > 0 {
		v.Set("with_watch_providers", joinInts(providers, "|"))
		v.Set("watch_region", region)
		v.Set("with_watch_monetization_types", "flatrate|free|ads")
	}
	switch f.Sort {
	case "rating":
		v.Set("sort_by", "vote_average.desc")
		if minVotes == 0 {
			v.Set("vote_count.gte", "200")
		}
	case "newest":
		v.Set("sort_by", dateField+".desc")
		if v.Get(dateField+".lte") == "" {
			v.Set(dateField+".lte", now.Format("2006-01-02")) // out already, not announced
		}
	case "votes":
		v.Set("sort_by", "vote_count.desc")
	case "revenue":
		if tv { // shows have no box office
			v.Set("sort_by", "vote_count.desc")
		} else {
			v.Set("sort_by", "revenue.desc")
		}
	default:
		v.Set("sort_by", "popularity.desc")
	}
	return v
}

func (s *Server) watchRegion() string {
	if v, _ := s.Settings.Get(keyWatchRegion); len(v) == 2 {
		return strings.ToUpper(v)
	}
	return watchRegionDflt
}

func (s *Server) myServices(ctx context.Context, userID int64) []int {
	var raw string
	_ = s.db.QueryRowContext(ctx, `SELECT providers FROM home_user_services WHERE user_id = ?`, userID).Scan(&raw)
	var ids []int
	_ = json.Unmarshal([]byte(raw), &ids)
	return ids
}

func userID(r *http.Request) int64 {
	if u := auth.UserFromContext(r.Context()); u != nil {
		return u.ID
	}
	return 0
}

// GET /api/home/recipes/titles?kind=movie|tv|anime&filters={json}&page=N:
// one page of what a recipe (saved or not) finds.
func (s *Server) handleRecipeTitles(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	var f RecipeFilters
	if raw := r.URL.Query().Get("filters"); raw != "" {
		if len(raw) > recipeFiltersMax || json.Unmarshal([]byte(raw), &f) != nil {
			writeError(w, http.StatusBadRequest, "These filters couldn't be read.")
			return
		}
	}
	if kind == "anime" {
		f.Anime = true
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if !s.TMDB().HasAPIKey() {
		writeJSON(w, http.StatusOK, browsePayload{Page: 1, Results: []browseItem{}})
		return
	}
	providers := f.Providers
	if f.MyServices {
		providers = s.myServices(r.Context(), userID(r))
		if len(providers) == 0 {
			writeJSON(w, http.StatusOK, map[string]any{"page": 1, "totalPages": 0, "results": []browseItem{}, "note": "noServices"})
			return
		}
	}
	v := f.tmdbParams(kind, providers, s.watchRegion(), page, time.Now())
	ctx := r.Context()
	if kind == "movie" {
		p, err := s.displayTMDB().DiscoverMoviesWith(ctx, v)
		if err != nil {
			writeUpstreamError(w, "find the titles", err)
			return
		}
		if f.Language == notEnglish {
			p.Results = keepIf(p.Results, func(m metadata.Movie) bool { return m.OriginalLanguage != "en" })
		}
		writeJSON(w, http.StatusOK, s.keepScored(ctx, "movie", f, s.moviePagePayload(ctx, p)))
		return
	}
	p, err := s.displayTMDB().DiscoverTVWith(ctx, v)
	if err != nil {
		writeUpstreamError(w, "find the titles", err)
		return
	}
	if f.Language == notEnglish {
		p.Results = keepIf(p.Results, func(sh metadata.Show) bool { return sh.OriginalLanguage != "en" })
	}
	writeJSON(w, http.StatusOK, s.keepScored(ctx, "tv", f, s.showPagePayload(ctx, p)))
}

type recipePayload struct {
	ID      int64           `json:"id"`
	Kind    string          `json:"kind"`
	Name    string          `json:"name"`
	Filters json.RawMessage `json:"filters"`
	Shared  bool            `json:"shared"`
	Mine    bool            `json:"mine"`
	Owner   string          `json:"owner,omitempty"` // someone else's shared recipe
}

// GET /api/home/recipes?kind=…: yours, then the ones others share.
func (s *Server) handleListRecipes(w http.ResponseWriter, r *http.Request) {
	me := userID(r)
	q := `SELECT r.id, r.kind, r.name, r.filters, r.shared, r.user_id, COALESCE(NULLIF(u.first_name, ''), u.username, '')
		FROM home_recipes r LEFT JOIN users u ON u.id = r.user_id
		WHERE (r.user_id = ? OR r.shared = 1)`
	args := []any{me}
	if k := r.URL.Query().Get("kind"); k != "" {
		q += ` AND r.kind = ?`
		args = append(args, k)
	}
	q += ` ORDER BY r.user_id != ?, r.position, r.id`
	args = append(args, me)
	rows, err := s.db.QueryContext(r.Context(), q, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	out := []recipePayload{}
	for rows.Next() {
		var p recipePayload
		var filters, owner string
		var uid int64
		if rows.Scan(&p.ID, &p.Kind, &p.Name, &filters, &p.Shared, &uid, &owner) != nil {
			continue
		}
		p.Filters = json.RawMessage(filters)
		p.Mine = uid == me
		if !p.Mine {
			p.Owner = owner
		}
		out = append(out, p)
	}
	writeJSON(w, http.StatusOK, out)
}

type recipeInput struct {
	Kind    string        `json:"kind"`
	Name    string        `json:"name"`
	Filters RecipeFilters `json:"filters"`
	Shared  bool          `json:"shared"`
}

func (in *recipeInput) check() string {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return "Give the recipe a name."
	}
	if len([]rune(in.Name)) > recipeNameMax {
		return "That name is too long."
	}
	if in.Kind != "movie" && in.Kind != "tv" && in.Kind != "anime" {
		return "Choose films, shows or anime."
	}
	return ""
}

// POST /api/home/recipes
func (s *Server) handleAddRecipe(w http.ResponseWriter, r *http.Request) {
	var in recipeInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "That recipe couldn't be read.")
		return
	}
	if msg := in.check(); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	b, _ := json.Marshal(in.Filters)
	me := userID(r)
	res, err := s.db.ExecContext(r.Context(), `INSERT INTO home_recipes (user_id, kind, name, filters, shared, position)
		VALUES (?, ?, ?, ?, ?, (SELECT COALESCE(MAX(position), 0) + 1 FROM home_recipes WHERE user_id = ?))`, me, in.Kind, in.Name, string(b), in.Shared, me)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	id, _ := res.LastInsertId()
	writeJSON(w, http.StatusOK, recipePayload{ID: id, Kind: in.Kind, Name: in.Name, Filters: b, Shared: in.Shared, Mine: true})
}

// recipeOwner returns who made a recipe, or sql.ErrNoRows.
func (s *Server) recipeOwner(ctx context.Context, id int64) (int64, error) {
	var uid int64
	err := s.db.QueryRowContext(ctx, `SELECT user_id FROM home_recipes WHERE id = ?`, id).Scan(&uid)
	return uid, err
}

// PUT /api/home/recipes/{id}: only its maker changes it.
func (s *Server) handleUpdateRecipe(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	owner, err := s.recipeOwner(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && owner != userID(r)) {
		writeError(w, http.StatusNotFound, "That recipe isn't yours to change.")
		return
	}
	var in recipeInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "That recipe couldn't be read.")
		return
	}
	if msg := in.check(); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	b, _ := json.Marshal(in.Filters)
	if _, err := s.db.ExecContext(r.Context(), `UPDATE home_recipes SET kind = ?, name = ?, filters = ?, shared = ? WHERE id = ?`, in.Kind, in.Name, string(b), in.Shared, id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, recipePayload{ID: id, Kind: in.Kind, Name: in.Name, Filters: b, Shared: in.Shared, Mine: true})
}

// DELETE /api/home/recipes/{id}: its maker, or an administrator.
func (s *Server) handleDeleteRecipe(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	owner, err := s.recipeOwner(r.Context(), id)
	u := auth.UserFromContext(r.Context())
	if errors.Is(err, sql.ErrNoRows) || (err == nil && owner != userID(r) && (u == nil || !u.IsAdmin)) {
		writeError(w, http.StatusNotFound, "That recipe isn't yours to remove.")
		return
	}
	_, _ = s.db.ExecContext(r.Context(), `DELETE FROM home_recipes WHERE id = ?`, id)
	writeJSON(w, http.StatusOK, map[string]string{"message": "Recipe removed."})
}

// POST /api/home/recipes/{id}/copy: a copy of someone's shared recipe, yours to change.
func (s *Server) handleCopyRecipe(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	me := userID(r)
	res, err := s.db.ExecContext(r.Context(), `INSERT INTO home_recipes (user_id, kind, name, filters, shared, position)
		SELECT ?, kind, name, filters, 0, (SELECT COALESCE(MAX(position), 0) + 1 FROM home_recipes WHERE user_id = ?) FROM home_recipes WHERE id = ? AND (shared = 1 OR user_id = ?)`, me, me, id, me)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "That recipe isn't shared.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Copied to your recipes."})
}

// ---- streaming services ----

// GET /api/home/providers: the services of the country, with the ones you pay for.
func (s *Server) handleProviders(w http.ResponseWriter, r *http.Request) {
	region := s.watchRegion()
	list, err := s.TMDB().Providers(r.Context(), region)
	if err != nil {
		writeUpstreamError(w, "list the streaming services", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"region": region, "providers": list, "mine": s.myServices(r.Context(), userID(r)), "scoresOn": s.mdblistKey() != ""})
}

// PUT /api/home/my-services {"providers": [ids]}
func (s *Server) handlePutMyServices(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Providers []int `json:"providers"`
	}
	if err := decodeJSON(r, &in); err != nil || len(in.Providers) > 60 {
		writeError(w, http.StatusBadRequest, "Choose your services.")
		return
	}
	if in.Providers == nil {
		in.Providers = []int{}
	}
	b, _ := json.Marshal(in.Providers)
	if _, err := s.db.ExecContext(r.Context(), `INSERT OR REPLACE INTO home_user_services (user_id, providers) VALUES (?, ?)`, userID(r), string(b)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"mine": in.Providers})
}

// GET /api/home/watch/{kind}/{id}: where a title streams, marking yours.
func (s *Server) handleTitleProviders(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	id, _ := strconv.Atoi(r.PathValue("id"))
	if (kind != "movie" && kind != "tv") || id <= 0 {
		writeError(w, http.StatusBadRequest, "Choose a film or show.")
		return
	}
	list, link, err := s.TMDB().TitleProviders(r.Context(), kind, id, s.watchRegion())
	if err != nil {
		writeUpstreamError(w, "find where it streams", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": list, "link": link, "mine": s.myServices(r.Context(), userID(r))})
}

// ---- filter helpers ----

// GET /api/home/search/people?q=
func (s *Server) handleSearchPeople(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) < 2 {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	list, err := s.TMDB().SearchPeople(r.Context(), q)
	if err != nil {
		writeUpstreamError(w, "find people", err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// GET /api/home/search/keywords?q=
func (s *Server) handleSearchKeywords(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) < 2 {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	list, err := s.TMDB().SearchKeywords(r.Context(), q)
	if err != nil {
		writeUpstreamError(w, "find keywords", err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// Ready-made Discover rows (top rated, box office, hidden gems, world cinema)
// are built-in recipes, defined in the web app. Each person can hide them.

func validRowKey(k string) bool {
	kind, name, ok := strings.Cut(k, ":")
	if !ok || (kind != "movie" && kind != "tv" && kind != "anime") || name == "" || len(name) > 20 {
		return false
	}
	for _, c := range name {
		if (c < 'a' || c > 'z') && c != '-' {
			return false
		}
	}
	return true
}

func (s *Server) hiddenRows(ctx context.Context, uid int64) []string {
	out := []string{}
	rows, err := s.db.QueryContext(ctx, `SELECT row_key FROM home_hidden_rows WHERE user_id = ? ORDER BY row_key`, uid)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		if rows.Scan(&k) == nil {
			out = append(out, k)
		}
	}
	return out
}

// GET /api/home/ready-rows: the ready-made rows you've hidden.
func (s *Server) handleReadyRows(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"hidden": s.hiddenRows(r.Context(), userID(r))})
}

// PUT /api/home/ready-rows {"key":"movie:top","hidden":true}, or {"showAll":true}.
func (s *Server) handlePutReadyRows(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Key     string `json:"key"`
		Hidden  bool   `json:"hidden"`
		ShowAll bool   `json:"showAll"`
	}
	if err := decodeJSON(r, &in); err != nil || (!in.ShowAll && !validRowKey(in.Key)) {
		writeError(w, http.StatusBadRequest, "Choose a row.")
		return
	}
	ctx, me := r.Context(), userID(r)
	switch {
	case in.ShowAll:
		_, _ = s.db.ExecContext(ctx, `DELETE FROM home_hidden_rows WHERE user_id = ?`, me)
	case in.Hidden:
		_, _ = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO home_hidden_rows (user_id, row_key) VALUES (?, ?)`, me, in.Key)
	default:
		_, _ = s.db.ExecContext(ctx, `DELETE FROM home_hidden_rows WHERE user_id = ? AND row_key = ?`, me, in.Key)
	}
	writeJSON(w, http.StatusOK, map[string]any{"hidden": s.hiddenRows(ctx, me)})
}
