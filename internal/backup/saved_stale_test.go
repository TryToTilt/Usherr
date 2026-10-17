package backup_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/backup"
)

// Pruning also clears the half-written file of a backup that was cut short,
// but not one that is still being written.
func TestPruneClearsOldHalfWrittenBackups(t *testing.T) {
	dir := t.TempDir()
	saved := filepath.Join(dir, backup.SavedDir)
	if err := os.MkdirAll(saved, 0o700); err != nil {
		t.Fatal(err)
	}
	old, fresh := filepath.Join(saved, ".saving-old.zip"), filepath.Join(saved, ".saving-new.zip")
	for _, p := range []string{old, fresh} {
		if err := os.WriteFile(p, []byte("part"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	long := time.Now().Add(-24 * time.Hour)
	if err := os.Chtimes(old, long, long); err != nil {
		t.Fatal(err)
	}
	if _, err := backup.Prune(dir, 3); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); err == nil {
		t.Error("the day-old half-written backup was kept")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Error("a backup that may still be written was removed")
	}
}
