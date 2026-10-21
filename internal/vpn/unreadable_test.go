package vpn_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/store"
	"github.com/rdborg/mediarium/internal/vpn"
)

// A config saved with another key is listed and flagged, not an error that takes
// the whole list down; the readable ones keep working and nothing is rewritten.
func TestUnreadableConfigIsFlaggedNotFatal(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	oldBox, err := crypto.LoadOrCreateKey(filepath.Join(dir, "old.key"))
	if err != nil {
		t.Fatal(err)
	}
	newBox, err := crypto.LoadOrCreateKey(filepath.Join(dir, "new.key"))
	if err != nil {
		t.Fatal(err)
	}
	oldRepo, newRepo := vpn.NewRepo(db, oldBox), vpn.NewRepo(db, newBox)
	cfg := func(key string) vpn.Config {
		return vpn.Config{PrivateKey: key, PeerPublicKey: "pub", Endpoint: "vpn.example:51820", AllowedIPs: []string{"0.0.0.0/0"}, LocalAddresses: []string{"10.2.0.2/32"}}
	}
	lost, err := oldRepo.Create("Old key", "custom", cfg("priv-old"))
	if err != nil {
		t.Fatal(err)
	}
	if err := oldRepo.SetActive(lost.ID); err != nil {
		t.Fatal(err)
	}
	fine, err := newRepo.Create("New key", "custom", cfg("priv-new"))
	if err != nil {
		t.Fatal(err)
	}
	var before string
	if err := db.QueryRow(`SELECT private_key_encrypted FROM vpn_configs WHERE id = ?`, lost.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}

	list, err := newRepo.List()
	if err != nil {
		t.Fatalf("List failed because of one unreadable config: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("both configs should be listed: %+v", list)
	}
	for _, sc := range list {
		switch sc.ID {
		case lost.ID:
			if !sc.SecretUnreadable || sc.Config.PrivateKey != "" {
				t.Errorf("the config saved with the other key should be flagged and empty: %+v", sc)
			}
		case fine.ID:
			if sc.SecretUnreadable || sc.Config.PrivateKey != "priv-new" {
				t.Errorf("the readable config should be intact: %+v", sc)
			}
		}
	}
	usable := vpn.Usable(list)
	if len(usable) != 1 || usable[0].ID != fine.ID {
		t.Errorf("Usable = %+v, want only the readable one", usable)
	}
	active, ok, err := newRepo.Active()
	if err != nil || !ok || active.ID != lost.ID || !active.SecretUnreadable {
		t.Errorf("Active = %+v %v %v, want the flagged active config and no error", active, ok, err)
	}

	// Nothing was rewritten: the old key still opens it.
	var after string
	if err := db.QueryRow(`SELECT private_key_encrypted FROM vpn_configs WHERE id = ?`, lost.ID).Scan(&after); err != nil && err != sql.ErrNoRows {
		t.Fatal(err)
	}
	if after != before {
		t.Error("the unreadable key was rewritten")
	}
	if got, err := oldRepo.List(); err != nil || len(got) != 2 || got[0].SecretUnreadable && got[0].ID == lost.ID {
		t.Errorf("the old key should still read its config: %+v %v", got, err)
	}
}
