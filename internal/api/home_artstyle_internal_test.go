package api

import (
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/rdborg/mediarium/internal/metadata"
)

// artTestArt is a stand-in for art without words: two colours, top and bottom.
func artTestArt(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.RGBA{0x30, 0x70, 0xa0, 0xff}), image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(0, h/2, w, h), image.NewUniform(color.RGBA{0xc0, 0x80, 0x30, 0xff}), image.Point{}, draw.Src)
	return img
}

func TestArtStyleDraw(t *testing.T) {
	st := artDefaultStyles()
	for _, s := range []artStyle{st.Poster, st.Season, st.Card} {
		if msg := s.check(); msg != "" {
			t.Fatalf("a default style doesn't pass: %s", msg)
		}
	}
	band := artStyle{V: 1, Layers: []artLayer{
		{Kind: "band", Pos: "bottom", Color: "#101820", Opacity: 90, Size: 20},
		{Kind: "border", Color: "#f2c14e", Size: 14},
		{Kind: "title", Pos: "bottom", Font: "anton", Color: "#ffffff", Size: 8, Caps: true, Sub: true},
	}}
	centre := artStyle{V: 1, Layers: []artLayer{
		{Kind: "fade", Pos: "middle", Color: "#000000", Opacity: 45, Size: 100},
		{Kind: "title", Pos: "middle", Font: "playfair", Color: "#fff4d6", Size: 10, Sub: true},
	}}
	logo := image.NewNRGBA(image.Rect(0, 0, 400, 150))
	draw.Draw(logo, image.Rect(20, 20, 380, 130), image.NewUniform(color.NRGBA{0xff, 0xff, 0xff, 0xff}), image.Point{}, draw.Src)
	cases := []struct {
		name  string
		st    artStyle
		w, h  int
		words artWords
		logo  image.Image
	}{
		{"poster-fade", st.Poster, 1000, 1500, artPosterWords(st.Poster, "movie", "The Long Way Round to Somewhere Far", 1987, ""), nil},
		{"poster-logo", st.Poster, 1000, 1500, artPosterWords(st.Poster, "movie", "Midnight Harbor", 2024, ""), logo},
		{"poster-band", band, 1000, 1500, artPosterWords(st.Poster, "movie", "Midnight Harbor", 2024, ""), nil},
		{"poster-centre", centre, 1000, 1500, artPosterWords(st.Poster, "movie", "Midnight Harbor", 2024, ""), nil},
		{"season", st.Season, 1000, 1500, artSeasonWords(st.Season, "Harbor Lights", 2), nil},
		{"card", st.Card, artCardW, artCardH, artCardWords(st.Card, "The Long Tide", 1, 3), nil},
	}
	out := os.Getenv("ART_DUMP") // a folder to look at the pictures in
	for _, c := range cases {
		img := artDraw(artTestArt(800, 1200), c.w, c.h, c.st, c.words, c.logo, artExtras{})
		if img.Rect.Dx() != c.w || img.Rect.Dy() != c.h {
			t.Fatalf("%s: drawn %v", c.name, img.Rect)
		}
		// The words (or logo) are there: a bright pixel low in the picture.
		bright := false
		for y := c.h / 2; y < c.h && !bright; y += 2 {
			for x := 0; x < c.w; x += 2 {
				p := img.RGBAAt(x, y)
				if p.R > 0xf0 && p.G > 0xe0 && p.B > 0xc0 {
					bright = true
					break
				}
			}
		}
		if !bright {
			t.Errorf("%s: no words drawn", c.name)
		}
		if out != "" {
			f, err := os.Create(filepath.Join(out, c.name+".jpg"))
			if err == nil {
				_ = jpeg.Encode(f, img, &jpeg.Options{Quality: 85})
				f.Close()
			}
		}
	}
}

func TestArtStyleCheck(t *testing.T) {
	bad := []artStyle{
		{V: 2, Layers: artDefaultStyles().Poster.Layers},
		{V: 1, Layers: []artLayer{{Kind: "fade", Pos: "bottom", Size: 40}}},                              // no title
		{V: 1, Layers: []artLayer{{Kind: "title", Font: "comic", Size: 8}}},                              // not a font it has
		{V: 1, Layers: []artLayer{{Kind: "title", Font: "bebas", Size: 8}, {Kind: "glow", Size: 3}}},     // a layer it doesn't know
		{V: 1, Layers: []artLayer{{Kind: "title", Font: "bebas", Size: 8, Color: "red"}}},                // a colour
		{V: 1, Layers: []artLayer{{Kind: "title", Font: "bebas", Size: 8}, {Kind: "border", Size: 400}}}, // too thick
	}
	for i, s := range bad {
		if s.check() == "" {
			t.Errorf("style %d passed", i)
		}
	}
	a, b := artDefaultStyles().Poster, artDefaultStyles().Poster
	b.Layers = append([]artLayer{}, b.Layers...)
	b.Layers[1].Font = "anton"
	if a.print() == b.print() || a.print() != artDefaultStyles().Poster.print() {
		t.Error("a style's fingerprint should follow the style")
	}
}

func TestArtPosterSwaps(t *testing.T) {
	s, db := optionsServer(t)
	s.db = db
	defer metadata.SetPosterSwaps(nil)
	if _, err := db.Exec(`INSERT INTO streamed_titles (kind, tmdb_id, title, poster_path) VALUES ('movie', 1271, '300', '/tmdb300.jpg')`); err != nil {
		t.Fatal(err)
	}
	res, err := db.Exec(`INSERT INTO art_changes (kind, tmdb_id, title, type, url, state, created_ms) VALUES ('movie', 1271, '300', 'poster', 'x', 'done', 1)`)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	s.artPosterSwaps(context.Background())
	if got := metadata.PosterURL("/tmdb300.jpg"); got != "/api/home/art/poster/"+strconv.FormatInt(id, 10) {
		t.Fatalf("Usherr shows %q, not Re-Posterr's poster", got)
	}
	if _, err := db.Exec(`UPDATE art_changes SET state = 'undone' WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	s.artPosterSwaps(context.Background())
	if got := metadata.PosterURL("/tmdb300.jpg"); got != "https://image.tmdb.org/t/p/w500/tmdb300.jpg" {
		t.Fatalf("put back, Usherr still shows %q", got)
	}
}

func TestArtStyleMore(t *testing.T) {
	st := artDefaultStyles()
	// Wording, collections, fonts that lack the letters.
	season := st.Season
	season.Layers = append([]artLayer{}, season.Layers...)
	season.Layers[1].Words = "Series {n}"
	if got := artSeasonWords(season, "Harbor", 3).Sub; got != "Series 3" {
		t.Errorf("season wording: %q", got)
	}
	if got := artCardWords(st.Card, "Pilot", 1, 2).Sub; got != "Season 1 · Episode 2" {
		t.Errorf("card wording: %q", got)
	}
	short := st.Poster
	short.Layers = append([]artLayer{}, short.Layers...)
	short.Layers[1].Short = true
	if got := artPosterWords(short, "collection", "Alien Collection", 0, "").Main; got != "Alien" {
		t.Errorf("collection: %q", got)
	}
	if got := artPosterWords(st.Poster, "movie", "Alien", 1979, "In space").Main; got != "In space" {
		t.Errorf("own words: %q", got)
	}
	f, _ := artFont("bebas")
	if artCovers(f, "東京") || !artCovers(f, "TOKYO") {
		t.Error("artCovers should know Bebas has no Japanese")
	}
	// Outline, shadow, one line, an overlay by resolution: they draw and pass.
	dir := t.TempDir()
	ov := image.NewNRGBA(image.Rect(0, 0, 100, 150))
	draw.Draw(ov, image.Rect(0, 0, 100, 10), image.NewUniform(color.NRGBA{0, 0xff, 0, 0xff}), image.Point{}, draw.Src)
	_ = os.MkdirAll(filepath.Join(dir, "overlays"), 0o755)
	of, _ := os.Create(filepath.Join(dir, "overlays", "four-k.png"))
	_ = png.Encode(of, ov)
	of.Close()
	fancy := artStyle{V: 1, Layers: []artLayer{
		{Kind: "overlay", ByRes: map[string]string{"4k": "four-k.png"}},
		{Kind: "title", Pos: "middle", Font: "anton", Color: "#ffffff", Size: 9, Outline: 6, Shadow: true, MaxLines: 1},
	}}
	if msg := fancy.check(); msg != "" {
		t.Fatal(msg)
	}
	img := artDraw(artTestArt(800, 1200), 1000, 1500, fancy, artPosterWords(fancy, "movie", "A Rather Long Title Indeed", 0, ""), nil, artExtras{dir: dir, res: "4k"})
	if p := img.RGBAAt(500, 3); p.G < 0xe0 || p.R > 0x20 {
		t.Errorf("the 4K overlay isn't on: %v", p)
	}
	img = artDraw(artTestArt(800, 1200), 1000, 1500, fancy, artWords{Main: "X"}, nil, artExtras{dir: dir, res: "1080"})
	if p := img.RGBAAt(500, 3); p.G > 0xe0 && p.R < 0x20 {
		t.Error("the 4K overlay is on a 1080 copy")
	}
	bad := fancy
	bad.Layers = []artLayer{{Kind: "overlay", File: "../x.png"}, fancy.Layers[1]}
	if bad.check() == "" {
		t.Error("an overlay outside the folder passed")
	}
}
