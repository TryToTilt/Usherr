package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var testModTime = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

// fakeStreamNZB answers like StreamNZB: its profiles, a title's streams,
// "play this NZB", a play link (with a redirect, as it does when it moves
// to another release) and closing a session.
func fakeStreamNZB(t *testing.T, closed *[]string) *httptest.Server {
	t.Helper()
	video := []byte(strings.Repeat("0123456789", 1000))
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization") == "Bearer admintoken"
		switch {
		case r.URL.Path == "/api/streams":
			if !auth {
				http.Error(w, "no", http.StatusUnauthorized)
				return
			}
			_, _ = io.WriteString(w, `[{"username":"Home TV","token":"tok4k","filter_profile_name":"Living Room 4K"},
				{"username":"Sam","token":"tok1080","filter_profile_name":"Travel 1080p"},
				{"username":"Phone","token":"tok720","filter_profile_name":"Phone 720p"}]`)
		case r.URL.Path == "/tok1080/stream/series/tt4236770:1:4.json":
			_, _ = io.WriteString(w, `{"streams":[{"name":"StreamNZB 1080p","title":"Yellowstone.2018.S01E04.1080p.BluRay.x264-ROVERS\n2.1 GB","url":"http://192.168.1.20:7000/play/abc"}]}`)
		case r.URL.Path == "/api/play/nzb":
			if !auth || r.ParseMultipartForm(1<<20) != nil || r.FormValue("url") != "https://indexer.example/get/1" {
				http.Error(w, "bad", http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(w, `{"session_id":"s1","play_path":"/play/s1","play_url":"http://192.168.1.20:7000/play/s1","external_play_url":"http://192.168.1.20:7000/play/s1"}`)
		case r.URL.Path == "/play/abc" || r.URL.Path == "/play/s1":
			http.Redirect(w, r, "/media/file.mkv", http.StatusFound)
		case r.URL.Path == "/media/file.mkv":
			http.ServeContent(w, r, "file.mkv", testModTime, strings.NewReader(string(video)))
		case r.URL.Path == "/api/sessions/close":
			b, _ := io.ReadAll(r.Body)
			*closed = append(*closed, string(b))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestStreamNZBClient(t *testing.T) {
	var closed []string
	srv := fakeStreamNZB(t, &closed)
	c := &streamNZB{base: srv.URL, token: "admintoken", http: srv.Client()}
	ctx := context.Background()

	profiles, err := c.profiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for q, want := range map[string]string{"4K": "Home TV", "1080p": "Sam", "720p": "Phone"} {
		if p, ok := profileFor(profiles, q); !ok || p.Name != want {
			t.Errorf("profileFor(%s) = %+v, want %s", q, p, want)
		}
	}
	list, err := c.titleStreams(ctx, "tok1080", "series", "tt4236770:1:4")
	if err != nil || len(list) != 1 {
		t.Fatalf("titleStreams = %v, %v", list, err)
	}
	if got := c.internalURL(list[0].URL); got != srv.URL+"/play/abc" {
		t.Errorf("internalURL = %s", got)
	}
	res, err := c.playNZB(ctx, "https://indexer.example/get/1")
	if err != nil || res.SessionID != "s1" {
		t.Fatalf("playNZB = %+v, %v", res, err)
	}
	c.closeSession(ctx, "s1")
	if len(closed) != 1 || !strings.Contains(closed[0], `"s1"`) {
		t.Errorf("close = %v", closed)
	}
}

func TestStreamVideoRelaysRanges(t *testing.T) {
	var closed []string
	srv := fakeStreamNZB(t, &closed)
	s := &Server{}
	streams.mu.Lock()
	streams.sessions["str_test"] = &streamSession{id: "str_test", target: srv.URL + "/play/s1", userID: 0, last: time.Now()}
	streams.mu.Unlock()
	t.Cleanup(func() {
		streams.mu.Lock()
		delete(streams.sessions, "str_test")
		streams.mu.Unlock()
	})

	req := httptest.NewRequest(http.MethodGet, "/api/stream/str_test/video", nil)
	req.SetPathValue("id", "str_test")
	req.Header.Set("Range", "bytes=10-19")
	rec := httptest.NewRecorder()
	s.handleStreamVideo(rec, req)
	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status %d, want 206: %s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "0123456789" || rec.Header().Get("Content-Range") != "bytes 10-19/10000" {
		t.Errorf("body %q range %q", rec.Body.String(), rec.Header().Get("Content-Range"))
	}

	// Another person's session is not found.
	req2 := httptest.NewRequest(http.MethodGet, "/api/stream/nope/video", nil)
	req2.SetPathValue("id", "nope")
	rec2 := httptest.NewRecorder()
	s.handleStreamVideo(rec2, req2)
	if rec2.Code != http.StatusNotFound {
		t.Errorf("unknown session: %d", rec2.Code)
	}
}

func TestProfileQuality(t *testing.T) {
	for in, want := range map[string]string{"Living Room 4K": "4K", "Travel 1080p": "1080p", "Phone 720p": "720p", "Kids": "", "UHD 2160p": "4K"} {
		if got := profileQuality(in); got != want {
			t.Errorf("profileQuality(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStremioLinkAndQuoting(t *testing.T) {
	if l, _ := stremioLink("movie", "tt2953050", 0, 0); l != "stremio:///detail/movie/tt2953050/tt2953050" {
		t.Errorf("movie link %s", l)
	}
	if l, _ := stremioLink("series", "tt4236770", 1, 4); l != "stremio:///detail/series/tt4236770/tt4236770:1:4" {
		t.Errorf("series link %s", l)
	}
	if _, err := stremioLink("movie", "tt1; rm -rf /", 0, 0); err == nil {
		t.Error("a bad id must be refused")
	}
	if q := shellQuote("http://h/p?a=1&b='x'"); q != `'http://h/p?a=1&b='\''x'\'''` {
		t.Errorf("quote %s", q)
	}
}
