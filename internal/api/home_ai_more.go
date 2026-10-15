package api

// Home fork: more AI and Discover helpers.
//   - Tonight (Dashboard): three things to watch tonight for the person
//     signed in, from what they're in the middle of, the next episodes of
//     their shows and films they have but haven't seen.
//   - Explain (Activity): a failed download in plain words, and what to try.
//   - Leanings (Discover): the genres the Not interested list keeps
//     dismissing, so Discover can show fewer of them. No AI.
//   - Comic recap: Catch me up for a comic series, up to an issue.

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand/v2"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/ai"
	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/comics"
	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/internal/queue"
)

// ---- Tonight ----

type jfEpisode struct {
	ID         string            `json:"Id"`
	Name       string            `json:"Name"`
	Type       string            `json:"Type"`
	SeriesID   string            `json:"SeriesId"`
	SeriesName string            `json:"SeriesName"`
	Season     int               `json:"ParentIndexNumber"`
	Episode    int               `json:"IndexNumber"`
	Year       int               `json:"ProductionYear"`
	Provider   map[string]string `json:"ProviderIds"`
}

func (e jfEpisode) tmdb() int { return jfItem{ProviderIDs: e.Provider}.tmdbID() }

// tonightChoice is one line of the list the model chooses from.
type tonightChoice struct {
	roomTitle
	Kind    string // movie or tv
	Label   string // "Continue S2E3", "Next: S2E4", "Film you haven't seen"
	Why     string // what the line tells the model
	Episode string // "S02E04 · Name" for shows
}

type tonightItem struct {
	Kind      string `json:"kind"`
	TMDBID    int    `json:"tmdbId"`
	Title     string `json:"title"`
	Year      int    `json:"year,omitempty"`
	PosterURL string `json:"posterUrl,omitempty"`
	Label     string `json:"label"`
	Episode   string `json:"episode,omitempty"`
	Why       string `json:"why"`
	To        string `json:"to"`
}

const tonightSystem = `You suggest what one person should watch tonight from a numbered list: things they're in the middle of, the next episodes of shows they watch, and films and shows they have but haven't seen. Reply with a JSON object only, shaped like this:
{"picks":[{"n":3,"title":"...","why":"..."}],"note":""}
- picks: exactly 3 different lines from the list, the best first: its number and its title exactly as listed. Mix them up (not three of the same kind) unless the list is small. Something they're in the middle of is a good first pick, but not always.
- why: one short, friendly sentence on why tonight. No spoilers.
- note: one short sentence for the three together.`

// tonightChoices gathers what a person could watch tonight.
func (s *Server) tonightChoices(ctx context.Context, person string) ([]tonightChoice, error) {
	srv, ok := s.jellyfinServer()
	if !ok || person == "" {
		return nil, nil
	}
	var out []tonightChoice
	seenShow := map[int]bool{}
	// The shows' TMDB ids, by the media server's id.
	showTMDB := func(ids []string) map[string]int {
		res := map[string]int{}
		if len(ids) == 0 {
			return res
		}
		var r struct {
			Items []jfItem `json:"Items"`
		}
		if jfJSON(ctx, srv, http.MethodGet, "/Users/"+url.PathEscape(person)+"/Items?EnableImages=false&Fields=ProviderIds&Ids="+strings.Join(ids, ","), nil, &r) == nil {
			for _, it := range r.Items {
				res[it.ID] = it.tmdbID()
			}
		}
		return res
	}
	epCode := func(e jfEpisode) string {
		c := fmt.Sprintf("S%02dE%02d", e.Season, e.Episode)
		if e.Name != "" {
			c += " · " + e.Name
		}
		return c
	}
	// In the middle of.
	var resume struct {
		Items []jfEpisode `json:"Items"`
	}
	_ = jfJSON(ctx, srv, http.MethodGet, "/Users/"+url.PathEscape(person)+"/Items/Resume?Limit=8&MediaTypes=Video&EnableImages=false&Fields=ProviderIds", nil, &resume)
	// Next episodes.
	var next struct {
		Items []jfEpisode `json:"Items"`
	}
	_ = jfJSON(ctx, srv, http.MethodGet, "/Shows/NextUp?UserId="+url.QueryEscape(person)+"&Limit=12&EnableImages=false&Fields=ProviderIds", nil, &next)
	var sids []string
	for _, e := range append(append([]jfEpisode{}, resume.Items...), next.Items...) {
		if e.SeriesID != "" {
			sids = append(sids, e.SeriesID)
		}
	}
	tm := showTMDB(sids)
	for _, e := range resume.Items {
		switch e.Type {
		case "Movie":
			if id := e.tmdb(); id > 0 {
				out = append(out, tonightChoice{roomTitle: roomTitle{TMDB: id, Name: e.Name, Year: e.Year}, Kind: "movie", Label: "Continue", Why: "film they started"})
			}
		case "Episode":
			if id := tm[e.SeriesID]; id > 0 && !seenShow[id] {
				seenShow[id] = true
				out = append(out, tonightChoice{roomTitle: roomTitle{TMDB: id, Name: e.SeriesName}, Kind: "tv", Label: "Continue", Why: "episode they started, " + fmt.Sprintf("S%dE%d", e.Season, e.Episode), Episode: epCode(e)})
			}
		}
	}
	for _, e := range next.Items {
		if id := tm[e.SeriesID]; id > 0 && !seenShow[id] {
			seenShow[id] = true
			out = append(out, tonightChoice{roomTitle: roomTitle{TMDB: id, Name: e.SeriesName}, Kind: "tv", Label: "Next", Why: "next episode of a show they watch, " + fmt.Sprintf("S%dE%d", e.Season, e.Episode), Episode: epCode(e)})
		}
	}
	// Films and shows they have but haven't seen.
	sh, err := s.shelfOf(ctx, person)
	if err != nil {
		return out, nil
	}
	wa, _ := s.watchingOf(ctx, person)
	r := rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 11))
	var films, shows []roomTitle
	for _, t := range sh.movies {
		if wa == nil || !wa.seen[t.TMDB] {
			films = append(films, t)
		}
	}
	for _, t := range sh.shows {
		if (wa == nil || !wa.seen[-t.TMDB]) && !seenShow[t.TMDB] {
			shows = append(shows, t)
		}
	}
	genres := func(t roomTitle) string {
		if len(t.Genres) == 0 {
			return ""
		}
		return ", " + strings.Join(t.Genres[:min(3, len(t.Genres))], "/")
	}
	// Home fork: the taste engine. Not what they gave a thumbs down; what
	// fits their taste more often, and the model is told the match.
	fits := func(m int) string {
		if m == 0 {
			return ""
		}
		return fmt.Sprintf(", match %d%%", m)
	}
	films, fm := s.tasteFilter(ctx, "movie", films, []*tasteProfile{s.myTaste(ctx, "movie")})
	shows, sm := s.tasteFilter(ctx, "tv", shows, []*tasteProfile{s.myTaste(ctx, "tv")})
	for _, t := range tasteSample(films, fm, 45, r) {
		out = append(out, tonightChoice{roomTitle: t, Kind: "movie", Label: "Unwatched", Why: "film they have and haven't seen" + genres(t) + fits(fm[t.TMDB])})
	}
	for _, t := range tasteSample(shows, sm, 15, r) {
		out = append(out, tonightChoice{roomTitle: t, Kind: "tv", Label: "New show", Why: "show they have and haven't started" + genres(t) + fits(sm[t.TMDB])})
	}
	return out, nil
}

// POST /api/home/ai/tonight {lang, again}: three picks for tonight.
func (s *Server) handleAITonight(w http.ResponseWriter, r *http.Request) {
	cfg, ok := s.aiConfig()
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "AI isn't set up. An administrator can set it up under Settings > Extras.")
		return
	}
	var req struct {
		Lang  string `json:"lang"`
		Again bool   `json:"again"`
	}
	_ = decodeJSON(r, &req)
	ctx := r.Context()
	u := auth.UserFromContext(ctx)
	people, err := s.jfPeople(ctx)
	if err != nil || len(people) == 0 {
		writeError(w, http.StatusServiceUnavailable, "Tonight needs Jellyfin or Emby connected.")
		return
	}
	person := personFor(people, u)
	now := time.Now()
	key := aiCacheKey("tonight", cfg.Provider, cfg.Model, person, req.Lang, now.Format("2006-01-02"))
	if !req.Again {
		aiCacheMu.Lock()
		if c, ok := aiCache[key]; ok && time.Since(c.at) < 3*time.Hour {
			aiCacheMu.Unlock()
			writeJSON(w, http.StatusOK, c.data)
			return
		}
		aiCacheMu.Unlock()
	}
	choices, err := s.tonightChoices(ctx, person)
	if err != nil {
		writeUpstreamError(w, "read what you're watching", err)
		return
	}
	if len(choices) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"note": "Nothing to suggest yet: watch something in Jellyfin first.", "items": []tonightItem{}})
		return
	}
	if !s.aiAllowed(ctx, u) {
		writeError(w, http.StatusTooManyRequests, "That's all the AI answers for today. More tomorrow.")
		return
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "It's %s evening.", now.Weekday())
	if line := s.myTaste(ctx, "movie").tasteLine(); line != "" {
		sb.WriteString(" " + line + " A match is how well a title fits their taste.") // home fork: the taste engine
	}
	fmt.Fprintf(&sb, " Write the why and the note in %s.\n\nThe list:\n", languageName(req.Lang))
	sample := make([]roomTitle, len(choices))
	for i, c := range choices {
		sample[i] = c.roomTitle
		fmt.Fprintf(&sb, "%d. %s", i+1, c.Name)
		if c.Year > 0 {
			fmt.Fprintf(&sb, " (%d)", c.Year)
		}
		fmt.Fprintf(&sb, " - %s\n", c.Why)
	}
	raw, err := ai.Ask(ctx, cfg, ai.Request{System: tonightSystem, Prompt: sb.String(), JSON: true, MaxTokens: 600})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	var ans roomAnswer
	if err := json.Unmarshal([]byte(ai.JSONObject(raw)), &ans); err != nil {
		writeError(w, http.StatusBadGateway, "The AI's answer couldn't be read. Try again.")
		return
	}
	items := s.tonightItems(ctx, choices, sample, ans)
	if len(items) < 3 {
		log.Printf("ai: tonight kept %d of the picks in %q", len(items), firstN(raw, 600))
	}
	res := map[string]any{"note": strings.TrimSpace(ans.Note), "items": items}
	if len(items) > 0 {
		aiCacheMu.Lock()
		aiCache[key] = aiCached{at: time.Now(), data: res}
		aiCacheMu.Unlock()
	}
	writeJSON(w, http.StatusOK, res)
}

// tonightItems turns the model's picks into cards, with posters and where
// each one opens.
func (s *Server) tonightItems(ctx context.Context, choices []tonightChoice, sample []roomTitle, ans roomAnswer) []tonightItem {
	libShow := map[int]int64{}
	if list, err := s.MovieRepo.ListSeries(); err == nil {
		for _, sr := range list {
			if sr.CopyOf == 0 {
				libShow[sr.TMDBID] = sr.ID
			}
		}
	}
	items := []tonightItem{}
	used := map[int]bool{}
	for _, p := range ans.Picks {
		if len(items) == 3 {
			break
		}
		id := roomPickID(sample, p.N, p.Title)
		if id == 0 {
			continue
		}
		// The line: by number when it is that title, else the first with it.
		idx := -1
		if p.N >= 1 && p.N <= len(choices) && choices[p.N-1].TMDB == id {
			idx = p.N - 1
		} else {
			for i, c := range choices {
				if c.TMDB == id {
					idx = i
					break
				}
			}
		}
		if idx < 0 || used[idx] {
			continue
		}
		used[idx] = true
		it := s.tonightCard(ctx, choices[idx], strings.TrimSpace(p.Why), libShow)
		items = append(items, it)
	}
	// A model that picked fewer than three: the rest from the top of the
	// list (what they're in the middle of first), without a why.
	for i := 0; len(items) < 3 && i < len(choices); i++ {
		dup := used[i]
		for _, it := range items {
			dup = dup || (it.TMDBID == choices[i].TMDB && it.Kind == choices[i].Kind)
		}
		if !dup {
			used[i] = true
			items = append(items, s.tonightCard(ctx, choices[i], "", libShow))
		}
	}
	return items
}

// tonightCard is one card: its poster and where it opens.
func (s *Server) tonightCard(ctx context.Context, c tonightChoice, why string, libShow map[int]int64) tonightItem {
	tm := s.displayTMDB()
	it := tonightItem{Kind: c.Kind, TMDBID: c.TMDB, Title: c.Name, Year: c.Year, Label: c.Label, Episode: c.Episode, Why: why}
	if c.Kind == "movie" {
		it.To = "/title/" + strconv.Itoa(c.TMDB)
		if m, err := tm.GetMovie(ctx, c.TMDB); err == nil {
			it.PosterURL = metadata.PosterURL(m.PosterPath)
			if it.Year == 0 {
				it.Year = m.Year()
			}
		}
		return it
	}
	if lid := libShow[c.TMDB]; lid > 0 {
		it.To = "/series/" + strconv.FormatInt(lid, 10)
	} else {
		it.To = "/show/" + strconv.Itoa(c.TMDB)
	}
	if d, err := tm.GetShow(ctx, c.TMDB); err == nil {
		it.PosterURL = metadata.PosterURL(d.PosterPath)
		if it.Year == 0 {
			it.Year = d.Year()
		}
	}
	return it
}

// ---- Explain a failed download ----

const explainSystem = `You help someone understand why a download failed in Mediarium, a home media manager that downloads from Usenet or torrents and then repairs, unpacks and imports the file into the library. You're given the release name, the error and the item's recent log. Explain in plain words, for someone who isn't technical, in 2 to 4 short sentences: what went wrong, the most likely reason, and what to try. The options they have are: Retry (the same release), pick another release (the title page's search lists them), or Remove. Mediarium usually blocks a failed release from being picked again by itself. Common causes: the release was taken down or is incomplete on the Usenet servers (pick another release, often a newer one); a password-protected or fake release; a release in another language or the wrong episode; the disk is full; the indexer refused the download (limits for the day); for a torrent, nobody is sharing it any more. Don't invent details that aren't in the log. No lists, no markdown.`

// POST /api/home/ai/explain {id, lang}: a failed download in plain words.
func (s *Server) handleAIExplain(w http.ResponseWriter, r *http.Request) {
	cfg, ok := s.aiConfig()
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "AI isn't set up. An administrator can set it up under Settings > Extras.")
		return
	}
	var req struct {
		ID   int64  `json:"id"`
		Lang string `json:"lang"`
	}
	if err := decodeJSON(r, &req); err != nil || req.ID <= 0 {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	it, err := s.QueueRepo.Get(req.ID)
	if err != nil {
		writeError(w, http.StatusNotFound, "That download isn't in Activity any more.")
		return
	}
	if it.Status != queue.StatusFailed && it.Status != queue.StatusStopped {
		writeError(w, http.StatusBadRequest, "Only failed or stopped downloads can be explained.")
		return
	}
	key := aiCacheKey("explain", cfg.Provider, cfg.Model, strconv.FormatInt(it.ID, 10), it.Error, req.Lang)
	aiCacheMu.Lock()
	if c, ok := aiCache[key]; ok && time.Since(c.at) < 7*24*time.Hour {
		aiCacheMu.Unlock()
		writeJSON(w, http.StatusOK, map[string]string{"text": c.data.(string)})
		return
	}
	aiCacheMu.Unlock()
	ctx := r.Context()
	if !s.aiAllowed(ctx, auth.UserFromContext(ctx)) {
		writeError(w, http.StatusTooManyRequests, "That's all the AI answers for today. More tomorrow.")
		return
	}
	var events []queue.Event
	switch {
	case it.AlbumID > 0:
		events, _ = s.QueueRepo.AlbumEvents(it.AlbumID, 15)
	case it.SeriesID > 0:
		events, _ = s.QueueRepo.SeriesEvents(it.SeriesID, 15)
	case it.MovieID > 0:
		events, _ = s.QueueRepo.MovieEvents(it.MovieID, 15)
	}
	var sb strings.Builder
	what := "a comic or other item"
	switch {
	case it.MovieID > 0:
		what = "a film"
	case it.SeriesID > 0:
		what = "TV episodes"
	case it.AlbumID > 0:
		what = "a music album"
	case it.BookID > 0:
		what = "a book (" + it.BookFormat + ")"
	}
	via := "Usenet"
	if it.Protocol == queue.ProtocolTorrent {
		via = "a torrent"
	}
	fmt.Fprintf(&sb, "What it is: %s, downloaded from %s\nRelease: %s\nStatus: %s\nError: %s\n", what, via, it.ReleaseTitle, it.Status, firstN(it.Error, 600))
	if it.SizeBytes > 0 {
		fmt.Fprintf(&sb, "Size: %.1f GB\n", float64(it.SizeBytes)/1e9)
	}
	if len(events) > 0 {
		sb.WriteString("\nRecent log for this title, newest first:\n")
		for _, e := range events {
			fmt.Fprintf(&sb, "- %s [%s] %s\n", e.At, e.Level, firstN(e.Message, 300))
		}
	}
	fmt.Fprintf(&sb, "\nAnswer in %s.", languageName(req.Lang))
	text, err := ai.Ask(ctx, cfg, ai.Request{System: explainSystem, Prompt: sb.String(), MaxTokens: 400})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	text = strings.TrimSpace(text)
	aiCacheMu.Lock()
	aiCache[key] = aiCached{at: time.Now(), data: text}
	aiCacheMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]string{"text": text})
}

// ---- Leanings: what the Not interested list says ----

var (
	genreMu    sync.Mutex
	genreCache = map[string][]string{} // "movie:603" -> genre names
)

// titleGenres is a title's TMDB genre names, kept once read.
func (s *Server) titleGenres(ctx context.Context, kind string, id int) []string {
	k := kind + ":" + strconv.Itoa(id)
	genreMu.Lock()
	g, ok := genreCache[k]
	genreMu.Unlock()
	if ok {
		return g
	}
	tm := s.displayTMDB()
	var names []string
	if kind == "tv" {
		if d, err := tm.GetShow(ctx, id); err == nil {
			for _, x := range d.Genres {
				names = append(names, x.Name)
			}
		} else {
			return nil
		}
	} else {
		if m, err := tm.GetMovie(ctx, id); err == nil {
			for _, x := range m.Genres {
				names = append(names, x.Name)
			}
		} else {
			return nil
		}
	}
	genreMu.Lock()
	genreCache[k] = names
	genreMu.Unlock()
	return names
}

type leaning struct {
	Genre string `json:"genre"`
	Count int    `json:"count"`
}

// leanAway picks the genres dismissed often: at least 3 times and in at
// least a quarter of all the dismissals.
func leanAway(counts map[string]int, total int) []leaning {
	var out []leaning
	for g, n := range counts {
		if n >= 3 && float64(n) >= 0.25*float64(total) {
			out = append(out, leaning{g, n})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Genre < out[j].Genre
	})
	return out
}

// GET /api/home/discover/leanings: the genres the Not interested list
// dismisses most, for Discover to show fewer of.
func (s *Server) handleDiscoverLeanings(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT kind, tmdb_id FROM exclusions ORDER BY created_at DESC LIMIT 300`)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"genres": []leaning{}, "total": 0})
		return
	}
	type ex struct {
		kind string
		id   int
	}
	var list []ex
	for rows.Next() {
		var e ex
		if rows.Scan(&e.kind, &e.id) == nil {
			list = append(list, e)
		}
	}
	rows.Close()
	counts := map[string]int{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for _, e := range list {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			g := s.titleGenres(r.Context(), e.kind, e.id)
			mu.Lock()
			for _, name := range g {
				counts[name]++
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	out := leanAway(counts, len(list))
	if out == nil {
		out = []leaning{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"genres": out, "total": len(list)})
}

// ---- Catch me up for comics ----

var (
	comicSumMu    sync.Mutex
	comicSumCache = map[int64]comicSums{}
)

type comicSums struct {
	at   time.Time
	list []comics.IssueSummary
}

// issueSummaries is a series' issue summaries from ComicVine, kept a week.
func (s *Server) issueSummaries(ctx context.Context, cvID int64) ([]comics.IssueSummary, error) {
	comicSumMu.Lock()
	if c, ok := comicSumCache[cvID]; ok && time.Since(c.at) < 7*24*time.Hour {
		comicSumMu.Unlock()
		return c.list, nil
	}
	comicSumMu.Unlock()
	list, err := s.comicVine().IssueSummaries(ctx, cvID)
	if err != nil {
		return nil, err
	}
	comicSumMu.Lock()
	comicSumCache[cvID] = comicSums{time.Now(), list}
	comicSumMu.Unlock()
	return list, nil
}

// issueOrder is an issue number as a number ("12.1" -> 12.1, "½" -> 0.5);
// -1 when it isn't one.
func issueOrder(n string) float64 {
	n = strings.TrimSpace(n)
	if n == "½" {
		return 0.5
	}
	f, err := strconv.ParseFloat(n, 64)
	if err != nil {
		return -1
	}
	return f
}

const comicRecapSystem = `You write spoiler-free "previously in" recaps of comic book series. You're given the series and what happens in its issues up to the one the reader has just finished (some issues may have no summary). Recap only up to and including that issue: never mention, hint at or foreshadow anything later, even if you know the comic. Where an issue has no summary, don't make one up; you may say briefly that the details of those issues aren't covered. Focus on what matters for the next issue: where the main characters are, what they want, and the open questions. About 200 words, plain prose (a short paragraph or two, then one line starting "Where you left off:"). No headings, no lists, no markdown.`

// POST /api/home/ai/comic-recap {seriesId, upTo, lang}: a recap of a comic
// series up to issue upTo (its number). Without upTo, the issues to choose
// from.
func (s *Server) handleAIComicRecap(w http.ResponseWriter, r *http.Request) {
	cfg, ok := s.aiConfig()
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "AI isn't set up. An administrator can set it up under Settings > Extras.")
		return
	}
	var req struct {
		SeriesID int64  `json:"seriesId"`
		UpTo     string `json:"upTo"`
		Lang     string `json:"lang"`
	}
	if err := decodeJSON(r, &req); err != nil || req.SeriesID <= 0 {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	ctx := r.Context()
	sr, err := s.comicRepo().GetSeries(req.SeriesID)
	if err != nil {
		writeError(w, http.StatusNotFound, "That comic series isn't in your library.")
		return
	}
	sums, err := s.issueSummaries(ctx, sr.CVID)
	if err != nil {
		writeUpstreamError(w, "read the issues from ComicVine", err)
		return
	}
	sort.SliceStable(sums, func(i, j int) bool { return issueOrder(sums[i].Number) < issueOrder(sums[j].Number) })
	numbers := []string{}
	for _, x := range sums {
		if issueOrder(x.Number) >= 0 {
			numbers = append(numbers, x.Number)
		}
	}
	out := map[string]any{"series": sr.Name, "issues": numbers, "upTo": req.UpTo, "recap": ""}
	upTo := issueOrder(req.UpTo)
	if upTo < 0 {
		writeJSON(w, http.StatusOK, out)
		return
	}
	// Only from what ComicVine says: an issue it doesn't list, or no
	// summaries at all up to it, would leave the AI guessing.
	known, told := false, 0
	for _, x := range sums {
		if o := issueOrder(x.Number); o >= 0 && o <= upTo {
			if o == upTo {
				known = true
			}
			if x.Summary != "" {
				told++
			}
		}
	}
	if !known {
		writeError(w, http.StatusBadRequest, "ComicVine doesn't list that issue for this series. Choose one from the list.")
		return
	}
	if told == 0 {
		writeError(w, http.StatusUnprocessableEntity, "ComicVine has no summaries for these issues yet, so a recap would only be guesswork.")
		return
	}
	key := aiCacheKey("comic-recap", cfg.Provider, cfg.Model, strconv.FormatInt(sr.CVID, 10), req.UpTo, req.Lang)
	aiCacheMu.Lock()
	if c, ok := aiCache[key]; ok && time.Since(c.at) < 30*24*time.Hour {
		aiCacheMu.Unlock()
		out["recap"] = c.data.(string)
		writeJSON(w, http.StatusOK, out)
		return
	}
	aiCacheMu.Unlock()
	if !s.aiAllowed(ctx, auth.UserFromContext(ctx)) {
		writeError(w, http.StatusTooManyRequests, "That's all the AI answers for today. More tomorrow.")
		return
	}
	var lines []string
	used := 0
	for i := len(sums) - 1; i >= 0; i-- {
		x := sums[i]
		o := issueOrder(x.Number)
		if o < 0 || o > upTo {
			continue
		}
		l := "#" + x.Number
		if x.Name != "" {
			l += " " + x.Name
		}
		if x.Summary != "" {
			l += ": " + x.Summary
		} else {
			l += ": (no summary)"
		}
		if used+len(l) > recapBudget {
			break
		}
		used += len(l)
		lines = append(lines, l)
	}
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Series: %s", sr.Name)
	if sr.StartYear > 0 {
		fmt.Fprintf(&sb, " (%d)", sr.StartYear)
	}
	if sr.Publisher != "" {
		fmt.Fprintf(&sb, ", %s", sr.Publisher)
	}
	if sr.Description != "" {
		fmt.Fprintf(&sb, "\nAbout the series: %s", firstN(sr.Description, 800))
	}
	fmt.Fprintf(&sb, "\nThe reader has just finished issue #%s. Write the recap in %s.\n\n%s", req.UpTo, languageName(req.Lang), strings.Join(lines, "\n"))
	text, err := ai.Ask(ctx, cfg, ai.Request{System: comicRecapSystem, Prompt: sb.String(), MaxTokens: 900})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	text = strings.TrimSpace(text)
	aiCacheMu.Lock()
	aiCache[key] = aiCached{at: time.Now(), data: text}
	aiCacheMu.Unlock()
	out["recap"] = text
	writeJSON(w, http.StatusOK, out)
}
