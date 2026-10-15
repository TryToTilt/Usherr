package organizer

// Home fork: how far along the long file steps are (PAR2 checking and
// repair, unpacking, copying into the library), so the queue can show how
// much time a step has left. ProgressHook is told the path being worked on
// (inside the download's folder), the step's name when it changes, and how
// much of it is done.

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ProgressHook, when set, hears progress. phase is "" when the step is the
// same as before. It must be quick: it is called from the copy loops.
var ProgressHook func(path, phase string, done, total int64)

func reportProgress(path, phase string, done, total int64) {
	if h := ProgressHook; h != nil && total > 0 {
		h(path, phase, done, total)
	}
}

// progressReader reports bytes read from r, at most every half second.
type progressReader struct {
	r     io.Reader
	path  string
	phase string
	done  int64
	total int64
	last  time.Time
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.done += int64(n)
	if now := time.Now(); now.Sub(p.last) >= 500*time.Millisecond || err == io.EOF {
		phase := ""
		if p.last.IsZero() {
			phase = p.phase
		}
		p.last = now
		reportProgress(p.path, phase, p.done, p.total)
	}
	return n, err
}

// progressWriter reports bytes written to w against a total, at most every
// half second.
type progressWriter struct {
	w     io.Writer
	path  string
	phase string
	done  *int64
	total int64
	last  *time.Time
}

func (p progressWriter) Write(b []byte) (int, error) {
	n, err := p.w.Write(b)
	*p.done += int64(n)
	if now := time.Now(); now.Sub(*p.last) >= 500*time.Millisecond {
		phase := ""
		if p.last.IsZero() {
			phase = p.phase
		}
		*p.last = now
		reportProgress(p.path, phase, *p.done, p.total)
	}
	return n, err
}

var (
	par2Files      = regexp.MustCompile(`There are (\d+) recoverable files`)
	par2TargetLine = regexp.MustCompile(`^Target: ".*" - `)
	par2Scanning   = regexp.MustCompile(`^Scanning: ".*": (\d+(?:\.\d+)?)%`)
	par2Repairing  = regexp.MustCompile(`^Repairing: (\d+(?:\.\d+)?)%`)
)

// par2Watch keeps par2's output (for error messages) and turns its progress
// lines into progress reports: while checking, files checked out of all of
// them; while repairing, its own percentage.
type par2Watch struct {
	mu        sync.Mutex
	out       bytes.Buffer
	line      []byte
	path      string
	files     int
	checked   int
	repairing bool
	verified  bool // checking again after a repair
	last      time.Time
	lastPhase string
}

const par2KeepOutput = 1 << 20

func newPar2Watch(path string) *par2Watch { return &par2Watch{path: path} }

func (w *par2Watch) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if room := par2KeepOutput - w.out.Len(); room > 0 {
		w.out.Write(b[:min(len(b), room)])
	}
	for _, c := range b {
		if c == '\r' || c == '\n' {
			w.handle(string(w.line))
			w.line = w.line[:0]
			continue
		}
		if len(w.line) < 4096 {
			w.line = append(w.line, c)
		}
	}
	return len(b), nil
}

func (w *par2Watch) Bytes() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]byte(nil), w.out.Bytes()...)
}

func (w *par2Watch) handle(line string) {
	switch {
	case par2Files.MatchString(line):
		w.files, _ = strconv.Atoi(par2Files.FindStringSubmatch(line)[1])
	case line == "Verifying repaired files:":
		w.repairing, w.verified, w.checked = false, true, 0
	case par2TargetLine.MatchString(line):
		w.checked++
		w.report(int64(w.checked)*1000, false)
	case !w.repairing && par2Scanning.MatchString(line):
		pct, _ := strconv.ParseFloat(par2Scanning.FindStringSubmatch(line)[1], 64)
		w.report(int64(w.checked)*1000+int64(pct*10), false)
	case par2Repairing.MatchString(line):
		w.repairing = true
		pct, _ := strconv.ParseFloat(par2Repairing.FindStringSubmatch(line)[1], 64)
		w.report(int64(pct*10), true)
	}
}

func (w *par2Watch) report(done int64, repairing bool) {
	phase, total := "Checking files", int64(w.files)*1000
	switch {
	case repairing:
		phase, total = "Repairing damaged files", 1000
	case w.verified:
		phase = "Checking the repaired files"
	}
	if total <= 0 {
		return
	}
	changed := phase != w.lastPhase
	if !changed && time.Since(w.last) < 500*time.Millisecond {
		return
	}
	w.last = time.Now()
	p := ""
	if changed {
		p, w.lastPhase = phase, phase
	}
	reportProgress(w.path, p, min(done, total), total)
}

// rarSetSize is the size on disk of the RAR set first belongs to: every
// .rar/.rNN file next to it with the same base name. Unpacked video is about
// that size (releases store it, they don't compress it).
func rarSetSize(first string) int64 {
	dir, base := filepath.Split(first)
	stem := strings.ToLower(base)
	stem = rarStem.ReplaceAllString(stem, "")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	var total int64
	for _, e := range entries {
		n := strings.ToLower(e.Name())
		if e.IsDir() || !rarVolume.MatchString(n) || rarStem.ReplaceAllString(n, "") != stem {
			continue
		}
		if info, err := e.Info(); err == nil {
			total += info.Size()
		}
	}
	return total
}

var (
	rarVolume = regexp.MustCompile(`\.(rar|r\d{2,3})$`)
	rarStem   = regexp.MustCompile(`(\.part\d+)?\.(rar|r\d{2,3})$`)
)
