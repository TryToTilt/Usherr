package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/mediaservers"
)

func TestBadgeListFromMediaInfo(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	all := badgeDefaults()
	all.Channels, all.New = true, true
	cases := []struct {
		name   string
		it     libItem
		rating float64
		label  string
		want   []posterBadge
	}{
		{"4K Dolby Vision Atmos 7.1, new", libItem{Kind: "movie", Added: now.Add(-48 * time.Hour), Media: badgeMedia{Width: 3840, Height: 1600, Range: jfRange("HDR", "DOVIWithHDR10"),
			Audio: []badgeTrack{{Codec: "ac3", Channels: 6}, {Codec: "truehd", Title: "TrueHD 7.1 Atmos", Channels: 8}}}}, 7.84, "",
			[]posterBadge{{"res", "4K"}, {"hdr", "Dolby Vision"}, {"audio", "Atmos"}, {"ch", "7.1"}, {"rating", "7.8"}, {"new", "New"}}},
		{"1080p DTS-HD MA (Jellyfin)", libItem{Kind: "movie", Added: now.AddDate(0, -2, 0), Media: badgeMedia{Width: 1920, Height: 800, Audio: []badgeTrack{{Codec: "dts", Profile: "DTS-HD MA", Channels: 6}}}}, 6.5, "IMDb",
			[]posterBadge{{"res", "1080p"}, {"audio", "DTS-HD MA"}, {"ch", "5.1"}, {"rating", "IMDb 6.5"}}},
		{"DTS-HD MA as Plex says it", libItem{Kind: "movie", Media: badgeMedia{Width: 1920, Audio: []badgeTrack{{Codec: "dca", Profile: "ma", Channels: 8}}}}, 0, "",
			[]posterBadge{{"res", "1080p"}, {"ch", "7.1"}}},
		{"DTS:X beats TrueHD; HDR10+", libItem{Kind: "movie", Media: badgeMedia{Width: 3840, Range: jfRange("HDR", "HDR10Plus"), Audio: []badgeTrack{{Codec: "truehd"}, {Codec: "dts", Profile: "DTS:X"}}}}, 0, "",
			[]posterBadge{{"res", "4K"}, {"hdr", "HDR10+"}, {"audio", "DTS:X"}}},
		{"720p, plain sound, no rating", libItem{Kind: "movie", Media: badgeMedia{Width: 1280, Audio: []badgeTrack{{Codec: "aac", Channels: 2}}}}, 0, "",
			[]posterBadge{{"res", "720p"}}},
		{"a show: rating and New only", libItem{Kind: "tv", Added: now.Add(-time.Hour), Media: badgeMedia{Width: 3840}}, 8, "",
			[]posterBadge{{"rating", "8.0"}, {"new", "New"}}},
	}
	for _, c := range cases {
		got := badgeList(all, c.it, c.rating, c.label, now)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	// Only the badges chosen.
	few := badgeDefaults()
	few.Audio, few.Rating = false, false
	if got := badgeList(few, cases[0].it, 9, "", now); !reflect.DeepEqual(got, []posterBadge{{"res", "4K"}, {"hdr", "Dolby Vision"}}) {
		t.Errorf("resolution and HDR only: %v", got)
	}
	if badgeKey(nil, few) != "" {
		t.Error("no badges is the original: an empty key")
	}
	if plexRange(mediaservers.PlexVideo{ColorTrc: "smpte2084"}) != "hdr" || plexRange(mediaservers.PlexVideo{DolbyVision: true}) != "dv" {
		t.Error("Plex's ranges")
	}
}

// Drawing is the same every time from the same original, and never touches
// the original.
func TestBadgeDrawFromOriginal(t *testing.T) {
	orig := image.NewRGBA(image.Rect(0, 0, 400, 600))
	for i := range orig.Pix {
		orig.Pix[i] = 90
	}
	before := append([]byte(nil), orig.Pix...)
	b := badgeDefaults()
	list := []posterBadge{{"res", "4K"}, {"hdr", "Dolby Vision"}, {"rating", "7.5"}}
	a1 := badgeDraw(orig, list, b)
	a2 := badgeDraw(orig, list, b)
	if !bytes.Equal(a1.Pix, a2.Pix) {
		t.Fatal("two drawings of the same badges differ")
	}
	if !bytes.Equal(orig.Pix, before) {
		t.Fatal("the original was drawn on")
	}
	if bytes.Equal(a1.Pix, orig.Pix) {
		t.Fatal("no badge was drawn")
	}
	// The middle of the poster is left alone; the top-left corner has a badge.
	if a1.RGBAAt(200, 300) != orig.RGBAAt(200, 300) {
		t.Error("the middle changed")
	}
	if a1.RGBAAt(30, 25) == orig.RGBAAt(30, 25) {
		t.Error("no badge in the top-left corner")
	}
	// A big poster is drawn 1000 wide.
	big := image.NewRGBA(image.Rect(0, 0, 2000, 3000))
	if r := badgeDraw(big, list, b).Bounds(); r.Dx() != 1000 || r.Dy() != 1500 {
		t.Errorf("drawn at %v", r)
	}
}

// fakeBadgeJF is a Jellyfin with one library ("Movies") and one film whose
// poster it keeps; every upload changes the poster's tag.
type fakeBadgeJF struct {
	mu      sync.Mutex
	poster  []byte
	tag     int
	rating  float64
	gets    int
	uploads [][]byte
}

func (f *fakeBadgeJF) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	js := func(v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	switch {
	case r.URL.Path == "/Library/VirtualFolders":
		js([]map[string]any{{"Name": "Movies", "ItemId": "lib1", "CollectionType": "movies"}})
	case r.URL.Path == "/Items" && r.URL.Query().Get("ParentId") == "lib1":
		js(map[string]any{"TotalRecordCount": 1, "Items": []map[string]any{{"Id": "m1", "Type": "Movie", "Name": "Dune", "ProductionYear": 2021,
			"ProviderIds": map[string]string{"Tmdb": "438631"}, "CommunityRating": f.rating, "DateCreated": "2020-01-01T00:00:00.0000000Z",
			"ImageTags":    map[string]string{"Primary": "t" + strconv.Itoa(f.tag)},
			"MediaStreams": []map[string]any{{"Type": "Video", "Width": 3840, "Height": 2160, "VideoRange": "HDR", "VideoRangeType": "DOVI"}, {"Type": "Audio", "Codec": "truehd", "DisplayTitle": "English - TrueHD - 7.1 - Atmos", "Channels": 8}}}}})
	case r.URL.Path == "/Items/m1/Images/Primary/0" && r.Method == http.MethodGet:
		f.gets++
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(f.poster)
	case r.URL.Path == "/Items/m1/Images/Primary/0" && r.Method == http.MethodPost:
		raw, _ := io.ReadAll(r.Body)
		data, _ := base64.StdEncoding.DecodeString(string(raw))
		f.poster = data
		f.tag++
		f.uploads = append(f.uploads, data)
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeBadgeJF) last() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.uploads) == 0 {
		return nil
	}
	return f.uploads[len(f.uploads)-1]
}

func badgeTestServer(t *testing.T, h http.Handler) *Server {
	t.Helper()
	s, db := optionsServer(t)
	s.db = db
	box, err := crypto.LoadOrCreateKey(filepath.Join(t.TempDir(), "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	s.MediaServers = mediaservers.NewRepo(db, box)
	s.mediaClient = mediaservers.NewClient("test")
	_ = s.Settings.Set(keyArtFolder, t.TempDir(), false)
	fs := httptest.NewServer(h)
	t.Cleanup(fs.Close)
	if _, err := s.MediaServers.Create(mediaservers.Server{Name: "Jellyfin", Kind: mediaservers.KindJellyfin, BaseURL: fs.URL, Token: "tok", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	old := badgePause
	badgePause = 0
	t.Cleanup(func() { badgePause = old; libItemsForget() })
	libItemsForget()
	return s
}

func (s *Server) setBadges(t *testing.T, b badgeSettings) {
	t.Helper()
	raw, _ := json.Marshal(b)
	if err := s.Settings.Set(keyBadges, string(raw), false); err != nil {
		t.Fatal(err)
	}
}

// The badges go on the original; drawing again (a new rating) starts from
// the original, not from the poster with badges; turning badges off puts
// the original back; a poster changed by hand becomes the new original.
func TestBadgesRedrawFromOriginalNeverStack(t *testing.T) {
	orig := pngOf(t, 300, 450, color.RGBA{R: 30, G: 60, B: 90, A: 255})
	fj := &fakeBadgeJF{poster: orig, rating: 7.1}
	s := badgeTestServer(t, fj)
	ctx := context.Background()
	b := badgeDefaults()
	b.On, b.Libraries = true, []string{"Movies"}
	s.setBadges(t, b)

	expect := func(rating string) []byte {
		img, _, err := image.Decode(bytes.NewReader(orig))
		if err != nil {
			t.Fatal(err)
		}
		out, err := artJPEG(badgeDraw(img, []posterBadge{{"res", "4K"}, {"hdr", "Dolby Vision"}, {"audio", "Atmos"}, {"rating", rating}}, b), 90)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}

	s.badgePass(ctx)
	if !bytes.Equal(fj.last(), expect("7.1")) {
		t.Fatal("the first drawing isn't the badges on the original")
	}
	kept, err := os.ReadFile(filepath.Join(s.badgeDir(), "m1.png"))
	if err != nil || !bytes.Equal(kept, orig) {
		t.Fatalf("the original wasn't kept before the upload: %v", err)
	}

	// Nothing changed: nothing is sent (the new tag is taken as Usherr's own).
	s.badgePass(ctx)
	if len(fj.uploads) != 1 {
		t.Fatalf("%d uploads with nothing changed", len(fj.uploads))
	}

	// A new rating: drawn again from the original (never from the badged one).
	fj.mu.Lock()
	fj.rating = 8.3
	fj.mu.Unlock()
	s.badgePass(ctx)
	if len(fj.uploads) != 2 || !bytes.Equal(fj.last(), expect("8.3")) {
		t.Fatal("the second drawing isn't the badges on the original")
	}
	if fj.gets != 1 {
		t.Errorf("the server's poster was read %d times; the original is kept after the first", fj.gets)
	}

	// Badges off: the original goes back, and nothing is kept any more.
	b.On = false
	s.setBadges(t, b)
	s.badgePass(ctx)
	if !bytes.Equal(fj.last(), orig) {
		t.Fatal("the original wasn't put back")
	}
	if _, err := os.Stat(filepath.Join(s.badgeDir(), "m1.png")); !os.IsNotExist(err) {
		t.Error("the kept original is still there")
	}
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM badge_posters`).Scan(&n)
	if n != 0 {
		t.Errorf("%d rows left", n)
	}

	// On again, then someone puts another poster on by hand: it becomes the
	// original the badges are drawn on.
	b.On = true
	s.setBadges(t, b)
	s.badgePass(ctx)
	s.badgePass(ctx) // takes the tag of its upload
	other := pngOf(t, 300, 450, color.RGBA{G: 200, A: 255})
	fj.mu.Lock()
	fj.poster, fj.tag = other, fj.tag+10
	fj.mu.Unlock()
	s.badgePass(ctx)
	kept, _ = os.ReadFile(filepath.Join(s.badgeDir(), "m1.png"))
	if !bytes.Equal(kept, other) {
		t.Error("the poster put on by hand isn't the new original")
	}

	// Re-Posterr keeps the original (not the badged poster) when it replaces
	// it, and its new poster becomes the original.
	backups := map[string]string{}
	srv, _ := s.jellyfinServer()
	if err := s.artKeep(ctx, srv, "m1", "poster", backups); err != nil {
		t.Fatal(err)
	}
	saved, _ := os.ReadFile(filepath.Join(s.artDir(), "replaced", backups["m1"]))
	if !bytes.Equal(saved, other) {
		t.Error("Re-Posterr kept the poster with badges")
	}
	third := pngOf(t, 300, 450, color.RGBA{B: 200, A: 255})
	if err := s.artSetImage(ctx, srv, "m1", "poster", third, "image/png"); err != nil {
		t.Fatal(err)
	}
	s.badgePass(ctx)
	kept, _ = os.ReadFile(filepath.Join(s.badgeDir(), "m1.png"))
	if !bytes.Equal(kept, third) {
		t.Error("Re-Posterr's poster isn't the new original")
	}
	if !strings.HasPrefix(http.DetectContentType(fj.last()), "image/jpeg") {
		t.Error("the badges weren't drawn on it")
	}
}

// Only the libraries chosen get badges.
func TestBadgesOnlyChosenLibraries(t *testing.T) {
	fj := &fakeBadgeJF{poster: pngOf(t, 100, 150, color.RGBA{A: 255}), rating: 7}
	s := badgeTestServer(t, fj)
	b := badgeDefaults()
	b.On, b.Libraries = true, []string{"Shows"}
	s.setBadges(t, b)
	s.badgePass(context.Background())
	if len(fj.uploads) != 0 {
		t.Fatal("a library not chosen got badges")
	}
}
