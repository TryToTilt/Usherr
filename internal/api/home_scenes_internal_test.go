package api

import (
	"os"
	"path/filepath"
	"testing"
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
		{"/movies/scenes/a/b.mp4", "/movies", "/data/scenes"}:  "/data/scenes/scenes/a/b.mp4",
		{"/movies2/x.mp4", "/movies", "/data/scenes"}:          "/movies2/x.mp4", // not under "from": left alone
		{"/movies/scenes/a/b.mp4", "/movies/", "/data/scenes"}: "/data/scenes/scenes/a/b.mp4",
	}
	for in, want := range cases {
		if got := mapWhisparrPath(in[0], in[1], in[2]); got != want {
			t.Errorf("map(%v) = %s, want %s", in, got, want)
		}
	}
}
