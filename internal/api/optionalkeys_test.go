package api_test

import (
	"path/filepath"
	"testing"

	"github.com/rdborg/mediarium/internal/api"
	"github.com/rdborg/mediarium/internal/config"
	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/settings"
)

// With a secret.key that does not match the database, the saved API keys
// cannot be read. The app still has to start so the person can see what is
// wrong and enter the keys again.
func TestNewStartsWhenSavedAPIKeysCannotBeDecrypted(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{ConfigDir: dir, DownloadsDir: dir, DownloadsIncomplete: dir, DownloadsComplete: dir, MoviesDir: dir}
	db, err := api.OpenTestDB(filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	oldBox, err := crypto.LoadOrCreateKey(filepath.Join(dir, "old.key"))
	if err != nil {
		t.Fatal(err)
	}
	st := settings.New(db, oldBox)
	for _, k := range []string{settings.KeyTMDBAPIKey, settings.KeyOpenSubtitlesAPIKey, settings.KeyTraktClientID} {
		if err := st.Set(k, "secret-value", true); err != nil {
			t.Fatal(err)
		}
	}
	newBox, err := crypto.LoadOrCreateKey(filepath.Join(dir, "new.key"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := api.New(db, cfg, newBox, "", "1.2.3-test"); err != nil {
		t.Fatalf("api.New should not fail on unreadable optional keys: %v", err)
	}
}
