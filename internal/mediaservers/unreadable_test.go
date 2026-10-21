package mediaservers_test

import (
	"path/filepath"
	"testing"

	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/mediaservers"
	"github.com/rdborg/mediarium/internal/store"
)

// A server whose token was saved with another key is flagged, not an error for
// the whole list, and an edit without a new token leaves the stored token alone.
func TestUnreadableTokenIsFlaggedAndKeptOnEdit(t *testing.T) {
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
	oldRepo, newRepo := mediaservers.NewRepo(db, oldBox), mediaservers.NewRepo(db, newBox)
	lost, err := oldRepo.Create(mediaservers.Server{Name: "Plex", Kind: mediaservers.KindPlex, BaseURL: "http://plex:32400", Token: "plex-token", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	fine, err := newRepo.Create(mediaservers.Server{Name: "Jellyfin", Kind: mediaservers.KindJellyfin, BaseURL: "http://jf:8096", Token: "jf-token", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}

	all, err := newRepo.ListAll()
	if err != nil {
		t.Fatalf("ListAll failed because of one unreadable token: %v", err)
	}
	if len(all) != 2 || !all[0].SecretUnreadable || all[1].SecretUnreadable {
		t.Fatalf("ListAll = %+v, want the first flagged and the second readable", all)
	}
	usable, err := newRepo.List()
	if err != nil || len(usable) != 1 || usable[0].ID != fine.ID {
		t.Fatalf("List = %+v %v, want only the readable server", usable, err)
	}

	// Edit the name without a token: the stored one stays, byte for byte.
	var before string
	if err := db.QueryRow(`SELECT token_encrypted FROM media_servers WHERE id = ?`, lost.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	edit := all[0]
	edit.Name = "Living room Plex"
	edit.Token = ""
	if _, err := newRepo.Update(edit); err != nil {
		t.Fatal(err)
	}
	var after string
	if err := db.QueryRow(`SELECT token_encrypted FROM media_servers WHERE id = ?`, lost.ID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatal("an edit with no new token replaced the stored token")
	}
	if got, err := oldRepo.Get(lost.ID); err != nil || got.Token != "plex-token" || got.Name != "Living room Plex" {
		t.Errorf("the old key should still read the token, and the name should be saved: %+v %v", got, err)
	}

	// A new token replaces it and clears the flag.
	edit.Token = "fresh-token"
	if _, err := newRepo.Update(edit); err != nil {
		t.Fatal(err)
	}
	if got, err := newRepo.Get(lost.ID); err != nil || got.SecretUnreadable || got.Token != "fresh-token" {
		t.Errorf("after entering the token again: %+v %v", got, err)
	}
}
