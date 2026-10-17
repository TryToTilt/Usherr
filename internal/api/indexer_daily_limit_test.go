package api_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A person can set how many requests a day an indexer is used for; the
// indexer's card gets the count, and a bad number is refused.
func TestIndexerRequestsPerDay(t *testing.T) {
	_, base, client := loginNewServer(t)
	idx := newTVIndexerWith(t, nil)

	created := postJSON[map[string]any](t, client, base+"/api/indexers", map[string]any{
		"name": "Slug", "definitionId": "fixture", "baseUrl": idx.URL, "apiKey": "k", "requestsPerDay": 25,
	}, http.StatusCreated)
	id := int64(created["id"].(float64))
	if created["requestsPerDay"] != float64(25) || created["dailyLimit"] != float64(25) || created["limitLearned"] != nil {
		t.Fatalf("the set limit should show as the limit in use: %+v", created)
	}
	if note, _ := created["limitNote"].(string); note != "" {
		t.Fatalf("an indexer with room left has no limit note, got %q", note)
	}

	url := fmt.Sprintf("%s/api/indexers/%d", base, id)
	bad := putJSONStatus(t, client, url, map[string]any{"requestsPerDay": -3}, http.StatusBadRequest)
	if msg, _ := bad["error"].(string); !strings.Contains(msg, "Requests per day") {
		t.Fatalf("expected a plain message about requests per day, got %+v", bad)
	}
	postJSON[map[string]any](t, client, base+"/api/indexers", map[string]any{
		"name": "Other", "definitionId": "fixture", "baseUrl": idx.URL, "apiKey": "k", "requestsPerDay": 9999999,
	}, http.StatusBadRequest)

	// a change that leaves the field out keeps it
	kept := putJSONStatus(t, client, url, map[string]any{"name": "Slug 2"}, http.StatusOK)
	if kept["requestsPerDay"] != float64(25) {
		t.Fatalf("leaving the field out should keep the limit: %+v", kept)
	}
	changed := putJSONStatus(t, client, url, map[string]any{"requestsPerDay": 0}, http.StatusOK)
	if changed["requestsPerDay"] != float64(0) || changed["dailyLimit"] != nil {
		t.Fatalf("0 should remove the limit: %+v", changed)
	}
}

// A search that is answered with "Request limit reached" is learned from: the
// limit shows on the indexer's card, and the indexer is not paused.
func TestIndexerLearnsDailyLimitFromTheTestButton(t *testing.T) {
	_, base, client := loginNewServer(t)
	slug := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<?xml version="1.0"?><error code="429" description="Request limit reached (26/25)"/>`)
	}))
	t.Cleanup(slug.Close)
	created := postJSON[map[string]any](t, client, base+"/api/indexers", map[string]any{
		"name": "Slug", "definitionId": "fixture", "baseUrl": slug.URL, "apiKey": "k",
	}, http.StatusCreated)
	id := int64(created["id"].(float64))

	res := postJSON[map[string]any](t, client, fmt.Sprintf("%s/api/indexers/%d/test", base, id), nil, http.StatusOK)
	if res["ok"] == true {
		t.Fatalf("the test should report the limit: %+v", res)
	}
	list := getJSON[[]map[string]any](t, client, base+"/api/indexers")
	var card map[string]any
	for _, c := range list {
		if c["name"] == "Slug" {
			card = c
		}
	}
	if card == nil {
		t.Fatalf("indexer not listed: %+v", list)
	}
	if card["dailyLimit"] != float64(25) || card["limitLearned"] != true {
		t.Fatalf("the limit should be learned: %+v", card)
	}
	if card["limitNote"] != "Daily limit of 25 reached. It is used again after midnight (UTC)." {
		t.Fatalf("unexpected note: %v", card["limitNote"])
	}
	if card["pausedUntil"] != nil || card["failures"] != nil {
		t.Fatalf("a daily limit is not a failure and does not pause: %+v", card)
	}
	if card["requestsToday"].(float64) < 25 {
		t.Fatalf("the day's count should be at least the limit: %+v", card)
	}
}
