package api

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestScreenOwnSettings(t *testing.T) {
	shared := defaultMarquee().clean()
	shared.Style, shared.TextScale = "posterr", 100

	// An older whole copy keeps only what differs from the shared settings.
	old := shared
	old.Style = "usherr"
	b, _ := json.Marshal(old)
	if _, legacy := decodeOwn(string(b)); !legacy {
		t.Fatal("a whole copy is the older kind")
	}
	own := diffOwn(shared, old)
	if keys := ownKeys(own); !slices.Equal(keys, []string{"style"}) {
		t.Fatalf("own: %v", keys)
	}

	// Stored and read back.
	got, legacy := decodeOwn(encodeOwn(own))
	if legacy || !slices.Equal(ownKeys(got), []string{"style"}) {
		t.Fatalf("read back: %v %v", ownKeys(got), legacy)
	}

	// The shared settings change: the screen follows, but keeps its look.
	shared.TextScale = 130
	m := withOwn(shared, got)
	if m.Style != "usherr" || m.TextScale != 130 {
		t.Fatalf("screen shows style %q, text %d", m.Style, m.TextScale)
	}
	if encodeOwn(nil) != "" || encodeOwn(screenOwn{}) != "" {
		t.Fatal("nothing of its own is stored as nothing")
	}
}
