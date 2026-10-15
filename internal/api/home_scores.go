package api

// Home fork: scores from the sites people trust (IMDb, Rotten Tomatoes,
// Metacritic, Letterboxd, Trakt) and Common Sense Media's age, read from
// MDBList with the household's own free key (Settings > Extras), up to 200
// titles a call. A background job fills the library first; Discover and the
// title pages ask for the rest as they're shown. Each title is kept a month.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	keyMDBListKey   = "home.mdblist.key" // secret
	mdblistAPI      = "https://api.mdblist.com"
	scoresBatch     = 200
	scoresPerRun    = 1000
	scoresEvery     = time.Hour
	scoresKeepDays  = 30
	scoresNoneDays  = 7
	scoresMinBudget = 100 // calls left for the day kept for title pages and Discover
)

// titleScores is what MDBList knows about a title. Zero is unknown.
type titleScores struct {
	None        bool    `json:"none,omitempty"`
	IMDb        float64 `json:"imdb,omitempty"`        // out of 10
	IMDbVotes   int     `json:"imdbVotes,omitempty"`   //
	Tomatoes    int     `json:"tomatoes,omitempty"`    // Rotten Tomatoes critics, %
	Popcorn     int     `json:"popcorn,omitempty"`     // Rotten Tomatoes audience, %
	Metacritic  int     `json:"metacritic,omitempty"`  // out of 100
	Letterboxd  float64 `json:"letterboxd,omitempty"`  // out of 5
	Trakt       int     `json:"trakt,omitempty"`       // %
	CommonSense int     `json:"commonSense,omitempty"` // Common Sense Media's age, 13 = "13+"
	IMDbID      string  `json:"imdbId,omitempty"`
	RTPath      string  `json:"rtPath,omitempty"` // "/m/jaws"
	LBPath      string  `json:"lbPath,omitempty"` // "/film/jaws/"
}

type mdbItem struct {
	IDs struct {
		TMDB int    `json:"tmdb"`
		IMDb string `json:"imdb"`
	} `json:"ids"`
	Ratings []struct {
		Source string   `json:"source"`
		Value  *float64 `json:"value"`
		Score  *float64 `json:"score"`
		Votes  *int     `json:"votes"`
		URL    any      `json:"url"`
	} `json:"ratings"`
	Commonsense any `json:"commonsense"`
	AgeRating   int `json:"age_rating"`
}

func (it mdbItem) scores() titleScores {
	s := titleScores{IMDbID: it.IDs.IMDb}
	for _, r := range it.Ratings {
		if r.Value == nil {
			continue
		}
		v := *r.Value
		path, _ := r.URL.(string)
		switch r.Source {
		case "imdb":
			s.IMDb = v
			if r.Votes != nil {
				s.IMDbVotes = *r.Votes
			}
		case "tomatoes":
			s.Tomatoes = int(v)
			s.RTPath = path
		case "popcorn":
			s.Popcorn = int(v)
		case "metacritic":
			s.Metacritic = int(v)
		case "letterboxd":
			if r.Score != nil {
				s.Letterboxd = *r.Score / 20 // 80 -> 4.0 of 5
			}
			s.LBPath = path
		case "trakt":
			s.Trakt = int(v)
		}
	}
	if b, ok := it.Commonsense.(bool); ok && b && it.AgeRating > 0 {
		s.CommonSense = it.AgeRating
	}
	return s
}

var (
	scoresMu        sync.Mutex
	scoresRemaining = -1 // calls left today, from MDBList's headers; -1 unknown
	scoresResetAt   time.Time
)

func (s *Server) mdblistKey() string {
	k, _ := s.Settings.Get(keyMDBListKey)
	return strings.TrimSpace(k)
}

func scoresBudgetLeft() bool {
	scoresMu.Lock()
	defer scoresMu.Unlock()
	if !scoresResetAt.IsZero() && time.Now().After(scoresResetAt) {
		scoresRemaining = -1
	}
	return scoresRemaining < 0 || scoresRemaining > scoresMinBudget/4
}

// fetchScores asks MDBList for up to 200 titles of one kind and keeps the
// answers (and a "nothing" marker for the ones it doesn't know).
func (s *Server) fetchScores(ctx context.Context, kind string, ids []int, background bool) error {
	key := s.mdblistKey()
	if key == "" || len(ids) == 0 {
		return nil
	}
	if background {
		scoresMu.Lock()
		low := scoresRemaining >= 0 && scoresRemaining < scoresMinBudget && time.Now().Before(scoresResetAt)
		scoresMu.Unlock()
		if low {
			return nil
		}
	}
	mtype := "movie"
	if kind == "tv" {
		mtype = "show"
	}
	strs := make([]string, len(ids))
	for i, id := range ids {
		strs[i] = strconv.Itoa(id)
	}
	body, _ := json.Marshal(map[string]any{"ids": strs})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, mdblistAPI+"/tmdb/"+mtype+"?"+url.Values{"apikey": {key}}.Encode(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mediarium (home)")
	resp, err := (&http.Client{Timeout: 25 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("MDBList didn't answer")
	}
	defer resp.Body.Close()
	if n, err := strconv.Atoi(resp.Header.Get("X-RateLimit-Remaining")); err == nil {
		scoresMu.Lock()
		scoresRemaining = n
		if r, err := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil {
			scoresResetAt = time.Unix(r, 0)
		}
		scoresMu.Unlock()
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		scoresMu.Lock()
		scoresRemaining = 0
		if scoresResetAt.IsZero() || time.Now().After(scoresResetAt) {
			scoresResetAt = time.Now().Add(6 * time.Hour)
		}
		scoresMu.Unlock()
		return fmt.Errorf("MDBList's daily limit is used up")
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return fmt.Errorf("MDBList doesn't accept the key")
	case resp.StatusCode >= 300:
		return fmt.Errorf("MDBList answered %d", resp.StatusCode)
	}
	var items []mdbItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return fmt.Errorf("MDBList's answer couldn't be read")
	}
	got := map[int]titleScores{}
	for _, it := range items {
		if it.IDs.TMDB > 0 {
			got[it.IDs.TMDB] = it.scores()
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, id := range ids {
		sc, ok := got[id]
		if !ok {
			sc = titleScores{None: true}
		}
		b, _ := json.Marshal(sc)
		_, _ = s.db.ExecContext(ctx, `INSERT OR REPLACE INTO home_scores (kind, tmdb_id, data, fetched_at) VALUES (?, ?, ?, ?)`, kind, id, string(b), now)
	}
	return nil
}

// cachedScores reads what's kept for these titles; missing lists the ones
// with nothing kept, or kept too long.
func (s *Server) cachedScores(ctx context.Context, kind string, ids []int) (map[int]titleScores, []int) {
	out := map[int]titleScores{}
	if len(ids) == 0 {
		return out, nil
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := []any{kind}
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT tmdb_id, data, fetched_at FROM home_scores WHERE kind = ? AND tmdb_id IN (`+ph+`)`, args...)
	fresh := map[int]bool{}
	if err == nil {
		for rows.Next() {
			var id int
			var data, at string
			if rows.Scan(&id, &data, &at) != nil {
				continue
			}
			var sc titleScores
			_ = json.Unmarshal([]byte(data), &sc)
			when, _ := time.Parse(time.RFC3339, at)
			keep := scoresKeepDays
			if sc.None {
				keep = scoresNoneDays
			}
			if time.Since(when) < time.Duration(keep)*24*time.Hour {
				fresh[id] = true
			}
			if !sc.None {
				out[id] = sc
			}
		}
		rows.Close()
	}
	var missing []int
	for _, id := range ids {
		if !fresh[id] {
			missing = append(missing, id)
		}
	}
	return out, missing
}

// scoresFor returns the scores of these titles, asking MDBList for the ones
// not kept yet (one call, while the day's allowance lasts).
func (s *Server) scoresFor(ctx context.Context, kind string, ids []int) map[int]titleScores {
	have, missing := s.cachedScores(ctx, kind, ids)
	if len(missing) == 0 || s.mdblistKey() == "" || !scoresBudgetLeft() {
		return have
	}
	if len(missing) > scoresBatch {
		missing = missing[:scoresBatch]
	}
	fctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := s.fetchScores(fctx, kind, missing, false); err != nil {
		log.Printf("scores: %v", err)
		return have
	}
	got, _ := s.cachedScores(ctx, kind, missing)
	for id, sc := range got {
		have[id] = sc
	}
	return have
}

var scoresRunning sync.Mutex

// scoresJob fills in the library's titles: films, shows, kept or streamed.
func (s *Server) scoresJob(ctx context.Context) {
	if s.mdblistKey() == "" || !scoresRunning.TryLock() {
		return
	}
	defer scoresRunning.Unlock()
	cut := time.Now().AddDate(0, 0, -scoresKeepDays).UTC().Format(time.RFC3339)
	rows, err := s.db.QueryContext(ctx, `
		SELECT kind, tmdb_id FROM (
			SELECT 'movie' AS kind, tmdb_id FROM movies
			UNION SELECT 'tv', tmdb_id FROM series
			UNION SELECT CASE kind WHEN 'movie' THEN 'movie' ELSE 'tv' END, tmdb_id FROM streamed_titles
		) t
		WHERE tmdb_id > 0 AND NOT EXISTS (SELECT 1 FROM home_scores h WHERE h.kind = t.kind AND h.tmdb_id = t.tmdb_id AND h.fetched_at > ?)
		LIMIT ?`, cut, scoresPerRun)
	if err != nil {
		return
	}
	todo := map[string][]int{}
	for rows.Next() {
		var kind string
		var id int
		if rows.Scan(&kind, &id) == nil {
			todo[kind] = append(todo[kind], id)
		}
	}
	rows.Close()
	for _, kind := range []string{"movie", "tv"} {
		ids := todo[kind]
		for start := 0; start < len(ids); start += scoresBatch {
			if ctx.Err() != nil {
				return
			}
			if err := s.fetchScores(ctx, kind, ids[start:min(start+scoresBatch, len(ids))], true); err != nil {
				log.Printf("scores: %v", err)
				return
			}
		}
	}
}

// GET /api/home/scores?kind=movie|tv&ids=1,2,3 (up to 200): scores by TMDB id.
func (s *Server) handleScores(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	if kind != "movie" {
		kind = "tv"
	}
	var ids []int
	for _, f := range strings.Split(r.URL.Query().Get("ids"), ",") {
		if id, err := strconv.Atoi(strings.TrimSpace(f)); err == nil && id > 0 && len(ids) < scoresBatch {
			ids = append(ids, id)
		}
	}
	out := map[string]titleScores{}
	for id, sc := range s.scoresFor(r.Context(), kind, ids) {
		out[strconv.Itoa(id)] = sc
	}
	writeJSON(w, http.StatusOK, out)
}

// ---- the key (Settings > Extras) ----

// GET /api/home/mdblist
func (s *Server) handleGetMDBList(w http.ResponseWriter, r *http.Request) {
	var n int
	_ = s.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM home_scores WHERE data NOT LIKE '%"none":true%'`).Scan(&n)
	scoresMu.Lock()
	left := scoresRemaining
	scoresMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"keySet": s.mdblistKey() != "", "titles": n, "callsLeft": left})
}

// PUT /api/home/mdblist {"key": "...", "removeKey": false}
func (s *Server) handlePutMDBList(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key       *string `json:"key"`
		RemoveKey bool    `json:"removeKey"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	if req.RemoveKey {
		_ = s.Settings.Set(keyMDBListKey, "", false)
	} else if req.Key != nil && strings.TrimSpace(*req.Key) != "" {
		_ = s.Settings.Set(keyMDBListKey, strings.TrimSpace(*req.Key), true)
		s.background(func() { s.scoresJob(context.Background()) })
	}
	s.handleGetMDBList(w, r)
}

// POST /api/home/mdblist/test: reads one well-known film's scores.
func (s *Server) handleTestMDBList(w http.ResponseWriter, r *http.Request) {
	if s.mdblistKey() == "" {
		writeError(w, http.StatusBadRequest, "Add your MDBList key first.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	if err := s.fetchScores(ctx, "movie", []int{578}, false); err != nil { // Jaws
		writeError(w, http.StatusBadGateway, err.Error()+".")
		return
	}
	got, _ := s.cachedScores(ctx, "movie", []int{578})
	sc := got[578]
	scoresMu.Lock()
	left := scoresRemaining
	scoresMu.Unlock()
	msg := fmt.Sprintf("It works: Jaws has IMDb %.1f, Rotten Tomatoes %d%%, Metacritic %d.", sc.IMDb, sc.Tomatoes, sc.Metacritic)
	if left >= 0 {
		msg += fmt.Sprintf(" %d calls left today.", left)
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": msg})
}
