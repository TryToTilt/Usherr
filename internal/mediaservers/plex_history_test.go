package mediaservers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A Plex household: who watches, and what one of them finished, with each
// play's film or show (and its TMDB id) looked up. A play of something no
// longer in the libraries is left out; the history comes a page at a time.
func TestPlexAccountsAndHistory(t *testing.T) {
	var asked []string
	meta := map[string]map[string]any{
		"10": {"ratingKey": "10", "type": "movie", "title": "Arrival", "year": 2016, "duration": 6_960_000, "Guid": []map[string]any{{"id": "tmdb://329865"}}},
		"21": {"ratingKey": "21", "type": "episode", "title": "Pilot", "grandparentRatingKey": "77", "parentIndex": 1, "index": 1, "duration": 3_000_000},
		"22": {"ratingKey": "22", "type": "episode", "title": "Two", "grandparentRatingKey": "77", "parentIndex": 1, "index": 2, "duration": 3_000_000},
		"77": {"ratingKey": "77", "type": "show", "title": "Severance", "year": 2022, "Guid": []map[string]any{{"id": "tmdb://95396"}}},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Plex-Token") != "tok" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		asked = append(asked, r.URL.Path+"?"+r.URL.RawQuery)
		w.Header().Set("Content-Type", "application/json")
		send := func(c map[string]any) { _ = json.NewEncoder(w).Encode(map[string]any{"MediaContainer": c}) }
		switch {
		case r.URL.Path == "/accounts":
			send(map[string]any{"Account": []map[string]any{{"id": 0, "name": ""}, {"id": 1, "name": "Chris"}, {"id": 5, "name": "Sam"}}})
		case r.URL.Path == "/status/sessions/history/all":
			if r.URL.Query().Get("accountID") != "5" {
				send(map[string]any{})
				return
			}
			// Two pages of plays (the page size is cut to 2 for the test),
			// newest first; 99 is a film since removed.
			all := []map[string]any{
				{"ratingKey": "22", "type": "episode", "viewedAt": 1_761_900_000},
				{"ratingKey": "99", "type": "movie", "viewedAt": 1_761_800_000},
				{"ratingKey": "10", "type": "movie", "viewedAt": 1_761_700_000},
				{"ratingKey": "21", "type": "episode", "viewedAt": 1_761_600_000},
			}
			start, _ := strconv.Atoi(r.URL.Query().Get("X-Plex-Container-Start"))
			start = min(start, len(all))
			send(map[string]any{"Metadata": all[start:min(start+2, len(all))]})
		case strings.HasPrefix(r.URL.Path, "/library/metadata/"):
			var found []map[string]any
			for _, k := range strings.Split(strings.TrimPrefix(r.URL.Path, "/library/metadata/"), ",") {
				if m, ok := meta[k]; ok {
					found = append(found, m)
				}
			}
			if len(found) == 0 {
				http.NotFound(w, r)
				return
			}
			send(map[string]any{"Metadata": found})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := NewClient("test")
	s := Server{ID: 3, Name: "Plex", Kind: KindPlex, BaseURL: srv.URL, Token: "tok", Enabled: true}

	accounts, err := c.PlexAccounts(context.Background(), s)
	if err != nil || len(accounts) != 2 || accounts[0].Name != "Chris" || !accounts[0].Owner || accounts[1].ID != 5 || accounts[1].Owner {
		t.Fatalf("accounts: %+v, %v", accounts, err)
	}

	old := plexHistoryPageSize
	plexHistoryPageSize = 2
	defer func() { plexHistoryPageSize = old }()
	plays, err := c.PlexHistory(context.Background(), s, 5, time.Time{})
	if err != nil || len(plays) != 3 {
		t.Fatalf("history: %+v, %v", plays, err)
	}
	if p := plays[0]; p.Kind != "episode" || p.TMDBID != 95396 || p.Title != "Severance" || p.Year != 2022 || p.Season != 1 || p.Episode != 2 || p.Minutes != 50 || p.WatchedAt.Unix() != 1_761_900_000 {
		t.Errorf("newest: %+v", p)
	}
	if p := plays[1]; p.Kind != "movie" || p.TMDBID != 329865 || p.Title != "Arrival" || p.Year != 2016 || p.Minutes != 116 {
		t.Errorf("film: %+v", p)
	}
	if p := plays[2]; p.Episode != 1 || p.TMDBID != 95396 {
		t.Errorf("oldest: %+v", p)
	}

	// Since a time: only newer plays are asked for.
	asked = nil
	if _, err := c.PlexHistory(context.Background(), s, 5, time.Unix(1_761_650_000, 0)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(asked[0], "viewedAt%3E=1761650000") {
		t.Errorf("since not asked for: %s", asked[0])
	}
}
