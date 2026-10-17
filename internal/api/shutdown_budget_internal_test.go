package api

import (
	"testing"
	"time"
)

// A torrent engine that is stuck closing must not keep Shutdown from
// returning inside the time it was given.
func TestShutdownIsBoundedWhenTheTorrentEngineHangs(t *testing.T) {
	s := &Server{}
	s.torrents.mu.Lock() // stopAll waits for this
	defer s.torrents.mu.Unlock()

	begin := time.Now()
	s.Shutdown(600 * time.Millisecond)
	if took := time.Since(begin); took > 2*time.Second {
		t.Fatalf("Shutdown took %s with the torrent engine stuck, want about 0.6s", took)
	}
	if !s.pipelines.down {
		t.Fatal("the pipelines were not told to stop")
	}
}

func TestShutdownReturnsAtOnceWhenNothingRuns(t *testing.T) {
	s := &Server{}
	begin := time.Now()
	s.Shutdown(5 * time.Second)
	if took := time.Since(begin); took > time.Second {
		t.Fatalf("Shutdown took %s with nothing running", took)
	}
}
