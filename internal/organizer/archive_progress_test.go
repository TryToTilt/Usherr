package organizer_test

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/organizer"
)

// progressLog collects what an Extractor's Progress callback is told.
type progressLog struct {
	mu    sync.Mutex
	calls [][2]int64
}

func (p *progressLog) record(done, total int64) {
	p.mu.Lock()
	p.calls = append(p.calls, [2]int64{done, total})
	p.mu.Unlock()
}

func (p *progressLog) all() [][2]int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([][2]int64(nil), p.calls...)
}

// checkProgress holds a log to the rules every format keeps: it reported, the
// total is the same each time, done never shrinks and never passes the total.
func checkProgress(t *testing.T, calls [][2]int64, wantTotal int64) {
	t.Helper()
	if len(calls) == 0 {
		t.Fatal("no progress was reported")
	}
	var prev int64
	for i, c := range calls {
		if c[1] != wantTotal {
			t.Errorf("report %d: total = %d, want %d", i, c[1], wantTotal)
		}
		if c[0] < prev || c[0] > c[1] {
			t.Errorf("report %d: done = %d (after %d, total %d)", i, c[0], prev, c[1])
		}
		prev = c[0]
	}
}

// A stored multi-volume RAR set reports how much of its volumes has been read,
// out of their combined size, and comes out byte for byte the same even for a
// file bigger than the write buffer.
func TestExtractRARReportsProgress(t *testing.T) {
	defer organizer.SetProgressEvery(time.Millisecond)()
	payload := make([]byte, 40<<20)
	rand.New(rand.NewSource(1)).Read(payload)
	dir := t.TempDir()
	names := []string{"movie.part01.rar", "movie.part02.rar", "movie.part03.rar", "movie.part04.rar"}
	rarSplit(t, dir, names, true, rarEntry{name: "movie.mkv", data: payload})
	var total int64
	for _, n := range names {
		fi, err := os.Stat(filepath.Join(dir, n))
		if err != nil {
			t.Fatal(err)
		}
		total += fi.Size()
	}

	var log progressLog
	e := organizer.NewExtractor()
	e.Progress = log.record
	dest := filepath.Join(dir, "out")
	if err := e.Extract(filepath.Join(dir, names[0]), dest); err != nil {
		t.Fatalf("extract: %v", err)
	}
	checkProgress(t, log.all(), total)
	got, err := os.ReadFile(filepath.Join(dest, "movie.mkv"))
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("the unpacked file differs from the original (err %v, %d bytes)", err, len(got))
	}
}

func TestExtractZipReportsProgress(t *testing.T) {
	defer organizer.SetProgressEvery(time.Millisecond)()
	payload := make([]byte, 24<<20)
	rand.New(rand.NewSource(2)).Read(payload)
	root := t.TempDir()
	archive := filepath.Join(root, "release.zip")
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for i := 0; i < 2; i++ {
		w, _ := zw.CreateHeader(&zip.FileHeader{Name: fmt.Sprintf("part%d.bin", i), Method: zip.Store})
		w.Write(payload)
	}
	zw.Close()
	writeTestFile(t, archive, buf.Bytes())

	var log progressLog
	e := organizer.NewExtractor()
	e.Progress = log.record
	if err := e.Extract(archive, filepath.Join(root, "out")); err != nil {
		t.Fatalf("extract: %v", err)
	}
	checkProgress(t, log.all(), 2*int64(len(payload)))
	if last := log.all(); last[len(last)-1][0] == 0 {
		t.Error("nothing was counted as written")
	}
}

// 7z reports how much it has written, found by looking at its output folder.
func TestExtract7zReportsProgress(t *testing.T) {
	defer organizer.SetProgressEvery(10 * time.Millisecond)()
	e := fake7z(t, listing7z("a.bin|1000000|-rw-r--r--"), "head -c 1000000 /dev/zero > a.bin; sleep 0.3")
	var log progressLog
	e.Progress = log.record
	archive, dest := fresh7zDest(t)
	if err := e.Extract(archive, dest); err != nil {
		t.Fatalf("extract: %v", err)
	}
	calls := log.all()
	checkProgress(t, calls, 1000000)
	if calls[len(calls)-1][0] != 1000000 {
		t.Errorf("last report = %v, want everything written", calls[len(calls)-1])
	}
}

// An unpack whose context has ended (the download was paused or stopped) fails
// with that error and leaves no half-written file behind.
func TestExtractStopsWhenContextEnds(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "release.rar")
	writeTestFile(t, archive, rarSingle(rarEntry{name: "movie.mkv", data: bytes.Repeat([]byte("x"), 1<<20)}))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	e := organizer.NewExtractor()
	e.Context = ctx
	dest := filepath.Join(root, "dest")
	err := e.Extract(archive, dest)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if got := entriesIn(t, dest); len(got) != 0 {
		t.Errorf("left behind: %v", got)
	}
}
