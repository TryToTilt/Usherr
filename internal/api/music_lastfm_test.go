package api_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/lastfm"
)

// fakeLastFMServer answers like ws.audioscrobbler.com and records the calls.
type fakeLastFMServer struct {
	*httptest.Server
	mu      sync.Mutex
	calls   []url.Values
	offline atomic.Bool
	revoked atomic.Bool
}

func newFakeLastFMServer(t *testing.T) *fakeLastFMServer {
	f := &fakeLastFMServer{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f.offline.Load() {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		_ = r.ParseForm()
		v := r.Form
		if lastfm.Sign(v, "shh") != v.Get("api_sig") {
			w.Write([]byte(`{"error":13,"message":"Invalid method signature supplied"}`))
			return
		}
		f.mu.Lock()
		f.calls = append(f.calls, v)
		f.mu.Unlock()
		if f.revoked.Load() && v.Get("sk") != "" {
			w.Write([]byte(`{"error":9,"message":"Invalid session key"}`))
			return
		}
		switch v.Get("method") {
		case "auth.getSession":
			if v.Get("token") != "good" {
				w.Write([]byte(`{"error":14,"message":"Unauthorized Token"}`))
				return
			}
			w.Write([]byte(`{"session":{"name":"fixture-listener","key":"sk-1"}}`))
		case "track.scrobble":
			n := 0
			for k := range v {
				if strings.HasPrefix(k, "timestamp[") {
					n++
				}
			}
			fmt.Fprintf(w, `{"scrobbles":{"@attr":{"accepted":%d,"ignored":0}}}`, n)
		default:
			w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(f.Close)
	return f
}

// count is how many calls of a method were made, and how many plays the
// scrobble calls carried.
func (f *fakeLastFMServer) count(method string) (calls, plays int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, v := range f.calls {
		if v.Get("method") != method {
			continue
		}
		calls++
		for k := range v {
			if strings.HasPrefix(k, "timestamp[") {
				plays++
			}
		}
	}
	return calls, plays
}

func TestLastFMConnectScrobbleRulesOfflineQueueAndLove(t *testing.T) {
	f := newFamilyMusic(t)
	fake := newFakeLastFMServer(t)
	f.server.TestSetLastFM(fake.URL + "/2.0/")
	if err := f.server.TestExec(`UPDATE tracks SET length_ms = 200000`); err != nil {
		t.Fatal(err)
	}
	track := f.trackIDs[0]

	st := getJSON[map[string]any](t, f.member, f.url("/api/music/lastfm"))
	if st["configured"] != false || st["connected"] != false {
		t.Fatalf("before setup: %+v", st)
	}
	postJSON[map[string]any](t, f.member, f.url("/api/music/lastfm/connect"), map[string]any{}, http.StatusConflict)
	postJSONMethod[map[string]any](t, f.member, http.MethodPut, f.url("/api/music/lastfm/app"), map[string]any{"apiKey": "key", "secret": "shh"}, http.StatusForbidden)
	app := postJSONMethod[map[string]any](t, f.admin, http.MethodPut, f.url("/api/music/lastfm/app"), map[string]any{"apiKey": "key", "secret": "shh"}, http.StatusOK)
	if app["apiKey"] != "key" || app["hasSecret"] != true || app["secret"] != nil {
		t.Fatalf("app: %+v", app)
	}

	// The web sign-in: Last.fm sends the person back with a token.
	con := postJSON[map[string]any](t, f.member, f.url("/api/music/lastfm/connect"), map[string]any{}, http.StatusOK)
	page, _ := url.Parse(con["url"].(string))
	if page.Host != "www.last.fm" || page.Query().Get("api_key") != "key" {
		t.Fatalf("auth page: %v", page)
	}
	cb, _ := url.Parse(page.Query().Get("cb"))
	state := cb.Query().Get("state")
	if !strings.HasSuffix(cb.Path, "/api/music/lastfm/callback") || state == "" {
		t.Fatalf("callback: %v", cb)
	}
	noFollow := *f.member
	noFollow.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	callback := func(c *http.Client, state, token string) string {
		t.Helper()
		resp, err := c.Get(f.url("/api/music/lastfm/callback?state=%s&token=%s", state, token))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusFound {
			t.Fatalf("callback status %d", resp.StatusCode)
		}
		return resp.Header.Get("Location")
	}
	// Someone else can't finish the member's sign-in.
	adminNoFollow := *f.admin
	adminNoFollow.CheckRedirect = noFollow.CheckRedirect
	if loc := callback(&adminNoFollow, state, "good"); !strings.Contains(loc, "lastfm=expired") {
		t.Fatalf("another account used the state: %s", loc)
	}
	con = postJSON[map[string]any](t, f.member, f.url("/api/music/lastfm/connect"), map[string]any{}, http.StatusOK)
	page, _ = url.Parse(con["url"].(string))
	cb, _ = url.Parse(page.Query().Get("cb"))
	state = cb.Query().Get("state")
	if loc := callback(&noFollow, state, "good"); !strings.Contains(loc, "lastfm=ok") {
		t.Fatalf("sign-in: %s", loc)
	}
	if loc := callback(&noFollow, state, "good"); !strings.Contains(loc, "lastfm=expired") {
		t.Fatalf("a state works once: %s", loc)
	}
	st = getJSON[map[string]any](t, f.member, f.url("/api/music/lastfm"))
	if st["connected"] != true || st["user"] != "fixture-listener" || st["enabled"] != true {
		t.Fatalf("connected: %+v", st)
	}

	// Now playing, then plays judged by Last.fm's rule (a 200 s track counts at 100 s).
	got := postJSON[map[string]any](t, f.member, f.url("/api/music/scrobble/playing"), map[string]any{"trackId": track}, http.StatusOK)
	if got["lastfm"] != true {
		t.Fatalf("now playing: %+v", got)
	}
	if n, _ := fake.count("track.updateNowPlaying"); n != 1 {
		t.Fatalf("now playing calls: %d", n)
	}
	now := time.Now().Unix()
	listen := func(played float64) map[string]any {
		return postJSON[map[string]any](t, f.member, f.url("/api/music/scrobble/listen"), map[string]any{"trackId": track, "listenedAt": now - 300, "playedSec": played}, http.StatusOK)
	}
	if got := listen(50); got["lastfm"] != false {
		t.Fatalf("under half counted: %+v", got)
	}
	if got := listen(100); got["lastfm"] != true {
		t.Fatalf("half: %+v", got)
	}
	if _, plays := fake.count("track.scrobble"); plays != 1 {
		t.Fatalf("plays sent: %d", plays)
	}

	// Offline: kept, then sent by the retry job.
	fake.offline.Store(true)
	if got := listen(150); got["lastfm"] != false {
		t.Fatalf("offline: %+v", got)
	}
	st = getJSON[map[string]any](t, f.member, f.url("/api/music/lastfm"))
	if st["pending"] != float64(1) || st["lastError"] == "" {
		t.Fatalf("kept: %+v", st)
	}
	fake.offline.Store(false)
	f.server.RunScrobbleRetry()
	st = getJSON[map[string]any](t, f.member, f.url("/api/music/lastfm"))
	if st["pending"] != float64(0) || st["submitted"] != float64(2) || st["lastError"] != nil {
		t.Fatalf("after retry: %+v", st)
	}

	// A 25-second track never goes to Last.fm, but is still in the history.
	if err := f.server.TestExec(`UPDATE tracks SET length_ms = 25000 WHERE id = ?`, f.trackIDs[1]); err != nil {
		t.Fatal(err)
	}
	if got := postJSON[map[string]any](t, f.member, f.url("/api/music/scrobble/listen"), map[string]any{"trackId": f.trackIDs[1], "listenedAt": now - 60, "playedSec": 25}, http.StatusOK); got["lastfm"] != false {
		t.Fatalf("short track: %+v", got)
	}
	var history int
	if err := f.server.TestDB().QueryRow(`SELECT COUNT(*) FROM music_play_history`).Scan(&history); err != nil || history != 4 {
		t.Fatalf("history: %d %v", history, err)
	}

	// Likes: love sync is a switch.
	postJSONMethod[map[string]any](t, f.member, http.MethodPut, f.url("/api/music/favorites/track/%d", track), nil, http.StatusOK)
	time.Sleep(200 * time.Millisecond)
	if n, _ := fake.count("track.love"); n != 0 {
		t.Fatal("loved without love sync")
	}
	postJSONMethod[map[string]any](t, f.member, http.MethodPut, f.url("/api/music/lastfm"), map[string]any{"loveSync": true}, http.StatusOK)
	postJSONMethod[map[string]any](t, f.member, http.MethodDelete, f.url("/api/music/favorites/track/%d", track), nil, http.StatusOK)
	postJSONMethod[map[string]any](t, f.member, http.MethodPut, f.url("/api/music/favorites/album/%d", f.albumID), nil, http.StatusOK)
	postJSONMethod[map[string]any](t, f.member, http.MethodPut, f.url("/api/music/favorites/track/%d", track), nil, http.StatusOK)
	deadline := time.Now().Add(3 * time.Second)
	for {
		loves, _ := fake.count("track.love")
		unloves, _ := fake.count("track.unlove")
		if loves == 1 && unloves == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("love sync: %d loves, %d unloves", loves, unloves)
		}
		time.Sleep(20 * time.Millisecond)
	}
	favs := getJSON[map[string]any](t, f.member, f.url("/api/music/favorites"))
	if fmt.Sprint(favs["tracks"]) != fmt.Sprintf("[%d]", track) || fmt.Sprint(favs["albums"]) != fmt.Sprintf("[%d]", f.albumID) {
		t.Fatalf("favourites: %+v", favs)
	}
	if mine := getJSON[map[string]any](t, f.admin, f.url("/api/music/favorites")); len(mine["tracks"].([]any)) != 0 {
		t.Fatalf("likes are per account: %+v", mine)
	}
	postJSONMethod[map[string]any](t, f.member, http.MethodPut, f.url("/api/music/favorites/track/999999"), nil, http.StatusNotFound)
	postJSONMethod[map[string]any](t, f.member, http.MethodPut, f.url("/api/music/favorites/song/1"), nil, http.StatusBadRequest)

	// A revoked session stops sending until the person connects again.
	fake.revoked.Store(true)
	listen(150)
	st = getJSON[map[string]any](t, f.member, f.url("/api/music/lastfm"))
	if st["enabled"] != false || !strings.Contains(st["lastError"].(string), "Connect again") {
		t.Fatalf("revoked: %+v", st)
	}
	req, _ := http.NewRequest(http.MethodDelete, f.url("/api/music/lastfm"), nil)
	if resp, err := f.member.Do(req); err != nil || resp.StatusCode != http.StatusNoContent {
		t.Fatalf("disconnect: %v %v", resp, err)
	}
}
