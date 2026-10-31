package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/mediaservers"
	"github.com/rdborg/mediarium/internal/settings"
)

// fakeLibraryPlex is a Plex whose libraries are given, and that records
// the libraries it's asked to make.
type fakeLibraryPlex struct {
	mu      sync.Mutex
	libs    []map[string]any
	created []string // "name|type|agent|scanner|location"
}

func (f *fakeLibraryPlex) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/library/sections":
		_ = json.NewEncoder(w).Encode(map[string]any{"MediaContainer": map[string]any{"Directory": f.libs}})
	case r.Method == http.MethodPost && r.URL.Path == "/library/sections":
		q := r.URL.Query()
		f.created = append(f.created, strings.Join([]string{q.Get("name"), q.Get("type"), q.Get("agent"), q.Get("scanner"), strings.Join(q["location"], ";")}, "|"))
	default:
		http.NotFound(w, r)
	}
}

func drivePlexServer(t *testing.T, f *fakeLibraryPlex) *Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	s, db := optionsServer(t)
	s.db = db
	box, err := crypto.LoadOrCreateKey(filepath.Join(t.TempDir(), "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	s.Settings = settings.New(db, box)
	s.MediaServers = mediaservers.NewRepo(db, box)
	s.mediaClient = mediaservers.NewClient("test")
	if _, err := s.MediaServers.Create(mediaservers.Server{Name: "Plex", Kind: mediaservers.KindPlex, BaseURL: srv.URL, Token: "tok", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	_ = s.Settings.Set(keyDriveOn, "1", false)
	return s
}

func postDrivePlex(t *testing.T, s *Server) map[string]any {
	t.Helper()
	w := httptest.NewRecorder()
	s.handleDrivePlexLibraries(w, httptest.NewRequest(http.MethodPost, "/api/home/drive/plex", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return out
}

// A new Plex household: Usherr makes the drive's three libraries, each with
// Plex's own agent and scanner for films or shows.
func TestDriveMakesPlexLibraries(t *testing.T) {
	f := &fakeLibraryPlex{libs: []map[string]any{{"key": "1", "type": "movie", "title": "Movies", "Location": []map[string]any{{"path": `D:\Movies`}}}}}
	s := drivePlexServer(t, f)
	postDrivePlex(t, s)
	want := []string{
		`Movies-Stream|movie|tv.plex.agents.movie|Plex Movie|U:\Movies`,
		`TV-Stream|show|tv.plex.agents.series|Plex TV Series|U:\TV`,
		`Anime-Stream|show|tv.plex.agents.series|Plex TV Series|U:\Anime`,
	}
	if strings.Join(f.created, "\n") != strings.Join(want, "\n") {
		t.Errorf("made:\n%s\nwant:\n%s", strings.Join(f.created, "\n"), strings.Join(want, "\n"))
	}
}

// A household that made them by hand (TV and anime in one library) gets
// nothing new.
func TestDriveLeavesPlexLibrariesThatAreThere(t *testing.T) {
	f := &fakeLibraryPlex{libs: []map[string]any{
		{"key": "4", "type": "movie", "title": "Movies-Stream", "Location": []map[string]any{{"path": `U:\Movies`}}},
		{"key": "5", "type": "show", "title": "TV-Stream", "Location": []map[string]any{{"path": `U:\TV`}, {"path": `U:\Anime`}}},
	}}
	s := drivePlexServer(t, f)
	out := postDrivePlex(t, s)
	if len(f.created) != 0 {
		t.Errorf("made %v; they were all there", f.created)
	}
	if b, _ := json.Marshal(out); !strings.Contains(string(b), `"already":["Movies-Stream","TV-Stream","TV-Stream"]`) {
		t.Errorf("answer: %s", b)
	}
}
