package api

import (
	"os"
	"path/filepath"
	"testing"
)

// A new install that maps only /data gets comics and NSFW folders in it, and
// anime only when /data/anime exists; a kind's own mount wins.
func TestExtraFolderDefaults(t *testing.T) {
	base := t.TempDir()
	old := extraFolderBase
	extraFolderBase = base
	t.Cleanup(func() { extraFolderBase = old })

	if got := extraFolderDefault("comics"); got != "" {
		t.Fatalf("no /data: comics = %q, want none", got)
	}
	if err := os.MkdirAll(filepath.Join(base, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	for kind, want := range map[string]string{"comics": "/data/comics", "nsfw": "/data/nsfw", "anime": ""} {
		if got := extraFolderDefault(kind); got != want {
			t.Errorf("only /data: %s = %q, want %q", kind, got, want)
		}
	}
	if err := os.MkdirAll(filepath.Join(base, "data", "anime"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := extraFolderDefault("anime"); got != "/data/anime" {
		t.Errorf("with /data/anime: anime = %q", got)
	}
	if err := os.MkdirAll(filepath.Join(base, "comics"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := extraFolderDefault("comics"); got != "/comics" {
		t.Errorf("with /comics mapped: comics = %q", got)
	}
}
