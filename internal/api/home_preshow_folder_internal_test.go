package api

import (
	"os"
	"path/filepath"
	"testing"
)

// A mapped clips folder is used by itself, starts with the usual categories,
// and gets the clips from the one in Usherr's config; a category deleted
// later isn't made again.
func TestPreshowMappedFolder(t *testing.T) {
	s, _ := optionsServer(t)
	s.cfg.ConfigDir = t.TempDir()
	mapped := filepath.Join(t.TempDir(), "preshow")
	old := preshowMapped
	preshowMapped = mapped
	t.Cleanup(func() { preshowMapped = old })

	inConfig := filepath.Join(s.cfg.ConfigDir, "preshow")
	if got := s.preshowDir(); got != inConfig {
		t.Fatalf("with nothing mapped the folder is %q", got)
	}
	_ = os.MkdirAll(filepath.Join(inConfig, "Bumpers"), 0o755)
	if err := os.WriteFile(filepath.Join(inConfig, "Bumpers", "lobby.mp4"), []byte("clip"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.preshowStart(inConfig)

	if err := os.MkdirAll(mapped, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := s.preshowDir(); got != mapped {
		t.Fatalf("a mapped folder isn't used: %q", got)
	}
	s.preshowStart(mapped)
	for _, c := range preshowStarters {
		if st, err := os.Stat(filepath.Join(mapped, c)); err != nil || !st.IsDir() {
			t.Errorf("no %s category", c)
		}
	}
	if b, err := os.ReadFile(filepath.Join(mapped, "Bumpers", "lobby.mp4")); err != nil || string(b) != "clip" {
		t.Errorf("the clip wasn't copied over: %v", err)
	}
	_ = os.Remove(filepath.Join(mapped, "Policy"))
	s.preshowStart(mapped)
	if _, err := os.Stat(filepath.Join(mapped, "Policy")); err == nil {
		t.Error("a deleted category came back")
	}
}
