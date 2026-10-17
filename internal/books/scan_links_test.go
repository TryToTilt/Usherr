package books

import (
	"os"
	"path/filepath"
	"testing"
)

// Authors kept as links to folders on another drive are scanned too, and a
// link back up the tree is not a loop.
func TestScanFollowsLinkedFolders(t *testing.T) {
	elsewhere := t.TempDir()
	if err := os.MkdirAll(filepath.Join(elsewhere, "Dune"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(elsewhere, "Dune", "Dune.epub"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(elsewhere, "Mort"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(elsewhere, "Mort", "01.mp3"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	if err := os.Symlink(elsewhere, filepath.Join(root, "Frank Herbert")); err != nil {
		t.Skipf("links are not available here: %v", err)
	}
	if err := os.Symlink(root, filepath.Join(elsewhere, "loop")); err != nil {
		t.Fatal(err)
	}

	ebooks, err := ScanEbooks(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(ebooks) != 1 || ebooks[0].Title != "Dune" {
		t.Fatalf("ebooks through a linked author folder: %+v", ebooks)
	}
	audio, err := ScanAudiobooks(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(audio) != 1 || audio[0].Title != "Mort" {
		t.Fatalf("audiobooks through a linked author folder: %+v", audio)
	}
}
