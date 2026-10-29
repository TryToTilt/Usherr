package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// Sports stacked on Sportarr: connected with its key, the home lists its
// leagues, teams and games, search marks what's there, and adding a league,
// following a team and getting a game reach Sportarr in its own shapes.
func TestSportsOnSportarr(t *testing.T) {
	var mu sync.Mutex
	var got []string
	soon := time.Now().Add(26 * time.Hour).UTC().Format(time.RFC3339)
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "sk" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		mu.Lock()
		if r.Method != http.MethodGet {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			b, _ := json.Marshal(body)
			got = append(got, r.Method+" "+r.URL.Path+" "+string(b))
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/system/status":
			_, _ = w.Write([]byte(`{"appName":"Sportarr","version":"4.0"}`))
		case "/api/leagues":
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte(`[{"id":1,"externalId":"lg-1","name":"NFL","sport":"Football","monitored":true,"logoUrl":"https://sportarr.net/static/images/l.png","eventCount":655}]`))
			}
		case "/api/leagues/search/Premier League":
			_, _ = w.Write([]byte(`[{"idLeague":"lg-1","strLeague":"NFL","strSport":"Football"},{"idLeague":"lg-2","strLeague":"English Premier League","strSport":"Soccer","strBadge":"b.png"}]`))
		case "/api/teams/search/Premier League":
			_, _ = w.Write([]byte(`[{"idTeam":"tm-9","strTeam":"Arsenal","strSport":"Soccer"}]`))
		case "/api/followed-teams":
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte(`[{"id":4,"externalId":"tm-7","name":"Kansas City Chiefs","sport":"American Football"}]`))
			}
		case "/api/calendar":
			if r.URL.Query().Get("unmonitored") != "true" {
				t.Errorf("calendar without unmonitored games: %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`[{"id":306,"title":"Ravens vs Steelers","sport":"Football","leagueId":1,"leagueName":"NFL","eventDate":"` + soon + `","images":["https://sportarr.net/static/images/e.jpg"]}]`))
		case "/api/leagues/1/seasons":
			_, _ = w.Write([]byte(`{"totalEvents":655,"seasons":[{"season":"2026"},{"season":"2025"}]}`))
		case "/api/leagues/1/events":
			if r.URL.Query().Get("season") != "2026" {
				t.Errorf("events without the season: %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`[{"id":306,"title":"Ravens vs Steelers","leagueId":1,"eventDate":"` + soon + `","hasFile":true,"quality":"WEBDL-1080p","homeScore":"24","awayScore":17}]`))
		case "/api/qualityprofile":
			_, _ = w.Write([]byte(`[{"id":1},{"id":3,"isDefault":true}]`))
		case "/api/leagues/external/lg-2/teams":
			_, _ = w.Write([]byte(`[{"idTeam":"tm-9"},{"idTeam":"tm-10"}]`))
		case "/api/rootfolder":
			_, _ = w.Write([]byte(`[{"id":2,"path":"/sports"}]`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer fake.Close()

	_, base, client := loginNewServer(t)
	if home := getJSON[map[string]any](t, client, base+"/api/home/sports"); home["on"] != false {
		t.Fatalf("off at first: %v", home)
	}
	st, body := doJSONStatus(t, client, http.MethodPut, base+"/api/home/sports/settings", map[string]any{"mode": "sportarr", "sportarrUrl": fake.URL + "/", "sportarrKey": "sk"})
	if st != http.StatusOK || body["ready"] != true || body["sportarrKeySet"] != true || body["sportarrUrl"] != fake.URL {
		t.Fatalf("settings: %d %v", st, body)
	}
	if strings.Contains(mustJSON(t, body), "sk\"") {
		t.Fatalf("the key came back: %v", body)
	}
	if st, _ := doJSONStatus(t, client, http.MethodPut, base+"/api/home/sports/settings", map[string]any{"mode": "cricket"}); st != http.StatusBadRequest {
		t.Fatalf("unknown mode: %d", st)
	}

	home := getJSON[map[string]any](t, client, base+"/api/home/sports")
	if home["on"] != true || home["canManage"] != true || home["canStream"] != false || len(home["leagues"].([]any)) != 1 || len(home["teams"].([]any)) != 1 {
		t.Fatalf("home: %v", home)
	}
	g := home["games"].([]any)[0].(map[string]any)
	if g["title"] != "Ravens vs Steelers" || g["league"] != "NFL" || g["image"] != "https://sportarr.net/static/images/e.jpg" {
		t.Fatalf("game: %v", g)
	}

	found := getJSON[map[string]any](t, client, base+"/api/home/sports/search?q=Premier+League")
	ls := found["leagues"].([]any)
	if ls[0].(map[string]any)["inLibrary"] != true || ls[0].(map[string]any)["id"] != "1" || ls[1].(map[string]any)["inLibrary"] != false {
		t.Fatalf("search leagues: %v", ls)
	}
	if tm := found["teams"].([]any)[0].(map[string]any); tm["name"] != "Arsenal" || tm["followed"] != false {
		t.Fatalf("search teams: %v", tm)
	}

	lg := getJSON[map[string]any](t, client, base+"/api/home/sports/leagues/1")
	if games := lg["games"].([]any); len(games) != 1 || lg["season"] != "2026" || len(lg["seasons"].([]any)) != 2 ||
		games[0].(map[string]any)["hasFile"] != true || games[0].(map[string]any)["homeScore"] != float64(24) || games[0].(map[string]any)["awayScore"] != float64(17) {
		t.Fatalf("league: %v", lg)
	}
	if st, _ := doJSONStatus(t, client, http.MethodGet, base+"/api/home/sports/leagues/99", nil); st != http.StatusNotFound {
		t.Fatalf("unknown league: %d", st)
	}

	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/api/home/sports/leagues"},
		{http.MethodPost, "/api/home/sports/teams"},
		{http.MethodPost, "/api/home/sports/events/306/get"},
		{http.MethodDelete, "/api/home/sports/teams/4"},
	} {
		var b any
		switch c.path {
		case "/api/home/sports/leagues":
			b = map[string]any{"externalId": "lg-2", "name": "English Premier League", "sport": "Soccer", "monitor": true}
		case "/api/home/sports/teams":
			b = map[string]any{"externalId": "tm-9", "name": "Arsenal", "sport": "Soccer"}
		}
		if st, body := doJSONStatus(t, client, c.method, base+c.path, b); st != http.StatusOK && st != http.StatusNoContent {
			t.Fatalf("%s %s: %d %v", c.method, c.path, st, body)
		}
	}
	mu.Lock()
	all := strings.Join(got, "\n")
	mu.Unlock()
	for _, want := range []string{
		`POST /api/leagues {"country":"","description":"","externalId":"lg-2","monitorType":"Future","monitored":true,"monitoredTeamIds":["tm-9","tm-10"],"name":"English Premier League","qualityProfileId":3,"rootFolderId":2,`,
		`POST /api/followed-teams {"badgeUrl":"","externalId":"tm-9","name":"Arsenal","sport":"Soccer"}`,
		`PUT /api/events/306 {"monitored":true}`,
		`POST /api/event/306/automatic-search {}`,
		`DELETE /api/followed-teams/4`,
	} {
		if !strings.Contains(all, want) {
			t.Fatalf("Sportarr wasn't sent %s\ngot:\n%s", want, all)
		}
	}

	// Streaming needs Usherr's streaming engine (no Usenet server here).
	if st, body := doJSONStatus(t, client, http.MethodPost, base+"/api/home/sports/events/306/stream", map[string]any{}); st != http.StatusPreconditionFailed {
		t.Fatalf("stream without the engine: %d %v", st, body)
	}
	if st, _ := doJSONStatus(t, client, http.MethodPost, base+"/api/home/sports/events/306/streamed", map[string]any{"title": "Ravens vs Steelers", "league": "NFL"}); st != http.StatusNoContent {
		t.Fatalf("streamed: %d", st)
	}

	// Only sports artwork goes through the image route.
	if st, _ := doJSONStatus(t, client, http.MethodGet, base+"/api/home/sports/image?u="+fake.URL+"/x.png", nil); st != http.StatusNotFound {
		t.Fatalf("other image host: %d", st)
	}

	// A wrong key is said plainly.
	doJSONStatus(t, client, http.MethodPut, base+"/api/home/sports/settings", map[string]any{"sportarrKey": "nope"})
	if st, body := doJSONStatus(t, client, http.MethodPost, base+"/api/home/sports/test", nil); st != http.StatusBadGateway || !strings.Contains(body["error"].(string), "API key") {
		t.Fatalf("wrong key: %d %v", st, body)
	}
}
