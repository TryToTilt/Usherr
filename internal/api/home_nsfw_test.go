package api_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// newFakeStashBox answers the stash-box queries the NSFW module makes,
// accepting only the key "good-key".
func newFakeStashBox(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("ApiKey") != "good-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		q := string(raw)
		switch {
		case strings.Contains(q, "me {"):
			_, _ = w.Write([]byte(`{"data":{"me":{"name":"tester"}}}`))
		case strings.Contains(q, "findScene"):
			_, _ = w.Write([]byte(`{"data":{"findScene":{"id":"nsfw-1","title":"Office Hours","release_date":"2024-03-15","duration":1800,
				"studio":{"id":"studio-1","name":"Acme Studios","aliases":[],"images":[]},"images":[],
				"performers":[{"as":"","performer":{"id":"perf-1","name":"Jane Doe","aliases":[]}}]}}}`))
		case strings.Contains(q, "findPerformer"):
			_, _ = w.Write([]byte(`{"data":{"findPerformer":{"id":"perf-1","name":"Jane Doe","aliases":[],"images":[],"gender":"FEMALE"}}}`))
		case strings.Contains(q, "queryScenes"):
			_, _ = w.Write([]byte(`{"data":{"queryScenes":{"count":2,"scenes":[
				{"id":"bf-1","title":"One","release_date":"2020-01-01","studio":{"id":"st","name":"S","aliases":[],"images":[]},"images":[],"tags":[],"performers":[{"as":"","performer":{"id":"perf-1","name":"Jane Doe","aliases":[],"gender":"FEMALE"}}]},
				{"id":"bf-2","title":"Two","release_date":"2021-01-01","studio":{"id":"st","name":"S","aliases":[],"images":[]},"images":[],"tags":[],"performers":[{"as":"","performer":{"id":"perf-1","name":"Jane Doe","aliases":[],"gender":"FEMALE"}}]}]}}}`))
		default:
			_, _ = w.Write([]byte(`{"data":{}}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFollowAndAddAllAddsTheCatalogue(t *testing.T) {
	_, base, admin, _, _ := familyServer(t)
	stash := newFakeStashBox(t)
	if code, body := doJSONStatus(t, admin, http.MethodPut, base+"/api/nsfw/settings", map[string]any{"enabled": true, "path": t.TempDir(), "endpoint": stash.URL, "key": "good-key"}); code != http.StatusOK {
		t.Fatalf("settings = %d %v", code, body)
	}
	// Following first (from today on) must not stop a later "add all".
	if code, body := doJSONStatus(t, admin, http.MethodPost, base+"/api/nsfw/performers", map[string]any{"stashId": "perf-1"}); code != http.StatusOK || body["added"] != float64(0) {
		t.Fatalf("follow = %d %v", code, body)
	}
	code, body := doJSONStatus(t, admin, http.MethodPost, base+"/api/nsfw/performers", map[string]any{"stashId": "perf-1", "backfill": true})
	if code != http.StatusOK || body["added"] != float64(2) {
		t.Fatalf("follow + add all = %d %v", code, body)
	}
	raw := videoGetRaw(t, admin, base+"/api/nsfw?status=wanted")
	if !strings.Contains(raw, "bf-1") || !strings.Contains(raw, "bf-2") {
		t.Fatalf("the catalogue should be in the library as wanted: %s", raw)
	}
	// Again: nothing new.
	if _, body := doJSONStatus(t, admin, http.MethodPost, base+"/api/nsfw/performers", map[string]any{"stashId": "perf-1", "backfill": true}); body["added"] != float64(0) {
		t.Fatalf("second add all = %v", body)
	}
}

func TestNsfwAreForAdministratorsOnly(t *testing.T) {
	_, base, admin, member, _ := familyServer(t)
	stash := newFakeStashBox(t)

	// Members can't reach any NSFW route, not even the settings.
	for _, path := range []string{"/api/nsfw/settings", "/api/nsfw", "/api/nsfw/performers", "/api/nsfw/lookup?q=x"} {
		if code, _ := doStatus(t, member, http.MethodGet, base+path); code != http.StatusForbidden && code != http.StatusNotFound {
			t.Fatalf("member GET %s = %d, want refused", path, code)
		}
	}

	// Off until switched on, for administrators too.
	if code, _ := doStatus(t, admin, http.MethodGet, base+"/api/nsfw"); code != http.StatusNotFound {
		t.Fatalf("videos while off = %d, want 404", code)
	}

	// A wrong key is refused and not saved.
	code, body := doJSONStatus(t, admin, http.MethodPut, base+"/api/nsfw/settings", map[string]any{"endpoint": stash.URL, "key": "bad-key"})
	if code != http.StatusBadRequest {
		t.Fatalf("bad key = %d %v", code, body)
	}
	dir := t.TempDir()
	code, body = doJSONStatus(t, admin, http.MethodPut, base+"/api/nsfw/settings", map[string]any{"enabled": true, "path": dir, "endpoint": stash.URL, "key": "good-key"})
	if code != http.StatusOK || body["keySet"] != true || body["enabled"] != true {
		t.Fatalf("save settings = %d %v", code, body)
	}
	if strings.Contains(videoJSON(t, body), "good-key") {
		t.Fatal("the settings answer must never include the key itself")
	}

	// Add a video and grab a release for it.
	code, body = doJSONStatus(t, admin, http.MethodPost, base+"/api/nsfw", map[string]any{"stashId": "nsfw-1"})
	if code != http.StatusOK || body["stashId"] != "nsfw-1" {
		t.Fatalf("add video = %d %v", code, body)
	}
	id := body["id"].(float64)
	nzb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) }))
	t.Cleanup(nzb.Close)
	code, body = doJSONStatus(t, admin, http.MethodPost, base+"/api/nsfw/"+videoIDText(id)+"/grab", map[string]any{
		"title": "AcmeStudios.24.03.15.Jane.Doe.XXX.1080p.MP4-WRB", "downloadUrl": nzb.URL + "/get.nzb", "protocol": "usenet",
	})
	if code != http.StatusOK {
		t.Fatalf("grab = %d %v", code, body)
	}

	// Video downloads only show on the NSFW page's own view
	// (?videos=only, administrators); every shared view leaves them out,
	// for administrators too.
	deadline := time.Now().Add(10 * time.Second)
	for {
		adminQueue := videoGetList(t, admin, base+"/api/queue?nsfw=only")
		if len(adminQueue) == 1 {
			if adminQueue[0]["subtitle"] != "Video · Acme Studios" {
				t.Fatalf("admin queue item = %v", adminQueue[0])
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("admin queue = %v", adminQueue)
		}
		time.Sleep(100 * time.Millisecond)
	}
	for _, client := range []*http.Client{admin, member} {
		if q := videoGetList(t, client, base+"/api/queue"); len(q) != 0 {
			t.Fatalf("a shared queue view shows a video download: %v", q)
		}
		raw := videoGetRaw(t, client, base+"/api/activity")
		if strings.Contains(raw, "Office Hours") || strings.Contains(raw, "nsfw-") {
			t.Fatalf("shared activity mentions the video: %s", raw)
		}
		dash := videoGetRaw(t, client, base+"/api/dashboard")
		if strings.Contains(dash, "Office Hours") || strings.Contains(dash, "AcmeStudios") {
			t.Fatalf("the dashboard mentions the video: %s", dash)
		}
	}
	// The NSFW page's own activity has it; members can't ask for that view.
	if raw := videoGetRaw(t, admin, base+"/api/activity?nsfw=only"); !strings.Contains(raw, "nsfw-grabbed") || strings.Contains(raw, `"update"`) {
		t.Fatalf("videos activity view = %s", raw)
	}
	if q := videoGetList(t, member, base+"/api/queue?nsfw=only"); len(q) != 0 {
		t.Fatalf("a member asking for the videos view must get nothing: %v", q)
	}
	// The failed download stays out of the shared blocklist and logs too.
	deadline = time.Now().Add(10 * time.Second)
	for {
		failed := false
		for _, it := range videoGetList(t, admin, base+"/api/queue?nsfw=only") {
			failed = failed || it["status"] == "failed"
		}
		if failed || time.Now().After(deadline) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	for _, path := range []string{"/api/blocklist", "/api/system/problems", "/api/stats/library"} {
		if raw := videoGetRaw(t, admin, base+path); strings.Contains(raw, "AcmeStudios") || strings.Contains(raw, "Office Hours") {
			t.Fatalf("%s mentions the video: %s", path, raw)
		}
	}
}

func videoIDText(f float64) string { return strconv.FormatInt(int64(f), 10) }

func videoJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func videoGetRaw(t *testing.T, client *http.Client, url string) string {
	t.Helper()
	resp, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

func videoGetList(t *testing.T, client *http.Client, url string) []map[string]any {
	t.Helper()
	var out []map[string]any
	if err := json.Unmarshal([]byte(videoGetRaw(t, client, url)), &out); err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return out
}

func TestNsfwImportFromWhisparr(t *testing.T) {
	_, base, admin, _, _ := familyServer(t)
	stash := newFakeStashBox(t)
	lib := t.TempDir()
	// Whisparr's "/movies/nsfw/Studio X/2024-01-02 - Title/x.mp4" is lib/nsfw/... here.
	fileDir := lib + "/nsfw/Studio X/2024-01-02 - Title"
	if err := os.MkdirAll(fileDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fileDir+"/x.mp4", []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	wh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "wkey" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/v3/movie":
			_, _ = w.Write([]byte(`[
				{"title":"Title","releaseDate":"2024-01-02T00:00:00Z","runtime":30,"studioTitle":"Studio X","studioForeignId":"st-x",
				 "path":"/movies/nsfw/Studio X/2024-01-02 - Title","hasFile":true,"monitored":true,"stashId":"sc-1","itemType":"scene",
				 "images":[{"coverType":"screenshot","remoteUrl":"https://cdn.example/1.jpg"}],
				 "performerForeignIds":["p-1"],"performerNames":["Jane Doe"],
				 "movieFile":{"relativePath":"x.mp4","quality":{"quality":{"resolution":2160}}}},
				{"title":"Wanted","releaseDate":"2024-02-03T00:00:00Z","studioTitle":"Studio X","studioForeignId":"st-x","monitored":true,"stashId":"sc-2","itemType":"scene"},
				{"title":"No id","monitored":true,"itemType":"scene"}
			]`))
		case "/api/v3/studio":
			_, _ = w.Write([]byte(`[{"title":"Studio X","foreignId":"st-x","monitored":true,"aliases":["StudioX"]},{"title":"Other","foreignId":"st-o","monitored":false}]`))
		case "/api/v3/performer":
			_, _ = w.Write([]byte(`[{"name":"Jane Doe","foreignId":"p-1","monitored":false}]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(wh.Close)

	if code, body := doJSONStatus(t, admin, http.MethodPut, base+"/api/nsfw/settings", map[string]any{"enabled": true, "path": lib, "endpoint": stash.URL, "key": "good-key"}); code != http.StatusOK {
		t.Fatalf("settings = %d %v", code, body)
	}
	req := map[string]any{"url": wh.URL, "apiKey": "wkey", "from": "/movies", "to": lib, "dryRun": true}
	code, rep := doJSONStatus(t, admin, http.MethodPost, base+"/api/nsfw/import/whisparr", req)
	if code != http.StatusOK || rep["nsfwAdded"] != float64(2) || rep["filesFound"] != float64(1) || rep["skippedNoStashId"] != float64(1) {
		t.Fatalf("preview = %d %v", code, rep)
	}
	if list := videoGetRaw(t, admin, base+"/api/nsfw"); strings.Contains(list, "sc-1") {
		t.Fatal("a preview must not change the library")
	}

	req["dryRun"] = false
	code, rep = doJSONStatus(t, admin, http.MethodPost, base+"/api/nsfw/import/whisparr", req)
	if code != http.StatusOK || rep["nsfwAdded"] != float64(2) {
		t.Fatalf("import = %d %v", code, rep)
	}
	var lib1 struct {
		Nsfw []struct {
			StashID  string `json:"stashId"`
			Status   string `json:"status"`
			FilePath string `json:"filePath"`
			Quality  string `json:"quality"`
		} `json:"nsfw"`
	}
	if err := json.Unmarshal([]byte(videoGetRaw(t, admin, base+"/api/nsfw")), &lib1); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, s := range lib1.Nsfw {
		got[s.StashID] = s.Status + "|" + s.Quality
		if s.StashID == "sc-1" && s.FilePath != fileDir+"/x.mp4" {
			t.Fatalf("file registered at %q, want it where it is", s.FilePath)
		}
	}
	if got["sc-1"] != "downloaded|2160p" || got["sc-2"] != "missing|" {
		t.Fatalf("imported videos = %v", got)
	}
	studios := videoGetList(t, admin, base+"/api/nsfw/studios")
	if len(studios) != 1 || studios[0]["name"] != "Studio X" || studios[0]["checkedAt"] == "" {
		t.Fatalf("followed studios = %v", studios)
	}
	if _, err := os.Stat(fileDir + "/x.mp4"); err != nil {
		t.Fatal("the import must never move or delete a file")
	}
	// Running it again changes nothing.
	_, rep = doJSONStatus(t, admin, http.MethodPost, base+"/api/nsfw/import/whisparr", req)
	if rep["nsfwAdded"] != float64(0) || rep["nsfwUpdated"] != float64(2) {
		t.Fatalf("second import = %v", rep)
	}
}

func TestVideoStreamServesOnlyItsFile(t *testing.T) {
	_, base, admin, member, _ := familyServer(t)
	stash := newFakeStashBox(t)
	lib := t.TempDir()
	if code, body := doJSONStatus(t, admin, http.MethodPut, base+"/api/nsfw/settings", map[string]any{"enabled": true, "path": lib, "endpoint": stash.URL, "key": "good-key"}); code != http.StatusOK {
		t.Fatalf("settings = %d %v", code, body)
	}
	if err := os.MkdirAll(lib+"/Studio X", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lib+"/Studio X/a.mp4", []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	wh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/movie":
			_, _ = w.Write([]byte(`[{"title":"A","releaseDate":"2024-01-02T00:00:00Z","path":"/movies/Studio X","hasFile":true,"monitored":true,"stashId":"sc-a","itemType":"scene","movieFile":{"relativePath":"a.mp4"}}]`))
		default:
			_, _ = w.Write([]byte(`[]`))
		}
	}))
	t.Cleanup(wh.Close)
	if code, rep := doJSONStatus(t, admin, http.MethodPost, base+"/api/nsfw/import/whisparr", map[string]any{"url": wh.URL, "apiKey": "k", "from": "/movies", "to": lib}); code != http.StatusOK || rep["filesFound"] != float64(1) {
		t.Fatalf("import = %d %v", code, rep)
	}
	list := videoGetRaw(t, admin, base+"/api/nsfw")
	var out struct {
		Nsfw []struct {
			ID float64 `json:"id"`
		} `json:"nsfw"`
	}
	_ = json.Unmarshal([]byte(list), &out)
	url := base + "/api/nsfw/" + videoIDText(out.Nsfw[0].ID) + "/stream"
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Range", "bytes=2-4")
	resp, err := admin.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent || string(body) != "234" || resp.Header.Get("Content-Type") != "video/mp4" {
		t.Fatalf("stream = %d %q %s", resp.StatusCode, body, resp.Header.Get("Content-Type"))
	}
	if code, _ := doStatus(t, member, http.MethodGet, url); code != http.StatusForbidden && code != http.StatusNotFound {
		t.Fatalf("a member must not stream videos, got %d", code)
	}
}

// A fake Jellyfin for the player: it converts unless the browser says it
// can show Dolby Vision, and records what it was asked.
type fakeJellyfin struct {
	srv       *httptest.Server
	stopped   bool
	leakedKey bool
}

func newFakeJellyfin(t *testing.T, libDir string) *fakeJellyfin {
	t.Helper()
	f := &fakeJellyfin{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Emby-Token") != "jf-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Query().Get("api_key") != "" {
			f.leakedKey = true
		}
		switch {
		case r.URL.Path == "/System/Info":
			_, _ = w.Write([]byte(`{"Id":"jf1","ServerName":"Fake","Version":"10.10.0"}`))
		case r.URL.Path == "/Library/VirtualFolders":
			_, _ = w.Write([]byte(`[{"ItemId":"lib1","Locations":["D:\\Whisparr"]}]`))
		case r.URL.Path == "/Items" && r.URL.Query().Get("ParentId") == "lib1":
			_, _ = w.Write([]byte(`{"Items":[{"Id":"item1","Path":"D:\\Whisparr\\Studio X\\a.mp4"}]}`))
		case r.URL.Path == "/Items/item1/PlaybackInfo":
			raw, _ := io.ReadAll(r.Body)
			if strings.Contains(string(raw), "DOVI") {
				_, _ = w.Write([]byte(`{"PlaySessionId":"ps1","MediaSources":[{"Id":"ms","SupportsDirectPlay":true}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"PlaySessionId":"ps1","MediaSources":[{"Id":"ms","SupportsDirectPlay":false,"TranscodeReasons":["VideoRangeTypeNotSupported"],
				"TranscodingUrl":"/videos/item1/master.m3u8?DeviceId=dev1&MediaSourceId=ms&PlaySessionId=ps1&api_key=SECRET"}]}`))
		case r.URL.Path == "/videos/item1/master.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = w.Write([]byte("#EXTM3U\nmain.m3u8?x=1\n"))
		case r.URL.Path == "/videos/item1/hls1/main/0.ts":
			_, _ = w.Write([]byte("SEGMENT"))
		case r.URL.Path == "/Videos/ActiveEncodings" && r.Method == http.MethodDelete:
			f.stopped = r.URL.Query().Get("PlaySessionId") == "ps1"
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func TestPlayerConvertsThroughJellyfin(t *testing.T) {
	_, base, admin, member, _ := familyServer(t)
	stash := newFakeStashBox(t)
	lib := t.TempDir()
	if code, body := doJSONStatus(t, admin, http.MethodPut, base+"/api/nsfw/settings", map[string]any{"enabled": true, "path": lib, "endpoint": stash.URL, "key": "good-key"}); code != http.StatusOK {
		t.Fatalf("settings = %d %v", code, body)
	}
	_ = os.MkdirAll(lib+"/Studio X", 0o755)
	_ = os.WriteFile(lib+"/Studio X/a.mp4", []byte("0123456789"), 0o644)
	wh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/movie" {
			_, _ = w.Write([]byte(`[{"title":"A","releaseDate":"2024-01-02T00:00:00Z","path":"/movies/Studio X","hasFile":true,"monitored":true,"stashId":"sc-a","itemType":"scene","movieFile":{"relativePath":"a.mp4"}}]`))
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(wh.Close)
	doJSONStatus(t, admin, http.MethodPost, base+"/api/nsfw/import/whisparr", map[string]any{"url": wh.URL, "apiKey": "k", "from": "/movies", "to": lib})
	var lst struct {
		Nsfw []struct {
			ID float64 `json:"id"`
		} `json:"nsfw"`
	}
	_ = json.Unmarshal([]byte(videoGetRaw(t, admin, base+"/api/nsfw")), &lst)
	id := lst.Nsfw[0].ID

	// Without Jellyfin: play the file as it is.
	code, plan := doJSONStatus(t, admin, http.MethodPost, base+"/api/play/prepare", map[string]any{"kind": "video", "id": id, "caps": map[string]any{"hevc": true}})
	if code != http.StatusOK || plan["mode"] != "direct" || plan["url"] != "/api/nsfw/"+videoIDText(id)+"/stream" {
		t.Fatalf("prepare without Jellyfin = %d %v", code, plan)
	}

	jf := newFakeJellyfin(t, lib)
	if code, body := doJSONStatus(t, admin, http.MethodPost, base+"/api/media-servers", map[string]any{
		"name": "Jellyfin", "kind": "jellyfin", "baseUrl": jf.srv.URL, "apiKey": "jf-key",
		"pathMap": []map[string]string{{"from": lib, "to": `D:\Whisparr`}},
	}); code/100 != 2 {
		t.Fatalf("add Jellyfin = %d %v", code, body)
	}

	// A browser that shows Dolby Vision plays directly.
	_, plan = doJSONStatus(t, admin, http.MethodPost, base+"/api/play/prepare", map[string]any{"kind": "video", "id": id, "caps": map[string]any{"hevc": true, "dv": true, "hdr": true}})
	if plan["mode"] != "direct" {
		t.Fatalf("a capable browser should play directly: %v", plan)
	}
	// One that can't gets Jellyfin's conversion, relayed by Mediarium.
	_, plan = doJSONStatus(t, admin, http.MethodPost, base+"/api/play/prepare", map[string]any{"kind": "video", "id": id, "caps": map[string]any{"hevc": true}})
	if plan["mode"] != "hls" {
		t.Fatalf("expected a converted stream: %v", plan)
	}
	if strings.Contains(videoJSON(t, plan), "SECRET") || strings.Contains(videoJSON(t, plan), "jf-key") {
		t.Fatal("Jellyfin's key must never reach the page")
	}
	master := videoGetRaw(t, admin, base+plan["url"].(string))
	if !strings.Contains(master, "main.m3u8") {
		t.Fatalf("master playlist = %q", master)
	}
	tok := plan["session"].(string)
	if seg := videoGetRaw(t, admin, base+"/api/play/hls/"+tok+"/hls1/main/0.ts?x=1"); seg != "SEGMENT" {
		t.Fatalf("segment = %q", seg)
	}
	if jf.leakedKey {
		t.Fatal("the relay must send Jellyfin's key as a header, not in the address")
	}
	// Someone else can't use the stream, and odd paths are refused.
	if code, _ := doStatus(t, member, http.MethodGet, base+"/api/play/hls/"+tok+"/master.m3u8"); code == http.StatusOK {
		t.Fatal("another account must not use this stream")
	}
	if code, _ := doStatus(t, admin, http.MethodGet, base+"/api/play/hls/"+tok+"/../../System/Info"); code == http.StatusOK {
		t.Fatal("the relay must only reach this video's files")
	}
	// Closing the player stops Jellyfin's conversion.
	if code, _ := doStatus(t, admin, http.MethodDelete, base+"/api/play/hls/"+tok); code != http.StatusNoContent || !jf.stopped {
		t.Fatalf("stop = %d stopped=%v", code, jf.stopped)
	}
	if code, _ := doStatus(t, admin, http.MethodGet, base+"/api/play/hls/"+tok+"/master.m3u8"); code == http.StatusOK {
		t.Fatal("a stopped stream must be gone")
	}
	// Members can't prepare a video at all.
	if code, _ := doJSONStatus(t, member, http.MethodPost, base+"/api/play/prepare", map[string]any{"kind": "video", "id": id}); code == http.StatusOK {
		t.Fatal("members must not play videos")
	}
}

func TestAddAllAndSearchQueuesTheWantedNsfw(t *testing.T) {
	_, base, admin, _, _ := familyServer(t)
	stash := newFakeStashBox(t)
	if code, body := doJSONStatus(t, admin, http.MethodPut, base+"/api/nsfw/settings", map[string]any{"enabled": true, "path": t.TempDir(), "endpoint": stash.URL, "key": "good-key"}); code != http.StatusOK {
		t.Fatalf("settings = %d %v", code, body)
	}
	code, body := doJSONStatus(t, admin, http.MethodPost, base+"/api/nsfw/performers", map[string]any{"stashId": "perf-1", "backfill": true, "search": true})
	if code != http.StatusOK || body["added"] != float64(2) || body["searching"] != float64(2) {
		t.Fatalf("add all and search = %d %v", code, body)
	}
	var lib struct {
		Nsfw []struct {
			ID float64 `json:"id"`
		} `json:"nsfw"`
	}
	_ = json.Unmarshal([]byte(videoGetRaw(t, admin, base+"/api/nsfw")), &lib)
	ids := []float64{}
	for _, s := range lib.Nsfw {
		ids = append(ids, s.ID)
	}
	// One video not wanted any more: bulk Search skips it.
	doJSONStatus(t, admin, http.MethodPost, base+"/api/nsfw/bulk", map[string]any{"ids": ids[:1], "action": "unwant"})
	_, res := doJSONStatus(t, admin, http.MethodPost, base+"/api/nsfw/bulk", map[string]any{"ids": ids, "action": "search"})
	if res["changed"].(float64)+res["skipped"].(float64) != float64(len(ids)) || res["skipped"].(float64) < 1 {
		t.Fatalf("bulk search = %v", res)
	}
	if code, st := doStatus(t, admin, http.MethodGet, base+"/api/nsfw/search-status"); code != http.StatusOK || st["total"] == nil {
		t.Fatalf("search status = %d %v", code, st)
	}
}
