package download

import "testing"

func TestConnectionCapLowersEveryGate(t *testing.T) {
	g := gateFor(ClientConfig{Host: "cap.example", Port: 563, Username: "u", Connections: 10})
	limit := func() int { g.mu.Lock(); defer g.mu.Unlock(); return g.limitLocked() }
	defer SetConnectionCap(0)
	if got := limit(); got != 10 {
		t.Fatalf("limit = %d, want 10", got)
	}
	SetConnectionCap(3)
	if got := limit(); got != 3 {
		t.Errorf("capped limit = %d, want 3", got)
	}
	SetConnectionCap(50)
	if got := limit(); got != 10 {
		t.Errorf("a cap above the plan = %d, want 10", got)
	}
	SetConnectionCap(0)
	if got := limit(); got != 10 {
		t.Errorf("uncapped limit = %d, want 10", got)
	}
}
