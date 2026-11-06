package api_test

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlaylistFilesAreBroughtInMatchedAndReported(t *testing.T) {
	f := newFamilyMusic(t)
	album := filepath.Join(f.musicRoot, "Fixture Band", "First Album (1999)")
	write := func(p, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(album, "00-fixture_band-first_album.m3u"), "01 - Opening.flac\r\n02 - Middle Song.flac\r\n03 - Closing Time.flac\r\n")
	write(filepath.Join(f.musicRoot, "Lists", "mix.m3u8"), "\ufeff#EXTM3U\n#PLAYLIST:Weekend Mix\n"+
		"#EXTINF:200,Somebody Else - Not Here\nmissing/nothing.flac\n"+
		`E:\Music\Fixture Band\First Album (1999)\02 - Middle Song.flac`+"\n"+
		"#EXTINF:-1,Fixture Band - Closing Time\n../moved/away.flac\n")
	write(filepath.Join(f.musicRoot, ".hidden", "skip.m3u"), "01 - Opening.flac\n")

	// Only an administrator reads the music folder.
	postJSON[map[string]any](t, f.member, f.url("/api/music/playlist-files/import"), map[string]any{}, http.StatusForbidden)

	rep := postJSON[map[string]any](t, f.admin, f.url("/api/music/playlist-files/import"), map[string]any{}, http.StatusOK)
	if rep["found"] != float64(2) || rep["new"] != float64(2) {
		t.Fatalf("report: %+v", rep)
	}
	byFile := map[string]map[string]any{}
	for _, x := range rep["files"].([]any) {
		m := x.(map[string]any)
		byFile[m["file"].(string)] = m
	}
	albumList := byFile["Fixture Band/First Album (1999)/00-fixture_band-first_album.m3u"]
	if albumList["status"] != "new" || albumList["matched"] != float64(3) || albumList["name"] != "Fixture Band - First Album" {
		t.Fatalf("album playlist: %+v", albumList)
	}
	mix := byFile["Lists/mix.m3u8"]
	if mix["name"] != "Weekend Mix" || mix["entries"] != float64(3) || mix["matched"] != float64(2) {
		t.Fatalf("mix: %+v", mix)
	}
	un := mix["unmatched"].([]any)
	if len(un) != 1 || un[0].(map[string]any)["entry"] != "Somebody Else - Not Here" || un[0].(map[string]any)["line"] != float64(4) {
		t.Fatalf("unmatched: %+v", un)
	}
	mixID := int64(mix["playlistId"].(float64))
	got := getJSON[map[string]any](t, f.admin, f.url("/api/music/playlists/%d", mixID))
	if strings.Join(titlesOf(got), ",") != "Middle Song,Closing Time" || got["shared"] != true {
		t.Fatalf("mix playlist: %+v", got)
	}

	// Again: nothing changes. The last report is kept.
	rep = postJSON[map[string]any](t, f.admin, f.url("/api/music/playlist-files/import"), map[string]any{}, http.StatusOK)
	if rep["new"] != float64(0) || rep["updated"] != float64(0) {
		t.Fatalf("second run: %+v", rep)
	}
	status := getJSON[map[string]any](t, f.admin, f.url("/api/music/playlist-files"))
	if len(status["last"].(map[string]any)["files"].([]any)) != 2 || status["settings"].(map[string]any)["auto"] != false {
		t.Fatalf("status: %+v", status)
	}

	// The file changes: its playlist follows.
	write(filepath.Join(album, "00-fixture_band-first_album.m3u"), "03 - Closing Time.flac\n01 - Opening.flac\n")
	rep = postJSON[map[string]any](t, f.admin, f.url("/api/music/playlist-files/import"), map[string]any{}, http.StatusOK)
	if rep["updated"] != float64(1) {
		t.Fatalf("after a change: %+v", rep)
	}

	// Export as .m3u8.
	resp, err := f.admin.Get(f.url("/api/music/playlists/%d/m3u?relative=1", mixID))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("export: %v %v", resp, err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.HasPrefix(string(body), "#EXTM3U\n#PLAYLIST:Weekend Mix\n#EXTINF:") ||
		!strings.Contains(string(body), "Fixture Band - Middle Song\nFixture Band/First Album (1999)/02 - Middle Song.flac\n") {
		t.Fatalf("export: %s", body)
	}

	// A deleted playlist stays deleted, unless asked for again.
	req, _ := http.NewRequest(http.MethodDelete, f.url("/api/music/playlists/%d", mixID), nil)
	if resp, err := f.admin.Do(req); err != nil || resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %v %v", resp, err)
	}
	rep = postJSON[map[string]any](t, f.admin, f.url("/api/music/playlist-files/import"), map[string]any{}, http.StatusOK)
	if rep["new"] != float64(0) {
		t.Fatalf("a deleted playlist came back: %+v", rep)
	}
	rep = postJSON[map[string]any](t, f.admin, f.url("/api/music/playlist-files/import"), map[string]any{"again": true}, http.StatusOK)
	if rep["new"] != float64(1) {
		t.Fatalf("asked for again: %+v", rep)
	}

	// The daily import is a switch.
	set := postJSONMethod[map[string]any](t, f.admin, http.MethodPut, f.url("/api/music/playlist-files"), map[string]any{"auto": true, "shared": false}, http.StatusOK)
	if set["auto"] != true || set["shared"] != false {
		t.Fatalf("settings: %+v", set)
	}
}
