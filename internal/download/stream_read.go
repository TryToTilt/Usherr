package download

// Usherr: how a stream's bytes get from Usenet to the reader.
//
//   - Memory. Fetched articles live in one budget shared by every stream,
//     instead of a fixed amount per file (a release in 50 RAR volumes could hold
//     8 GB that way). The oldest go first, and a stream nobody has read for a
//     couple of minutes lets go of its articles, however long its session stays
//     open.
//   - Read-ahead follows the reader. A reader starts with a modest window. If it
//     has to wait for articles while reading steadily (a remux, or a player
//     filling its buffer), the window and the connections behind it grow; if it
//     never waits (an ordinary film at its own pace), they shrink back, so a slow
//     reader doesn't hold connections it doesn't need.
//   - Nothing waits behind stale work. What a reader is waiting for starts right
//     away, and read-ahead is a list that's replaced as the reader moves, so a
//     jump drops what the old place had queued.
//   - Nothing hangs. A connection gets streamAttemptTimeout to answer, then it's
//     closed and the article is asked for on another. A reader kept waiting by
//     read-ahead that's stuck asks a second connection after streamHedgeAfter,
//     and the first answer wins.
//
// Adapted from InfiniDysk backend/Streams/MultiSegmentStream.cs and its prefetch
// batch sizer (MIT): the 8 s article timeout with retries on another connection,
// and read-ahead that grows up to 160 articles when the reader catches up with it.

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

const (
	streamAheadStart = 24  // articles read ahead of a new reader
	streamAheadMax   = 160 // at most, for one that keeps catching up
	streamWorkersMin = 12  // articles fetched at once, per file, to start with
	streamWorkersMax = 40  // at most
	streamMaxCursors = 4   // readers of one file told apart at once
)

// streamSetting is a number the read path goes by. Tests change them while
// goroutines of the test before may still be reading them.
type streamSetting struct{ atomic.Int64 }

func newStreamSetting(v int64) *streamSetting {
	s := &streamSetting{}
	s.Store(v)
	return s
}

func (s *streamSetting) time() time.Duration { return time.Duration(s.Load()) }

var (
	// streamMemLimit is the memory all streams' articles may take together.
	streamMemLimit = newStreamSetting(512 << 20)
	// streamQuiet is how long a stream keeps its articles after its last read.
	streamQuiet = newStreamSetting(int64(2 * time.Minute))
	// streamAttemptTimeout is what one connection gets to deliver an article;
	// the slowest 1% of articles at home take 3.6 s.
	streamAttemptTimeout = newStreamSetting(int64(8 * time.Second))
	// streamConnectTimeout is what a new connection gets to be ready to use.
	streamConnectTimeout = newStreamSetting(int64(6 * time.Second))
	// streamFetchTimeout bounds one article's fetch across all connections.
	streamFetchTimeout = newStreamSetting(int64(30 * time.Second))
	// streamHedgeAfter is how long a reader waits for a fetch already under way
	// before it asks a second connection as well.
	streamHedgeAfter = newStreamSetting(int64(3 * time.Second))
	// missingAskAgainAfter is the moment before a missing article is asked for again.
	missingAskAgainAfter = newStreamSetting(int64(300 * time.Millisecond))
)

// streamCursorActive is how recently a reader must have read for its read-ahead
// to go on. Long enough for two readers taking turns.
const streamCursorActive = time.Second

// ---- the shared memory budget ----

type cachedArticle struct {
	s    *nzbStream
	idx  int
	data []byte
}

// articleMemory holds every stream's articles, least recently used first.
type articleMemory struct {
	mu      sync.Mutex
	used    int64
	order   *list.List // of *cachedArticle
	readers map[*StreamFile]time.Time
}

var streamMem = &articleMemory{order: list.New(), readers: map[*StreamFile]time.Time{}}

func init() {
	go func() {
		for range time.Tick(30 * time.Second) {
			streamMem.sweep(time.Now())
		}
	}()
}

func (m *articleMemory) put(s *nzbStream, idx int, data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := s.cache[idx]; ok {
		return
	}
	s.cache[idx] = m.order.PushBack(&cachedArticle{s: s, idx: idx, data: data})
	m.used += int64(len(data))
	for m.used > streamMemLimit.Load() && m.order.Len() > 1 {
		m.removeLocked(m.order.Front())
	}
}

func (m *articleMemory) removeLocked(el *list.Element) {
	a := el.Value.(*cachedArticle)
	delete(a.s.cache, a.idx)
	m.used -= int64(len(a.data))
	m.order.Remove(el)
}

// get answers an article's data and marks it as just used.
func (m *articleMemory) get(s *nzbStream, idx int) ([]byte, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	el, ok := s.cache[idx]
	if !ok {
		return nil, false
	}
	m.order.MoveToBack(el)
	return el.Value.(*cachedArticle).data, true
}

func (m *articleMemory) has(s *nzbStream, idx int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := s.cache[idx]
	return ok
}

// sweep lets go of the articles of streams that have gone quiet, and forgets
// readers that have.
func (m *articleMemory) sweep(now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for el := m.order.Front(); el != nil; {
		next := el.Next()
		if a := el.Value.(*cachedArticle); now.Sub(time.Unix(0, a.s.lastRead.Load())) > streamQuiet.time() {
			m.removeLocked(el)
		}
		el = next
	}
	for f, at := range m.readers {
		if now.Sub(at) > streamQuiet.time() {
			delete(m.readers, f)
		}
	}
}

// reading notes that a video is being read, and answers how many are.
func (m *articleMemory) reading(f *StreamFile, now time.Time) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.readers[f] = now
	return len(m.readers)
}

// ---- one file of the NZB ----

// articleFetch is an article on its way, on one connection or, once a reader
// has lost patience, two.
type articleFetch struct {
	done    chan struct{}
	running int
	hedged  bool
	closed  bool
	err     error
}

// nzbStream reads one file of an NZB at any offset.
type nzbStream struct {
	servers  []ClientConfig
	file     NZBFile
	name     string // from the yEnc header (the real name, even when the NZB's is obfuscated)
	size     int64
	partSize int64 // every article but the last holds this many bytes
	lastRead atomic.Int64

	cache map[int]*list.Element // guarded by streamMem.mu

	mu           sync.Mutex
	inflight     map[int]*articleFetch
	errs         map[int]error     // failed fetches, until a reader comes for them
	holes        map[int]time.Time // articles every server says are gone, and since when
	ahead        []int             // what to read ahead, next first
	aheadRunning int
	workers      int
}

// openNZBFile reads a file's first article: its real name, size and article size.
func openNZBFile(ctx context.Context, servers []ClientConfig, f NZBFile) (*nzbStream, error) {
	segs := append([]NZBSegment(nil), f.Segments...)
	sort.Slice(segs, func(a, b int) bool { return segs[a].Number < segs[b].Number })
	f.Segments = segs
	if len(segs) == 0 {
		return nil, errors.New("a file with no articles")
	}
	part, err := fetchArticle(ctx, servers, segs[0].MessageID)
	if err != nil {
		return nil, err
	}
	s := &nzbStream{servers: servers, file: f, name: part.Name, cache: map[int]*list.Element{},
		inflight: map[int]*articleFetch{}, errs: map[int]error{}, holes: map[int]time.Time{}, workers: streamWorkersMin}
	s.lastRead.Store(time.Now().UnixNano())
	s.size = part.Size
	s.partSize = int64(len(part.Data))
	if part.PartBegin > 0 && part.PartEnd >= part.PartBegin {
		s.partSize = part.PartEnd - part.PartBegin + 1
	}
	if s.size <= 0 {
		s.size = s.partSize * int64(len(segs))
	}
	if s.partSize <= 0 {
		return nil, errors.New("an empty article")
	}
	streamMem.put(s, 0, part.Data)
	return s, nil
}

// article returns article idx's data, fetching it when it isn't in memory.
func (s *nzbStream) article(ctx context.Context, idx int) ([]byte, error) {
	tried := false
	for {
		if d, ok := streamMem.get(s, idx); ok {
			return d, nil
		}
		s.mu.Lock()
		if s.holeKnownLocked(idx) {
			s.mu.Unlock()
			return nil, ErrArticleMissing
		}
		if af, busy := s.inflight[idx]; busy {
			s.mu.Unlock()
			if err := s.wait(ctx, idx, af); err != nil {
				return nil, err
			}
			continue
		}
		if err := s.errs[idx]; err != nil {
			delete(s.errs, idx)
			if tried {
				s.mu.Unlock()
				return nil, err
			}
			// Read-ahead's failure, from who knows how long ago: this reader
			// tries for itself before it gets to hear about it.
		}
		tried = true
		s.startLocked(idx, false)
		s.mu.Unlock()
	}
}

// wait waits for a fetch under way, asking a second connection if it takes long.
func (s *nzbStream) wait(ctx context.Context, idx int, af *articleFetch) error {
	patience := time.NewTimer(streamHedgeAfter.time())
	defer patience.Stop()
	select {
	case <-af.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-patience.C:
	}
	s.mu.Lock()
	if !af.closed && !af.hedged {
		af.hedged = true
		af.running++
		go s.fetch(idx, af, false)
	}
	s.mu.Unlock()
	select {
	case <-af.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// startLocked starts fetching an article that isn't on its way.
func (s *nzbStream) startLocked(idx int, ahead bool) {
	af := &articleFetch{done: make(chan struct{}), running: 1}
	s.inflight[idx] = af
	if ahead {
		s.aheadRunning++
	}
	go s.fetch(idx, af, ahead)
}

func (s *nzbStream) fetch(idx int, af *articleFetch, ahead bool) {
	ctx, cancel := context.WithTimeout(context.Background(), streamFetchTimeout.time())
	part, err := fetchArticle(ctx, s.servers, s.file.Segments[idx].MessageID)
	cancel()
	if err == nil && part.PartBegin > 0 && part.PartBegin-1 != int64(idx)*s.partSize {
		err = fmt.Errorf("article %d holds bytes from %d, not %d: the file's articles are uneven", idx, part.PartBegin-1, int64(idx)*s.partSize)
	}
	if err == nil {
		streamMem.put(s, idx, part.Data)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if ahead {
		s.aheadRunning--
		s.pumpLocked()
	}
	af.running--
	if af.closed {
		return
	}
	if err != nil && af.running > 0 {
		return // the other connection may yet get it
	}
	if err == nil {
		delete(s.holes, idx)
	} else {
		s.errs[idx] = err
		if errors.Is(err, ErrArticleMissing) {
			s.holes[idx] = time.Now()
		}
	}
	af.closed = true
	delete(s.inflight, idx)
	close(af.done)
}

// setAhead replaces what's to be read ahead: what was queued for a place the
// reader has left is forgotten. Fetches under way finish; their articles may be
// wanted yet, and closing a connection costs more than letting it finish.
func (s *nzbStream) setAhead(list []int, workers int) {
	s.mu.Lock()
	s.ahead, s.workers = list, workers
	s.pumpLocked()
	s.mu.Unlock()
}

func (s *nzbStream) pumpLocked() {
	for s.aheadRunning < s.workers && len(s.ahead) > 0 {
		idx := s.ahead[0]
		s.ahead = s.ahead[1:]
		if _, busy := s.inflight[idx]; busy || s.holeKnownLocked(idx) || s.errs[idx] != nil || streamMem.has(s, idx) {
			continue
		}
		s.startLocked(idx, true)
	}
}

// holeKnownLocked says whether an article was found missing a moment ago, so
// it isn't asked for again on every read. After a while it is.
func (s *nzbStream) holeKnownLocked(idx int) bool {
	at, ok := s.holes[idx]
	if ok && time.Since(at) >= holeRemember {
		delete(s.holes, idx)
		delete(s.errs, idx)
		return false
	}
	return ok
}

func (s *nzbStream) forgetHole(idx int) {
	s.mu.Lock()
	delete(s.holes, idx)
	delete(s.errs, idx)
	s.mu.Unlock()
}

// readAt reads the file's bytes at off.
func (s *nzbStream) readAt(ctx context.Context, p []byte, off int64) (int, error) {
	s.lastRead.Store(time.Now().UnixNano())
	n := 0
	for n < len(p) {
		at := off + int64(n)
		if at >= s.size {
			return n, io.EOF
		}
		// The last article can be a little longer than the others.
		idx := min(int(at/s.partSize), len(s.file.Segments)-1)
		data, err := s.article(ctx, idx)
		if err != nil {
			return n, err
		}
		in := at - int64(idx)*s.partSize
		if in >= int64(len(data)) {
			return n, io.ErrUnexpectedEOF
		}
		n += copy(p[n:], data[in:])
	}
	return n, nil
}

// firstBytes are a file's bytes from its first article, while they're in memory.
func (s *nzbStream) firstBytes() []byte { return s.firstBytesAt(0) }

func (s *nzbStream) firstBytesAt(off int64) []byte {
	if first, ok := streamMem.get(s, 0); ok && off < int64(len(first)) {
		return first[off:]
	}
	return nil
}

// ---- following a video's readers ----

// readCursor is one reader of a video, told apart by where it reads.
type readCursor struct {
	next    int64      // where its next read is expected
	window  int        // articles read ahead for it
	src     *nzbStream // the file and article it was last in, to act once per article
	article int64
	settled int  // articles in a row it didn't have to wait for
	moved   bool // it has read on from where it began
	last    time.Time
}

// noteRead follows a read of the video at at: it finds the reader it belongs
// to, adapts that reader's read-ahead, and when the reader has moved on to
// another article, says what to fetch.
func (f *StreamFile) noteRead(sp span, at, n int64) {
	now := time.Now()
	partSize := sp.src.partSize
	f.mu.Lock()
	defer f.mu.Unlock()
	var c *readCursor
	for _, o := range f.cursors {
		if at >= o.next-(1<<20) && at <= o.next+int64(o.window)*partSize {
			c = o
			break
		}
	}
	fresh := c == nil
	if fresh {
		c = &readCursor{window: streamAheadStart, article: -1}
		if len(f.cursors) >= streamMaxCursors {
			oldest := 0
			for i, o := range f.cursors {
				if o.last.Before(f.cursors[oldest].last) {
					oldest = i
				}
			}
			f.cursors = append(f.cursors[:oldest], f.cursors[oldest+1:]...)
		}
		f.cursors = append(f.cursors, c)
	} else if at >= c.next {
		c.moved = true
	}
	c.next, c.last = at+n, now
	srcAt := sp.srcOff + (at - sp.off)
	article := srcAt / partSize
	if article == c.article && sp.src == c.src {
		return
	}
	c.src, c.article = sp.src, article
	readers := streamMem.reading(f, now)
	if c.moved {
		if streamMem.has(sp.src, int(article)) {
			if c.settled++; c.settled >= 2*c.window {
				c.settled = 0
				c.window = max(c.window*3/4, streamAheadStart)
			}
		} else {
			// It caught up with what was fetched for it: more is needed.
			c.settled = 0
			c.window = min(c.window*3/2+1, f.maxAhead(partSize, readers))
		}
	}
	f.planLocked(c, now, fresh)
}

// maxAhead is the most read-ahead one reader may have: a share of the memory, so
// that what's fetched for it isn't pushed out before it gets there.
func (f *StreamFile) maxAhead(partSize int64, readers int) int {
	share := streamMemLimit.Load() / int64(max(readers, 1)) * 3 / 4
	return int(max(min(share/partSize, streamAheadMax), streamAheadStart/2))
}

// planLocked tells each file of the video what to read ahead: for the reader
// just seen first, then for any other that's reading right now. A reader that
// has just appeared most often is the same one, jumping: what the old place had
// queued goes. If it was another reader, it asks for its own again at its next
// article.
func (f *StreamFile) planLocked(current *readCursor, now time.Time, jumped bool) {
	plan := map[*nzbStream][]int{}
	seen := map[holeKey]bool{}
	widest := current.window
	add := func(c *readCursor) {
		left := c.window
		// The next c.window articles from where it reads, into later volumes too.
		for i := sort.Search(len(f.spans), func(i int) bool { return f.spans[i].off+f.spans[i].n > c.next }); i < len(f.spans) && left > 0; i++ {
			sp := f.spans[i]
			from := sp.srcOff + max(c.next-sp.off, 0)
			end := sp.srcOff + sp.n
			for idx := from / sp.src.partSize; idx*sp.src.partSize < end && left > 0 && idx < int64(len(sp.src.file.Segments)); idx++ {
				k := holeKey{sp.src, int(idx)}
				if !seen[k] {
					seen[k] = true
					plan[sp.src] = append(plan[sp.src], int(idx))
				}
				left--
			}
		}
	}
	add(current)
	for _, c := range f.cursors {
		if c != current && !jumped && now.Sub(c.last) < streamCursorActive {
			add(c)
			widest = max(widest, c.window)
		}
	}
	workers := streamWorkersFor(f.spans[0].src.servers, widest)
	if f.sources == nil {
		have := map[*nzbStream]bool{}
		for _, sp := range f.spans {
			if !have[sp.src] {
				have[sp.src] = true
				f.sources = append(f.sources, sp.src)
			}
		}
	}
	for _, src := range f.sources {
		src.setAhead(plan[src], workers)
	}
}

// streamWorkersFor is how many articles of a file are fetched at once for a
// window of read-ahead: never more than half of what the provider allows, so one
// stream can't take every connection.
func streamWorkersFor(servers []ClientConfig, window int) int {
	limit := streamWorkersMax
	if len(servers) > 0 {
		limit = min(limit, max(servers[0].Connections/2, 4))
	}
	return min(max(window/2, streamWorkersMin), limit)
}
