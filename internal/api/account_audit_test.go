package api_test

import (
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
)

func activityLines(t *testing.T, client *http.Client, base string) string {
	t.Helper()
	var b strings.Builder
	for _, e := range getJSON[[]map[string]any](t, client, base+"/api/activity?limit=200") {
		fmt.Fprintf(&b, "%v: %v\n", e["eventType"], e["message"])
	}
	return b.String()
}

// Changes to accounts and API keys leave a line in the Activity feed that
// only administrators see, and never hold a password or a key.
func TestAccountChangesAreLoggedForAdministrators(t *testing.T) {
	_, base, admin, member, memberID := familyServer(t)
	url := fmt.Sprintf("%s/api/users/%v", base, memberID)

	postJSONMethod[map[string]any](t, admin, http.MethodPut, url, map[string]any{"role": "viewer"}, http.StatusOK)
	postJSONMethod[map[string]any](t, admin, http.MethodPut, url, map[string]any{"disabled": true}, http.StatusOK)
	postJSONMethod[map[string]any](t, admin, http.MethodPut, url, map[string]any{"disabled": false, "password": "a-fresh-secret-pass"}, http.StatusOK)
	key := postJSON[map[string]any](t, admin, base+"/api/auth/api-keys", map[string]any{"name": "dashboard"}, http.StatusCreated)
	keys := getJSON[[]map[string]any](t, admin, base+"/api/auth/api-keys")
	if len(keys) != 1 {
		t.Fatalf("keys = %v", keys)
	}
	if code := deleteReq(t, admin, fmt.Sprintf("%s/api/auth/api-keys/%v", base, keys[0]["id"])); code != http.StatusOK && code != http.StatusNoContent {
		t.Fatalf("revoke key: %d", code)
	}
	if code := deleteReq(t, admin, url); code != http.StatusOK && code != http.StatusNoContent {
		t.Fatalf("delete account: %d", code)
	}

	lines := activityLines(t, admin, base)
	for _, want := range []string{
		`Account "sam" was created as a member`,
		`Account "sam" went from a member to a viewer`,
		`Account "sam" was turned off`,
		`Account "sam" was turned on again`,
		`The password of account "sam" was set by an administrator`,
		`An API key named "dashboard" was made for account "ryan"`,
		`was revoked`,
		`Account "sam" was deleted`,
	} {
		if !strings.Contains(lines, want) {
			t.Errorf("the Activity feed lacks %q:\n%s", want, lines)
		}
	}
	for _, secret := range []string{"a-fresh-secret-pass", fmt.Sprint(key["key"])} {
		if secret != "" && strings.Contains(lines, secret) {
			t.Errorf("a secret reached the Activity feed: %q", secret)
		}
	}
	// The member's session ended with the deletion; a new member sees none of it.
	_ = member
}

func TestAccountLinesAreHiddenFromMembers(t *testing.T) {
	_, base, admin, member, _ := familyServer(t)
	if !strings.Contains(activityLines(t, admin, base), "was created as a member") {
		t.Fatal("the administrator should see the account line")
	}
	if lines := activityLines(t, member, base); strings.Contains(lines, "account:") {
		t.Fatalf("a member sees account lines:\n%s", lines)
	}
}

// Where files lie on the server is for administrators: the Activity feed
// shows everybody else only the file name.
func TestActivityHidesLibraryPathsFromMembers(t *testing.T) {
	server, base, admin, member, _ := familyServer(t)
	if err := server.TestExec(`INSERT INTO activity (event_type, message) VALUES ('imported', ?)`,
		"Night Harbour (2021) imported to /srv/media/Movies/Night Harbour (2021)/Night Harbour (2021).mkv"); err != nil {
		t.Fatal(err)
	}
	if lines := activityLines(t, admin, base); !strings.Contains(lines, "/srv/media/Movies/Night Harbour (2021)/") {
		t.Fatalf("an administrator should see the full path:\n%s", lines)
	}
	lines := activityLines(t, member, base)
	if strings.Contains(lines, "/srv/media") {
		t.Fatalf("a member sees the folder path:\n%s", lines)
	}
	if !strings.Contains(lines, "imported to Night Harbour (2021).mkv") {
		t.Fatalf("the file name should stay:\n%s", lines)
	}
}

// If the second-step state cannot be read, the password alone must not sign
// anyone in.
func TestSignInFailsClosedWhenTwoStepStateIsUnreadable(t *testing.T) {
	server, base, _ := loginNewServer(t)
	if err := server.TestExec(`UPDATE users SET recovery_codes = 'not json' WHERE username = 'ryan'`); err != nil {
		t.Fatal(err)
	}
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	r := rawPost(t, client, base+"/api/auth/login", map[string]string{"username": "ryan", "password": "correct-horse-battery-staple"}, nil)
	if r.StatusCode != http.StatusInternalServerError {
		t.Fatalf("sign-in with an unreadable two-step state: %d, want 500", r.StatusCode)
	}
	if len(r.Cookies()) != 0 {
		t.Fatal("a session was started anyway")
	}
}

// A turned-off account's Plex watchlist adds nothing any more.
func TestWatchlistJobSkipsTurnedOffAccounts(t *testing.T) {
	server, base, admin, member, memberID := familyServer(t)
	server.TestSetTMDBBaseURL("fixture-key", arrTMDB(t).URL)
	plex := newFakePlexWatchlist(t)
	server.TestSetPlexTVURL(plex.URL)
	server.TestSetPlexDiscoverURL(plex.URL)
	plex.put("old1", "movie", "Night Harbour", 2021, "tmdb://701")
	connectWatchlist(t, member, base)

	count := func() int {
		n := len(getJSON[[]map[string]any](t, admin, base+"/api/series"))
		return n + len(getJSON[map[string]any](t, admin, base+"/api/requests")["requests"].([]any))
	}
	plex.put("new1", "show", "Salt & Static", 2019, "tvdb://777001", "tmdb://9001")
	server.TestRunWatchlistJob()
	if count() != 1 {
		t.Fatalf("the job should act for a working account, got %d", count())
	}

	postJSONMethod[map[string]any](t, admin, http.MethodPut, fmt.Sprintf("%s/api/users/%v", base, memberID), map[string]any{"disabled": true}, http.StatusOK)
	plex.put("new2", "movie", "Second Light", 2020, "tmdb://702")
	server.TestRunWatchlistJob()
	if count() != 1 {
		t.Fatalf("the job added something for a turned-off account, got %d", count())
	}
}

// Signed-in devices can be listed and ended, except this one.
func TestSignedInDevicesCanBeEnded(t *testing.T) {
	_, base, admin, member, _ := familyServer(t)
	// A second sign-in of the same member.
	jar, _ := cookiejar.New(nil)
	other := &http.Client{Jar: jar}
	postJSON[map[string]any](t, other, base+"/api/auth/login", map[string]string{"username": "sam", "password": "kids-password"}, http.StatusOK)

	list := getJSON[[]map[string]any](t, member, base+"/api/auth/sessions")
	if len(list) != 2 {
		t.Fatalf("sessions = %v, want 2", list)
	}
	current := 0
	for _, s := range list {
		if s["current"] == true {
			current++
		}
	}
	if current != 1 {
		t.Fatalf("%d sessions are marked as this one, want 1: %v", current, list)
	}
	if code := deleteReq(t, member, base+"/api/auth/sessions"); code != http.StatusOK {
		t.Fatalf("end other sessions: %d", code)
	}
	if code, _ := doStatus(t, member, http.MethodGet, base+"/api/auth/me"); code != http.StatusOK {
		t.Fatalf("this device was signed out too: %d", code)
	}
	if code, _ := doStatus(t, other, http.MethodGet, base+"/api/auth/me"); code != http.StatusUnauthorized {
		t.Fatalf("the other device is still signed in: %d", code)
	}
	_ = admin
}
