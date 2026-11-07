package download

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"
)

const holeArticle = 16 << 10

// holeFixture is an MKV-looking file of n small articles on one fake server, so
// 1% of it is several articles.
func holeFixture(t *testing.T, n int, magic []byte) (*fakeNNTPServer, *NZB, []byte) {
	t.Helper()
	data := make([]byte, n*holeArticle)
	for i := range data {
		data[i] = byte(i%251) + 1
	}
	copy(data, magic)
	bodies := map[string][]byte{}
	f := NZBFile{Subject: `[1/1] "movie.mkv" yEnc (1/1)`}
	for i := range n {
		id := fmt.Sprintf("a%d@hole", i)
		bodies[id] = buildMultipartYenc("movie.mkv", data, i*holeArticle+1, (i+1)*holeArticle)
		f.Segments = append(f.Segments, NZBSegment{Number: i + 1, Bytes: holeArticle, MessageID: id})
	}
	return newFakeNNTPServer(t, bodies), &NZB{Files: []NZBFile{f}}, data
}

var mkvMagic = []byte{0x1a, 0x45, 0xdf, 0xa3}

func holeServers(srv *fakeNNTPServer) []ClientConfig {
	return []ClientConfig{{Host: srv.addr, Port: srv.port, Connections: 20}}
}

func quickAskAgain(t *testing.T) {
	t.Helper()
	setStreamTime(t, missingAskAgainAfter, time.Millisecond)
}

func openHoleFixture(t *testing.T, srv *fakeNNTPServer, nzb *NZB) *StreamFile {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	f, err := OpenStream(ctx, holeServers(srv), nzb)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// One article gone everywhere: the film still reads to the end, with zeros for
// just that article's bytes and everything else as it was.
func TestStreamPlaysPastOneMissingArticle(t *testing.T) {
	quickAskAgain(t)
	srv, nzb, data := holeFixture(t, 1000, mkvMagic)
	srv.setBody("a500@hole", nil)
	before := StreamHolesFilled()
	f := openHoleFixture(t, srv, nzb)
	got, err := io.ReadAll(io.NewSectionReader(f, 0, f.Size))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	want := append([]byte(nil), data...)
	clear(want[500*holeArticle : 501*holeArticle])
	if !bytes.Equal(got, want) {
		t.Fatalf("the bytes around the hole aren't right")
	}
	if StreamHolesFilled() != before+1 {
		t.Errorf("filled %d", StreamHolesFilled()-before)
	}
	// Read again: not counted again, and the servers aren't asked again yet.
	hits := srv.hitCount("a500@hole")
	if _, err := f.ReadAt(make([]byte, 100), 500*holeArticle); err != nil {
		t.Fatal(err)
	}
	if StreamHolesFilled() != before+1 || srv.hitCount("a500@hole") != hits {
		t.Errorf("a second read of the hole counted or asked again")
	}
}

// A provider that wrongly says "no such article" once is asked again.
func TestStreamAsksAgainBeforeCallingAnArticleMissing(t *testing.T) {
	quickAskAgain(t)
	srv, nzb, data := holeFixture(t, 1000, mkvMagic)
	f := openHoleFixture(t, srv, nzb)
	body, _ := srv.body("a500@hole")
	srv.setBody("a500@hole", nil)
	done := make(chan struct{})
	go func() {
		for srv.hitCount("a500@hole") == 0 {
			time.Sleep(50 * time.Microsecond)
		}
		srv.setBody("a500@hole", body)
		close(done)
	}()
	missingAskAgainAfter.Store(int64(20 * time.Millisecond))
	got := make([]byte, holeArticle)
	if _, err := f.ReadAt(got, 500*holeArticle); err != nil {
		t.Fatal(err)
	}
	<-done
	if !bytes.Equal(got, data[500*holeArticle:501*holeArticle]) {
		t.Errorf("the article came back as something else")
	}
}

func TestStreamHoleLimits(t *testing.T) {
	read := func(f *StreamFile, idx int) error {
		_, err := f.ReadAt(make([]byte, holeArticle), int64(idx)*holeArticle)
		return err
	}
	t.Run("two in a row, not three", func(t *testing.T) {
		quickAskAgain(t)
		srv, nzb, _ := holeFixture(t, 1000, mkvMagic)
		for _, i := range []int{500, 501, 502} {
			srv.setBody(fmt.Sprintf("a%d@hole", i), nil)
		}
		f := openHoleFixture(t, srv, nzb)
		if err := read(f, 500); err != nil {
			t.Fatal(err)
		}
		if err := read(f, 501); err != nil {
			t.Fatal(err)
		}
		if err := read(f, 502); !errors.Is(err, ErrTooManyHoles) || !errors.Is(err, ErrArticleMissing) {
			t.Fatalf("third in a row: %v", err)
		}
		// Rclone tries again with a new range: the count holds.
		if err := read(f, 502); !errors.Is(err, ErrTooManyHoles) {
			t.Fatalf("again: %v", err)
		}
		// Somewhere else in the file still plays.
		if err := read(f, 700); err != nil {
			t.Fatalf("a good article after the limit: %v", err)
		}
	})
	t.Run("five in all", func(t *testing.T) {
		quickAskAgain(t)
		srv, nzb, _ := holeFixture(t, 1000, mkvMagic)
		for i := range 6 {
			srv.setBody(fmt.Sprintf("a%d@hole", 200+i*50), nil)
		}
		f := openHoleFixture(t, srv, nzb)
		for i := range 5 {
			if err := read(f, 200+i*50); err != nil {
				t.Fatalf("hole %d: %v", i, err)
			}
		}
		if err := read(f, 450); !errors.Is(err, ErrTooManyHoles) {
			t.Fatalf("sixth: %v", err)
		}
	})
	t.Run("one percent of the file", func(t *testing.T) {
		quickAskAgain(t)
		srv, nzb, _ := holeFixture(t, 150, mkvMagic) // 1% is under two articles
		srv.setBody("a100@hole", nil)
		srv.setBody("a120@hole", nil)
		f := openHoleFixture(t, srv, nzb)
		if err := read(f, 100); err != nil {
			t.Fatal(err)
		}
		if err := read(f, 120); !errors.Is(err, ErrTooManyHoles) {
			t.Fatalf("second: %v", err)
		}
	})
	t.Run("the start of the file", func(t *testing.T) {
		quickAskAgain(t)
		srv, nzb, _ := holeFixture(t, 1000, mkvMagic)
		srv.setBody("a3@hole", nil)
		f := openHoleFixture(t, srv, nzb)
		if err := read(f, 3); !errors.Is(err, ErrTooManyHoles) {
			t.Fatalf("in the first megabyte: %v", err)
		}
	})
	t.Run("a container that can't take it", func(t *testing.T) {
		quickAskAgain(t)
		avi := append([]byte("RIFF"), 0, 0, 0, 0, 'A', 'V', 'I', ' ')
		srv, nzb, _ := holeFixture(t, 1000, avi)
		srv.setBody("a500@hole", nil)
		f := openHoleFixture(t, srv, nzb)
		if err := read(f, 500); !errors.Is(err, ErrArticleMissing) {
			t.Fatalf("an AVI: %v", err)
		}
	})
}

// "Couldn't ask" is never "missing": with one server down and the other saying
// no, the read fails plainly and nothing counts as a hole.
func TestStreamServerDownIsNotAHole(t *testing.T) {
	quickAskAgain(t)
	srv, nzb, _ := holeFixture(t, 1000, mkvMagic)
	other := newFakeNNTPServer(t, map[string][]byte{})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	f, err := OpenStream(ctx, append(holeServers(srv), holeServers(other)...), nzb)
	if err != nil {
		t.Fatal(err)
	}
	srv.setBody("a500@hole", nil)
	other.dropBodies.Store(1 << 20)
	before := StreamHolesFilled()
	_, err = f.ReadAt(make([]byte, 100), 500*holeArticle)
	if err == nil || errors.Is(err, ErrArticleMissing) {
		t.Fatalf("want a plain failure, got %v", err)
	}
	f.guard.mu.Lock()
	counted := len(f.guard.entries)
	f.guard.mu.Unlock()
	if counted != 0 {
		t.Errorf("counted as a hole")
	}
	if StreamHolesFilled() < before {
		t.Errorf("the count went down")
	}
}

func TestConfirmHoles(t *testing.T) {
	over := func(t *testing.T) (*fakeNNTPServer, *fakeNNTPServer, *StreamFile, map[string][]byte) {
		quickAskAgain(t)
		srv, nzb, _ := holeFixture(t, 1000, mkvMagic)
		other := newFakeNNTPServer(t, map[string][]byte{})
		saved := map[string][]byte{}
		for _, i := range []int{500, 501, 502} {
			id := fmt.Sprintf("a%d@hole", i)
			saved[id], _ = srv.body(id)
			srv.setBody(id, nil)
		}
		octx, ocancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer ocancel()
		f, err := OpenStream(octx, append(holeServers(srv), holeServers(other)...), nzb)
		if err != nil {
			t.Fatal(err)
		}
		for _, i := range []int{500, 501} {
			if _, err := f.ReadAt(make([]byte, 10), int64(i)*holeArticle); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := f.ReadAt(make([]byte, 10), 502*holeArticle); !errors.Is(err, ErrTooManyHoles) {
			t.Fatal(err)
		}
		return srv, other, f, saved
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	t.Run("still gone", func(t *testing.T) {
		_, _, f, _ := over(t)
		if v := f.ConfirmHoles(ctx); v != HolesGone {
			t.Fatalf("verdict %d", v)
		}
	})
	t.Run("back again", func(t *testing.T) {
		srv, _, f, saved := over(t)
		for id, b := range saved {
			srv.setBody(id, b)
		}
		if v := f.ConfirmHoles(ctx); v != HolesBearable {
			t.Fatalf("verdict %d", v)
		}
		got := make([]byte, 10)
		if _, err := f.ReadAt(got, 502*holeArticle); err != nil || bytes.Equal(got, make([]byte, 10)) {
			t.Fatalf("read after they came back: %v %v", err, got)
		}
	})
	t.Run("a server can't be asked", func(t *testing.T) {
		_, other, f, _ := over(t)
		other.refuseStat.Store(true)
		if v := f.ConfirmHoles(ctx); v != HolesUnknown {
			t.Fatalf("verdict %d", v)
		}
	})
}

func TestHoleSafeFromMP4(t *testing.T) {
	box := func(kind string, size int) []byte {
		b := make([]byte, 8)
		b[0], b[1], b[2], b[3] = byte(size>>24), byte(size>>16), byte(size>>8), byte(size)
		copy(b[4:], kind)
		return b
	}
	ftyp := append(box("ftyp", 24), make([]byte, 16)...)
	// The index first: fine after it (and after the first megabyte).
	fast := append(append([]byte(nil), ftyp...), box("moov", 3<<20)...)
	if got := holeSafeFrom(fast); got != 24+3<<20 {
		t.Errorf("fast start: %d", got)
	}
	// The video first: the index is at the end, and any loss can be fatal.
	late := append(append([]byte(nil), ftyp...), box("mdat", 3<<20)...)
	if got := holeSafeFrom(late); got != -1 {
		t.Errorf("index at the end: %d", got)
	}
	frag := append(append([]byte(nil), ftyp...), box("moof", 100)...)
	if got := holeSafeFrom(frag); got != holeHeadBytes {
		t.Errorf("fragmented: %d", got)
	}
	for _, odd := range [][]byte{nil, []byte("hello"), append(append([]byte(nil), ftyp...), box("moov", 2)...), append(append([]byte(nil), ftyp...), 0, 0, 0, 1, 'm', 'o', 'o', 'v')} {
		if got := holeSafeFrom(odd); got != -1 {
			t.Errorf("%v: %d", odd, got)
		}
	}
}

// The sample's rule for a stream: PAR2 doesn't help it, one miss doesn't sink it.
func TestTooThinToStream(t *testing.T) {
	for _, c := range []struct {
		r    PrecheckResult
		want bool
	}{
		{PrecheckResult{Checked: 100}, false},
		{PrecheckResult{Checked: 100, Missing: 1, EstimatedMissingBytes: 500}, false},
		{PrecheckResult{Checked: 100, Missing: 2, EstimatedMissingBytes: 500, RepairableBytes: 1 << 30}, true},
		{PrecheckResult{Skipped: true, Missing: 9}, false},
	} {
		if got := c.r.TooThinToStream(); got != c.want {
			t.Errorf("%+v: %v", c.r, got)
		}
	}
}
