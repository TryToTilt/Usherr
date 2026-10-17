package api

import (
	"testing"
	"time"
)

// Shutdown ends a running pipeline without it counting as a failure, and a
// pipeline that starts afterwards ends at once.
func TestPipelinesShutdown(t *testing.T) {
	var r pipelineRegistry
	ctx, run := r.begin(1)
	go func() {
		<-ctx.Done()
		r.end(1, run)
	}()

	r.shutdown(2 * time.Second)
	if ctx.Err() == nil {
		t.Fatal("the running pipeline was not cancelled")
	}
	if r.isRunning(1) {
		t.Fatal("shutdown should wait until the pipeline has wound down")
	}
	handled, err := (&Server{}).settleInterrupted(1, run)
	if !handled || err != errPaused {
		t.Fatalf("a pipeline ended by shutdown must be left alone, got handled=%v err=%v", handled, err)
	}

	late, lateRun := r.begin(2)
	defer r.end(2, lateRun)
	if late.Err() == nil || !lateRun.shutdown.Load() {
		t.Fatal("a pipeline that starts after shutdown must end at once")
	}
}
