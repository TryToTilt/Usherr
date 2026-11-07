package download

// Usherr: streaming from Usenet. A file in an NZB is read as it's watched:
// only the articles under the part being played (and some ahead) are
// fetched, decoded and kept in memory for a while (stream_read.go). Nothing is
// written to disk. Each yEnc article says which bytes of the file it holds, so
// a jump anywhere in the file goes straight to the right article.
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

// ErrArticleMissing means no server has an article of the file, or only a
// damaged copy. It doesn't by itself end a stream (stream_holes.go).
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
	// A hung connection mustn't hold up a stream, while it dials or signs in.
	cctx, cancel := context.WithTimeout(ctx, streamConnectTimeout.time())
	defer cancel()
	c, err := DialNNTPContext(cctx, cfg.Host, cfg.Port, cfg.UseSSL, streamConnectTimeout.time())
	if err == nil {
		stop := context.AfterFunc(cctx, func() { c.Close() })
		if err = c.Authenticate(cfg.Username, cfg.Password); err != nil {
			c.Quit()
		}
		stop()
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
//
// When every server says it's gone, the first is asked once more after a moment:
// a provider now and then answers "no such article" for one it does have.
func fetchArticle(ctx context.Context, servers []ClientConfig, messageID string) (*YencPart, error) {
	part, err := fetchArticleOnce(ctx, servers, messageID)
	if !errors.Is(err, ErrArticleMissing) || len(servers) == 0 {
		return part, err
	}
	select {
	case <-time.After(missingAskAgainAfter.time()):
	case <-ctx.Done():
		return nil, err
	}
	if part, again := fetchArticleOnce(ctx, servers[:1], messageID); again == nil {
		return part, nil
	}
	return nil, err
}

func fetchArticleOnce(ctx context.Context, servers []ClientConfig, messageID string) (*YencPart, error) {
	missing := 0 // servers that don't have it, or only a damaged copy
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
			// A read that runs past the stream's patience gives the connection
			// up, and the article is asked for on another.
			actx, cancel := context.WithTimeout(ctx, streamAttemptTimeout.time())
			stop := context.AfterFunc(actx, func() { pc.c.Close() })
			body, err := pc.c.FetchBody(messageID)
			stop()
			cancel()
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
				missing++
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

// ---- the playable file ----

// StreamFile is the video in a release, ready to read at any offset: a file
// of the NZB as it is, or the file inside a set of stored RAR volumes.
type StreamFile struct {
	Name  string
	Size  int64
	spans []span
	ra    io.ReaderAt // Usherr: a stand-in's bytes (StreamFileFrom), in place of spans
	guard *holeGuard  // Usherr: its missing articles (stream_holes.go)

	mu      sync.Mutex // Usherr: who's reading it (stream_read.go)
	cursors []*readCursor
	sources []*nzbStream
}

// StreamFileFrom is a StreamFile read from r instead of from Usenet: for a
// stand-in in tests, so code that serves streams can be tried without a
// news server.
func StreamFileFrom(name string, r io.ReaderAt, size int64) *StreamFile {
	return &StreamFile{Name: name, Size: size, ra: r}
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
	if f.ra != nil {
		if off >= f.Size {
			return 0, io.EOF
		}
		if rest := f.Size - off; int64(len(p)) > rest {
			n, err := f.ra.ReadAt(p[:rest], off)
			if err == nil {
				err = io.EOF
			}
			return n, err
		}
		return f.ra.ReadAt(p, off)
	}
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
		// An article at a time: each one is a chance to see whether the reader
		// had to wait for it, and to move read-ahead along.
		srcAt := sp.srcOff + (at - sp.off)
		want := min(int64(len(p)-n), sp.off+sp.n-at, sp.src.partSize-srcAt%sp.src.partSize)
		f.noteRead(sp, at, want)
		m, err := sp.src.readAt(context.Background(), p[n:n+int(want)], srcAt)
		n += m
		streamBytes.Add(int64(m))
		if errors.Is(err, ErrArticleMissing) && f.guard != nil {
			filled, ferr := f.fillHole(p[n:n+int(want)-m], sp, at+int64(m))
			if ferr != nil {
				return n, ferr
			}
			n += filled
			streamBytes.Add(int64(filled))
			continue
		}
		if err != nil && !(errors.Is(err, io.EOF) && int64(m) == want) {
			return n, err
		}
	}
	return n, nil
}

// fillHole answers a read that reached a missing article: zeros up to the end
// of the article, when the video can take it, or ErrTooManyHoles.
func (f *StreamFile) fillHole(p []byte, sp span, at int64) (int, error) {
	srcAt := sp.srcOff + (at - sp.off)
	idx := srcAt / sp.src.partSize
	start := sp.off + idx*sp.src.partSize - sp.srcOff // where the article is in the video
	if !f.guard.allow(sp.src, int(idx), start, sp.src.partSize) {
		return 0, ErrTooManyHoles
	}
	n := min(int64(len(p)), (idx+1)*sp.src.partSize-srcAt)
	clear(p[:n])
	return int(n), nil
}

// withGuard gets a video ready to count its missing articles, going by what its
// first bytes say it is.
func (f *StreamFile) withGuard() *StreamFile {
	var head []byte
	if len(f.spans) > 0 {
		head = f.spans[0].src.firstBytesAt(f.spans[0].srcOff)
	}
	f.guard = newHoleGuard(f.Name, f.Size, head)
	return f
}

// looksLikeVideo says whether a file starts as a video does: Matroska,
// MP4/MOV, MPEG-TS or AVI.
func looksLikeVideo(s *nzbStream) bool {
	b := s.firstBytes()
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

// streamableFiles leaves out what's never the video (par2 repair files, nfo,
// sfv, samples by size later).
func streamableFiles(nzb *NZB) []NZBFile {
	var files []NZBFile
	for _, f := range nzb.Files {
		l := strings.ToLower(f.Subject)
		if strings.Contains(l, ".par2") || strings.Contains(l, ".nfo") || strings.Contains(l, ".sfv") || strings.Contains(l, ".nzb") {
			continue
		}
		files = append(files, f)
	}
	return files
}

// OpenStream finds the biggest video in a release and makes it readable.
func OpenStream(ctx context.Context, servers []ClientConfig, nzb *NZB) (*StreamFile, error) {
	return OpenStreamFor(ctx, servers, nzb, StreamWant{})
}

// OpenStreamFor finds the video wanted in a release and makes it readable. It
// reads the first article of each file it needs to look at (real names, sizes,
// archive headers); the rest is read as it's played. A release that needs
// unpacking (compressed or locked archives), or doesn't clearly hold what's
// wanted (stream_pick.go), is refused, so the next release can be tried.
func OpenStreamFor(ctx context.Context, servers []ClientConfig, nzb *NZB, want StreamWant) (*StreamFile, error) {
	if len(servers) == 0 {
		return nil, errors.New("no Usenet server is set up")
	}
	files := streamableFiles(nzb)
	if len(files) == 0 {
		return nil, errors.New("the release has no files to play")
	}
	files, narrowed := narrowBySubject(files, want)
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
	var cands []videoCand
	var missing int64 // what the files whose first article is gone would hold
	for i, s := range streams {
		if errs[i] != nil {
			if errors.Is(errs[i], ErrArticleMissing) {
				for _, seg := range files[i].Segments {
					missing += seg.Bytes
				}
			}
			continue
		}
		opened = append(opened, s)
		// A video file as it is (by its name, or by what it is when the name is
		// scrambled).
		if isVideoName(s.name) || looksLikeVideo(s) {
			name := s.name
			if !isVideoName(name) {
				name += ".mkv" // a scrambled name: the player goes by what it is anyway
			}
			cands = append(cands, videoCand{
				name: name, subject: articleFilename(files[i].Subject, 0), size: s.size, head: s.firstBytes(),
				post: postPlace(files[i].Subject), order: i,
				file: &StreamFile{Name: name, Size: s.size, spans: []span{{src: s, n: s.size}}},
			})
		}
	}
	if len(opened) == 0 {
		return nil, fmt.Errorf("couldn't read the release: %w", errors.Join(errs...))
	}
	// RAR volumes: the videos inside, if they're stored (not compressed).
	inRAR, refused := rarStreams(opened)
	for _, f := range inRAR {
		cands = append(cands, videoCand{name: f.Name, size: f.Size, head: f.spans[0].src.firstBytesAt(f.spans[0].srcOff), order: len(files) + len(cands), file: f})
	}
	if len(cands) == 0 && refused != nil {
		return nil, refused
	}
	if len(cands) == 0 && missing > 0 {
		return nil, ErrArticleMissing
	}
	best, err := pickVideo(cands, want, missing, narrowed)
	if err != nil {
		return nil, err
	}
	return best.file.withGuard(), nil
}
