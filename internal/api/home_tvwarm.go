package api

// Usherr: Live TV tuning that doesn't make anyone wait. Finding a streamed
// episode's release (a search, an NZB, a check, a first read) can take 20
// seconds or more when nothing is remembered, which is the case after every
// restart. Two things keep that from showing:
//   - a channel that's tuned asks for its stream with a short wait; when the
//     lookup takes longer, the page is told to ask again in a few seconds
//     ("tuning in…") while one lookup, shared by everyone asking, carries on;
//   - every few hours (and a minute and a half after start-up) the programs
//     that are on now, or start soon, on every channel are looked up in
//     advance, a few at a time, so a channel is ready when it is switched to.

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rdborg/mediarium/internal/tv"
)

const (
	tvTuneWait   = 12 * time.Second // how long a tune waits for the lookup before saying "again in a moment"
	tvWarmAhead  = 30 * time.Minute // programs starting within this are looked up too
	tvWarmAtOnce = 3
)

type tvFlight struct {
	done chan struct{}
	src  tvStreamSrc
	err  error
}

var tvFlights sync.Map // stream path -> *tvFlight

// tvLookupForTest stands in for the lookup in tests.
var tvLookupForTest func(ctx context.Context, path string) (tvStreamSrc, error)

// tvStreamSourceWithin is tvStreamSource that waits at most wait. When the
// lookup takes longer it carries on in the background (one lookup for all who
// ask) and pending is true; asking again then finds its answer remembered.
func (s *Server) tvStreamSourceWithin(ctx context.Context, path string, wait time.Duration) (src tvStreamSrc, pending bool, err error) {
	f := &tvFlight{done: make(chan struct{})}
	if v, loaded := tvFlights.LoadOrStore(path, f); loaded {
		f = v.(*tvFlight)
	} else {
		look := s.tvStreamSource
		if tvLookupForTest != nil {
			look = tvLookupForTest
		}
		go func() {
			f.src, f.err = look(context.Background(), path)
			tvFlights.Delete(path)
			close(f.done)
		}()
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-f.done:
		return f.src, false, f.err
	case <-timer.C:
		return tvStreamSrc{}, true, nil
	case <-ctx.Done():
		return tvStreamSrc{}, true, nil
	}
}

// tvWarmJob looks up the streams of what's on now, and starting soon, on every
// enabled channel.
func (s *Server) tvWarmJob(ctx context.Context) {
	repo := s.tvRepo()
	chans, err := repo.Channels()
	if err != nil {
		return
	}
	now := time.Now()
	seen := map[string]bool{}
	var paths []string
	add := func(p tv.Program) {
		if p.Kind == "stream" && p.FilePath != "" && !seen[p.FilePath] {
			seen[p.FilePath] = true
			paths = append(paths, p.FilePath)
		}
	}
	for _, c := range chans {
		if !c.Enabled {
			continue
		}
		p, ok, _ := repo.At(c.ID, now)
		if !ok {
			continue
		}
		add(p)
		if n, ok, _ := repo.At(c.ID, p.End.Add(time.Millisecond)); ok && n.Start.Sub(now) < tvWarmAhead {
			add(n)
		}
	}
	var wg sync.WaitGroup
	var looked atomic.Int32
	sem := make(chan struct{}, tvWarmAtOnce)
	for _, path := range paths {
		if ctx.Err() != nil {
			break
		}
		if v, ok := tvStreamSrcs.Load(path); ok && time.Since(v.(tvStreamSrc).at) < 2*time.Hour {
			continue // already known
		}
		if v, ok := tvMissing.Load(path); ok && time.Since(v.(time.Time)) < 5*time.Minute {
			continue
		}
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer func() { <-sem; wg.Done() }()
			tvWarming.Store(path, time.Now())
			s.tvPrewarm(path)
			looked.Add(1)
		}()
	}
	wg.Wait()
	if len(paths) > 0 {
		slog.Info("tv: streamed programs looked up in advance", "programs", len(paths), "looked up", looked.Load())
	}
}
