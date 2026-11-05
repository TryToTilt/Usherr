package api_test

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/mediaservers"
)

// A rule set puts a match on Leaving soon first, acts on it only after the
// grace period and only if it still matches, and never touches streamed files.
func TestCleanupRuleSetsLeavingSoon(t *testing.T) {
	server, base, client := loginNewServer(t)
	if _, err := server.MediaServers.Create(mediaservers.Server{Name: "Plex", Kind: mediaservers.KindPlex, BaseURL: fakeCleanupPlex(t).URL, Token: "tok", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	longAgo := time.Now().AddDate(0, 0, -90)
	movie := func(tmdb int, title, path string) (library.Movie, string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("video"), 0o644); err != nil {
			t.Fatal(err)
		}
		_ = os.Chtimes(path, longAgo, longAgo)
		m, err := server.MovieRepo.Add(library.Movie{TMDBID: tmdb, Title: title, Year: 2000, Monitored: true})
		if err != nil {
			t.Fatal(err)
		}
		if err := server.MovieRepo.SetStatus(m.ID, library.StatusDownloaded, "Bluray-1080p", path); err != nil {
			t.Fatal(err)
		}
		server.TestSetMovieAddedAt(m.ID, longAgo)
		return m, path
	}
	root := server.TestMoviesRoot()
	other := t.TempDir()
	matrix, matrixFile := movie(603, "The Matrix", filepath.Join(root, "The Matrix (2000)", "The Matrix (2000).mkv")) // watched 40 days ago
	heat, heatFile := movie(949, "Heat", filepath.Join(root, "Heat (2000)", "Heat (2000).mkv"))                       // watched 40 days ago, kept later
	_, alienFile := movie(348, "Alien", filepath.Join(root, "Alien (2000)", "Alien (2000).strm"))                     // never watched, but a .strm pointer
	_, roninFile := movie(8195, "Ronin", filepath.Join(other, "Movies-Stream", "Ronin (2000)", "Ronin (2000).mkv"))   // never watched, in a streamed folder

	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/watched/settings", map[string]any{"sync": true}, http.StatusOK)
	postJSON[map[string]any](t, client, base+"/api/watched/sync", nil, http.StatusOK)

	// Off by default, and a rule set can't be switched on without a condition.
	postJSON[map[string]any](t, client, base+"/api/cleanup/rule-sets", map[string]any{"name": "Empty", "library": "movies", "enabled": true}, http.StatusBadRequest)
	rs := postJSON[map[string]any](t, client, base+"/api/cleanup/rule-sets", map[string]any{
		"name": "Movies", "library": "movies", "match": "any",
		"conditions": []map[string]any{{"type": "watchedDays", "number": 30}, {"type": "unwatchedDays", "number": 60}},
	}, http.StatusOK)
	if rs["enabled"] != false || rs["graceDays"] != float64(14) || rs["action"] != "delete-unmonitor" {
		t.Fatalf("new rule set: %v", rs)
	}
	id := int64(rs["id"].(float64))

	// Preview of that rule set: two go on Leaving soon, the streamed two are left out.
	pv := getJSON[map[string]any](t, client, fmt.Sprintf("%s/api/cleanup/preview?ruleSet=%d", base, id))
	if n := len(pv["newLeaving"].([]any)); n != 2 || pv["skippedStreamed"] != float64(2) || len(pv["due"].([]any)) != 0 {
		t.Fatalf("preview: %v", pv)
	}

	rs["enabled"] = true
	postJSONMethod[map[string]any](t, client, http.MethodPut, fmt.Sprintf("%s/api/cleanup/rule-sets/%d", base, id), rs, http.StatusOK)

	now := time.Now()
	added, acted, _, err := server.TestRunCleanupRulesAt(now)
	if err != nil || added != 2 || acted != 0 {
		t.Fatalf("first run: added %d acted %d err %v", added, acted, err)
	}
	soon := getJSON[map[string]any](t, client, base+"/api/leaving-soon")
	if len(soon["items"].([]any)) != 2 || soon["canKeep"] != true {
		t.Fatalf("leaving soon: %v", soon)
	}

	// A week on nothing is due. Heat is kept with "Keep it".
	if _, acted, _, err := server.TestRunCleanupRulesAt(now.AddDate(0, 0, 7)); err != nil || acted != 0 {
		t.Fatalf("a week on: acted %d err %v", acted, err)
	}
	postJSON[map[string]any](t, client, base+"/api/leaving-soon/keep", map[string]any{"kind": "movie", "id": heat.ID}, http.StatusOK)

	// After the grace period: only The Matrix goes.
	if _, acted, _, err := server.TestRunCleanupRulesAt(now.AddDate(0, 0, 15)); err != nil || acted != 1 {
		t.Fatalf("after grace: acted %d err %v", acted, err)
	}
	if _, err := os.Stat(matrixFile); !os.IsNotExist(err) {
		t.Errorf("The Matrix's file should be gone: %v", err)
	}
	if m, _ := server.MovieRepo.Get(matrix.ID); m.FilePath != "" || m.Monitored {
		t.Errorf("The Matrix still has a file or is still looked for: %+v", m)
	}
	for _, f := range []string{heatFile, alienFile, roninFile} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("%s should still be there: %v", f, err)
		}
	}
	if left := getJSON[map[string]any](t, client, base+"/api/leaving-soon")["items"].([]any); len(left) != 0 {
		t.Errorf("leaving soon after the run: %v", left)
	}

	// The kept title can be let go again.
	sets := getJSON[map[string]any](t, client, base+"/api/cleanup/rule-sets")
	if ex := sets["exempt"].([]any); len(ex) != 1 || ex[0].(map[string]any)["byName"] != "Ryan Tester" {
		t.Fatalf("kept: %v", sets["exempt"])
	}
	postJSONMethod[map[string]any](t, client, http.MethodDelete, base+"/api/cleanup/exemptions/movie:"+fmt.Sprint(heat.ID), nil, http.StatusNoContent)

	// With a media server that doesn't answer, nothing happens.
	if _, err := server.MediaServers.Create(mediaservers.Server{Name: "Jellyfin", Kind: mediaservers.KindJellyfin, BaseURL: "http://127.0.0.1:1", Token: "k", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	postJSON[map[string]any](t, client, base+"/api/watched/sync", nil, http.StatusOK)
	postJSON[map[string]any](t, client, base+"/api/cleanup/run", nil, http.StatusConflict)
}
