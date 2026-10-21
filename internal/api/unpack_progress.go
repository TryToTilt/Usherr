package api

import (
	"log/slog"
	"sync"
	"time"
)

// unpackStatus is how far the unpack of one download has come, for the
// Activity page. Index is the archive being unpacked (1 is the first) out of
// Count; Done and Total are bytes of that archive.
type unpackStatus struct {
	Done, Total  int64
	Index, Count int
}

// Percent is how much of the whole unpack is finished, 0 to 100.
func (u unpackStatus) Percent() float64 {
	if u.Count < 1 || u.Total <= 0 {
		return 0
	}
	frac := (float64(u.Index-1) + float64(u.Done)/float64(u.Total)) / float64(u.Count)
	return min(max(frac*100, 0), 100)
}

// unpackTracker keeps the status of the unpacks that are running, by queue
// item, in memory: it means nothing once the program restarts. The zero value
// is ready to use.
type unpackTracker struct {
	mu sync.Mutex
	m  map[int64]unpackStatus
}

func (t *unpackTracker) set(id int64, st unpackStatus) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.m == nil {
		t.m = map[int64]unpackStatus{}
	}
	t.m[id] = st
}

func (t *unpackTracker) get(id int64) (unpackStatus, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	st, ok := t.m[id]
	return st, ok
}

func (t *unpackTracker) clear(id int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.m, id)
}

const (
	// unpackSlowAfter is how long an unpack runs before the log says it is
	// still going, and unpackLogEvery how often it says so after that.
	unpackSlowAfter = time.Minute
	unpackLogEvery  = 10 * time.Minute
	// unpackStallAfter is how long nothing is written before the log warns
	// that the disk or share may be stuck (a stuck network share blocks a
	// write for good, with no error to show for it).
	unpackStallAfter = 2 * time.Minute
)

// unpackWatch follows one download's unpack: it keeps the status for the
// Activity page and writes the log lines that say it is slow or stuck. It is
// fed by the extractor every couple of seconds, from another goroutine.
type unpackWatch struct {
	id  int64
	set func(unpackStatus)
	now func() time.Time

	mu        sync.Mutex
	st        unpackStatus
	archive   time.Time // when the archive being unpacked started
	moved     time.Time // when the figures last changed
	lastLog   time.Time
	lastStall time.Time
}

func newUnpackWatch(id int64, set func(unpackStatus)) *unpackWatch {
	return &unpackWatch{id: id, set: set, now: time.Now}
}

// begin is called when an archive starts.
func (w *unpackWatch) begin(index, count int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.st = unpackStatus{Index: index, Count: count}
	w.archive = w.now()
	w.moved = w.archive
	w.set(w.st)
}

// report is the extractor's progress callback.
func (w *unpackWatch) report(done, total int64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := w.now()
	if done != w.st.Done {
		w.moved = now
	}
	w.st.Done, w.st.Total = done, total
	w.set(w.st)

	if still := now.Sub(w.moved); still >= unpackStallAfter && now.Sub(w.lastStall) >= unpackStallAfter {
		w.lastStall = now
		slog.Warn("unpack: nothing has been written for a while; the disk or share may be stuck",
			"queueId", w.id, "seconds", int(still.Seconds()), "doneBytes", done, "totalBytes", total)
		return
	}
	if took := now.Sub(w.archive); took >= unpackSlowAfter && now.Sub(w.lastLog) >= unpackLogEvery {
		w.lastLog = now
		slog.Info("unpack: still going",
			"queueId", w.id, "seconds", int(took.Seconds()), "doneBytes", done, "totalBytes", total, "bytesPerSecond", rate(done, took))
	}
}

// end is called when an archive is unpacked. It logs a line when that took a
// while, with the speed, and returns how many bytes it was and how long it took.
func (w *unpackWatch) end() (bytes int64, took time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()
	took, bytes = w.now().Sub(w.archive), w.st.Total
	if took >= unpackSlowAfter/2 {
		slog.Info("unpack: finished an archive", "queueId", w.id, "seconds", int(took.Seconds()), "bytes", bytes, "bytesPerSecond", rate(bytes, took))
	}
	return bytes, took
}

// rate is bytes per second, or 0 when no time has passed.
func rate(bytes int64, d time.Duration) int64 {
	if d <= 0 {
		return 0
	}
	return int64(float64(bytes) / d.Seconds())
}
