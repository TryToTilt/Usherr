package api

// Home fork: the AI bar. One question, from anywhere in the app: the AI
// works out what is wanted (find titles, a recap, a skip guide, a playlist,
// books like one, a reading order, add something…), Mediarium finds the
// title, book, artist or comic it means, and the page opens that feature.
// Anything that changes the library still waits for a tap.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/rdborg/mediarium/internal/ai"
	"github.com/rdborg/mediarium/internal/metadata"
)

// The actions the bar can take, as the page knows them.
var askActions = map[string]bool{
	"titles": true, "room": true, "tonight": true, "recap": true, "skip": true, "vibe": true, "add": true,
	"books": true, "read_alikes": true, "book_recap": true,
	"comic_guide": true, "comic_recap": true,
	"playlist": true, "similar_artists": true,
	"failures": true, "answer": true, "search": true,
}

const askSystem = `You are the assistant inside Mediarium, a home media server (films, TV, anime, ebooks, audiobooks, music, comics). Work out what the person wants and reply with a JSON object only:
{"action":"...","query":"","title":"","type":"","year":0,"season":0,"episode":0,"issue":"","mood":"","answer":""}
Actions:
- "titles": find films or shows from a description ("a 90s heist movie", "every Villeneuve film", "anime like Cowboy Bebop"). query = what they asked.
- "room": pick something for several people watching together.
- "tonight": what should I (one person) watch tonight.
- "recap": catch me up on a TV show. title; season and episode if they said where they are.
- "skip": which episodes of a show can be skipped / filler guide. title.
- "vibe": what is a film or show like, is it scary, is it for kids. title, type "movie" or "tv".
- "add": add / download / get a specific film or show. title, type, year.
- "books": find books or audiobooks from a description. query.
- "read_alikes": books like a given book. title.
- "book_recap": catch me up on a book or book series. title.
- "comic_guide": comic reading order or where to start. query.
- "comic_recap": catch me up on a comic series. title; issue if they said one.
- "playlist": make a music playlist. mood = the mood or occasion.
- "similar_artists": music like an artist. title = the artist.
- "failures": why downloads failed / problems with downloads.
- "answer": any other question about films, shows, books, music or comics you can answer in a few sentences (answer = your reply, in the language asked for). Use it also for questions about how to use the app.
- "search": they just typed a name to look up. query = the name.
Use the page they're on to decide: on Books, a description is "books"; on Music, a mood is "playlist"; on Comics, "where do I start" is "comic_guide".`

type askReply struct {
	Action  string `json:"action"`
	Query   string `json:"query"`
	Title   string `json:"title"`
	Type    string `json:"type"`
	Year    int    `json:"year"`
	Season  int    `json:"season"`
	Episode int    `json:"episode"`
	Issue   string `json:"issue"`
	Mood    string `json:"mood"`
	Answer  string `json:"answer"`
	// Filled in by Mediarium.
	TMDBID    int    `json:"tmdbId,omitempty"`
	Kind      string `json:"kind,omitempty"` // movie or tv
	PosterURL string `json:"posterUrl,omitempty"`
	BookID    int64  `json:"bookId,omitempty"`
	ArtistID  int64  `json:"artistId,omitempty"`
	SeriesID  int64  `json:"seriesId,omitempty"` // a comic series
	Note      string `json:"note,omitempty"`     // why Mediarium couldn't do it
}

// POST /api/home/ai/ask {query, page, lang}: what the bar should do.
// Working out the question doesn't count against the daily limit; the
// feature it opens does.
func (s *Server) handleAIAsk(w http.ResponseWriter, r *http.Request) {
	cfg, ok := s.aiConfig()
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "AI isn't set up. An administrator can set it up under Settings > Extras.")
		return
	}
	var req struct {
		Query string `json:"query"`
		Page  string `json:"page"` // dashboard, movie, tv, anime, ebook, audiobook, music, comics, nsfw…
		Lang  string `json:"lang"`
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Query) == "" {
		writeError(w, http.StatusBadRequest, "Ask something first.")
		return
	}
	req.Query = strings.TrimSpace(firstN(req.Query, 500))
	ctx := r.Context()
	prompt := fmt.Sprintf("Page they're on: %s\nAnswer in %s.\n\nThey asked: %s", firstNonEmptyStr(req.Page, "dashboard"), languageName(req.Lang), req.Query)
	raw, err := ai.Ask(ctx, cfg, ai.Request{System: askSystem, Prompt: prompt, JSON: true, MaxTokens: 600})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	var rep askReply
	if err := json.Unmarshal([]byte(ai.JSONObject(raw)), &rep); err != nil || !askActions[rep.Action] {
		rep = askReply{Action: "titles", Query: req.Query}
	}
	rep.Title = strings.TrimSpace(rep.Title)
	rep = askFixups(rep, req.Query, req.Page)
	tm := s.displayTMDB()
	switch rep.Action {
	case "recap", "skip", "vibe", "add":
		typ := rep.Type
		if rep.Action == "recap" || rep.Action == "skip" {
			typ = "tv"
		}
		if rep.Title == "" {
			rep.Action, rep.Answer = "answer", "Which one do you mean? Say its name."
			break
		}
		mv, sh := matchTitle(ctx, tm, aiTitle{Title: rep.Title, Year: rep.Year, Type: typ})
		switch {
		case sh != nil:
			rep.Kind, rep.TMDBID, rep.Title, rep.Year, rep.PosterURL = "tv", sh.TMDBID, sh.Name, sh.Year(), posterOf(sh.PosterPath)
		case mv != nil && rep.Action != "recap" && rep.Action != "skip":
			rep.Kind, rep.TMDBID, rep.Title, rep.Year, rep.PosterURL = "movie", mv.TMDBID, mv.Title, mv.Year(), posterOf(mv.PosterPath)
		default:
			rep.Note = "I couldn't find that title. Try its full name."
		}
	case "read_alikes", "book_recap":
		if lib, err := s.BookRepo.List(); err == nil {
			if ok, to := bookPlace(lib, rep.Title, ""); ok {
				rep.BookID, _ = strconv.ParseInt(strings.TrimPrefix(to, "/book/"), 10, 64)
			}
		}
		if rep.BookID == 0 {
			if rep.Action == "read_alikes" {
				rep.Action, rep.Query = "books", "books like "+rep.Title
			} else {
				rep.Note = "That book isn't in your library yet, so there's nothing to recap from. Add it first."
			}
		}
	case "similar_artists":
		if list, err := s.MusicRepo.ListArtists(); err == nil {
			for _, a := range list {
				if normTitle(a.Name) == normTitle(rep.Title) {
					rep.ArtistID = a.ID
				}
			}
		}
		if rep.ArtistID == 0 {
			rep.Note = "That artist isn't in your music library."
		}
	case "comic_recap":
		if list, err := s.comicRepo().ListSeries(); err == nil {
			for _, c := range list {
				if normTitle(c.Name) == normTitle(rep.Title) {
					rep.SeriesID = c.ID
				}
			}
		}
		if rep.SeriesID == 0 {
			rep.Note = "That comic series isn't in your library."
		}
	case "playlist":
		if strings.TrimSpace(rep.Mood) == "" {
			rep.Mood = req.Query
		}
	}
	writeJSON(w, http.StatusOK, rep)
}

func posterOf(path string) string {
	if path == "" {
		return ""
	}
	return metadata.PosterURL(path)
}

var (
	askQuestion = regexp.MustCompile(`(?i)^(who|what|when|where|why|how|which|is|are|was|were|does|did|do|can|should)\b`)
	askLead     = regexp.MustCompile(`(?i)^\s*(please\s+)?(catch me up on|catch up on|recap|add|download|get me|get|what can i skip in|what to skip in|skip guide for|what'?s|what is|is|books like|music like|artists like|more like|read-alikes for)\s+(the show\s+|the movie\s+|the film\s+)?`)
	askTail     = regexp.MustCompile(`(?i)\s+(like\??|for me|please|\?)\s*$`)
)

// askFixups catches the usual slips of smaller models: the follow-up gets
// the person's own words, the page decides what a description is for, a
// plain question gets the answer, and a missing title comes from the question.
func askFixups(rep askReply, query, page string) askReply {
	switch rep.Action {
	case "titles", "books", "comic_guide":
		rep.Query = query
	}
	if rep.Query == "" {
		rep.Query = query
	}
	switch {
	case rep.Action == "answer" && strings.TrimSpace(rep.Answer) == "": // nothing said: look for titles instead
		rep.Action, rep.Query = "titles", query
		if page == "ebook" || page == "audiobook" {
			rep.Action = "books"
		}
	case rep.Action == "titles" && (page == "ebook" || page == "audiobook"):
		rep.Action = "books"
	case rep.Action == "titles" && page == "comics":
		rep.Action = "comic_guide"
	case rep.Action == "titles" && strings.TrimSpace(rep.Answer) != "" && askQuestion.MatchString(strings.TrimSpace(query)):
		rep.Action = "answer"
	}
	if rep.Title == "" {
		switch rep.Action {
		case "recap", "skip", "vibe", "add", "read_alikes", "book_recap", "comic_recap", "similar_artists":
			t := askLead.ReplaceAllString(strings.TrimSpace(query), "")
			t = strings.TrimSpace(askTail.ReplaceAllString(t, ""))
			rep.Title = strings.Trim(t, "\"“”'?. ")
		}
	}
	return rep
}
