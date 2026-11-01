package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/api"
	"github.com/rdborg/mediarium/internal/notify"
	"github.com/rdborg/mediarium/internal/settings"
)

// resetServer has an administrator (ryan, no email address), a member (sam,
// sam@example.com, signed in on member) and an email notification.
func resetServer(t *testing.T) (*api.Server, string, *http.Client, *http.Client, chan [3]string) {
	t.Helper()
	server, base, admin := loginNewServer(t)
	postJSON[map[string]any](t, admin, base+"/api/users", map[string]any{
		"username": "sam", "password": "kids-password", "name": "Sam", "email": "sam@example.com", "role": "member",
	}, http.StatusCreated)
	jar, _ := cookiejar.New(nil)
	member := &http.Client{Jar: jar, Timeout: 20 * time.Second}
	postJSON[map[string]any](t, member, base+"/api/auth/login", map[string]string{"username": "sam", "password": "kids-password"}, http.StatusOK)
	if _, err := server.NotifyRepo.Create(notify.Target{Name: "Mail", Type: notify.TargetEmail, Enabled: true, Config: map[string]string{
		"host": "smtp.example.com", "port": "587", "security": "starttls", "from": "usherr@example.com", "to": "household@example.com",
	}}); err != nil {
		t.Fatal(err)
	}
	mail := make(chan [3]string, 10)
	api.CaptureAccountMail(t, mail)
	return server, base, admin, member, mail
}

// ask sends a Forgot password? request from a page claiming another site.
func ask(t *testing.T, base, who string) (int, string) {
	t.Helper()
	b, _ := json.Marshal(map[string]string{"who": who})
	req, _ := http.NewRequest(http.MethodPost, base+"/api/auth/forgot", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-Host", "evil.example")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out["message"] + out["error"]
}

func noMail(t *testing.T, mail chan [3]string, why string) {
	t.Helper()
	select {
	case m := <-mail:
		t.Fatalf("%s: an email went to %s", why, m[0])
	case <-time.After(300 * time.Millisecond):
	}
}

var resetLink = regexp.MustCompile(`(\S+)/reset/([0-9a-f]+)`)

func TestForgotPasswordByEmail(t *testing.T) {
	server, base, admin, member, mail := resetServer(t)

	// Off until switched on: no link on the sign-in page, and the addresses answer 404.
	if m := getJSON[map[string]any](t, http.DefaultClient, base+"/api/auth/methods"); m["emailReset"] != false {
		t.Fatalf("offered while off: %v", m)
	}
	if code, _ := ask(t, base, "sam"); code != http.StatusNotFound {
		t.Fatalf("asked while off: %d", code)
	}
	// It can't be switched on without the address people open Usherr at.
	postJSONMethod[map[string]any](t, admin, http.MethodPut, base+"/api/auth/sign-in", map[string]any{"emailReset": true}, http.StatusBadRequest)
	if err := server.Settings.Set(settings.KeyPublicURL, "https://usherr.example.ts.net", false); err != nil {
		t.Fatal(err)
	}
	postJSONMethod[map[string]any](t, admin, http.MethodPut, base+"/api/auth/sign-in", map[string]any{"emailReset": true}, http.StatusOK)
	if m := getJSON[map[string]any](t, http.DefaultClient, base+"/api/auth/methods"); m["emailReset"] != true {
		t.Fatalf("not offered once on: %v", m)
	}

	// The same reply for an account that doesn't exist and one without an
	// email address, and nothing sent.
	_, want := ask(t, base, "sam")
	got := <-mail
	for _, who := range []string{"nobody", "ryan"} {
		if code, reply := ask(t, base, who); code != http.StatusAccepted || reply != want {
			t.Fatalf("%s: %d %q, want 202 %q", who, code, reply, want)
		}
		noMail(t, mail, who)
	}

	// Sam's email: to Sam only, with a link from the address set, never the request's.
	if got[0] != "sam@example.com" || !strings.Contains(got[2], "If this wasn't you") {
		t.Fatalf("the email: %v", got)
	}
	m := resetLink.FindStringSubmatch(got[2])
	if m == nil || m[1] != "https://usherr.example.ts.net" || strings.Contains(got[2], "evil") {
		t.Fatalf("the link: %q", got[2])
	}
	token := m[2]
	if who := getJSON[map[string]any](t, http.DefaultClient, base+"/api/auth/reset/"+token); who["username"] != "sam" {
		t.Fatalf("checking the link: %v", who)
	}

	// Too short a password is refused and leaves the link working.
	postJSON[map[string]any](t, http.DefaultClient, base+"/api/auth/reset", map[string]string{"token": token, "password": "short"}, http.StatusBadRequest)
	postJSON[map[string]any](t, http.DefaultClient, base+"/api/auth/reset", map[string]string{"token": token, "password": "sams-new-password"}, http.StatusOK)
	postJSON[map[string]any](t, http.DefaultClient, base+"/api/auth/reset", map[string]string{"token": token, "password": "another-password"}, http.StatusGone)
	if code, _ := doStatus(t, member, http.MethodGet, base+"/api/auth/me"); code != http.StatusUnauthorized {
		t.Fatalf("Sam's session outlived the reset: %d", code)
	}
	postJSON[map[string]any](t, member, base+"/api/auth/login", map[string]string{"username": "sam", "password": "sams-new-password"}, http.StatusOK)

	lines := activityLines(t, admin, base)
	for _, want := range []string{"Password reset by email requested for sam", "Password reset by email for sam"} {
		if !strings.Contains(lines, want) {
			t.Errorf("Activity lacks %q:\n%s", want, lines)
		}
	}
	if strings.Contains(lines, token) {
		t.Error("the link's secret reached Activity")
	}

	// The sixth request from one address in 15 minutes is refused (three were
	// made above; Sam's second email ends the first link, so it's read here).
	ask(t, base, "nobody")
	ask(t, base, "sam")
	<-mail
	if code, _ := ask(t, base, "sam"); code != http.StatusTooManyRequests {
		t.Fatalf("the sixth request: %d", code)
	}
	noMail(t, mail, "the sixth request")
}
