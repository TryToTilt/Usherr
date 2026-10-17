package api

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/library/librarytest"
)

// Callers that arrive together share one piece of work, and keep its answer
// until the library changes.
func TestSharedCacheSharesTheWork(t *testing.T) {
	var c sharedCache[int]
	var runs atomic.Int32
	release := make(chan struct{})
	compute := func(ctx context.Context) (int, error) {
		runs.Add(1)
		<-release
		return 42, nil
	}
	var wg sync.WaitGroup
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if v, err := c.get(context.Background(), time.Minute, compute); v != 42 || err != nil {
				t.Errorf("got %d, %v", v, err)
			}
		}()
	}
	time.Sleep(50 * time.Millisecond) // let them all arrive
	close(release)
	wg.Wait()
	if n := runs.Load(); n != 1 {
		t.Fatalf("worked out %d times for six callers at once, want once", n)
	}
	if v, _ := c.get(context.Background(), time.Minute, compute); v != 42 || runs.Load() != 1 {
		t.Fatalf("a fresh answer was not kept")
	}
	if _, err := c.get(context.Background(), 0, compute); err != nil || runs.Load() != 2 {
		t.Fatalf("an answer older than the time limit was used again (runs %d)", runs.Load())
	}
}

// When the caller doing the work gives up, the work stops with it, and the
// callers still waiting do it themselves instead of all failing.
func TestSharedCacheCallerGivesUp(t *testing.T) {
	var c sharedCache[int]
	started := make(chan struct{})
	first, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() {
		_, err := c.get(first, time.Minute, func(ctx context.Context) (int, error) {
			close(started)
			<-ctx.Done() // a query that stops when its request is cancelled
			return 0, ctx.Err()
		})
		done <- err
	}()
	<-started
	second := make(chan int)
	go func() {
		v, err := c.get(context.Background(), time.Minute, func(ctx context.Context) (int, error) { return 7, nil })
		if err != nil {
			t.Error(err)
		}
		second <- v
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("the caller that gave up got %v", err)
	}
	if v := <-second; v != 7 {
		t.Fatalf("the caller still waiting got %d", v)
	}
}

// The Dashboard's totals come from the kept overview, follow a change to the
// library at once, and a request that has given up stops its queries.
func TestLibraryOverviewFollowsTheLibrary(t *testing.T) {
	s := newAuditServer(t)
	if err := librarytest.Fill(s.db, librarytest.Size{Movies: 30, Shows: 4, EpisodesPerShow: 10, Monitored: true, DownloadedEvery: 10}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	o, err := s.libraryOverview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if o.Movies.Total != 30 || o.WantedMovies != 27 || o.Series.Series != 4 || o.WantedEpisodes != 36 {
		t.Fatalf("overview = %+v", o)
	}
	m, _, err := s.MovieRepo.GetByTMDBID(1)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MovieRepo.SetMonitored(m.ID, false); err != nil {
		t.Fatal(err)
	}
	if o, _ = s.libraryOverview(ctx); o.WantedMovies != 26 {
		t.Fatalf("after a change the overview still says %d wanted movies", o.WantedMovies)
	}

	s.overviewCache.forget()
	gone, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.libraryOverview(gone); !errors.Is(err, context.Canceled) {
		t.Fatalf("a request that gave up still got its answer worked out: %v", err)
	}
	if _, err := s.MovieRepo.MovieCounts(gone); !errors.Is(err, context.Canceled) {
		t.Fatalf("a count ran for a request that gave up: %v", err)
	}
}
