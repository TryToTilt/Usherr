package api

// Usherr: Read-to-Me's jobs. A job makes one audiobook from one ebook: the EPUB
// is read as chapters, each chapter is cut into pieces and spoken piece by piece
// (one at a time, there is one graphics card), the pieces are joined into a
// chapter and the chapters into one M4B with chapter marks and the book's cover,
// which is filed with the audiobooks. The pieces are kept on disk while the job
// runs, so a job stopped by a restart carries on where it was.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/books"
	"github.com/rdborg/mediarium/internal/notify"
	"github.com/rdborg/mediarium/internal/organizer"
)

const (
	jobQueued    = "queued"
	jobRunning   = "running"
	jobWaiting   = "waiting" // held back (night only, something playing)
	jobDone      = "done"
	jobFailed    = "failed"
	jobCancelled = "cancelled"

	gapPiece   = 150 * time.Millisecond  // between pieces of one paragraph
	gapPara    = 600 * time.Millisecond  // after a paragraph
	gapScene   = 1500 * time.Millisecond // at a scene break
	gapChapter = 2500 * time.Millisecond // after a chapter
)

// rtvOptions are a job's choices of what is read. The zero value reads it all.
type rtvOptions struct {
	Chapters []int `json:"chapters,omitempty"` // the chapters to read (from 0); none: all
	NoIntro  bool  `json:"noIntro,omitempty"`  // don't start with the title and author
	NoTitles bool  `json:"noTitles,omitempty"` // don't read the chapters' titles
	// The pauses, in milliseconds (0: the usual one).
	ParaMs    int `json:"paraMs,omitempty"`
	SceneMs   int `json:"sceneMs,omitempty"`
	ChapterMs int `json:"chapterMs,omitempty"`
	// The Chatterbox model (standard, turbo; "": the household's default).
	CbModel string `json:"cbModel,omitempty"`
	// Other voices for the dialogue (home_readtome_cast.go); nil: one voice reads it all.
	Cast *rtvCast `json:"cast,omitempty"`
	// A job that reads again a line or a chapter of a made audiobook (home_readtome_redo.go).
	Redo *rtvRedo `json:"redo,omitempty"`
	// How Chatterbox reads (home_readtome_voices.go); nil: its usual way.
	Tune *rtvTune `json:"tune,omitempty"`
}

// clamped keeps the pauses within what makes sense.
func (o rtvOptions) clamped() rtvOptions {
	clamp := func(v, lo, hi int) int {
		if v == 0 {
			return 0
		}
		return max(lo, min(hi, v))
	}
	o.ParaMs = clamp(o.ParaMs, 200, 2000)
	o.SceneMs = clamp(o.SceneMs, 500, 4000)
	o.ChapterMs = clamp(o.ChapterMs, 1000, 6000)
	o.Tune = o.Tune.clamped()
	if o.Cast != nil && len(o.Cast.People) > maxCastPeople {
		o.Cast = nil
	}
	if o.Cast != nil && o.Cast.Dialogue == nil && o.Cast.Men == nil && o.Cast.Women == nil && len(o.Cast.People) == 0 {
		o.Cast = nil // nobody gets another voice
	}
	return o
}

// gap is a pause: the one chosen, else the usual one.
func gap(chosen int, usual time.Duration) time.Duration {
	if chosen > 0 {
		return time.Duration(chosen) * time.Millisecond
	}
	return usual
}

func parseOptions(s string) rtvOptions {
	var o rtvOptions
	_ = json.Unmarshal([]byte(s), &o)
	return o
}

// chapterIndexes is which of a book's n chapters are read, in order.
func (o rtvOptions) chapterIndexes(n int) []int {
	if picked := sortedChapters(o.Chapters, n); len(picked) > 0 {
		return picked
	}
	all := make([]int, n)
	for i := range all {
		all[i] = i
	}
	return all
}

type rtvJob struct {
	ID          int64      `json:"id"`
	BookID      int64      `json:"bookId"`
	UserID      int64      `json:"-"`
	Engine      string     `json:"engine"`
	Voice       string     `json:"voice"`
	Speed       float64    `json:"speed"`
	State       string     `json:"state"`
	Chapter     int        `json:"chapter"`
	Chapters    int        `json:"chapters"`
	PiecesDone  int        `json:"piecesDone"`
	PiecesTotal int        `json:"piecesTotal"`
	Note        string     `json:"note,omitempty"`
	Error       string     `json:"error,omitempty"`
	OutputPath  string     `json:"outputPath,omitempty"`
	CreatedMs   int64      `json:"createdMs"`
	StartedMs   int64      `json:"startedMs,omitempty"`
	DoneMs      int64      `json:"doneMs,omitempty"`
	SpokenMs    int64      `json:"-"`
	Options     rtvOptions `json:"options"`
	Replaces    string     `json:"-"` // a remake: the audiobook it takes the place of
	Remake      bool       `json:"remake,omitempty"`
	// For the page.
	Title  string `json:"title,omitempty"`
	Author string `json:"author,omitempty"`
	EtaSec int    `json:"etaSec,omitempty"`
	// Chapters finished so far (1 to n), to listen to while the rest is made.
	ReadyChapters int `json:"readyChapters,omitempty"`
}

const jobColumns = `id, book_id, user_id, engine, voice, speed, state, chapter, chapters, pieces_done, pieces_total, note, error, output_path, created_ms, started_ms, done_ms, spoken_ms, replaces, options`

func scanJob(row interface{ Scan(...any) error }) (rtvJob, error) {
	var j rtvJob
	var opts string
	err := row.Scan(&j.ID, &j.BookID, &j.UserID, &j.Engine, &j.Voice, &j.Speed, &j.State, &j.Chapter, &j.Chapters, &j.PiecesDone, &j.PiecesTotal, &j.Note, &j.Error, &j.OutputPath, &j.CreatedMs, &j.StartedMs, &j.DoneMs, &j.SpokenMs, &j.Replaces, &opts)
	j.Remake = j.Replaces != ""
	j.Options = parseOptions(opts)
	return j, err
}

func (s *Server) getJob(ctx context.Context, id int64) (rtvJob, error) {
	return scanJob(s.db.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM readtome_jobs WHERE id = ?`, id))
}

// view adds what the page shows: the book, and how long is left.
func (s *Server) jobView(j rtvJob) rtvJob {
	if b, err := s.BookRepo.Get(j.BookID); err == nil {
		j.Title, j.Author = b.Title, b.Author
	}
	if (j.State == jobRunning || j.State == jobWaiting) && j.PiecesDone > 3 && j.PiecesTotal > j.PiecesDone {
		per := float64(j.SpokenMs) / float64(j.PiecesDone)
		j.EtaSec = int(per * float64(j.PiecesTotal-j.PiecesDone) / 1000)
	}
	if (j.State == jobRunning || j.State == jobWaiting) && j.Options.Redo == nil {
		for rtvExists(s.jobChapterFile(j.ID, j.ReadyChapters+1)) {
			j.ReadyChapters++ // finished: it can be heard while the rest is made
		}
	}
	return j
}

// ---- the queue ----

type rtvRunner struct {
	wake    chan struct{}
	mu      sync.Mutex
	cancels map[int64]context.CancelFunc
}

var rtvRunners sync.Map // *Server -> *rtvRunner

// readToMe is the server's job runner, started the first time it's needed.
func (s *Server) readToMe() *rtvRunner {
	if v, ok := rtvRunners.Load(s); ok {
		return v.(*rtvRunner)
	}
	r := &rtvRunner{wake: make(chan struct{}, 1), cancels: map[int64]context.CancelFunc{}}
	if v, loaded := rtvRunners.LoadOrStore(s, r); loaded {
		return v.(*rtvRunner)
	}
	// Jobs a restart stopped go back in line.
	_, _ = s.db.Exec(`UPDATE readtome_jobs SET state = ?, note = '' WHERE state IN (?, ?)`, jobQueued, jobRunning, jobWaiting)
	go func() {
		for {
			for s.runNextReadToMeJob(context.Background()) {
			}
			s.readToMeIdle(context.Background()) // Chatterbox goes when nothing needs it
			select {
			case <-r.wake:
			case <-time.After(time.Minute):
			}
		}
	}()
	return r
}

func (r *rtvRunner) poke() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// readToMeJob (an automation job, a little after start-up and then hourly)
// starts the runner when jobs are waiting, such as those a restart stopped.
func (s *Server) readToMeJob(context.Context) {
	var n int
	if s.db.QueryRow(`SELECT COUNT(*) FROM readtome_jobs WHERE state IN (?, ?, ?)`, jobQueued, jobRunning, jobWaiting).Scan(&n) == nil && n > 0 {
		s.readToMe().poke()
	}
}

// runNextReadToMeJob runs the oldest job in line, if there is one.
func (s *Server) runNextReadToMeJob(base context.Context) bool {
	row := s.db.QueryRowContext(base, `SELECT `+jobColumns+` FROM readtome_jobs WHERE state = ? ORDER BY id LIMIT 1`, jobQueued)
	j, err := scanJob(row)
	if err != nil {
		return false
	}
	r := s.readToMe()
	ctx, cancel := context.WithCancel(base)
	r.mu.Lock()
	r.cancels[j.ID] = cancel
	r.mu.Unlock()
	defer func() {
		cancel()
		r.mu.Lock()
		delete(r.cancels, j.ID)
		r.mu.Unlock()
	}()
	_, _ = s.db.Exec(`UPDATE readtome_jobs SET state = ?, started_ms = CASE WHEN started_ms = 0 THEN ? ELSE started_ms END WHERE id = ? AND state = ?`, jobRunning, time.Now().UnixMilli(), j.ID, jobQueued)
	err = s.runReadToMeJob(ctx, j)
	now := time.Now().UnixMilli()
	switch {
	case err == nil:
		// finished: runReadToMeJob has recorded it
	case errors.Is(err, context.Canceled):
		_, _ = s.db.Exec(`UPDATE readtome_jobs SET state = ?, note = '', done_ms = ? WHERE id = ?`, jobCancelled, now, j.ID)
		_ = os.RemoveAll(s.readToMeCache("jobs", strconv.FormatInt(j.ID, 10)))
	default:
		slog.Warn("read-to-me: a job failed", "job", j.ID, "err", err)
		_, _ = s.db.Exec(`UPDATE readtome_jobs SET state = ?, error = ?, note = '', done_ms = ? WHERE id = ?`, jobFailed, err.Error(), now, j.ID)
		if b, berr := s.BookRepo.Get(j.BookID); berr == nil && j.Options.Redo == nil {
			s.notifyEvent(notify.EventAudiobook, "Audiobook not made: "+b.Title, "Read-to-Me stopped: "+err.Error()+". What was spoken is kept; make it again to carry on.")
		}
	}
	return true
}

// ---- making the audiobook ----

func rtvExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Size() > 0
}

var punctOrSpace = regexp.MustCompile(`[^\p{L}\p{N}]+`)

func rtvSquash(s string) string { return strings.ToLower(punctOrSpace.ReplaceAllString(s, "")) }

// chapterPieces is what is spoken for a chapter: its title (unless the text
// starts with it, or titles aren't read), then its text, made ready to be read
// aloud.
func chapterPieces(ch rtvChapter, max int, rules []sayRule, titles bool) []speechPiece {
	return append(titlePieces(ch, rules, titles), splitSpeech(speakable(ch.Text, rules), max)...)
}

// titlePieces is a chapter's title, spoken, when it is read and the text doesn't start with it.
func titlePieces(ch rtvChapter, rules []sayRule, titles bool) []speechPiece {
	if t := strings.TrimSpace(ch.Title); titles && t != "" && !strings.HasPrefix(rtvSquash(ch.Text), rtvSquash(t)) {
		return []speechPiece{{Text: strings.TrimRight(speakableTitle(t, rules), ".") + ".", ParaEnd: true}}
	}
	return nil
}

func (s *Server) setJobProgress(id int64, state string, chapter, chapters, done, total int, note string, spokenMs int64) {
	_, _ = s.db.Exec(`UPDATE readtome_jobs SET state = ?, chapter = ?, chapters = ?, pieces_done = ?, pieces_total = ?, note = ?, spoken_ms = ? WHERE id = ? AND state IN (?, ?)`,
		state, chapter, chapters, done, total, note, spokenMs, id, jobRunning, jobWaiting)
}

func (s *Server) runReadToMeJob(ctx context.Context, j rtvJob) error {
	if j.Options.Redo != nil {
		return s.runRedo(ctx, j) // a line or a chapter of a made audiobook (home_readtome_redo.go)
	}
	b, err := s.BookRepo.Get(j.BookID)
	if err != nil {
		return errors.New("the book isn't in the library any more")
	}
	if b.EbookPath == "" {
		return errors.New("the book has no EPUB any more")
	}
	e, on := s.readEngine(j.Engine)
	if !on {
		return fmt.Errorf("the %s voice engine isn't set up any more", j.Engine)
	}
	book, err := s.readingBook(ctx, b)
	if err != nil {
		return err
	}
	work := s.readToMeCache("jobs", strconv.FormatInt(j.ID, 10))
	if err := os.MkdirAll(work, 0o755); err != nil {
		return fmt.Errorf("the work folder couldn't be made: %w", err)
	}

	// What will be spoken, chapter by chapter.
	plan, cb := s.planChapters(ctx, j, b, book, e, j.Options.chapterIndexes(len(book.Chapters)), !j.Options.NoIntro, func(note string) {
		s.setJobProgress(j.ID, jobRunning, 0, len(book.Chapters), 0, 0, note, j.SpokenMs)
	})
	p := &rtvProgress{chapters: len(plan), spoken: j.SpokenMs}
	for _, cp := range plan {
		p.total += len(cp.pieces)
	}
	var made rtvMakings
	var files, titles []string
	for ci, cp := range plan {
		p.chapter = ci + 1
		file := s.jobChapterFile(j.ID, ci+1)
		var lines []rtvLine
		if rtvExists(file) {
			p.done += len(cp.pieces) // what is on disk already is counted as it is passed
			lines = readLines(file + ".json")
		} else if lines, err = s.speakChapter(ctx, j, e, cb, cp, ci+1, work, file, p); err != nil {
			return err
		}
		files = append(files, file)
		titles = append(titles, cp.title)
		made.Chapters = append(made.Chapters, rtvMadeChapter{Index: cp.index, Title: cp.title, Intro: cp.intro, File: filepath.Base(file), Lines: lines})
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.setJobProgress(j.ID, jobRunning, len(plan), len(plan), p.total, p.total, "Putting the audiobook together", p.spoken)
	cover := ""
	if data, _, err := books.EPUBCover(b.EbookPath); err == nil {
		cover = filepath.Join(work, "cover.jpg")
		if err := imageToJPEG(ctx, data, cover); err != nil {
			cover = ""
		}
	}
	out, length, voiceName, err := s.fileAudiobook(ctx, j, b, e, files, titles, cover)
	if err != nil {
		return err
	}
	// What this replaced is gone from the list of made audiobooks.
	_, _ = s.db.Exec(`DELETE FROM readtome_jobs WHERE book_id = ? AND state = ? AND id <> ?`, b.ID, jobDone, j.ID)
	_, _ = s.db.Exec(`UPDATE readtome_jobs SET state = ?, note = '', output_path = ?, pieces_done = ?, spoken_ms = ?, done_ms = ? WHERE id = ?`, jobDone, out, p.total, p.spoken, time.Now().UnixMilli(), j.ID)
	// The chapters, losslessly, and where each line is: a line or a chapter can be read again.
	if err := s.keepMakings(b.ID, work, made, cover); err != nil {
		slog.Warn("read-to-me: the makings of an audiobook couldn't be kept", "book", b.Title, "err", err)
	}
	_ = os.RemoveAll(work)
	slog.Info("read-to-me: an audiobook was made", "book", b.Title, "voice", voiceName, "minutes", int(length.Minutes()), "lines to check", made.flagged())
	msg := fmt.Sprintf("Read by Usherr · %s, %s. It is in your Audiobooks library.", voiceName, lengthWords(length))
	if n := made.flagged(); n > 0 {
		msg += fmt.Sprintf(" %d may be read badly: Lines to check, on its entry in Read-to-Me.", n)
	}
	s.notifyEvent(notify.EventAudiobook, "Audiobook made: "+b.Title, msg)
	return nil
}

// lengthWords says a length of audio in words: "3 h 20 min", "45 min".
func lengthWords(d time.Duration) string {
	m := int(d.Round(time.Minute).Minutes())
	if m < 60 {
		return fmt.Sprintf("%d min", max(1, m))
	}
	return fmt.Sprintf("%d h %d min", m/60, m%60)
}

// rtvProgress is how far a job is, as the page shows it.
type rtvProgress struct {
	chapter, chapters, done, total int
	spoken                         int64 // time spent speaking, for the estimate
}

func (s *Server) showProgress(j rtvJob, p *rtvProgress, note string) {
	s.setJobProgress(j.ID, jobRunning, p.chapter, p.chapters, p.done, p.total, note, p.spoken)
}

type chapterPlan struct {
	index  int // the book's chapter
	title  string
	intro  bool // it starts with the book's title and author
	pieces []speechPiece
}

// planChapters is what will be spoken of the chapters picked, piece by piece
// (the book's title and author first, when intro). With a cast: who says what,
// the AI asked first if wanted, and pieces no longer than the most particular
// engine of the cast takes.
func (s *Server) planChapters(ctx context.Context, j rtvJob, b books.Book, book rtvBook, e rtvEngine, picks []int, intro bool, note func(string)) ([]chapterPlan, castBook) {
	rules := s.sayRules(ctx, b.ID)
	cast := j.Options.Cast
	var cb castBook
	max := e.maxPiece()
	if cast != nil {
		if cast.AI {
			if err := s.castAI(ctx, b.ID, book, rules, note); err != nil {
				slog.Warn("read-to-me: the AI couldn't go through the book's dialogue", "book", b.Title, "err", err)
			}
		}
		cb = s.castOf(ctx, b.ID, book, rules)
		max = s.castMax(e, cast)
	}
	var plan []chapterPlan
	for i, ci := range picks {
		ch := book.Chapters[ci]
		var ps []speechPiece
		if cast != nil {
			ps = append(titlePieces(ch, rules, !j.Options.NoTitles), castPieces(cb.Paras[ci], cb.Lines[ci], max)...)
		} else {
			ps = chapterPieces(ch, max, rules, !j.Options.NoTitles)
		}
		first := intro && i == 0
		if first {
			text := strings.TrimSpace(b.Title)
			if b.Author != "" {
				text += ", by " + b.Author
			}
			ps = append([]speechPiece{{Text: speakable(text, rules) + ".", ParaEnd: true}}, ps...)
		}
		plan = append(plan, chapterPlan{index: ci, title: ch.Title, intro: first, pieces: ps})
	}
	return plan, cb
}

// speakChapter speaks a chapter's pieces into work and joins them, with their
// pauses, into file; it says where each piece is in it. Pieces already in work
// are kept (a job a restart stopped carries on where it was).
func (s *Server) speakChapter(ctx context.Context, j rtvJob, e rtvEngine, cb castBook, cp chapterPlan, n int, work, file string, p *rtvProgress) ([]rtvLine, error) {
	var list []string
	var lines []rtvLine
	var at time.Duration
	for pi, piece := range cp.pieces {
		pieceFile := filepath.Join(work, fmt.Sprintf("c%03d-p%04d.flac", n, pi+1))
		pe, pv := e, j.Voice // who reads it: the narrator, or the speaker's voice in the cast
		if ref := j.Options.Cast.voiceFor(piece.Speaker, cb.Genders); ref != nil {
			if ce, ok := s.readEngine(ref.Engine); ok {
				pe, pv = ce, ref.Voice
			}
		}
		flag := ""
		if !rtvExists(pieceFile) {
			if err := s.waitToSpeak(ctx, j, p.chapter, p.chapters, p.done, p.total, p.spoken); err != nil {
				return nil, err
			}
			if pe.name() == engineChatterbox {
				if err := s.ensureChatterbox(ctx, j.Options.CbModel, func(note string) { s.showProgress(j, p, note) }); err != nil {
					return nil, err
				}
			}
			began := time.Now()
			var err error
			if flag, err = s.speakLine(ctx, pe, pv, piece.Text, j.Speed, j.Options.Tune, pieceFile); err != nil {
				return nil, err
			}
			if flag != "" {
				_ = os.WriteFile(pieceFile+".flag", []byte(flag), 0o644)
			}
			p.spoken += time.Since(began).Milliseconds()
		} else if f, err := os.ReadFile(pieceFile + ".flag"); err == nil {
			flag = string(f)
		}
		d, err := audioDuration(ctx, pieceFile)
		if err != nil {
			return nil, err
		}
		lines = append(lines, rtvLine{Text: piece.Text, Engine: pe.name(), Voice: pv, StartMs: at.Milliseconds(), EndMs: (at + d).Milliseconds(), Flag: flag})
		at += d
		p.done++
		list = append(list, pieceFile)
		if pi < len(cp.pieces)-1 {
			pause := gapPiece
			switch {
			case piece.SceneEnd:
				pause = gap(j.Options.SceneMs, gapScene)
			case piece.ParaEnd:
				pause = gap(j.Options.ParaMs, gapPara)
			}
			sil, err := silenceFile(ctx, work, pause)
			if err != nil {
				return nil, err
			}
			list = append(list, sil)
			at += pause
		}
		s.showProgress(j, p, "")
	}
	sil, err := silenceFile(ctx, work, gap(j.Options.ChapterMs, gapChapter))
	if err != nil {
		return nil, err
	}
	list = append(list, sil)
	data, _ := json.Marshal(lines)
	if err := os.WriteFile(file+".json", data, 0o644); err != nil {
		return nil, err
	}
	if err := concatAudio(ctx, work, list, file); err != nil {
		return nil, err
	}
	for _, f := range list {
		if strings.Contains(filepath.Base(f), "-p") {
			_ = os.Remove(f) // the pieces are in the chapter now
			_ = os.Remove(f + ".flag")
		}
	}
	return lines, nil
}

// fileAudiobook puts the chapters together into the M4B (in the place of the
// one a remake or a redo replaces) and tells the library. It says where it
// is, how long, and the voice's name.
func (s *Server) fileAudiobook(ctx context.Context, j rtvJob, b books.Book, e rtvEngine, chapterFiles, titles []string, cover string) (string, time.Duration, string, error) {
	var marks []chapterMark
	var at time.Duration
	for i, f := range chapterFiles {
		d, err := audioDuration(ctx, f)
		if err != nil {
			return "", 0, "", err
		}
		marks = append(marks, chapterMark{Title: titles[i], Start: at, End: at + d})
		at += d
	}
	folder, err := s.bookFolder(b, books.Audiobook)
	if err != nil {
		return "", 0, "", err
	}
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return "", 0, "", fmt.Errorf("the audiobook folder couldn't be made: %w", err)
	}
	mode, repl := s.illegalCharSettings()
	out := rtvFreeName(folder, organizer.Sanitize(b.Title, mode, repl), ".m4b")
	if j.Replaces != "" && rtvExists(j.Replaces) {
		out = j.Replaces // a remake takes the old one's place, once it is finished
		folder = filepath.Dir(out)
	}
	vs, _ := e.voices(ctx)
	voiceName := voiceName(vs, j.Voice)
	meta := bookMeta{Title: b.Title, Author: b.Author, ReadBy: "Read by Usherr · " + voiceName, Year: b.Year}
	jobs := s.readToMeCache("jobs")
	_ = os.MkdirAll(jobs, 0o755)
	tmp, err := os.MkdirTemp(jobs, "assemble-")
	if err != nil {
		return "", 0, "", err
	}
	defer os.RemoveAll(tmp)
	if err := assembleM4B(ctx, tmp, chapterFiles, marks, meta, cover, out+".part"); err != nil {
		_ = os.Remove(out + ".part")
		return "", 0, "", err
	}
	if err := os.Rename(out+".part", out); err != nil {
		// Some file systems won't rename over a file; the old one goes first then.
		if rmErr := os.Remove(out); rmErr != nil || os.Rename(out+".part", out) != nil {
			_ = os.Remove(out + ".part")
			return "", 0, "", fmt.Errorf("the audiobook couldn't be filed: %w", err)
		}
	}
	// The library knows it now.
	_ = s.BookRepo.SetWant(b.ID, books.Audiobook, true)
	if err := s.BookRepo.SetState(b.ID, books.Audiobook, books.StatusDownloaded, "m4b", folder); err != nil {
		return "", 0, "", err
	}
	_ = s.BookRepo.SetAudioDetails(b.ID, books.AudioDetails{Title: b.Title, Narrators: []string{"Usherr · " + voiceName}, RuntimeMin: int(at.Minutes())})
	return out, at, voiceName, nil
}

// waitToSpeak holds a job back while it shouldn't use the graphics card (night
// only, something playing; home_readtome_hold.go). It says why, in the job.
func (s *Server) waitToSpeak(ctx context.Context, j rtvJob, chapter, chapters, done, total int, spoken int64) error {
	held := time.Now()
	for {
		why := s.readToMeHold(ctx)
		if why == "" {
			return ctx.Err()
		}
		// Held a long while: Chatterbox gives the graphics card back (started again after).
		if time.Since(held) > rtvHeldStop {
			s.stopStartedChatterbox(ctx)
		}
		s.setJobProgress(j.ID, jobWaiting, chapter, chapters, done, total, why, spoken)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(rtvHoldPoll):
		}
	}
}

// rtvFreeName is a file name in dir that nothing uses yet: name.ext, else
// "name (Read-to-Me).ext", else numbered.
func rtvFreeName(dir, name, ext string) string {
	p := filepath.Join(dir, name+ext)
	if _, err := os.Stat(p); err != nil {
		return p
	}
	p = filepath.Join(dir, name+" (Read-to-Me)"+ext)
	for n := 2; ; n++ {
		if _, err := os.Stat(p); err != nil {
			return p
		}
		p = filepath.Join(dir, fmt.Sprintf("%s (Read-to-Me %d)%s", name, n, ext))
	}
}

// ---- ffmpeg ----

func ffmpeg(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "ffmpeg", append([]string{"-hide_banner", "-loglevel", "error", "-nostdin", "-y"}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("ffmpeg: %s", firstLine(strings.TrimSpace(stderr.String())))
	}
	return nil
}

// silenceFile is a file of silence of length d, made once per job.
func silenceFile(ctx context.Context, dir string, d time.Duration) (string, error) {
	f := filepath.Join(dir, fmt.Sprintf("silence-%d.flac", d.Milliseconds()))
	if rtvExists(f) {
		return f, nil
	}
	err := ffmpeg(ctx, "-f", "lavfi", "-i", "anullsrc=r=24000:cl=mono", "-t", strconv.FormatFloat(d.Seconds(), 'f', 3, 64), "-c:a", "flac", f)
	return f, err
}

func concatList(dir, name string, files []string) (string, error) {
	var b strings.Builder
	for _, f := range files {
		abs, err := filepath.Abs(f)
		if err != nil {
			return "", err
		}
		b.WriteString("file '" + strings.ReplaceAll(filepath.ToSlash(abs), "'", `'\''`) + "'\n")
	}
	p := filepath.Join(dir, name)
	return p, os.WriteFile(p, []byte(b.String()), 0o644)
}

// concatAudio joins FLAC files end to end into one. It is encoded again (lossless):
// copying leaves the first file's length in the header.
func concatAudio(ctx context.Context, dir string, files []string, out string) error {
	list, err := concatList(dir, filepath.Base(out)+".txt", files)
	if err != nil {
		return err
	}
	if err := ffmpeg(ctx, "-f", "concat", "-safe", "0", "-i", list, "-c:a", "flac", out+".part.flac"); err != nil {
		return err
	}
	_ = os.Remove(list)
	return os.Rename(out+".part.flac", out)
}

func audioDuration(ctx context.Context, f string) (time.Duration, error) {
	out, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "default=nw=1:nk=1", f).Output()
	if err != nil {
		return 0, fmt.Errorf("a chapter's length couldn't be read: %w", err)
	}
	secs, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil {
		return 0, fmt.Errorf("a chapter's length couldn't be read")
	}
	return time.Duration(secs * float64(time.Second)), nil
}

func imageToJPEG(ctx context.Context, data []byte, out string) error {
	cmd := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error", "-nostdin", "-y", "-i", "pipe:0", "-frames:v", "1", "-q:v", "3", out)
	cmd.Stdin = bytes.NewReader(data)
	return cmd.Run()
}

type chapterMark struct {
	Title      string
	Start, End time.Duration
}

type bookMeta struct {
	Title, Author, ReadBy string
	Year                  int
}

// ffEscape escapes a value for an ffmpeg metadata file.
func ffEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, "=", `\=`, ";", `\;`, "#", `\#`, "\n", `\`+"\n")
	return r.Replace(s)
}

// ffMetadata is the metadata file with the chapter marks.
func ffMetadata(m bookMeta, marks []chapterMark) string {
	var b strings.Builder
	b.WriteString(";FFMETADATA1\n")
	b.WriteString("title=" + ffEscape(m.Title) + "\n")
	b.WriteString("album=" + ffEscape(m.Title) + "\n")
	if m.Author != "" {
		b.WriteString("artist=" + ffEscape(m.Author) + "\n")
		b.WriteString("album_artist=" + ffEscape(m.Author) + "\n")
	}
	b.WriteString("composer=" + ffEscape(m.ReadBy) + "\n")
	b.WriteString("comment=" + ffEscape(m.ReadBy+" (a computer voice)") + "\n")
	b.WriteString("genre=Audiobook\n")
	if m.Year > 0 {
		b.WriteString("date=" + strconv.Itoa(m.Year) + "\n")
	}
	for _, c := range marks {
		b.WriteString("[CHAPTER]\nTIMEBASE=1/1000\n")
		fmt.Fprintf(&b, "START=%d\nEND=%d\n", c.Start.Milliseconds(), c.End.Milliseconds())
		b.WriteString("title=" + ffEscape(c.Title) + "\n")
	}
	return b.String()
}

// assembleM4B joins the chapters into one M4B: speech at 64 kbit/s mono, the
// chapter marks, the book's details and its cover.
func assembleM4B(ctx context.Context, dir string, chapters []string, marks []chapterMark, m bookMeta, cover, out string) error {
	list, err := concatList(dir, "all.txt", chapters)
	if err != nil {
		return err
	}
	metaFile := filepath.Join(dir, "ffmeta.txt")
	if err := os.WriteFile(metaFile, []byte(ffMetadata(m, marks)), 0o644); err != nil {
		return err
	}
	args := []string{"-f", "concat", "-safe", "0", "-i", list, "-i", metaFile}
	if cover != "" {
		args = append(args, "-i", cover)
	}
	args = append(args, "-map", "0:a:0")
	if cover != "" {
		args = append(args, "-map", "2:v:0")
	}
	// The whole book at one loudness (EBU R128, as audiobook shops ask for).
	args = append(args, "-af", "loudnorm=I=-18:LRA=11:TP=-2")
	args = append(args, "-map_metadata", "1", "-map_chapters", "1", "-c:a", "aac", "-b:a", "64k", "-ac", "1", "-ar", "24000")
	if cover != "" {
		args = append(args, "-c:v", "copy", "-disposition:v:0", "attached_pic")
	}
	args = append(args, "-movflags", "+faststart", "-f", "ipod", out)
	if err := ffmpeg(ctx, args...); err != nil {
		return fmt.Errorf("the audiobook couldn't be put together: %w", err)
	}
	return nil
}

// ---- the routes ----

const maxQueuedJobs = 12

// POST /api/home/readtome/jobs {bookId, engine, voice, speed}: makes an audiobook of the book.
func (s *Server) handleReadToMeMake(w http.ResponseWriter, r *http.Request) {
	req, b, _, err := s.checkRead(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	replaces := ""
	if err := s.checkCast(r, req.options().Cast); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if b.AudioStatus == books.StatusDownloaded {
		made, ok := s.madeAudiobook(r.Context(), b.ID)
		switch {
		case !ok:
			writeError(w, http.StatusConflict, "This book already has an audiobook that Read-to-Me didn't make, and Read-to-Me never replaces those.")
			return
		case !req.Remake:
			writeError(w, http.StatusConflict, "This book already has an audiobook made by Read-to-Me. Use Remake to make it again.")
			return
		case !s.mayChangeMade(r, made):
			writeError(w, http.StatusForbidden, "Only an administrator or the person who made it can remake this audiobook.")
			return
		}
		replaces = made.OutputPath
	}
	var active, queued int
	_ = s.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM readtome_jobs WHERE book_id = ? AND state IN (?, ?, ?)`, b.ID, jobQueued, jobRunning, jobWaiting).Scan(&active)
	if active > 0 {
		writeError(w, http.StatusConflict, "An audiobook of this book is already being made.")
		return
	}
	_ = s.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM readtome_jobs WHERE state IN (?, ?, ?)`, jobQueued, jobRunning, jobWaiting).Scan(&queued)
	if queued >= maxQueuedJobs {
		writeError(w, http.StatusConflict, "Too many audiobooks are waiting to be made. Wait for some to finish.")
		return
	}
	opts, _ := json.Marshal(req.options())
	res, err := s.db.ExecContext(r.Context(), `INSERT INTO readtome_jobs (book_id, user_id, engine, voice, speed, created_ms, replaces, options) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		b.ID, requestUserID(r), req.Engine, req.Voice, req.Speed, time.Now().UnixMilli(), replaces, string(opts))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	id, _ := res.LastInsertId()
	s.readToMe().poke()
	j, _ := s.getJob(r.Context(), id)
	writeJSON(w, http.StatusOK, s.jobView(j))
}

// jobChapterFile is where a job keeps its n-th chapter (from 1) once it is spoken.
func (s *Server) jobChapterFile(id int64, n int) string {
	return filepath.Join(s.readToMeCache("jobs", strconv.FormatInt(id, 10)), fmt.Sprintf("chapter-%03d.flac", n))
}

// GET /api/home/readtome/jobs/{id}/chapters/{n}: a chapter already made of an
// audiobook still being made, as an MP3 (made as it is played).
func (s *Server) handleReadToMeEarlyChapter(w http.ResponseWriter, r *http.Request) {
	id, err1 := strconv.ParseInt(r.PathValue("id"), 10, 64)
	n, err2 := strconv.Atoi(r.PathValue("n"))
	if err1 != nil || err2 != nil || n < 1 {
		writeError(w, http.StatusNotFound, "That chapter isn't made yet.")
		return
	}
	j, err := s.getJob(r.Context(), id)
	file := s.jobChapterFile(id, n)
	if err != nil || (j.State != jobRunning && j.State != jobWaiting) || !rtvExists(file) {
		writeError(w, http.StatusNotFound, "That chapter isn't made yet (or the audiobook is finished: it is in your library).")
		return
	}
	w.Header().Set("Content-Type", "audio/mpeg")
	w.Header().Set("Cache-Control", "no-store")
	cmd := exec.CommandContext(r.Context(), "ffmpeg", "-hide_banner", "-loglevel", "error", "-nostdin", "-i", file, "-f", "mp3", "-b:a", "96k", "pipe:1")
	cmd.Stdout = w
	_ = cmd.Run()
}

// GET /api/home/readtome/jobs[?bookId=]: the jobs, the newest first.
func (s *Server) handleReadToMeJobs(w http.ResponseWriter, r *http.Request) {
	q := `SELECT ` + jobColumns + ` FROM readtome_jobs`
	var args []any
	if id, err := strconv.ParseInt(r.URL.Query().Get("bookId"), 10, 64); err == nil {
		q += ` WHERE book_id = ?`
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(r.Context(), q+` ORDER BY id DESC LIMIT 50`, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	out := []rtvJob{}
	for rows.Next() {
		if j, err := scanJob(rows); err == nil {
			out = append(out, s.jobView(j))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": out})
}

// DELETE /api/home/readtome/jobs/{id}: stops a job that is waiting or running;
// a finished one is taken off the list.
func (s *Server) handleReadToMeCancel(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a job.")
		return
	}
	j, err := s.getJob(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "That job isn't there.")
		return
	}
	switch j.State {
	case jobQueued:
		_, _ = s.db.ExecContext(r.Context(), `UPDATE readtome_jobs SET state = ?, done_ms = ? WHERE id = ? AND state = ?`, jobCancelled, time.Now().UnixMilli(), id, jobQueued)
	case jobRunning, jobWaiting:
		run := s.readToMe()
		run.mu.Lock()
		cancel := run.cancels[id]
		run.mu.Unlock()
		if cancel != nil {
			cancel() // the runner records it and clears the work folder
		}
	default:
		_, _ = s.db.ExecContext(r.Context(), `DELETE FROM readtome_jobs WHERE id = ?`, id)
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

// GET /api/home/readtome/book?bookId=: what making this book into an audiobook
// involves, for the dialog.
func (s *Server) handleReadToMeBook(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.URL.Query().Get("bookId"), 10, 64)
	b, err := s.BookRepo.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "That book isn't in the library.")
		return
	}
	out := map[string]any{"title": b.Title, "author": b.Author, "hasAudiobook": b.AudioStatus == books.StatusDownloaded, "canMake": false}
	if b.EbookPath != "" && strings.EqualFold(b.EbookFormat, "epub") && b.EbookStatus == books.StatusDownloaded {
		if book, err := s.readingBook(r.Context(), b); err == nil {
			words := 0
			for _, ch := range book.Chapters {
				words += len(strings.Fields(ch.Text))
			}
			type chapterInfo struct {
				Title  string `json:"title"`
				Words  int    `json:"words"`
				Edited bool   `json:"edited,omitempty"`
			}
			list := []chapterInfo{}
			for i, ch := range book.Chapters {
				title := strings.TrimSpace(ch.Title)
				if title == "" {
					title = fmt.Sprintf("Part %d", i+1)
				}
				list = append(list, chapterInfo{Title: title, Words: len(strings.Fields(ch.Text)), Edited: ch.Edited})
			}
			out["chapterList"] = list
			out["canMake"] = true
			out["chapters"] = len(book.Chapters)
			out["words"] = words
			out["audioMinutes"] = words / 150 // an audiobook is read at about 150 words a minute
		} else {
			out["problem"] = err.Error()
		}
	}
	if made, ok := s.madeAudiobook(r.Context(), id); ok {
		out["made"] = s.madeView(r, made)
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT `+jobColumns+` FROM readtome_jobs WHERE book_id = ? ORDER BY id DESC LIMIT 1`, id)
	if err == nil {
		defer rows.Close()
		if rows.Next() {
			if j, err := scanJob(rows); err == nil {
				out["job"] = s.jobView(j)
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}
