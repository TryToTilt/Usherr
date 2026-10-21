package api

import (
	"encoding/json"
	"testing"
)

// Settings saved before the looks existed get the defaults; nonsense is
// replaced, good choices are kept.
func TestMarqueeLookCleaning(t *testing.T) {
	var old marqueeSettings
	if err := json.Unmarshal([]byte(`{"signMode":"auto","textScale":110}`), &old); err != nil {
		t.Fatal(err)
	}
	m := old.clean()
	if m.Style != "usherr" || m.Theme != "usherr" || m.Font != "classic" || m.Frame != "bulbs" || m.Transition != "fade" || m.TransitionMs != 900 || m.WallColumns != 3 || m.NightLevel != 40 {
		t.Fatalf("defaults: %+v", m)
	}
	if m.TextScale != 110 {
		t.Fatalf("kept setting lost: %d", m.TextScale)
	}

	bad := marqueeSettings{Style: "hologram", Accent: "red", Background: "#12345", Font: "comic", WallColumns: 40, TransitionMs: 99999, NightFrom: "25:00"}
	m = bad.clean()
	if m.Style != "usherr" || m.Accent != "" || m.Background != "" || m.Font != "classic" || m.WallColumns != 3 || m.TransitionMs != 900 || m.NightFrom != "23:00" {
		t.Fatalf("nonsense kept: %+v", m)
	}

	good := marqueeSettings{Style: "wall", Theme: "neon", Accent: "#FF00aa", Font: "deco", Frame: "gold", Transition: "cut", TransitionMs: 700, WallLayout: "hero", WallColumns: 5}
	m = good.clean()
	if m.Style != "wall" || m.Theme != "neon" || m.Accent != "#FF00aa" || m.Font != "deco" || m.Frame != "gold" || m.WallLayout != "hero" || m.WallColumns != 5 {
		t.Fatalf("good choices lost: %+v", m)
	}
	if m.Transition != "cut" || m.TransitionMs != 0 {
		t.Fatalf("a cut has no length: %s %d", m.Transition, m.TransitionMs)
	}
}
