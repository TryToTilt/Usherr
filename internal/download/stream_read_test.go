package download

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"testing"
	"time"
)

func setStream(t *testing.T, v *streamSetting, to int64) {
	t.Helper()
	old := v.Load()
	v.Store(to)
	t.Cleanup(func() { v.Store(old) })
}

func setStreamTime(t *testing.T, v *streamSetting, to time.Duration) {
	t.Helper()
	setStream(t, v, int64(to))
}

func wholeFile(t *testing.T, f *StreamFile) []byte {
	t.Helper()
	got, err := io.ReadAll(io.NewSectionReader(f, 0, f.Size))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return got
}

// However many streams read at once, their articles stay inside one budget, and
// a read of what was pushed out still works.
func TestStreamsShareOneMemoryBudget(t *testing.T) {
	setStream(t, streamMemLimit, 2<<20)
	var files []*StreamFile
	var want [][]byte
	for range 3 {
		srv, nzb, data := holeFixture(t, 300, mkvMagic) // 4.8 MB each
		files = append(files, openHoleFixture(t, srv, nzb))
		want = append(want, data)
	}
	done := make(chan error, len(files))
	for i, f := range files {
		go func() {
			buf := make([]byte, f.Size)
			_, err := io.ReadFull(io.NewSectionReader(f, 0, f.Size), buf)
			if err == nil && !bytes.Equal(buf, want[i]) {
				err = fmt.Errorf("stream %d: wrong bytes", i)
			}
			done <- err
		}()
	}
	for range files {
		if err := <-done; err != nil {
			t.Error(err)
		}
	}
	streamMem.mu.Lock()
	used := streamMem.used
	streamMem.mu.Unlock()
	if used > 2<<20 {
		t.Errorf("%d bytes held, over the budget", used)
	}
	got := make([]byte, holeArticle)
	if _, err := files[0].ReadAt(got, 0); err != nil || !bytes.Equal(got, want[0][:holeArticle]) {
		t.Errorf("reading the start again: %v", err)
	}
}

// A stream that's gone quiet lets go of its articles, session or no session.
func TestQuietStreamLetsGoOfItsArticles(t *testing.T) {
	srv, nzb, data := holeFixture(t, 100, mkvMagic)
	f := openHoleFixture(t, srv, nzb)
	if !bytes.Equal(wholeFile(t, f), data) {
		t.Fatal("wrong bytes")
	}
	src := f.spans[0].src
	waitFor(t, func() bool { return streamMem.has(src, 99) })
	streamMem.sweep(time.Now()) // just read: stays
	if !streamMem.has(src, 99) {
		t.Error("a stream being read lost its articles")
	}
	streamMem.sweep(time.Now().Add(streamQuiet.time() + time.Minute))
	streamMem.mu.Lock()
	left := len(src.cache)
	streamMem.mu.Unlock()
	if left != 0 {
		t.Errorf("%d articles still held after going quiet", left)
	}
	if !bytes.Equal(wholeFile(t, f), data) {
		t.Error("wrong bytes after coming back")
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	for range 2000 {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("it didn't happen")
}

func cursorWindow(f *StreamFile) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	w := 0
	for _, c := range f.cursors {
		w = max(w, c.window)
	}
	return w
}

// A reader that keeps catching up with read-ahead gets more of it. One that
// never has to wait doesn't, and gives back what it got.
func TestReadAheadFollowsTheReader(t *testing.T) {
	srv, nzb, data := holeFixture(t, 600, mkvMagic)
	srv.latency.Store(int64(30 * time.Millisecond))
	fast := openHoleFixture(t, srv, nzb)
	if !bytes.Equal(wholeFile(t, fast), data) {
		t.Fatal("wrong bytes")
	}
	if w := cursorWindow(fast); w <= streamAheadStart {
		t.Errorf("a reader as fast as it can go stayed at %d", w)
	}

	srv2, nzb2, _ := holeFixture(t, 120, mkvMagic)
	srv2.latency.Store(int64(time.Millisecond))
	slow := openHoleFixture(t, srv2, nzb2)
	buf := make([]byte, holeArticle)
	src := slow.spans[0].src
	for i := range 40 {
		// A film at its own pace: its next article is there before it's wanted.
		waitFor(t, func() bool { return i == 0 || streamMem.has(src, i) })
		if _, err := slow.ReadAt(buf, int64(i)*holeArticle); err != nil {
			t.Fatal(err)
		}
	}
	if w := cursorWindow(slow); w != streamAheadStart {
		t.Errorf("a reader that never waits went to %d", w)
	}
	_ = data
}

func TestReadAheadShrinksWhenNotNeeded(t *testing.T) {
	srv, nzb, _ := holeFixture(t, 2, mkvMagic)
	f := openHoleFixture(t, srv, nzb)
	sp := f.spans[0]
	// Every read lands on an article already in memory.
	streamMem.put(sp.src, 1, make([]byte, holeArticle))
	f.mu.Lock()
	f.cursors = []*readCursor{{window: streamAheadMax, next: 0, article: -1, moved: true, last: time.Now()}}
	c := f.cursors[0]
	f.mu.Unlock()
	for range 2 * streamAheadMax {
		f.mu.Lock()
		c.article, c.next = 5, holeArticle // another article each time, as far as it's told
		f.mu.Unlock()
		f.noteRead(sp, holeArticle, 1)
	}
	if c.window >= streamAheadMax {
		t.Errorf("still %d", c.window)
	}
}

// After a jump, what the old place had queued is dropped at once, and the new
// place is served first.
func TestAJumpDropsStaleReadAhead(t *testing.T) {
	srv, nzb, data := holeFixture(t, 1000, mkvMagic)
	const latency = 100 * time.Millisecond
	srv.latency.Store(int64(latency))
	f := openHoleFixture(t, srv, nzb)
	buf := make([]byte, 100)
	if _, err := f.ReadAt(buf, 0); err != nil { // in memory already: read-ahead has only just started
		t.Fatal(err)
	}
	start := time.Now()
	got := make([]byte, holeArticle)
	if _, err := f.ReadAt(got, 800*holeArticle); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took > 3*latency {
		t.Errorf("the jump took %v behind %v of stale work", took, latency)
	}
	if !bytes.Equal(got, data[800*holeArticle:801*holeArticle]) {
		t.Error("wrong bytes")
	}
	time.Sleep(3 * latency)
	// Opening read the first 64 KB (4 of these small articles), and read-ahead had
	// started at most 12 more.
	for i := 4 + streamWorkersMin; i < streamAheadStart; i++ {
		if n := srv.hitCount(fmt.Sprintf("a%d@hole", i)); n != 0 {
			t.Errorf("article %d, queued for the old place, was fetched after the jump", i)
		}
	}
}

// Two readers taking turns (a player and a scan) each keep their read-ahead.
func TestTwoReadersBothGetReadAhead(t *testing.T) {
	srv, nzb, _ := holeFixture(t, 1000, mkvMagic)
	f := openHoleFixture(t, srv, nzb)
	buf := make([]byte, holeArticle)
	for i := range 5 {
		if _, err := f.ReadAt(buf, int64(10+i)*holeArticle); err != nil {
			t.Fatal(err)
		}
		if _, err := f.ReadAt(buf, int64(500+i)*holeArticle); err != nil {
			t.Fatal(err)
		}
	}
	src := f.spans[0].src
	waitFor(t, func() bool { return streamMem.has(src, 20) && streamMem.has(src, 510) })
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.cursors) != 2 {
		t.Errorf("%d readers", len(f.cursors))
	}
}

// A connection that hangs costs a few seconds, not the film.
func TestHungConnectionDoesNotHangTheRead(t *testing.T) {
	setStreamTime(t, streamAttemptTimeout, 150*time.Millisecond)
	srv, nzb, data := holeFixture(t, 100, mkvMagic)
	srv.hangRequests("a50@hole", 1)
	f := openHoleFixture(t, srv, nzb)
	start := time.Now()
	if !bytes.Equal(wholeFile(t, f), data) {
		t.Error("wrong bytes")
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("took %v", took)
	}
	if srv.hitCount("a50@hole") < 2 {
		t.Errorf("the article wasn't asked for again")
	}
}

// A reader held up by read-ahead that's stuck asks a second connection, without
// waiting out the first one's timeout.
func TestReaderHedgesAStuckFetch(t *testing.T) {
	setStreamTime(t, streamAttemptTimeout, 20*time.Second)
	setStreamTime(t, streamHedgeAfter, 50*time.Millisecond)
	srv, nzb, data := holeFixture(t, 100, mkvMagic)
	srv.hangRequests("a8@hole", 1)
	f := openHoleFixture(t, srv, nzb)
	if _, err := f.ReadAt(make([]byte, 10), 0); err != nil { // read-ahead reaches 8, and sticks there
		t.Fatal(err)
	}
	waitFor(t, func() bool { return srv.hitCount("a8@hole") == 1 })
	start := time.Now()
	got := make([]byte, holeArticle)
	if _, err := f.ReadAt(got, 8*holeArticle); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("the reader waited %v", took)
	}
	if !bytes.Equal(got, data[8*holeArticle:9*holeArticle]) {
		t.Error("wrong bytes")
	}
}

// A server that dials but never greets costs one connect timeout.
func TestStreamConnectTimeout(t *testing.T) {
	setStreamTime(t, streamConnectTimeout, 100*time.Millisecond)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ln := listener.Addr().(*net.TCPAddr).Port
	go func() {
		for {
			c, err := listener.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { c.Close() }) // accepted, and never a word said
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := time.Now()
	_, err = fetchArticle(ctx, []ClientConfig{{Host: "127.0.0.1", Port: ln, Connections: 1}}, "x@y")
	if err == nil {
		t.Fatal("no error")
	}
	if took := time.Since(start); took > 3*time.Second {
		t.Errorf("took %v", took)
	}
}

// Read-ahead that failed isn't the reader's problem until the reader has tried.
func TestReadAheadFailureIsTriedAgainByTheReader(t *testing.T) {
	srv, nzb, data := holeFixture(t, 100, mkvMagic)
	srv.dropRequests("a10@hole", 2) // both of read-ahead's attempts
	f := openHoleFixture(t, srv, nzb)
	if _, err := f.ReadAt(make([]byte, 10), 0); err != nil {
		t.Fatal(err)
	}
	src := f.spans[0].src
	waitFor(t, func() bool {
		src.mu.Lock()
		defer src.mu.Unlock()
		return src.errs[10] != nil
	})
	got := make([]byte, holeArticle)
	if _, err := f.ReadAt(got, 10*holeArticle); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data[10*holeArticle:11*holeArticle]) {
		t.Error("wrong bytes")
	}
}

// Near the end of a RAR volume, the next volume's articles are read ahead too.
func TestReadAheadCrossesVolumes(t *testing.T) {
	video1 := bytes.Repeat([]byte{1}, 40*holeArticle)
	copy(video1, mkvMagic)
	video2 := bytes.Repeat([]byte{2}, 40*holeArticle)
	full := append(append([]byte(nil), video1...), video2...)
	vol := func(name string, data []byte, after, before bool) []byte {
		v := append(rar4Start(), rar4File("Film.2024.mkv", data, len(full), after, before)...)
		return append(v, rar4End...)
	}
	vols := map[string][]byte{"film.part1.rar": vol("a", video1, true, false), "film.part2.rar": vol("b", video2, false, true)}
	bodies := map[string][]byte{}
	var nzb NZB
	for name, v := range vols {
		f := NZBFile{Subject: `"` + name + `" yEnc (1/1)`}
		for i, n := 0, 0; n < len(v); i, n = i+1, n+holeArticle {
			id := fmt.Sprintf("%s-%d@v", name, i)
			bodies[id] = buildMultipartYenc(name, v, n+1, min(n+holeArticle, len(v)))
			f.Segments = append(f.Segments, NZBSegment{Number: i + 1, Bytes: holeArticle, MessageID: id})
		}
		nzb.Files = append(nzb.Files, f)
	}
	srv := newFakeNNTPServer(t, bodies)
	f := openHoleFixture(t, srv, &nzb)
	if f.Size != int64(len(full)) {
		t.Fatalf("size %d", f.Size)
	}
	if _, err := f.ReadAt(make([]byte, holeArticle), 35*holeArticle); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return srv.hitCount("film.part2.rar-5@v") > 0 })
	got := make([]byte, 2*holeArticle)
	if _, err := f.ReadAt(got, 39*holeArticle); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, full[39*holeArticle:41*holeArticle]) {
		t.Error("wrong bytes across the volumes")
	}
}

func TestStreamWorkersStayBelowTheProvidersLimit(t *testing.T) {
	for _, c := range []struct{ conns, window, want int }{
		{80, 24, 12},
		{80, 160, 40},
		{20, 160, 10},
		{2, 160, 4},
	} {
		if got := streamWorkersFor([]ClientConfig{{Connections: c.conns}}, c.window); got != c.want {
			t.Errorf("%+v: %d", c, got)
		}
	}
}
