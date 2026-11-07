package download

// Usherr: a stream survives a few missing articles. When every server says an
// article is gone, the read gets zeros for its bytes, as long as the video can
// take it, so one bad article is a moment's glitch instead of an end to the
// film. Past a limit the read fails with ErrTooManyHoles and the player gets an
// error; only then is anyone asked whether the release is really gone.
//
// Why zeros, and when:
//   - Matroska, WebM and MPEG-TS are made of blocks a player can find again
//     after damage: a decoder shows a glitch and carries on.
//   - MP4 finds its samples through an index (moov). With the index before the
//     video, a hole in the video is the same glitch. With the index at the end,
//     or anything else unusual, nothing is filled.
//   - Other containers (AVI and whatever isn't recognised) go by exact offsets,
//     so nothing is filled there either: the read fails, as it always did.
//   - The start of the file (codecs, track list, the index) never gets zeros.
//
// The limits are nzbdav's (InfiniDysk, MIT): at most 2 articles in a row, 5 in
// all, and 1% of the file. Holes are remembered per opened file, so rclone
// asking again for the same range neither resets the count nor adds to it.
//
// "Couldn't ask" never counts: an article only counts as missing when every
// server has answered that it doesn't have it. A connection problem stays a
// failed read.
//
// Adapted from InfiniDysk backend/Streams/GapFillLimits.cs, PlaybackHoleTracker.cs
// and docs/configuration/repairs.md (MIT), rewritten for Go.

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// ErrTooManyHoles means a read reached more missing articles than the video can
// take. It is an ErrArticleMissing.
var ErrTooManyHoles = fmt.Errorf("too many missing articles to keep playing: %w", ErrArticleMissing)

const (
	holeMaxInARow = 2
	holeMaxTotal  = 5
	holeMaxShare  = 100 // one part in this many of the file
	// holeHeadBytes is the start of a video that never gets zeros.
	holeHeadBytes = 1 << 20
	// holeRemember is how long a missing article is taken as missing before
	// the servers are asked again.
	holeRemember = 5 * time.Minute
)

// streamHolesFilled counts the articles streams have filled with zeros.
var streamHolesFilled atomic.Int64

// StreamHolesFilled is how many missing articles streams have played past.
func StreamHolesFilled() int64 { return streamHolesFilled.Load() }

// holeSafeFrom says from which byte of a video a hole can be filled, by what
// the video's first bytes say it is; -1 when it can't be at all.
func holeSafeFrom(head []byte) int64 {
	switch {
	case len(head) >= 4 && head[0] == 0x1a && head[1] == 0x45 && head[2] == 0xdf && head[3] == 0xa3:
		return holeHeadBytes // Matroska, WebM
	case len(head) >= 377 && head[0] == 0x47 && head[188] == 0x47 && head[376] == 0x47:
		return holeHeadBytes // MPEG-TS
	case len(head) >= 8 && string(head[4:8]) == "ftyp":
		if end := mp4IndexEnd(head); end >= 0 {
			return max(end, holeHeadBytes)
		}
	}
	return -1
}

// mp4IndexEnd is where an MP4's index ends, when it stands before the video; -1
// when the index is at the end or can't be told from these bytes. A fragmented
// MP4 (moof boxes) carries its index along with the video, in pieces.
func mp4IndexEnd(head []byte) int64 {
	var pos int64
	for pos+8 <= int64(len(head)) {
		size := int64(binary.BigEndian.Uint32(head[pos:]))
		kind := string(head[pos+4 : pos+8])
		if size == 1 { // the size follows as 64 bits
			if pos+16 > int64(len(head)) {
				return -1
			}
			big := binary.BigEndian.Uint64(head[pos+8:])
			if big > 1<<62 {
				return -1
			}
			size = int64(big)
		}
		switch kind {
		case "moov":
			if size < 8 {
				return -1
			}
			return pos + size
		case "moof":
			return pos
		case "mdat":
			return -1
		}
		if size < 8 {
			return -1
		}
		pos += size
	}
	return -1
}

type holeKey struct {
	src *nzbStream
	idx int
}

type holeEntry struct {
	start, n int64 // where the article is in the video
	filled   bool  // refused ones are kept too, for ConfirmHoles
}

// holeGuard counts one opened video's missing articles.
type holeGuard struct {
	mu       sync.Mutex
	name     string
	size     int64
	safeFrom int64
	entries  map[holeKey]*holeEntry
}

func newHoleGuard(name string, size int64, head []byte) *holeGuard {
	return &holeGuard{name: name, size: size, safeFrom: holeSafeFrom(head), entries: map[holeKey]*holeEntry{}}
}

// overLimit says whether these holes are more than the video can take.
func (g *holeGuard) overLimit(list []*holeEntry) bool {
	if len(list) == 0 {
		return false
	}
	if g.safeFrom < 0 || len(list) > holeMaxTotal {
		return true
	}
	sorted := append([]*holeEntry(nil), list...)
	sort.Slice(sorted, func(a, b int) bool { return sorted[a].start < sorted[b].start })
	var bytes int64
	run := 0
	for i, e := range sorted {
		if e.start < g.safeFrom {
			return true
		}
		bytes += e.n
		// In a row: this one starts where the one before ends, give or take a
		// RAR volume's header.
		if i > 0 && e.start <= sorted[i-1].start+sorted[i-1].n+(64<<10) {
			run++
		} else {
			run = 1
		}
		if run > holeMaxInARow {
			return true
		}
	}
	return bytes > g.size/holeMaxShare
}

// allow says whether a missing article may read as zeros, and counts it the
// first time it's seen.
func (g *holeGuard) allow(src *nzbStream, idx int, start, n int64) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	k := holeKey{src, idx}
	if e, ok := g.entries[k]; ok {
		return e.filled
	}
	e := &holeEntry{start: start, n: n}
	list := []*holeEntry{e}
	for _, o := range g.entries {
		if o.filled {
			list = append(list, o)
		}
	}
	e.filled = !g.overLimit(list)
	g.entries[k] = e
	if e.filled {
		streamHolesFilled.Add(1)
		slog.Warn("stream: a missing article plays as zeros", "file", g.name, "at", start, "bytes", n, "holes", len(list))
	} else {
		slog.Warn("stream: too many missing articles to play past", "file", g.name, "at", start, "holes", len(list))
	}
	return e.filled
}

// HoleVerdict is what a second look at a video's missing articles found.
type HoleVerdict int

const (
	// HolesUnknown: nothing's over the limit, or a server couldn't be asked.
	// Either way nothing should change.
	HolesUnknown HoleVerdict = iota
	// HolesBearable: every server answered, and what is really missing is
	// within what the video can take. The rest can be read again.
	HolesBearable
	// HolesGone: every server answered, and more is missing than the video can
	// take. This release won't play.
	HolesGone
)

// ConfirmHoles asks every server again (STAT) about the articles a read found
// missing. Only an answer from all of them counts.
func (f *StreamFile) ConfirmHoles(ctx context.Context) HoleVerdict {
	g := f.guard
	if g == nil {
		return HolesUnknown
	}
	g.mu.Lock()
	keys := make([]holeKey, 0, len(g.entries))
	for k := range g.entries {
		keys = append(keys, k)
	}
	g.mu.Unlock()
	if len(keys) == 0 {
		return HolesUnknown
	}
	found := map[holeKey]bool{}
	for _, k := range keys {
		has, err := articleOnAnyServer(ctx, k.src.servers, k.src.file.Segments[k.idx].MessageID)
		if err != nil {
			slog.Info("stream: couldn't ask about missing articles", "file", g.name, "err", err)
			return HolesUnknown
		}
		found[k] = has
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	var still []*holeEntry
	for k, e := range g.entries {
		if found[k] {
			delete(g.entries, k) // it's back: the next read fetches it
			k.src.forgetHole(k.idx)
			continue
		}
		still = append(still, e)
	}
	if g.overLimit(still) {
		return HolesGone
	}
	for _, e := range still {
		e.filled = true
	}
	return HolesBearable
}

// articleOnAnyServer asks the servers in order whether they have an article. An
// error means a server couldn't be asked and none before it had the article.
func articleOnAnyServer(ctx context.Context, servers []ClientConfig, messageID string) (bool, error) {
	var askErr error
	for _, cfg := range servers {
		has, err := statOn(ctx, cfg, messageID)
		if err != nil {
			askErr = errors.Join(askErr, err)
			continue
		}
		if has {
			return true, nil
		}
	}
	return false, askErr
}

func statOn(ctx context.Context, cfg ClientConfig, messageID string) (bool, error) {
	pc, err := pool.get(ctx, cfg)
	if err != nil {
		return false, err
	}
	stop := context.AfterFunc(ctx, func() { pc.c.Close() })
	has, err := pc.c.Stat(messageID)
	stop()
	var ne *NNTPError
	if err != nil && !errors.As(err, &ne) {
		pc.close()
		return false, err
	}
	pool.put(pc)
	return has, err
}
