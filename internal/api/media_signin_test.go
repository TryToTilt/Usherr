package api_test

import (
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
	"time"
)

// Usherr: a friend signs in with their Jellyfin name and password once an
// administrator switches it on. The first sign-in makes a member account; a
// name with a password here never goes to Jellyfin.
func TestSignInWithJellyfin(t *testing.T) {
	_, base, admin, _, _ := familyServer(t)
	jf := newFakeJellyfinLogin(t)
	if code, raw := rawRequest(t, admin, http.MethodPost, base+"/api/media-servers/login", map[string]any{"kind": "jellyfin", "baseUrl": jf.URL, "username": "ryan", "password": "pw"}); code != http.StatusCreated {
		t.Fatalf("add jellyfin: %d %s", code, raw)
	}
	friend := func() *http.Client {
		jar, _ := cookiejar.New(nil)
		return &http.Client{Jar: jar, Timeout: 20 * time.Second}
	}
	// Off: the Jellyfin name is unknown here.
	if code, _ := rawRequest(t, friend(), http.MethodPost, base+"/api/auth/login", map[string]string{"username": "jamie", "password": "jamie-pw"}); code != http.StatusUnauthorized {
		t.Fatalf("sign-in while off: %d", code)
	}
	if code, raw := rawRequest(t, admin, http.MethodPut, base+"/api/media-signin", map[string]any{"jellyfin": true}); code != http.StatusOK {
		t.Fatalf("switch on: %d %s", code, raw)
	}
	if _, raw := rawRequest(t, friend(), http.MethodGet, base+"/api/auth/media-signin", nil); !strings.Contains(raw, `"jellyfin":true`) {
		t.Errorf("public switch: %s", raw)
	}
	if code, _ := rawRequest(t, friend(), http.MethodPost, base+"/api/auth/login", map[string]string{"username": "jamie", "password": "wrong"}); code != http.StatusUnauthorized {
		t.Fatalf("wrong password: %d", code)
	}
	c := friend()
	code, raw := rawRequest(t, c, http.MethodPost, base+"/api/auth/login", map[string]string{"username": "jamie", "password": "jamie-pw"})
	if code != http.StatusOK || !strings.Contains(raw, `"role":"member"`) || strings.Contains(raw, "jf-friend") {
		t.Fatalf("jellyfin sign-in: %d %s", code, raw)
	}
	// Again: the same account, found by the Jellyfin user's id.
	code, raw2 := rawRequest(t, friend(), http.MethodPost, base+"/api/auth/login", map[string]string{"username": "jamie", "password": "jamie-pw"})
	if code != http.StatusOK || decodeInto(t, raw2)["id"] != decodeInto(t, raw)["id"] {
		t.Errorf("second sign-in: %d %s vs %s", code, raw2, raw)
	}
	// A name with a password here is never sent to Jellyfin.
	before := len(jf.bodies)
	if code, _ := rawRequest(t, friend(), http.MethodPost, base+"/api/auth/login", map[string]string{"username": "sam", "password": "kids-password"}); code != http.StatusOK {
		t.Errorf("local member: %d", code)
	}
	if len(jf.bodies) != before {
		t.Error("a local account's sign-in reached Jellyfin")
	}
	// Plex is refused while it isn't switched on.
	if code, _ := rawRequest(t, friend(), http.MethodPost, base+"/api/auth/plex/pin", nil); code != http.StatusForbidden {
		t.Errorf("plex pin while off: %d", code)
	}
}
