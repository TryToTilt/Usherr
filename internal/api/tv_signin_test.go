package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
	"time"
)

// tvDo sends a JSON request with an optional user agent and API key, and
// answers the status and the decoded body.
func tvDo(t *testing.T, c *http.Client, method, url string, body any, ua, key string) (int, map[string]any) {
	t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, url, rd)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	if key != "" {
		req.Header.Set("X-API-Key", key)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func newJarClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar}
}

const shieldUA = "Mozilla/5.0 (Linux; Android 11; SHIELD Android TV) AppleWebKit/537.36 Chrome/120.0 Safari/537.36"

// tvStart asks for a code as a TV would.
func tvStart(t *testing.T, tv *http.Client, base string, body any, want int) map[string]any {
	t.Helper()
	status, out := tvDo(t, tv, http.MethodPost, base+"/api/auth/tv/start", body, shieldUA, "")
	if status != want {
		t.Fatalf("start: %d %v", status, out)
	}
	return out
}

func tvPoll(t *testing.T, tv *http.Client, base, secret string) map[string]any {
	t.Helper()
	status, out := tvDo(t, tv, http.MethodPost, base+"/api/auth/tv/poll", map[string]string{"secret": secret}, shieldUA, "")
	if status != http.StatusOK {
		t.Fatalf("poll: %d %v", status, out)
	}
	return out
}

func TestTVSignInApproveOnPhone(t *testing.T) {
	_, base, admin := loginNewServer(t)
	postJSON[map[string]any](t, admin, base+"/api/users", map[string]any{"username": "kim", "password": "kim-password-1", "role": "viewer"}, http.StatusCreated)
	phone := newJarClient()
	postJSON[map[string]any](t, phone, base+"/api/auth/login", map[string]string{"username": "kim", "password": "kim-password-1"}, http.StatusOK)

	if m := getJSON[map[string]any](t, newJarClient(), base+"/api/auth/methods"); m["tvSignIn"] != true {
		t.Fatalf("the sign-in page isn't told about TV sign-in: %v", m)
	}

	tv := newJarClient()
	start := tvStart(t, tv, base, nil, http.StatusOK)
	code, _ := start["code"].(string)
	secret, _ := start["secret"].(string)
	link, _ := start["url"].(string)
	if len(code) != 8 || len(secret) != 64 || !strings.HasSuffix(link, "/tv-code?code="+code) || len(start["qr"].([]any)) < 21 {
		t.Fatalf("start: %v", start)
	}
	if strings.Contains(link, secret) {
		t.Fatal("the link carries the secret")
	}
	if p := tvPoll(t, tv, base, secret); p["status"] != "waiting" || p["username"] != nil {
		t.Fatalf("poll before approval: %v", p)
	}
	if status, _ := doStatus(t, tv, http.MethodGet, base+"/api/auth/me"); status != http.StatusUnauthorized {
		t.Fatalf("signed in before approval: %d", status)
	}
	// The code alone (without signing in) shows nothing.
	if status, _ := doStatus(t, newJarClient(), http.MethodGet, base+"/api/auth/tv/code/"+code); status != http.StatusUnauthorized {
		t.Fatalf("code info without signing in: %d", status)
	}

	// The phone sees what asked, typed in any case and with a dash.
	typed := strings.ToLower(code[:4] + "-" + code[4:])
	info := getJSON[map[string]any](t, phone, base+"/api/auth/tv/code/"+typed)
	if info["device"] != "Chrome on NVIDIA Shield" || info["home"] != true || info["code"] != code {
		t.Fatalf("info: %v", info)
	}
	ok := postJSON[map[string]any](t, phone, base+"/api/auth/tv/code/"+code, map[string]bool{"approve": true}, http.StatusOK)
	if ok["status"] != "approved" {
		t.Fatalf("approve: %v", ok)
	}
	// A second answer, from anyone, is too late.
	postJSON[map[string]any](t, admin, base+"/api/auth/tv/code/"+code, map[string]bool{"approve": true}, http.StatusNotFound)

	p := tvPoll(t, tv, base, secret)
	if p["status"] != "approved" || p["username"] != "kim" || p["role"] != "viewer" {
		t.Fatalf("poll after approval: %v", p)
	}
	me := getJSON[map[string]any](t, tv, base+"/api/auth/me")
	if me["username"] != "kim" {
		t.Fatalf("TV signed in as %v", me["username"])
	}
	// Single use: the secret is spent.
	if p := tvPoll(t, tv, base, secret); p["status"] != "expired" {
		t.Fatalf("second poll: %v", p)
	}
	// Signed-in devices shows the TV.
	var list []map[string]any
	for _, s := range getJSON[[]map[string]any](t, phone, base+"/api/auth/sessions") {
		if s["method"] == "tv" {
			list = append(list, s)
		}
	}
	if len(list) != 1 || list[0]["device"] != "Chrome on NVIDIA Shield" {
		t.Fatalf("sessions: %v", list)
	}

	// The TV's own session can't approve another TV.
	other := tvStart(t, newJarClient(), base, nil, http.StatusOK)
	postJSON[map[string]any](t, tv, base+"/api/auth/tv/code/"+other["code"].(string), map[string]bool{"approve": true}, http.StatusForbidden)
	// Nor can an API key.
	key := postJSON[map[string]any](t, admin, base+"/api/auth/api-keys", map[string]any{"name": "script"}, http.StatusCreated)["key"].(string)
	if status, _ := tvDo(t, http.DefaultClient, http.MethodPost, base+"/api/auth/tv/code/"+other["code"].(string), map[string]bool{"approve": true}, "", key); status != http.StatusForbidden {
		t.Fatalf("API key approved a TV: %d", status)
	}
}

func TestTVSignInDenyAndExpire(t *testing.T) {
	server, base, admin := loginNewServer(t)
	tv := newJarClient()

	start := tvStart(t, tv, base, nil, http.StatusOK)
	postJSON[map[string]any](t, admin, base+"/api/auth/tv/code/"+start["code"].(string), map[string]bool{"approve": false}, http.StatusOK)
	if p := tvPoll(t, tv, base, start["secret"].(string)); p["status"] != "denied" {
		t.Fatalf("after deny: %v", p)
	}
	if p := tvPoll(t, tv, base, start["secret"].(string)); p["status"] != "expired" {
		t.Fatalf("after deny, again: %v", p)
	}

	// Five minutes: then it can't be approved and the TV is told.
	start = tvStart(t, tv, base, nil, http.StatusOK)
	server.TestShiftTVClock(5*time.Minute + time.Second)
	postJSON[map[string]any](t, admin, base+"/api/auth/tv/code/"+start["code"].(string), map[string]bool{"approve": true}, http.StatusNotFound)
	if p := tvPoll(t, tv, base, start["secret"].(string)); p["status"] != "expired" {
		t.Fatalf("after five minutes: %v", p)
	}
	// Approved but not picked up in time: gone too.
	start = tvStart(t, tv, base, nil, http.StatusOK)
	postJSON[map[string]any](t, admin, base+"/api/auth/tv/code/"+start["code"].(string), map[string]bool{"approve": true}, http.StatusOK)
	server.TestShiftTVClock(5*time.Minute + time.Second)
	if p := tvPoll(t, tv, base, start["secret"].(string)); p["status"] != "expired" {
		t.Fatalf("approved, then too late: %v", p)
	}
	if status, _ := doStatus(t, tv, http.MethodGet, base+"/api/auth/me"); status != http.StatusUnauthorized {
		t.Fatalf("signed in after expiry: %d", status)
	}
}

func TestTVSignInLimits(t *testing.T) {
	_, base, admin := loginNewServer(t)
	tv := newJarClient()

	// Three codes may wait from one address; a fourth is refused until one is replaced.
	var last string
	for range 3 {
		last = tvStart(t, tv, base, nil, http.StatusOK)["secret"].(string)
	}
	tvStart(t, tv, base, nil, http.StatusTooManyRequests)
	for range 6 { // 3 + 1 refused + 6 = 10 starts
		last = tvStart(t, tv, base, map[string]string{"replace": last}, http.StatusOK)["secret"].(string)
	}
	// Ten new codes per address in fifteen minutes.
	tvStart(t, tv, base, map[string]string{"replace": last}, http.StatusTooManyRequests)

	// Guessing codes on the phone runs out.
	for i := range 10 {
		postJSON[map[string]any](t, admin, base+"/api/auth/tv/code/ABCDEFG"+string("23456789AB"[i]), map[string]bool{"approve": true}, http.StatusNotFound)
	}
	postJSON[map[string]any](t, admin, base+"/api/auth/tv/code/ABCDEFGH", map[string]bool{"approve": true}, http.StatusTooManyRequests)
}

func TestTVSignInSwitchedOff(t *testing.T) {
	_, base, admin := loginNewServer(t)
	tv := newJarClient()
	start := tvStart(t, tv, base, nil, http.StatusOK)

	if s := getJSON[map[string]any](t, admin, base+"/api/auth/tv/settings"); s["enabled"] != true {
		t.Fatalf("not on by default: %v", s)
	}
	postJSONMethod[map[string]any](t, admin, http.MethodPut, base+"/api/auth/tv/settings", map[string]bool{"enabled": false}, http.StatusOK)
	tvStart(t, tv, base, nil, http.StatusForbidden)
	if m := getJSON[map[string]any](t, newJarClient(), base+"/api/auth/methods"); m["tvSignIn"] != false {
		t.Fatalf("methods: %v", m)
	}
	// A code made before can't be approved any more.
	postJSON[map[string]any](t, admin, base+"/api/auth/tv/code/"+start["code"].(string), map[string]bool{"approve": true}, http.StatusForbidden)
	postJSONMethod[map[string]any](t, admin, http.MethodPut, base+"/api/auth/tv/settings", map[string]bool{"enabled": true}, http.StatusOK)
	if p := tvPoll(t, tv, base, start["secret"].(string)); p["status"] != "expired" {
		t.Fatalf("an old code survived switching off: %v", p)
	}
}

func TestTVHandoffToPhone(t *testing.T) {
	server, base, admin := loginNewServer(t)
	postJSON[map[string]any](t, admin, base+"/api/users", map[string]any{"username": "kim", "password": "kim-password-1", "role": "member"}, http.StatusCreated)
	kim := newJarClient()
	postJSON[map[string]any](t, kim, base+"/api/auth/login", map[string]string{"username": "kim", "password": "kim-password-1"}, http.StatusOK)

	h := postJSON[map[string]any](t, kim, base+"/api/auth/handoff", map[string]string{"path": "/title/603?tab=reviews", "what": "your review"}, http.StatusOK)
	code, _ := h["code"].(string)
	if len(code) != 8 || !strings.HasSuffix(h["url"].(string), "/p/"+code) || len(h["qr"].([]any)) < 21 {
		t.Fatalf("handoff: %v", h)
	}
	if s := getJSON[map[string]any](t, kim, base+"/api/auth/handoff/"+code+"/status"); s["opened"] != false {
		t.Fatalf("status: %v", s)
	}
	// Another account can't use it, and trying doesn't spend it.
	if status, _ := doStatus(t, admin, http.MethodGet, base+"/api/auth/handoff/"+code); status != http.StatusForbidden {
		t.Fatalf("another account opened it: %d", status)
	}
	if status, _ := doStatus(t, newJarClient(), http.MethodGet, base+"/api/auth/handoff/"+code); status != http.StatusUnauthorized {
		t.Fatalf("opened without signing in: %d", status)
	}
	got := getJSON[map[string]any](t, kim, base+"/api/auth/handoff/"+code)
	if got["path"] != "/title/603?tab=reviews" || got["what"] != "your review" {
		t.Fatalf("open: %v", got)
	}
	if status, _ := doStatus(t, kim, http.MethodGet, base+"/api/auth/handoff/"+code); status != http.StatusNotFound {
		t.Fatalf("opened twice: %d", status)
	}
	if s := getJSON[map[string]any](t, kim, base+"/api/auth/handoff/"+code+"/status"); s["opened"] != true {
		t.Fatalf("status after: %v", s)
	}

	// Only paths inside the app.
	for _, p := range []string{"https://evil.example/", "//evil.example", "/\\evil.example", "javascript:alert(1)", "/../api/system/backup", "/api/system/backup", "title/1", ""} {
		postJSON[map[string]any](t, kim, base+"/api/auth/handoff", map[string]string{"path": p}, http.StatusBadRequest)
	}

	// Ten minutes, then it's gone.
	h = postJSON[map[string]any](t, kim, base+"/api/auth/handoff", map[string]string{"path": "/you/diary"}, http.StatusOK)
	server.TestShiftTVClock(10*time.Minute + time.Second)
	if status, _ := doStatus(t, kim, http.MethodGet, base+"/api/auth/handoff/"+h["code"].(string)); status != http.StatusNotFound {
		t.Fatalf("opened after ten minutes: %d", status)
	}
}
