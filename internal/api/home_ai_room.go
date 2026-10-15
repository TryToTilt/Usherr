package api

// Home fork: two more AI features.
//   - Pick for the Room: who's watching, a film or a show, and a mood; the
//     AI picks three titles from what the household has, that nobody in the
//     room has seen, each with a line on why.
//   - Catch Me Up: a spoiler-free recap of a show up to where you are (from
//     the media server, or an episode you choose), written from TMDB's
//     episode summaries.
// The model sees titles, years, genres and episode summaries only.

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand/v2"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/ai"
	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/metadata"
)

// languageName is how the model is told which language to answer in.
func languageName(code string) string {
	switch code {
	case "es":
		return "Spanish"
	case "de":
		return "German"
	case "fr":
		return "French"
	case "pt":
		return "Brazilian Portuguese"
	case "pt-PT":
		return "European Portuguese"
	}
	return "English"
}

// ---- what a person can watch in the media server ----

type roomTitle struct {
	TMDB   int
	Name   string
	Year   int
	Genres []string
	Rating float64
}

type roomShelf struct {
	movies, shows []roomTitle
	at            time.Time
}

var (
	shelfMu    sync.Mutex
	shelfCache = map[string]*roomShelf{}
)

// adultLibrary matches the libraries kept out of every suggestion.
var adultLibrary = regexp.MustCompile(`(?i)(18\+|nsfw|adult|xxx)`)

// shelfOf is every film and show a person can watch in the media server
// (adult libraries left out), kept for an hour.
func (s *Server) shelfOf(ctx context.Context, person string) (*roomShelf, error) {
	shelfMu.Lock()
	if sh, ok := shelfCache[person]; ok && time.Since(sh.at) < time.Hour {
		shelfMu.Unlock()
		return sh, nil
	}
	shelfMu.Unlock()
	srv, ok := s.jellyfinServer()
	if !ok || person == "" {
		return &roomShelf{}, nil
	}
	var views struct {
		Items []struct {
			ID             string `json:"Id"`
			Name           string `json:"Name"`
			CollectionType string `json:"CollectionType"`
		} `json:"Items"`
	}
	if err := jfJSON(ctx, srv, http.MethodGet, "/Users/"+url.PathEscape(person)+"/Views", nil, &views); err != nil {
		return nil, err
	}
	sh := &roomShelf{at: time.Now()}
	seenM, seenS := map[int]bool{}, map[int]bool{}
	for _, v := range views.Items {
		if adultLibrary.MatchString(v.Name) {
			continue
		}
		var typ string
		switch v.CollectionType {
		case "movies":
			typ = "Movie"
		case "tvshows":
			typ = "Series"
		default:
			continue
		}
		var res struct {
			Items []struct {
				jfItem
				Genres          []string `json:"Genres"`
				CommunityRating float64  `json:"CommunityRating"`
				OfficialRating  string   `json:"OfficialRating"`
			} `json:"Items"`
		}
		p := "/Users/" + url.PathEscape(person) + "/Items?Recursive=true&ParentId=" + url.QueryEscape(v.ID) +
			"&IncludeItemTypes=" + typ + "&EnableImages=false&Fields=ProviderIds,Genres"
		if err := jfJSON(ctx, srv, http.MethodGet, p, nil, &res); err != nil {
			continue
		}
		for _, it := range res.Items {
			id := it.tmdbID()
			if id <= 0 || strings.EqualFold(it.OfficialRating, "XXX") {
				continue
			}
			t := roomTitle{TMDB: id, Name: it.Name, Year: it.ProductionYear, Genres: it.Genres, Rating: it.CommunityRating}
			if typ == "Movie" && !seenM[id] {
				seenM[id] = true
				sh.movies = append(sh.movies, t)
			} else if typ == "Series" && !seenS[id] {
				seenS[id] = true
				sh.shows = append(sh.shows, t)
			}
		}
	}
	shelfMu.Lock()
	shelfCache[person] = sh
	shelfMu.Unlock()
	return sh, nil
}

// ---- Pick for the Room ----

// moodGenres are the media server's genre names each mood keeps to.
var moodGenres = map[string][]string{
	"funny":      {"Comedy"},
	"cozy":       {"Family", "Animation", "Romance", "Comedy"},
	"exciting":   {"Action", "Adventure", "Thriller", "Action & Adventure"},
	"scary":      {"Horror"},
	"thoughtful": {"Drama", "Documentary", "Science Fiction", "Sci-Fi & Fantasy", "History"},
	"family":     {"Family", "Animation", "Kids"},
	"mystery":    {"Mystery", "Crime"},
}

func inMood(t roomTitle, want []string) bool {
	for _, g := range t.Genres {
		for _, w := range want {
			if strings.EqualFold(g, w) {
				return true
			}
		}
	}
	return false
}

// roomCandidates is what everyone in the room can watch and nobody has seen,
// in the mood when one is chosen.
func (s *Server) roomCandidates(ctx context.Context, people []string, kind, mood string) ([]roomTitle, error) {
	var common map[int]roomTitle
	seen := map[int]bool{}
	sign := 1
	if kind == "tv" {
		sign = -1
	}
	for _, p := range people {
		sh, err := s.shelfOf(ctx, p)
		if err != nil {
			return nil, err
		}
		list := sh.movies
		if kind == "tv" {
			list = sh.shows
		}
		mine := map[int]roomTitle{}
		for _, t := range list {
			if common == nil || common[t.TMDB].TMDB != 0 {
				mine[t.TMDB] = t
			}
		}
		common = mine
		if wa, err := s.watchingOf(ctx, p); err == nil {
			for id := range wa.seen {
				if id*sign > 0 {
					seen[id*sign] = true
				}
			}
		}
	}
	want := moodGenres[mood]
	var out []roomTitle
	for id, t := range common {
		if seen[id] || (len(want) > 0 && !inMood(t, want)) {
			continue
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TMDB < out[j].TMDB })
	return out, nil
}

// roomSample is up to n candidates for the model to choose from: the better
// rated more often, but never the same handful every time.
func roomSample(list []roomTitle, n int, r *rand.Rand) []roomTitle {
	return roomSampleWeighted(list, n, r, nil)
}

// roomSampleWeighted is roomSample with each title's chances also multiplied
// by weight (home fork: the taste engine's match).
func roomSampleWeighted(list []roomTitle, n int, r *rand.Rand, weight func(roomTitle) float64) []roomTitle {
	if len(list) <= n && weight == nil {
		out := append([]roomTitle(nil), list...)
		r.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
		return out
	}
	type keyed struct {
		t roomTitle
		k float64
	}
	ks := make([]keyed, len(list))
	for i, t := range list {
		w := 1.0 + max(0, t.Rating-5)
		if weight != nil {
			w *= weight(t)
		}
		ks[i] = keyed{t, r.Float64() * w}
	}
	sort.Slice(ks, func(i, j int) bool { return ks[i].k > ks[j].k })
	out := make([]roomTitle, min(n, len(ks)))
	for i := range out {
		out[i] = ks[i].t
	}
	return out
}

const roomSystem = `You pick what a group should watch tonight from a numbered list of titles they have and haven't seen. Reply with a JSON object only, shaped like this:
{"picks":[{"n":12,"title":"...","why":"..."}],"note":""}
- picks: exactly 3 different titles from the list, the best fit first: its number and its title exactly as listed. Pick only from the list.
- why: one short sentence on why it suits this group and this mood. No spoilers.
- note: one short, friendly sentence about the three.`

type roomAnswer struct {
	Picks []struct {
		N     int    `json:"n"`
		Title string `json:"title"`
		Why   string `json:"why"`
	} `json:"picks"`
	Note string `json:"note"`
}

// POST /api/home/ai/room {people, kind, mood, wish, lang, again}: three titles
// for the people in the room.
func (s *Server) handleAIRoom(w http.ResponseWriter, r *http.Request) {
	cfg, ok := s.aiConfig()
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "AI isn't set up. An administrator can set it up under Settings > Extras.")
		return
	}
	var req struct {
		People []string `json:"people"`
		Kind   string   `json:"kind"`
		Mood   string   `json:"mood"`
		Wish   string   `json:"wish"` // anything else, in their words
		Lang   string   `json:"lang"`
		Again  bool     `json:"again"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	if req.Kind != "tv" {
		req.Kind = "movie"
	}
	req.Wish = strings.TrimSpace(firstN(req.Wish, 200))
	ctx := r.Context()
	people, err := s.jfPeople(ctx)
	if err != nil || len(people) == 0 {
		writeError(w, http.StatusServiceUnavailable, "Pick for the Room needs Jellyfin or Emby connected.")
		return
	}
	known := map[string]string{}
	for _, p := range people {
		known[p.ID] = p.Name
	}
	var room []string
	for _, id := range req.People {
		if known[id] != "" && !containsString(room, id) {
			room = append(room, id)
		}
	}
	if len(room) == 0 {
		room = []string{personFor(people, auth.UserFromContext(ctx))}
	}
	sort.Strings(room)
	key := aiCacheKey("room", cfg.Provider, cfg.Model, strings.Join(room, ","), req.Kind, req.Mood, strings.ToLower(req.Wish), req.Lang)
	if !req.Again {
		aiCacheMu.Lock()
		if c, ok := aiCache[key]; ok && time.Since(c.at) < time.Hour {
			aiCacheMu.Unlock()
			writeJSON(w, http.StatusOK, c.data)
			return
		}
		aiCacheMu.Unlock()
	}
	cands, err := s.roomCandidates(ctx, room, req.Kind, req.Mood)
	if err != nil {
		writeUpstreamError(w, "read the library", err)
		return
	}
	// Home fork: the taste engine. Nothing anyone here gave a thumbs down;
	// what fits the room's taste more often, and the model is told the match.
	cands, matches := s.tasteFilter(ctx, req.Kind, cands, s.roomTastes(ctx, room, req.Kind))
	if len(cands) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"note": "Nothing in the library fits that, that nobody here has seen. Try another mood.", "items": []browseItem{}, "why": map[int]string{}})
		return
	}
	if !s.aiAllowed(ctx, auth.UserFromContext(ctx)) {
		writeError(w, http.StatusTooManyRequests, "That's all the AI answers for today. More tomorrow.")
		return
	}
	sample := tasteSample(cands, matches, 150, rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 7)))
	var sb strings.Builder
	what := "films"
	if req.Kind == "tv" {
		what = "TV shows"
	}
	fmt.Fprintf(&sb, "%d people are watching together. They want %s", len(room), what)
	if req.Mood != "" {
		fmt.Fprintf(&sb, ", something %s", req.Mood)
	}
	sb.WriteString(".")
	if req.Wish != "" {
		fmt.Fprintf(&sb, " They also said: %q.", req.Wish)
	}
	if len(matches) > 0 {
		sb.WriteString(" A title's match is how well it fits their taste, from what they liked and watched: prefer good matches.")
	}
	fmt.Fprintf(&sb, " Write the why and the note in %s.\n\nThe titles:\n", languageName(req.Lang))
	for i, t := range sample {
		fmt.Fprintf(&sb, "%d. %s", i+1, t.Name)
		if t.Year > 0 {
			fmt.Fprintf(&sb, " (%d)", t.Year)
		}
		if len(t.Genres) > 0 {
			sb.WriteString(" - " + strings.Join(t.Genres[:min(3, len(t.Genres))], ", "))
		}
		if m := matches[t.TMDB]; m > 0 {
			fmt.Fprintf(&sb, " - match %d%%", m)
		}
		sb.WriteString("\n")
	}
	raw, err := ai.Ask(ctx, cfg, ai.Request{System: roomSystem, Prompt: sb.String(), JSON: true, MaxTokens: 600})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	var ans roomAnswer
	if err := json.Unmarshal([]byte(ai.JSONObject(raw)), &ans); err != nil {
		writeError(w, http.StatusBadGateway, "The AI's answer couldn't be read. Try again.")
		return
	}
	res := s.roomResult(ctx, req.Kind, sample, ans)
	if len(res.Items) == 0 {
		log.Printf("ai: pick for the room matched none of %q", raw)
	} else {
		aiCacheMu.Lock()
		aiCache[key] = aiCached{at: time.Now(), data: res}
		aiCacheMu.Unlock()
	}
	writeJSON(w, http.StatusOK, res)
}

type roomResultPayload struct {
	Note  string         `json:"note"`
	Items []browseItem   `json:"items"`
	Why   map[int]string `json:"why"`
}

// roomResult keeps the picks that are on the list, with TMDB's details.
func (s *Server) roomResult(ctx context.Context, kind string, sample []roomTitle, ans roomAnswer) roomResultPayload {
	res := roomResultPayload{Note: strings.TrimSpace(ans.Note), Items: []browseItem{}, Why: map[int]string{}}
	var ids []int
	for _, p := range ans.Picks {
		id := roomPickID(sample, p.N, p.Title)
		if id == 0 || len(ids) == 3 {
			continue
		}
		if _, dup := res.Why[id]; dup {
			continue
		}
		res.Why[id] = strings.TrimSpace(p.Why)
		ids = append(ids, id)
	}
	tm := s.displayTMDB()
	if kind == "tv" {
		var shows []metadata.Show
		for _, id := range ids {
			if d, err := tm.GetShow(ctx, id); err == nil {
				shows = append(shows, d.Show)
			}
		}
		if len(shows) > 0 {
			res.Items = s.showPagePayload(ctx, &metadata.ShowPage{Page: 1, TotalPages: 1, TotalResults: len(shows), Results: shows}).Results
		}
	} else {
		var movies []metadata.Movie
		for _, id := range ids {
			if m, err := tm.GetMovie(ctx, id); err == nil {
				movies = append(movies, *m)
			}
		}
		if len(movies) > 0 {
			res.Items = s.moviePagePayload(ctx, &metadata.MoviePage{Page: 1, TotalPages: 1, TotalResults: len(movies), Results: movies}).Results
		}
	}
	if len(res.Items) == 0 && res.Note == "" {
		res.Note = "The AI couldn't choose this time. Try again."
	}
	return res
}

// roomPickID is the listed title the model meant: by its name when it
// gave one (small models get numbers wrong more often than names), else by
// its number. 0 when it isn't on the list.
func roomPickID(sample []roomTitle, n int, title string) int {
	// The title as listed may come back with its year and genres.
	if i := strings.Index(title, " - "); i > 0 {
		title = title[:i]
	}
	title = strings.TrimSpace(roomYear.ReplaceAllString(title, ""))
	if want := normTitle(title); want != "" {
		if n >= 1 && n <= len(sample) && normTitle(sample[n-1].Name) == want {
			return sample[n-1].TMDB
		}
		for _, t := range sample {
			if normTitle(t.Name) == want {
				return t.TMDB
			}
		}
	}
	if n >= 1 && n <= len(sample) {
		return sample[n-1].TMDB
	}
	return 0
}

var roomYear = regexp.MustCompile(`\s*\(\d{4}\)\s*$`)

func containsString(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// ---- Catch Me Up ----

type recapSeason struct {
	Season   int `json:"season"`
	Episodes int `json:"episodes"`
}

type recapPayload struct {
	Show        string        `json:"show"`
	Season      int           `json:"season"`
	Episode     int           `json:"episode"`
	EpisodeName string        `json:"episodeName"`
	From        string        `json:"from"` // "watched" (from the media server) or "chosen"
	Recap       string        `json:"recap"`
	Seasons     []recapSeason `json:"seasons"`
}

// lastWatched is the furthest episode of the show a person has finished in
// the media server, or 0, 0.
func (s *Server) lastWatched(ctx context.Context, person string, tmdbID int) (int, int) {
	srv, ok := s.jellyfinServer()
	if !ok || person == "" {
		return 0, 0
	}
	sh, err := s.shelfOf(ctx, person)
	if err != nil {
		return 0, 0
	}
	found := false
	for _, t := range sh.shows {
		if t.TMDB == tmdbID {
			found = true
			break
		}
	}
	if !found {
		return 0, 0
	}
	// The show's id in the media server.
	var series struct {
		Items []jfItem `json:"Items"`
	}
	p := "/Users/" + url.PathEscape(person) + "/Items?Recursive=true&IncludeItemTypes=Series&EnableImages=false&Fields=ProviderIds&HasTmdbId=true"
	if err := jfJSON(ctx, srv, http.MethodGet, p, nil, &series); err != nil {
		return 0, 0
	}
	season, episode := 0, 0
	for _, it := range series.Items {
		if it.tmdbID() != tmdbID {
			continue
		}
		var eps struct {
			Items []struct {
				Season  int `json:"ParentIndexNumber"`
				Episode int `json:"IndexNumber"`
			} `json:"Items"`
		}
		q := "/Users/" + url.PathEscape(person) + "/Items?Recursive=true&ParentId=" + url.QueryEscape(it.ID) +
			"&IncludeItemTypes=Episode&Filters=IsPlayed&SortBy=ParentIndexNumber,IndexNumber&SortOrder=Descending&Limit=5&EnableImages=false"
		if jfJSON(ctx, srv, http.MethodGet, q, nil, &eps) != nil {
			continue
		}
		for _, e := range eps.Items {
			if e.Season > 0 && (e.Season > season || (e.Season == season && e.Episode > episode)) {
				season, episode = e.Season, e.Episode
			}
		}
	}
	return season, episode
}

const recapSystem = `You write spoiler-free "previously on" recaps of TV shows. You're given episode summaries up to the point the viewer has reached. Recap only what happens up to and including that episode: never mention, hint at or foreshadow anything later, even if you know the show. Focus on what matters for the next episode: where the main characters are, what they want, and the open questions. Write about 200 words, in plain prose (a short paragraph or two, then one line starting "Where you left off:"). No headings, no lists, no markdown.`

// recapBudget is about how much of the summaries the model is given; the
// most recent episodes are kept whole when a show is long.
const recapBudget = 40000

// recapPrompt lays out the summaries up to season/episode, the most recent
// first when they have to be cut.
func recapPrompt(show string, overview string, eps []metadata.EpisodeInfo, season, episode int, lang string) string {
	var upto []metadata.EpisodeInfo
	for _, e := range eps {
		if e.Season > 0 && (e.Season < season || (e.Season == season && e.Episode <= episode)) {
			upto = append(upto, e)
		}
	}
	sort.Slice(upto, func(i, j int) bool {
		if upto[i].Season != upto[j].Season {
			return upto[i].Season < upto[j].Season
		}
		return upto[i].Episode < upto[j].Episode
	})
	lines := make([]string, 0, len(upto))
	used, cut := 0, false
	for i := len(upto) - 1; i >= 0; i-- {
		e := upto[i]
		l := "S" + strconv.Itoa(e.Season) + "E" + strconv.Itoa(e.Episode) + " " + e.Name + ": " + strings.TrimSpace(e.Overview)
		if used+len(l) > recapBudget {
			cut = true
			break
		}
		used += len(l)
		lines = append(lines, l)
	}
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Show: %s\n", show)
	if overview != "" {
		fmt.Fprintf(&sb, "About the show: %s\n", overview)
	}
	fmt.Fprintf(&sb, "The viewer has just watched season %d, episode %d. Write the recap in %s.\n\n", season, episode, languageName(lang))
	if cut {
		sb.WriteString("(The earliest episodes are left out here; cover them briefly, only as far as these summaries make clear.)\n")
	}
	sb.WriteString(strings.Join(lines, "\n"))
	return sb.String()
}

// POST /api/home/ai/recap {tmdbId, season, episode, lang}: a recap up to that
// episode, or up to the last one the person watched when none is given.
// Without either, it answers with the seasons to choose from.
func (s *Server) handleAIRecap(w http.ResponseWriter, r *http.Request) {
	cfg, ok := s.aiConfig()
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "AI isn't set up. An administrator can set it up under Settings > Extras.")
		return
	}
	var req struct {
		TMDBID  int    `json:"tmdbId"`
		Season  int    `json:"season"`
		Episode int    `json:"episode"`
		Lang    string `json:"lang"`
	}
	if err := decodeJSON(r, &req); err != nil || req.TMDBID <= 0 {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	ctx := r.Context()
	detail, eps, err := s.displayTMDB().GetShowEpisodes(ctx, req.TMDBID)
	if err != nil {
		writeUpstreamError(w, "load the episodes", err)
		return
	}
	out := recapPayload{Show: detail.Name, Seasons: []recapSeason{}}
	today := time.Now().Format("2006-01-02")
	count := map[int]int{}
	for _, e := range eps {
		if e.Season > 0 && e.AirDate != "" && e.AirDate <= today {
			count[e.Season] = max(count[e.Season], e.Episode)
		}
	}
	for n, c := range count {
		out.Seasons = append(out.Seasons, recapSeason{Season: n, Episodes: c})
	}
	sort.Slice(out.Seasons, func(i, j int) bool { return out.Seasons[i].Season < out.Seasons[j].Season })
	if req.Season > 0 && req.Episode > 0 {
		out.Season, out.Episode, out.From = req.Season, req.Episode, "chosen"
	} else if people, err := s.jfPeople(ctx); err == nil && len(people) > 0 {
		if se, ep := s.lastWatched(ctx, personFor(people, auth.UserFromContext(ctx)), req.TMDBID); se > 0 {
			out.Season, out.Episode, out.From = se, ep, "watched"
		}
	}
	if out.Season == 0 {
		writeJSON(w, http.StatusOK, out) // nothing watched yet: they choose
		return
	}
	for _, e := range eps {
		if e.Season == out.Season && e.Episode == out.Episode {
			out.EpisodeName = e.Name
		}
	}
	key := aiCacheKey("recap", cfg.Provider, cfg.Model, strconv.Itoa(req.TMDBID), strconv.Itoa(out.Season), strconv.Itoa(out.Episode), req.Lang)
	aiCacheMu.Lock()
	if c, ok := aiCache[key]; ok && time.Since(c.at) < 30*24*time.Hour {
		aiCacheMu.Unlock()
		out.Recap = c.data.(string)
		writeJSON(w, http.StatusOK, out)
		return
	}
	aiCacheMu.Unlock()
	if !s.aiAllowed(ctx, auth.UserFromContext(ctx)) {
		writeError(w, http.StatusTooManyRequests, "That's all the AI answers for today. More tomorrow.")
		return
	}
	prompt := recapPrompt(detail.Name, detail.Overview, eps, out.Season, out.Episode, req.Lang)
	text, err := ai.Ask(ctx, cfg, ai.Request{System: recapSystem, Prompt: prompt, MaxTokens: 900})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	out.Recap = strings.TrimSpace(text)
	aiCacheMu.Lock()
	aiCache[key] = aiCached{at: time.Now(), data: out.Recap}
	aiCacheMu.Unlock()
	writeJSON(w, http.StatusOK, out)
}

// ---- Why these suggestions ----

// GET /api/home/discover/why?kind=&who=: for each "Because you watched"
// suggestion, the watched titles that led to it (up to two).
func (s *Server) handleDiscoverWhy(w http.ResponseWriter, r *http.Request) {
	kind, ok := discoverKind(r)
	if !ok {
		writeError(w, http.StatusBadRequest, `kind must be "movie" or "tv"`)
		return
	}
	ctx := r.Context()
	person := s.discoverPerson(ctx, r)
	if _, err := s.watchedSuggestions(ctx, kind, person); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"why": map[int][]string{}})
		return
	}
	whyMu.Lock()
	m := whyCache[person+"|"+kind]
	whyMu.Unlock()
	if m == nil {
		m = map[int][]string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"why": m})
}

var (
	whyMu    sync.Mutex
	whyCache = map[string]map[int][]string{}
)

// rememberWhy keeps, for each suggestion, the names of the (up to two most
// recent) watched titles that suggested it.
func rememberWhy(person, kind string, because map[int][]string) {
	whyMu.Lock()
	whyCache[person+"|"+kind] = because
	whyMu.Unlock()
}
