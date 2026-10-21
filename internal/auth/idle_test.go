package auth_test

import (
	"errors"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
)

// fakeClock is a clock the test moves by hand.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time                { return c.t }
func (c *fakeClock) add(d time.Duration)           { c.t = c.t.Add(d) }
func newFakeClock() *fakeClock                     { return &fakeClock{t: time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)} }
func days(n int) time.Duration                     { return time.Duration(n) * 24 * time.Hour }
func idleOf(d *time.Duration) func() time.Duration { return func() time.Duration { return *d } }

// A step is a wait followed by a request with the session cookie.
type idleStep struct {
	wait time.Duration
	want error // nil: the session works
}

func TestSessionIdleTimeout(t *testing.T) {
	for _, tc := range []struct {
		name  string
		limit time.Duration
		steps []idleStep
	}{
		{"off: nothing changes however long it sits", 0, []idleStep{{days(3), nil}, {days(3), nil}}},
		{"used every day, limit two days", days(2), []idleStep{{days(1), nil}, {days(1), nil}, {days(1), nil}, {days(1), nil}}},
		{"unused for longer than the limit", days(2), []idleStep{{days(3), auth.ErrSessionIdle}}},
		{"exactly the limit is still fine", days(2), []idleStep{{days(2), nil}}},
		{"a minute over the limit ends it", days(2), []idleStep{{days(2) + time.Minute, auth.ErrSessionIdle}}},
		{"stays ended once found idle", days(2), []idleStep{{days(3), auth.ErrSessionIdle}, {time.Minute, auth.ErrSessionIdle}}},
		{"a use resets the clock", days(2), []idleStep{{days(2), nil}, {days(2), nil}, {days(2) + time.Hour, auth.ErrSessionIdle}}},
		{"the seven day sign-in still ends it", days(5), []idleStep{{days(4), nil}, {days(4), auth.ErrSessionNotFound}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := newTestService(t)
			clock := newFakeClock()
			svc.SetClockForTest(clock.now)
			limit := tc.limit
			svc.SetIdleTimeout(idleOf(&limit))
			user, err := svc.CreateUser("ryan", "correct-horse-battery")
			if err != nil {
				t.Fatal(err)
			}
			token, _, err := svc.CreateSession(user.ID)
			if err != nil {
				t.Fatal(err)
			}
			for i, st := range tc.steps {
				clock.add(st.wait)
				_, err := svc.UserForSession(token)
				if st.want == nil && err != nil || st.want != nil && !errors.Is(err, st.want) {
					t.Fatalf("step %d (after %v): got %v, want %v", i, st.wait, err, st.want)
				}
				// An idle session is still "not found" for code that only asks that.
				if st.want == auth.ErrSessionIdle && !errors.Is(err, auth.ErrSessionNotFound) {
					t.Fatalf("step %d: an idle session should also be ErrSessionNotFound", i)
				}
			}
		})
	}
}

// Turning the limit off again does not bring back a session that was already ended for being idle.
func TestIdleSessionStaysEnded(t *testing.T) {
	svc := newTestService(t)
	clock := newFakeClock()
	svc.SetClockForTest(clock.now)
	limit := days(1)
	svc.SetIdleTimeout(idleOf(&limit))
	user, _ := svc.CreateUser("ryan", "correct-horse-battery")
	token, _, _ := svc.CreateSession(user.ID)

	clock.add(days(2))
	if _, err := svc.UserForSession(token); !errors.Is(err, auth.ErrSessionIdle) {
		t.Fatalf("want idle, got %v", err)
	}
	limit = 0
	if _, err := svc.UserForSession(token); !errors.Is(err, auth.ErrSessionIdle) {
		t.Fatalf("a session found idle must not come back when the limit is switched off, got %v", err)
	}
	// It is no longer listed as a place the person is signed in.
	if list, _ := svc.SessionsOf(user.ID, ""); len(list) != 0 {
		t.Fatalf("an idle session is still listed: %+v", list)
	}
	// And it goes with the others once it would have expired anyway.
	clock.add(days(6))
	if n, err := svc.PurgeSessions(); err != nil || n != 1 {
		t.Fatalf("purge = %d, %v; want 1", n, err)
	}
}

// A session from before last-used times were kept counts from when it was made.
func TestIdleOldSessionsUseCreatedTime(t *testing.T) {
	svc := newTestService(t)
	user, _ := svc.CreateUser("ryan", "correct-horse-battery")
	token, _, _ := svc.CreateSession(user.ID)
	svc.ClearSessionUsedForTest(token)

	limit := days(1)
	svc.SetIdleTimeout(idleOf(&limit))
	// Created "now" (real clock): fine.
	if _, err := svc.UserForSession(token); err != nil {
		t.Fatalf("a fresh session without a last-used time should work: %v", err)
	}
	svc.ClearSessionUsedForTest(token)
	clock := newFakeClock()
	clock.t = time.Now().Add(days(2))
	svc.SetClockForTest(clock.now)
	if _, err := svc.UserForSession(token); !errors.Is(err, auth.ErrSessionIdle) {
		t.Fatalf("want idle by the creation time, got %v", err)
	}
}

// The last-used time is written at most once a minute.
func TestSessionLastUsedWrittenOncePerMinute(t *testing.T) {
	svc := newTestService(t)
	clock := newFakeClock()
	svc.SetClockForTest(clock.now)
	user, _ := svc.CreateUser("ryan", "correct-horse-battery")
	token, _, _ := svc.CreateSession(user.ID)
	first := svc.SessionLastUsedForTest(token)
	if first == "" {
		t.Fatal("a new session should have a last-used time")
	}
	steps := []struct {
		wait    time.Duration
		changed bool
	}{
		{10 * time.Second, false},
		{40 * time.Second, false}, // 50 s since the write
		{15 * time.Second, true},  // 65 s
		{5 * time.Second, false},
		{time.Minute, true},
	}
	last := first
	for i, st := range steps {
		clock.add(st.wait)
		if _, err := svc.UserForSession(token); err != nil {
			t.Fatal(err)
		}
		got := svc.SessionLastUsedForTest(token)
		if (got != last) != st.changed {
			t.Fatalf("step %d: written=%v, want %v", i, got != last, st.changed)
		}
		last = got
	}
}
