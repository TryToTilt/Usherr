package download_test

import (
	"path/filepath"
	"testing"

	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/download"
	"github.com/rdborg/mediarium/internal/store"
)

// A Usenet server whose password was saved with another key is flagged and left
// out of what downloads use; the list still loads, and an edit without a new
// password leaves the stored one alone.
func TestUnreadablePasswordIsFlaggedAndKeptOnEdit(t *testing.T) {
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
	oldRepo, newRepo := download.NewRepo(db, oldBox), download.NewRepo(db, newBox)
	cfg := func(host, pass string) download.ClientConfig {
		return download.ClientConfig{Host: host, Port: 563, UseSSL: true, Username: "u", Password: pass, Connections: 5}
	}
	lost, err := oldRepo.Create(download.StoredClient{Name: "Old", Config: cfg("old.example.com", "old-pass")})
	if err != nil {
		t.Fatal(err)
	}
	fine, err := newRepo.Create(download.StoredClient{Name: "New", Config: cfg("new.example.com", "new-pass"), Priority: 1})
	if err != nil {
		t.Fatal(err)
	}

	all, err := newRepo.ListAll()
	if err != nil {
		t.Fatalf("ListAll failed because of one unreadable password: %v", err)
	}
	if len(all) != 2 || !all[0].SecretUnreadable || all[0].Config.Password != "" || all[1].SecretUnreadable {
		t.Fatalf("ListAll = %+v, want the first flagged with no password and the second readable", all)
	}
	usable, err := newRepo.List()
	if err != nil || len(usable) != 1 || usable[0].ID != fine.ID {
		t.Fatalf("List = %+v %v, want only the readable server", usable, err)
	}

	var before string
	if err := db.QueryRow(`SELECT password_encrypted FROM download_clients WHERE id = ?`, lost.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	edit := all[0]
	edit.Name = "Old renamed"
	edit.Config.Password = ""
	if err := newRepo.Update(edit); err != nil {
		t.Fatal(err)
	}
	var after string
	if err := db.QueryRow(`SELECT password_encrypted FROM download_clients WHERE id = ?`, lost.ID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatal("an edit with no new password replaced the stored password")
	}
	if got, err := oldRepo.Get(lost.ID); err != nil || got.Config.Password != "old-pass" || got.Name != "Old renamed" {
		t.Errorf("the old key should still read the password: %+v %v", got, err)
	}

	edit.Config.Password = "typed-again"
	if err := newRepo.Update(edit); err != nil {
		t.Fatal(err)
	}
	if got, err := newRepo.Get(lost.ID); err != nil || got.SecretUnreadable || got.Config.Password != "typed-again" {
		t.Errorf("after entering the password again: %+v %v", got, err)
	}
}
