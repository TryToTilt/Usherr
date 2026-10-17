package cleanup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A removal that was cut short, after the files moved in but before the record
// was written (older versions wrote it last), leaves a folder with no record.
// It is listed, aged by its name, purged once it is old enough, and it is never
// "restored" (which would delete its files).
func TestAFolderWithoutARecordIsListedPurgedAndNeverRestored(t *testing.T) {
	root := t.TempDir()
	id := "20260901-101500-deadbeef"
	dir := filepath.Join(root, TrashDir, id)
	writeFile(t, filepath.Join(dir, "files", "Heat (1995)", "Heat (1995).mkv"), "video")

	list, err := ListTrash(root)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %+v (err %v)", list, err)
	}
	e := list[0]
	if !e.NoRecord || e.ID != id || e.Files != 1 || e.Size != int64(len("video")) {
		t.Errorf("entry: %+v", e)
	}
	if want := time.Date(2026, 9, 1, 10, 15, 0, 0, time.UTC); !e.DeletedAt.Equal(want) {
		t.Errorf("age comes from the name: got %v, want %v", e.DeletedAt, want)
	}

	if _, err := RestoreTrash(root, id); err == nil {
		t.Error("restoring an entry with no record should be refused")
	}
	if !exists(filepath.Join(dir, "files", "Heat (1995)", "Heat (1995).mkv")) {
		t.Fatal("a refused restore must leave the files alone")
	}

	if n, _, err := PurgeTrash(root, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)); err != nil || n != 0 {
		t.Fatalf("a young entry was purged: %d %v", n, err)
	}
	n, freed, err := PurgeTrash(root, trashNow)
	if err != nil || n != 1 || freed != int64(len("video")) {
		t.Fatalf("purge: %d %d %v", n, freed, err)
	}
	if exists(dir) {
		t.Error("the old entry is still there")
	}
}

// The record is written first, so a stop after only some of the files moved
// leaves an entry that can be restored: what moved comes back, what never
// moved stays where it is.
func TestAnEntryCutShortHalfwayCanStillBeRestored(t *testing.T) {
	root := t.TempDir()
	id := "20261003-093000-0badf00d"
	dir := filepath.Join(root, TrashDir, id)
	a, b := filepath.Join(root, "A", "a.mkv"), filepath.Join(root, "B", "b.mkv")
	writeFile(t, filepath.Join(dir, "files", "A", "a.mkv"), "aaa") // moved
	writeFile(t, b, "bbb")                                         // never moved
	rec, _ := json.Marshal(TrashEntry{ID: id, Label: "x", DeletedAt: trashNow, Paths: []string{a, b}, Size: 6, Files: 2})
	if err := os.WriteFile(filepath.Join(dir, "entry.json"), rec, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := RestoreTrash(root, id); err != nil {
		t.Fatalf("restore after a stop: %v", err)
	}
	if !exists(a) || !exists(b) {
		t.Error("both files should be in place")
	}
	if exists(dir) {
		t.Error("the entry should be gone once restored")
	}
}

// A normal removal writes the record before it moves anything, and a move that
// fails puts everything back and leaves no entry behind.
func TestAFailedRemovalPutsEverythingBack(t *testing.T) {
	root := t.TempDir()
	a, b := filepath.Join(root, "A", "a.mkv"), filepath.Join(root, "B", "b.mkv")
	writeFile(t, a, "aaa")
	writeFile(t, b, "bbb")
	// The second file is gone by the time it is moved, so that move fails.
	if err := os.Remove(b); err != nil {
		t.Fatal(err)
	}
	if _, err := moveToTrash(root, "x", []string{a, b}, trashNow); err == nil {
		t.Fatal("moving a file that is gone should fail")
	}
	if !exists(a) {
		t.Error("the first file was not put back")
	}
	if list, _ := ListTrash(root); len(list) != 0 {
		t.Errorf("a failed removal left an entry: %+v", list)
	}
}
