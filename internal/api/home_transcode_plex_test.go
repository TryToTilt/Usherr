package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeTranscodePlex is a Plex with one film, Arrival, as Plex on Windows
// sees it, and its universal transcoder.
type fakeTranscodePlex struct {
	mu      sync.Mutex
	path    string // the decision's path
	profile string // the profile extra it was given
	stopped string // the session stopped
	keyLeak bool   // a request carried the token in its address
}

func (f *fakeTranscodePlex) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.URL.Query().Get("X-Plex-Token") != "" {
		f.keyLeak = true
	}
	if r.Header.Get("X-Plex-Token") != "tok" {
		http.Error(w, "no token", http.StatusUnauthorized)
		return
	}
	send := func(c map[string]any) { _ = json.NewEncoder(w).Encode(map[string]any{"MediaContainer": c}) }
	session := r.URL.Query().Get("session")
	switch {
	case r.URL.Path == "/library/sections":
		send(map[string]any{"Directory": []map[string]any{{"key": "2", "type": "show"}, {"key": "1", "type": "movie"}}})
	case r.URL.Path == "/library/sections/1/all":
		send(map[string]any{"Metadata": []map[string]any{
			{"ratingKey": "42", "Media": []map[string]any{{"Part": []map[string]any{{"file": `D:\Movies\Arrival (2016)\Arrival (2016).mkv`}}}}},
			{"ratingKey": "43", "Media": []map[string]any{{"Part": []map[string]any{{"file": `D:\Movies\Dune (2021)\Dune (2021).mkv`}}}}},
		}})
	case r.URL.Path == "/video/:/transcode/universal/decision":
		f.path, f.profile = r.URL.Query().Get("path"), r.Header.Get("X-Plex-Client-Profile-Extra")
		send(map[string]any{"generalDecisionCode": 1001, "generalDecisionText": "Direct play not available; Conversion OK."})
	case r.URL.Path == "/video/:/transcode/universal/start.m3u8":
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=20000000\nsession/" + session + "/base/index.m3u8?X-Plex-Token=tok\n"))
	case strings.HasSuffix(r.URL.Path, "/base/index.m3u8"):
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:4,\n00000.ts\n#EXTINF:4,\n/video/:/transcode/universal/" + strings.TrimPrefix(strings.TrimSuffix(r.URL.Path, "index.m3u8"), "/video/:/transcode/universal/") + "00001.ts\n"))
	case strings.HasSuffix(r.URL.Path, ".ts"):
		_, _ = w.Write([]byte("piece"))
	case r.URL.Path == "/video/:/transcode/universal/stop":
		f.stopped = session
	default:
		http.NotFound(w, r)
	}
}

// Plex converts a film Jellyfin can't: it's found by its folder and name
// (Plex sees D:\Movies, Usherr /data/movies), and its stream is relayed
// without Plex's token reaching the browser, only under its own session,
// and stopped when the player closes.
func TestPlexConvertsForThePlayer(t *testing.T) {
	f := &fakeTranscodePlex{}
	s, servers := prerollServer(t, &fakePrerollPlex{})
	ps := httptest.NewServer(f)
	defer ps.Close()
	plex := servers[0]
	plex.BaseURL = ps.URL
	if _, err := s.MediaServers.Update(plex); err != nil {
		t.Fatal(err)
	}
	srv, ok := s.plexServerForPlay()
	if !ok {
		t.Fatal("no Plex to convert with")
	}
	ctx := context.Background()

	if _, err := s.preparePlex(ctx, srv, "movie", "/data/movies/Not Here (2020)/Not Here (2020).mkv", browserCaps{}, true, 0); err == nil {
		t.Fatal("a file Plex doesn't have: no error, so no fall-back to Usherr's own conversion")
	}
	sess, err := s.preparePlex(ctx, srv, "movie", "/data/movies/Arrival (2016)/Arrival (2016).mkv", browserCaps{}, true, 0)
	if err != nil {
		t.Fatal(err)
	}
	if f.path != "/library/metadata/42" {
		t.Fatalf("decision asked for %q, want Arrival's /library/metadata/42", f.path)
	}
	if !strings.Contains(f.profile, "protocol=hls") || !strings.Contains(f.profile, "videoCodec=h264") {
		t.Fatalf("profile extra: %q", f.profile)
	}
	hlsSessions.Store(sess.token, sess)
	defer hlsSessions.Delete(sess.token)

	get := func(rest string) (int, string) {
		r := httptest.NewRequest(http.MethodGet, "/api/play/hls/"+sess.token+"/"+rest, nil)
		r.SetPathValue("token", sess.token)
		r.SetPathValue("rest", strings.SplitN(rest, "?", 2)[0])
		w := httptest.NewRecorder()
		s.handlePlayHLS(w, r)
		return w.Code, w.Body.String()
	}
	code, master := get("master.m3u8")
	if code != http.StatusOK || strings.Contains(master, "tok") || !strings.Contains(master, "session/"+sess.playSession+"/base/index.m3u8") {
		t.Fatalf("master playlist %d:\n%s", code, master)
	}
	code, index := get("session/" + sess.playSession + "/base/index.m3u8")
	if code != http.StatusOK || !strings.Contains(index, "\n00000.ts\n") || !strings.Contains(index, "/api/play/hls/"+sess.token+"/session/"+sess.playSession+"/base/00001.ts") {
		t.Fatalf("index playlist %d:\n%s", code, index)
	}
	if code, body := get("session/" + sess.playSession + "/base/00000.ts"); code != http.StatusOK || body != "piece" {
		t.Fatalf("a piece: %d %q", code, body)
	}
	if code, _ := get("session/someone-else/base/index.m3u8"); code != http.StatusBadRequest {
		t.Fatalf("another session's stream answered %d", code)
	}
	if code, _ := get("library/sections"); code != http.StatusBadRequest {
		t.Fatalf("the rest of Plex answered %d through the relay", code)
	}
	if f.keyLeak {
		t.Fatal("Plex's token went in an address")
	}

	r := httptest.NewRequest(http.MethodDelete, "/api/play/hls/"+sess.token, nil)
	r.SetPathValue("token", sess.token)
	w := httptest.NewRecorder()
	s.handleStopHLS(w, r)
	if w.Code != http.StatusNoContent || f.stopped != sess.playSession {
		t.Fatalf("closing the player: %d, Plex stopped %q, want %q", w.Code, f.stopped, sess.playSession)
	}
}

// The household chooses who converts first; the others stay in line behind
// it, and Usherr itself is always last (its own conversion isn't in the list).
func TestConvertOrder(t *testing.T) {
	s, _ := prerollServer(t, &fakePrerollPlex{})
	put := func(with string) int {
		w := httptest.NewRecorder()
		s.handlePutConvert(w, httptest.NewRequest(http.MethodPut, "/api/home/convert", strings.NewReader(`{"with":"`+with+`"}`)))
		return w.Code
	}
	for _, c := range []struct{ with, want string }{
		{"", "jellyfin,plex"},
		{"jellyfin", "jellyfin,plex"},
		{"plex", "plex,jellyfin"},
		{"usherr", ""},
	} {
		if code := put(c.with); code != http.StatusOK {
			t.Fatalf("choosing %q answered %d", c.with, code)
		}
		if got := strings.Join(s.convertOrder(), ","); got != c.want {
			t.Errorf("with %q: order %q, want %q", c.with, got, c.want)
		}
	}
	if code := put("vlc"); code != http.StatusBadRequest {
		t.Errorf("an unknown converter answered %d", code)
	}
	if info := s.convertInfo(); info.Plex != "Plex" || info.Jellyfin != "" {
		t.Errorf("connected servers: %+v", info)
	}
}

func TestPathTail(t *testing.T) {
	for _, c := range []struct {
		p    string
		n    int
		want string
	}{
		{`D:\Movies\Arrival (2016)\Arrival (2016).mkv`, 2, "arrival (2016)/arrival (2016).mkv"},
		{"/data/movies/Arrival (2016)/Arrival (2016).mkv", 2, "arrival (2016)/arrival (2016).mkv"},
		{`G:\TV Shows\Severance\Season 01\Severance S01E01.mkv`, 3, "severance/season 01/severance s01e01.mkv"},
		{"x.mkv", 2, ""},
	} {
		if got := pathTail(c.p, c.n); got != c.want {
			t.Errorf("pathTail(%q, %d) = %q, want %q", c.p, c.n, got, c.want)
		}
	}
}
