package api

// Home fork: AI guides.
//   - What's it like? A spoiler-free feel for a film or show: tone, pace,
//     how intense, who it's for, what it's like.
//   - Skip guide: which episodes of a show matter and which can be skipped
//     (filler and stand-alones), from TMDB's episode list.
//   - Comic reading guide: reading orders and where to start ("what do I
//     read before Secret Wars?"), linked to the comics you have.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/ai"
	"github.com/rdborg/mediarium/internal/auth"
)

const vibeSystem = `You tell people what a film or TV show is like before they watch it, without spoilers. Reply with a JSON object only:
{"feel":"...","pace":"...","intensity":"...","forWho":"...","like":["Title (year)","Title (year)"]}
- feel: two or three sentences on the tone and style (no plot beyond the premise).
- pace: a few words ("slow burn", "brisk", "episodic").
- intensity: a few words on violence, scares, language or sad themes, plainly ("mild", "some gory scenes", "strong language").
- forWho: one sentence on who will love it.
- like: up to 4 similar films or shows.
All in the language asked for.`

// POST /api/home/ai/vibe {kind, tmdbId, lang}
func (s *Server) handleAIVibe(w http.ResponseWriter, r *http.Request) {
	cfg, ok := s.aiConfig()
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "AI isn't set up. An administrator can set it up under Settings > Extras.")
		return
	}
	var req struct {
		Kind   string `json:"kind"`
		TMDBID int    `json:"tmdbId"`
		Lang   string `json:"lang"`
	}
	if err := decodeJSON(r, &req); err != nil || req.TMDBID <= 0 || (req.Kind != "movie" && req.Kind != "tv") {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	ctx := r.Context()
	key := aiCacheKey("vibe", cfg.Provider, cfg.Model, req.Kind, strconv.Itoa(req.TMDBID), req.Lang)
	aiCacheMu.Lock()
	if c, ok := aiCache[key]; ok && time.Since(c.at) < 30*24*time.Hour {
		aiCacheMu.Unlock()
		writeJSON(w, http.StatusOK, c.data)
		return
	}
	aiCacheMu.Unlock()
	tm := s.displayTMDB()
	var title, overview string
	var year int
	var genres []string
	if req.Kind == "movie" {
		m, err := tm.GetMovie(ctx, req.TMDBID)
		if err != nil {
			writeUpstreamError(w, "load the film", err)
			return
		}
		title, overview, year = m.Title, m.Overview, m.Year()
		for _, g := range m.Genres {
			genres = append(genres, g.Name)
		}
	} else {
		d, err := tm.GetShow(ctx, req.TMDBID)
		if err != nil {
			writeUpstreamError(w, "load the show", err)
			return
		}
		title, overview, year = d.Name, d.Overview, d.Year()
		for _, g := range d.Genres {
			genres = append(genres, g.Name)
		}
	}
	if !s.aiAllowed(ctx, auth.UserFromContext(ctx)) {
		writeError(w, http.StatusTooManyRequests, "That's all the AI answers for today. More tomorrow.")
		return
	}
	what := "Film"
	if req.Kind == "tv" {
		what = "TV show"
	}
	prompt := fmt.Sprintf("%s: %s (%d)\nGenres: %s\nPremise: %s\n\nAnswer in %s.", what, title, year, strings.Join(genres, ", "), firstN(overview, 1200), languageName(req.Lang))
	raw, err := ai.Ask(ctx, cfg, ai.Request{System: vibeSystem, Prompt: prompt, JSON: true, MaxTokens: 600})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	var ans struct {
		Feel      string   `json:"feel"`
		Pace      string   `json:"pace"`
		Intensity string   `json:"intensity"`
		ForWho    string   `json:"forWho"`
		Like      []string `json:"like"`
	}
	if err := json.Unmarshal([]byte(ai.JSONObject(raw)), &ans); err != nil {
		writeError(w, http.StatusBadGateway, "The AI's answer couldn't be read. Try again.")
		return
	}
	if len(ans.Like) > 4 {
		ans.Like = ans.Like[:4]
	}
	type likeOut struct {
		Title string `json:"title"`
		To    string `json:"to"`
	}
	likes := []likeOut{}
	for _, l := range ans.Like {
		if l = strings.TrimSpace(l); l != "" {
			name := l
			if i := strings.LastIndex(name, " ("); i > 0 {
				name = name[:i]
			}
			likes = append(likes, likeOut{l, "/search?q=" + url.QueryEscape(name)})
		}
	}
	res := map[string]any{"feel": strings.TrimSpace(ans.Feel), "pace": strings.TrimSpace(ans.Pace), "intensity": strings.TrimSpace(ans.Intensity), "forWho": strings.TrimSpace(ans.ForWho), "like": likes}
	aiCacheMu.Lock()
	aiCache[key] = aiCached{at: time.Now(), data: res}
	aiCacheMu.Unlock()
	writeJSON(w, http.StatusOK, res)
}

const skipSystem = `You make episode guides that say which episodes of a TV show matter and which can be skipped. You're given the show and its episodes with TMDB's summaries. Reply with a JSON object only:
{"seasons":[{"season":1,"must":[{"episode":1,"why":"..."}],"skip":[{"episode":5,"why":"..."}]}],"note":""}
- must: the episodes the main story needs (premieres, finales, big turns).
- skip: episodes that can be skipped without missing the main story: filler, recaps, clip shows, stand-alone side stories. For anime, use what is known about its filler episodes.
- Leave out episodes that are neither (they're worth watching but not essential). Leave out seasons with nothing to say.
- why: a few words, spoiler-free (say "filler", "recap episode", "stand-alone story", "season finale" rather than what happens).
- note: one short sentence about the show overall (for example "Nearly every episode matters").
All in the language asked for.`

// POST /api/home/ai/skip-guide {tmdbId, lang}
func (s *Server) handleAISkipGuide(w http.ResponseWriter, r *http.Request) {
	cfg, ok := s.aiConfig()
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "AI isn't set up. An administrator can set it up under Settings > Extras.")
		return
	}
	var req struct {
		TMDBID int    `json:"tmdbId"`
		Lang   string `json:"lang"`
	}
	if err := decodeJSON(r, &req); err != nil || req.TMDBID <= 0 {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	ctx := r.Context()
	key := aiCacheKey("skip", cfg.Provider, cfg.Model, strconv.Itoa(req.TMDBID), req.Lang)
	aiCacheMu.Lock()
	if c, ok := aiCache[key]; ok && time.Since(c.at) < 30*24*time.Hour {
		aiCacheMu.Unlock()
		writeJSON(w, http.StatusOK, c.data)
		return
	}
	aiCacheMu.Unlock()
	detail, eps, err := s.displayTMDB().GetShowEpisodes(ctx, req.TMDBID)
	if err != nil {
		writeUpstreamError(w, "load the episodes", err)
		return
	}
	if !s.aiAllowed(ctx, auth.UserFromContext(ctx)) {
		writeError(w, http.StatusTooManyRequests, "That's all the AI answers for today. More tomorrow.")
		return
	}
	today := time.Now().Format("2006-01-02")
	var aired int
	for _, e := range eps {
		if e.Season > 0 && e.AirDate != "" && e.AirDate <= today {
			aired++
		}
	}
	// Long shows get shorter summaries (and very long ones names only).
	per := 220
	switch {
	case aired > 400:
		per = 0
	case aired > 150:
		per = 90
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Show: %s (%d)\nAbout: %s\nAnswer in %s.\n\nEpisodes:\n", detail.Name, detail.Year(), firstN(detail.Overview, 600), languageName(req.Lang))
	for _, e := range eps {
		if e.Season <= 0 || e.AirDate == "" || e.AirDate > today {
			continue
		}
		fmt.Fprintf(&sb, "S%dE%d %s", e.Season, e.Episode, e.Name)
		if per > 0 && e.Overview != "" {
			fmt.Fprintf(&sb, ": %s", firstN(e.Overview, per))
		}
		sb.WriteString("\n")
	}
	raw, err := ai.Ask(ctx, cfg, ai.Request{System: skipSystem, Prompt: sb.String(), JSON: true, MaxTokens: 4000})
	if err != nil {
		writeError(w, http.StatusBadGateway, "The AI couldn't make a guide for this show ("+err.Error()+"). A small local model may struggle with long shows; Claude handles them.")
		return
	}
	type pick struct {
		Episode int    `json:"episode"`
		Why     string `json:"why"`
		Name    string `json:"name,omitempty"`
	}
	var ans struct {
		Seasons []struct {
			Season int    `json:"season"`
			Must   []pick `json:"must"`
			Skip   []pick `json:"skip"`
		} `json:"seasons"`
		Note string `json:"note"`
	}
	if err := json.Unmarshal([]byte(ai.JSONObject(raw)), &ans); err != nil {
		writeError(w, http.StatusBadGateway, "The AI's answer couldn't be read. Try again.")
		return
	}
	names := map[[2]int]string{}
	for _, e := range eps {
		names[[2]int{e.Season, e.Episode}] = e.Name
	}
	// Only episodes that exist, with their names.
	keep := func(season int, list []pick) []pick {
		out := []pick{}
		for _, p := range list {
			if n, ok := names[[2]int{season, p.Episode}]; ok {
				p.Name = n
				p.Why = strings.TrimSpace(p.Why)
				out = append(out, p)
			}
		}
		return out
	}
	type seasonOut struct {
		Season int    `json:"season"`
		Must   []pick `json:"must"`
		Skip   []pick `json:"skip"`
	}
	seasons := []seasonOut{}
	for _, se := range ans.Seasons {
		m, sk := keep(se.Season, se.Must), keep(se.Season, se.Skip)
		if len(m)+len(sk) > 0 {
			seasons = append(seasons, seasonOut{se.Season, m, sk})
		}
	}
	res := map[string]any{"show": detail.Name, "note": strings.TrimSpace(ans.Note), "seasons": seasons}
	aiCacheMu.Lock()
	aiCache[key] = aiCached{at: time.Now(), data: res}
	aiCacheMu.Unlock()
	writeJSON(w, http.StatusOK, res)
}

const comicGuideSystem = `You are a comics guide. People ask about reading orders ("what do I read before Secret Wars?") and where to start with a character, team or creator. Reply with a JSON object only:
{"steps":[{"series":"Daredevil","volume":2015,"issues":"#1-28","why":"..."}],"note":""}
- steps: up to 12 entries in reading order. series is the series' name as published; volume is the year that run started (to tell runs apart), 0 if not sure; issues is the range, or "" for the whole run.
- why: one short sentence each, spoiler-free. note: one or two sentences of advice.
Use real series and issue numbers only. All prose in the language asked for.`

// POST /api/home/ai/comic-guide {question, lang}
func (s *Server) handleAIComicGuide(w http.ResponseWriter, r *http.Request) {
	cfg, ok := s.aiConfig()
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "AI isn't set up. An administrator can set it up under Settings > Extras.")
		return
	}
	var req struct {
		Question string `json:"question"`
		Lang     string `json:"lang"`
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Question) == "" {
		writeError(w, http.StatusBadRequest, "Ask about a reading order or where to start.")
		return
	}
	req.Question = strings.TrimSpace(firstN(req.Question, 300))
	ctx := r.Context()
	key := aiCacheKey("comic-guide", cfg.Provider, cfg.Model, strings.ToLower(req.Question), req.Lang)
	aiCacheMu.Lock()
	if c, ok := aiCache[key]; ok && time.Since(c.at) < 7*24*time.Hour {
		aiCacheMu.Unlock()
		writeJSON(w, http.StatusOK, c.data)
		return
	}
	aiCacheMu.Unlock()
	if !s.aiAllowed(ctx, auth.UserFromContext(ctx)) {
		writeError(w, http.StatusTooManyRequests, "That's all the AI answers for today. More tomorrow.")
		return
	}
	raw, err := ai.Ask(ctx, cfg, ai.Request{System: comicGuideSystem, Prompt: req.Question + "\n\nAnswer in " + languageName(req.Lang) + ".", JSON: true, MaxTokens: 2500})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	var ans struct {
		Steps []struct {
			Series string `json:"series"`
			Volume int    `json:"volume"`
			Issues string `json:"issues"`
			Why    string `json:"why"`
		} `json:"steps"`
		Note string `json:"note"`
	}
	if err := json.Unmarshal([]byte(ai.JSONObject(raw)), &ans); err != nil {
		writeError(w, http.StatusBadGateway, "The AI's answer couldn't be read. Try asking another way.")
		return
	}
	type have struct {
		id   int64
		year int
	}
	lib := map[string][]have{}
	if list, err := s.comicRepo().ListSeries(); err == nil {
		for _, c := range list {
			lib[normTitle(c.Name)] = append(lib[normTitle(c.Name)], have{c.ID, c.StartYear})
		}
	}
	type stepOut struct {
		Series string `json:"series"`
		Volume int    `json:"volume,omitempty"`
		Issues string `json:"issues,omitempty"`
		Why    string `json:"why,omitempty"`
		Have   bool   `json:"have"`
		To     string `json:"to"`
	}
	steps := []stepOut{}
	for _, st := range ans.Steps {
		st.Series = strings.TrimSpace(st.Series)
		if st.Series == "" || len(steps) == 12 {
			continue
		}
		o := stepOut{Series: st.Series, Volume: st.Volume, Issues: strings.TrimSpace(st.Issues), Why: strings.TrimSpace(st.Why)}
		// The run you have: same name, and the same start year when one is given.
		runs := lib[normTitle(st.Series)]
		for _, h := range runs {
			// The same start year, or the only run by that name you have.
			if st.Volume == 0 || h.year == 0 || h.year == st.Volume || len(runs) == 1 {
				o.Have, o.To = true, "/comics/"+strconv.FormatInt(h.id, 10)
				break
			}
		}
		if !o.Have {
			q := st.Series
			if st.Volume > 0 {
				q += " " + strconv.Itoa(st.Volume)
			}
			o.To = "/comics?tab=add&q=" + url.QueryEscape(q)
		}
		steps = append(steps, o)
	}
	res := map[string]any{"note": strings.TrimSpace(ans.Note), "steps": steps}
	aiCacheMu.Lock()
	aiCache[key] = aiCached{at: time.Now(), data: res}
	aiCacheMu.Unlock()
	writeJSON(w, http.StatusOK, res)
}
