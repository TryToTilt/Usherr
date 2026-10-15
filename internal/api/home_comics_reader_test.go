package api

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestUnpackComicOrdersPages(t *testing.T) {
	dir := t.TempDir()
	cbz := filepath.Join(dir, "a.cbr") // a zip named .cbr, as many are
	f, _ := os.Create(cbz)
	zw := zip.NewWriter(f)
	for _, n := range []string{"page10.jpg", "page2.jpg", "page1.jpg", "notes.txt", "__MACOSX/._page1.jpg", "sub/page3.png"} {
		w, _ := zw.Create(n)
		_, _ = w.Write([]byte(n))
	}
	zw.Close()
	f.Close()
	out := filepath.Join(dir, "out")
	_ = os.MkdirAll(out, 0o755)
	if err := unpackComic(cbz, out); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"0001.jpg": "page1.jpg", "0002.jpg": "page2.jpg", "0003.jpg": "page10.jpg", "0004.png": "sub/page3.png"} // full paths: a subfolder (chapter) comes after
	for name, content := range want {
		b, err := os.ReadFile(filepath.Join(out, name))
		if err != nil || string(b) != content {
			t.Errorf("%s = %q, %v; want %q", name, b, err, content)
		}
	}
	if entries, _ := os.ReadDir(out); len(entries) != 4 {
		t.Errorf("%d files, want 4 pages", len(entries))
	}
}

func TestNaturalLess(t *testing.T) {
	if !pageNameLess("p2.jpg", "p10.jpg") || pageNameLess("p10.jpg", "p2.jpg") || !pageNameLess("a.jpg", "b.jpg") {
		t.Error("natural order wrong")
	}
}
