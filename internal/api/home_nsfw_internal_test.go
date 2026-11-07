package api

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFreeNameNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "Studio - 2024-01-02 - Title.mp4")
	if got := freeName(p); got != p {
		t.Fatalf("a free name must be kept, got %s", got)
	}
	if err := os.WriteFile(p, []byte("whisparr's file"), 0o644); err != nil {
		t.Fatal(err)
	}
	second := freeName(p)
	if second != filepath.Join(dir, "Studio - 2024-01-02 - Title (2).mp4") {
		t.Fatalf("taken name: got %s", second)
	}
	_ = os.WriteFile(second, []byte("x"), 0o644)
	if third := freeName(p); third != filepath.Join(dir, "Studio - 2024-01-02 - Title (3).mp4") {
		t.Fatalf("third: got %s", third)
	}
}

func TestMapWhisparrPath(t *testing.T) {
	cases := map[[3]string]string{
		{"/movies/nsfw/a/b.mp4", "/movies", "/data/nsfw"}:  "/data/nsfw/nsfw/a/b.mp4",
		{"/movies2/x.mp4", "/movies", "/data/nsfw"}:        "/movies2/x.mp4", // not under "from": left alone
		{"/movies/nsfw/a/b.mp4", "/movies/", "/data/nsfw"}: "/data/nsfw/nsfw/a/b.mp4",
	}
	for in, want := range cases {
		if got := mapWhisparrPath(in[0], in[1], in[2]); got != want {
			t.Errorf("map(%v) = %s, want %s", in, got, want)
		}
	}
}

func TestFavoritesRunOnlyAtNight(t *testing.T) {
	at := func(hour int) time.Time { return time.Date(2026, 11, 6, hour, 30, 0, 0, time.Local) }
	if favoritesDue(false, "", at(3)) {
		t.Error("off by default: the switch must be on")
	}
	for hour := 0; hour < 24; hour++ {
		want := hour >= 2 && hour < 9
		if got := favoritesDue(true, "", at(hour)); got != want {
			t.Errorf("at %d:30 due = %v, want %v", hour, got, want)
		}
	}
	yesterday := at(3).Add(-24 * time.Hour).Format(time.RFC3339)
	if !favoritesDue(true, yesterday, at(3)) {
		t.Error("a day later it runs again")
	}
	earlier := at(3).Add(-2 * time.Hour).Format(time.RFC3339)
	if favoritesDue(true, earlier, at(3)) {
		t.Error("only once a night")
	}
}
