package api_test

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// "Sign out after being idle" is off at first. When an administrator sets a
// number of days, a browser session not used for that long is refused and the
// answer says why; API keys are not touched.
func TestIdleSignOut(t *testing.T) {
	e := newSignInEnv(t, nil)
	e.addAccount("kim", "member")
	kim, status, _ := e.login("kim", "long-enough-password")
	if status != http.StatusOK {
		t.Fatalf("sign in: %d", status)
	}
	key := e.must(e.admin, "POST", "/api/auth/api-keys", map[string]string{"name": "script"}, http.StatusCreated)["key"].(string)

	// Off by default: a session left alone for a month still works.
	if got := e.must(e.admin, "GET", "/api/auth/idle-signout", nil, http.StatusOK); got["days"].(float64) != 0 || got["maxDays"].(float64) != 7 {
		t.Fatalf("default: %v", got)
	}
	e.server.TestAgeSessions(30 * 24 * time.Hour)
	if name, _ := e.signedInAs(e.admin); name != "ryan" {
		t.Fatalf("with the limit off, an old session should work, got %q", name)
	}

	// Only an administrator may change it, and only to a number it can use.
	if status, _ := e.call(kim, "PUT", "/api/auth/idle-signout", map[string]int{"days": 2}); status != http.StatusForbidden {
		t.Errorf("a member changed the limit: %d", status)
	}
	if status, _ := e.call(kim, "GET", "/api/auth/idle-signout", nil); status != http.StatusForbidden {
		t.Errorf("a member read the limit: %d", status)
	}
	for _, bad := range []int{-1, 8, 365} {
		if status, out := e.call(e.admin, "PUT", "/api/auth/idle-signout", map[string]int{"days": bad}); status != http.StatusBadRequest || !strings.Contains(out["error"].(string), "1 to 7") {
			t.Errorf("days=%d: status %d %v", bad, status, out["error"])
		}
	}
	if status, _ := e.call(e.admin, "PUT", "/api/auth/idle-signout", map[string]string{"days": "x"}); status != http.StatusBadRequest {
		t.Errorf("a word instead of a number: %d", status)
	}
	e.must(e.admin, "PUT", "/api/auth/idle-signout", map[string]int{"days": 2}, http.StatusOK)

	for _, tc := range []struct {
		name   string
		idle   time.Duration
		signed bool
	}{
		{"just used", time.Hour, true},
		{"a day", 24 * time.Hour, true},
		{"under the limit", 47 * time.Hour, true},
		{"over the limit", 49 * time.Hour, false},
		{"far over", 5 * 24 * time.Hour, false},
	} {
		e.server.TestAgeSessions(tc.idle)
		status, out := e.call(e.admin, "GET", "/api/auth/me", nil)
		if tc.signed {
			if status != http.StatusOK {
				t.Fatalf("%s: signed out too early: %d", tc.name, status)
			}
			continue
		}
		if status != http.StatusUnauthorized || out["reason"] != "idle" || out["error"] != "You need to sign in first." {
			t.Fatalf("%s: want 401 with reason idle, got %d %v", tc.name, status, out["_raw"])
		}
		// Later calls from the same browser say the same, on any page.
		status, out = e.call(e.admin, "GET", "/api/movies", nil)
		if status != http.StatusUnauthorized || out["reason"] != "idle" {
			t.Fatalf("%s: a later call: %d %v", tc.name, status, out["_raw"])
		}
		break
	}

	// An API key does not go through browser sessions.
	if status, _ := e.call(e.client(), "GET", "/api/auth/me", nil, "X-API-Key", key); status != http.StatusOK {
		t.Errorf("the API key stopped working: %d", status)
	}
	// Someone with no session at all just gets the usual answer, without a reason.
	if status, out := e.call(e.client(), "GET", "/api/auth/me", nil); status != http.StatusUnauthorized || out["reason"] != nil || !strings.Contains(out["_raw"].(string), "unauthorized") {
		t.Errorf("no session: %d %v", status, out["_raw"])
	}

	// Signing in again works, and the new session is not idle.
	admin2, status, _ := e.login("ryan", adminPassword)
	if status != http.StatusOK {
		t.Fatalf("sign in again: %d", status)
	}
	if name, _ := e.signedInAs(admin2); name != "ryan" {
		t.Fatal("the new session should work")
	}
	// Switching it off again lets sessions that are not yet ended carry on.
	e.must(admin2, "PUT", "/api/auth/idle-signout", map[string]int{"days": 0}, http.StatusOK)
	e.server.TestAgeSessions(20 * 24 * time.Hour)
	if name, _ := e.signedInAs(admin2); name != "ryan" {
		t.Error("with the limit off, the session should work")
	}
}
