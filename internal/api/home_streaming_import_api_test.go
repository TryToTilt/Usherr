package api_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestImportFromStreamNZB(t *testing.T) {
	_, base, admin := loginNewServer(t)
	cfg := `{"indexers":[
	  {"name":"Prowlarr-NZBgeek","url":"http://prowlarr:9696","api_key":"SECRET-KEY-1","api_path":"1/api","type":"aggregator","enabled":true}],
	 "providers":[
	  {"name":"Newshosting","host":"news.newshosting.com","port":563,"username":"u1","password":"SECRET-PASS-1","connections":20,"use_ssl":true,"priority":2,"enabled":true},
	  {"name":"Backup","host":"news.backup.example","port":563,"username":"u2","password":"SECRET-PASS-2","connections":8,"use_ssl":true,"priority":1,"enabled":true,"backup":true}],
	 "filter_profiles":[{"name":"Phone","preset":"720p","rules":[{"when":"sizeGB > 9","action":"reject"}]}]}`

	// The preview lists what is there and shows no login.
	pre := postJSON[map[string]any](t, admin, base+"/api/home/streaming/import/preview", map[string]any{"config": cfg}, http.StatusOK)
	raw, _ := json.Marshal(pre)
	for _, secret := range []string{"SECRET-KEY-1", "SECRET-PASS-1", "SECRET-PASS-2"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("the preview sent a login: %s", raw)
		}
	}
	if len(pre["providers"].([]any)) != 2 || len(pre["indexers"].([]any)) != 1 || len(pre["profiles"].([]any)) != 1 {
		t.Fatalf("preview: %s", raw)
	}

	// Only what is ticked is copied.
	res := postJSON[map[string]any](t, admin, base+"/api/home/streaming/import", map[string]any{
		"config": cfg, "providers": []string{"Newshosting"}, "indexers": []string{"Prowlarr-NZBgeek"}, "profiles": true,
	}, http.StatusOK)
	if got := len(res["imported"].([]any)); got != 3 {
		t.Fatalf("imported %d things: %v", got, res)
	}
	servers := getJSON[[]map[string]any](t, admin, base+"/api/usenet-servers")
	if len(servers) != 1 || servers[0]["host"] != "news.newshosting.com" || servers[0]["hasPassword"] != true {
		t.Fatalf("servers: %v", servers)
	}
	idx := getJSON[[]map[string]any](t, admin, base+"/api/indexers")
	if len(idx) != 1 || idx[0]["baseUrl"] != "http://prowlarr:9696/1/api" {
		t.Fatalf("indexers: %v", idx)
	}
	prof := getJSON[map[string]any](t, admin, base+"/api/home/streaming")["profiles"].([]any)
	found := false
	for _, p := range prof {
		if m := p.(map[string]any); m["quality"] == "720p" && m["maxSizeGB"] == float64(9) {
			found = true
		}
	}
	if !found {
		t.Fatalf("the 720p size limit wasn't imported: %v", prof)
	}

	// Again: what is there already is left alone, and the preview says so.
	pre = postJSON[map[string]any](t, admin, base+"/api/home/streaming/import/preview", map[string]any{"config": cfg}, http.StatusOK)
	if !pre["providers"].([]any)[0].(map[string]any)["existing"].(bool) || !pre["indexers"].([]any)[0].(map[string]any)["existing"].(bool) {
		t.Fatalf("existing ones not marked: %v", pre)
	}
	res = postJSON[map[string]any](t, admin, base+"/api/home/streaming/import", map[string]any{"config": cfg, "providers": []string{"Newshosting"}, "indexers": []string{"Prowlarr-NZBgeek"}}, http.StatusOK)
	if len(res["imported"].([]any)) != 0 || len(res["skipped"].([]any)) != 2 {
		t.Fatalf("a second import copied again: %v", res)
	}
	if n := len(getJSON[[]map[string]any](t, admin, base+"/api/usenet-servers")); n != 1 {
		t.Fatalf("%d servers after a second import", n)
	}

	// Not StreamNZB's file.
	if st, _ := doJSONStatus(t, admin, http.MethodPost, base+"/api/home/streaming/import/preview", map[string]any{"config": "nope"}); st != http.StatusBadRequest {
		t.Fatalf("a bad file answered %d", st)
	}
}
