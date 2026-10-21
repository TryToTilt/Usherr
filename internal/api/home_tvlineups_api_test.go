package api_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/api"
)

// A fake TV listings service: one over-the-air lineup and one cable lineup.
func fakeGracenote(t *testing.T) *httptest.Server {
	t.Helper()
	ev := `{"startTime":"2026-10-16T12:00:00Z","endTime":"2026-10-16T13:00:00Z","filter":[],"flag":[],"program":{"title":"A Show","tmsId":"EP1","season":"1","episode":"1","episodeTitle":"Pilot"}}`
	mux := http.NewServeMux()
	mux.HandleFunc("/gapzap_webapi/api/Providers/getPostalCodeProviders/USA/73107/gapzap/en-us", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"Providers":[
		 {"type":"CABLE","device":"X","lineupId":"USA-OK35543-DEFAULT","name":"Cox Communications - Digital","headendId":"OK35543"},
		 {"type":"OTA","device":"","lineupId":"USA-lineupId-DEFAULT","name":"Local Over the Air Broadcast","headendId":"lineupId"}]}`))
	})
	mux.HandleFunc("/api/grid", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("lineupId") == "USA-OK35543-DEFAULT" {
			if r.URL.Query().Get("headendId") != "OK35543" || r.URL.Query().Get("device") != "X" {
				t.Errorf("cable grid asked with headend %q device %q", r.URL.Query().Get("headendId"), r.URL.Query().Get("device"))
			}
			_, _ = w.Write([]byte(`{"channels":[
			 {"callSign":"ESPN","affiliateName":"","channelId":"c28","channelNo":"28","stationFilters":["filter-sports"],"events":[` + ev + `]},
			 {"callSign":"ESPNHD","affiliateName":"","channelId":"c29","channelNo":"29","stationFilters":["filter-sports"],"events":[` + ev + `]},
			 {"callSign":"HGTVD","affiliateName":"","channelId":"c58","channelNo":"58","stationFilters":[],"events":[` + ev + `]},
			 {"callSign":"EMPTY","affiliateName":"","channelId":"c99","channelNo":"99","stationFilters":[],"events":[]}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"channels":[
		 {"callSign":"KFORDT","affiliateName":"NATIONAL BROADCASTING COMPANY","channelId":"o4","channelNo":"4.1","stationFilters":["filter-news"],"events":[` + ev + `]},
		 {"callSign":"KFORDT2","affiliateName":"ANTENNA TV","channelId":"o42","channelNo":"4.2","stationFilters":["filter-movie"],"events":[` + ev + `]}]}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestRealTVLineups(t *testing.T) {
	fake := fakeGracenote(t)
	defer api.SetGracenoteBaseForTest(fake.URL)()
	_, base, admin := loginNewServer(t)

	provs := getJSON[map[string]any](t, admin, base+"/api/tv/replay/providers?zip=73107")["providers"].([]any)
	if len(provs) != 2 || provs[0].(map[string]any)["type"] != "OTA" || provs[1].(map[string]any)["id"] != "USA-OK35543-DEFAULT" {
		t.Fatalf("providers: %v", provs)
	}
	if st, _ := doJSONStatus(t, admin, http.MethodGet, base+"/api/tv/replay/providers?zip=12", nil); st != http.StatusBadRequest {
		t.Fatalf("a bad zip answered %d", st)
	}

	// Over the air: the market's own NBC is marked, subchannels come with their names and kinds (all local stations).
	ota := getJSON[map[string]any](t, admin, base+"/api/tv/replay/lineup?zip=73107&date=2026-10-16")["channels"].([]any)
	if len(ota) != 2 {
		t.Fatalf("over the air: %v", ota)
	}
	first, second := ota[0].(map[string]any), ota[1].(map[string]any)
	if first["network"] != "NBC" || first["name"] != "NBC" || first["category"] != "Local" || first["number"] != "4.1" {
		t.Fatalf("first: %v", first)
	}
	if second["name"] != "Antenna TV" || second["category"] != "Local" || second["network"] != nil {
		t.Fatalf("second: %v", second)
	}

	// Cable: channels without programs are left out, and a standard channel whose HD twin is there; the rest are in channel order.
	cable := getJSON[map[string]any](t, admin, base+"/api/tv/replay/lineup?zip=73107&date=2026-10-16&lineup=USA-OK35543-DEFAULT")["channels"].([]any)
	if len(cable) != 2 || cable[0].(map[string]any)["name"] != "ESPN" || cable[0].(map[string]any)["category"] != "Sports" || cable[1].(map[string]any)["name"] != "HGTVD" {
		t.Fatalf("cable: %v", cable)
	}
	if st, _ := doJSONStatus(t, admin, http.MethodGet, base+"/api/tv/replay/lineup?zip=73107&date=2026-10-16&lineup=USA-NOPE-DEFAULT", nil); st != http.StatusBadRequest {
		t.Fatalf("an unknown lineup answered %d", st)
	}
	if !strings.Contains(first["id"].(string), "o4") {
		t.Fatal("channel ids aren't the listings' ids")
	}

	// Too many channels at once are refused.
	ids := make([]string, 41)
	for i := range ids {
		ids[i] = "c"
	}
	if st, _ := doJSONStatus(t, admin, http.MethodPost, base+"/api/tv/replay", map[string]any{"zip": "73107", "date": "2026-10-16", "channels": ids}); st != http.StatusBadRequest {
		t.Fatalf("41 channels answered %d", st)
	}
}
