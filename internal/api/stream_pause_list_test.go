package api_test

import (
	"sync"
	"testing"
)

// The list of downloads paused for a stream is changed by the start-up resume
// and by the stream-pause check at the same time; no id may be lost.
func TestPausedForStreamingListSurvivesConcurrentChanges(t *testing.T) {
	server, _, _ := newControlServer(t, false)

	// Ids 1-40 are held by many goroutines at once, some of them twice.
	var wg sync.WaitGroup
	for id := int64(1); id <= 40; id++ {
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				server.TestHoldForStreaming([]int64{id})
			}()
		}
	}
	// Ids 101-110 were held earlier and are given back while the rest is added.
	old := make([]int64, 0, 10)
	for id := int64(101); id <= 110; id++ {
		old = append(old, id)
	}
	server.TestHoldForStreaming(old)
	for _, id := range old {
		wg.Add(1)
		go func() {
			defer wg.Done()
			server.TestForgetStreamPaused([]int64{id})
		}()
	}
	wg.Wait()

	got := server.TestPausedForStreaming()
	seen := map[int64]int{}
	for _, id := range got {
		seen[id]++
	}
	for id := int64(1); id <= 40; id++ {
		if seen[id] != 1 {
			t.Errorf("id %d is on the list %d times, want once (list: %v)", id, seen[id], got)
		}
	}
	for id := int64(101); id <= 110; id++ {
		if seen[id] != 0 {
			t.Errorf("id %d was given back but is still listed (list: %v)", id, got)
		}
	}
}
