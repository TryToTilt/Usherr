package api

// Home fork: AI for books and audiobooks.
//   - Book AI Search: books from a description ("a cozy mystery set in
//     Scotland", "narrated by someone like Ray Porter").
//   - Read-alikes: books on your shelves that go with one, and a few new ones.
//   - Catch me up: a spoiler-free recap of a series up to a book, before the
//     next one.
// Titles not in the library link to the usual search; nothing is added
// without you.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/ai"
	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/books"
)

type aiBookOut struct {
	Title  string `json:"title"`
	Author string `json:"author,omitempty"`
	Year   int    `json:"year,omitempty"`
	Why    string `json:"why,omitempty"`
	Have   bool   `json:"have"`
	To     string `json:"to"`
}

// bookPlace finds a book in the library by its title (and author when given),
// else the search for it.
func bookPlace(lib []books.Book, title, author string) (bool, string) {
	t := normTitle(title)
	a := normTitle(author)
	for _, b := range lib {
		if normTitle(b.Title) == t && (a == "" || strings.Contains(normTitle(b.Author), a) || strings.Contains(a, normTitle(b.Author))) {
			return true, "/book/" + strconv.FormatInt(b.ID, 10)
		}
	}
	q := strings.TrimSpace(title + " " + author)
	return false, "/search?q=" + url.QueryEscape(q)
}

const bookSearchSystem = `You help people find books and audiobooks. Reply with a JSON object only:
{"books":[{"title":"...","author":"...","year":2019,"why":"..."}],"note":""}
- books: up to 10 real books that best match what they describe, the best first. For audiobook wishes (a narrator, a length), pick audiobooks that exist.
- why: one short sentence each. note: one short, friendly sentence. Both in the language asked for.`

// POST /api/home/ai/book-search {query, lang}
func (s *Server) handleAIBookSearch(w http.ResponseWriter, r *http.Request) {
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
		writeError(w, http.StatusBadRequest, "Describe the book you're looking for.")
		return
	}
	req.Query = strings.TrimSpace(firstN(req.Query, 400))
	ctx := r.Context()
	key := aiCacheKey("book-search", cfg.Provider, cfg.Model, strings.ToLower(req.Query), req.Lang)
	aiCacheMu.Lock()
	if c, ok := aiCache[key]; ok && time.Since(c.at) < time.Hour {
		aiCacheMu.Unlock()
		writeJSON(w, http.StatusOK, c.data)
		return
	}
	aiCacheMu.Unlock()
	if !s.aiAllowed(ctx, auth.UserFromContext(ctx)) {
		writeError(w, http.StatusTooManyRequests, "That's all the AI answers for today. More tomorrow.")
		return
	}
	raw, err := ai.Ask(ctx, cfg, ai.Request{System: bookSearchSystem, Prompt: req.Query + "\n\nAnswer in " + languageName(req.Lang) + ".", JSON: true, MaxTokens: 1200})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	var ans struct {
		Books []aiBookOut `json:"books"`
		Note  string      `json:"note"`
	}
	if err := json.Unmarshal([]byte(ai.JSONObject(raw)), &ans); err != nil {
		writeError(w, http.StatusBadGateway, "The AI's answer couldn't be read. Try asking another way.")
		return
	}
	lib, _ := s.BookRepo.List()
	out := []aiBookOut{}
	for _, b := range ans.Books {
		b.Title = strings.TrimSpace(b.Title)
		if b.Title == "" || len(out) == 10 {
			continue
		}
		b.Have, b.To = bookPlace(lib, b.Title, b.Author)
		out = append(out, b)
	}
	res := map[string]any{"note": strings.TrimSpace(ans.Note), "books": out}
	aiCacheMu.Lock()
	aiCache[key] = aiCached{at: time.Now(), data: res}
	aiCacheMu.Unlock()
	writeJSON(w, http.StatusOK, res)
}

const readAlikesSystem = `You recommend books. Given one book and a numbered list of the other books on the person's shelves, reply with a JSON object only:
{"have":[3,14],"discover":[{"title":"...","author":"...","year":2020,"why":"..."}],"note":""}
- have: up to 8 numbers of books from the list that a reader of this book would enjoy next, the best first. Only from the list.
- discover: up to 6 books NOT on the list they'd likely enjoy, with one short sentence each on why (no spoilers).
- note: one short sentence. Write the why and the note in the language asked for.`

// POST /api/home/ai/read-alikes {bookId, lang}
func (s *Server) handleAIReadAlikes(w http.ResponseWriter, r *http.Request) {
	cfg, ok := s.aiConfig()
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "AI isn't set up. An administrator can set it up under Settings > Extras.")
		return
	}
	var req struct {
		BookID int64  `json:"bookId"`
		Lang   string `json:"lang"`
	}
	if err := decodeJSON(r, &req); err != nil || req.BookID <= 0 {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	ctx := r.Context()
	lib, err := s.BookRepo.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var me *books.Book
	var others []books.Book
	for i := range lib {
		if lib[i].ID == req.BookID {
			me = &lib[i]
		} else {
			others = append(others, lib[i])
		}
	}
	if me == nil {
		writeError(w, http.StatusNotFound, "That book isn't in your library.")
		return
	}
	sort.Slice(others, func(i, j int) bool { return strings.ToLower(others[i].Title) < strings.ToLower(others[j].Title) })
	if len(others) > 1500 {
		others = others[:1500]
	}
	key := aiCacheKey("read-alikes", cfg.Provider, cfg.Model, strconv.FormatInt(me.ID, 10), strconv.Itoa(len(others)), req.Lang)
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
	var sb strings.Builder
	fmt.Fprintf(&sb, "The book: %s by %s", me.Title, me.Author)
	if me.Year > 0 {
		fmt.Fprintf(&sb, " (%d)", me.Year)
	}
	fmt.Fprintf(&sb, ". Answer in %s.\n\nThe other books on their shelves:\n", languageName(req.Lang))
	for i, b := range others {
		fmt.Fprintf(&sb, "%d. %s - %s\n", i+1, b.Title, b.Author)
	}
	raw, err := ai.Ask(ctx, cfg, ai.Request{System: readAlikesSystem, Prompt: sb.String(), JSON: true, MaxTokens: 1000})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	var ans struct {
		Have     []int       `json:"have"`
		Discover []aiBookOut `json:"discover"`
		Note     string      `json:"note"`
	}
	if err := json.Unmarshal([]byte(ai.JSONObject(raw)), &ans); err != nil {
		writeError(w, http.StatusBadGateway, "The AI's answer couldn't be read. Try again.")
		return
	}
	res := struct {
		Note     string      `json:"note"`
		Have     []aiBookOut `json:"have"`
		Discover []aiBookOut `json:"discover"`
	}{Note: strings.TrimSpace(ans.Note), Have: []aiBookOut{}, Discover: []aiBookOut{}}
	used := map[int]bool{}
	for _, n := range ans.Have {
		if n >= 1 && n <= len(others) && !used[n] && len(res.Have) < 8 {
			used[n] = true
			b := others[n-1]
			res.Have = append(res.Have, aiBookOut{Title: b.Title, Author: b.Author, Year: b.Year, Have: true, To: "/book/" + strconv.FormatInt(b.ID, 10)})
		}
	}
	for _, d := range ans.Discover {
		d.Title = strings.TrimSpace(d.Title)
		if d.Title == "" || len(res.Discover) == 6 {
			continue
		}
		d.Have, d.To = bookPlace(lib, d.Title, d.Author)
		if !d.Have {
			res.Discover = append(res.Discover, d)
		}
	}
	aiCacheMu.Lock()
	aiCache[key] = aiCached{at: time.Now(), data: res}
	aiCacheMu.Unlock()
	writeJSON(w, http.StatusOK, res)
}

const bookRecapSystem = `You write spoiler-free "the story so far" recaps of book series, for a reader about to start the next book. You're given the series and the books up to and including the last one they read, with each book's description where known. Recap the story up to the end of that book only: never mention, hint at or foreshadow anything from later books, even if you know them. Focus on what the reader needs for the next book: where the main characters are, what they want, the open threads. If you don't know a book well, say so briefly rather than inventing. About 250 words, plain prose (a few short paragraphs, then one line starting "Where you left off:"). No headings, no lists, no markdown.`

// POST /api/home/ai/book-recap {bookId, lang}: the series so far, up to and
// including this book.
func (s *Server) handleAIBookRecap(w http.ResponseWriter, r *http.Request) {
	cfg, ok := s.aiConfig()
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "AI isn't set up. An administrator can set it up under Settings > Extras.")
		return
	}
	var req struct {
		BookID int64  `json:"bookId"`
		Lang   string `json:"lang"`
	}
	if err := decodeJSON(r, &req); err != nil || req.BookID <= 0 {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	ctx := r.Context()
	lib, err := s.BookRepo.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var me *books.Book
	for i := range lib {
		if lib[i].ID == req.BookID {
			me = &lib[i]
		}
	}
	if me == nil {
		writeError(w, http.StatusNotFound, "That book isn't in your library.")
		return
	}
	pos := func(b books.Book) float64 {
		f, err := strconv.ParseFloat(strings.TrimSpace(b.SeriesPosition), 64)
		if err != nil {
			return -1
		}
		return f
	}
	key := aiCacheKey("book-recap", cfg.Provider, cfg.Model, strconv.FormatInt(me.ID, 10), req.Lang)
	aiCacheMu.Lock()
	if c, ok := aiCache[key]; ok && time.Since(c.at) < 30*24*time.Hour {
		aiCacheMu.Unlock()
		writeJSON(w, http.StatusOK, c.data)
		return
	}
	aiCacheMu.Unlock()
	if !s.aiAllowed(ctx, auth.UserFromContext(ctx)) {
		writeError(w, http.StatusTooManyRequests, "That's all the AI answers for today. More tomorrow.")
		return
	}
	var sb strings.Builder
	if me.SeriesName != "" {
		var series []books.Book
		for _, b := range lib {
			if strings.EqualFold(b.SeriesName, me.SeriesName) && (pos(*me) < 0 || (pos(b) >= 0 && pos(b) <= pos(*me))) {
				series = append(series, b)
			}
		}
		sort.SliceStable(series, func(i, j int) bool { return pos(series[i]) < pos(series[j]) })
		fmt.Fprintf(&sb, "Series: %s, by %s. The reader has just finished book %s, %q.\n\nThe books so far:\n", me.SeriesName, me.Author, firstNonEmptyStr(me.SeriesPosition, "?"), me.Title)
		for _, b := range series {
			fmt.Fprintf(&sb, "- Book %s: %s", firstNonEmptyStr(b.SeriesPosition, "?"), b.Title)
			if b.Year > 0 {
				fmt.Fprintf(&sb, " (%d)", b.Year)
			}
			if d := strings.TrimSpace(b.Description); d != "" {
				fmt.Fprintf(&sb, ": %s", firstN(d, 900))
			}
			sb.WriteString("\n")
		}
	} else {
		fmt.Fprintf(&sb, "The book: %s by %s", me.Title, me.Author)
		if me.Year > 0 {
			fmt.Fprintf(&sb, " (%d)", me.Year)
		}
		if d := strings.TrimSpace(me.Description); d != "" {
			fmt.Fprintf(&sb, "\nDescription: %s", firstN(d, 1200))
		}
		sb.WriteString("\nThe reader has just finished this book (recap it, and the earlier books of its series if it has any).\n")
	}
	fmt.Fprintf(&sb, "\nWrite the recap in %s.", languageName(req.Lang))
	text, err := ai.Ask(ctx, cfg, ai.Request{System: bookRecapSystem, Prompt: sb.String(), MaxTokens: 1000})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	res := map[string]any{"title": me.Title, "series": me.SeriesName, "position": me.SeriesPosition, "recap": strings.TrimSpace(text)}
	aiCacheMu.Lock()
	aiCache[key] = aiCached{at: time.Now(), data: res}
	aiCacheMu.Unlock()
	writeJSON(w, http.StatusOK, res)
}

func firstNonEmptyStr(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}
