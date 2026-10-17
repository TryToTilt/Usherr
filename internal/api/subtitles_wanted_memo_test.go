package api_test

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/library"
)

// The Wanted list remembers what each video lacked, but a subtitle file that
// appears next to it, or a change of languages, still shows at once.
func TestSubtitlesWantedNoticesChangesAfterBeingRemembered(t *testing.T) {
	env := newTVAutoEnv(t, nil, nil)
	env.server.TestSetSubtitlesBaseURL("fixture-key", newFakeOpenSubtitles(t).srv.URL)
	dir := t.TempDir()
	video := filepath.Join(dir, "Some Movie (2001).mkv")
	if err := os.WriteFile(video, []byte("v"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(dir, old, old); err != nil { // a settled folder, so the answer is kept
		t.Fatal(err)
	}
	m, err := env.server.MovieRepo.Add(library.Movie{TMDBID: 9, Title: "Some Movie", Year: 2001, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	_ = env.server.MovieRepo.SetStatus(m.ID, library.StatusDownloaded, "Bluray-1080p", video)
	url := env.baseURL + "/api/subtitles/wanted"
	for i := 0; i < 2; i++ {
		if w := getJSON[[]map[string]any](t, env.client, url); len(w) != 1 {
			t.Fatalf("round %d: want the movie missing a subtitle, got %v", i, w)
		}
	}

	putJSONStatus(t, env.client, env.baseURL+"/api/settings", map[string]any{"subtitleLanguages": []string{"en", "it"}}, http.StatusOK)
	w := getJSON[[]map[string]any](t, env.client, url)
	if len(w) != 1 || len(w[0]["missing"].([]any)) != 2 {
		t.Fatalf("a new language should show as missing: %v", w)
	}

	for _, l := range []string{"en", "it"} {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("Some Movie (2001).%s.srt", l)), []byte("1\n00:00:01,000 --> 00:00:02,000\nhi\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if w := getJSON[[]map[string]any](t, env.client, url); len(w) != 0 {
		t.Fatalf("subtitle files were added, the movie should be off the list: %v", w)
	}
}
