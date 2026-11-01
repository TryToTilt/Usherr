package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/mediaservers"
	"github.com/rdborg/mediarium/internal/settings"
)

// fakePrerollPlex is a Plex whose pre-roll setting can be read and set.
type fakePrerollPlex struct {
	mu      sync.Mutex
	preroll string
	sets    int
}

func (f *fakePrerollPlex) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/:/prefs":
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"MediaContainer": map[string]any{"Setting": []map[string]any{
			{"id": "FriendlyName", "value": "JellyPlex"}, {"id": plexPrerollPref, "value": f.preroll},
		}}})
	case r.Method == http.MethodPut && r.URL.Path == "/:/prefs":
		f.preroll = r.URL.Query().Get(plexPrerollPref)
		f.sets++
	default:
		http.NotFound(w, r)
	}
}

func prerollServer(t *testing.T, f *fakePrerollPlex) (*Server, []mediaservers.Server) {
	t.Helper()
	ps := httptest.NewServer(f)
	t.Cleanup(ps.Close)
	s, db := optionsServer(t)
	s.db = db
	box, err := crypto.LoadOrCreateKey(filepath.Join(t.TempDir(), "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	s.Settings = settings.New(db, box)
	s.MediaServers = mediaservers.NewRepo(db, box)
	s.mediaClient = mediaservers.NewClient("test")
	srv, err := s.MediaServers.Create(mediaservers.Server{Name: "Plex", Kind: mediaservers.KindPlex, BaseURL: ps.URL, Token: "tok", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	return s, []mediaservers.Server{srv}
}

// Usherr gives Plex its pre-roll only where Plex has none or Usherr's own,
// never over one from elsewhere, and takes back only its own.
func TestPreshowPlexLeavesOthersPreroll(t *testing.T) {
	ctx := context.Background()
	f := &fakePrerollPlex{}
	s, servers := prerollServer(t, f)

	if note := s.preshowPlexGive(ctx, servers, `U:\Preshow\01 Welcome.mp4`); note != "" || f.preroll != `U:\Preshow\01 Welcome.mp4` {
		t.Fatalf("none before: %q, %q", note, f.preroll)
	}
	if note := s.preshowPlexGive(ctx, servers, `U:\Preshow\01 Popcorn.mp4`); note != "" || f.preroll != `U:\Preshow\01 Popcorn.mp4` {
		t.Fatalf("Usherr's own, replaced: %q, %q", note, f.preroll)
	}
	if s.preshowPlexGive(ctx, servers, ""); f.preroll != "" {
		t.Fatalf("off: Usherr's should be taken back: %q", f.preroll)
	}

	// Someone else's (NeXroll's) pre-roll: left, with a word why.
	f.preroll = `E:\NeXroll\Prerolls\halloween.mp4`
	sets := f.sets
	note := s.preshowPlexGive(ctx, servers, `U:\Preshow\01 Welcome.mp4`)
	if f.preroll != `E:\NeXroll\Prerolls\halloween.mp4` || f.sets != sets || !strings.Contains(note, "of its own") {
		t.Fatalf("another's pre-roll was touched: %q (%q)", f.preroll, note)
	}
	if s.preshowPlexGive(ctx, servers, ""); f.preroll != `E:\NeXroll\Prerolls\halloween.mp4` {
		t.Fatal("switching off took another's pre-roll away")
	}
}

// The drive's Preshow folder lists Usherr's own videos and reads them as
// they are on disk.
func TestDrivePreshowFolder(t *testing.T) {
	s, ts, _ := driveTestServer(t)
	dir := t.TempDir()
	clip := bytes.Repeat([]byte("preshow!"), 500)
	p := filepath.Join(dir, "welcome.mp4")
	if err := os.WriteFile(p, clip, 0o644); err != nil {
		t.Fatal(err)
	}
	setDrivePreshow([]driveLocal{{name: "01 Welcome.mp4", path: p, size: int64(len(clip)), mod: time.Now()}})
	t.Cleanup(func() { setDrivePreshow(nil) })

	resp, body := driveDo(t, s, ts, "PROPFIND", "/dav/Preshow/", "", map[string]string{"Depth": "1"})
	if resp.StatusCode != http.StatusMultiStatus || !strings.Contains(body, "01%20Welcome.mp4") {
		t.Fatalf("listing: %d %s", resp.StatusCode, body)
	}
	resp, body = driveDo(t, s, ts, "GET", "/dav/Preshow/01%20Welcome.mp4", "", map[string]string{"Range": "bytes=8-23"})
	if resp.StatusCode != http.StatusPartialContent || body != string(clip[8:24]) {
		t.Fatalf("range: %d %q", resp.StatusCode, body)
	}
	resp, body = driveDo(t, s, ts, "GET", "/dav/Preshow/01%20Welcome.mp4", "", nil)
	if resp.StatusCode != http.StatusOK || body != string(clip) {
		t.Fatalf("whole: %d, %d bytes", resp.StatusCode, len(body))
	}
}
