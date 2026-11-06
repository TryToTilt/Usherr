package api_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestPlayerSettingsHoldTranscodeAndLyricsFileSwitches(t *testing.T) {
	f := newFamilyMusic(t)
	got := getJSON[map[string]any](t, f.member, f.url("/api/music/player-settings"))
	if got["transcode"] != true || got["transcodeMaxKbps"] != float64(320) || got["transcodeFormat"] != "opus" || got["lyricsSaveLrc"] != false || got["lyricsOnline"] != true {
		t.Fatalf("defaults: %+v", got)
	}
	postJSONMethod[map[string]any](t, f.member, http.MethodPut, f.url("/api/music/player-settings"), map[string]any{"transcode": false}, http.StatusForbidden)
	postJSONMethod[map[string]any](t, f.admin, http.MethodPut, f.url("/api/music/player-settings"), map[string]any{"transcodeFormat": "flac"}, http.StatusBadRequest)
	postJSONMethod[map[string]any](t, f.admin, http.MethodPut, f.url("/api/music/player-settings"), map[string]any{"transcodeMaxKbps": 8}, http.StatusBadRequest)
	got = postJSONMethod[map[string]any](t, f.admin, http.MethodPut, f.url("/api/music/player-settings"), map[string]any{"transcodeFormat": "aac", "transcodeMaxKbps": 192, "lyricsSaveLrc": true}, http.StatusOK)
	if got["transcodeFormat"] != "aac" || got["transcodeMaxKbps"] != float64(192) || got["lyricsSaveLrc"] != true || got["lyricsOnline"] != true {
		t.Fatalf("saved: %+v", got)
	}
}

func TestSyncedLyricsFoundOnlineAreSavedAsLRCOnlyWhenSwitchedOn(t *testing.T) {
	f := newFamilyMusic(t)
	lr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":1,"plainLyrics":"online words","syncedLyrics":"[00:01.00]online\n[00:02.00]words","instrumental":false}`))
	}))
	t.Cleanup(lr.Close)
	f.server.TestSetLRCLIB(lr.URL)
	tracks, _ := f.server.MusicRepo.ListTracks(f.albumID)
	lrc := func(i int) string { return strings.TrimSuffix(tracks[i].FilePath, ".flac") + ".lrc" }

	getJSON[map[string]any](t, f.member, f.url("/api/music/tracks/%d/lyrics", f.trackIDs[0]))
	if _, err := os.Stat(lrc(0)); err == nil {
		t.Fatal("a .lrc was written while the switch is off")
	}
	postJSONMethod[map[string]any](t, f.admin, http.MethodPut, f.url("/api/music/player-settings"), map[string]any{"lyricsSaveLrc": true}, http.StatusOK)
	getJSON[map[string]any](t, f.member, f.url("/api/music/tracks/%d/lyrics", f.trackIDs[1]))
	b, err := os.ReadFile(lrc(1))
	if err != nil || string(b) != "[00:01.00]online\n[00:02.00]words\n" {
		t.Fatalf("saved .lrc: %q %v", b, err)
	}
	// An existing file is never replaced.
	if err := os.WriteFile(lrc(2), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	getJSON[map[string]any](t, f.member, f.url("/api/music/tracks/%d/lyrics", f.trackIDs[2]))
	if b, _ := os.ReadFile(lrc(2)); string(b) != "mine" {
		t.Fatalf("replaced: %q", b)
	}
}

func TestPlayConvertsWhenAskedAndKeepsWhatFits(t *testing.T) {
	f := newFamilyMusic(t)
	tracks, _ := f.server.MusicRepo.ListTracks(f.albumID)
	get := func(query string) (*http.Response, []byte) {
		t.Helper()
		resp, err := f.member.Get(f.url("/api/music/tracks/%d/play%s", f.trackIDs[0], query))
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return resp, b
	}
	// Nothing asked: the file as it is.
	if resp, _ := get(""); resp.StatusCode != http.StatusOK || resp.Header.Get("X-Usherr-Transcode") != "" || resp.Header.Get("Content-Type") != "audio/flac" {
		t.Fatalf("original: %d %v", resp.StatusCode, resp.Header)
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg isn't installed here")
	}
	if out, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=5", "-c:a", "flac", "-y", tracks[0].FilePath).CombinedOutput(); err != nil {
		t.Fatalf("make a flac: %v %s", err, out)
	}
	resp, body := get("?maxBitRate=96")
	if resp.StatusCode != http.StatusOK || resp.Header.Get("X-Usherr-Transcode") != "opus 96k" || resp.Header.Get("Content-Type") != "audio/ogg" || !strings.HasPrefix(string(body), "OggS") {
		t.Fatalf("converted: %d %v %q", resp.StatusCode, resp.Header, body[:min(len(body), 8)])
	}
	if resp.Header.Get("Accept-Ranges") != "bytes" {
		t.Fatalf("a converted file can be seeked: %v", resp.Header)
	}
	// The second time comes from the cache, with ranges.
	req, _ := http.NewRequest(http.MethodGet, f.url("/api/music/tracks/%d/play?maxBitRate=96", f.trackIDs[0]), nil)
	req.Header.Set("Range", "bytes=0-3")
	r2, err := f.member.Do(req)
	if err != nil || r2.StatusCode != http.StatusPartialContent {
		t.Fatalf("range: %v %v", r2, err)
	}
	r2.Body.Close()
	if resp, body := get("?format=mp3"); resp.Header.Get("Content-Type") != "audio/mpeg" || len(body) < 1000 {
		t.Fatalf("mp3: %v %d", resp.Header, len(body))
	}
	if resp, _ := get("?format=raw&maxBitRate=64"); resp.Header.Get("X-Usherr-Transcode") != "" {
		t.Fatalf("raw: %v", resp.Header)
	}
}
