package api

// Usherr: Read-to-Me's lines. Chatterbox now and then repeats itself, babbles
// or leaves words out; a piece whose length doesn't fit its text is read again
// (another take) and the best one kept, and one still off is marked for a
// person to check. A made audiobook keeps its makings: its chapters, losslessly,
// and where each line is in them. So one line, or one chapter, can be read
// again (another take, or other words) and the audiobook put together anew
// without the rest being spoken again or losing anything. The makings take
// about 100 MB an hour of audio; they go after 30 days, or when asked.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	rtvTakes       = 3                   // a Chatterbox piece is read up to this many times when it doesn't fit its text
	rtvMakingsKeep = 30 * 24 * time.Hour // how long a made audiobook's makings are kept
)

// rtvLine is one piece of a made audiobook: what was read, in which voice, and
// where it is in its chapter.
type rtvLine struct {
	Text    string `json:"text"`
	Engine  string `json:"engine"`
	Voice   string `json:"voice"`
	StartMs int64  `json:"startMs"`
	EndMs   int64  `json:"endMs"`
	Flag    string `json:"flag,omitempty"` // why it may be read badly
}

// rtvMakings is what a made audiobook is made of (map.json in its folder).
type rtvMakings struct {
	Chapters []rtvMadeChapter `json:"chapters"`
}

type rtvMadeChapter struct {
	Index int       `json:"index"` // the book's chapter
	Title string    `json:"title"`
	Intro bool      `json:"intro,omitempty"` // it starts with the book's title and author
	File  string    `json:"file"`
	Lines []rtvLine `json:"lines"`
}

func (m rtvMakings) flagged() int {
	n := 0
	for _, c := range m.Chapters {
		for _, l := range c.Lines {
			if l.Flag != "" {
				n++
			}
		}
	}
	return n
}

// rtvRedo is what a redo job reads again.
type rtvRedo struct {
	Chapter int    `json:"chapter"`        // the made audiobook's chapter (from 0)
	Line    int    `json:"line"`           // its line; -1: the whole chapter
	Text    string `json:"text,omitempty"` // the line in other words
	Seed    int64  `json:"seed,omitempty"` // the line's take; 0: a new one
}

// ---- checking a line ----

// badLine says why a piece's audio doesn't fit its text ("" when it does): far
// longer (repeating, babbling, a long silence) or far shorter (words left out).
// The score is how far off it is; lower is better.
func badLine(text string, d time.Duration, speed float64) (string, float64) {
	words := len(strings.Fields(text))
	if speed <= 0 {
		speed = 1
	}
	expect := float64(words) / 2.6 / speed // seconds: about 156 words a minute
	got := d.Seconds()
	if words == 0 || got <= 0 {
		return "", 0
	}
	score := math.Abs(math.Log(got / expect))
	switch {
	case words < 4:
		return "", score // too short to judge
	case got > expect*2+1.5:
		return "much longer than its words: it may repeat itself or babble", score
	case got < expect*0.4:
		return "much shorter than its words: it may leave some out", score
	}
	return "", score
}

// takeOf is the tune for the n-th reading of a piece: the first as asked, the
// others another take (the kept take moved on, so a remake still reads the same).
func takeOf(t *rtvTune, n int) *rtvTune {
	if n == 0 || t == nil || t.Seed == 0 {
		return t // no kept take: each reading is a new one anyway
	}
	c := *t
	c.Seed = (t.Seed+int64(n)*7919)%math.MaxInt32 + 1
	return &c
}

// writeFLAC writes an engine's audio as the FLAC a piece is kept as (mono,
// 24 kHz; written to a file, so its length is in its header).
func writeFLAC(ctx context.Context, data []byte, ext, out string) error {
	cmd := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error", "-nostdin", "-y", "-f", ext, "-i", "pipe:0",
		"-ar", "24000", "-ac", "1", "-c:a", "flac", out)
	cmd.Stdin = bytes.NewReader(data)
	if msg, err := cmd.CombinedOutput(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("the voice engine's audio couldn't be read: %s", firstLine(strings.TrimSpace(string(msg))))
	}
	return nil
}

// speakLine speaks one piece into file. With Chatterbox a piece whose length
// doesn't fit its text is read again, up to rtvTakes times, and the best kept;
// it says why that one may still be read badly ("" when it is fine).
func (s *Server) speakLine(ctx context.Context, e rtvEngine, voice, text string, speed float64, tune *rtvTune, file string) (string, error) {
	tries := 1
	if e.name() == engineChatterbox {
		tries = rtvTakes
	}
	best, bestFlag, bestScore := file+".best.flac", "", math.Inf(1)
	defer os.Remove(best)
	for n := 0; n < tries; n++ {
		raw, ext, err := speakPiece(ctx, tuned(e, takeOf(tune, n)), voice, text, speed)
		if err != nil {
			return "", err
		}
		take := file + ".take.flac"
		if err := writeFLAC(ctx, raw, ext, take); err != nil {
			return "", err
		}
		d, err := audioDuration(ctx, take)
		if err != nil {
			return "", err
		}
		flag, score := badLine(text, d, speed)
		if score < bestScore || n == 0 {
			bestFlag, bestScore = flag, score
			if err := os.Rename(take, best); err != nil {
				return "", err
			}
		} else {
			_ = os.Remove(take)
		}
		if tries == 1 || flag == "" {
			break
		}
		if n < tries-1 {
			slog.Info("read-to-me: a line didn't fit its words; reading it again", "why", flag, "take", n+2)
		}
	}
	if tries == 1 {
		bestFlag = "" // Kokoro reads what it is given
	}
	return bestFlag, os.Rename(best, file)
}

// spliceLine puts a new reading of a line in a chapter, in the place of the
// old one (from start to end), into out. All of it lossless.
func spliceLine(ctx context.Context, chapter, piece string, start, end time.Duration, out string) error {
	f := fmt.Sprintf("[0:a]asplit=2[x][y];[x]atrim=end=%.3f,asetpts=PTS-STARTPTS[a];[y]atrim=start=%.3f,asetpts=PTS-STARTPTS[c];[a][1:a][c]concat=n=3:v=0:a=1[o]", start.Seconds(), end.Seconds())
	return ffmpeg(ctx, "-i", chapter, "-i", piece, "-filter_complex", f, "-map", "[o]", "-ar", "24000", "-ac", "1", "-c:a", "flac", out)
}

// ---- the makings ----

func (s *Server) makingsDir(bookID int64) string {
	return filepath.Join(s.cfg.ConfigDir, "readtome", "made", strconv.FormatInt(bookID, 10))
}

func readLines(file string) []rtvLine {
	var lines []rtvLine
	if data, err := os.ReadFile(file); err == nil {
		_ = json.Unmarshal(data, &lines)
	}
	return lines
}

// keepMakings moves a finished job's chapters and cover into the book's
// makings folder (in place of a remade one's) with their map.
func (s *Server) keepMakings(bookID int64, work string, m rtvMakings, cover string) error {
	dir := s.makingsDir(bookID)
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, c := range m.Chapters {
		if err := os.Rename(filepath.Join(work, c.File), filepath.Join(dir, c.File)); err != nil {
			_ = os.RemoveAll(dir)
			return err
		}
	}
	if cover != "" {
		_ = os.Rename(cover, filepath.Join(dir, "cover.jpg"))
	}
	return s.saveMakings(bookID, m)
}

func (s *Server) saveMakings(bookID int64, m rtvMakings) error {
	data, _ := json.Marshal(m)
	f := filepath.Join(s.makingsDir(bookID), "map.json")
	if err := os.WriteFile(f+".part", data, 0o644); err != nil {
		return err
	}
	return os.Rename(f+".part", f)
}

// makingsOf is a made audiobook's makings, when they are still kept.
func (s *Server) makingsOf(bookID int64) (rtvMakings, bool) {
	var m rtvMakings
	data, err := os.ReadFile(filepath.Join(s.makingsDir(bookID), "map.json"))
	if err != nil || json.Unmarshal(data, &m) != nil || len(m.Chapters) == 0 {
		return m, false
	}
	for _, c := range m.Chapters {
		if !rtvExists(filepath.Join(s.makingsDir(bookID), c.File)) {
			return m, false
		}
	}
	return m, true
}

func dirSize(dir string) int64 {
	var n int64
	_ = filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if fi, err := d.Info(); err == nil {
				n += fi.Size()
			}
		}
		return nil
	})
	return n
}

var rtvTidied struct {
	sync.Mutex
	at time.Time
}

// tidyMakings (when nothing is being made, at most hourly) lets go of makings
// older than 30 days, and of those whose audiobook is gone.
func (s *Server) tidyMakings(ctx context.Context) {
	rtvTidied.Lock()
	if time.Since(rtvTidied.at) < time.Hour {
		rtvTidied.Unlock()
		return
	}
	rtvTidied.at = time.Now()
	rtvTidied.Unlock()
	root := filepath.Join(s.cfg.ConfigDir, "readtome", "made")
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		id, err := strconv.ParseInt(e.Name(), 10, 64)
		if err != nil || !e.IsDir() {
			continue
		}
		fi, err := os.Stat(filepath.Join(root, e.Name(), "map.json"))
		_, made := s.madeAudiobook(ctx, id)
		if err != nil || time.Since(fi.ModTime()) > rtvMakingsKeep || !made {
			_ = os.RemoveAll(filepath.Join(root, e.Name()))
		}
	}
}

// ---- a redo job ----

// runRedo reads again a line or a chapter of a made audiobook, from its
// makings, and puts the audiobook together anew. The audiobook and its makings
// change only once all of it is done.
func (s *Server) runRedo(ctx context.Context, j rtvJob) error {
	r := *j.Options.Redo
	b, err := s.BookRepo.Get(j.BookID)
	if err != nil {
		return errors.New("the book isn't in the library any more")
	}
	e, on := s.readEngine(j.Engine)
	if !on {
		return fmt.Errorf("the %s voice engine isn't set up any more", j.Engine)
	}
	m, ok := s.makingsOf(b.ID)
	if !ok {
		return errors.New("what this audiobook was made of isn't kept any more; remake it instead")
	}
	if r.Chapter < 0 || r.Chapter >= len(m.Chapters) {
		return errors.New("that chapter isn't in the audiobook")
	}
	dir := s.makingsDir(b.ID)
	mc := m.Chapters[r.Chapter]
	work := s.readToMeCache("jobs", strconv.FormatInt(j.ID, 10))
	if err := os.MkdirAll(work, 0o755); err != nil {
		return fmt.Errorf("the work folder couldn't be made: %w", err)
	}
	defer os.RemoveAll(work)
	p := &rtvProgress{chapter: r.Chapter + 1, chapters: len(m.Chapters), spoken: j.SpokenMs}
	chapterFile := filepath.Join(work, "chapter.flac")
	var lines []rtvLine
	if r.Line < 0 {
		// The whole chapter, from the book as it reads now (its text may have been put right).
		book, err := s.readingBook(ctx, b)
		if err != nil {
			return err
		}
		if mc.Index >= len(book.Chapters) {
			return errors.New("the ebook has changed since the audiobook was made; remake it instead")
		}
		plan, cb := s.planChapters(ctx, j, b, book, e, []int{mc.Index}, mc.Intro, func(note string) { s.showProgress(j, p, note) })
		p.total = len(plan[0].pieces)
		if lines, err = s.speakChapter(ctx, j, e, cb, plan[0], r.Chapter+1, work, chapterFile, p); err != nil {
			return err
		}
	} else {
		if r.Line >= len(mc.Lines) {
			return errors.New("that line isn't in the chapter")
		}
		l := mc.Lines[r.Line]
		text := l.Text
		if strings.TrimSpace(r.Text) != "" {
			text = speakable(r.Text, s.sayRules(ctx, b.ID))
		}
		le, ok := s.readEngine(l.Engine)
		if !ok {
			return fmt.Errorf("the %s voice engine isn't set up any more", l.Engine)
		}
		p.total = 1
		if err := s.waitToSpeak(ctx, j, p.chapter, p.chapters, 0, 1, p.spoken); err != nil {
			return err
		}
		if le.name() == engineChatterbox {
			if err := s.ensureChatterbox(ctx, j.Options.CbModel, func(note string) { s.showProgress(j, p, note) }); err != nil {
				return err
			}
		}
		// Another take: the seed asked for, else a new one.
		tune := rtvTune{}
		if j.Options.Tune != nil {
			tune = *j.Options.Tune
		}
		tune.Seed = r.Seed
		if tune.Seed == 0 {
			tune.Seed = 1 + rand.Int64N(math.MaxInt32-1)
		}
		piece := filepath.Join(work, "line.flac")
		began := time.Now()
		flag, err := s.speakLine(ctx, le, l.Voice, text, j.Speed, &tune, piece)
		if err != nil {
			return err
		}
		p.spoken += time.Since(began).Milliseconds()
		d, err := audioDuration(ctx, piece)
		if err != nil {
			return err
		}
		start, end := time.Duration(l.StartMs)*time.Millisecond, time.Duration(l.EndMs)*time.Millisecond
		if err := spliceLine(ctx, filepath.Join(dir, mc.File), piece, start, end, chapterFile); err != nil {
			return fmt.Errorf("the line couldn't be put in its chapter: %w", err)
		}
		shift := d.Milliseconds() - (l.EndMs - l.StartMs)
		lines = append([]rtvLine(nil), mc.Lines...)
		lines[r.Line] = rtvLine{Text: text, Engine: l.Engine, Voice: l.Voice, StartMs: l.StartMs, EndMs: l.StartMs + d.Milliseconds(), Flag: flag}
		for k := r.Line + 1; k < len(lines); k++ {
			lines[k].StartMs += shift
			lines[k].EndMs += shift
		}
		p.done = 1
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	// The audiobook anew, with this chapter.
	s.setJobProgress(j.ID, jobRunning, p.chapter, p.chapters, p.total, p.total, "Putting the audiobook together", p.spoken)
	var files, titles []string
	for i, c := range m.Chapters {
		f := filepath.Join(dir, c.File)
		if i == r.Chapter {
			f = chapterFile
		}
		files = append(files, f)
		titles = append(titles, c.Title)
	}
	cover := filepath.Join(dir, "cover.jpg")
	if !rtvExists(cover) {
		cover = ""
	}
	if _, _, _, err := s.fileAudiobook(ctx, j, b, e, files, titles, cover); err != nil {
		return err
	}
	if err := os.Rename(chapterFile, filepath.Join(dir, mc.File)); err != nil {
		return err
	}
	m.Chapters[r.Chapter].Lines = lines
	if err := s.saveMakings(b.ID, m); err != nil {
		return err
	}
	// Done: the made audiobook is as it was, with this read again; the redo isn't kept.
	_, _ = s.db.Exec(`DELETE FROM readtome_jobs WHERE id = ?`, j.ID)
	_, _ = s.db.Exec(`UPDATE readtome_jobs SET done_ms = ? WHERE book_id = ? AND state = ?`, time.Now().UnixMilli(), b.ID, jobDone)
	what := "a line"
	if r.Line < 0 {
		what = "a chapter"
	}
	slog.Info("read-to-me: "+what+" was read again", "book", b.Title, "chapter", r.Chapter+1)
	return nil
}

// ---- routes ----

// madeFromPath is the made audiobook a route names, when it is the book's
// current one.
func (s *Server) madeFromPath(w http.ResponseWriter, r *http.Request) (rtvJob, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a made audiobook.")
		return rtvJob{}, false
	}
	j, err := s.getJob(r.Context(), id)
	if err == nil {
		if made, ok := s.madeAudiobook(r.Context(), j.BookID); ok && made.ID == j.ID {
			return made, true
		}
	}
	writeError(w, http.StatusNotFound, "That audiobook isn't one Read-to-Me made.")
	return rtvJob{}, false
}

type madeLine struct {
	rtvLine
	Index int `json:"index"`
}

// GET /api/home/readtome/made/{id}/lines[?chapter=]: the chapters, the lines
// to check in all of them, and a chapter's lines (the first with a line to
// check, unless one is asked for).
func (s *Server) handleReadToMeMadeLines(w http.ResponseWriter, r *http.Request) {
	j, ok := s.madeFromPath(w, r)
	if !ok {
		return
	}
	m, kept := s.makingsOf(j.BookID)
	type chapterInfo struct {
		Title   string `json:"title"`
		Lines   int    `json:"lines"`
		Flagged int    `json:"flagged"`
	}
	type flaggedLine struct {
		madeLine
		Chapter int `json:"chapter"`
	}
	out := struct {
		Kept     bool          `json:"kept"`
		Chapters []chapterInfo `json:"chapters"`
		Flagged  []flaggedLine `json:"flagged"`
		Chapter  int           `json:"chapter"`
		Lines    []madeLine    `json:"lines"`
	}{Kept: kept, Chapters: []chapterInfo{}, Flagged: []flaggedLine{}, Lines: []madeLine{}, Chapter: -1}
	if !kept {
		writeJSON(w, http.StatusOK, out)
		return
	}
	for ci, c := range m.Chapters {
		info := chapterInfo{Title: c.Title, Lines: len(c.Lines)}
		for li, l := range c.Lines {
			if l.Flag != "" {
				info.Flagged++
				if len(out.Flagged) < 200 {
					out.Flagged = append(out.Flagged, flaggedLine{madeLine{l, li}, ci})
				}
			}
		}
		if info.Flagged > 0 && out.Chapter < 0 {
			out.Chapter = ci
		}
		out.Chapters = append(out.Chapters, info)
	}
	if c, err := strconv.Atoi(r.URL.Query().Get("chapter")); err == nil && c >= 0 && c < len(m.Chapters) {
		out.Chapter = c
	}
	if out.Chapter < 0 {
		out.Chapter = 0
	}
	for li, l := range m.Chapters[out.Chapter].Lines {
		out.Lines = append(out.Lines, madeLine{l, li})
	}
	writeJSON(w, http.StatusOK, out)
}

// GET /api/home/readtome/made/{id}/line?chapter=&line=: one line as it is in
// the audiobook, as an MP3.
func (s *Server) handleReadToMeMadeLine(w http.ResponseWriter, r *http.Request) {
	j, ok := s.madeFromPath(w, r)
	if !ok {
		return
	}
	m, kept := s.makingsOf(j.BookID)
	c, err1 := strconv.Atoi(r.URL.Query().Get("chapter"))
	li, err2 := strconv.Atoi(r.URL.Query().Get("line"))
	if !kept || err1 != nil || err2 != nil || c < 0 || c >= len(m.Chapters) || li < 0 || li >= len(m.Chapters[c].Lines) {
		writeError(w, http.StatusNotFound, "That line isn't kept.")
		return
	}
	l := m.Chapters[c].Lines[li]
	start := max(0, float64(l.StartMs)/1000-0.2)
	cmd := exec.CommandContext(r.Context(), "ffmpeg", "-hide_banner", "-loglevel", "error", "-nostdin",
		"-ss", strconv.FormatFloat(start, 'f', 3, 64), "-to", strconv.FormatFloat(float64(l.EndMs)/1000+0.3, 'f', 3, 64),
		"-i", filepath.Join(s.makingsDir(j.BookID), m.Chapters[c].File), "-f", "mp3", "-b:a", "96k", "pipe:1")
	data, err := cmd.Output()
	if err != nil || len(data) == 0 {
		writeError(w, http.StatusInternalServerError, "That line couldn't be read from the audiobook.")
		return
	}
	sendAudio(w, data)
}

// POST /api/home/readtome/made/{id}/redo {chapter, line (-1: the whole
// chapter), text?, seed?}: reads it again (a job in the queue), with the
// settings the audiobook was made with.
func (s *Server) handleReadToMeRedo(w http.ResponseWriter, r *http.Request) {
	j, ok := s.madeFromPath(w, r)
	if !ok {
		return
	}
	if !s.mayChangeMade(r, j) {
		writeError(w, http.StatusForbidden, "Only an administrator or the person who made it can change this audiobook.")
		return
	}
	var req rtvRedo
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	m, kept := s.makingsOf(j.BookID)
	switch {
	case !kept:
		writeError(w, http.StatusConflict, "What this audiobook was made of isn't kept any more (it is kept for 30 days). Remake it instead.")
		return
	case req.Chapter < 0 || req.Chapter >= len(m.Chapters):
		writeError(w, http.StatusBadRequest, "That chapter isn't in the audiobook.")
		return
	case req.Line < -1 || req.Line >= len(m.Chapters[req.Chapter].Lines):
		writeError(w, http.StatusBadRequest, "That line isn't in the chapter.")
		return
	case len(req.Text) > 2000:
		writeError(w, http.StatusBadRequest, "A line is at most 2000 letters.")
		return
	}
	if req.Line < 0 {
		req.Text, req.Seed = "", 0
	}
	if req.Seed < 0 || req.Seed > math.MaxInt32 {
		req.Seed = 0
	}
	var active int
	_ = s.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM readtome_jobs WHERE book_id = ? AND state IN (?, ?, ?)`, j.BookID, jobQueued, jobRunning, jobWaiting).Scan(&active)
	if active > 0 {
		writeError(w, http.StatusConflict, "This audiobook is being made or changed already. Wait for that to finish.")
		return
	}
	o := j.Options
	o.Redo = &req
	opts, _ := json.Marshal(o)
	res, err := s.db.ExecContext(r.Context(), `INSERT INTO readtome_jobs (book_id, user_id, engine, voice, speed, created_ms, replaces, options) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		j.BookID, requestUserID(r), j.Engine, j.Voice, j.Speed, time.Now().UnixMilli(), j.OutputPath, string(opts))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	id, _ := res.LastInsertId()
	s.readToMe().poke()
	job, _ := s.getJob(r.Context(), id)
	writeJSON(w, http.StatusOK, s.jobView(job))
}

// DELETE /api/home/readtome/made/{id}/makings: lets go of what a made
// audiobook was made of (its lines can't be read again after; a remake can).
func (s *Server) handleReadToMeFreeMakings(w http.ResponseWriter, r *http.Request) {
	j, ok := s.madeFromPath(w, r)
	if !ok {
		return
	}
	if !s.mayChangeMade(r, j) {
		writeError(w, http.StatusForbidden, "Only an administrator or the person who made it can change this audiobook.")
		return
	}
	var active int
	_ = s.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM readtome_jobs WHERE book_id = ? AND state IN (?, ?, ?)`, j.BookID, jobQueued, jobRunning, jobWaiting).Scan(&active)
	if active > 0 {
		writeError(w, http.StatusConflict, "This audiobook is being changed. Wait for that to finish.")
		return
	}
	_ = os.RemoveAll(s.makingsDir(j.BookID))
	w.WriteHeader(http.StatusNoContent)
}

// makingsView is what a made audiobook's entry says of its makings.
func (s *Server) makingsView(bookID int64) (kept bool, size int64, flagged int) {
	m, ok := s.makingsOf(bookID)
	if !ok {
		return false, 0, 0
	}
	return true, dirSize(s.makingsDir(bookID)), m.flagged()
}

// forgetMakings lets go of a book's makings (its audiobook was deleted).
func (s *Server) forgetMakings(bookID int64) { _ = os.RemoveAll(s.makingsDir(bookID)) }
