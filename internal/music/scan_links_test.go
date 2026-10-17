package music

import (
	"os"
	"path/filepath"
	"testing"
)

// Artist and album folders that are links to folders elsewhere are scanned.
func TestScanLibraryFollowsLinkedFolders(t *testing.T) {
	elsewhere := t.TempDir()
	album := filepath.Join(elsewhere, "OK Computer (1997)")
	if err := os.MkdirAll(album, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(album, "01 Airbag.flac"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, "01 Song.flac"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	if err := os.Symlink(elsewhere, filepath.Join(root, "Radiohead")); err != nil {
		t.Skipf("links are not available here: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "Blur"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, filepath.Join(root, "Blur", "Parklife (1994)")); err != nil {
		t.Fatal(err)
	}

	artists, err := ScanLibrary(root)
	if err != nil {
		t.Fatal(err)
	}
	albums := map[string]int{}
	for _, a := range artists {
		for _, al := range a.Albums {
			albums[a.Name+"/"+al.Title] = len(al.Files)
		}
	}
	if albums["Radiohead/OK Computer"] != 1 || albums["Blur/Parklife"] != 1 {
		t.Fatalf("albums found through links: %v", albums)
	}
}
