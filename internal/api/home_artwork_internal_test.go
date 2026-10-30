package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/mediaservers"
)

// The best first: a background with no words when textless is wanted, a
// poster in your language, nothing too small ahead of a big one.
func TestArtRank(t *testing.T) {
	bg := []artCand{{URL: "en", Lang: "en", Score: 9, Width: 1920}, {URL: "none", Lang: "", Score: 5, Width: 1920}, {URL: "de", Lang: "de", Score: 9, Width: 1920}}
	artRank(bg, "backdrop", "en", true)
	if bg[0].URL != "none" || bg[1].URL != "en" {
		t.Fatalf("textless backgrounds: %v", bg)
	}
	po := []artCand{{URL: "none", Lang: "", Score: 9, Width: 1000}, {URL: "en-small", Lang: "en", Score: 9, Width: 300}, {URL: "en", Lang: "en", Score: 6, Width: 1000}}
	artRank(po, "poster", "en", true)
	if po[0].URL != "en" || po[1].URL != "en-small" || po[2].URL != "none" {
		t.Fatalf("posters: %v", po)
	}
	set := artSettings{From: 23, To: 7}
	for h, want := range map[int]bool{23: true, 2: true, 7: false, 12: false} {
		if got := set.inNight(timeAt(h)); got != want {
			t.Fatalf("hour %d in the night: %v", h, got)
		}
	}
}

// fanart.tv's art read: "00" is no language, and a personal key that isn't
// taken as the API key alone is tried as both (and remembered).
func TestFanartImages(t *testing.T) {
	s, db := optionsServer(t)
	s.db = db
	fa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("client_key") == "" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/movies/603" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"name":"The Matrix","hdmovielogo":[{"url":"https://assets.fanart.tv/fanart/movies/603/hdmovielogo/a.png","lang":"en","likes":"7","width":"800","height":"310"}],
			"moviebackground":[{"url":"https://assets.fanart.tv/fanart/movies/603/moviebackground/b.jpg","lang":"00","likes":"3","width":"1920","height":"1080"}],"moviedisc":[{"url":"https://x"}]}`))
	}))
	defer fa.Close()
	old := fanartBase
	fanartBase = fa.URL
	defer func() { fanartBase = old }()
	if _, err := s.fanartImages(context.Background(), "movie", 603, 0); err != errNoFanart {
		t.Fatalf("without a key: %v", err)
	}
	_ = s.Settings.Set(keyArtFanart, "personal", false)
	got, err := s.fanartImages(context.Background(), "movie", 603, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got["logo"]) != 1 || got["logo"][0].Width != 800 || len(got["backdrop"]) != 1 || got["backdrop"][0].Lang != "" || len(got["poster"]) != 0 {
		t.Fatalf("read: %+v", got)
	}
	if !strings.HasPrefix(got["logo"][0].Preview, "/api/home/art/preview?u=https%3A%2F%2Fassets.fanart.tv") {
		t.Fatalf("preview: %s", got["logo"][0].Preview)
	}
	if as, _ := s.Settings.Get(keyArtFanartAs); as != "both" {
		t.Fatalf("how the key is taken: %q", as)
	}
	if _, err := s.fanartImages(context.Background(), "tv", 1, 0); err != errNoFanart {
		t.Fatalf("a show with no TheTVDB id: %v", err)
	}
}

// A fake Jellyfin: two copies of a film (the library's and the streamed
// one), the first with a poster, neither with a logo.
type fakeJF struct {
	mu      sync.Mutex
	images  map[string][]byte // item/type -> picture
	posted  map[string]int
	deleted []string
}

func (f *fakeJF) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.URL.Path == "/Items" && r.Method == http.MethodGet:
		_, _ = w.Write([]byte(`{"Items":[
			{"Id":"a1","Type":"Movie","Name":"Dune","ProductionYear":2021,"ProviderIds":{"Tmdb":"438631"},"ImageTags":{"Primary":"x"},"BackdropImageTags":["y"]},
			{"Id":"a2","Type":"Movie","Name":"Dune","ProductionYear":2021,"ProviderIds":{"Tmdb":"438631"},"ImageTags":{},"BackdropImageTags":[]},
			{"Id":"c1","Type":"BoxSet","Name":"Dune Collection","ProviderIds":{"Tmdb":"726871"},"ImageTags":{"Primary":"x"}},
			{"Id":"n1","Type":"Movie","Name":"Home video","ProviderIds":{}}],"TotalRecordCount":4}`))
	case strings.HasPrefix(r.URL.Path, "/Items/") && strings.Contains(r.URL.Path, "/Images/"):
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/Items/"), "/")
		k := parts[0] + "/" + parts[2]
		switch r.Method {
		case http.MethodGet:
			if b, ok := f.images[k]; ok {
				w.Header().Set("Content-Type", "image/png")
				_, _ = w.Write(b)
				return
			}
			http.NotFound(w, r)
		case http.MethodPost:
			if !strings.Contains(r.Header.Get("Authorization"), `Token="k"`) {
				http.Error(w, "who?", http.StatusUnauthorized)
				return
			}
			var buf bytes.Buffer
			_, _ = buf.ReadFrom(r.Body)
			b, err := base64.StdEncoding.DecodeString(buf.String())
			if err != nil {
				http.Error(w, "not base64", http.StatusBadRequest)
				return
			}
			f.images[k] = b
			f.posted[k]++
			w.WriteHeader(http.StatusNoContent)
		case http.MethodDelete:
			delete(f.images, k)
			f.deleted = append(f.deleted, k)
			w.WriteHeader(http.StatusNoContent)
		}
	default:
		http.NotFound(w, r)
	}
}

func pngOf(t *testing.T, w, h int, c color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// Jellyfin's titles become one row a title (copies together, what they lack);
// a change goes on every copy, keeping what it replaces, and comes off again.
func TestArtSyncApplyUndo(t *testing.T) {
	s, db := optionsServer(t)
	s.db = db
	_ = s.Settings.Set(keyArtFolder, t.TempDir(), false)
	old := pngOf(t, 40, 60, color.RGBA{R: 200, A: 255})
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

	ctx := context.Background()
	items, err := s.artJFItems(ctx, srv, true)
	if err != nil || len(items) != 4 {
		t.Fatalf("items: %v %v", items, err)
	}
	if err := s.artSync(ctx, items); err != nil {
		t.Fatal(err)
	}
	var ids, missing string
	if err := db.QueryRow(`SELECT jf_ids, missing FROM art_titles WHERE kind = 'movie' AND tmdb_id = 438631`).Scan(&ids, &missing); err != nil {
		t.Fatal(err)
	}
	if ids != "a1,a2" || missing != "poster,backdrop,logo" {
		t.Fatalf("the film: %q %q", ids, missing)
	}
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM art_titles`).Scan(&n)
	if n != 2 {
		t.Fatalf("titles (the one with no TMDB id left out): %d", n)
	}

	res, err := db.Exec(`INSERT INTO art_changes (kind, tmdb_id, title, type, source, url, why, created_ms) VALUES ('movie', 438631, 'Dune', 'poster', 'tmdb', ?, 'small', 1)`, pics.URL+"/p.png")
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	if err := s.artMake(ctx, srv, id); err != nil {
		t.Fatal(err)
	}
	if jf.posted["a1/Primary"] != 1 || jf.posted["a2/Primary"] != 1 {
		t.Fatalf("posted: %v", jf.posted)
	}
	got, _, err := image.Decode(bytes.NewReader(jf.images["a2/Primary"]))
	if err != nil || got.Bounds().Dx() != artPosterW || got.Bounds().Dy() != 1500 {
		t.Fatalf("sent at Jellyfin's size: %v %v", got.Bounds(), err)
	}
	list, _ := s.artChanges(ctx, `WHERE c.id = ?`, id)
	if len(list) != 1 || list[0].State != "done" || !strings.Contains(list[0].Backups, `"a2":""`) {
		t.Fatalf("change: %+v", list)
	}
	kept, _ := os.ReadDir(filepath.Join(s.artDir(), "replaced"))
	if len(kept) != 1 {
		t.Fatalf("the replaced poster kept: %d", len(kept))
	}

	if err := s.artUndo(ctx, srv, list[0]); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(jf.images["a1/Primary"], old) {
		t.Fatal("the old poster isn't back")
	}
	if _, still := jf.images["a2/Primary"]; still || len(jf.deleted) != 1 {
		t.Fatalf("the copy that had none: %v", jf.deleted)
	}
}

func timeAt(hour int) time.Time { return time.Date(2026, 10, 29, hour, 30, 0, 0, time.Local) }

// fanart.tv's small copy through Usherr: its own preview, or the picture
// made small when it has none.
func TestArtPreview(t *testing.T) {
	s, _ := optionsServer(t)
	big := pngOf(t, 1000, 1426, color.RGBA{B: 200, A: 255})
	pics := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/preview/") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(big)
	}))
	defer pics.Close()
	oldHTTP, oldHost := artHTTP, artTestHost
	artHTTP = func() *http.Client { return pics.Client() }
	artTestHost = func(u string) bool { return strings.HasPrefix(u, pics.URL) }
	defer func() { artHTTP, artTestHost = oldHTTP, oldHost }()
	rec := httptest.NewRecorder()
	s.handleArtPreview(rec, httptest.NewRequest(http.MethodGet, "/api/home/art/preview?u="+pics.URL+"/fanart/movies/1/movieposter/a.png", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	img, _, err := image.Decode(rec.Body)
	if err != nil || img.Bounds().Dx() != 400 {
		t.Fatalf("small copy: %v %v", img, err)
	}
	rec = httptest.NewRecorder()
	s.handleArtPreview(rec, httptest.NewRequest(http.MethodGet, "/api/home/art/preview?u=https://example.com/x.jpg", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("another site's picture: %d", rec.Code)
	}
}
