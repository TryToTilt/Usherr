package api_test

import (
	"net/http"
	"path/filepath"
	"testing"

	"github.com/rdborg/mediarium/internal/api"
	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/settings"
	"github.com/rdborg/mediarium/internal/store"
)

// A saved key that can't be read is a Health item that names the indexer. The
// page must not blame a missing indexer, and the other indexer stays out of it.
func TestHealthNamesTheIndexerWhoseKeyCannotBeRead(t *testing.T) {
	db, base, client := newServerWithDB(t)
	idx := newTVIndexerWith(t, []string{"A.Release.2001.1080p-GRP"})
	for _, name := range []string{"Fine", "Broken"} {
		postJSON[map[string]any](t, client, base+"/api/indexers", map[string]any{"name": name, "definitionId": "x", "baseUrl": idx.URL, "apiKey": "k", "protocol": "usenet"}, http.StatusCreated)
	}
	if _, err := db.Exec(`UPDATE indexers SET api_key_encrypted = 'bm90IGEgcmVhbCBrZXk=' WHERE name = 'Broken'`); err != nil {
		t.Fatal(err)
	}

	h := healthByID(t, client, base)
	var found bool
	for id, it := range h {
		if len(id) > 21 && id[:21] == "indexer-key-unreadabl" {
			found = true
			if it["level"] != "error" || it["title"] != "Broken has a key that can't be read" {
				t.Errorf("unexpected item: %+v", it)
			}
		}
	}
	if !found {
		t.Fatalf("no item for the unreadable key: %v", keysOf(h))
	}
	if _, wrong := h["no-indexers"]; wrong {
		t.Error("Health says no indexer is set up although the real cause is a key that can't be read")
	}

	// The list page still loads, and shows the broken one with the reason.
	list := getJSON[[]map[string]any](t, client, base+"/api/indexers")
	if len(list) != 2 {
		t.Fatalf("got %d indexers, want 2", len(list))
	}
}

// A secret.key that belongs to another database is found at start, and is a
// Health item of its own.
func TestCheckSecretKeyFindsAKeyFromAnotherDatabase(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	mine, err := crypto.LoadOrCreateKey(filepath.Join(dir, "a.key"))
	if err != nil {
		t.Fatal(err)
	}
	other, err := crypto.LoadOrCreateKey(filepath.Join(dir, "b.key"))
	if err != nil {
		t.Fatal(err)
	}

	if err := api.CheckSecretKey(db, other); err != nil {
		t.Fatalf("a database with nothing stored matches any key: %v", err)
	}
	if err := settings.New(db, mine).Set(settings.KeyTMDBAPIKey, "abc", true); err != nil {
		t.Fatal(err)
	}
	if err := api.CheckSecretKey(db, mine); err != nil {
		t.Fatalf("the right key was refused: %v", err)
	}
	if err := api.CheckSecretKey(db, other); err != api.ErrKeyMismatch {
		t.Fatalf("got %v, want ErrKeyMismatch", err)
	}
}
