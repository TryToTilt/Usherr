package indexers_test

import (
	"errors"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/indexers"
)

func TestParseDailyLimitBounds(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	midnight := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	dl, ok := indexers.ParseDailyLimit("Request limit reached (99999999999999999999/99999999999999999999)", now)
	if !ok || dl.Used != 0 || dl.Limit != 0 {
		t.Fatalf("huge numbers should be left out, got %+v", dl)
	}
	dl, _ = indexers.ParseDailyLimit("Request limit reached (26/250000)", now)
	if dl.Limit != 0 {
		t.Fatalf("a limit above 100000 is nonsense, got %+v", dl)
	}
	dl, _ = indexers.ParseDailyLimit("Request limit reached. Retry in 99999 hours.", now)
	if !dl.BackAt.Equal(now.Add(48 * time.Hour)) {
		t.Fatalf("a very long wait is cut to 48 hours, got %v", dl.BackAt)
	}
	dl, _ = indexers.ParseDailyLimit("Request limit reached. Retry in 99999999999999999999 minutes.", now)
	if !dl.BackAt.Equal(midnight) {
		t.Fatalf("a wait that does not fit falls back to midnight, got %v", dl.BackAt)
	}
}

func TestHugeLimitAnswerDoesNotRunTheCountAway(t *testing.T) {
	clock := newClock()
	tr, list, again := trackerWithIndexers(t, clock, "Slug")
	id := list[0].ID
	tr.LearnLimit(id, "Request limit reached (99999999999999999999/99999999999999999999)")
	tr.CountRequest(id, 1)
	if q := tr.Quota(id, 0); q.Used < 0 || q.Used > 100 {
		t.Fatalf("used = %d, want a small count", q.Used)
	}
	if q := again().Quota(id, 0); q.Used < 0 || q.Used > 100 || q.Limit != 0 {
		t.Fatalf("after a restart: %+v", q)
	}
}

func TestLearnedLimitIsDroppedWhenTheIndexerWorksAgain(t *testing.T) {
	clock := newClock()
	tr, list, again := trackerWithIndexers(t, clock, "Slug")
	id := list[0].ID
	tr.LearnLimit(id, "Request limit reached (26/25)")
	// the plan was upgraded: a search by hand works, past the old limit
	tr.CountRequest(id, 1)
	tr.RecordSearch(id, nil, nil)
	if q := tr.Quota(id, 0); q.Limit != 0 || q.Blocked {
		t.Fatalf("a search that worked past the limit should drop it, got %+v", q)
	}
	if q := again().Quota(id, 0); q.Limit != 0 || q.Blocked {
		t.Fatalf("the dropped limit should stay dropped after a restart, got %+v", q)
	}
	// a set limit is not touched by any of it
	tr.CountRequest(id, 30)
	if q := tr.Quota(id, 20); q.Limit != 20 || !q.Reached {
		t.Fatalf("a set limit stays, got %+v", q)
	}
}

func TestLearnedLimitExpires(t *testing.T) {
	clock := newClock()
	tr, list, again := trackerWithIndexers(t, clock, "Slug")
	id := list[0].ID
	tr.LearnLimit(id, "Request limit reached (26/25)")
	clock.advance(6 * 24 * time.Hour)
	if q := tr.Quota(id, 0); q.Limit != 25 {
		t.Fatalf("after six days the limit is still believed, got %+v", q)
	}
	clock.advance(2 * 24 * time.Hour)
	if q := tr.Quota(id, 0); q.Limit != 0 || q.Blocked {
		t.Fatalf("after a week without news the limit is forgotten, got %+v", q)
	}
	if q := again().Quota(id, 0); q.Limit != 0 {
		t.Fatalf("after a restart: %+v", q)
	}
	// hearing it again keeps it
	tr.LearnLimit(id, "Request limit reached (26/25)")
	clock.advance(5 * 24 * time.Hour)
	tr.LearnLimit(id, "Request limit reached (26/25)")
	clock.advance(5 * 24 * time.Hour)
	if q := tr.Quota(id, 0); q.Limit != 25 {
		t.Fatalf("a limit heard again within the week stays, got %+v", q)
	}
}

func TestTinyLearnedLimitNeedsToBeHeardTwice(t *testing.T) {
	clock := newClock()
	tr, list, _ := trackerWithIndexers(t, clock, "Slug")
	id := list[0].ID
	tr.LearnLimit(id, "Request limit reached (1/1)")
	q := tr.Quota(id, 0)
	if q.Limit != 0 || q.Learned {
		t.Fatalf("a limit of 1 heard once is not believed, got %+v", q)
	}
	if !q.Blocked || !q.BackAt.Equal(time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("the answer still holds the indexer back until midnight, got %+v", q)
	}
	tr.LearnLimit(id, "Request limit reached (1/1)")
	if q := tr.Quota(id, 0); q.Limit != 1 || !q.Learned {
		t.Fatalf("heard twice it is believed, got %+v", q)
	}
	// a limit of 5 or more is taken at once
	tr2, list2, _ := trackerWithIndexers(t, clock, "Beta")
	tr2.LearnLimit(list2[0].ID, "Request limit reached (6/5)")
	if q := tr2.Quota(list2[0].ID, 0); q.Limit != 5 {
		t.Fatalf("a limit of 5 is believed at once, got %+v", q)
	}
}

func TestProbeAdviceSeesRecentFailuresAndStaleLimits(t *testing.T) {
	clock := newClock()
	tr, list, _ := trackerWithIndexers(t, clock, "Alpha")
	id := list[0].ID

	tr.NoteProbe(id)
	clock.advance(time.Minute)
	tr.RecordSearch(id, nil, nil)
	if got := tr.ProbeAdvice(id, 0); got != indexers.ProbeSkipWorks {
		t.Fatalf("after a search that worked, got %v", got)
	}
	// the next real search fails (key revoked): the old success no longer counts
	clock.advance(time.Minute)
	tr.RecordSearch(id, nil, errors.New("newznab error from Alpha: Incorrect user credentials (code 100)"))
	if got := tr.ProbeAdvice(id, 0); got == indexers.ProbeSkipWorks {
		t.Fatal("a failure after the last success must not be hidden")
	}
	// a success after the failure counts again
	clock.advance(time.Minute)
	tr.RecordSearch(id, nil, nil)
	if got := tr.ProbeAdvice(id, 0); got != indexers.ProbeSkipWorks {
		t.Fatalf("after a new success, got %v", got)
	}

	// a learned limit that the indexer no longer holds against us does not keep the check away
	tr2, list2, _ := trackerWithIndexers(t, clock, "Beta")
	id2 := list2[0].ID
	tr2.LearnLimit(id2, "Request limit reached (26/25)")
	tr2.Resume(id2) // the wait the indexer told is over
	if got := tr2.ProbeAdvice(id2, 0); got != indexers.ProbeNow {
		t.Fatalf("a learned limit alone must not hide a broken indexer, got %v", got)
	}
	// but a limit a person set does
	if got := tr2.ProbeAdvice(id2, 20); got != indexers.ProbeSkipWait {
		t.Fatalf("a set limit that is used up holds the check back, got %v", got)
	}
}
