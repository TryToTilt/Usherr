package api_test

import (
	"fmt"
	"net/http"
	"testing"
)

// A watchlist that adds new titles stops doing so when its account is turned
// off by an administrator.
func TestPlexWatchlistSkipsTurnedOffAccounts(t *testing.T) {
	server, base, admin, member, memberID := familyServer(t)
	server.TestSetTMDBBaseURL("fixture-key", arrTMDB(t).URL)
	plex := newFakePlexWatchlist(t)
	server.TestSetPlexTVURL(plex.URL)
	server.TestSetPlexDiscoverURL(plex.URL)
	connectWatchlist(t, member, base)

	postJSONMethod[map[string]any](t, admin, http.MethodPut, fmt.Sprintf("%s/api/users/%v", base, memberID), map[string]any{"disabled": true}, http.StatusOK)
	plex.put("new1", "show", "Salt & Static", 2019, "tvdb://777001", "tmdb://9001")
	server.TestRunWatchlistJob()

	series := getJSON[[]map[string]any](t, admin, base+"/api/series")
	if len(series) != 0 {
		t.Fatalf("a turned-off account's watchlist added a show: %v", series)
	}
	reqs := getJSON[map[string]any](t, admin, base+"/api/requests")["requests"].([]any)
	if len(reqs) != 0 {
		t.Fatalf("a turned-off account's watchlist filed a request: %v", reqs)
	}
}
