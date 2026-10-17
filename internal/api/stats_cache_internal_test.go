package api

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Several callers that arrive before the first statistics exist share one walk
// of the library instead of each starting their own.
func TestFirstStatsWalkIsSharedBetweenCallers(t *testing.T) {
	var c statsCache
	var walks atomic.Int32
	release := make(chan struct{})
	walk := func() (libraryStats, error) {
		walks.Add(1)
		<-release
		return libraryStats{movieBytes: 42, qualities: map[string]int{}}, nil
	}

	var wg sync.WaitGroup
	results := make([]int64, 5)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.mu.Lock()
			st, _ := c.firstWalk(walk)
			results[i] = st.movieBytes
		}()
	}
	time.Sleep(100 * time.Millisecond) // let them all queue up behind the first
	close(release)
	wg.Wait()

	if n := walks.Load(); n != 1 {
		t.Errorf("%d walks, want 1", n)
	}
	for i, r := range results {
		if r != 42 {
			t.Errorf("caller %d got %d", i, r)
		}
	}
	if c.stats.movieBytes != 42 {
		t.Error("the result was not kept")
	}
}
