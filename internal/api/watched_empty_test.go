package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/mediaservers"
)

// A media server that answers with no libraries (its drive is not mounted
// yet) must not wipe what was recorded, and must not count as "nobody
// watched anything".
func TestWatchedSyncIgnoresServerWithNoLibraries(t *testing.T) {
	server, base, client := loginNewServer(t)
	good, err := server.MediaServers.Create(mediaservers.Server{Name: "Plex", Kind: mediaservers.KindPlex, BaseURL: fakeWatchPlex(t).URL, Token: "tok", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	m, err := server.MovieRepo.Add(library.Movie{TMDBID: 603, Title: "The Matrix", Year: 1999, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/watched/settings", map[string]any{"sync": true}, http.StatusOK)
	postJSON[map[string]any](t, client, base+"/api/watched/sync", nil, http.StatusOK)
	if rows, _ := server.MovieRepo.ListWatched(); len(rows) == 0 {
		t.Fatal("the first sync recorded nothing")
	}

	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		c := map[string]any{}
		if r.URL.Path == "/library/sections" {
			c = map[string]any{"Directory": []map[string]any{}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"MediaContainer": c})
	}))
	t.Cleanup(empty.Close)
	if err := server.MediaServers.Delete(good.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := server.MediaServers.Create(mediaservers.Server{Name: "Plex", Kind: mediaservers.KindPlex, BaseURL: empty.URL, Token: "tok", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	postJSON[map[string]any](t, client, base+"/api/watched/sync", nil, http.StatusBadGateway)
	rows, _ := server.MovieRepo.ListWatched()
	found := false
	for _, r := range rows {
		if r.Kind == "movie" && r.TitleID == m.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("an empty answer wiped the recorded plays: %v", rows)
	}
}
