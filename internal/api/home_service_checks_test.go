package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/mediaservers"
	"github.com/rdborg/mediarium/internal/notify"
	"github.com/rdborg/mediarium/internal/servicecheck"
	"github.com/rdborg/mediarium/internal/settings"
)

func serviceCheckTestServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	db, err := OpenTestDB(filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	box, err := crypto.LoadOrCreateKey(filepath.Join(dir, "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	return &Server{db: db, Settings: settings.New(db, box), NotifyRepo: notify.NewRepo(db, box), MediaServers: mediaservers.NewRepo(db, box)}
}

// Usherr: a service that stops answering sends one "down" message (after
// three failed checks) and one "back" message, only to the targets that chose
// those events, and never with the service's address in it.
func TestServiceChecksSendDownAndBack(t *testing.T) {
	s := serviceCheckTestServer(t)
	got := make(chan map[string]any, 10)
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		got <- m
	}))
	t.Cleanup(hook.Close)
	other := make(chan struct{}, 10)
	quiet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { other <- struct{}{} }))
	t.Cleanup(quiet.Close)
	if _, err := s.NotifyRepo.Create(notify.Target{Name: "phone", Type: notify.TargetWebhook, Events: []string{notify.EventServiceDown, notify.EventServiceUp}, Config: map[string]string{"url": hook.URL}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.NotifyRepo.Create(notify.Target{Name: "older", Type: notify.TargetWebhook, Config: map[string]string{"url": quiet.URL}}); err != nil {
		t.Fatal(err)
	}
	if n := s.serviceAlertTargets(); n != 1 {
		t.Fatalf("%d targets hear about outages, want 1 (the events are opt-in)", n)
	}

	var up atomic.Bool
	up.Store(true)
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !up.Load() {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte("Healthy"))
	}))
	t.Cleanup(svc.Close)

	ctx := context.Background()
	runner := s.serviceChecks()
	now := time.Date(2026, 11, 4, 21, 0, 0, 0, time.UTC)
	runner.Now = func() time.Time { return now }
	c, err := servicecheck.Normalize(servicecheck.Check{Name: "Jellyfin", URL: svc.URL + "/health?api_key=topsecret", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Repo.Create(ctx, c, now); err != nil {
		t.Fatal(err)
	}
	for i, ok := range []bool{true, false, false, false, false, true, true} {
		up.Store(ok)
		s.serviceChecksJob(ctx)
		if i < 6 {
			now = now.Add(time.Minute)
		}
	}
	s.bg.Wait()

	var msgs []map[string]any
	for len(got) > 0 {
		msgs = append(msgs, <-got)
	}
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want one down and one back: %v", len(msgs), msgs)
	}
	byEvent := map[string]string{}
	for _, m := range msgs {
		ev, _ := m["event"].(string)
		title, _ := m["title"].(string)
		msg, _ := m["message"].(string)
		byEvent[ev] = title + " | " + msg
	}
	down, back := byEvent["servicedown"], byEvent["serviceup"]
	if !strings.HasPrefix(down, "Jellyfin is down | Jellyfin has not answered for 3 checks in a row") || !strings.Contains(down, "502") {
		t.Errorf("down message %q", down)
	}
	if want := "Jellyfin is back | Jellyfin is answering again after 4 minutes."; back != want {
		t.Errorf("back message %q, want %q", back, want)
	}
	for _, m := range byEvent {
		if strings.Contains(m, "topsecret") || strings.Contains(m, "127.0.0.1") {
			t.Errorf("a message holds the address: %q", m)
		}
	}
	if len(other) != 0 {
		t.Error("a target that did not choose the events got a message")
	}
}

func TestServiceCheckSuggestions(t *testing.T) {
	s := serviceCheckTestServer(t)
	if _, err := s.MediaServers.Create(mediaservers.Server{Name: "Jellyfin", Kind: mediaservers.KindJellyfin, BaseURL: "http://host.docker.internal:8096/", Token: "k", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MediaServers.Create(mediaservers.Server{Name: "Plex", Kind: mediaservers.KindPlex, BaseURL: "http://plex:32400", Token: "k", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]string{
		settings.KeyUsenetClientKind: "nzbget", settings.KeyUsenetClientURL: "http://nzbget:6789",
		settings.KeyDownloadClientKind: "qbittorrent", settings.KeyDownloadClientURL: "http://qbit:8080/",
	} {
		if err := s.Settings.Set(k, v, false); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	if _, err := s.serviceChecks().Repo.Create(ctx, servicecheck.Check{Name: "Plex", URL: "http://plex:32400/identity", Expect: servicecheck.Expect200, Interval: time.Minute, FailAfter: 3, Enabled: true}, time.Now()); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.handleServiceCheckSuggestions(rec, httptest.NewRequest(http.MethodGet, "/api/service-checks/suggestions", nil))
	var out []serviceSuggestion
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, x := range out {
		lines = append(lines, x.Name+" "+x.URL+" "+x.Expect)
	}
	want := "Jellyfin http://host.docker.internal:8096/health 200,qBittorrent http://qbit:8080/ ok,NZBGet http://nzbget:6789/ any"
	if got := strings.Join(lines, ","); got != want {
		t.Errorf("suggestions\n got %s\nwant %s (Plex is on the list already)", got, want)
	}
}

func TestProwlarrOrigin(t *testing.T) {
	tests := map[string]string{
		"http://prowlarr:9696/1/api":            "http://prowlarr:9696",
		"http://192.168.1.5:9696/12/":           "http://192.168.1.5:9696",
		"https://home.example/prowlarr/3/api":   "https://home.example/prowlarr",
		"https://api.nzbgeek.info/api":          "",
		"http://host.docker.internal:9117/jack": "",
	}
	for in, want := range tests {
		got, ok := prowlarrOrigin(in)
		if got != want || ok != (want != "") {
			t.Errorf("%s: %q %v, want %q", in, got, ok, want)
		}
	}
}

// The routes: add, list, check now, try, change, a bad address, remove.
func TestServiceCheckRoutes(t *testing.T) {
	s := serviceCheckTestServer(t)
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) }))
	t.Cleanup(svc.Close)
	call := func(method, path, body string, handler http.HandlerFunc, id string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if id != "" {
			req.SetPathValue("id", id)
		}
		rec := httptest.NewRecorder()
		handler(rec, req)
		return rec
	}
	rec := call(http.MethodPost, "/api/service-checks", `{"name":"Jellyfin","url":"`+svc.URL+`/health","expect":"200"}`, s.handleSaveServiceCheck, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("add: %d %s", rec.Code, rec.Body)
	}
	var made serviceCheckJSON
	_ = json.Unmarshal(rec.Body.Bytes(), &made)
	if made.State != "unknown" || made.IntervalSec != 60 || made.FailAfter != 3 || !made.Enabled {
		t.Errorf("added: %+v", made)
	}
	id := strconv.FormatInt(made.ID, 10)

	rec = call(http.MethodPost, "/api/service-checks/"+id+"/check", ``, s.handleCheckServiceNow, id)
	var checked serviceCheckJSON
	_ = json.Unmarshal(rec.Body.Bytes(), &checked)
	if rec.Code != http.StatusOK || checked.State != "up" || len(checked.Recent) != 1 || checked.Uptime24h.Percent != 100 {
		t.Fatalf("check now: %d %s", rec.Code, rec.Body)
	}

	rec = call(http.MethodPost, "/api/service-checks/try", `{"url":"`+svc.URL+`/","keyword":"missing"}`, s.handleTryServiceCheck, "")
	var tried serviceResultJSON
	_ = json.Unmarshal(rec.Body.Bytes(), &tried)
	if rec.Code != http.StatusOK || tried.OK || tried.Code != 200 || !strings.Contains(tried.Error, "missing") {
		t.Errorf("try: %d %s", rec.Code, rec.Body)
	}

	rec = call(http.MethodPut, "/api/service-checks/"+id, `{"name":"Jellyfin","url":"`+svc.URL+`/health","intervalSec":120,"failAfter":5,"enabled":false}`, s.handleSaveServiceCheck, id)
	var changed serviceCheckJSON
	_ = json.Unmarshal(rec.Body.Bytes(), &changed)
	if rec.Code != http.StatusOK || changed.IntervalSec != 120 || changed.FailAfter != 5 || changed.Enabled {
		t.Errorf("change: %d %s", rec.Code, rec.Body)
	}

	if rec = call(http.MethodPost, "/api/service-checks", `{"name":"Bad","url":"jellyfin:8096"}`, s.handleSaveServiceCheck, ""); rec.Code != http.StatusBadRequest {
		t.Errorf("a bad address: %d %s", rec.Code, rec.Body)
	}

	rec = call(http.MethodGet, "/api/service-checks", ``, s.handleListServiceChecks, "")
	var list struct {
		Checks       []serviceCheckJSON `json:"checks"`
		AlertTargets int                `json:"alertTargets"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if rec.Code != http.StatusOK || len(list.Checks) != 1 || list.AlertTargets != 0 {
		t.Errorf("list: %d %s", rec.Code, rec.Body)
	}

	if rec = call(http.MethodDelete, "/api/service-checks/"+id, ``, s.handleDeleteServiceCheck, id); rec.Code != http.StatusNoContent {
		t.Errorf("remove: %d %s", rec.Code, rec.Body)
	}
	if rec = call(http.MethodDelete, "/api/service-checks/"+id, ``, s.handleDeleteServiceCheck, id); rec.Code != http.StatusNotFound {
		t.Errorf("remove again: %d", rec.Code)
	}
}
