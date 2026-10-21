package notify_test

import (
	"path/filepath"
	"testing"

	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/notify"
	"github.com/rdborg/mediarium/internal/store"
)

// A target whose secrets were saved with another key is flagged, not an error
// for the whole list; it sends nothing, and an edit with no new secret leaves the
// stored ones alone.
func TestUnreadableSecretsAreFlaggedAndKeptOnEdit(t *testing.T) {
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
	oldRepo, newRepo := notify.NewRepo(db, oldBox), notify.NewRepo(db, newBox)
	lost, err := oldRepo.Create(notify.Target{Name: "Phone", Type: notify.TargetWebhook, URL: "https://example.com/hook/secret-path"})
	if err != nil {
		t.Fatal(err)
	}
	fine, err := newRepo.Create(notify.Target{Name: "Other", Type: notify.TargetWebhook, URL: "https://example.com/other/secret"})
	if err != nil {
		t.Fatal(err)
	}

	all, err := newRepo.ListAll()
	if err != nil {
		t.Fatalf("ListAll failed because of one unreadable target: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("ListAll = %+v", all)
	}
	flagged := 0
	for _, tg := range all {
		if tg.ID == lost.ID && tg.SecretUnreadable {
			flagged++
		}
		if tg.ID == fine.ID && tg.SecretUnreadable {
			t.Error("the readable target was flagged")
		}
	}
	if flagged != 1 {
		t.Fatalf("the target saved with the other key should be flagged: %+v", all)
	}
	usable, err := newRepo.List()
	if err != nil || len(usable) != 1 || usable[0].ID != fine.ID {
		t.Fatalf("List = %+v %v, want only the readable target", usable, err)
	}
	if senders, err := newRepo.Senders(); err != nil || len(senders) != 1 {
		t.Errorf("only the readable target should send: %d %v", len(senders), err)
	}

	// Rename it with no secret: what is stored stays.
	var before string
	if err := db.QueryRow(`SELECT secrets_encrypted FROM notification_targets WHERE id = ?`, lost.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	got, err := newRepo.Get(lost.ID)
	if err != nil {
		t.Fatal(err)
	}
	got.Name = "Phone alerts"
	got.Config = map[string]string{}
	got.URL = ""
	if _, err := newRepo.Update(got); err != nil {
		t.Fatal(err)
	}
	var after string
	if err := db.QueryRow(`SELECT secrets_encrypted FROM notification_targets WHERE id = ?`, lost.ID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before || after == "" {
		t.Fatal("an edit with no new secret replaced the stored secrets")
	}
	if back, err := oldRepo.Get(lost.ID); err != nil || back.Name != "Phone alerts" || back.Config["url"] != "https://example.com/hook/secret-path" {
		t.Errorf("the old key should still read the secret, and the name should be saved: %+v %v", back, err)
	}
}
