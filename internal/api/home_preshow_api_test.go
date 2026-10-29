package api_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"testing"
)

// The pre-show: ready-made themes, a theme of one's own, a schedule that
// picks it, and the plan for a film.
func TestPreshowPlan(t *testing.T) {
	_, base, client := loginNewServer(t)
	all := getJSON[map[string]any](t, client, base+"/api/home/preshow")
	if themes := all["themes"].([]any); len(themes) != 2 || themes[0].(map[string]any)["name"] != "Movie night" {
		t.Fatalf("ready-made themes: %v", themes)
	}
	st, body := doJSONStatus(t, client, http.MethodPost, base+"/api/home/preshow/themes", map[string]any{
		"name": "Short", "blocks": []map[string]any{{"type": "card", "card": "silence"}, {"type": "clips", "category": "Bumpers", "count": 2}, {"type": "card", "card": "feature"}},
	})
	if st != http.StatusCreated {
		t.Fatalf("theme: %d %v", st, body)
	}
	id := int64(body["id"].(float64))
	if st, _ := doJSONStatus(t, client, http.MethodPost, base+"/api/home/preshow/themes", map[string]any{"name": "Bad", "blocks": []map[string]any{{"type": "card", "card": "nope"}}}); st != http.StatusBadRequest {
		t.Fatalf("a card Usherr doesn't draw: %d", st)
	}

	// Asked for by name: its cards (no clips in the folder yet).
	plan := getJSON[map[string]any](t, client, base+"/api/home/preshow/plan?kind=movie&dry=1&theme="+itoa(id))
	steps := plan["steps"].([]any)
	if len(steps) != 2 || steps[0].(map[string]any)["card"] != "silence" || plan["seconds"] != float64(14.5) {
		t.Fatalf("plan: %v", plan)
	}

	// A schedule for every day makes it the one that plays.
	if st, body := doJSONStatus(t, client, http.MethodPost, base+"/api/home/preshow/schedules", map[string]any{
		"name": "Always", "themeId": id, "enabled": true, "rule": map[string]any{"kind": "weekly", "days": []int{0, 1, 2, 3, 4, 5, 6}},
	}); st != http.StatusCreated {
		t.Fatalf("schedule: %d %v", st, body)
	}
	plan = getJSON[map[string]any](t, client, base+"/api/home/preshow/plan?kind=movie&dry=1")
	if plan["theme"].(map[string]any)["name"] != "Short" {
		t.Fatalf("scheduled theme: %v", plan["theme"])
	}
	if st, _ := doJSONStatus(t, client, http.MethodPost, base+"/api/home/preshow/schedules", map[string]any{
		"name": "Never", "themeId": id, "rule": map[string]any{"kind": "weekly"},
	}); st != http.StatusBadRequest {
		t.Fatalf("a week with no days: %d", st)
	}
	names := getJSON[map[string]any](t, client, base+"/api/home/preshow/themes")
	if len(names["themes"].([]any)) != 3 || names["active"] != float64(id) {
		t.Fatalf("theme names: %v", names)
	}
}

// The Jellyfin app's pre-show: the plugin's calls need Usherr's key.
func TestPreshowJellyfinPlugin(t *testing.T) {
	_, base, client := loginNewServer(t)
	jf := getJSON[map[string]any](t, client, base+"/api/home/preshow/jellyfin")
	key, _ := jf["key"].(string)
	if len(key) != 32 || jf["connected"] != false || jf["movies"] != true || jf["episodes"] != false {
		t.Fatalf("state: %v", jf)
	}
	ask := func(k string) (int, map[string]any) {
		req, _ := http.NewRequest(http.MethodGet, base+"/plugin/intros?media_type=Movie&item_id=abc", nil)
		req.Header.Set("X-Api-Key", k)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	if st, _ := ask("nope"); st != http.StatusUnauthorized {
		t.Fatalf("without the key: %d", st)
	}
	// No Jellyfin connected: nothing to play, but a proper answer.
	if st, out := ask(key); st != http.StatusOK || out["Exact"] != true || len(out["Items"].([]any)) != 0 {
		t.Fatalf("with the key: %d %v", st, out)
	}
	if resp, err := http.Get(base + "/plugin/stream/card-0123-bad"); err != nil || resp.StatusCode != http.StatusNotFound {
		t.Fatalf("a link not signed: %v %v", resp, err)
	}
	// Episodes on, and a new key: the old one stops working.
	if st, body := doJSONStatus(t, client, http.MethodPut, base+"/api/home/preshow/jellyfin", map[string]any{"address": "http://localhost:8264", "movies": true, "episodes": true}); st != http.StatusOK || body["episodes"] != true {
		t.Fatalf("save: %d %v", st, body)
	}
	if st, _ := doJSONStatus(t, client, http.MethodPut, base+"/api/home/preshow/jellyfin", map[string]any{"address": "ftp://x"}); st != http.StatusBadRequest {
		t.Fatalf("a bad address: %d", st)
	}
	st, body := doJSONStatus(t, client, http.MethodPost, base+"/api/home/preshow/jellyfin/key", nil)
	if st != http.StatusOK || body["key"] == key {
		t.Fatalf("new key: %d %v", st, body)
	}
	if st, _ := ask(key); st != http.StatusUnauthorized {
		t.Fatalf("the old key: %d", st)
	}
}

// An uploaded clip (written whole under a hidden name, then named) is listed
// at once.
func TestPreshowUpload(t *testing.T) {
	_, base, client := loginNewServer(t)
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("category", "Bumpers")
	part, _ := mw.CreateFormFile("file", "Logo.mp4")
	part.Write(bytes.Repeat([]byte{0}, 4096))
	mw.Close()
	req, _ := http.NewRequest(http.MethodPost, base+"/api/home/preshow/clips", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload: %d", resp.StatusCode)
	}
	all := getJSON[map[string]any](t, client, base+"/api/home/preshow")
	clips := all["clips"].([]any)
	if len(clips) != 1 || clips[0].(map[string]any)["path"] != "Bumpers/Logo.mp4" {
		t.Fatalf("clips: %v", clips)
	}
}
