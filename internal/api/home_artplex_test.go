package api

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/mediaservers"
)

// fakeArtPlex is a Plex with one film (Dune, TMDB 438631) that has a
// poster and no background; it keeps what's uploaded and locked.
type fakeArtPlex struct {
	mu      sync.Mutex
	thumb   []byte
	uploads int
	locked  []string
}

func (f *fakeArtPlex) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("X-Plex-Token") != "tok" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	js := func(c map[string]any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"MediaContainer": c})
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/library/sections":
		js(map[string]any{"Directory": []map[string]any{{"key": "1", "type": "movie", "title": "Movies"}}})
	case r.Method == http.MethodGet && r.URL.Path == "/library/sections/1/all":
		js(map[string]any{"Metadata": []map[string]any{{"ratingKey": "77", "type": "movie", "title": "Dune", "year": 2021, "thumb": "/library/metadata/77/thumb/1", "Guid": []map[string]any{{"id": "tmdb://438631"}}}}})
	case r.Method == http.MethodGet && r.URL.Path == "/library/metadata/77/thumb":
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(f.thumb)
	case r.Method == http.MethodGet && r.URL.Path == "/library/metadata/77/art":
		http.NotFound(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/library/metadata/77/posters":
		f.thumb, _ = io.ReadAll(r.Body)
		f.uploads++
	case r.Method == http.MethodPut && r.URL.Path == "/library/sections/1/all":
		q := r.URL.Query()
		f.locked = append(f.locked, q.Get("type")+"|"+q.Get("id")+"|"+q.Get("thumb.locked"))
	case r.Method == http.MethodGet && r.URL.Path == "/status/sessions":
		js(map[string]any{})
	default:
		http.NotFound(w, r)
	}
}

// A Plex-only household: Re-Posterr reads Plex's titles, puts a poster on
// (uploaded, then locked so Plex's refresh keeps it), keeps the one it
// replaced, and puts that back on undo.
func TestArtWithPlex(t *testing.T) {
	s, db := optionsServer(t)
	s.db = db
	box, err := crypto.LoadOrCreateKey(filepath.Join(t.TempDir(), "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	s.MediaServers = mediaservers.NewRepo(db, box)
	s.mediaClient = mediaservers.NewClient("test")
	_ = s.Settings.Set(keyArtFolder, t.TempDir(), false)
	old := pngOf(t, 40, 60, color.RGBA{R: 200, A: 255})
	fp := &fakeArtPlex{thumb: old}
	ps := httptest.NewServer(fp)
	defer ps.Close()
	plex, err := s.MediaServers.Create(mediaservers.Server{Name: "Plex", Kind: mediaservers.KindPlex, BaseURL: ps.URL, Token: "tok", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	big := pngOf(t, 2000, 3000, color.RGBA{G: 200, A: 255})
	pics := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(big) }))
	defer pics.Close()
	oldHTTP, oldHost := artHTTP, artTestHost
	artHTTP = func() *http.Client { return pics.Client() }
	artTestHost = func(u string) bool { return strings.HasPrefix(u, pics.URL) }
	defer func() { artHTTP, artTestHost = oldHTTP, oldHost }()

	srv, ok := s.artServer()
	if !ok {
		t.Fatal("Plex alone should be somewhere to write")
	}
	ctx := context.Background()
	items, err := s.artItems(ctx, srv, true)
	if err != nil || len(items) != 1 {
		t.Fatalf("items: %+v %v", items, err)
	}
	if err := s.artSync(ctx, items); err != nil {
		t.Fatal(err)
	}
	var ids, missing string
	if err := db.QueryRow(`SELECT jf_ids, missing FROM art_titles WHERE kind = 'movie' AND tmdb_id = 438631`).Scan(&ids, &missing); err != nil {
		t.Fatal(err)
	}
	want := artPlexRef{plex.ID, "77", "1", "movie"}.String()
	if ids != want || missing != "backdrop" {
		t.Fatalf("the film: %q %q (want %q, backdrop: Plex has no logos to fill)", ids, missing, want)
	}
	if w := s.artImageWidth(ctx, srv, ids, "poster"); w != 40 {
		t.Errorf("the poster's width: %d", w)
	}

	res, err := db.Exec(`INSERT INTO art_changes (kind, tmdb_id, title, type, source, url, why, created_ms) VALUES ('movie', 438631, 'Dune', 'poster', 'tmdb', ?, 'small', 1)`, pics.URL+"/p.png")
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	if err := s.artMake(ctx, srv, id); err != nil {
		t.Fatal(err)
	}
	if fp.uploads != 1 || len(fp.locked) != 1 || fp.locked[0] != "1|77|1" {
		t.Fatalf("uploads %d, locked %v", fp.uploads, fp.locked)
	}
	got, _, err := image.Decode(bytes.NewReader(fp.thumb))
	if err != nil || got.Bounds().Dx() != artPosterW {
		t.Fatalf("sent at the poster size: %v %v", got.Bounds(), err)
	}
	list, _ := s.artChanges(ctx, `WHERE c.id = ?`, id)
	if len(list) != 1 || list[0].State != "done" || list[0].Backups == "{}" {
		t.Fatalf("change: %+v", list)
	}

	if err := s.artUndo(ctx, srv, list[0]); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(fp.thumb, old) || fp.uploads != 2 {
		t.Fatal("the old poster isn't back in Plex")
	}
}

// A title in Jellyfin and in Plex: a change for every server goes to both;
// with different pictures for each, a change for one leaves the other.
func TestArtPerServer(t *testing.T) {
	s, db := optionsServer(t)
	s.db = db
	box, err := crypto.LoadOrCreateKey(filepath.Join(t.TempDir(), "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	s.MediaServers = mediaservers.NewRepo(db, box)
	s.mediaClient = mediaservers.NewClient("test")
	_ = s.Settings.Set(keyArtFolder, t.TempDir(), false)
	old := pngOf(t, 40, 60, color.RGBA{R: 200, A: 255})
	fp := &fakeArtPlex{thumb: old}
	ps := httptest.NewServer(fp)
	defer ps.Close()
	plex, err := s.MediaServers.Create(mediaservers.Server{Name: "Plex", Kind: mediaservers.KindPlex, BaseURL: ps.URL, Token: "tok", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	jf := &fakeJF{images: map[string][]byte{"a1/Primary": old}, posted: map[string]int{}}
	js := httptest.NewServer(jf)
	defer js.Close()
	srv := mediaservers.Server{Kind: mediaservers.KindJellyfin, BaseURL: js.URL, Token: "k", Enabled: true}
	big := pngOf(t, 2000, 3000, color.RGBA{G: 200, A: 255})
	pics := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(big) }))
	defer pics.Close()
	oldHTTP, oldHost := artHTTP, artTestHost
	artHTTP = func() *http.Client { return pics.Client() }
	artTestHost = func(u string) bool { return strings.HasPrefix(u, pics.URL) }
	defer func() { artHTTP, artTestHost = oldHTTP, oldHost }()

	plexID := artPlexRef{plex.ID, "77", "1", "movie"}.String()
	if _, err := db.Exec(`INSERT INTO art_titles (kind, tmdb_id, title, jf_ids) VALUES ('movie', 438631, 'Dune', ?)`, "a1,"+plexID); err != nil {
		t.Fatal(err)
	}
	if got := artServersOf("a1," + plexID); strings.Join(got, ",") != "jellyfin,plex" {
		t.Errorf("servers: %v", got)
	}
	ctx := context.Background()
	change := func(server string) {
		t.Helper()
		res, err := db.Exec(`INSERT INTO art_changes (kind, tmdb_id, title, type, source, url, why, by_hand, created_ms, server) VALUES ('movie', 438631, 'Dune', 'poster', 'tmdb', ?, 'chosen', 1, 1, ?)`, pics.URL+"/p.png", server)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		if err := s.artMake(ctx, srv, id); err != nil {
			t.Fatal(err)
		}
	}
	change("plex")
	if fp.uploads != 1 || jf.posted["a1/Primary"] != 0 {
		t.Fatalf("for Plex only: Plex %d, Jellyfin %d", fp.uploads, jf.posted["a1/Primary"])
	}
	change("jellyfin")
	if fp.uploads != 1 || jf.posted["a1/Primary"] != 1 {
		t.Fatalf("for Jellyfin only: Plex %d, Jellyfin %d", fp.uploads, jf.posted["a1/Primary"])
	}
	change("")
	if fp.uploads != 2 || jf.posted["a1/Primary"] != 2 {
		t.Fatalf("for both: Plex %d, Jellyfin %d", fp.uploads, jf.posted["a1/Primary"])
	}

	// A copy's poster style: its library's, else its server's, else the house's.
	st := artStyles{Poster: artStyle{V: 1}, Libraries: map[string]artStyle{"Plex": {V: 2}, "Plex: Anime": {V: 3}}}
	for _, c := range []struct {
		server, lib string
		want        int
	}{{"Plex", "Plex: Anime", 3}, {"Plex", "Plex: Movies", 2}, {"Jellyfin", "Movies", 1}} {
		if got := st.forCopy(c.server, c.lib).V; got != c.want {
			t.Errorf("%s %s: style %d, want %d", c.server, c.lib, got, c.want)
		}
	}
}
