package api

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestComicPageCount(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.cbz")
	f, _ := os.Create(good)
	zw := zip.NewWriter(f)
	for _, n := range []string{"001.jpg", "002.jpg", "ComicInfo.xml"} {
		w, _ := zw.Create(n)
		_, _ = w.Write([]byte("data " + n))
	}
	_ = zw.Close()
	_ = f.Close()
	if n, err := comicPageCount(good); err != nil || n != 2 {
		t.Fatalf("good: %d, %v", n, err)
	}
	if _, err := comicPageCount(filepath.Join(dir, "gone.cbz")); err == nil {
		t.Fatal("missing file passed")
	}
	junk := filepath.Join(dir, "junk.cbr")
	_ = os.WriteFile(junk, []byte("<html>not a comic</html>"), 0o644)
	if _, err := comicPageCount(junk); err == nil {
		t.Fatal("junk passed")
	}
	// A cut-short ZIP fails.
	b, _ := os.ReadFile(good)
	cut := filepath.Join(dir, "cut.cbz")
	_ = os.WriteFile(cut, b[:len(b)/2], 0o644)
	if _, err := comicPageCount(cut); err == nil {
		t.Fatal("truncated passed")
	}
}

// TestRarToCBZReal converts a real CBR when MEDIARIUM_TEST_CBR names one
// (a copy is used; the file itself is never changed).
func TestRarToCBZReal(t *testing.T) {
	src := os.Getenv("MEDIARIUM_TEST_CBR")
	if src == "" {
		t.Skip("set MEDIARIUM_TEST_CBR to a .cbr to try a real conversion")
	}
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	cp := filepath.Join(dir, "copy.cbr")
	_ = os.WriteFile(cp, b, 0o644)
	want, err := comicPageCount(cp)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "copy.cbz.part")
	if err := rarToCBZ(cp, out, want); err != nil {
		t.Fatal(err)
	}
	got, err := comicPageCount(out)
	t.Logf("%d pages, CBR %d bytes, CBZ written", got, len(b))
	if err != nil || got != want {
		t.Fatalf("got %d pages (%v), want %d", got, err, want)
	}
}
