package download

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"
)

// Usherr: a saved NZB played again reads each article from the first server
// that has it: the backup gives what the primary lacks.
func TestStreamReadsWhatThePrimaryLacksFromTheBackup(t *testing.T) {
	seg1, seg2, mid := multiArticles()
	primary := newFakeNNTPServer(t, map[string][]byte{"seg1@example": seg1}) // no seg2
	backup := newFakeNNTPServer(t, map[string][]byte{"seg2@example": seg2})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	f, err := OpenStream(ctx, []ClientConfig{
		{Host: primary.addr, Port: primary.port, Connections: 1},
		{Host: backup.addr, Port: backup.port, Connections: 1},
	}, twoSegmentNZB(mid, len(multiFixture)))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	got, err := io.ReadAll(io.NewSectionReader(f, 0, f.Size))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, multiFixture) {
		t.Fatalf("got %q", got)
	}
	if backup.hitCount("seg1@example") != 0 {
		t.Errorf("the backup was asked for an article the primary had")
	}
	if backup.hitCount("seg2@example") == 0 {
		t.Errorf("the backup wasn't asked for the article the primary lacks")
	}
}

// A release whose first article is on no server is refused as missing, so
// the caller marks it dead and tries another.
func TestStreamMissingEverywhereIsArticleMissing(t *testing.T) {
	_, seg2, mid := multiArticles()
	a := newFakeNNTPServer(t, map[string][]byte{"seg2@example": seg2})
	b := newFakeNNTPServer(t, map[string][]byte{})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_, err := OpenStream(ctx, []ClientConfig{
		{Host: a.addr, Port: a.port, Connections: 1},
		{Host: b.addr, Port: b.port, Connections: 1},
	}, twoSegmentNZB(mid, len(multiFixture)))
	if !errors.Is(err, ErrArticleMissing) {
		t.Fatalf("want ErrArticleMissing, got %v", err)
	}
}

func TestStreamFileFrom(t *testing.T) {
	f := StreamFileFrom("a.mkv", bytes.NewReader([]byte("0123456789extra")), 10)
	buf := make([]byte, 4)
	if n, err := f.ReadAt(buf, 8); n != 2 || err != io.EOF || string(buf[:n]) != "89" {
		t.Fatalf("read past the end: %d %v %q", n, err, buf[:n])
	}
	if n, err := f.ReadAt(buf, 2); n != 4 || err != nil || string(buf) != "2345" {
		t.Fatalf("read: %d %v %q", n, err, buf)
	}
	if _, err := f.ReadAt(buf, 10); err != io.EOF {
		t.Fatalf("at the end: %v", err)
	}
}
