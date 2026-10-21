package api_test

import (
	"net/http"
	"testing"
)

func TestStreamingEngineSetting(t *testing.T) {
	_, base, admin := loginNewServer(t)

	got := getJSON[map[string]any](t, admin, base+"/api/home/streaming")
	// A new install, with no StreamNZB connected, streams with Usherr's engine.
	if got["engine"] != "usherr" {
		t.Fatalf("the default engine is %v, want usherr", got["engine"])
	}
	if ps, _ := got["profiles"].([]any); len(ps) != 3 {
		t.Fatalf("expected the three default profiles, got %v", got["profiles"])
	}

	if st, _ := doJSONStatus(t, admin, http.MethodPut, base+"/api/home/streaming", map[string]any{"engine": "plex"}); st != http.StatusBadRequest {
		t.Fatalf("an unknown engine answered %d, want 400", st)
	}
	putJSON(t, admin, base+"/api/home/streaming", map[string]any{"engine": "streamnzb"}, http.StatusOK)
	if got = getJSON[map[string]any](t, admin, base+"/api/home/streaming"); got["engine"] != "streamnzb" {
		t.Fatalf("engine = %v after choosing streamnzb", got["engine"])
	}
	putJSON(t, admin, base+"/api/home/streaming", map[string]any{"engine": "usherr"}, http.StatusOK)
	if got = getJSON[map[string]any](t, admin, base+"/api/home/streaming"); got["engine"] != "usherr" {
		t.Fatalf("engine = %v after choosing usherr", got["engine"])
	}

	// A profile is saved, and one that wasn't sent keeps its default.
	putJSON(t, admin, base+"/api/home/streaming", map[string]any{"profiles": []map[string]any{{"quality": "720p", "maxSizeGB": 3, "exclude": "av1,hdr"}}}, http.StatusOK)
	got = getJSON[map[string]any](t, admin, base+"/api/home/streaming")
	ps, _ := got["profiles"].([]any)
	byQ := map[string]map[string]any{}
	for _, p := range ps {
		m := p.(map[string]any)
		byQ[m["quality"].(string)] = m
	}
	if byQ["720p"]["maxSizeGB"] != float64(3) || byQ["720p"]["exclude"] != "av1,hdr" {
		t.Fatalf("720p profile not saved: %v", byQ["720p"])
	}
	if byQ["1080p"]["maxSizeGB"] != float64(20) {
		t.Fatalf("1080p lost its default: %v", byQ["1080p"])
	}
	if st, _ := doJSONStatus(t, admin, http.MethodPut, base+"/api/home/streaming", map[string]any{"profiles": []map[string]any{{"quality": "1080p", "maxSizeGB": 9999}}}); st != http.StatusBadRequest {
		t.Fatalf("an absurd size limit answered %d, want 400", st)
	}

	// Without a Usenet server or an indexer the own engine says what is missing.
	st := getJSON[map[string]any](t, admin, base+"/api/stream/status")
	if st["ready"] != false || st["problem"] == "" {
		t.Fatalf("stream status with nothing set up: %v", st)
	}
}
