package api_test

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// The temporary download folder is checked when it is saved, saved when it is
// fine, reported back, and can be cleared again.
func TestTempDownloadFolderSetting(t *testing.T) {
	base, client := newFolderTestServer(t)
	url := base + "/api/settings"
	dir := t.TempDir()
	cache := filepath.Join(dir, "cache")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}

	got := getJSON[map[string]any](t, client, url)
	if v, _ := got["downloadsTempPath"].(string); v != "" {
		t.Fatalf("a fresh install has a temporary folder: %q", v)
	}
	wantBad(t, client, http.MethodPut, url, map[string]any{"downloadsTempPath": "cache"}, "isn't complete")
	wantBad(t, client, http.MethodPut, url, map[string]any{"downloadsTempPath": filepath.Join(dir, "missing")}, "doesn't exist")

	saved := postJSONMethod[map[string]any](t, client, http.MethodPut, url, map[string]any{"downloadsTempPath": " " + cache + " "}, http.StatusOK)
	if saved["downloadsTempPath"] != cache {
		t.Fatalf("saved folder = %v, want %s", saved["downloadsTempPath"], cache)
	}
	// Other settings can be saved without sending it, and it stays.
	again := postJSONMethod[map[string]any](t, client, http.MethodPut, url, map[string]any{"namingPreset": "plex"}, http.StatusOK)
	if again["downloadsTempPath"] != cache {
		t.Fatalf("the folder was lost by another save: %v", again["downloadsTempPath"])
	}
	// An empty value switches it off.
	off := postJSONMethod[map[string]any](t, client, http.MethodPut, url, map[string]any{"downloadsTempPath": ""}, http.StatusOK)
	if v, _ := off["downloadsTempPath"].(string); v != "" {
		t.Fatalf("the folder was not cleared: %q", v)
	}
}
