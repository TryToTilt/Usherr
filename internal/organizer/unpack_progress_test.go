package organizer

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"
)

// SetProgressEvery makes the extractor report quickly, for tests in other
// packages; it returns the function that puts the old interval back.
func SetProgressEvery(d time.Duration) (restore func()) {
	old := progressEvery
	progressEvery = d
	return func() { progressEvery = old }
}

func touch(t *testing.T, dir, name string, size int) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRARSetFiles(t *testing.T) {
	tests := []struct {
		name  string
		files []string
		first string
		want  []string
	}{
		{"new style", []string{"m.part01.rar", "m.part02.rar", "m.part03.rar", "other.part01.rar", "m.nfo"}, "m.part01.rar", []string{"m.part01.rar", "m.part02.rar", "m.part03.rar"}},
		{"new style, any case", []string{"M.PART1.RAR", "m.part2.rar"}, "M.PART1.RAR", []string{"M.PART1.RAR", "m.part2.rar"}},
		{"old style", []string{"m.rar", "m.r00", "m.r01", "m2.r00", "m.nfo"}, "m.rar", []string{"m.r00", "m.r01", "m.rar"}},
		{"one file", []string{"m.rar", "x.mkv"}, "m.rar", []string{"m.rar"}},
		{"not a set name", []string{"m.cbr", "m.r00"}, "m.cbr", []string{"m.cbr"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, f := range tt.files {
				touch(t, dir, f, 1)
			}
			var got []string
			for _, f := range rarSetFiles(filepath.Join(dir, tt.first)) {
				got = append(got, filepath.Base(f))
			}
			sort.Strings(got)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("rarSetFiles = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTotalSizeAndDirSize(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "a", 100)
	touch(t, dir, "b", 50)
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	touch(t, filepath.Join(dir, "sub"), "c", 25)
	if got := totalSize([]string{filepath.Join(dir, "a"), filepath.Join(dir, "b"), filepath.Join(dir, "missing")}); got != 150 {
		t.Errorf("totalSize = %d, want 150", got)
	}
	if got := dirSize(dir); got != 175 {
		t.Errorf("dirSize = %d, want 175", got)
	}
}

// The meter keeps reporting while nothing moves (a stuck write), never reports
// more than the total, and stops when finished.
func TestMeterReportsAndStops(t *testing.T) {
	defer SetProgressEvery(5 * time.Millisecond)()
	var (
		mu    sync.Mutex
		calls [][2]int64
	)
	e := &Extractor{Progress: func(done, total int64) {
		mu.Lock()
		calls = append(calls, [2]int64{done, total})
		mu.Unlock()
	}}
	m := e.startMeter(100, true, nil)
	m.addWritten(40)
	time.Sleep(60 * time.Millisecond)
	m.addWritten(500) // more than the total
	time.Sleep(30 * time.Millisecond)
	m.finish()

	mu.Lock()
	n := len(calls)
	mu.Unlock()
	if n < 3 {
		t.Fatalf("got %d reports, want several (also while nothing moved)", n)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls[0] != [2]int64{40, 100} {
		t.Errorf("first report = %v, want [40 100]", calls[0])
	}
	if last := calls[len(calls)-1]; last != [2]int64{100, 100} {
		t.Errorf("last report = %v, want done capped at the total [100 100]", last)
	}
	time.Sleep(30 * time.Millisecond)
	if len(calls) != n {
		t.Error("the meter kept reporting after finish")
	}
}

// A meter that counts reads ignores what is written, and the other way round.
func TestMeterCountsOnlyWhatItWasMadeFor(t *testing.T) {
	reads := (&Extractor{}).startMeter(10, false, nil)
	reads.addWritten(5)
	if d, _ := reads.snapshot(); d != 0 {
		t.Errorf("a read-counting meter counted writes: %d", d)
	}
	var nilMeter *meter
	nilMeter.addWritten(5) // must not panic
	nilMeter.finish()
}

func TestCountingFSCountsReads(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "v.rar", 1000)
	m := (&Extractor{}).startMeter(1000, false, nil)
	f, err := countingFS{m: m}.Open(filepath.Join(dir, "v.rar"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	buf := make([]byte, 300)
	if _, err := f.Read(buf); err != nil {
		t.Fatal(err)
	}
	if _, err := f.(interface {
		Seek(int64, int) (int64, error)
	}).Seek(0, 0); err != nil {
		t.Fatalf("the counting file must stay seekable for the RAR reader: %v", err)
	}
	if d, _ := m.snapshot(); d != 300 {
		t.Errorf("counted %d bytes, want 300", d)
	}
}
