package api

import (
	"testing"
	"time"
)

// A scan can be stopped by hand, and stopping it ends its context so the
// lookups in flight give up. A stopped scan doesn't block a new one.
func TestMusicScanCanBeStopped(t *testing.T) {
	var m musicScans
	job, created := m.start("/music")
	if !created || !job.running() {
		t.Fatal("first scan should start")
	}
	if again, created := m.start("/music"); created || again != job {
		t.Fatal("a running scan is returned instead of starting another")
	}
	if !job.stop(musicScanStopped, "") {
		t.Fatal("stop should report the scan was running")
	}
	if job.ctx.Err() == nil {
		t.Fatal("stopping cancels the scan's context")
	}
	job.setPhase(musicScanDone, -1)
	if p := job.snapshot().Phase; p != musicScanStopped {
		t.Fatalf("a stopped scan stays stopped, got %q", p)
	}
	if job.stop(musicScanStopped, "") {
		t.Fatal("stopping twice reports false")
	}
	next, created := m.start("/music")
	if !created || next == job {
		t.Fatal("a new scan starts once the old one is stopped")
	}
	if m.latest() != next {
		t.Fatal("latest is the newest scan")
	}
}

// A scan that hasn't moved for a long time is replaced, so a stuck one never
// blocks new scans for good.
func TestStuckMusicScanIsReplaced(t *testing.T) {
	var m musicScans
	stuck, _ := m.start("/music")
	stuck.mu.Lock()
	stuck.moved = time.Now().Add(-musicScanStale - time.Minute)
	stuck.mu.Unlock()

	next, created := m.start("/music")
	if !created || next == stuck {
		t.Fatal("a stuck scan should be replaced")
	}
	if p := stuck.snapshot(); p.Phase != musicScanFailed || p.Error == "" || stuck.ctx.Err() == nil {
		t.Fatalf("the stuck scan is ended with a reason: %+v", p)
	}
}
