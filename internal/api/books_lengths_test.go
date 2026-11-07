package api

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/books"
	"github.com/rdborg/mediarium/internal/settings"
)

// fakeProber gives every file 100 seconds per byte, except names containing
// "bad", which can't be read, and counts what it was asked.
func fakeProber(t *testing.T, onCall func(n int64)) *atomic.Int64 {
	t.Helper()
	var calls atomic.Int64
	oldProbe, oldReady := probeAudioFile, audioProberReady
	probeAudioFile = func(_ context.Context, path string) (books.AudioProbe, error) {
		n := calls.Add(1)
		if onCall != nil {
			onCall(n)
		}
		if strings.Contains(filepath.Base(path), "bad") {
			return books.AudioProbe{}, errors.New("not audio")
		}
		st, err := os.Stat(path)
		if err != nil {
			return books.AudioProbe{}, err
		}
		return books.AudioProbe{Seconds: float64(st.Size()) * 100}, nil
	}
	audioProberReady = func() bool { return true }
	t.Cleanup(func() { probeAudioFile, audioProberReady = oldProbe, oldReady })
	return &calls
}

func lengthsBook(t *testing.T, s *Server, root, key string, files map[string]string) books.Book {
	t.Helper()
	dir := filepath.Join(root, key)
	for name, body := range files {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	b, err := s.BookRepo.Add(books.Book{OLKey: key, Title: key, WantAudiobook: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.BookRepo.SetState(b.ID, books.Audiobook, books.StatusDownloaded, "mp3", dir); err != nil {
		t.Fatal(err)
	}
	b, err = s.BookRepo.Get(b.ID)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func lengthsServer(t *testing.T) (*Server, string) {
	t.Helper()
	s, _, dir := bigServer(t)
	root := filepath.Join(dir, "audiobooks")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := s.Settings.Set(settings.KeyAudiobooksPath, root, false); err != nil {
		t.Fatal(err)
	}
	return s, root
}

// The real thing, where ffprobe is installed: one second of silence.
func TestReadBookLengthsWithRealFFprobe(t *testing.T) {
	if !audioProberReady() {
		t.Skip("ffprobe isn't installed here")
	}
	const rate, seconds = 8000, 1
	data := make([]byte, 44+rate*seconds)
	copy(data[0:], "RIFF")
	binary.LittleEndian.PutUint32(data[4:], uint32(len(data)-8))
	copy(data[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:], 16)
	binary.LittleEndian.PutUint16(data[20:], 1) // PCM
	binary.LittleEndian.PutUint16(data[22:], 1) // mono
	binary.LittleEndian.PutUint32(data[24:], rate)
	binary.LittleEndian.PutUint32(data[28:], rate)
	binary.LittleEndian.PutUint16(data[32:], 1)
	binary.LittleEndian.PutUint16(data[34:], 8)
	copy(data[36:], "data")
	binary.LittleEndian.PutUint32(data[40:], rate*seconds)
	path := filepath.Join(t.TempDir(), "one.wav")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := probeAudioFile(context.Background(), path)
	if err != nil || p.Seconds < 0.9 || p.Seconds > 1.1 {
		t.Errorf("read %v seconds, %v", p.Seconds, err)
	}
}

func TestLengthsDueOnlyAtNight(t *testing.T) {
	at := func(h int) time.Time { return time.Date(2026, 11, 6, h, 30, 0, 0, time.Local) }
	for h := 0; h < 24; h++ {
		if got, want := lengthsDue("", at(h)), h >= 2 && h < 9; got != want {
			t.Errorf("%d:30: due %v, want %v", h, got, want)
		}
	}
	done := at(3).Add(-time.Hour).UTC().Format(time.RFC3339)
	if lengthsDue(done, at(3)) {
		t.Error("a pass finished tonight isn't run again")
	}
	if !lengthsDue(at(3).Add(-24*time.Hour).UTC().Format(time.RFC3339), at(3)) {
		t.Error("last night's pass doesn't stop tonight's")
	}
}

func TestReadBookLengthsBuildsTimelineAndKeepsPlaces(t *testing.T) {
	s, root := lengthsServer(t)
	calls := fakeProber(t, nil)
	b := lengthsBook(t, s, root, "OL1W", map[string]string{"01.mp3": "abc", "02.mp3": "a"}) // 300 s and 100 s
	if _, err := s.db.Exec(`INSERT INTO users (username, password_hash) VALUES ('sam', 'x')`); err != nil {
		t.Fatal(err)
	}
	if err := s.BookRepo.SetProgress(1, books.Progress{BookID: b.ID, Format: books.Audiobook, Position: "1:50", Percent: 10}); err != nil {
		t.Fatal(err)
	}
	if !s.readBookLengths(context.Background(), b, lengthsOptions{}) {
		t.Fatal("stopped early")
	}
	if calls.Load() != 2 {
		t.Fatalf("read %d files", calls.Load())
	}
	p, _, _ := s.BookRepo.GetProgress(1, b.ID, books.Audiobook)
	if p.Position != "1:50" || p.Percent != 87.5 {
		t.Errorf("the place stays, the percent becomes true: %+v", p)
	}
	if got, _ := s.BookRepo.Get(b.ID); got.RuntimeMin != 7 {
		t.Errorf("runtime filled in: %d", got.RuntimeMin)
	}
	// Already read: nothing is read again.
	s.readBookLengths(context.Background(), b, lengthsOptions{})
	if calls.Load() != 2 {
		t.Errorf("read again: %d", calls.Load())
	}
	// A replaced file is read again; a removed one is forgotten.
	if err := os.WriteFile(filepath.Join(b.AudioPath, "02.mp3"), []byte("abcd"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.readBookLengths(context.Background(), b, lengthsOptions{})
	if calls.Load() != 3 {
		t.Errorf("a changed file: %d", calls.Load())
	}
	if err := os.Remove(filepath.Join(b.AudioPath, "01.mp3")); err != nil {
		t.Fatal(err)
	}
	s.readBookLengths(context.Background(), b, lengthsOptions{})
	got, _ := s.BookRepo.AudioLengths(b.ID)
	if _, ok := got["01.mp3"]; ok || got["02.mp3"].Seconds != 400 {
		t.Errorf("after the changes: %+v", got)
	}
}

func TestReadBookLengthsGivesUpOnAnUnreadableFile(t *testing.T) {
	s, root := lengthsServer(t)
	calls := fakeProber(t, nil)
	b := lengthsBook(t, s, root, "OL2W", map[string]string{"bad.mp3": "x", "good.mp3": "x"})
	for range books.MaxLengthTries + 2 {
		s.readBookLengths(context.Background(), b, lengthsOptions{})
	}
	if want := int64(books.MaxLengthTries + 1); calls.Load() != want { // the good file once
		t.Errorf("asked %d times, want %d", calls.Load(), want)
	}
	got, _ := s.BookRepo.AudioLengths(b.ID)
	if got["bad.mp3"].Tries != books.MaxLengthTries || got["good.mp3"].Seconds != 100 {
		t.Errorf("%+v", got)
	}
}

// The night pass stops when told and carries on from the same place, without
// reading again what it has, and a pass that ended starts over next time.
func TestAudiobookLengthsPassIsResumable(t *testing.T) {
	s, root := lengthsServer(t)
	calls := fakeProber(t, nil)
	var books3 []books.Book
	for _, key := range []string{"OL1W", "OL2W", "OL3W"} {
		books3 = append(books3, lengthsBook(t, s, root, key, map[string]string{"1.mp3": "a", "2.mp3": "b"}))
	}
	stopAfter := int64(3)
	stop := func() bool { return calls.Load() >= stopAfter }
	if s.audiobookLengthsPass(context.Background(), stop, 0) {
		t.Fatal("told to stop, it says it finished")
	}
	if calls.Load() != 3 {
		t.Fatalf("stopped after %d files", calls.Load())
	}
	if v, _ := s.Settings.Get(keyAudioLengthsCursor); v != "1" {
		t.Errorf("first book done, the second under way: cursor %q", v)
	}
	if !s.audiobookLengthsPass(context.Background(), func() bool { return false }, 0) {
		t.Fatal("didn't finish")
	}
	if calls.Load() != 6 {
		t.Errorf("each file read once: %d", calls.Load())
	}
	for _, b := range books3 {
		if got, _ := s.BookRepo.AudioLengths(b.ID); len(got) != 2 {
			t.Errorf("book %d: %+v", b.ID, got)
		}
	}
	if v, _ := s.Settings.Get(keyAudioLengthsCursor); v != "" {
		t.Errorf("cursor after the end: %q", v)
	}
	if v, _ := s.Settings.Get(keyAudioLengthsDone); v == "" {
		t.Error("the finished pass isn't noted")
	}
}

func TestAudiobookLengthsPassLeavesLinksAlone(t *testing.T) {
	s, root := lengthsServer(t)
	calls := fakeProber(t, nil)
	b := lengthsBook(t, s, root, "OL4W", map[string]string{"1.mp3": "a"})
	outside := filepath.Join(root, "elsewhere.bin")
	if err := os.WriteFile(outside, []byte("zz"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(b.AudioPath, "2.mp3")); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	s.audiobookLengthsPass(context.Background(), func() bool { return false }, 0)
	if calls.Load() != 1 {
		t.Errorf("the night pass read a link: %d files", calls.Load())
	}
}
