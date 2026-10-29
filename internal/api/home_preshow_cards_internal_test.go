package api

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/config"
	"github.com/rdborg/mediarium/internal/settings"
)

// Every card is drawn by ffmpeg (where it's installed). PRESHOW_CARDS_OUT
// keeps them, to look at.
func TestPreshowCardsDraw(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("no ffmpeg")
	}
	dir := t.TempDir()
	db, err := OpenTestDB(filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Settings: settings.New(db, nil), cfg: config.Config{ConfigDir: dir}}
	out := os.Getenv("PRESHOW_CARDS_OUT")
	if out == "" {
		out = filepath.Join(dir, "out")
	}
	poster, backdrop := "https://image.tmdb.org/t/p/w780/1pdfLvkbY9ohJlCjQH2CZjjYVvJ.jpg", "https://image.tmdb.org/t/p/w1280/xOMo8BRK7PfcJv9JCnx7s5hj0PX.jpg"
	for _, c := range []preshowCardSpec{
		{Card: "welcome", Theater: "The Jellyplex", Title: "Dune: Part Two", At: "8:00 PM", Poster: poster, Backdrop: backdrop},
		{Card: "rating", Title: "Dune: Part Two", Rated: "PG-13", Backdrop: backdrop, Theme: "deco", Font: "deco"},
		{Card: "popcorn", Theme: "neon", Font: "neon"},
		{Card: "silence", Theme: "midnight", Font: "modern"},
		{Card: "coming", Theme: "velvet", Font: "elegant"},
		{Card: "countdown"},
		{Card: "feature", Title: "Dune: Part Two", Backdrop: backdrop, Theme: "drivein", Font: "retro"},
		{Card: "soon", Title: "Avatar: Fire and Ash", Poster: poster, Date: "In theaters December 19"},
		{Card: "announce", Title: "Pizza's here! Grab a slice before the feature starts", Theme: "neon", Font: "neon"},
		{Card: "qr", QR: "https://usherr.example.com", Theme: "midnight", Font: "modern"},
		{Card: "reel", Reel: []preshowReelItem{{Title: "Dune: Part Three", PosterURL: poster, ReleaseDate: "2026-12-18", Why: "cinema"}, {Title: "The Bear", PosterURL: poster, ReleaseDate: "2026-11-20", Season: 5}, {Title: "Ebenezer", PosterURL: poster, ReleaseDate: "2026-11-27"}}},
	} {
		c.Seconds = preshowCards[c.Card]
		if c.Theme == "" {
			c.Theme, c.Font = "usherr", "classic"
		}
		file := filepath.Join(out, c.Card+".mp4")
		_ = os.Remove(file)
		if err := s.preshowMakeCard(context.Background(), c, file); err != nil {
			t.Fatalf("%s: %v", c.Card, err)
		}
		if st, err := os.Stat(file); err != nil || st.Size() < 10_000 {
			t.Fatalf("%s: %v %v", c.Card, st, err)
		}
	}
}

func TestSoonLine(t *testing.T) {
	now := time.Date(2026, 10, 29, 20, 0, 0, 0, time.Local)
	for _, c := range []struct {
		date, why string
		season    int
		want      string
	}{
		{"2026-11-21", "cinema", 0, "In theaters November 21"},
		{"2026-11-14", "season", 3, "Season 3 · November 14"},
		{"2027-01-09", "wanted", 0, "Coming January 9, 2027"},
		{"2026-10-20", "cinema", 0, "Now in theaters"},
		{"", "cinema", 0, "Coming soon"},
	} {
		if got := soonLine(c.date, c.why, c.season, now); got != c.want {
			t.Errorf("soonLine(%q, %q, %d) = %q, want %q", c.date, c.why, c.season, got, c.want)
		}
	}
	if got := wrapWords("Pizza's here! Grab a slice before the feature starts", 26); len(got) != 2 {
		t.Errorf("wrapWords = %q", got)
	}
}
