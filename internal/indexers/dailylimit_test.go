package indexers_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/indexers"
)

// The two answers free indexers give once the day's requests are used up.
const (
	drunkenSlugLimit = `<?xml version="1.0" encoding="UTF-8"?><error code="429" description="Request limit reached (26/25)"/>`
	althubLimit      = `<?xml version="1.0" encoding="UTF-8"?><error code="500" description="Request limit reached. Retry in 894 minutes."/>`
)

func TestParseDailyLimit(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	midnight := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		msg   string
		ok    bool
		used  int
		limit int
		back  time.Time
	}{
		{"used over limit", "newznab error from Slug: Request limit reached (26/25) (code 429)", true, 26, 25, midnight},
		{"retry in minutes", "newznab error from Althub: Request limit reached. Retry in 894 minutes. (code 500)", true, 0, 0, now.Add(894 * time.Minute)},
		{"in an http error body", `unexpected status 429: <error code="429" description="Request limit reached (25/25)"/>`, true, 25, 25, midnight},
		{"retry in hours", "Request limit reached. Retry in 2 hours.", true, 0, 0, now.Add(2 * time.Hour)},
		{"no numbers at all", "Request limit reached", true, 0, 0, midnight},
		{"a daily limit wording", "You have hit the daily limit", true, 0, 0, midnight},
		{"plain too many requests is about speed", "unexpected status 429: Too Many Requests", false, 0, 0, time.Time{}},
		{"a bad key", "newznab error from X: Incorrect user credentials (code 100)", false, 0, 0, time.Time{}},
		{"empty", "", false, 0, 0, time.Time{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dl, ok := indexers.ParseDailyLimit(tc.msg, now)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if !ok {
				return
			}
			if dl.Used != tc.used || dl.Limit != tc.limit || !dl.BackAt.Equal(tc.back) {
				t.Fatalf("got %+v, want used %d limit %d back %v", dl, tc.used, tc.limit, tc.back)
			}
		})
	}
}

func TestLearnQuotaAndMidnightReset(t *testing.T) {
	clock := newClock() // 2026-10-09 12:00 UTC
	tr, list, again := trackerWithIndexers(t, clock, "Slug")
	id := list[0].ID

	if q := tr.Quota(id, 0); q.Limit != 0 || q.Blocked {
		t.Fatalf("an indexer that never said anything has no limit, got %+v", q)
	}
	if !tr.LearnLimit(id, "newznab error from Slug: Request limit reached (26/25) (code 429)") {
		t.Fatal("the answer was not taken for a daily limit")
	}
	q := tr.Quota(id, 0)
	if q.Limit != 25 || !q.Learned || !q.Reached || !q.Blocked || q.Used != 26 {
		t.Fatalf("after the answer: %+v", q)
	}
	want := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	if !q.BackAt.Equal(want) {
		t.Fatalf("back at %v, want %v", q.BackAt, want)
	}
	if got, w := q.Message(time.UTC), "Daily limit of 25 reached. It is used again after midnight (UTC)."; got != w {
		t.Fatalf("message = %q, want %q", got, w)
	}

	// the limit is remembered across a restart
	if q := again().Quota(id, 0); q.Limit != 25 || !q.Blocked {
		t.Fatalf("after a restart: %+v", q)
	}

	// midnight: a new day, the count starts again, the limit stays known
	clock.advance(12*time.Hour + time.Minute)
	if q := tr.Quota(id, 0); q.Used != 0 || q.Blocked || q.Reached || q.Limit != 25 {
		t.Fatalf("after midnight: %+v", q)
	}
}

func TestQuotaMarginAndSetLimit(t *testing.T) {
	clock := newClock()
	tr, list, _ := trackerWithIndexers(t, clock, "Alpha")
	id := list[0].ID

	tr.CountRequest(id, 22)
	if q := tr.Quota(id, 25); q.Blocked || q.Reached {
		t.Fatalf("22 of 25 leaves room, got %+v", q)
	}
	tr.CountRequest(id, 1)
	if q := tr.Quota(id, 25); !q.Blocked || q.Reached {
		t.Fatalf("23 of 25: automation stops two before the limit, a manual search still fits, got %+v", q)
	}
	tr.CountRequest(id, 2)
	if q := tr.Quota(id, 25); !q.Reached {
		t.Fatalf("25 of 25 is reached, got %+v", q)
	}
	// a set limit wins over what the indexer reported
	tr.LearnLimit(id, "Request limit reached (30/10)")
	if q := tr.Quota(id, 100); q.Limit != 100 || q.Learned {
		t.Fatalf("a set limit should win, got %+v", q)
	}
	if q := tr.Quota(id, 0); q.Limit != 10 || !q.Learned {
		t.Fatalf("without a set limit the learned one is used, got %+v", q)
	}
	// a request told as "retry in N minutes" keeps it out for that long
	tr2, list2, _ := trackerWithIndexers(t, clock, "Beta")
	tr2.LearnLimit(list2[0].ID, "Request limit reached. Retry in 894 minutes.")
	q := tr2.Quota(list2[0].ID, 0)
	if !q.Blocked || !q.BackAt.Equal(clock.t.Add(894*time.Minute)) {
		t.Fatalf("retry-in answer: %+v", q)
	}
	clock.advance(895 * time.Minute)
	if q := tr2.Quota(list2[0].ID, 0); q.Blocked {
		t.Fatalf("it should be usable again after the wait, got %+v", q)
	}
}

// limitServer answers every request with body and counts the requests.
func limitServer(t *testing.T, status int, body string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func TestSearchAllLearnsLimitWithoutAFailureAndSkips(t *testing.T) {
	restore := indexers.SetSearchGaps(0, 0)
	defer restore()
	tests := []struct {
		name   string
		status int
		body   string
		limit  int
	}{
		{"Drunken Slug", http.StatusOK, drunkenSlugLimit, 25},
		{"Althub", http.StatusOK, althubLimit, 0},
		{"as an http error", http.StatusTooManyRequests, drunkenSlugLimit, 25},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, hits := limitServer(t, tc.status, tc.body)
			clock := newClock()
			tr, list, _ := trackerWithIndexers(t, clock, "Alpha")
			list[0].BaseURL = srv.URL

			o := indexers.SearchAll(context.Background(), list, "matrix", nil)[0]
			if o.Err == nil {
				t.Fatal("the search that hit the limit should say so")
			}
			if hits.Load() != 1 {
				t.Fatalf("hits = %d, want 1", hits.Load())
			}
			// not a failure, not paused
			if _, ok := tr.Health(list[0].ID); ok {
				t.Fatal("a daily limit was counted as a failure")
			}
			if _, paused := tr.PausedUntil(list[0].ID); paused {
				t.Fatal("a daily limit paused the indexer for hours")
			}
			if q := tr.Quota(list[0].ID, 0); q.Limit != tc.limit || !q.Blocked {
				t.Fatalf("quota = %+v, want limit %d and blocked", q, tc.limit)
			}

			// the next automatic search does not ask, and is no error
			o = indexers.SearchAll(context.Background(), list, "matrix", nil)[0]
			if !o.Skipped || o.Err != nil || o.LimitNote == "" || o.PausedUntil.IsZero() {
				t.Fatalf("expected a skipped outcome with a note, got %+v", o)
			}
			if !strings.Contains(o.LimitNote, "Daily limit") {
				t.Fatalf("the note should say it is the daily limit: %q", o.LimitNote)
			}
			if hits.Load() != 1 {
				t.Fatalf("a limit-reached indexer was asked again: hits = %d", hits.Load())
			}
			stats, err := tr.Stats(clock.t.AddDate(0, 0, -1))
			if err != nil || len(stats) != 1 || stats[0].Failures != 0 {
				t.Fatalf("stats = %+v, %v: the limit should not count as a failure", stats, err)
			}

			// a search a person started is still tried
			o = indexers.SearchAll(indexers.AsManual(context.Background()), list, "matrix", nil)[0]
			if o.Skipped {
				t.Fatalf("a manual search should still ask: %+v", o)
			}
			if hits.Load() != 2 {
				t.Fatalf("hits = %d, want 2 after the manual search", hits.Load())
			}
			if o.LimitNote == "" {
				t.Fatal("a manual search past the limit should carry the warning")
			}

			// the next day it is asked again
			clock.advance(24 * time.Hour)
			list[0].BaseURL = srv.URL
			o = indexers.SearchAll(context.Background(), list, "matrix", nil)[0]
			if o.Skipped && tc.limit == 25 {
				t.Fatalf("after midnight the indexer should be asked again: %+v", o)
			}
		})
	}
}

func TestSearchAllSkipsAtTheSetLimitAndCountsRequests(t *testing.T) {
	restore := indexers.SetSearchGaps(0, 0)
	defer restore()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(fixtureRSS)) }))
	defer srv.Close()
	clock := newClock()
	tr, list, _ := trackerWithIndexers(t, clock, "Alpha")
	list[0].BaseURL = srv.URL
	list[0].RequestsPerDay = 8 // automation stops at 6

	asked := 0
	for i := 0; i < 10; i++ {
		o := indexers.SearchAll(context.Background(), list, "matrix", nil)[0]
		if o.Err != nil {
			t.Fatalf("search %d: %v", i, o.Err)
		}
		if !o.Skipped {
			asked++
		}
	}
	if asked != 6 {
		t.Fatalf("asked %d times, want 6 (the limit of 8 less the margin of 2)", asked)
	}
	if q := tr.Quota(list[0].ID, 8); q.Used != 6 {
		t.Fatalf("used = %d, want 6", q.Used)
	}
	// every call counts: a download of a release found by the indexer too
	tr.CountRequest(list[0].ID, 1)
	if q := tr.Quota(list[0].ID, 8); q.Used != 7 {
		t.Fatalf("used = %d, want 7", q.Used)
	}
}

func TestProbeAdvice(t *testing.T) {
	restore := indexers.SetSearchGaps(0, 0)
	defer restore()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(fixtureRSS)) }))
	defer srv.Close()
	clock := newClock()
	tr, list, _ := trackerWithIndexers(t, clock, "Alpha")
	list[0].BaseURL = srv.URL
	id := list[0].ID

	if got := tr.ProbeAdvice(id, 0); got != indexers.ProbeNow {
		t.Fatalf("a new indexer is checked once, got %v", got)
	}
	tr.NoteProbe(id)
	clock.advance(30 * time.Minute)
	if got := tr.ProbeAdvice(id, 0); got != indexers.ProbeSkipWait {
		t.Fatalf("a second check within six hours waits, got %v", got)
	}
	// a search that worked is as good as a check
	clock.advance(time.Minute)
	if o := indexers.SearchAll(context.Background(), list, "matrix", nil)[0]; o.Err != nil {
		t.Fatal(o.Err)
	}
	if got := tr.ProbeAdvice(id, 0); got != indexers.ProbeSkipWorks {
		t.Fatalf("after a search that worked, got %v", got)
	}
	clock.advance(7 * time.Hour)
	if got := tr.ProbeAdvice(id, 0); got != indexers.ProbeNow {
		t.Fatalf("after six hours without news it is checked, got %v", got)
	}
	// an indexer with its day used up is not checked
	tr.LearnLimit(id, "Request limit reached (26/25)")
	if got := tr.ProbeAdvice(id, 0); got != indexers.ProbeSkipWait {
		t.Fatalf("a limit-reached indexer is not checked, got %v", got)
	}
}

func TestCapsAnswers(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{"working", http.StatusOK, `<?xml version="1.0"?><caps><server title="x"/></caps>`, ""},
		{"bad key", http.StatusOK, `<?xml version="1.0"?><error code="100" description="Incorrect user credentials"/>`, "Incorrect user credentials"},
		{"daily limit", http.StatusOK, drunkenSlugLimit, "Request limit reached"},
		{"web page", http.StatusOK, `<!DOCTYPE html><html><body>hi</body></html>`, "web page"},
		{"server error", http.StatusInternalServerError, "oops", "500"},
		{"not an indexer", http.StatusOK, `<foo/>`, "capabilities"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := limitServer(t, tc.status, tc.body)
			err := indexers.NewNewznabClient("X", srv.URL, "key").Caps(context.Background())
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
				t.Fatalf("error = %v, want one with %q", err, tc.wantErr)
			}
		})
	}
	// the same limit answer is taken for one
	if err := errors.New(drunkenSlugLimit); !indexers.IsDailyLimit(err) {
		t.Fatal("IsDailyLimit should know the answer")
	}
}
