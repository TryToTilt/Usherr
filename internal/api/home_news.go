package api

// Home fork: entertainment news on the Dashboard. A background job reads the
// news feeds set in Settings > Extras every few hours, keeps a week of
// stories, and has the AI write a one- or two-sentence summary of each (from
// the feed's own teaser, never the whole article) and name the films, shows,
// books, comics and music it is about. The Dashboard links each one to the
// library, or offers to add it. A daily budget caps how many stories are
// summarised, so the AI's cost stays small.

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/ai"
	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/internal/news"
)

const (
	keyNewsSources = "home.news.sources" // JSON list of newsSource
	keyNewsBudget  = "home.news.daily"   // stories summarised a day
	keyNewsOn      = "home.news.on"      // "0" switches the news off
	newsBudgetDflt = 40
	newsKeepDays   = 7
	newsEvery      = 3 * time.Hour
)

// News categories, as the Dashboard groups them.
var newsCategories = []string{"film-tv", "home-theater", "books", "comics", "music"}

type newsSource struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Category string `json:"category"`
	On       bool   `json:"on"`
}

// defaultNewsSources is the starting list; every one can be changed in Extras.
var defaultNewsSources = []newsSource{
	{"Variety", "https://variety.com/feed/", "film-tv", true},
	{"Deadline", "https://deadline.com/feed/", "film-tv", true},
	{"The Hollywood Reporter", "https://www.hollywoodreporter.com/feed/", "film-tv", true},
	{"IndieWire", "https://www.indiewire.com/feed/", "film-tv", true},
	{"FlatpanelsHD", "https://www.flatpanelshd.com/rss/news.xml", "home-theater", true},
	{"Sound & Vision", "https://www.soundandvision.com/rss.xml", "home-theater", true},
	{"Audioholics", "https://www.audioholics.com/rss.xml", "home-theater", true},
	{"The Verge: Entertainment", "https://www.theverge.com/rss/entertainment/index.xml", "home-theater", true},
	{"Book Riot", "https://bookriot.com/feed/", "books", true},
	{"The Beat", "https://www.comicsbeat.com/feed/", "comics", true},
	{"CBR", "https://www.cbr.com/feed/", "comics", false},
	{"Stereogum", "https://www.stereogum.com/feed/", "music", true},
}

func (s *Server) newsSources() []newsSource {
	v, _ := s.Settings.Get(keyNewsSources)
	if strings.TrimSpace(v) == "" {
		return append([]newsSource(nil), defaultNewsSources...)
	}
	var list []newsSource
	if json.Unmarshal([]byte(v), &list) != nil {
		return append([]newsSource(nil), defaultNewsSources...)
	}
	return list
}

func (s *Server) newsBudget() int {
	v, _ := s.Settings.Get(keyNewsBudget)
	if n, err := strconv.Atoi(v); err == nil && n >= 0 {
		return n
	}
	return newsBudgetDflt
}

func (s *Server) newsOn() bool {
	v, _ := s.Settings.Get(keyNewsOn)
	return v != "0"
}

// ---- reading the feeds ----

var newsRunning sync.Mutex

// newsJob reads the feeds and summarises what's new (the automation job).
func (s *Server) newsJob(ctx context.Context) {
	if !s.newsOn() {
		return
	}
	if _, ok := s.aiConfig(); !ok {
		return
	}
	if !newsRunning.TryLock() {
		return
	}
	defer newsRunning.Unlock()
	s.fetchNews(ctx)
	s.summariseNews(ctx)
	_, _ = s.db.ExecContext(ctx, `DELETE FROM home_news WHERE published_at < ?`, time.Now().AddDate(0, 0, -newsKeepDays).UTC().Format(time.RFC3339))
}

// fetchNews stores the stories of the last two days not seen before.
func (s *Server) fetchNews(ctx context.Context) {
	since := time.Now().Add(-48 * time.Hour)
	for _, src := range s.newsSources() {
		if !src.On || src.URL == "" {
			continue
		}
		items, err := news.Fetch(ctx, src.URL, "Mozilla/5.0 (compatible; Mediarium-home/"+s.version+")")
		if err != nil {
			log.Printf("news: %s: %v", src.Name, err)
			continue
		}
		for _, it := range items {
			at := it.Published
			if at.IsZero() {
				at = time.Now().UTC()
			}
			if at.Before(since) {
				continue
			}
			_, _ = s.db.ExecContext(ctx, `INSERT INTO home_news (guid, source, category, title, link, published_at, teaser)
				VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT(guid) DO NOTHING`,
				src.Name+"|"+it.GUID, src.Name, src.Category, firstN(it.Title, 300), it.Link, at.UTC().Format(time.RFC3339), it.Teaser)
		}
	}
}

type newsRow struct {
	ID       int64
	Source   string
	Category string
	Title    string
	Teaser   string
}

// newsToday is how many stories were summarised in the last 24 hours.
func (s *Server) newsToday(ctx context.Context) int {
	var n int
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM home_news WHERE state = 'done' AND created_at >= ?`, time.Now().Add(-24*time.Hour).UTC().Format(time.RFC3339)).Scan(&n)
	return n
}

// pickNews chooses which waiting stories to summarise: the newest of each
// source in turn, so one busy feed can't use up the day.
func pickNews(rows []newsRow, n int) []newsRow {
	bySource := map[string][]newsRow{}
	var order []string
	for _, r := range rows {
		if _, ok := bySource[r.Source]; !ok {
			order = append(order, r.Source)
		}
		bySource[r.Source] = append(bySource[r.Source], r)
	}
	var out []newsRow
	for len(out) < n {
		took := false
		for _, src := range order {
			if len(out) == n {
				break
			}
			if q := bySource[src]; len(q) > 0 {
				out = append(out, q[0])
				bySource[src] = q[1:]
				took = true
			}
		}
		if !took {
			break
		}
	}
	return out
}

const newsSystem = `You write bite-sized entertainment news for a home media server's dashboard. For each story you're given its source, headline and the feed's teaser. Reply with a JSON object only, shaped like this:
{"stories":[{"id":12,"summary":"...","mentions":[{"title":"Dune: Part Three","year":2026,"type":"movie","creator":""}]}]}
- summary: one or two short sentences in your own words, from the headline and teaser only. Plain and factual; no hype, no opinions, nothing that isn't in the text.
- mentions: the specific films, TV shows, books, comics or music the story is about (not people, companies or devices on their own). type is "movie", "tv", "book", "comic", "album" or "artist". creator is the author or band when that helps find it, else "". Always give the year of release for films and shows; if you can't tell which one the story means, leave it out. An empty list when the story isn't about one.
Include every story id you were given.`

type newsMention struct {
	Title   string `json:"title"`
	Year    int    `json:"year,omitempty"`
	Type    string `json:"type"`
	Creator string `json:"creator,omitempty"`
	// Filled in by Mediarium: the TMDB id for films and shows, and a poster.
	TMDBID    int    `json:"tmdbId,omitempty"`
	PosterURL string `json:"posterUrl,omitempty"`
}

// summariseNews has the AI summarise waiting stories, within the day's budget.
func (s *Server) summariseNews(ctx context.Context) {
	cfg, ok := s.aiConfig()
	if !ok {
		return
	}
	left := s.newsBudget() - s.newsToday(ctx)
	if left <= 0 {
		return
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, source, category, title, teaser FROM home_news WHERE state = 'new' ORDER BY published_at DESC LIMIT 400`)
	if err != nil {
		return
	}
	var waiting []newsRow
	for rows.Next() {
		var r newsRow
		if rows.Scan(&r.ID, &r.Source, &r.Category, &r.Title, &r.Teaser) == nil {
			waiting = append(waiting, r)
		}
	}
	rows.Close()
	chosen := pickNews(waiting, left)
	// What wasn't chosen this time stays waiting until it's two days old.
	_, _ = s.db.ExecContext(ctx, `UPDATE home_news SET state = 'skipped' WHERE state = 'new' AND published_at < ?`, time.Now().Add(-48*time.Hour).UTC().Format(time.RFC3339))
	tm := s.displayTMDB()
	for start := 0; start < len(chosen); start += 10 {
		batch := chosen[start:min(start+10, len(chosen))]
		var sb strings.Builder
		for _, r := range batch {
			fmt.Fprintf(&sb, "id %d | %s | %s\n%s\n\n", r.ID, r.Source, r.Title, firstN(r.Teaser, 500))
		}
		raw, err := ai.Ask(ctx, cfg, ai.Request{System: newsSystem, Prompt: sb.String(), JSON: true, MaxTokens: 4000})
		if err != nil {
			log.Printf("news: summaries: %v", err)
			return
		}
		var ans struct {
			Stories []struct {
				ID       int64         `json:"id"`
				Summary  string        `json:"summary"`
				Mentions []newsMention `json:"mentions"`
			} `json:"stories"`
		}
		if err := json.Unmarshal([]byte(ai.JSONObject(raw)), &ans); err != nil {
			log.Printf("news: the AI's answer couldn't be read: %v", err)
			continue
		}
		inBatch := map[int64]bool{}
		for _, r := range batch {
			inBatch[r.ID] = true
		}
		for _, st := range ans.Stories {
			if !inBatch[st.ID] || strings.TrimSpace(st.Summary) == "" {
				continue
			}
			ms := s.resolveMentions(ctx, tm, st.Mentions)
			b, _ := json.Marshal(ms)
			_, _ = s.db.ExecContext(ctx, `UPDATE home_news SET summary = ?, mentions = ?, state = 'done', created_at = ? WHERE id = ?`,
				firstN(strings.TrimSpace(st.Summary), 600), string(b), time.Now().UTC().Format(time.RFC3339), st.ID)
		}
	}
}

// resolveMentions keeps up to four mentions, and finds films and shows on TMDB.
func (s *Server) resolveMentions(ctx context.Context, tm *metadata.Client, in []newsMention) []newsMention {
	out := []newsMention{}
	for _, m := range in {
		if len(out) == 4 {
			break
		}
		m.Title = strings.TrimSpace(m.Title)
		if m.Title == "" {
			continue
		}
		switch m.Type {
		case "movie", "tv":
			// Without a year, a title several films or shows share is left
			// to Find rather than guessed ("Season of the Witch", 1972 or 2011).
			if m.Year == 0 && titleAmbiguous(ctx, tm, m.Title) {
				out = append(out, m)
				continue
			}
			mv, sh := matchTitle(ctx, tm, aiTitle{Title: m.Title, Year: m.Year, Type: m.Type})
			if mv != nil {
				m.Type, m.TMDBID, m.Year, m.PosterURL = "movie", mv.TMDBID, mv.Year(), metadata.PosterURL(mv.PosterPath)
			} else if sh != nil {
				m.Type, m.TMDBID, m.Year, m.PosterURL = "tv", sh.TMDBID, sh.Year(), metadata.PosterURL(sh.PosterPath)
			}
		case "book", "comic", "album", "artist":
		default:
			continue
		}
		out = append(out, m)
	}
	return out
}

// titleAmbiguous reports whether TMDB has more than one film or show by
// that exact name.
func titleAmbiguous(ctx context.Context, tm *metadata.Client, title string) bool {
	res, err := tm.SearchMulti(ctx, title)
	if err != nil {
		return true
	}
	n := 0
	want := normTitle(title)
	for _, r := range res {
		name := r.Title
		if r.MediaType == "tv" {
			name = r.Name
		}
		if (r.MediaType == "movie" || r.MediaType == "tv") && normTitle(name) == want {
			n++
		}
	}
	return n > 1
}

// ---- the Dashboard ----

type newsMentionOut struct {
	newsMention
	// Have: it's in the library (Open goes there). To: where Open or Find
	// goes. Add: a film or show not in the library, offered with the add dialog.
	Have bool   `json:"have"`
	To   string `json:"to"`
	Add  bool   `json:"add"`
}

type newsItemOut struct {
	ID          int64            `json:"id"`
	Source      string           `json:"source"`
	Category    string           `json:"category"`
	Title       string           `json:"title"`
	Link        string           `json:"link"`
	PublishedAt string           `json:"publishedAt"`
	Summary     string           `json:"summary"`
	Mentions    []newsMentionOut `json:"mentions"`
}

// newsLibrary is what the household has, by the keys news mentions use.
type newsLibrary struct {
	movies  map[int]bool
	shows   map[int]int64 // TMDB id -> library show
	comics  map[string]int64
	artists map[string]int64
	books   map[string]int64
}

func (s *Server) newsLibrary(ctx context.Context) newsLibrary {
	lib := newsLibrary{movies: s.ownedMovies(ctx), shows: map[int]int64{}, comics: map[string]int64{}, artists: map[string]int64{}, books: map[string]int64{}}
	if list, err := s.MovieRepo.ListSeries(); err == nil {
		for _, sr := range list {
			if sr.CopyOf == 0 {
				lib.shows[sr.TMDBID] = sr.ID
			}
		}
	}
	if list, err := s.comicRepo().ListSeries(); err == nil {
		for _, c := range list {
			lib.comics[normTitle(c.Name)] = c.ID
		}
	}
	if list, err := s.MusicRepo.ListArtists(); err == nil {
		for _, a := range list {
			lib.artists[normTitle(a.Name)] = a.ID
		}
	}
	if list, err := s.BookRepo.List(); err == nil {
		for _, b := range list {
			lib.books[normTitle(b.Title)] = b.ID
		}
	}
	return lib
}

func (lib newsLibrary) place(m newsMention) newsMentionOut {
	o := newsMentionOut{newsMention: m}
	find := "/search?q=" + url.QueryEscape(m.Title)
	switch m.Type {
	case "movie":
		if m.TMDBID > 0 {
			o.To = "/title/" + strconv.Itoa(m.TMDBID)
			o.Have = lib.movies[m.TMDBID]
			o.Add = !o.Have
			return o
		}
	case "tv":
		if m.TMDBID > 0 {
			if id := lib.shows[m.TMDBID]; id > 0 {
				o.Have, o.To = true, "/series/"+strconv.FormatInt(id, 10)
			} else {
				o.To, o.Add = "/show/"+strconv.Itoa(m.TMDBID), true
			}
			return o
		}
	case "comic":
		if id := lib.comics[normTitle(m.Title)]; id > 0 {
			o.Have, o.To = true, "/comics/"+strconv.FormatInt(id, 10)
		} else {
			o.To = "/comics?tab=add&q=" + url.QueryEscape(m.Title)
		}
		return o
	case "artist":
		if id := lib.artists[normTitle(m.Title)]; id > 0 {
			o.Have, o.To = true, "/music/artist/"+strconv.FormatInt(id, 10)
			return o
		}
	case "album":
		if m.Creator != "" {
			if id := lib.artists[normTitle(m.Creator)]; id > 0 {
				o.Have, o.To = true, "/music/artist/"+strconv.FormatInt(id, 10)
				return o
			}
			find = "/search?q=" + url.QueryEscape(m.Creator+" "+m.Title)
		}
	case "book":
		if id := lib.books[normTitle(m.Title)]; id > 0 {
			o.Have, o.To = true, "/book/"+strconv.FormatInt(id, 10)
			return o
		}
	}
	o.To = find
	return o
}

// GET /api/home/news: this week's summarised stories, newest first.
func (s *Server) handleNews(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !s.newsOn() {
		writeJSON(w, http.StatusOK, map[string]any{"on": false, "items": []newsItemOut{}})
		return
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, source, category, title, link, published_at, summary, mentions FROM home_news WHERE state = 'done' ORDER BY published_at DESC LIMIT 150`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	lib := s.newsLibrary(ctx)
	items := []newsItemOut{}
	for rows.Next() {
		var it newsItemOut
		var ms string
		if rows.Scan(&it.ID, &it.Source, &it.Category, &it.Title, &it.Link, &it.PublishedAt, &it.Summary, &ms) != nil {
			continue
		}
		var mentions []newsMention
		_ = json.Unmarshal([]byte(ms), &mentions)
		it.Mentions = []newsMentionOut{}
		for _, m := range mentions {
			it.Mentions = append(it.Mentions, lib.place(m))
		}
		items = append(items, it)
	}
	writeJSON(w, http.StatusOK, map[string]any{"on": true, "items": items, "categories": newsCategories})
}

// ---- Settings > Extras ----

// GET /api/home/news/settings (administrators).
func (s *Server) handleGetNewsSettings(w http.ResponseWriter, r *http.Request) {
	var waiting, done int
	_ = s.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM home_news WHERE state = 'new'`).Scan(&waiting)
	_ = s.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM home_news WHERE state = 'done'`).Scan(&done)
	writeJSON(w, http.StatusOK, map[string]any{
		"on": s.newsOn(), "sources": s.newsSources(), "dailyBudget": s.newsBudget(), "categories": newsCategories,
		"summarisedToday": s.newsToday(r.Context()), "waiting": waiting, "stories": done,
	})
}

// PUT /api/home/news/settings {on, sources, dailyBudget}: only what's sent changes.
func (s *Server) handlePutNewsSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		On          *bool         `json:"on"`
		Sources     *[]newsSource `json:"sources"`
		DailyBudget *int          `json:"dailyBudget"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	if req.On != nil {
		v := "1"
		if !*req.On {
			v = "0"
		}
		_ = s.Settings.Set(keyNewsOn, v, false)
	}
	if req.Sources != nil {
		list := []newsSource{}
		valid := map[string]bool{}
		for _, c := range newsCategories {
			valid[c] = true
		}
		for _, src := range *req.Sources {
			src.Name = strings.TrimSpace(src.Name)
			src.URL = strings.TrimSpace(src.URL)
			u, err := url.Parse(src.URL)
			if src.Name == "" || err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				writeError(w, http.StatusBadRequest, "Each news source needs a name and a web address (http or https).")
				return
			}
			if !valid[src.Category] {
				src.Category = "film-tv"
			}
			list = append(list, src)
		}
		sort.SliceStable(list, func(i, j int) bool {
			return indexOf(newsCategories, list[i].Category) < indexOf(newsCategories, list[j].Category)
		})
		b, _ := json.Marshal(list)
		_ = s.Settings.Set(keyNewsSources, string(b), false)
	}
	if req.DailyBudget != nil && *req.DailyBudget >= 0 && *req.DailyBudget <= 500 {
		_ = s.Settings.Set(keyNewsBudget, strconv.Itoa(*req.DailyBudget), false)
	}
	s.handleGetNewsSettings(w, r)
}

func indexOf(list []string, v string) int {
	for i, x := range list {
		if x == v {
			return i
		}
	}
	return len(list)
}

// POST /api/home/news/test {url}: does a feed answer, and with what?
func (s *Server) handleTestNewsSource(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL string `json:"url"`
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.URL) == "" {
		writeError(w, http.StatusBadRequest, "Give the feed's web address.")
		return
	}
	items, err := news.Fetch(r.Context(), strings.TrimSpace(req.URL), "Mozilla/5.0 (compatible; Mediarium-home/"+s.version+")")
	if err != nil {
		writeError(w, http.StatusBadGateway, "That feed couldn't be read: "+err.Error())
		return
	}
	if len(items) == 0 {
		writeError(w, http.StatusBadGateway, "That feed has no stories.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": fmt.Sprintf("Works: %d stories, the newest is “%s”.", len(items), firstN(items[0].Title, 120))})
}

// POST /api/home/news/refresh: read the feeds now (in the background).
func (s *Server) handleRefreshNews(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.aiConfig(); !ok {
		writeError(w, http.StatusServiceUnavailable, "The news summaries need AI. Set it up above first.")
		return
	}
	s.background(func() { s.newsJob(context.Background()) })
	writeJSON(w, http.StatusOK, map[string]string{"message": "Reading the news feeds now. The stories appear on the Dashboard in a minute or two."})
}
