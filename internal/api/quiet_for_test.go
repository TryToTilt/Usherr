package api_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/api"
)

func TestQuietForAWhileHoldsHeavyJobs(t *testing.T) {
	defer api.SetQuietRecheck(10 * time.Millisecond)()
	server, base, client := loginNewServer(t)
	runs := 0
	server.TestHeavy(context.Background(), func() { runs++ })
	if runs != 1 {
		t.Fatalf("a heavy job runs normally: %d", runs)
	}
	if got := getJSON[map[string]any](t, client, base+"/api/automation/quiet"); got["active"] != false {
		t.Fatalf("not quiet at the start: %v", got)
	}
	got := putJSON(t, client, base+"/api/automation/quiet", map[string]any{"minutes": 60}, http.StatusOK)
	if got["active"] != true || got["until"] == nil {
		t.Fatalf("quiet for an hour: %v", got)
	}
	// A job that comes due during the quiet time does nothing while it lasts...
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	server.TestHeavy(ctx, func() { runs++ })
	cancel()
	if runs != 1 {
		t.Fatalf("a heavy job does nothing during the quiet time: %d", runs)
	}
	// ...but it is only delayed: when the quiet time ends the same run goes ahead.
	done := make(chan struct{})
	go func() {
		defer close(done)
		server.TestHeavy(context.Background(), func() { runs++ })
	}()
	select {
	case <-done:
		t.Fatal("the job ran during the quiet time")
	case <-time.After(60 * time.Millisecond):
	}
	putJSON(t, client, base+"/api/automation/quiet", map[string]any{"minutes": 100000}, http.StatusBadRequest)
	putJSON(t, client, base+"/api/automation/quiet", map[string]any{}, http.StatusBadRequest)
	got = putJSON(t, client, base+"/api/automation/quiet", map[string]any{"minutes": 0}, http.StatusOK)
	if got["active"] == true {
		t.Fatalf("0 ends it: %v", got)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the delayed job never ran after the quiet time ended")
	}
	if runs != 2 {
		t.Fatalf("the delayed run goes ahead once it is over: %d", runs)
	}
	server.TestHeavy(context.Background(), func() { runs++ })
	if runs != 3 {
		t.Fatalf("back to normal: %d", runs)
	}
}
