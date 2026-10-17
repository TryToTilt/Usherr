package api_test

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// With "Keep the original file names" on, a downloaded movie goes into the
// usual title folder under the name its file came with.
func TestImportKeepsTheOriginalFileName(t *testing.T) {
	movieContent := []byte("a movie that keeps its own file name")
	_, client, httpSrv, movie, existingPath := newImportConflictEnv(t, movieContent)
	saved := postJSONMethod[map[string]any](t, client, http.MethodPut, httpSrv.URL+"/api/settings", map[string]any{
		"keepOriginalNames": true,
	}, http.StatusOK)
	if saved["keepOriginalNames"] != true {
		t.Fatalf("the setting should read back as on, got %v", saved["keepOriginalNames"])
	}

	item := grabFixtureAndWaitForEnd(t, client, httpSrv, movie, movieContent)
	if item["status"] != "completed" {
		t.Fatalf("expected the import to complete, got %+v", item)
	}
	kept := filepath.Join(filepath.Dir(existingPath), "fixture-movie.mkv")
	if got, err := os.ReadFile(kept); err != nil || string(got) != string(movieContent) {
		t.Fatalf("the movie should be at %s under its own name: %q %v", kept, got, err)
	}
	if got, _ := os.ReadFile(existingPath); string(got) != "pre-existing file content" {
		t.Fatalf("the file with the preset's name must be left alone, got %q", got)
	}
}

// A file already at the kept name goes through "If a file already exists"
// like any other clash: with "ask" the download waits for a decision.
func TestImportKeptNameClashUsesTheConflictSetting(t *testing.T) {
	movieContent := []byte("a second fixture-movie.mkv")
	_, client, httpSrv, movie, existingPath := newImportConflictEnv(t, movieContent)
	postJSONMethod[map[string]any](t, client, http.MethodPut, httpSrv.URL+"/api/settings", map[string]any{
		"keepOriginalNames": true,
	}, http.StatusOK)
	kept := filepath.Join(filepath.Dir(existingPath), "fixture-movie.mkv")
	if err := os.WriteFile(kept, []byte("already here"), 0o644); err != nil {
		t.Fatal(err)
	}

	item := grabFixtureAndWaitForConflict(t, client, httpSrv, movie, movieContent)
	if item["destPath"] != kept {
		t.Fatalf("the clash should be on the kept name %s, got %v", kept, item["destPath"])
	}
	if got, _ := os.ReadFile(kept); string(got) != "already here" {
		t.Fatalf("the file already there must be untouched while the download waits, got %q", got)
	}
}

// The settings round trip, and the rename tool refusing while names are kept.
func TestKeepOriginalNamesSettingsAndRename(t *testing.T) {
	_, client, httpSrv, _, _ := newImportConflictEnv(t, []byte("x"))
	got := getJSON[map[string]any](t, client, httpSrv.URL+"/api/settings")
	if got["keepOriginalNames"] != false {
		t.Fatalf("off unless switched on, got %v", got["keepOriginalNames"])
	}
	postJSONMethod[map[string]any](t, client, http.MethodPut, httpSrv.URL+"/api/settings", map[string]any{"keepOriginalNames": true}, http.StatusOK)
	// A save that leaves the field out keeps it.
	after := postJSONMethod[map[string]any](t, client, http.MethodPut, httpSrv.URL+"/api/settings", map[string]any{"namingPreset": "jellyfin"}, http.StatusOK)
	if after["keepOriginalNames"] != true {
		t.Fatalf("an unrelated save must not switch it off, got %v", after["keepOriginalNames"])
	}
	if plan := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/rename"); len(plan) != 0 {
		t.Fatalf("nothing to rename while names are kept, got %v", plan)
	}
	postJSONMethod[map[string]any](t, client, http.MethodPost, httpSrv.URL+"/api/rename", map[string]any{
		"items": []map[string]any{{"kind": "movie", "id": 1}},
	}, http.StatusConflict)
	preview := getJSON[map[string]any](t, client, httpSrv.URL+"/api/settings/naming-preview?preset=plex&keep=1")
	if preview["filename"] != "Example.Movie.2024.Extended.1080p.BluRay.DTS.x265-GROUP.mkv" || preview["folder"] != "Example Movie (2024)" {
		t.Fatalf("the preview should show the kept name in the usual folder, got %v", preview)
	}
}
