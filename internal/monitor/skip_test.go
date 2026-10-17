package monitor_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/monitor"
)

// A check that skips itself leaves the earlier result alone and announces
// nothing, whether the thing was working or failing.
func TestSkippedCheckKeepsTheEarlierResult(t *testing.T) {
	indexer := &fakeThing{kind: monitor.KindIndexer, id: 7, name: "NZBgeek"}
	indexer.set(errors.New("401 unauthorized"))
	things := []*fakeThing{indexer}
	rec := &recorder{}
	m := newMonitor(&things, rec, time.Now)

	m.RunOnce(context.Background())
	if evs := rec.take(); len(evs) != 1 || !evs[0].Failing {
		t.Fatalf("the first failure should be announced once, got %+v", evs)
	}

	indexer.set(monitor.ErrSkip)
	m.RunOnce(context.Background())
	if evs := rec.take(); len(evs) != 0 {
		t.Fatalf("a skipped check announced %+v", evs)
	}
	st := m.Statuses()
	if len(st) != 1 || st[0].OK || st[0].Error == "" {
		t.Fatalf("the earlier failing result should stay: %+v", st)
	}

	// a skipped check with nothing known yet leaves no result at all
	fresh := &fakeThing{kind: monitor.KindIndexer, id: 8, name: "Other"}
	fresh.set(monitor.ErrSkip)
	things = append(things, fresh)
	m.RunOnce(context.Background())
	if st := m.Statuses(); len(st) != 1 {
		t.Fatalf("a skipped, never-checked indexer should have no result yet: %+v", st)
	}
}
