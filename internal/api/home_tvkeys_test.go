package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// The Live TV feeds' key is given out openly (Plex's lineup, the add-on), so
// it must not open the streamed films' pointer files, which have their own.
func TestTunerKeyDoesNotOpenStreams(t *testing.T) {
	s, _ := prerollServer(t, &fakePrerollPlex{})
	old := s.strmKey()
	tuner := s.tvFeedKey()
	if old == "" || tuner == "" || old == tuner {
		t.Fatalf("keys: strm %q, tuner %q", old, tuner)
	}
	req := func(key string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/x", nil)
		r.SetPathValue("key", key)
		return r
	}
	if s.strmOK(req(tuner)) {
		t.Fatal("the tuner's key opened a pointer file")
	}
	if !s.strmOK(req(old)) {
		t.Fatal("the pointer files' own key was refused")
	}
	if !s.tvFeedOK(req(tuner)) || !s.tvFeedOK(req(old)) {
		t.Fatal("Live TV should take the tuner's key and the older shared one")
	}
	if s.tvFeedOK(req("nope")) || s.strmOK(req("")) {
		t.Fatal("a wrong key got in")
	}
}
