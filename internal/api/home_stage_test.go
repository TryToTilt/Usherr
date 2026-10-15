package api

import (
	"testing"
	"time"
)

func TestQueueStageEstimatesEnd(t *testing.T) {
	const id = 987654
	defer clearQueueStage(id)
	setQueueStage(id, "Unpacking")
	if st, _, ends := queueStage(id, 0); st != "Unpacking" || ends != "" {
		t.Fatalf("no progress yet: stage %q ends %q", st, ends)
	}
	setStageProgress(id, "", 0)
	stageMu.Lock()
	queueStages[id].since = time.Now().Add(-60 * time.Second) // a minute in
	stageMu.Unlock()
	setStageProgress(id, "", 0.25)
	_, _, ends := queueStage(id, 0)
	end, err := time.Parse(time.RFC3339, ends)
	if err != nil {
		t.Fatalf("ends %q: %v", ends, err)
	}
	// 25% in a minute: three more minutes.
	if left := time.Until(end); left < 170*time.Second || left > 190*time.Second {
		t.Errorf("left = %v, want about 3m", left)
	}
	// A new step name starts a new step.
	setStageProgress(id, "Copying into the library", 0.1)
	if st, _, ends := queueStage(id, 0); st != "Copying into the library" || ends != "" {
		t.Errorf("new step: %q %q", st, ends)
	}
}
