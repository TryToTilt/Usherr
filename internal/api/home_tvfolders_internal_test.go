package api

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExistingSeasonFolder(t *testing.T) {
	show := t.TempDir()
	for _, d := range []string{"Season 1", "Season 02", "season 3", "Specials", "Extras"} {
		if err := os.MkdirAll(filepath.Join(show, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cases := map[int]string{1: "Season 1", 2: "Season 02", 3: "season 3", 0: "Specials", 4: ""}
	for season, want := range cases {
		got := existingSeasonFolder(show, season)
		if want == "" && got != "" || want != "" && got != filepath.Join(show, want) {
			t.Errorf("season %d: got %q, want %q", season, got, want)
		}
	}
}
