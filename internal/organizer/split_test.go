package organizer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestJoinSplitFiles(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "inner")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(path, s string) {
		if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A comic cut from .000, inside a folder.
	write(filepath.Join(sub, "Issue 001.cbr.000"), "AB")
	write(filepath.Join(sub, "Issue 001.cbr.001"), "CD")
	write(filepath.Join(sub, "Issue 001.cbr.002"), "E")
	// A film cut from .001.
	write(filepath.Join(dir, "Film.mkv.001"), "12")
	write(filepath.Join(dir, "Film.mkv.002"), "34")
	// Left alone: a gap, a single piece, 7z volumes.
	write(filepath.Join(dir, "Gap.cbz.001"), "x")
	write(filepath.Join(dir, "Gap.cbz.003"), "y")
	write(filepath.Join(dir, "Single.cbr.001"), "z")
	write(filepath.Join(dir, "Pack.7z.001"), "7")
	write(filepath.Join(dir, "Pack.7z.002"), "7")

	made, err := JoinSplitFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(made) != 2 {
		t.Fatalf("made %v, want the comic and the film", made)
	}
	for path, want := range map[string]string{filepath.Join(sub, "Issue 001.cbr"): "ABCDE", filepath.Join(dir, "Film.mkv"): "1234"} {
		b, err := os.ReadFile(path)
		if err != nil || string(b) != want {
			t.Errorf("%s = %q, %v; want %q", filepath.Base(path), b, err, want)
		}
	}
	if _, err := os.Stat(filepath.Join(sub, "Issue 001.cbr.001")); !os.IsNotExist(err) {
		t.Error("the pieces should be removed once joined")
	}
	for _, kept := range []string{"Gap.cbz.001", "Gap.cbz.003", "Single.cbr.001", "Pack.7z.001"} {
		if _, err := os.Stat(filepath.Join(dir, kept)); err != nil {
			t.Errorf("%s should be left alone", kept)
		}
	}
}
