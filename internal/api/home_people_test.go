package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/mediaservers"
)

func TestPersonPartsAndMerge(t *testing.T) {
	people := mergePeople([]jfPerson{
		{ID: "abc", Name: "Taylor", IsAdmin: true},
		{ID: "def", Name: "Sam"},
		{ID: "plex:3:1", Name: "taylor", IsAdmin: true},
		{ID: "plex:3:7", Name: "Robin"},
	})
	if len(people) != 3 || people[0].ID != "abc,plex:3:1" || people[2].ID != "def" || people[1].ID != "plex:3:7" {
		t.Fatalf("merged: %+v", people)
	}
	jf, plex := personParts("abc,plex:3:1")
	if jf != "abc" || len(plex) != 1 || plex[0] != (plexPersonRef{3, 1}) {
		t.Errorf("parts: %q %+v", jf, plex)
	}
	if jf, plex := personParts("plex:3:7"); jf != "" || len(plex) != 1 {
		t.Errorf("plex only: %q %+v", jf, plex)
	}
	if jfViewer(people) != "abc" {
		t.Errorf("viewer: %q", jfViewer(people))
	}
}

// fakeHouseholdPlex is a Plex server where Sam (account 7) watched an
// episode of Severance and Arrival.
func fakeHouseholdPlex(t *testing.T) *httptest.Server {
	t.Helper()
	meta := map[string]map[string]any{
		"10": {"ratingKey": "10", "type": "movie", "title": "Arrival", "year": 2016, "duration": 6_960_000, "Guid": []map[string]any{{"id": "tmdb://329865"}}},
		"21": {"ratingKey": "21", "type": "episode", "title": "Pilot", "grandparentRatingKey": "77", "parentIndex": 1, "index": 3, "duration": 3_000_000},
		"77": {"ratingKey": "77", "type": "show", "title": "Severance", "year": 2022, "Guid": []map[string]any{{"id": "tmdb://95396"}}},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var c map[string]any
		switch {
		case r.URL.Path == "/accounts":
			c = map[string]any{"Account": []map[string]any{{"id": 1, "name": "Owner"}, {"id": 7, "name": "Sam"}}}
		case r.URL.Path == "/status/sessions/history/all":
			c = map[string]any{}
			if r.URL.Query().Get("accountID") == "7" {
				var plays []map[string]any
				after, _ := strconv.ParseInt(r.URL.Query().Get("viewedAt>"), 10, 64)
				for _, p := range []map[string]any{
					{"ratingKey": "21", "type": "episode", "viewedAt": 1_761_900_000},
					{"ratingKey": "10", "type": "movie", "viewedAt": 1_761_700_000},
				} {
					if p["viewedAt"].(int) > int(after) {
						plays = append(plays, p)
					}
				}
				c["Metadata"] = plays
			}
		case strings.HasPrefix(r.URL.Path, "/library/metadata/"):
			var found []map[string]any
			for _, k := range strings.Split(strings.TrimPrefix(r.URL.Path, "/library/metadata/"), ",") {
				if m, ok := meta[k]; ok {
					found = append(found, m)
				}
			}
			c = map[string]any{"Metadata": found}
		default:
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"MediaContainer": c})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// A Plex household's account gets its Diary, Because you watched and
// Catch Me Up from Plex, with no Jellyfin at all.
func TestPlexHouseholdWatching(t *testing.T) {
	server, db := optionsServer(t)
	server.db = db
	server.Auth = auth.New(db)
	box, err := crypto.LoadOrCreateKey(filepath.Join(t.TempDir(), "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	server.MediaServers = mediaservers.NewRepo(db, box) // tokens are kept encrypted
	server.mediaClient = mediaservers.NewClient("test")
	plexSrv, err := server.MediaServers.Create(mediaservers.Server{Name: "Plex", Kind: mediaservers.KindPlex, BaseURL: fakeHouseholdPlex(t).URL, Token: "tok", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	sam, err := server.Auth.CreateUser("sam", "another-long-password")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	people, err := server.jfPeople(ctx)
	if err != nil || len(people) != 2 {
		t.Fatalf("people: %+v, %v", people, err)
	}
	person := personOfUser(people, sam)
	if person != plexPersonID(plexSrv.ID, 7) {
		t.Fatalf("Sam is %q", person)
	}

	// Sam's Diary was read a moment ago, before Usherr kept who it read:
	// linking Plex reads the whole Plex history anyway.
	if _, err := db.Exec(`INSERT INTO home_diary_sync (user_id, synced_at) VALUES (?, ?)`, sam.ID, time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if err := server.syncDiary(ctx, sam); err != nil {
		t.Fatal(err)
	}
	list, err := server.diaryEntries(ctx, sam.ID, "")
	if err != nil || len(list) != 2 {
		t.Fatalf("diary: %+v, %v", list, err)
	}
	for _, e := range list {
		if e.Source != "plex" {
			t.Errorf("source: %+v", e)
		}
		if e.Kind == "tv" && (e.TMDBID != 95396 || e.Episode != "S1E3" || e.Runtime != 50) {
			t.Errorf("episode entry: %+v", e)
		}
		if e.Kind == "movie" && (e.TMDBID != 329865 || e.Runtime != 116 || e.Year != 2016) {
			t.Errorf("film entry: %+v", e)
		}
	}

	wa, err := server.watchingOf(ctx, person)
	if err != nil || len(wa.recentMovies) != 1 || wa.recentMovies[0] != 329865 || len(wa.recentShows) != 1 || wa.recentShows[0] != 95396 {
		t.Fatalf("watching: %+v, %v", wa, err)
	}
	if se, ep := server.lastWatched(ctx, person, 95396); se != 1 || ep != 3 {
		t.Errorf("Catch Me Up from S%dE%d; want S1E3", se, ep)
	}
	if !server.hasMediaServerPerson(ctx, sam) {
		t.Error("the Diary should say Sam watches in a media server")
	}
}
