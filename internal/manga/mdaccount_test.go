package manga

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

func set(keys ...string) map[string]bool {
	out := map[string]bool{}
	for _, k := range keys {
		out[k] = true
	}
	return out
}

func TestMergeSetsKeepsEachSidesChange(t *testing.T) {
	scope := set("1", "2", "3", "4", "5", "6")
	here := set("1", "2", "4")  // 4 read here since; 3 unread here since
	there := set("1", "3", "5") // 5 read there since; 2 unread there since
	synced := set("1", "2", "3")
	got := MergeSets(here, there, synced, scope)
	want := set("1", "4", "5")
	if fmt.Sprint(SortedKeys(got)) != fmt.Sprint(SortedKeys(want)) {
		t.Fatalf("merged %v, want %v", SortedKeys(got), SortedKeys(want))
	}
	// Outside the scope, MangaDex's word stands.
	got = MergeSets(set(), set("9"), set(), set())
	if !got["9"] {
		t.Fatal("a mark outside the scope was dropped")
	}
	// The first sync, with nothing agreed yet, takes both sides.
	got = MergeSets(set("1"), set("2"), set(), set("1", "2"))
	if !got["1"] || !got["2"] {
		t.Fatalf("first sync lost a side: %v", got)
	}
}

func TestPlanReads(t *testing.T) {
	m := DexChapterMap{
		Number: map[string]string{"a1": "1", "a2": "2", "b2": "2", "a3": "3", "a4": "4"},
		IDs:    map[string][]string{"1": {"a1"}, "2": {"a2", "b2"}, "3": {"a3"}, "4": {"a4"}},
		Volume: map[string]string{"1": "1", "2": "1", "3": "2", "4": "2"},
	}
	lr := LocalReading{Read: set("1", "2", "3", "7"), Shown: set("1", "2", "3", "7")} // 7: a chapter from a file, unknown there
	plan := PlanReads(lr, []string{"b2", "zz"}, set("2"), m)
	// 1 and 3 were read here: up they go. 2 was read there and agreed before: it stays.
	if fmt.Sprint(plan.Read) != "[a1 a3]" || len(plan.Unread) != 0 {
		t.Fatalf("plan %+v", plan)
	}
	if plan.Scope["7"] || !plan.Merged["2"] {
		t.Fatalf("scope %v merged %v", plan.Scope, plan.Merged)
	}
	// Unread here after being agreed: unread there too, every upload of it.
	lr = LocalReading{Read: set(), Shown: set("2")}
	plan = PlanReads(lr, []string{"a2", "b2"}, set("2"), m)
	if fmt.Sprint(plan.Unread) != "[a2 b2]" || plan.Merged["2"] {
		t.Fatalf("unread plan %+v", plan)
	}
}

func TestMergeStatus(t *testing.T) {
	for _, c := range []struct{ here, there, synced, want string }{
		{"reading", "reading", "reading", "reading"},
		{"completed", "reading", "reading", "completed"}, // changed here
		{"reading", "dropped", "reading", "dropped"},     // changed there
		{"", "plan_to_read", "", "plan_to_read"},
		{"on_hold", "dropped", "reading", "on_hold"}, // both: Usherr's
	} {
		if got := MergeStatus(c.here, c.there, c.synced); got != c.want {
			t.Fatalf("%+v: got %q", c, got)
		}
	}
}

// fakeAuthDex answers the sign-in and a few account calls, and counts them.
type fakeAuthDex struct {
	*httptest.Server
	mu        sync.Mutex
	grants    []string // grant types asked for
	authSeen  map[string]string
	refuse    bool // the sign-in is turned down
	reject401 bool // every account call answers 401
	noStatus  bool // the account has no reading statuses: MangaDex sends []
}

func newFakeAuthDex(t *testing.T) *fakeAuthDex {
	f := &fakeAuthDex{authSeen: map[string]string{}}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.authSeen[r.URL.Path] = r.Header.Get("Authorization")
		switch {
		case r.URL.Path == "/auth/token":
			body, _ := io.ReadAll(r.Body)
			form, _ := url.ParseQuery(string(body))
			f.grants = append(f.grants, form.Get("grant_type"))
			if r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" || form.Get("client_id") != "cid" || form.Get("client_secret") != "sec" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"error":"invalid_client"}`)
				return
			}
			if f.refuse || (form.Get("grant_type") == "password" && form.Get("password") != "pw") {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"error":"invalid_grant"}`)
				return
			}
			_, _ = io.WriteString(w, `{"access_token":"tok","refresh_token":"ref","expires_in":900}`)
		case f.reject401 && r.Header.Get("Authorization") != "":
			w.WriteHeader(http.StatusUnauthorized)
		case r.URL.Path == "/manga/status":
			if r.Header.Get("Authorization") != "Bearer tok" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if f.noStatus {
				_, _ = io.WriteString(w, `{"result":"ok","statuses":[]}`)
				return
			}
			_, _ = io.WriteString(w, `{"result":"ok","statuses":{"AAAAAAAA-aaaa-aaaa-aaaa-aaaaaaaaaaaa":"reading","bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb":"nonsense"}}`)
		case r.URL.Path == "/manga":
			_, _ = io.WriteString(w, `{"data":[{"id":"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa","attributes":{"title":{"en":"Tower"},"contentRating":"safe"},"relationships":[{"id":"c","type":"cover_art","attributes":{"fileName":"x.jpg"}}]}]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func testClient(f *fakeAuthDex) *MangaDex {
	c := NewMangaDex("Usherr/test", f.URL)
	c.APIGap, c.AtHomeGap, c.ImageGap = 0, 0, 0
	return c
}

func TestDexSessionSignsInOnceAndUsesTheToken(t *testing.T) {
	f := newFakeAuthDex(t)
	c := testClient(f)
	var kept string
	sess := NewDexSession(c, DexCredentials{ClientID: "cid", ClientSecret: "sec", Username: "me", Password: "pw"}, "")
	sess.OnRefresh = func(r string) { kept = r }
	got, err := sess.ReadingStatuses(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got["aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"] != "reading" || len(got) != 1 {
		t.Fatalf("statuses %v", got)
	}
	if _, err := sess.ReadingStatuses(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(f.grants) != "[password]" || kept != "ref" {
		t.Fatalf("grants %v, kept %q", f.grants, kept)
	}
	// A public call carries no login, and covers are named on the uploads server.
	titles, err := c.FollowTitlesByID(context.Background(), []string{"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"})
	if err != nil || len(titles) != 1 {
		t.Fatalf("titles %v %v", titles, err)
	}
	if f.authSeen["/manga"] != "" {
		t.Fatal("a public call carried the login")
	}
	if titles[0].Cover != "https://uploads.mangadex.org/covers/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa/x.jpg.256.jpg" {
		t.Fatalf("cover %q", titles[0].Cover)
	}
}

func TestDexSessionUsesAKeptRefreshToken(t *testing.T) {
	f := newFakeAuthDex(t)
	sess := NewDexSession(testClient(f), DexCredentials{ClientID: "cid", ClientSecret: "sec", Username: "me", Password: "pw"}, "old")
	if _, err := sess.ReadingStatuses(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(f.grants) != "[refresh_token]" {
		t.Fatalf("grants %v", f.grants)
	}
}

func TestDexSessionStopsWhenTurnedDown(t *testing.T) {
	f := newFakeAuthDex(t)
	sess := NewDexSession(testClient(f), DexCredentials{ClientID: "cid", ClientSecret: "sec", Username: "me", Password: "wrong"}, "")
	for i := 0; i < 3; i++ {
		if _, err := sess.ReadingStatuses(context.Background()); !errors.Is(err, ErrDexSignIn) {
			t.Fatalf("try %d: %v", i, err)
		}
	}
	if len(f.grants) != 1 || !sess.Refused() {
		t.Fatalf("asked %d times after being turned down", len(f.grants))
	}
}

func TestDexSession401GetsOneNewTokenThenStops(t *testing.T) {
	f := newFakeAuthDex(t)
	f.reject401 = true
	sess := NewDexSession(testClient(f), DexCredentials{ClientID: "cid", ClientSecret: "sec", Username: "me", Password: "pw"}, "")
	if _, err := sess.ReadingStatuses(context.Background()); !errors.Is(err, ErrDexSignIn) {
		t.Fatalf("got %v", err)
	}
	if _, err := sess.ReadingStatuses(context.Background()); !errors.Is(err, ErrDexSignIn) {
		t.Fatalf("got %v", err)
	}
	if len(f.grants) != 2 { // one sign-in, one renewal, then nothing
		t.Fatalf("grants %v", f.grants)
	}
}

func TestDexAuthOnlyGoesToMangaDex(t *testing.T) {
	for target, ok := range map[string]bool{
		"https://api.mangadex.org/manga/status":            true,
		"https://auth.mangadex.org/realms/x":               true,
		"https://uploads.mangadex.org/covers/x":            false,
		"https://cmdxd98sb0x3yprd.mangadex.network/data/x": false,
		"http://api.mangadex.org/manga":                    false,
		"https://evil.example/api.mangadex.org":            false,
	} {
		if got := dexAuthHost(target, dexBaseURL); got != ok {
			t.Errorf("%s: %v", target, got)
		}
	}
	if !strings.HasPrefix(DexAuthURLFor(NewMangaDex("x", "")), "https://auth.mangadex.org/") {
		t.Fatal("the real client signs in elsewhere")
	}
}

// MangaDex answers an account with no statuses with [] for the map.
func TestDexReadingStatusesEmptyList(t *testing.T) {
	f := newFakeAuthDex(t)
	f.noStatus = true
	sess := NewDexSession(testClient(f), DexCredentials{ClientID: "cid", ClientSecret: "sec", Username: "me", Password: "pw"}, "")
	got, err := sess.ReadingStatuses(context.Background())
	if err != nil || len(got) != 0 {
		t.Fatalf("statuses %v, %v", got, err)
	}
}
