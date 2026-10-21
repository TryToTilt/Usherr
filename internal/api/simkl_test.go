package api_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/rdborg/mediarium/internal/api"
	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/settings"
)

const (
	simklTestClient = "fixture-simkl-client-id-0123456789"
	simklTestToken  = "fixture-simkl-token-abcdef0123456789"
)

// fakeSimkl is a Simkl with the sign-in code flow and a person's lists. entered
// says whether the code AB12C has been entered at simkl.com/pin yet; listCode
// is the status the lists answer with (200, or an error to test).
type fakeSimkl struct {
	srv      *httptest.Server
	entered  atomic.Bool
	listCode atomic.Int32
	pinCode  atomic.Int32 // status of the sign-in code calls; 0 means normal
	calls    atomic.Int32
}

func newFakeSimkl(t *testing.T) *fakeSimkl {
	t.Helper()
	f := &fakeSimkl{}
	f.listCode.Store(200)
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/pin", func(w http.ResponseWriter, r *http.Request) {
		if code := int(f.pinCode.Load()); code != 0 {
			w.WriteHeader(code)
			return
		}
		if r.URL.Query().Get("client_id") != simklTestClient {
			_, _ = w.Write([]byte(`{"result":"KO","message":"unknown client"}`))
			return
		}
		_, _ = w.Write([]byte(`{"result":"OK","user_code":"AB12C","verification_url":"https://simkl.com/pin","expires_in":900,"interval":5}`))
	})
	mux.HandleFunc("/oauth/pin/{code}", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.PathValue("code") != "AB12C":
			_, _ = w.Write([]byte(`{"result":"KO","message":"Code expired"}`))
		case f.entered.Load():
			_, _ = w.Write([]byte(`{"result":"OK","access_token":"` + simklTestToken + `"}`))
		default:
			_, _ = w.Write([]byte(`{"result":"KO","message":"Authorization pending"}`))
		}
	})
	mux.HandleFunc("/sync/all-items/{kind}/{status}", func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer "+simklTestToken || r.Header.Get("simkl-api-key") != simklTestClient {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if code := int(f.listCode.Load()); code != 200 {
			w.WriteHeader(code)
			return
		}
		if r.PathValue("status") != "plantowatch" {
			_, _ = w.Write([]byte(`null`))
			return
		}
		switch r.PathValue("kind") {
		case "movies":
			// Foxtrot by its TMDB id, Alpha by its IMDb id only.
			_, _ = w.Write([]byte(`{"movies":[
				{"movie":{"title":"Foxtrot","year":2021,"ids":{"simkl":1,"tmdb":105}}},
				{"movie":{"title":"Alpha","year":2010,"ids":{"simkl":2,"imdb":"tt0000100"}}}]}`))
		case "shows":
			// Show One by its TheTVDB id only.
			_, _ = w.Write([]byte(`{"shows":[{"show":{"title":"Show One","year":2015,"ids":{"simkl":3,"tvdb":5000}}}]}`))
		default:
			_, _ = w.Write([]byte(`{"anime":[]}`))
		}
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func simklServer(t *testing.T) (*api.Server, string, *http.Client, *fakeSimkl) {
	t.Helper()
	server, base, admin := listsServer(t)
	f := newFakeSimkl(t)
	server.TestSetSimklBaseURL(simklTestClient, f.srv.URL)
	return server, base, admin, f
}

func connectSimkl(t *testing.T, base string, admin *http.Client, f *fakeSimkl) {
	t.Helper()
	pin := postJSON[map[string]any](t, admin, base+"/api/settings/simkl/pin", nil, http.StatusOK)
	if pin["code"] != "AB12C" || pin["url"] != "https://simkl.com/pin" {
		t.Fatalf("pin = %v", pin)
	}
	if got := getJSON[map[string]any](t, admin, base+"/api/settings/simkl/pin/AB12C"); got["done"] != false {
		t.Fatalf("pending: %v", got)
	}
	f.entered.Store(true)
	got := getJSON[map[string]any](t, admin, base+"/api/settings/simkl/pin/AB12C")
	if got["done"] != true {
		t.Fatalf("done: %v", got)
	}
	if st, _ := got["status"].(map[string]any); st["connected"] != true {
		t.Fatalf("status after connecting: %v", got)
	}
}

func TestSimklConnectAndImportAList(t *testing.T) {
	server, base, admin, f := simklServer(t)
	connectSimkl(t, base, admin, f)

	// The token never comes back from the server.
	for _, p := range []string{"/api/settings", "/api/settings/simkl", "/api/import-lists"} {
		resp, err := admin.Get(base + p)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if strings.Contains(string(raw), simklTestToken) || strings.Contains(string(raw), simklTestClient) {
			t.Fatalf("a Simkl secret came back from %s", p)
		}
	}
	if st := getJSON[map[string]any](t, admin, base+"/api/settings/simkl"); st["connected"] != true || st["hasClientId"] != true {
		t.Fatalf("status: %v", st)
	}

	list := postJSON[map[string]any](t, admin, base+"/api/import-lists", map[string]any{
		"source": "simkl", "media": "both", "config": map[string]any{"status": "plantowatch"}, "intervalHours": 24, "searchNow": false,
	}, http.StatusCreated)
	waitBackground(t, server)
	l := readList(t, admin, base, list["id"].(float64))
	if l["lastStatus"] != "ok" || l["lastFound"] != float64(3) || l["lastAdded"] != float64(3) {
		t.Fatalf("simkl list after the first read: %v", l)
	}
	movies := libraryTMDBIDs(t, admin, base, "/api/movies")
	if !movies[105] || !movies[100] {
		t.Fatalf("movies in the library: %v", movies)
	}
	if shows := libraryTMDBIDs(t, admin, base, "/api/series"); !shows[200] {
		t.Fatalf("the show found by its TheTVDB id is missing: %v", shows)
	}

	// Another status is another list: empty here, and nothing is removed.
	other := postJSON[map[string]any](t, admin, base+"/api/import-lists", map[string]any{
		"source": "simkl", "media": "both", "config": map[string]any{"status": "completed"}, "intervalHours": 24, "searchNow": false,
	}, http.StatusCreated)
	waitBackground(t, server)
	if l := readList(t, admin, base, other["id"].(float64)); l["lastStatus"] != "ok" || l["lastFound"] != float64(0) {
		t.Fatalf("empty list: %v", l)
	}
	if movies := libraryTMDBIDs(t, admin, base, "/api/movies"); !movies[105] {
		t.Fatal("reading an empty list must not remove anything")
	}
}

func TestSimklListNeedsTheAccountAndSaysSo(t *testing.T) {
	server, base, admin, _ := simklServer(t)
	list := postJSON[map[string]any](t, admin, base+"/api/import-lists", map[string]any{
		"source": "simkl", "media": "both", "config": map[string]any{}, "intervalHours": 24, "searchNow": false,
	}, http.StatusCreated)
	waitBackground(t, server)
	l := readList(t, admin, base, list["id"].(float64))
	if l["lastStatus"] != "error" || !strings.Contains(l["lastMessage"].(string), "Connect your Simkl account") {
		t.Fatalf("no account connected: %v", l)
	}
	if h := healthByID(t, admin, base); h["simkl-off"] == nil {
		t.Fatalf("Health should say Simkl isn't connected: %v", keysOf(h))
	}
}

func TestSimklErrorsAreShownPlainly(t *testing.T) {
	server, base, admin, f := simklServer(t)
	connectSimkl(t, base, admin, f)
	list := postJSON[map[string]any](t, admin, base+"/api/import-lists", map[string]any{
		"source": "simkl", "media": "both", "config": map[string]any{}, "intervalHours": 24, "searchNow": false, "enabled": false,
	}, http.StatusCreated)
	id := list["id"].(float64)
	for _, tc := range []struct {
		code int
		want string
	}{
		{401, "no longer accepts the saved sign-in"},
		{429, "asked to slow down"},
		{503, "isn't answering properly"},
	} {
		f.listCode.Store(int32(tc.code))
		l := runList(t, server, admin, base, id)
		msg, _ := l["lastMessage"].(string)
		if l["lastStatus"] != "error" || !strings.Contains(msg, tc.want) {
			t.Errorf("status %d: %v", tc.code, l)
		}
		if strings.Contains(msg, simklTestToken) || strings.Contains(msg, simklTestClient) {
			t.Errorf("status %d: a secret is in the message %q", tc.code, msg)
		}
	}
}

func TestSimklPinStatesAndDisconnect(t *testing.T) {
	_, base, admin, f := simklServer(t)
	// A code Mediarium never handed out is refused without asking Simkl.
	if r, err := admin.Get(base + "/api/settings/simkl/pin/ZZ99Z"); err != nil || r.StatusCode != http.StatusGone {
		t.Fatalf("an unknown code: %v %v", r, err)
	}
	connectSimkl(t, base, admin, f)
	if code := deleteReq(t, admin, base+"/api/settings/simkl"); code != http.StatusNoContent {
		t.Fatalf("disconnect: %d", code)
	}
	if st := getJSON[map[string]any](t, admin, base+"/api/settings/simkl"); st["connected"] != false {
		t.Fatalf("still connected: %v", st)
	}
}

func TestSimklClientIDIsCheckedAndNeverShown(t *testing.T) {
	_, base, admin, _ := simklServer(t)
	res := postJSON[map[string]any](t, admin, base+"/api/settings/test-service", map[string]any{"service": "simkl", "key": "someone-elses-id"}, http.StatusOK)
	if res["ok"] == true {
		t.Fatalf("a client ID Simkl does not know was accepted: %v", res)
	}
	res = postJSON[map[string]any](t, admin, base+"/api/settings/test-service", map[string]any{"service": "simkl", "key": simklTestClient}, http.StatusOK)
	if res["ok"] != true {
		t.Fatalf("the right client ID was refused: %v", res)
	}
	list := postJSON[map[string]any](t, admin, base+"/api/import-lists", map[string]any{
		"source": "simkl", "media": "both", "config": map[string]any{"status": "someday"}, "intervalHours": 24,
	}, http.StatusBadRequest)
	if msg, _ := list["error"].(string); !strings.Contains(msg, "Simkl list") {
		t.Fatalf("a made-up status: %v", list)
	}
}

func TestSimklSignInThatCannotBeReadIsFlaggedNotLost(t *testing.T) {
	server, base, admin, _ := simklServer(t)
	db, err := api.OpenTestDB(filepath.Join(server.TestConfigDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	other, err := crypto.LoadOrCreateKey(filepath.Join(t.TempDir(), "other.key"))
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.New(db, other).Set(settings.KeySimklToken, simklTestToken, true); err != nil {
		t.Fatal(err)
	}
	if st := getJSON[map[string]any](t, admin, base+"/api/settings/simkl"); st["unreadable"] != true || st["connected"] != false {
		t.Fatalf("status with an unreadable sign-in: %v", st)
	}
	list := postJSON[map[string]any](t, admin, base+"/api/import-lists", map[string]any{
		"source": "simkl", "media": "both", "config": map[string]any{}, "intervalHours": 24, "searchNow": false,
	}, http.StatusCreated)
	waitBackground(t, server)
	l := readList(t, admin, base, list["id"].(float64))
	if l["lastStatus"] != "error" || !strings.Contains(l["lastMessage"].(string), "can't be read") {
		t.Fatalf("list with an unreadable sign-in: %v", l)
	}
	h := healthByID(t, admin, base)
	if h["settings-secrets-unreadable"] == nil {
		t.Fatalf("Health should name the saved secret that can't be read: %v", keysOf(h))
	}
	if item := h["settings-secrets-unreadable"]; !strings.Contains(item["impact"].(string), "Simkl sign-in") {
		t.Fatalf("health item: %v", item)
	}
}

// A shared Simkl key that Simkl refuses shows on Health, with the way out.
func TestSharedSimklKeyAtItsLimitIsOnHealth(t *testing.T) {
	server, base, admin, f := simklServer(t)
	f.pinCode.Store(http.StatusTooManyRequests)
	server.TestSetSimklBaseURL("", f.srv.URL)
	server.SetBuiltinKeys(api.BuiltinKeys{SimklClientID: simklTestClient})
	if h := healthByID(t, admin, base); h["simkl-limit"] != nil {
		t.Fatal("no warning before any request was refused")
	}
	resp, err := admin.Post(base+"/api/settings/simkl/pin", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("start of the sign-in while Simkl refuses: %d", resp.StatusCode)
	}
	item := healthByID(t, admin, base)["simkl-limit"]
	if item == nil || item["level"] != "warn" || item["title"] != "The shared Simkl key is at its limit" {
		t.Fatalf("expected the simkl-limit warning, got %v", item)
	}
}
