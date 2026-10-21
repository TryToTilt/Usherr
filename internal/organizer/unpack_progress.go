package organizer

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

const (
	// unpackBufferBytes is the size of the buffer unpacking reads archive
	// volumes with and writes the files out with. The default of the RAR reader
	// is 4 KB and a plain copy uses 32 KB: on a network share or a FUSE file
	// system (Unraid's user shares, mergerfs) every read or write is a round
	// trip to another process, so a 75 GB movie meant tens of millions of them.
	unpackBufferBytes = 4 << 20
)

// progressEvery is how often an Extractor reports its progress. A variable so
// tests can make it quick.
var progressEvery = 2 * time.Second

// meter counts how far an unpack has come and hands the figures to the
// Extractor's Progress callback on a timer of its own. The timer runs apart
// from the copying, so a write that hangs (a stuck network share) still shows
// up: the callback is called again with the same figures.
type meter struct {
	done     atomic.Int64
	total    atomic.Int64
	byWrites bool // count the bytes written; otherwise the bytes read count (see countingFS)
	poll     func() int64
	stop     chan struct{}
	stopped  chan struct{}
}

// startMeter begins reporting to e.Progress, if there is one. total is the
// amount of work expected (0 when it is not known). The returned meter is
// always usable; call finish when the unpack ends.
func (e *Extractor) startMeter(total int64, byWrites bool, poll func() int64) *meter {
	m := &meter{byWrites: byWrites, poll: poll}
	m.total.Store(total)
	if e.Progress == nil {
		return m
	}
	m.stop, m.stopped = make(chan struct{}), make(chan struct{})
	report, every := e.Progress, progressEvery
	go func() {
		defer close(m.stopped)
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-m.stop:
				return
			case <-t.C:
				done, total := m.snapshot()
				report(done, total)
			}
		}
	}()
	return m
}

// snapshot is the figures now. Done never goes past total.
func (m *meter) snapshot() (done, total int64) {
	if m.poll != nil {
		m.done.Store(m.poll())
	}
	done, total = m.done.Load(), m.total.Load()
	if total > 0 && done > total {
		done = total
	}
	return done, total
}

// finish stops the reporting and waits until the callback is no longer running.
func (m *meter) finish() {
	if m == nil || m.stop == nil {
		return
	}
	close(m.stop)
	<-m.stopped
}

// addWritten counts bytes written to disk (when the meter counts writes).
func (m *meter) addWritten(n int) {
	if m != nil && m.byWrites {
		m.done.Add(int64(n))
	}
}

// addTotal raises the expected amount of work, for a total learnt on the way.
func (m *meter) addTotal(n int64) {
	if m != nil && n > 0 {
		m.total.Add(n)
	}
}

// countingFS opens files for the RAR reader and counts the bytes it reads from
// them, which is how far through the volumes of a set the unpack has come.
type countingFS struct{ m *meter }

func (c countingFS) Open(name string) (fs.File, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	return &countingFile{File: f, m: c.m}, nil
}

type countingFile struct {
	*os.File
	m *meter
}

func (f *countingFile) Read(p []byte) (int, error) {
	n, err := f.File.Read(p)
	f.m.done.Add(int64(n))
	return n, err
}

// rarSetFiles returns the first volume of a RAR set and every volume that
// follows it in the same folder, found by name the way the reader finds them.
func rarSetFiles(first string) []string {
	dir, base := filepath.Split(first)
	entries, err := os.ReadDir(filepath.Clean(dir))
	if err != nil {
		return []string{first}
	}
	out := []string{first}
	lower := strings.ToLower(base)
	var match func(name string) bool
	if m := rarSetRe.FindStringSubmatch(base); m != nil {
		prefix := m[1]
		match = func(name string) bool {
			vm := rarSetRe.FindStringSubmatch(name)
			return vm != nil && strings.EqualFold(vm[1], prefix)
		}
	} else if stem, ok := strings.CutSuffix(lower, ".rar"); ok {
		match = func(name string) bool {
			rest, ok := strings.CutPrefix(strings.ToLower(name), stem)
			loc := rarOldStyle.FindStringIndex(rest)
			return ok && loc != nil && loc[0] == 0
		}
	} else {
		return out
	}
	for _, e := range entries {
		if e.IsDir() || e.Name() == base || !match(e.Name()) {
			continue
		}
		out = append(out, filepath.Join(dir, e.Name()))
	}
	return out
}

// totalSize adds up the sizes of the files that can be read.
func totalSize(files []string) int64 {
	var n int64
	for _, f := range files {
		if fi, err := os.Stat(f); err == nil {
			n += fi.Size()
		}
	}
	return n
}

// dirSize is the size of all regular files under dir.
func dirSize(dir string) int64 {
	var n int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			n += info.Size()
		}
		return nil
	})
	return n
}

// ctxOrBackground is the Extractor's context, or one that never ends.
func (e *Extractor) ctxOrBackground() context.Context {
	if e.Context != nil {
		return e.Context
	}
	return context.Background()
}
