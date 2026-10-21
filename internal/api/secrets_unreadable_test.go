package api_test

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/api"
	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/download"
	"github.com/rdborg/mediarium/internal/mediaservers"
	"github.com/rdborg/mediarium/internal/notify"
)

// Secrets saved with another key are named on Health and flagged in their own
// lists; the readable ones around them keep working.
func TestUnreadableSecretsShowOnHealthAndInTheLists(t *testing.T) {
	server, base, admin := loginNewServer(t)

	// One readable Usenet server, so the key as a whole is the right one.
	good := postJSON[map[string]any](t, admin, base+"/api/usenet-servers", map[string]any{
		"name": "Good", "host": "news.example.com", "port": 563, "useSsl": true, "username": "u", "password": "p",
	}, http.StatusCreated)

	// Rows another key saved, written straight into the same database.
	db, err := api.OpenTestDB(filepath.Join(server.TestConfigDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	other, err := crypto.LoadOrCreateKey(filepath.Join(t.TempDir(), "other.key"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := download.NewRepo(db, other).Create(download.StoredClient{
		Name: "Lost Usenet", Config: download.ClientConfig{Host: "lost.example.com", Port: 563, UseSSL: true, Username: "u", Password: "x", Connections: 5},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := mediaservers.NewRepo(db, other).Create(mediaservers.Server{Name: "Lost Plex", Kind: mediaservers.KindPlex, BaseURL: "http://plex:32400", Token: "t", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := notify.NewRepo(db, other).Create(notify.Target{Name: "Lost hook", Type: notify.TargetWebhook, URL: "https://example.com/hook/abc"}); err != nil {
		t.Fatal(err)
	}

	health := healthByID(t, admin, base)
	var found []string
	for id, it := range health {
		if strings.Contains(id, "secret-unreadable") {
			found = append(found, id)
			if it["impact"] == "" || it["action"] == nil {
				t.Errorf("%s needs an impact and an action: %v", id, it)
			}
		}
	}
	if len(found) != 3 {
		t.Fatalf("expected the Usenet server, the media server and the notification on Health, got %v (all: %v)", found, keysOf(health))
	}
	if health["secret-key-mismatch"] != nil {
		t.Error("the key as a whole is right: no mismatch item")
	}

	// The lists still load, the unreadable row carries a plain message, the good one has none.
	servers := getJSON[[]map[string]any](t, admin, base+"/api/usenet-servers")
	if len(servers) != 2 {
		t.Fatalf("both servers should be listed: %v", servers)
	}
	for _, s := range servers {
		msg, _ := s["secretError"].(string)
		switch s["name"] {
		case "Lost Usenet":
			if !strings.Contains(msg, "can't read the saved password") {
				t.Errorf("Lost Usenet: secretError = %q", msg)
			}
		case "Good":
			if msg != "" {
				t.Errorf("Good: secretError = %q", msg)
			}
		}
	}
	if media := getJSON[[]map[string]any](t, admin, base+"/api/media-servers"); len(media) != 1 || media[0]["secretError"] == nil {
		t.Errorf("media server list: %v", media)
	}
	if targets := getJSON[[]map[string]any](t, admin, base+"/api/notifications"); len(targets) != 1 || targets[0]["secretError"] == nil {
		t.Errorf("notification list: %v", targets)
	}
	// Testing the unreadable server says why instead of trying with an empty password.
	var lostID any
	for _, s := range servers {
		if s["name"] == "Lost Usenet" {
			lostID = s["id"]
		}
	}
	res := postJSON[map[string]any](t, admin, base+"/api/usenet-servers/"+fmt.Sprint(int64(lostID.(float64)))+"/test", nil, http.StatusOK)
	if msg, _ := res["message"].(string); !strings.Contains(msg, "can't read the saved password") {
		t.Errorf("test of the unreadable server: %v", res)
	}
	_ = good
}
