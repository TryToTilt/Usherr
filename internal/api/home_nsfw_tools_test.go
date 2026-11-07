package api_test

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/nsfw"
)

// fakeToolStash answers what the NSFW tools ask, and notes every query so a
// test can see nothing else was asked.
type fakeToolStash struct {
	mu      sync.Mutex
	hash    string // the fingerprint that has a match
	queries []string
	srv     *httptest.Server
}

func (f *fakeToolStash) asked(word string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, q := range f.queries {
		if strings.Contains(q, word) {
			n++
		}
	}
	return n
}

func newFakeToolStash(t *testing.T) *fakeToolStash {
	t.Helper()
	f := &fakeToolStash{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("ApiKey") != "good-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		q := string(raw)
		f.mu.Lock()
		f.queries = append(f.queries, q)
		hash := f.hash
		f.mu.Unlock()
		switch {
		case strings.Contains(q, "me {"):
			_, _ = w.Write([]byte(`{"data":{"me":{"name":"tester"}}}`))
		case strings.Contains(q, "findScenesBySceneFingerprints"):
			var body struct {
				Variables struct {
					FP [][]struct {
						Hash string `json:"hash"`
					} `json:"fp"`
				} `json:"variables"`
			}
			_ = json.Unmarshal(raw, &body)
			out := []any{}
			for _, list := range body.Variables.FP {
				row := []any{}
				if list[0].Hash == hash {
					row = append(row, map[string]any{"id": "scene-1", "title": "Long Evening", "release_date": "2024-03-15", "duration": 1800,
						"studio": map[string]any{"id": "st-1", "name": "Example Studio", "aliases": []any{}, "images": []any{}}, "images": []any{}, "tags": []any{},
						"performers":   []any{map[string]any{"as": "", "performer": map[string]any{"id": "p-1", "name": "Sample Person", "aliases": []any{}}}},
						"fingerprints": []any{map[string]any{"hash": hash, "algorithm": "OSHASH", "duration": 1800, "submissions": 3}}})
				}
				out = append(out, row)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"findScenesBySceneFingerprints": out}})
		case strings.Contains(q, "queryPerformers"):
			_, _ = w.Write([]byte(`{"data":{"queryPerformers":{"count":2,"performers":[{"id":"fav-p1","name":"First Favorite","aliases":[],"images":[]},{"id":"fav-p2","name":"Second Favorite","aliases":[],"images":[]}]}}}`))
		case strings.Contains(q, "queryStudios"):
			_, _ = w.Write([]byte(`{"data":{"queryStudios":{"count":1,"studios":[{"id":"fav-s1","name":"Favorite Studio","aliases":[],"images":[]}]}}}`))
		default:
			t.Errorf("unexpected question to StashDB: %s", q)
			_, _ = w.Write([]byte(`{"data":{}}`))
		}
	}))
	t.Cleanup(f.srv.Close)
	old := nsfw.RequestGap
	nsfw.RequestGap = 0
	t.Cleanup(func() { nsfw.RequestGap = old })
	return f
}

// waitNsfwJob polls a tools page until its job has ended.
func waitNsfwJob(t *testing.T, client *http.Client, url string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		var page map[string]any
		if err := json.Unmarshal([]byte(videoGetRaw(t, client, url)), &page); err != nil {
			t.Fatal(err)
		}
		job, _ := page["job"].(map[string]any)
		if job != nil && job["phase"] != "running" {
			return page
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the job did not finish")
	return nil
}

func TestNsfwToolsIdentifyRenameUndo(t *testing.T) {
	_, base, admin, _, _ := familyServer(t)
	stash := newFakeToolStash(t)
	root := t.TempDir()
	if code, body := doJSONStatus(t, admin, http.MethodPut, base+"/api/nsfw/settings", map[string]any{"enabled": true, "path": root, "endpoint": stash.srv.URL, "key": "good-key"}); code != http.StatusOK {
		t.Fatalf("settings = %d %v", code, body)
	}

	data := make([]byte, 128*1024)
	binary.LittleEndian.PutUint64(data, 12345)
	old := filepath.Join(root, "incoming", "abc123.mp4")
	if err := os.MkdirAll(filepath.Dir(old), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, data, 0o644); err != nil {
		t.Fatal(err)
	}
	srt := filepath.Join(root, "incoming", "abc123.en.srt")
	if err := os.WriteFile(srt, []byte("subtitle"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "incoming", "nomatch.mp4"), append([]byte{1}, data[1:]...), 0o644); err != nil {
		t.Fatal(err)
	}
	h, err := nsfw.OSHash(old)
	if err != nil {
		t.Fatal(err)
	}
	stash.mu.Lock()
	stash.hash = h
	stash.mu.Unlock()

	// Find matches: shows the match, registers nothing.
	if code, body := doJSONStatus(t, admin, http.MethodPost, base+"/api/nsfw/identify/start", map[string]any{}); code != http.StatusAccepted {
		t.Fatalf("start = %d %v", code, body)
	}
	page := waitNsfwJob(t, admin, base+"/api/nsfw/identify")
	items := page["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items = %v", items)
	}
	item := items[0].(map[string]any)
	if item["path"] != filepath.Join("incoming", "abc123.mp4") {
		t.Fatalf("path = %v", item["path"])
	}
	cands := item["candidates"].([]any)
	cand := cands[0].(map[string]any)
	if cand["matched"] != "OSHASH" || cand["video"].(map[string]any)["stashId"] != "scene-1" {
		t.Fatalf("candidate = %v", cand)
	}
	if cand["unsure"] != true || cand["length"] != "unknown" {
		t.Fatalf("this file has no readable length, so the match is unsure: %v", cand)
	}
	if counts := page["counts"].(map[string]any); counts["none"] != float64(1) {
		t.Fatalf("counts = %v", counts)
	}
	if raw := videoGetRaw(t, admin, base+"/api/nsfw"); strings.Contains(raw, "scene-1") {
		t.Fatalf("nothing is registered until confirmed: %s", raw)
	}

	// Confirm: registered where it is.
	if code, body := doJSONStatus(t, admin, http.MethodPost, base+"/api/nsfw/identify/confirm", map[string]any{"items": []any{map[string]any{"id": item["id"], "stashId": "scene-1"}}}); code != http.StatusAccepted {
		t.Fatalf("confirm = %d %v", code, body)
	}
	waitNsfwJob(t, admin, base+"/api/nsfw/identify")
	var lib struct {
		Nsfw []nsfw.Video `json:"nsfw"`
	}
	if err := json.Unmarshal([]byte(videoGetRaw(t, admin, base+"/api/nsfw")), &lib); err != nil {
		t.Fatal(err)
	}
	if len(lib.Nsfw) != 1 || lib.Nsfw[0].FilePath != old || lib.Nsfw[0].Status != nsfw.StatusDownloaded || lib.Nsfw[0].Wanted {
		t.Fatalf("library = %+v", lib.Nsfw)
	}
	if _, err := os.Stat(old); err != nil {
		t.Fatal("registering must not move the file")
	}

	// Check names: a preview that changes nothing.
	if code, body := doJSONStatus(t, admin, http.MethodPost, base+"/api/nsfw/rename/check", map[string]any{}); code != http.StatusAccepted {
		t.Fatalf("check = %d %v", code, body)
	}
	page = waitNsfwJob(t, admin, base+"/api/nsfw/rename")
	planID := int64(page["job"].(map[string]any)["planId"].(float64))
	if _, err := os.Stat(old); err != nil {
		t.Fatal("a check must not move the file")
	}
	var plan struct {
		Items []struct {
			ID      int64  `json:"id"`
			Status  string `json:"status"`
			OldName string `json:"oldName"`
			NewName string `json:"newName"`
			NewPath string `json:"newPath"`
		} `json:"items"`
	}
	planURL := fmt.Sprintf("%s/api/nsfw/rename/plans/%d", base, planID)
	if err := json.Unmarshal([]byte(videoGetRaw(t, admin, planURL)), &plan); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("Example Studio", "Example Studio - 2024-03-15 - Long Evening.mp4")
	if len(plan.Items) != 1 || plan.Items[0].Status != nsfw.ItemEligible || plan.Items[0].NewName != want {
		t.Fatalf("plan = %+v", plan.Items)
	}
	newPath := plan.Items[0].NewPath

	// Apply: renamed with its subtitle, and the library follows.
	if code, body := doJSONStatus(t, admin, http.MethodPost, planURL+"/apply", map[string]any{"items": []int64{plan.Items[0].ID}}); code != http.StatusAccepted {
		t.Fatalf("apply = %d %v", code, body)
	}
	waitNsfwJob(t, admin, base+"/api/nsfw/rename")
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("not renamed: %v", err)
	}
	if b, err := os.ReadFile(strings.TrimSuffix(newPath, ".mp4") + ".en.srt"); err != nil || string(b) != "subtitle" {
		t.Fatalf("the subtitle should move along: %v", err)
	}
	if err := json.Unmarshal([]byte(videoGetRaw(t, admin, base+"/api/nsfw")), &lib); err != nil {
		t.Fatal(err)
	}
	if lib.Nsfw[0].FilePath != newPath {
		t.Fatalf("file path = %s", lib.Nsfw[0].FilePath)
	}

	// Undo: all of it back.
	if code, body := doJSONStatus(t, admin, http.MethodPost, planURL+"/undo", map[string]any{}); code != http.StatusAccepted {
		t.Fatalf("undo = %d %v", code, body)
	}
	waitNsfwJob(t, admin, base+"/api/nsfw/rename")
	for _, p := range []string{old, srt} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s should be back: %v", p, err)
		}
	}
	if _, err := os.Stat(newPath); err == nil {
		t.Fatal("the new name should be gone")
	}
	if err := json.Unmarshal([]byte(videoGetRaw(t, admin, base+"/api/nsfw")), &lib); err != nil {
		t.Fatal(err)
	}
	if lib.Nsfw[0].FilePath != old {
		t.Fatalf("file path = %s", lib.Nsfw[0].FilePath)
	}
	if code, _ := doJSONStatus(t, admin, http.MethodPost, planURL+"/undo", map[string]any{}); code != http.StatusBadRequest {
		t.Fatalf("second undo = %d", code)
	}
}

func TestNsfwFavoritesOnlyAddFollows(t *testing.T) {
	_, base, admin, _, _ := familyServer(t)
	stash := newFakeToolStash(t)
	if code, body := doJSONStatus(t, admin, http.MethodPut, base+"/api/nsfw/settings", map[string]any{"enabled": true, "path": t.TempDir(), "endpoint": stash.srv.URL, "key": "good-key"}); code != http.StatusOK {
		t.Fatalf("settings = %d %v", code, body)
	}

	// Off by default.
	if code, body := doStatus(t, admin, http.MethodGet, base+"/api/nsfw/favorites"); code != http.StatusOK || body["auto"] != false {
		t.Fatalf("favorites = %d %v", code, body)
	}

	// The preview shows what would be added and adds nothing.
	code, body := doJSONStatus(t, admin, http.MethodPost, base+"/api/nsfw/favorites/preview", map[string]any{})
	if code != http.StatusOK {
		t.Fatalf("preview = %d %v", code, body)
	}
	if got := body["performers"].(map[string]any)["toFollow"].([]any); len(got) != 2 {
		t.Fatalf("performers = %v", got)
	}
	if got := body["studios"].(map[string]any)["toFollow"].([]any); len(got) != 1 {
		t.Fatalf("studios = %v", got)
	}
	if raw := videoGetRaw(t, admin, base+"/api/nsfw/performers"); strings.Contains(raw, "fav-p1") {
		t.Fatalf("a preview follows nobody: %s", raw)
	}

	// Choosing one follows only that one. A name that isn't a favorite is ignored.
	code, body = doJSONStatus(t, admin, http.MethodPost, base+"/api/nsfw/favorites/apply", map[string]any{"performers": []string{"fav-p1", "someone-else"}, "studios": []string{"fav-s1"}})
	if code != http.StatusOK || body["performers"] != float64(1) || body["studios"] != float64(1) {
		t.Fatalf("apply = %d %v", code, body)
	}
	raw := videoGetRaw(t, admin, base+"/api/nsfw/performers")
	if !strings.Contains(raw, "fav-p1") || strings.Contains(raw, "fav-p2") || strings.Contains(raw, "someone-else") {
		t.Fatalf("followed: %s", raw)
	}
	if raw := videoGetRaw(t, admin, base+"/api/nsfw/studios"); !strings.Contains(raw, "fav-s1") {
		t.Fatalf("studios: %s", raw)
	}

	// No video was added, and StashDB wasn't asked for any.
	var lib struct {
		Counts nsfw.Counts  `json:"counts"`
		Nsfw   []nsfw.Video `json:"nsfw"`
	}
	if err := json.Unmarshal([]byte(videoGetRaw(t, admin, base+"/api/nsfw")), &lib); err != nil {
		t.Fatal(err)
	}
	if len(lib.Nsfw) != 0 || lib.Counts.Wanted != 0 {
		t.Fatalf("no wanted videos: %+v", lib)
	}
	if n := stash.asked("queryScenes"); n != 0 {
		t.Fatalf("no catalogue may be read: %d", n)
	}
	if n := stash.asked("mutation"); n != 0 {
		t.Fatalf("nothing may be written to StashDB: %d", n)
	}
	// Nothing went into the download line.
	var queue []any
	if err := json.Unmarshal([]byte(videoGetRaw(t, admin, base+"/api/queue")), &queue); err == nil && len(queue) != 0 {
		t.Fatalf("queue = %v", queue)
	}

	// Stopping a follow sticks, however favorite they stay.
	var followed []nsfw.Performer
	if err := json.Unmarshal([]byte(videoGetRaw(t, admin, base+"/api/nsfw/performers")), &followed); err != nil || len(followed) != 1 {
		t.Fatalf("followed = %+v, %v", followed, err)
	}
	if code, _ := doStatus(t, admin, http.MethodDelete, fmt.Sprintf("%s/api/nsfw/performers/%d", base, followed[0].ID)); code != http.StatusNoContent {
		t.Fatalf("unfollow = %d", code)
	}
	code, body = doJSONStatus(t, admin, http.MethodPost, base+"/api/nsfw/favorites/preview", map[string]any{})
	if code != http.StatusOK {
		t.Fatalf("preview = %d %v", code, body)
	}
	side := body["performers"].(map[string]any)
	if len(side["leftAlone"].([]any)) != 1 || len(side["toFollow"].([]any)) != 1 {
		t.Fatalf("an unfollowed favorite is left alone: %v", side)
	}
	code, body = doJSONStatus(t, admin, http.MethodPost, base+"/api/nsfw/favorites/apply", map[string]any{"performers": []string{"fav-p1"}})
	if code != http.StatusOK || body["performers"] != float64(0) {
		t.Fatalf("apply = %d %v", code, body)
	}

	// The daily switch.
	if code, body := doJSONStatus(t, admin, http.MethodPut, base+"/api/nsfw/favorites/auto", map[string]any{"on": true}); code != http.StatusOK || body["auto"] != true {
		t.Fatalf("auto = %d %v", code, body)
	}
}

func TestNsfwToolsAreForAdministrators(t *testing.T) {
	_, base, admin, member, _ := familyServer(t)
	stash := newFakeToolStash(t)
	routes := [][2]string{
		{http.MethodPost, "/api/nsfw/tools/stop"}, {http.MethodGet, "/api/nsfw/identify"}, {http.MethodPost, "/api/nsfw/identify/start"}, {http.MethodPost, "/api/nsfw/identify/confirm"},
		{http.MethodGet, "/api/nsfw/favorites"}, {http.MethodPut, "/api/nsfw/favorites/auto"}, {http.MethodPost, "/api/nsfw/favorites/preview"}, {http.MethodPost, "/api/nsfw/favorites/apply"},
		{http.MethodGet, "/api/nsfw/rename"}, {http.MethodPost, "/api/nsfw/rename/check"}, {http.MethodGet, "/api/nsfw/rename/plans/1"}, {http.MethodPost, "/api/nsfw/rename/plans/1/apply"}, {http.MethodPost, "/api/nsfw/rename/plans/1/undo"},
	}
	// While the module is off, even an administrator gets nothing.
	for _, rt := range routes {
		if code, _ := doJSONStatus(t, admin, rt[0], base+rt[1], map[string]any{}); code != http.StatusNotFound {
			t.Errorf("%s %s with the module off = %d", rt[0], rt[1], code)
		}
	}
	if code, body := doJSONStatus(t, admin, http.MethodPut, base+"/api/nsfw/settings", map[string]any{"enabled": true, "path": t.TempDir(), "endpoint": stash.srv.URL, "key": "good-key"}); code != http.StatusOK {
		t.Fatalf("settings = %d %v", code, body)
	}
	for _, rt := range routes {
		if code, _ := doJSONStatus(t, member, rt[0], base+rt[1], map[string]any{}); code != http.StatusForbidden {
			t.Errorf("%s %s as a member = %d", rt[0], rt[1], code)
		}
	}
}
