package api

// Usherr: the AI channel builder. You describe a channel ("a 1995 TGIF
// channel", "horror for October nights"); the AI picks shows and movies from
// the library, names shows to stream that aren't in it, and sets the start
// times, set times and special programming. The answer opens in the channel
// editor to look over before it's saved.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/rdborg/mediarium/internal/ai"
	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/tv"
)

const tvBuildSystem = `You program a TV channel for a home media server, like a network scheduler would. You get the shows and movies in the library (id | title | year | genres) and a description of the channel wanted.
Reply with a JSON object only:
{"name":"short channel name","series":[library show ids],"movies":[library movie ids],"stream":[{"title":"...","year":0}],"grid":30,"slots":[{"days":[1,2,3,4,5],"at":"19:00","seriesId":0,"streamTitle":"","movie":false,"count":1,"name":""}],"holidays":false,"premieres":false,"familyHours":false,"note":"one sentence on what you made"}
Rules:
- %s
- grid: 30 for sitcoms and cartoons, 60 for dramas, 0 for movie channels.
- slots: pin the best-known fits to classic times when the description suggests it (a Friday 8 PM line-up, a Sunday Night Movie). seriesId must be a library show id; for a streamed show leave it 0 and put its title in streamTitle. days: 0 = Sunday ... 6 = Saturday.
- familyHours for kids' or family channels; holidays for seasonal ones; premieres when the shows are still airing.
- Keep it to what fits the description: 3 to 25 shows.`

// POST /api/tv/ai-build {prompt, lang}: a channel to look over.
func (s *Server) handleTVAIBuild(w http.ResponseWriter, r *http.Request) {
	cfg, ok := s.aiConfig()
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "AI isn't set up. An administrator can set it up under Settings > Extras.")
		return
	}
	var req struct {
		Prompt string `json:"prompt"`
		Lang   string `json:"lang"`
		// Usherr: "library" (no streaming), "mix" (the default) or "stream" (mostly streamed).
		Streaming string `json:"streaming"`
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Prompt) == "" {
		writeError(w, http.StatusBadRequest, "Describe the channel first.")
		return
	}
	ctx := r.Context()
	if !s.aiAllowed(ctx, auth.UserFromContext(ctx)) {
		writeError(w, http.StatusTooManyRequests, "That's all the AI answers for today. More tomorrow.")
		return
	}
	var sb strings.Builder
	sb.WriteString("Shows in the library:\n")
	showIDs := map[int64]bool{}
	if rows, err := s.db.QueryContext(ctx, `SELECT id, title, year, COALESCE(genres, '') FROM series WHERE copy_of IS NULL AND EXISTS (SELECT 1 FROM episodes e WHERE e.series_id = series.id AND e.file_path <> '') ORDER BY title LIMIT 600`); err == nil {
		for rows.Next() {
			var id int64
			var title, genres string
			var year int
			if rows.Scan(&id, &title, &year, &genres) == nil {
				showIDs[id] = true
				fmt.Fprintf(&sb, "%d | %s | %d | %s\n", id, title, year, genreList(genres))
			}
		}
		rows.Close()
	}
	sb.WriteString("\nMovies in the library:\n")
	movieIDs := map[int64]bool{}
	if rows, err := s.db.QueryContext(ctx, `SELECT id, title, year, COALESCE(genres, '') FROM movies WHERE file_path <> '' AND copy_of IS NULL ORDER BY title LIMIT 800`); err == nil {
		for rows.Next() {
			var id int64
			var title, genres string
			var year int
			if rows.Scan(&id, &title, &year, &genres) == nil {
				movieIDs[id] = true
				fmt.Fprintf(&sb, "%d | %s | %d | %s\n", id, title, year, genreList(genres))
			}
		}
		rows.Close()
	}
	prompt := fmt.Sprintf("%s\nThe channel wanted: %s\nWrite the name and note in %s.", sb.String(), firstN(strings.TrimSpace(req.Prompt), 400), languageName(req.Lang))
	streamCap := 8
	streamRule := `Use library shows and movies that fit; prefer them. "stream" is for a few famous shows that fit but aren't in the library (they play from the internet); leave it empty when the library has enough.`
	switch req.Streaming {
	case "library":
		streamCap = 0
		streamRule = `Use only library shows and movies. Leave "stream" empty.`
	case "stream":
		streamCap = 16
		streamRule = `Build the line-up mostly from streamed shows (they play from the internet): put the shows that fit in "stream", in the library or not (up to 16). Library movies may still go in "movies".`
	}
	raw, err := ai.Ask(ctx, cfg, ai.Request{System: fmt.Sprintf(tvBuildSystem, streamRule), Prompt: prompt, JSON: true, MaxTokens: 2500})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	var ans struct {
		Name     string    `json:"name"`
		Series   []int64   `json:"series"`
		Movies   []int64   `json:"movies"`
		Stream   []aiTitle `json:"stream"`
		Grid     int       `json:"grid"`
		Slots    []tv.Slot `json:"slots"`
		Holidays bool      `json:"holidays"`
		Prem     bool      `json:"premieres"`
		Family   bool      `json:"familyHours"`
		Note     string    `json:"note"`
	}
	if err := json.Unmarshal([]byte(ai.JSONObject(raw)), &ans); err != nil {
		writeError(w, http.StatusBadGateway, "The AI's answer couldn't be read. Try again.")
		return
	}
	// Only what is really in the library; streamed shows looked up on TMDB.
	rules := tv.Rules{Order: "rotate", Holidays: ans.Holidays, Premieres: ans.Prem, FamilyHours: ans.Family}
	for _, id := range ans.Series {
		if showIDs[id] {
			rules.Series = append(rules.Series, id)
		}
	}
	for _, id := range ans.Movies {
		if movieIDs[id] {
			rules.Movies = append(rules.Movies, id)
		}
	}
	switch ans.Grid {
	case 15, 30, 60:
		rules.Grid = ans.Grid
	}
	tm := s.displayTMDB()
	for _, st := range ans.Stream {
		if len(rules.Stream) >= streamCap {
			break
		}
		if _, sh := matchTitle(ctx, tm, aiTitle{Title: st.Title, Year: st.Year, Type: "tv"}); sh != nil {
			rules.Stream = append(rules.Stream, tv.StreamShow{TMDBID: int64(sh.TMDBID), Title: sh.Name, Year: sh.Year()})
		}
	}
	for _, sl := range ans.Slots {
		// Usherr: a set time may stream a show, one of the channel's streamed ones.
		if sl.SeriesID == 0 && !sl.Movie && sl.StreamTitle != "" && streamCap > 0 {
			for _, st := range rules.Stream {
				if strings.EqualFold(st.Title, strings.TrimSpace(sl.StreamTitle)) {
					sl.StreamTMDB, sl.StreamTitle = st.TMDBID, st.Title
				}
			}
		}
		if (sl.Movie && len(rules.Movies) > 0) || showIDs[sl.SeriesID] || (sl.SeriesID == 0 && sl.StreamTMDB > 0) {
			if _, _, ok := clockOfSlot(sl.At); ok {
				rules.Slots = append(rules.Slots, sl)
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": strings.TrimSpace(ans.Name), "rules": rules, "note": strings.TrimSpace(ans.Note)})
}

// genreList turns a stored genres JSON array into "A, B".
func genreList(raw string) string {
	var g []string
	if json.Unmarshal([]byte(raw), &g) != nil {
		return ""
	}
	return strings.Join(g, ", ")
}

// clockOfSlot checks a slot's "HH:MM".
func clockOfSlot(s string) (int, int, bool) {
	var h, m int
	if n, err := fmt.Sscanf(strings.TrimSpace(s), "%d:%d", &h, &m); err != nil || n != 2 || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, false
	}
	return h, m, true
}
