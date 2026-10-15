package api

// Home fork: AI in Mediarium. Settings > Extras picks the model: a local one
// through Ollama (or any OpenAI-compatible server), or Claude. AI Search
// (Discover) turns a description or a photo into titles: the model only
// names them; every one is looked up on TMDB, and only titles TMDB has are
// shown, so a model that misremembers can't invent one. Each person gets a
// number of answers a day (administrators aren't limited); repeated
// questions are answered from memory for an hour, free.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	_ "image/gif" // photos for AI Search
	"image/jpeg"
	_ "image/png" // photos for AI Search
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/rdborg/mediarium/internal/ai"
	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/metadata"
)

const (
	keyAIProvider = "home.ai.provider" // "", ollama, openai or claude
	keyAIURL      = "home.ai.url"
	keyAIModel    = "home.ai.model"
	keyAIKey      = "home.ai.key" // secret
	keyAILimit    = "home.ai.daily_limit"
	aiLimitDflt   = 20
)

func (s *Server) aiConfig() (ai.Config, bool) {
	p, _ := s.Settings.Get(keyAIProvider)
	if p == "" {
		return ai.Config{}, false
	}
	cfg := ai.Config{Provider: p}
	cfg.URL, _ = s.Settings.Get(keyAIURL)
	cfg.Model, _ = s.Settings.Get(keyAIModel)
	cfg.Key, _ = s.Settings.Get(keyAIKey)
	switch p {
	case ai.Ollama:
		if cfg.URL == "" {
			cfg.URL = ai.DefaultOllamaURL
		}
		if cfg.Model == "" {
			cfg.Model = ai.DefaultOllamaModel
		}
	case ai.Claude:
		if cfg.Model == "" {
			cfg.Model = ai.DefaultClaudeModel
		}
	}
	return cfg, cfg.Model != ""
}

func (s *Server) aiLimit() int {
	v, _ := s.Settings.Get(keyAILimit)
	if n, err := strconv.Atoi(v); err == nil && n >= 0 {
		return n
	}
	return aiLimitDflt
}

// GET /api/home/ai: the AI settings (administrators).
func (s *Server) handleGetAI(w http.ResponseWriter, r *http.Request) {
	cfg, _ := s.aiConfig()
	key, _ := s.Settings.Get(keyAIKey)
	writeJSON(w, http.StatusOK, map[string]any{
		"provider": cfg.Provider, "url": cfg.URL, "model": cfg.Model, "keySet": key != "", "dailyLimit": s.aiLimit(),
		"defaults": map[string]string{"ollamaUrl": ai.DefaultOllamaURL, "ollamaModel": ai.DefaultOllamaModel, "claudeModel": ai.DefaultClaudeModel},
	})
}

// PUT /api/home/ai: change the AI settings; only what's sent changes.
func (s *Server) handlePutAI(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider   *string `json:"provider"`
		URL        *string `json:"url"`
		Model      *string `json:"model"`
		Key        *string `json:"key"`
		RemoveKey  bool    `json:"removeKey"`
		DailyLimit *int    `json:"dailyLimit"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	if req.Provider != nil {
		switch *req.Provider {
		case "", ai.Ollama, ai.OpenAI, ai.Claude:
			_ = s.Settings.Set(keyAIProvider, *req.Provider, false)
		default:
			writeError(w, http.StatusBadRequest, "Pick Ollama, an OpenAI-compatible server or Claude.")
			return
		}
	}
	if req.URL != nil {
		u, err := cleanBaseURL(*req.URL)
		if err != nil {
			writeError(w, http.StatusBadRequest, "The AI's address "+err.Error())
			return
		}
		_ = s.Settings.Set(keyAIURL, u, false)
	}
	if req.Model != nil {
		_ = s.Settings.Set(keyAIModel, strings.TrimSpace(*req.Model), false)
	}
	if req.RemoveKey {
		_ = s.Settings.Set(keyAIKey, "", false)
	} else if req.Key != nil && strings.TrimSpace(*req.Key) != "" {
		_ = s.Settings.Set(keyAIKey, strings.TrimSpace(*req.Key), true)
	}
	if req.DailyLimit != nil && *req.DailyLimit >= 0 {
		_ = s.Settings.Set(keyAILimit, strconv.Itoa(*req.DailyLimit), false)
	}
	aiCacheMu.Lock()
	aiCache = map[string]aiCached{}
	aiCacheMu.Unlock()
	s.handleGetAI(w, r)
}

// POST /api/home/ai/test: does the model answer?
func (s *Server) handleTestAI(w http.ResponseWriter, r *http.Request) {
	cfg, ok := s.aiConfig()
	if !ok {
		writeError(w, http.StatusBadRequest, "Pick an AI first.")
		return
	}
	start := time.Now()
	ans, err := ai.Ask(r.Context(), cfg, ai.Request{Prompt: "Reply with the single word OK.", MaxTokens: 10})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "The AI (" + cfg.Model + ") answered \"" + strings.TrimSpace(ans) + "\" in " + time.Since(start).Round(100*time.Millisecond).String() + "."})
}

// GET /api/home/ai/on: whether AI features can be offered.
func (s *Server) handleAIOn(w http.ResponseWriter, r *http.Request) {
	cfg, ok := s.aiConfig()
	writeJSON(w, http.StatusOK, map[string]any{"on": ok, "provider": cfg.Provider})
}

// ---- answers from memory, and the daily limit ----

type aiCached struct {
	at   time.Time
	data any
}

var (
	aiCacheMu sync.Mutex
	aiCache   = map[string]aiCached{}
)

func aiCacheKey(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// aiAllowed counts an answer against the person's daily limit; false when
// they've used them all today.
func (s *Server) aiAllowed(ctx context.Context, u *auth.User) bool {
	if u == nil {
		return false
	}
	if u.IsAdmin {
		return true
	}
	limit := s.aiLimit()
	if limit == 0 {
		return true
	}
	day := time.Now().Format("2006-01-02")
	var n int
	_ = s.db.QueryRowContext(ctx, `SELECT n FROM home_ai_usage WHERE user_id = ? AND day = ?`, u.ID, day).Scan(&n)
	if n >= limit {
		return false
	}
	_, _ = s.db.ExecContext(ctx, `INSERT INTO home_ai_usage (user_id, day, n) VALUES (?, ?, 1)
		ON CONFLICT(user_id, day) DO UPDATE SET n = n + 1`, u.ID, day)
	return true
}

// ---- AI Search ----

const aiSearchSystem = `You help people find films and TV shows. Reply with a JSON object only, shaped like this:
{"titles":[{"title":"Heat","year":1995,"type":"movie"}],"person":"","collection":"","note":""}
- titles: up to 10 real titles that best match what they describe, most likely first. type is "movie" or "tv". Give the year of release when you know it.
- person: when they want the work of one director, writer or actor, that person's name; otherwise "".
- collection: when they want a whole film series or franchise, its name (such as "The Lord of the Rings Collection"); otherwise "".
- note: one short, friendly sentence about what you found.
If a photo is given, name the films or shows it shows (a poster, a cover, a screen, a list).`

type aiTitle struct {
	Title string `json:"title"`
	Year  int    `json:"year"`
	Type  string `json:"type"`
}

type aiSearchAnswer struct {
	Titles     []aiTitle `json:"titles"`
	Person     string    `json:"person"`
	Collection string    `json:"collection"`
	Note       string    `json:"note"`
}

type aiSearchResult struct {
	Note   string       `json:"note"`
	Movies []browseItem `json:"movies"`
	Shows  []browseItem `json:"shows"`
}

// POST /api/home/ai/search {query, image}: titles for a description or a photo.
func (s *Server) handleAISearch(w http.ResponseWriter, r *http.Request) {
	cfg, ok := s.aiConfig()
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "AI isn't set up. An administrator can set it up under Settings > Extras.")
		return
	}
	var req struct {
		Query string `json:"query"`
		Image string `json:"image"` // a data: URL
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	req.Query = strings.TrimSpace(req.Query)
	if len(req.Query) > 500 {
		req.Query = req.Query[:500]
	}
	var img *ai.Image
	if req.Image != "" {
		im, err := photoForAI(req.Image)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		img = im
	}
	if req.Query == "" && img == nil {
		writeError(w, http.StatusBadRequest, "Describe what you're looking for, or send a photo.")
		return
	}
	imgKey := ""
	if img != nil {
		imgKey = sha256Hex(img.Data)
	}
	key := aiCacheKey("search", cfg.Provider, cfg.Model, strings.ToLower(req.Query), imgKey)
	aiCacheMu.Lock()
	if c, ok := aiCache[key]; ok && time.Since(c.at) < time.Hour {
		aiCacheMu.Unlock()
		writeJSON(w, http.StatusOK, c.data)
		return
	}
	aiCacheMu.Unlock()
	if !s.aiAllowed(r.Context(), auth.UserFromContext(r.Context())) {
		writeError(w, http.StatusTooManyRequests, "That's all the AI answers for today. More tomorrow.")
		return
	}
	prompt := req.Query
	if prompt == "" {
		prompt = "What films or shows are in this photo?"
	}
	areq := ai.Request{System: aiSearchSystem, Prompt: prompt, JSON: true, MaxTokens: 700}
	if img != nil {
		areq.Images = []ai.Image{*img}
	}
	raw, err := ai.Ask(r.Context(), cfg, areq)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	var ans aiSearchAnswer
	if err := json.Unmarshal([]byte(ai.JSONObject(raw)), &ans); err != nil {
		writeError(w, http.StatusBadGateway, "The AI's answer couldn't be read. Try asking another way.")
		return
	}
	res := s.resolveAISearch(r.Context(), ans, req.Query)
	if len(res.Movies)+len(res.Shows) == 0 && res.Note == "" {
		res.Note = "Nothing found for that. Try describing it another way."
	}
	aiCacheMu.Lock()
	aiCache[key] = aiCached{at: time.Now(), data: res}
	aiCacheMu.Unlock()
	writeJSON(w, http.StatusOK, res)
}

// resolveAISearch turns the model's answer into titles TMDB has.
// The person's work and the film series are followed only when the question
// names them: a model tends to add the lead actor of whatever it
// recognised, which isn't what a photo of a poster asks for.
func (s *Server) resolveAISearch(ctx context.Context, ans aiSearchAnswer, query string) aiSearchResult {
	tm := s.displayTMDB()
	var movies []metadata.Movie
	var shows []metadata.Show
	seenM, seenS := map[int]bool{}, map[int]bool{}
	addM := func(m metadata.Movie) {
		if m.TMDBID > 0 && !seenM[m.TMDBID] {
			seenM[m.TMDBID] = true
			movies = append(movies, m)
		}
	}
	addS := func(sh metadata.Show) {
		if sh.TMDBID > 0 && !seenS[sh.TMDBID] {
			seenS[sh.TMDBID] = true
			shows = append(shows, sh)
		}
	}
	// Named titles first: they answer the question most directly.
	type found struct {
		movie *metadata.Movie
		show  *metadata.Show
	}
	results := make([]found, len(ans.Titles))
	var wg sync.WaitGroup
	for i, t := range ans.Titles {
		if i >= 12 || strings.TrimSpace(t.Title) == "" {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			m, sh := matchTitle(ctx, tm, t)
			results[i] = found{m, sh}
		}()
	}
	wg.Wait()
	for _, f := range results {
		if f.movie != nil {
			addM(*f.movie)
		}
		if f.show != nil {
			addS(*f.show)
		}
	}
	if p := strings.TrimSpace(ans.Person); p != "" && namedIn(query, p) {
		if id, err := tm.PersonID(ctx, p); err == nil && id > 0 {
			if ms, ss, err := tm.PersonWork(ctx, id); err == nil {
				for _, m := range ms[:min(20, len(ms))] {
					addM(m)
				}
				for _, sh := range ss[:min(10, len(ss))] {
					addS(sh)
				}
			}
		}
	}
	if c := strings.TrimSpace(ans.Collection); c != "" && namedIn(query, strings.TrimSuffix(strings.TrimSpace(c), " Collection")) {
		if id, err := tm.CollectionID(ctx, c); err == nil && id > 0 {
			if col, err := tm.GetCollection(ctx, id); err == nil {
				for _, p := range col.Parts {
					addM(metadata.Movie{TMDBID: p.TMDBID, Title: p.DisplayTitle(), Overview: p.Overview, ReleaseDate: p.ReleaseDate, PosterPath: p.PosterPath, VoteAverage: p.VoteAverage})
				}
			}
		}
	}
	// "The whole … series": the film series of the first films found, from
	// TMDB, whatever the model said, so none is left out.
	if wantsSeries.MatchString(query) {
		for _, m := range movies[:min(2, len(movies))] {
			full, err := tm.GetMovie(ctx, m.TMDBID)
			if err != nil || full.BelongsToCollection == nil {
				continue
			}
			if col, err := tm.GetCollection(ctx, full.BelongsToCollection.ID); err == nil {
				for _, p := range col.Parts {
					if p.ReleaseDate != "" {
						addM(metadata.Movie{TMDBID: p.TMDBID, Title: p.DisplayTitle(), Overview: p.Overview, ReleaseDate: p.ReleaseDate, PosterPath: p.PosterPath, VoteAverage: p.VoteAverage})
					}
				}
			}
		}
	}
	res := aiSearchResult{Note: strings.TrimSpace(ans.Note), Movies: []browseItem{}, Shows: []browseItem{}}
	if len(movies) > 0 {
		res.Movies = s.moviePagePayload(ctx, &metadata.MoviePage{Page: 1, TotalPages: 1, TotalResults: len(movies), Results: movies}).Results
	}
	if len(shows) > 0 {
		res.Shows = s.showPagePayload(ctx, &metadata.ShowPage{Page: 1, TotalPages: 1, TotalResults: len(shows), Results: shows}).Results
	}
	return res
}

// namedIn reports whether the question mentions the name: any of its
// words of four letters or more ("Villeneuve", "Alien").
func namedIn(query, name string) bool {
	q := " " + normTitle(query) + " "
	for _, w := range strings.Fields(normTitle(name)) {
		if len([]rune(w)) >= 4 && strings.Contains(q, " "+w) {
			return true
		}
	}
	return false
}

// wantsSeries: the question asks for a whole film series.
var wantsSeries = regexp.MustCompile(`(?i)\b(series|franchise|saga|collection|trilogy|all the|whole|every one)\b`)

var nonWord = regexp.MustCompile(`[^\p{L}\p{N}]+`)

// normTitle compares titles without case, punctuation or a leading "the".
func normTitle(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "the ")
	s = nonWord.ReplaceAllString(s, " ")
	return strings.Join(strings.FieldsFunc(s, unicode.IsSpace), " ")
}

// matchTitle finds the model's title on TMDB: the same title (near enough)
// and, when it gave one, a year at most one off. A title TMDB doesn't have,
// or only has under a clearly different name, is dropped.
func matchTitle(ctx context.Context, tm *metadata.Client, t aiTitle) (*metadata.Movie, *metadata.Show) {
	res, err := tm.SearchMulti(ctx, t.Title)
	if err != nil {
		return nil, nil
	}
	type cand struct {
		r     metadata.MultiResult
		score float64
	}
	var cands []cand
	for _, r := range res {
		if r.MediaType != "movie" && r.MediaType != "tv" {
			continue
		}
		name, date := r.Title, r.ReleaseDate
		if r.MediaType == "tv" {
			name, date = r.Name, r.FirstAirDate
		}
		if !sameTitle(t.Title, name) {
			continue
		}
		score := r.Popularity
		if t.Type != "" && r.MediaType == strings.ToLower(t.Type) {
			score += 1000
		}
		if t.Year > 0 {
			y, _ := strconv.Atoi(firstN(date, 4))
			if y == 0 || y < t.Year-1 || y > t.Year+1 {
				continue
			}
			score += 2000
		}
		cands = append(cands, cand{r, score})
	}
	if len(cands) == 0 {
		return nil, nil
	}
	// A film when a film was meant, a show when a show was: the other kind
	// only when TMDB has nothing of the kind named.
	if kind := strings.ToLower(t.Type); kind == "movie" || kind == "tv" {
		var same []cand
		for _, c := range cands {
			if c.r.MediaType == kind {
				same = append(same, c)
			}
		}
		if len(same) > 0 {
			cands = same
		}
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].score > cands[j].score })
	r := cands[0].r
	if r.MediaType == "tv" {
		return nil, &metadata.Show{TMDBID: r.ID, Name: r.Name, Overview: r.Overview, FirstAirDate: r.FirstAirDate, PosterPath: r.PosterPath, VoteAverage: r.VoteAverage, VoteCount: r.VoteCount, Popularity: r.Popularity, GenreIDs: r.GenreIDs}
	}
	return &metadata.Movie{TMDBID: r.ID, Title: r.Title, Overview: r.Overview, ReleaseDate: r.ReleaseDate, PosterPath: r.PosterPath, VoteAverage: r.VoteAverage, VoteCount: r.VoteCount, Popularity: r.Popularity, GenreIDs: r.GenreIDs}, nil
}

// sameTitle: the same title, near enough. TMDB's name may add a subtitle
// ("Alien: Covenant" for "Alien: Covenant", but "Iron Fists" is not "Iron
// Fist") or Marvel's prefix ("Marvel's Iron Fist").
func sameTitle(asked, name string) bool {
	want, got := normTitle(asked), normTitle(name)
	if got == want || strings.TrimPrefix(got, "marvel s ") == want {
		return true
	}
	a, n := strings.ToLower(strings.TrimSpace(asked)), strings.ToLower(strings.TrimSpace(name))
	return len(want) >= 4 && strings.HasPrefix(n, a+":")
}

func firstN(s string, n int) string {
	if len(s) < n {
		return s
	}
	return s[:n]
}

// photoForAI reads a photo sent as a data: URL and makes it smaller (at most
// 1024 pixels across) as a fresh JPEG, which also leaves out where and with
// what it was taken.
func photoForAI(dataURL string) (*ai.Image, error) {
	comma := strings.Index(dataURL, ",")
	if !strings.HasPrefix(dataURL, "data:image/") || comma < 0 {
		return nil, errors.New("send the photo as an image")
	}
	raw, err := base64.StdEncoding.DecodeString(dataURL[comma+1:])
	if err != nil || len(raw) > 15<<20 {
		return nil, errors.New("that photo couldn't be read (or is too big)")
	}
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, errors.New("that photo couldn't be read; send a JPEG or PNG")
	}
	b := src.Bounds()
	scale := 1024.0 / float64(max(b.Dx(), b.Dy()))
	if scale > 1 {
		scale = 1
	}
	w, h := max(1, int(float64(b.Dx())*scale)), max(1, int(float64(b.Dy())*scale))
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		sy := b.Min.Y + int(float64(y)/scale)
		for x := 0; x < w; x++ {
			dst.Set(x, y, src.At(b.Min.X+int(float64(x)/scale), sy))
		}
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 85}); err != nil {
		return nil, err
	}
	return &ai.Image{MIME: "image/jpeg", Data: out.Bytes()}, nil
}
