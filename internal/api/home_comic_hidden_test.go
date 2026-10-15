package api

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestNameHiddenComics(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "file_0.bin")
	f, _ := os.Create(p)
	zw := zip.NewWriter(f)
	for i := 0; i < 3; i++ {
		w, _ := zw.CreateHeader(&zip.FileHeader{Name: "page" + string(rune('0'+i)) + ".jpg", Method: zip.Store})
		_, _ = w.Write(make([]byte, 40<<10))
	}
	_ = zw.Close()
	_ = f.Close()
	nameHiddenComics(dir, "Daredevil 022 (2013) (Digital) (Zone-Empire)")
	files := comicFilesIn(dir)
	if len(files) != 1 || filepath.Base(files[0]) != "Daredevil 022 (2013) (Digital) (Zone-Empire).cbz" {
		t.Fatalf("files = %v", files)
	}
}

func TestNameSaysNothing(t *testing.T) {
	for name, want := range map[string]bool{
		"file_0.bin": true, "a1b2c3d4": true, "x.1a2b3c": true,
		"Vagabond.Volume.023.[KO].part01.rar": false, "Show.r00": false, "Saga 31.cbr": false, "info.nfo": false, "vol0+1.par2": false,
	} {
		if got := nameSaysNothing(name); got != want {
			t.Errorf("%s: %v", name, got)
		}
	}
}
