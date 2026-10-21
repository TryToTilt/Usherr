package api

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/queue"
)

func TestUnpackStatusPercent(t *testing.T) {
	tests := []struct {
		name string
		st   unpackStatus
		want float64
	}{
		{"nothing known", unpackStatus{Index: 1, Count: 1}, 0},
		{"half of one", unpackStatus{Done: 50, Total: 100, Index: 1, Count: 1}, 50},
		{"second of two, a quarter in", unpackStatus{Done: 25, Total: 100, Index: 2, Count: 2}, 62.5},
		{"finished", unpackStatus{Done: 100, Total: 100, Index: 3, Count: 3}, 100},
		{"more than the total", unpackStatus{Done: 300, Total: 100, Index: 1, Count: 1}, 100},
		{"no count", unpackStatus{Done: 1, Total: 2}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.st.Percent(); got != tt.want {
				t.Errorf("Percent = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHumanDurationAndUnpackedMessage(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{400 * time.Millisecond, "1 second"},
		{45 * time.Second, "45 seconds"},
		{90 * time.Second, "2 minutes"},
		{5 * time.Minute, "5 minutes"},
		{59*time.Minute + 40*time.Second, "1 hour"},
		{3*time.Hour + 5*time.Minute, "3 hours 5 minutes"},
		{3*time.Hour + 20*time.Minute + 29*time.Second, "3 hours 20 minutes"},
	}
	for _, tt := range tests {
		if got := humanDuration(tt.d); got != tt.want {
			t.Errorf("humanDuration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
	if got := unpackedMessage(5<<30, 3*time.Second); got != "Unpacked" {
		t.Errorf("a quick unpack: %q", got)
	}
	if got := unpackedMessage(0, time.Minute); got != "Unpacked" {
		t.Errorf("an unpack of nothing: %q", got)
	}
	if got := unpackedMessage(75<<30, 5*time.Minute); !strings.HasPrefix(got, "Unpacked 80.5 GB") || !strings.HasSuffix(got, "in 5 minutes") {
		t.Errorf("a long unpack: %q", got)
	}
}

// captureLog sends the log to a buffer for the length of a test.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(old) })
	return &buf
}

// The watch keeps the status current, says nothing for a quick unpack, says it
// is still going (with the speed) for a long one, and warns when nothing moves.
func TestUnpackWatchLogs(t *testing.T) {
	logs := captureLog(t)
	clock := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	var last unpackStatus
	w := newUnpackWatch(7, func(st unpackStatus) { last = st })
	w.now = func() time.Time { return clock }
	step := func(d time.Duration, done, total int64) {
		clock = clock.Add(d)
		w.report(done, total)
	}

	w.begin(1, 2)
	step(2*time.Second, 1<<20, 10<<20)
	if last != (unpackStatus{Done: 1 << 20, Total: 10 << 20, Index: 1, Count: 2}) {
		t.Errorf("status = %+v", last)
	}
	if logs.Len() != 0 {
		t.Errorf("a quick unpack logged: %s", logs)
	}

	step(70*time.Second, 5<<20, 10<<20) // over a minute in
	if out := logs.String(); !strings.Contains(out, "unpack: still going") || !strings.Contains(out, "queueId=7") || !strings.Contains(out, "bytesPerSecond=") {
		t.Errorf("a slow unpack should say so with its speed, got: %s", out)
	}
	logs.Reset()
	step(2*time.Second, 6<<20, 10<<20) // the next report is not another line
	if logs.Len() != 0 {
		t.Errorf("repeated the line at once: %s", logs)
	}

	// Nothing moves for two minutes: one warning, then quiet until two more pass.
	for i := 0; i < 30; i++ {
		step(5*time.Second, 6<<20, 10<<20)
	}
	if got := strings.Count(logs.String(), "may be stuck"); got != 1 {
		t.Errorf("stuck warnings in 2.5 minutes = %d, want 1:\n%s", got, logs)
	}
	step(5*time.Second, 7<<20, 10<<20) // moving again
	logs.Reset()
	step(time.Minute, 7<<20, 10<<20)
	if strings.Contains(logs.String(), "may be stuck") {
		t.Error("warned before two minutes without progress")
	}

	logs.Reset()
	bytesDone, took := w.end()
	if bytesDone != 10<<20 || took < time.Minute {
		t.Errorf("end = %d bytes, %v", bytesDone, took)
	}
	if !strings.Contains(logs.String(), "finished an archive") {
		t.Errorf("a long archive should log when it is done: %s", logs)
	}
	for _, secret := range []string{"password", "Password"} {
		if strings.Contains(logs.String(), secret) {
			t.Errorf("the log must not mention %q", secret)
		}
	}
}

func TestUnpackWatchQuietWhenFast(t *testing.T) {
	logs := captureLog(t)
	w := newUnpackWatch(1, func(unpackStatus) {})
	w.begin(1, 1)
	w.report(10, 10)
	w.end()
	if logs.Len() != 0 {
		t.Errorf("a quick unpack logged: %s", logs)
	}
}

// A paused or stopped download ends the unpack with the context's error.
func TestUnpackArchivesWatchedStopsOnContext(t *testing.T) {
	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, "release.zip"))
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, _ := zw.Create("Movie.2001.mkv")
	w.Write(bytes.Repeat([]byte("video"), 1000))
	zw.Close()
	f.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = unpackArchivesWatched(ctx, dir, newUnpackWatch(1, func(unpackStatus) {}))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Movie.2001.mkv")); err == nil {
		t.Error("a stopped unpack left a file behind")
	}
}

// While a download is importing, the Activity list carries how far the unpack
// is and shows that instead of the stored 100%.
func TestQueueListShowsUnpackProgress(t *testing.T) {
	s := newAuditServer(t)
	id, err := s.QueueRepo.Enqueue(queue.Item{ReleaseTitle: "Some.Movie.2020.2160p", NZBURL: "http://indexer.invalid/x"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.QueueRepo.SetStatus(id, queue.StatusImporting, ""); err != nil {
		t.Fatal(err)
	}
	list := func() queueItemPayload {
		t.Helper()
		rec := httptest.NewRecorder()
		s.handleListQueue(rec, httptest.NewRequest(http.MethodGet, "/api/queue", nil))
		var items []queueItemPayload
		if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil || len(items) != 1 {
			t.Fatalf("queue list: %v (%s)", err, rec.Body)
		}
		return items[0]
	}

	if it := list(); it.Unpack != nil || it.ProgressPct != 100 {
		t.Errorf("before unpacking: unpack %+v, progress %v", it.Unpack, it.ProgressPct)
	}
	s.unpacking.set(id, unpackStatus{Done: 30 << 30, Total: 75 << 30, Index: 1, Count: 1})
	it := list()
	if it.Unpack == nil || it.Unpack.DoneBytes != 30<<30 || it.Unpack.TotalBytes != 75<<30 {
		t.Fatalf("unpack = %+v", it.Unpack)
	}
	if it.ProgressPct != 40 {
		t.Errorf("progress = %v, want 40", it.ProgressPct)
	}
	s.unpacking.clear(id)
	if it := list(); it.Unpack != nil {
		t.Error("the unpack figures stayed after the unpack ended")
	}
}
