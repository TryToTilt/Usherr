package main

import (
	"testing"
	"time"
)

// All the steps of the stop share one budget that ends before Docker's kill.
func TestShutdownStepsShareOneBudget(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	c := &shutdownClock{now: func() time.Time { return now }}

	// requests that are still running get what is left less the later steps
	if got := c.left(shutdownReserve); got != shutdownBudget-shutdownReserve {
		t.Fatalf("first step may take %s", got)
	}
	// that step used all of its time: the next ones have what remains
	now = now.Add(shutdownBudget - shutdownReserve)
	if got := c.left(schedulerReserve); got != shutdownReserve-schedulerReserve {
		t.Fatalf("second step may take %s, want %s", got, shutdownReserve-schedulerReserve)
	}
	now = now.Add(shutdownReserve - schedulerReserve)
	if got := c.left(0); got != schedulerReserve {
		t.Fatalf("last step may take %s, want %s", got, schedulerReserve)
	}
	// a step that ran over leaves the later ones a moment to try, no more
	now = now.Add(30 * time.Second)
	if got := c.left(0); got <= 0 || got > time.Second {
		t.Fatalf("with the budget spent a step may take %s", got)
	}
	if shutdownBudget >= 10*time.Second {
		t.Fatalf("the budget %s does not leave room before Docker's 10 second kill", shutdownBudget)
	}
}
