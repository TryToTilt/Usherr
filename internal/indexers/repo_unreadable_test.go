package indexers_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/store"
)

// One indexer whose saved key can't be read must not hide the others: it is
// listed and marked, the rest come back normally, and a search leaves it out.
func TestListMarksOnlyTheUnreadableIndexer(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	box, err := crypto.LoadOrCreateKey(filepath.Join(dir, "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	repo := indexers.NewRepo(db, box)

	good, err := repo.Create(indexers.Instance{Name: "Good", BaseURL: "https://good.test", APIKey: "good-key", Enabled: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	bad, err := repo.Create(indexers.Instance{Name: "Broken", BaseURL: "https://broken.test", APIKey: "broken-key", Enabled: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Written with another key, as after copying app.db to an install that has
	// its own secret.key.
	otherBox, err := crypto.LoadOrCreateKey(filepath.Join(dir, "other.key"))
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := otherBox.Encrypt("broken-key")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE indexers SET api_key_encrypted = ? WHERE id = ?`, foreign, bad.ID); err != nil {
		t.Fatal(err)
	}

	list, err := repo.List()
	if err != nil {
		t.Fatalf("one unreadable key must not fail the whole list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("got %d indexers, want 2", len(list))
	}
	for _, inst := range list {
		switch inst.ID {
		case good.ID:
			if inst.SecretError != "" || inst.APIKey != "good-key" {
				t.Errorf("the readable indexer was touched: %+v", inst)
			}
		case bad.ID:
			if !strings.Contains(inst.SecretError, "can't read the saved key of Broken") || inst.APIKey != "" {
				t.Errorf("the unreadable indexer was not marked: %+v", inst)
			}
		}
	}

	outcomes := indexers.SearchAll(t.Context(), list, "anything", nil)
	for _, o := range outcomes {
		if o.IndexerName == "Broken" {
			t.Errorf("the indexer with the unreadable key was searched")
		}
	}
}
