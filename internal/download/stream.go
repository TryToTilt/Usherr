package download

// Usherr: streaming from Usenet. A file in an NZB is read as it's watched:
// only the articles under the part being played (and a little ahead) are
// fetched, decoded and kept in memory for a while. Nothing is written to
// disk. Each yEnc article says which bytes of the file it holds, so a jump
// anywhere in the file goes straight to the right article.
//
// Connections come from the same per-server limit as downloads, so streams
// and downloads together never open more than a provider allows.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"
)

// ErrArticleMissing means no server has an article of the file: the release
// is incomplete, and another release should be tried.
var ErrArticleMissing = errors.New("an article of this release is missing on every server")

// ---- connections ----

type pooledConn struct {
	c       *NNTPConn
	cfg     ClientConfig
	release func()
	idle    time.Time
}

// streamPool keeps a few connections open between reads, so a stream doesn't
// dial for every article. Idle ones are closed after a short while: their
// places are shared with downloads.
type streamPool struct {
	mu   sync.Mutex
	idle map[gateKey][]*pooledConn
}

var pool = &streamPool{idle: map[gateKey][]*pooledConn{}}

const streamIdle = 40 * time.Second

func init() {
	go func() {
		for range time.Tick(10 * time.Second) {
			pool.reap()
		}
	}()
}

func (p *streamPool) reap() {
	p.mu.Lock()
	var old []*pooledConn
	for k, list := range p.idle {
		keep := list[:0]
		for _, pc := range list {
			if time.Since(pc.idle) > streamIdle {
				old = append(old, pc)
			} else {
				keep = append(keep, pc)
			}
		}
		p.idle[k] = keep
	}
	p.mu.Unlock()
	for _, pc := range old {
		pc.close()
	}
}

func (pc *pooledConn) close() {
	g := gateFor(pc.cfg)
	g.untrack(pc.c)
	g.noteClosed()
	pc.c.Quit()
	pc.release()
}

func (p *streamPool) get(ctx context.Context, cfg ClientConfig) (*pooledConn, error) {
	k := keyFor(cfg)
	p.mu.Lock()
	if list := p.idle[k]; len(list) > 0 {
		pc := list[len(list)-1]
		p.idle[k] = list[:len(list)-1]
		p.mu.Unlock()
		return pc, nil
	}
	p.mu.Unlock()
	g := gateFor(cfg)
	release, err := g.acquire(ctx)
	if err != nil {
		return nil, err
	}
	c, err := DialNNTP(cfg.Host, cfg.Port, cfg.UseSSL, 20*time.Second)
	if err == nil {
		if err = c.Authenticate(cfg.Username, cfg.Password); err != nil {
			c.Quit()
		}
	}
	if err != nil {
		release()
		if IsTooManyConnections(err) {
			g.tooMany()
		}
		return nil, err
	}
	g.track(c)
	g.succeeded()
	return &pooledConn{c: c, cfg: cfg, release: release}, nil
}

func (p *streamPool) put(pc *pooledConn) {
	pc.idle = time.Now()
	k := keyFor(pc.cfg)
	p.mu.Lock()
	p.idle[k] = append(p.idle[k], pc)
	p.mu.Unlock()
}

// fetchArticle gets one article, decoded, from the first server that has a
// good copy (servers in their order: primary first).
func fetchArticle(ctx context.Context, servers []ClientConfig, messageID string) (*YencPart, error) {
	missing := 0
	var last error
	for _, cfg := range servers {
		for attempt := 0; attempt < 2; attempt++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			pc, err := pool.get(ctx, cfg)
			if err != nil {
				last = err
				break
			}
			// A read that runs past the stream's patience gives the connection up.
			stop := context.AfterFunc(ctx, func() { pc.c.Close() })
			body, err := pc.c.FetchBody(messageID)
			stop()
			var ne *NNTPError
			if errors.As(err, &ne) && (ne.Code == 430 || ne.Code == 423) {
				pool.put(pc)
				missing++
				last = err
				break // this server doesn't have it: the next one may
			}
			if err != nil {
				pc.close()
				last = err
				continue // a broken connection: once more on a fresh one
			}
			pool.put(pc)
			part, err := DecodeYenc(bytes.NewReader(body))
			if err != nil || part.Damaged() {
				last = fmt.Errorf("article %s doesn't decode", messageID)
				break
			}
			return part, nil
		}
	}
	if missing == len(servers) {
		return nil, ErrArticleMissing
	}
	if last == nil {
		last = ErrArticleMissing
	}
	return nil, last
}

// ---- one file of the NZB ----

// nzbStream reads one file of an NZB at any offset.
type nzbStream struct {
	servers  []ClientConfig
	file     NZBFile
	name     string // from the yEnc header (the real name, even when the NZB's is obfuscated)
	size     int64
	partSize int64 // every article but the last holds this many bytes
	first    []byte

	mu       sync.Mutex
	cache    map[int][]byte
	order    []int // oldest first
	cached   int64
	inflight map[int]chan struct{}
	errs     map[int]error
	sem      chan struct{}
}

const (
	streamCacheBytes = 160 << 20 // per file: about 25 s of a 4K stream
	streamAhead      = 24        // articles read ahead of the one playing
	streamWorkers    = 12        // articles fetched at once, per file
)

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
	s := &nzbStream{servers: servers, file: f, name: part.Name, first: part.Data,
		cache: map[int][]byte{}, inflight: map[int]chan struct{}{}, errs: map[int]error{}, sem: make(chan struct{}, streamWorkers)}
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
	s.store(0, part.Data)
	return s, nil
}

func (s *nzbStream) store(idx int, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.cache[idx]; ok {
		return
	}
	s.cache[idx] = data
	s.order = append(s.order, idx)
	s.cached += int64(len(data))
	for s.cached > streamCacheBytes && len(s.order) > 1 {
		old := s.order[0]
		s.order = s.order[1:]
		s.cached -= int64(len(s.cache[old]))
		delete(s.cache, old)
	}
}

// article returns article idx's data, fetching it when it isn't in memory,
// and with ahead set, starting the ones after it.
func (s *nzbStream) article(ctx context.Context, idx int, ahead bool) ([]byte, error) {
	if ahead {
		s.readAhead(idx+1, streamAhead)
	}
	for {
		s.mu.Lock()
		if d, ok := s.cache[idx]; ok {
			s.mu.Unlock()
			return d, nil
		}
		if err := s.errs[idx]; err != nil {
			delete(s.errs, idx) // a later read may try again
			s.mu.Unlock()
			return nil, err
		}
		wait, busy := s.inflight[idx]
		s.mu.Unlock()
		if !busy {
			s.start(idx)
			continue
		}
		select {
		case <-wait:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// readAhead starts fetching n articles from idx on.
func (s *nzbStream) readAhead(idx, n int) {
	for i := idx; i < idx+n && i < len(s.file.Segments); i++ {
		s.start(i)
	}
}

// start fetches article idx in the background, unless it's there or coming.
func (s *nzbStream) start(idx int) {
	s.mu.Lock()
	if _, ok := s.cache[idx]; ok {
		s.mu.Unlock()
		return
	}
	if _, ok := s.inflight[idx]; ok {
		s.mu.Unlock()
		return
	}
	done := make(chan struct{})
	s.inflight[idx] = done
	s.mu.Unlock()
	go func() {
		s.sem <- struct{}{}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		part, err := fetchArticle(ctx, s.servers, s.file.Segments[idx].MessageID)
		cancel()
		<-s.sem
		if err == nil && part.PartBegin > 0 && part.PartBegin-1 != int64(idx)*s.partSize {
			err = fmt.Errorf("article %d holds bytes from %d, not %d: the file's articles are uneven", idx, part.PartBegin-1, int64(idx)*s.partSize)
		}
		if err == nil {
			s.store(idx, part.Data)
		}
		s.mu.Lock()
		if err != nil {
			s.errs[idx] = err
		}
		delete(s.inflight, idx)
		s.mu.Unlock()
		close(done)
	}()
}

// ReadAt reads the file's bytes at off, reading ahead of them.
func (s *nzbStream) ReadAt(p []byte, off int64) (int, error) {
	return s.readAt(context.Background(), p, off, true)
}

// readAt reads at off; ahead says whether to read ahead (not for a header).
func (s *nzbStream) readAt(ctx context.Context, p []byte, off int64, ahead bool) (int, error) {
	n := 0
	for n < len(p) {
		at := off + int64(n)
		if at >= s.size {
			return n, io.EOF
		}
		idx := int(at / s.partSize)
		if idx >= len(s.file.Segments) {
			return n, io.EOF
		}
		data, err := s.article(ctx, idx, ahead)
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

// ---- the playable file ----

// StreamFile is the video in a release, ready to read at any offset: a file
// of the NZB as it is, or the file inside a set of stored RAR volumes.
type StreamFile struct {
	Name  string
	Size  int64
	spans []span
}

// span is a stretch of the video held in one NZB file.
type span struct {
	src    *nzbStream
	srcOff int64 // where in that file
	off    int64 // where in the video
	n      int64
}

// Parts describes where the video is, for the log: each volume's name, and
// where its stretch starts and how long it is.
func (f *StreamFile) Parts() []string {
	var out []string
	for _, sp := range f.spans {
		out = append(out, fmt.Sprintf("%s @%d +%d", sp.src.name, sp.srcOff, sp.n))
	}
	return out
}

// ReadAt reads the video's bytes at off.
func (f *StreamFile) ReadAt(p []byte, off int64) (int, error) {
	n := 0
	for n < len(p) {
		at := off + int64(n)
		if at >= f.Size {
			return n, io.EOF
		}
		i := sort.Search(len(f.spans), func(i int) bool { return f.spans[i].off+f.spans[i].n > at })
		if i == len(f.spans) {
			return n, io.EOF
		}
		sp := f.spans[i]
		want := min(int64(len(p)-n), sp.off+sp.n-at)
		// Near the end of one volume, the next one's start is read ahead too.
		if i+1 < len(f.spans) && sp.off+sp.n-at < int64(streamAhead)*sp.src.partSize {
			nx := f.spans[i+1]
			nx.src.readAhead(int(nx.srcOff/nx.src.partSize), streamAhead/2)
		}
		m, err := sp.src.ReadAt(p[n:n+int(want)], sp.srcOff+(at-sp.off))
		n += m
		streamBytes.Add(int64(m))
		if err != nil && !(errors.Is(err, io.EOF) && int64(m) == want) {
			return n, err
		}
	}
	return n, nil
}

// looksLikeVideo says whether a file starts as a video does: Matroska,
// MP4/MOV, MPEG-TS or AVI.
func looksLikeVideo(s *nzbStream) bool {
	s.mu.Lock()
	b := s.cache[0]
	s.mu.Unlock()
	switch {
	case len(b) >= 4 && b[0] == 0x1a && b[1] == 0x45 && b[2] == 0xdf && b[3] == 0xa3:
		return true // Matroska, WebM
	case len(b) >= 12 && string(b[4:8]) == "ftyp":
		return true // MP4, MOV
	case len(b) >= 12 && string(b[0:4]) == "RIFF" && string(b[8:11]) == "AVI":
		return true
	case len(b) >= 377 && b[0] == 0x47 && b[188] == 0x47 && b[376] == 0x47:
		return true // MPEG-TS
	}
	return false
}

var videoExts = []string{".mkv", ".mp4", ".m4v", ".avi", ".ts", ".m2ts", ".mov", ".wmv", ".webm"}

func isVideoName(name string) bool {
	l := strings.ToLower(name)
	for _, e := range videoExts {
		if strings.HasSuffix(l, e) {
			return true
		}
	}
	return false
}

// OpenStream finds the video in a release and makes it readable. It reads
// the first article of each file (real names, sizes, archive headers); the
// rest is read as it's played. A release that needs unpacking (compressed or
// locked archives) is refused, so the next release can be tried.
func OpenStream(ctx context.Context, servers []ClientConfig, nzb *NZB) (*StreamFile, error) {
	if len(servers) == 0 {
		return nil, errors.New("no Usenet server is set up")
	}
	// Leave out what's never the video (par2 repair files, nfo, sfv, samples by size later).
	var files []NZBFile
	for _, f := range nzb.Files {
		l := strings.ToLower(f.Subject)
		if strings.Contains(l, ".par2") || strings.Contains(l, ".nfo") || strings.Contains(l, ".sfv") || strings.Contains(l, ".nzb") {
			continue
		}
		files = append(files, f)
	}
	if len(files) == 0 {
		return nil, errors.New("the release has no files to play")
	}
	streams := make([]*nzbStream, len(files))
	errs := make([]error, len(files))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, f := range files {
		wg.Add(1)
		go func(i int, f NZBFile) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			streams[i], errs[i] = openNZBFile(ctx, servers, f)
		}(i, f)
	}
	wg.Wait()
	var opened []*nzbStream
	for i, s := range streams {
		if errs[i] != nil {
			if errors.Is(errs[i], ErrArticleMissing) {
				return nil, errs[i]
			}
			continue
		}
		opened = append(opened, s)
	}
	if len(opened) == 0 {
		return nil, fmt.Errorf("couldn't read the release: %w", errors.Join(errs...))
	}
	// A video file as it is: the biggest (by its name, or by what it is when
	// the name is scrambled).
	var best *nzbStream
	for _, s := range opened {
		if (isVideoName(s.name) || looksLikeVideo(s)) && (best == nil || s.size > best.size) {
			best = s
		}
	}
	// RAR volumes: the video inside, if it's stored (not compressed).
	if f, err := rarStream(opened); err == nil && (best == nil || f.Size > best.size) {
		return f, nil
	} else if best == nil {
		if err == nil {
			err = errors.New("no video in the release")
		}
		return nil, err
	}
	name := best.name
	if !isVideoName(name) {
		name += ".mkv" // a scrambled name: the player goes by what it is anyway
	}
	return &StreamFile{Name: name, Size: best.size, spans: []span{{src: best, n: best.size}}}, nil
}
