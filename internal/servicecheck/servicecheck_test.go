package servicecheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/store"
)

func TestStep(t *testing.T) {
	type step struct {
		ok   bool
		want Verdict
	}
	tests := []struct {
		name      string
		failAfter int
		steps     []step
		state     string
	}{
		{"answers: up, nothing to tell", 3, []step{{true, Same}, {true, Same}}, StateUp},
		{"two failures are a blip", 3, []step{{true, Same}, {false, Same}, {false, Same}, {true, Same}}, StateUp},
		{"three failures: down once", 3, []step{{false, Same}, {false, Same}, {false, WentDown}, {false, Same}, {false, Same}}, StateDown},
		{"down, then back once", 3, []step{{false, Same}, {false, Same}, {false, WentDown}, {true, CameBack}, {true, Same}}, StateUp},
		{"a good answer starts the count over", 3, []step{{false, Same}, {false, Same}, {true, Same}, {false, Same}, {false, Same}}, StateUp},
		{"one failure is enough when asked", 1, []step{{true, Same}, {false, WentDown}, {true, CameBack}}, StateUp},
		{"never answered and failing: down after three", 3, []step{{false, Same}, {false, Same}, {false, WentDown}}, StateDown},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := Check{State: StateUnknown, FailAfter: tc.failAfter}
			at := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
			for i, s := range tc.steps {
				var v Verdict
				c, v = Step(c, Result{At: at, OK: s.ok})
				if v != s.want {
					t.Fatalf("step %d: verdict %v, want %v", i, v, s.want)
				}
				at = at.Add(time.Minute)
			}
			if c.State != tc.state {
				t.Errorf("state %q, want %q", c.State, tc.state)
			}
		})
	}
}

func TestStepOutage(t *testing.T) {
	at := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	c := Check{State: StateUp, FailAfter: 3}
	for i := 0; i < 12; i++ {
		c, _ = Step(c, Result{At: at.Add(time.Duration(i) * time.Minute)})
	}
	back := at.Add(12 * time.Minute)
	if got := c.Outage(back); got != 12*time.Minute {
		t.Fatalf("outage %v, want 12m", got)
	}
	c, v := Step(c, Result{At: back, OK: true})
	if v != CameBack || !c.FailSince.IsZero() || c.Alerted || c.Fails != 0 {
		t.Fatalf("after coming back: %v %+v", v, c)
	}
}

func TestNormalize(t *testing.T) {
	c, err := Normalize(Check{Name: " Jellyfin ", URL: " http://host.docker.internal:8096/health "})
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "Jellyfin" || c.Expect != ExpectOK || c.Interval != time.Minute || c.FailAfter != 3 {
		t.Errorf("defaults not filled in: %+v", c)
	}
	bad := []Check{
		{URL: "http://x"},
		{Name: "x"},
		{Name: "x", URL: "ftp://x"},
		{Name: "x", URL: "jellyfin:8096"},
		{Name: "x", URL: "http://me:pw@x"},
		{Name: "x", URL: "http://x", Expect: "500"},
		{Name: "x", URL: "http://x", Interval: 5 * time.Second},
		{Name: "x", URL: "http://x", FailAfter: 99},
		{Name: "x\ny", URL: "http://x"},
	}
	for _, b := range bad {
		if _, err := Normalize(b); err == nil {
			t.Errorf("%+v was accepted", b)
		}
	}
}

func TestProbe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			_, _ = w.Write([]byte("Healthy"))
		case "/login":
			w.WriteHeader(http.StatusUnauthorized)
		case "/moved":
			http.Redirect(w, r, "/health", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	client := NewClient()
	tests := []struct {
		path, expect, keyword string
		ok                    bool
	}{
		{"/health", Expect200, "", true},
		{"/health", ExpectOK, "healthy", true},
		{"/health", ExpectOK, "Unhealthy", false},
		{"/moved", ExpectOK, "", true},
		{"/moved", Expect200, "", false}, // a redirect is not followed
		{"/login", ExpectOK, "", false},
		{"/login", ExpectAny, "", true},
		{"/nothing", ExpectOK, "", false},
	}
	for _, tc := range tests {
		r := Probe(context.Background(), client, Check{URL: srv.URL + tc.path, Expect: tc.expect, Keyword: tc.keyword}, nil)
		if r.OK != tc.ok {
			t.Errorf("%s expect %s keyword %q: ok %v (%s), want %v", tc.path, tc.expect, tc.keyword, r.OK, r.Error, tc.ok)
		}
		if !r.OK && r.Error == "" {
			t.Errorf("%s: a failure without a reason", tc.path)
		}
	}
	// Nothing listening: a plain reason, and never the address (it may hold a key).
	dead := httptest.NewServer(http.NotFoundHandler())
	addr := dead.URL + "/health?api_key=secret123"
	dead.Close()
	r := Probe(context.Background(), client, Check{URL: addr, Expect: ExpectOK}, nil)
	if r.OK || r.Code != 0 || r.Error == "" || strings.Contains(r.Error, "secret123") || strings.Contains(r.Error, "127.0.0.1") {
		t.Errorf("dead service: %+v", r)
	}
}

type fakeNotifier struct {
	mu     sync.Mutex
	events []string
}

func (f *fakeNotifier) Down(c Check, _ time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, "down:"+c.Name)
}

func (f *fakeNotifier) Back(c Check, outage time.Duration, _ time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, "back:"+c.Name+":"+outage.String())
}

func (f *fakeNotifier) list() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.events, ",")
}

func testRepo(t *testing.T) *Repo {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return NewRepo(db)
}

// The watchdog's rule end to end: a service that stops answering is told
// once after three checks, once when it is back, and a blip says nothing.
func TestRunnerDownAndBack(t *testing.T) {
	var up atomic.Bool
	up.Store(true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !up.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("Healthy"))
	}))
	defer srv.Close()

	ctx := context.Background()
	repo := testRepo(t)
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	c, err := Normalize(Check{Name: "Jellyfin", URL: srv.URL + "/health", Keyword: "healthy", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	c, err = repo.Create(ctx, c, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Create(ctx, Check{Name: "Paused", URL: "http://paused.invalid/", Expect: ExpectOK, Interval: time.Minute, FailAfter: 3}, now); err != nil {
		t.Fatal(err)
	}
	fake := &fakeNotifier{}
	r := &Runner{Repo: repo, Notify: fake, Now: func() time.Time { return now }}
	tick := func(ok bool) {
		up.Store(ok)
		r.RunDue(ctx)
		now = now.Add(time.Minute)
	}
	tick(true)
	tick(false) // a blip
	tick(false)
	tick(true)
	if got := fake.list(); got != "" {
		t.Fatalf("a blip sent %q", got)
	}
	for i := 0; i < 5; i++ {
		tick(false)
	}
	if got := fake.list(); got != "down:Jellyfin" {
		t.Fatalf("after the outage: %q", got)
	}
	got, _ := repo.Get(ctx, c.ID)
	if got.State != StateDown || got.Fails != 5 || got.LastCode != 503 || got.LastError == "" {
		t.Errorf("down state: %+v", got)
	}
	// Checked less than a minute ago: not due.
	now = now.Add(-30 * time.Second)
	r.RunDue(ctx)
	now = now.Add(30 * time.Second)
	tick(true)
	if got := fake.list(); got != "down:Jellyfin,back:Jellyfin:5m0s" {
		t.Fatalf("after coming back: %q", got)
	}
	changes, _ := repo.Changes(ctx, c.ID, 10)
	if len(changes) != 2 || changes[0].State != StateUp || changes[1].State != StateDown {
		t.Errorf("changes: %+v", changes)
	}
	recent, _ := repo.Recent(ctx, c.ID, 100)
	if len(recent) != 10 {
		t.Errorf("%d results, want 10", len(recent))
	}
	u, _ := repo.UptimeSince(ctx, c.ID, now.Add(-time.Hour))
	if u.Checks != 10 || u.Percent != 30 {
		t.Errorf("uptime %+v, want 3 of 10", u)
	}
	// The paused service was never asked.
	list, _ := repo.List(ctx)
	if !list[1].Checked.IsZero() {
		t.Error("a paused service was checked")
	}
}

func TestRepoUpdateAndDelete(t *testing.T) {
	ctx := context.Background()
	repo := testRepo(t)
	now := time.Now()
	c, err := repo.Create(ctx, Check{Name: "A", URL: "http://a/", Expect: ExpectOK, Interval: time.Minute, FailAfter: 3, Enabled: true}, now)
	if err != nil {
		t.Fatal(err)
	}
	c.State, c.Fails, c.Alerted, c.Checked = StateDown, 3, true, now
	if err := repo.Record(ctx, c, Result{At: now}, &Change{At: now, State: StateDown}); err != nil {
		t.Fatal(err)
	}
	c.Name = "B"
	got, err := repo.Update(ctx, c)
	if err != nil || got.Name != "B" || got.State != StateDown {
		t.Fatalf("rename: %+v %v", got, err)
	}
	c.URL = "http://b/"
	got, _ = repo.Update(ctx, c)
	if got.State != StateUnknown || got.Alerted || got.Fails != 0 {
		t.Errorf("a new address keeps the old state: %+v", got)
	}
	if err := repo.Delete(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Get(ctx, c.ID); err != ErrNotFound {
		t.Errorf("after delete: %v", err)
	}
	if err := repo.Delete(ctx, c.ID); err != ErrNotFound {
		t.Errorf("second delete: %v", err)
	}
}
