package notify_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/notify"
)

// A target that says "slow down" is tried once more after the pause it asked
// for, instead of the message being dropped.
func TestSenderRetriesOnceAfterTooManyRequests(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
		}
	}))
	defer srv.Close()
	start := time.Now()
	if err := notify.NewWebhookSender(srv.URL).Send(context.Background(), fixtureEvent); err != nil {
		t.Fatalf("send: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2", calls.Load())
	}
	if time.Since(start) < 900*time.Millisecond {
		t.Fatalf("did not wait for Retry-After: %v", time.Since(start))
	}
}

// Targets are sent to together, so a slow one does not eat the time of the
// ones after it.
func TestNotifyAllSendsTogether(t *testing.T) {
	slow := func() *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(400 * time.Millisecond)
		}))
	}
	a, b, c := slow(), slow(), slow()
	defer a.Close()
	defer b.Close()
	defer c.Close()
	senders := []notify.Sender{notify.NewWebhookSender(a.URL), notify.NewWebhookSender(b.URL), notify.NewWebhookSender(c.URL)}
	start := time.Now()
	if errs := notify.NotifyAll(context.Background(), senders, fixtureEvent); len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}
	if took := time.Since(start); took > 900*time.Millisecond {
		t.Fatalf("three 0.4 s targets took %v, they were sent one after another", took)
	}
}
