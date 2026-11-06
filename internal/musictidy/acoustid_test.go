package musictidy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func fakeAcoustID(t *testing.T, h http.HandlerFunc) *AcoustID {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &AcoustID{
		Key:     func() string { return "app-key" },
		BaseURL: srv.URL,
		Sleep:   func(ctx context.Context, d time.Duration) error { return ctx.Err() },
	}
}

const goodAnswer = `{"status":"ok","results":[
	{"id":"a","score":%s,"recordings":[{"id":"rec-1","title":"Song","releasegroups":[{"id":"rg-1","title":"Album","type":"Album"}]}]},
	{"id":"b","score":0.99,"recordings":[{"id":"rec-other"}]}]}`

func TestLookupSendsTheFormAndTakesTheBestResult(t *testing.T) {
	c := fakeAcoustID(t, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Method != http.MethodPost || r.Form.Get("client") != "app-key" || r.Form.Get("duration") != "241" ||
			r.Form.Get("fingerprint") != "FP" || r.Form.Get("meta") != "recordings releasegroups" {
			t.Errorf("request: %s %v", r.Method, r.Form)
		}
		fmt.Fprintf(w, goodAnswer, "0.91")
	})
	m, ok, err := c.Lookup(context.Background(), "FP", 241)
	if err != nil || !ok {
		t.Fatalf("lookup: ok=%v err=%v", ok, err)
	}
	if m.Score != 0.91 || len(m.Recordings) != 1 || m.Recordings[0].ID != "rec-1" || m.Recordings[0].ReleaseGroups[0].ID != "rg-1" {
		t.Fatalf("match: %+v", m)
	}
}

func TestLookupScoreThreshold(t *testing.T) {
	for _, tc := range []struct {
		score string
		ok    bool
	}{{"0.69", false}, {"0.70", true}, {"0.95", true}} {
		c := fakeAcoustID(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprintf(w, goodAnswer, tc.score) })
		if _, ok, err := c.Lookup(context.Background(), "FP", 100); err != nil || ok != tc.ok {
			t.Errorf("score %s: ok=%v err=%v", tc.score, ok, err)
		}
	}
}

func TestLookupNoResultsIsNoMatch(t *testing.T) {
	c := fakeAcoustID(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"status":"ok","results":[]}`) })
	if _, ok, err := c.Lookup(context.Background(), "FP", 100); err != nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}

func TestLookupHonoursRetryAfter(t *testing.T) {
	var hits atomic.Int32
	var waited []time.Duration
	c := fakeAcoustID(t, func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		fmt.Fprintf(w, goodAnswer, "0.9")
	})
	c.Sleep = func(ctx context.Context, d time.Duration) error {
		if d >= time.Second {
			waited = append(waited, d)
		}
		return nil
	}
	if _, ok, err := c.Lookup(context.Background(), "FP", 100); err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if hits.Load() != 2 || len(waited) != 1 || waited[0] != 7*time.Second {
		t.Fatalf("hits=%d waited=%v", hits.Load(), waited)
	}
}

func TestLookupGivesUpOnRepeated429(t *testing.T) {
	var hits atomic.Int32
	c := fakeAcoustID(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	})
	if _, ok, err := c.Lookup(context.Background(), "FP", 100); err == nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if hits.Load() != 3 {
		t.Fatalf("hits=%d", hits.Load())
	}
}

func TestLookupFailsOpen(t *testing.T) {
	t.Run("no key", func(t *testing.T) {
		c := &AcoustID{Key: func() string { return " " }}
		if _, ok, err := c.Lookup(context.Background(), "FP", 100); !errors.Is(err, ErrNoKey) || ok {
			t.Fatalf("ok=%v err=%v", ok, err)
		}
	})
	t.Run("bad key", func(t *testing.T) {
		c := fakeAcoustID(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadRequest) })
		if _, ok, err := c.Lookup(context.Background(), "FP", 100); !errors.Is(err, ErrRejected) || ok {
			t.Fatalf("ok=%v err=%v", ok, err)
		}
	})
	t.Run("network", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		url := srv.URL
		srv.Close()
		c := &AcoustID{Key: func() string { return "k" }, BaseURL: url}
		if _, ok, err := c.Lookup(context.Background(), "FP", 100); err == nil || ok {
			t.Fatalf("ok=%v err=%v", ok, err)
		}
	})
	t.Run("server error", func(t *testing.T) {
		c := fakeAcoustID(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) })
		if _, ok, err := c.Lookup(context.Background(), "FP", 100); err == nil || ok {
			t.Fatalf("ok=%v err=%v", ok, err)
		}
	})
}
