package lastfm_test

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/lastfm"
)

func TestSignSortsDropsFormatAndAddsSecret(t *testing.T) {
	v := url.Values{"method": {"auth.getSession"}, "api_key": {"k"}, "token": {"t"}, "format": {"json"}}
	sum := md5.Sum([]byte("api_keykmethodauth.getSessiontokentsecret"))
	if got := lastfm.Sign(v, "secret"); got != hex.EncodeToString(sum[:]) {
		t.Fatalf("sig: %q", got)
	}
	w := url.Values{"token": {"t"}, "method": {"auth.getSession"}, "api_key": {"k"}}
	if lastfm.Sign(v, "secret") != lastfm.Sign(w, "secret") {
		t.Fatal("format and order must not change the signature")
	}
	if lastfm.Sign(v, "secret") == lastfm.Sign(v, "other") {
		t.Fatal("the secret must change the signature")
	}
}

func TestShouldScrobbleFollowsLastFMsRule(t *testing.T) {
	for _, tc := range []struct {
		length, played float64
		want           bool
	}{
		{30, 30, false},   // 30 seconds or shorter never counts
		{31, 15, false},   // under half
		{31, 15.5, true},  // half
		{200, 99, false},  //
		{200, 100, true},  // half of a short track
		{600, 239, false}, // four minutes cap
		{600, 240, true},
		{0, 1000, false}, // unknown length
	} {
		if got := lastfm.ShouldScrobble(tc.length, tc.played); got != tc.want {
			t.Errorf("length %v played %v: %v", tc.length, tc.played, got)
		}
	}
}

// fakeLastFM checks every call's signature and answers like Last.fm.
func fakeLastFM(t *testing.T, calls *[]url.Values) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		v := r.Form
		*calls = append(*calls, v)
		if lastfm.Sign(v, "shh") != v.Get("api_sig") || v.Get("format") != "json" {
			w.Write([]byte(`{"error":13,"message":"Invalid method signature supplied"}`))
			return
		}
		switch v.Get("method") {
		case "auth.getSession":
			if r.Method != http.MethodGet {
				t.Errorf("getSession is a GET")
			}
			if v.Get("token") != "good" {
				w.Write([]byte(`{"error":14,"message":"Unauthorized Token"}`))
				return
			}
			w.Write([]byte(`{"session":{"name":"listener","key":"sk-1","subscriber":0}}`))
		case "track.scrobble":
			if v.Get("sk") == "revoked" {
				w.Write([]byte(`{"error":9,"message":"Invalid session key"}`))
				return
			}
			n := 0
			for k := range v {
				if strings.HasPrefix(k, "timestamp[") {
					n++
				}
			}
			w.Write([]byte(`{"scrobbles":{"@attr":{"accepted":` + string(rune('0'+n)) + `,"ignored":0}}}`))
		case "track.updateNowPlaying", "track.love", "track.unlove":
			w.Write([]byte(`{}`))
		default:
			w.Write([]byte(`{"error":3,"message":"Invalid Method"}`))
		}
	}))
}

func TestSessionScrobbleAndErrors(t *testing.T) {
	var calls []url.Values
	srv := fakeLastFM(t, &calls)
	defer srv.Close()
	c := lastfm.New("key", "shh", "test", lastfm.WithBaseURL(srv.URL+"/2.0/"), lastfm.WithHTTPClient(srv.Client()))
	ctx := context.Background()

	if u := c.AuthPageURL("https://usherr.local/cb?state=x"); !strings.HasPrefix(u, "https://www.last.fm/api/auth/?api_key=key&cb=https%3A%2F%2Fusherr.local%2Fcb%3Fstate%3Dx") {
		t.Fatalf("auth page: %s", u)
	}
	if _, _, err := c.Session(ctx, "bad"); !errors.Is(err, lastfm.ErrBadToken) {
		t.Fatalf("bad token: %v", err)
	}
	user, key, err := c.Session(ctx, "good")
	if err != nil || user != "listener" || key != "sk-1" {
		t.Fatalf("session: %q %q %v", user, key, err)
	}
	if err := c.NowPlaying(ctx, key, lastfm.Track{Artist: "Fixture Band", Title: "Opening", DurationSec: 215}); err != nil {
		t.Fatal(err)
	}
	last := calls[len(calls)-1]
	if last.Get("artist") != "Fixture Band" || last.Get("duration") != "215" || last.Get("sk") != "sk-1" {
		t.Fatalf("now playing: %v", last)
	}
	n, err := c.Scrobble(ctx, key, []lastfm.Track{{Artist: "A", Title: "One", Timestamp: 1700000000}, {Artist: "A", Title: "Two", Album: "Al", Timestamp: 1700000300}})
	if err != nil || n != 2 {
		t.Fatalf("scrobble: %d %v", n, err)
	}
	last = calls[len(calls)-1]
	if last.Get("track[1]") != "Two" || last.Get("album[1]") != "Al" || last.Get("timestamp[0]") != "1700000000" {
		t.Fatalf("batch: %v", last)
	}
	if _, err := c.Scrobble(ctx, "revoked", []lastfm.Track{{Artist: "A", Title: "One", Timestamp: 1}}); !errors.Is(err, lastfm.ErrBadSession) || lastfm.Temporary(err) {
		t.Fatalf("revoked: %v", err)
	}
	bad := lastfm.New("key", "wrong", "test", lastfm.WithBaseURL(srv.URL+"/2.0/"), lastfm.WithHTTPClient(srv.Client()))
	if err := bad.Love(ctx, key, "A", "One", true); !errors.Is(err, lastfm.ErrBadKey) {
		t.Fatalf("wrong secret: %v", err)
	}
	if err := c.Love(ctx, key, "A", "One", false); err != nil || calls[len(calls)-1].Get("method") != "track.unlove" {
		t.Fatalf("unlove: %v", err)
	}
}
