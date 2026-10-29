package api

// Usherr: Read-to-Me's full cast, the AI's part and the routes. The AI (the
// household's, Settings > Extras > AI) is asked chapter by chapter who says
// each line, when the dialog or a job asks for it: a request a chapter, done
// once and kept (a chapter whose text changed is asked again). What it says
// never wins over the text's own tags, nor over what someone set by hand.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/ai"
	"github.com/rdborg/mediarium/internal/books"
)

const (
	maxCastPeople = 60
	castAIChunk   = 9000 // characters of a chapter in one request
)

// castMax is the longest piece every engine of a cast takes.
func (s *Server) castMax(e rtvEngine, cast *rtvCast) int {
	max := e.maxPiece()
	for _, r := range cast.refs() {
		if ce, ok := s.readEngine(r.Engine); ok && ce.maxPiece() < max {
			max = ce.maxPiece()
		}
	}
	return max
}

// checkCast checks a cast's voices are real ones, and its names are names.
func (s *Server) checkCast(r *http.Request, c *rtvCast) error {
	if c == nil {
		return nil
	}
	if len(c.People) > maxCastPeople {
		return fmt.Errorf("A cast is up to %d characters.", maxCastPeople)
	}
	for name := range c.People {
		if strings.TrimSpace(name) == "" || len([]rune(name)) > 60 {
			return errors.New("A character's name is 1 to 60 letters.")
		}
	}
	for _, ref := range c.refs() {
		if _, err := s.voiceExists(r.Context(), ref.Engine, ref.Voice); err != nil {
			return fmt.Errorf("A voice in the cast: %s", err.Error())
		}
	}
	if c.AI {
		if _, ok := s.aiConfig(); !ok {
			return errors.New("The AI isn't set up (Settings > Extras > AI), so it can't work out who says what.")
		}
	}
	return nil
}

// ---- the AI ----

var (
	castAILocks sync.Map // book id -> *sync.Mutex
	castAIState = struct {
		sync.Mutex
		progress map[int64]string // book id -> what it is doing
		errs     map[int64]string
	}{progress: map[int64]string{}, errs: map[int64]string{}}
)

const castAISystem = `You work out who speaks each line of dialogue in a novel. Each quoted line in the text is marked [Q<number>] just before it.
Answer only with JSON: {"lines": {"<number>": "<speaker>"}, "people": {"<name>": "m" | "f" | ""}}.
For the speaker, use the name the book uses most for that character (one of the known characters when they fit). Use "I" for the lines of a narrator who tells the story as "I". Use "?" when the text doesn't let you tell.
Give every marked line. Under "people", give each speaker's gender ("m" or "f") when the text makes it clear, else "".`

// castAI asks the AI about every chapter it hasn't gone through yet. say
// tells what it is doing.
func (s *Server) castAI(ctx context.Context, bookID int64, book rtvBook, rules []sayRule, say func(string)) error {
	cfg, ok := s.aiConfig()
	if !ok {
		return errors.New("the AI isn't set up")
	}
	l, _ := castAILocks.LoadOrStore(bookID, &sync.Mutex{})
	mu := l.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()

	// The names the text's own tags give (twice or more), as a guide.
	count := map[string]int{}
	type chap struct {
		paras []castPara
		lines []castLine
		hash  string
	}
	var chaps []chap
	for _, ch := range book.Chapters {
		text := speakable(ch.Text, rules)
		paras, lines := castChapter(text)
		for _, l := range lines {
			if l.How != "" && !strings.HasPrefix(l.Speaker, "?") {
				count[l.Speaker]++
			}
		}
		chaps = append(chaps, chap{paras, lines, hashOf(text)})
	}
	var known []string
	for n, c := range count {
		if c >= 2 && n != "I" {
			known = append(known, n)
		}
	}
	sort.Slice(known, func(i, j int) bool { return count[known[i]] > count[known[j]] })
	if len(known) > 40 {
		known = known[:40]
	}

	done := map[int]string{}
	if rows, err := s.db.QueryContext(ctx, `SELECT chapter, hash FROM readtome_ai_chapters WHERE book_id = ?`, bookID); err == nil {
		for rows.Next() {
			var c int
			var h string
			if rows.Scan(&c, &h) == nil {
				done[c] = h
			}
		}
		rows.Close()
	}
	for ci, c := range chaps {
		if len(c.lines) == 0 || done[ci] == c.hash {
			continue
		}
		if say != nil {
			say(fmt.Sprintf("Working out who says what: chapter %d of %d", ci+1, len(chaps)))
		}
		lines, people, err := s.askCastChapter(ctx, cfg, c.paras, c.lines, known)
		if err != nil {
			return err
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		_, _ = tx.Exec(`DELETE FROM readtome_lines WHERE book_id = ? AND chapter = ? AND source = 'ai'`, bookID, ci)
		for idx, who := range lines {
			if idx >= 0 && idx < len(c.lines) {
				_, _ = tx.Exec(`INSERT INTO readtome_lines (book_id, chapter, quote, hash, speaker, source) VALUES (?, ?, ?, ?, ?, 'ai')`, bookID, ci, idx, c.lines[idx].Hash, who)
			}
		}
		for name, g := range people {
			_, _ = tx.Exec(`INSERT INTO readtome_people (book_id, name, gender) VALUES (?, ?, ?) ON CONFLICT(book_id, name) DO UPDATE SET gender = CASE WHEN excluded.gender <> '' THEN excluded.gender ELSE gender END`, bookID, name, g)
		}
		_, _ = tx.Exec(`INSERT INTO readtome_ai_chapters (book_id, chapter, hash) VALUES (?, ?, ?) ON CONFLICT(book_id, chapter) DO UPDATE SET hash = excluded.hash`, bookID, ci, c.hash)
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// askCastChapter asks about one chapter, in parts when it is long: the
// paragraphs with dialogue, each with the one before it, quotes marked.
func (s *Server) askCastChapter(ctx context.Context, cfg ai.Config, paras []castPara, lines []castLine, known []string) (map[int]string, map[string]string, error) {
	var blocks []string
	for pi, p := range paras {
		if len(p.Lines) == 0 {
			continue
		}
		var b strings.Builder
		if pi > 0 && len(paras[pi-1].Lines) == 0 && !paras[pi-1].Scene {
			for _, sg := range paras[pi-1].Segs {
				b.WriteString(sg.Text + " ")
			}
			b.WriteString("\n")
		}
		q := 0
		for _, sg := range p.Segs {
			if sg.Quote {
				fmt.Fprintf(&b, "[Q%d] “%s” ", p.Lines[q], sg.Text)
				q++
			} else {
				b.WriteString(sg.Text + " ")
			}
		}
		blocks = append(blocks, strings.TrimSpace(b.String()))
	}
	lineOut, people := map[int]string{}, map[string]string{}
	knownSet := map[string]string{}
	for _, n := range known {
		knownSet[strings.ToLower(n)] = n
	}
	var part []string
	size := 0
	ask := func() error {
		if len(part) == 0 {
			return nil
		}
		prompt := "Known characters: " + strings.Join(known, ", ") + "\n\n" + strings.Join(part, "\n\n")
		n := strings.Count(prompt, "[Q")
		cctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
		defer cancel()
		raw, err := ai.Ask(cctx, cfg, ai.Request{System: castAISystem, Prompt: prompt, JSON: true, MaxTokens: min(4000, 200+n*16)})
		if err != nil {
			return fmt.Errorf("the AI: %w", err)
		}
		var ans struct {
			Lines  map[string]string `json:"lines"`
			People map[string]string `json:"people"`
		}
		if err := json.Unmarshal([]byte(ai.JSONObject(raw)), &ans); err != nil {
			return errors.New("the AI's answer couldn't be read")
		}
		// The names as the AI writes them in "people" count as known too (one casing for each).
		for name := range ans.People {
			if n := strings.TrimSpace(name); n != "" && knownSet[strings.ToLower(n)] == "" && n != strings.ToLower(n) {
				knownSet[strings.ToLower(n)] = n
			}
		}
		for k, who := range ans.Lines {
			idx, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(k), "Q"))
			who = strings.TrimSpace(who)
			if err != nil || who == "" || who == "?" || len([]rune(who)) > 40 || idx < 0 || idx >= len(lines) {
				continue
			}
			if n, ok := knownSet[strings.ToLower(who)]; ok {
				who = n
			} else if strings.EqualFold(who, "i") || strings.EqualFold(who, "narrator") {
				who = "I"
			} else if who == strings.ToLower(who) {
				who = strings.ToUpper(who[:1]) + who[1:] // "mary": Mary
			}
			lineOut[idx] = who
		}
		for name, g := range ans.People {
			if g = strings.ToLower(strings.TrimSpace(g)); g == "m" || g == "f" {
				if n, ok := knownSet[strings.ToLower(name)]; ok {
					name = n
				}
				people[strings.TrimSpace(name)] = g
			}
		}
		part, size = nil, 0
		return nil
	}
	for _, b := range blocks {
		if size+len(b) > castAIChunk {
			if err := ask(); err != nil {
				return nil, nil, err
			}
		}
		part = append(part, b)
		size += len(b)
	}
	if err := ask(); err != nil {
		return nil, nil, err
	}
	return lineOut, people, nil
}

// ---- routes ----

func (s *Server) castBook(r *http.Request) (books.Book, rtvBook, error) {
	id, _ := strconv.ParseInt(r.URL.Query().Get("bookId"), 10, 64)
	return s.castBookByID(id)
}

func (s *Server) castBookByID(id int64) (books.Book, rtvBook, error) {
	b, err := s.BookRepo.Get(id)
	if err != nil {
		return b, rtvBook{}, errors.New("That book isn't in the library.")
	}
	if b.EbookPath == "" || !strings.EqualFold(b.EbookFormat, "epub") {
		return b, rtvBook{}, errors.New("Read-to-Me needs the book as an EPUB in the library.")
	}
	book, err := s.readingBook(context.Background(), b)
	return b, book, err
}

// GET /api/home/readtome/cast?bookId=: the book's characters (with how many
// lines each has), how many lines are known, and the AI's progress.
func (s *Server) handleReadToMeCast(w http.ResponseWriter, r *http.Request) {
	b, book, err := s.castBook(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	cb := s.castOf(r.Context(), b.ID, book, s.sayRules(r.Context(), b.ID))
	type chapterCast struct {
		Title   string `json:"title"`
		Lines   int    `json:"lines"`
		Unknown int    `json:"unknown"`
		AI      bool   `json:"ai"`
	}
	done := map[int]bool{}
	if rows, err := s.db.QueryContext(r.Context(), `SELECT chapter FROM readtome_ai_chapters WHERE book_id = ?`, b.ID); err == nil {
		for rows.Next() {
			var c int
			if rows.Scan(&c) == nil {
				done[c] = true
			}
		}
		rows.Close()
	}
	chapters := []chapterCast{}
	for ci, lines := range cb.Lines {
		cc := chapterCast{Title: book.Chapters[ci].Title, Lines: len(lines), AI: done[ci]}
		if cc.Title == "" {
			cc.Title = fmt.Sprintf("Part %d", ci+1)
		}
		for _, l := range lines {
			if strings.HasPrefix(l.Speaker, "?") {
				cc.Unknown++
			}
		}
		chapters = append(chapters, cc)
	}
	_, aiOn := s.aiConfig()
	castAIState.Lock()
	progress, aiErr := castAIState.progress[b.ID], castAIState.errs[b.ID]
	castAIState.Unlock()
	people := cb.People
	if people == nil {
		people = []castPerson{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"people": people, "quotes": cb.Quotes, "known": cb.Known, "chapters": chapters,
		"aiOn": aiOn, "aiRunning": progress, "aiError": aiErr,
	})
}

// POST /api/home/readtome/cast/ai {bookId}: the AI goes through the book's
// dialogue now (in the background; GET cast says how far it is).
func (s *Server) handleReadToMeCastAI(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BookID int64 `json:"bookId"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	if _, ok := s.aiConfig(); !ok {
		writeError(w, http.StatusBadRequest, "The AI isn't set up (Settings > Extras > AI).")
		return
	}
	b, book, err := s.castBookByID(req.BookID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	castAIState.Lock()
	if castAIState.progress[b.ID] != "" {
		castAIState.Unlock()
		writeJSON(w, http.StatusAccepted, map[string]any{})
		return
	}
	castAIState.progress[b.ID] = "Starting"
	delete(castAIState.errs, b.ID)
	castAIState.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
		defer cancel()
		err := s.castAI(ctx, b.ID, book, s.sayRules(ctx, b.ID), func(note string) {
			castAIState.Lock()
			castAIState.progress[b.ID] = note
			castAIState.Unlock()
		})
		castAIState.Lock()
		delete(castAIState.progress, b.ID)
		if err != nil {
			castAIState.errs[b.ID] = err.Error()
		}
		castAIState.Unlock()
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{})
}

// GET /api/home/readtome/cast/lines?bookId=&chapter=: a chapter's lines, who
// says each and how that was found, for checking them.
func (s *Server) handleReadToMeCastLines(w http.ResponseWriter, r *http.Request) {
	b, book, err := s.castBook(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ci, err := strconv.Atoi(r.URL.Query().Get("chapter"))
	if err != nil || ci < 0 || ci >= len(book.Chapters) {
		writeError(w, http.StatusBadRequest, "That chapter isn't in the book.")
		return
	}
	cb := s.castOf(r.Context(), b.ID, book, s.sayRules(r.Context(), b.ID))
	lines := cb.Lines[ci]
	if lines == nil {
		lines = []castLine{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"lines": lines})
}

// PUT /api/home/readtome/cast/lines {bookId, chapter, index, speaker}: who says
// a line, set by hand ("" undoes it).
func (s *Server) handlePutReadToMeCastLine(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BookID  int64  `json:"bookId"`
		Chapter int    `json:"chapter"`
		Index   int    `json:"index"`
		Speaker string `json:"speaker"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	b, book, err := s.castBookByID(req.BookID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Chapter < 0 || req.Chapter >= len(book.Chapters) {
		writeError(w, http.StatusBadRequest, "That chapter isn't in the book.")
		return
	}
	_, lines := castChapter(speakable(book.Chapters[req.Chapter].Text, s.sayRules(r.Context(), b.ID)))
	if req.Index < 0 || req.Index >= len(lines) {
		writeError(w, http.StatusBadRequest, "That line isn't in the chapter.")
		return
	}
	who := strings.Join(strings.Fields(req.Speaker), " ")
	if len([]rune(who)) > 60 {
		writeError(w, http.StatusBadRequest, "A name is up to 60 letters.")
		return
	}
	if who == "" {
		_, _ = s.db.ExecContext(r.Context(), `DELETE FROM readtome_lines WHERE book_id = ? AND chapter = ? AND quote = ? AND source = 'you'`, b.ID, req.Chapter, req.Index)
	} else {
		_, err = s.db.ExecContext(r.Context(), `INSERT INTO readtome_lines (book_id, chapter, quote, hash, speaker, source) VALUES (?, ?, ?, ?, ?, 'you')
			ON CONFLICT(book_id, chapter, quote, source) DO UPDATE SET hash = excluded.hash, speaker = excluded.speaker`, b.ID, req.Chapter, req.Index, lines[req.Index].Hash, who)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}
