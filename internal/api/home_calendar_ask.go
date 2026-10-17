package api

// Usherr: the calendar's AI search. The AI reads what's on the calendar
// (titles, dates, kinds, downloaded or not) and picks the entries a question
// is about: "anime this weekend", "superhero films next month", "when is the
// new Dune out". The page then shows just those.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/ai"
	"github.com/rdborg/mediarium/internal/auth"
)

const calendarAskSystem = `You help someone search their media calendar. You get today's date, a numbered list of calendar entries (number | date | kind | title | detail | status) and their question.
Pick the entries the question is about. Use what you know about the titles (genre, franchise, cast, studio, country) and the dates (this weekend, next month, before Christmas). For a TV show, pick all its matching episodes.
Reply with a JSON object only: {"picks":[numbers],"answer":"one or two short sentences"}
The answer says what you found in at most 25 words, in the language asked for. Never list the titles: the page shows them. If nothing fits, picks is [] and the answer says so.`

// The most entries the AI is shown; the calendar's window rarely has more.
const calendarAskMax = 900

// POST /api/home/calendar/ask {query, lang}: {answer, keys}. A key is
// "kind:id", as the page builds it for each entry.
func (s *Server) handleCalendarAsk(w http.ResponseWriter, r *http.Request) {
	cfg, ok := s.aiConfig()
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "AI isn't set up. An administrator can set it up under Settings > Extras.")
		return
	}
	var req struct {
		Query string `json:"query"`
		Lang  string `json:"lang"`
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Query) == "" {
		writeError(w, http.StatusBadRequest, "Ask something first.")
		return
	}
	ctx := r.Context()
	if !s.aiAllowed(ctx, auth.UserFromContext(ctx)) {
		writeError(w, http.StatusTooManyRequests, "That's all the AI answers for today. More tomorrow.")
		return
	}
	entries, err := s.calendarEntries()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if more, err := s.moduleCalendarEntries(); err == nil {
		entries = append(entries, more...)
	}
	entries = append(entries, s.cinemaCalendarEntries(ctx)...)
	entries = append(entries, s.streamedCalendarEntries()...)
	if len(entries) > calendarAskMax {
		entries = entries[:calendarAskMax]
	}
	anime := s.animeSeriesIDs(ctx)
	var sb strings.Builder
	for i, e := range entries {
		kind := e.Kind
		switch {
		case e.Kind == "episode" && (anime[e.SeriesID] || e.Anime):
			kind = "anime episode"
		case e.Kind == "episode":
			kind = "TV episode"
		case e.Kind == "album":
			kind = "music album"
		case e.Status == "theaters":
			kind = "film in cinemas"
		}
		status := e.Status
		if status == "" {
			status = "-"
		}
		fmt.Fprintf(&sb, "%d | %s | %s | %s | %s | %s\n", i, e.ReleaseDate, kind, e.Title, e.Subtitle, status)
	}
	prompt := fmt.Sprintf("Today: %s\nAnswer in %s.\n\nCalendar:\n%s\nQuestion: %s", time.Now().Format("Monday 2 January 2006"), languageName(req.Lang), sb.String(), firstN(strings.TrimSpace(req.Query), 500))
	raw, err := ai.Ask(ctx, cfg, ai.Request{System: calendarAskSystem, Prompt: prompt, JSON: true, MaxTokens: 3000})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	var rep struct {
		Picks  []int  `json:"picks"`
		Answer string `json:"answer"`
	}
	if err := json.Unmarshal([]byte(ai.JSONObject(raw)), &rep); err != nil {
		writeError(w, http.StatusBadGateway, "The AI's answer couldn't be read. Try again.")
		return
	}
	keys := []string{}
	for _, i := range rep.Picks {
		if i >= 0 && i < len(entries) {
			keys = append(keys, entries[i].Kind+":"+strconv.FormatInt(entries[i].ID, 10))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"answer": shortAnswer(rep.Answer, 240), "keys": keys})
}

// shortAnswer keeps an answer to about n characters, ending at a sentence.
func shortAnswer(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	cut := s[:n]
	if i := strings.LastIndexAny(cut, ".!?"); i > 40 {
		return cut[:i+1]
	}
	if i := strings.LastIndex(cut, " "); i > 0 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, ",;: ") + "…"
}
