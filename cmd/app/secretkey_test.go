package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/store"
)

func TestLoadSecretKeyCreatesAKeyForAnEmptyDatabase(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	keyPath := filepath.Join(dir, "secret.key")
	if _, err := loadSecretKey(db, keyPath); err != nil {
		t.Fatalf("fresh install: %v", err)
	}
	if _, err := os.Stat(keyPath); err != nil {
		t.Fatalf("key file was not written: %v", err)
	}
}

func TestLoadSecretKeyRefusesToReplaceAMissingKey(t *testing.T) {
	for name, stmt := range map[string]string{
		"encrypted setting":   `INSERT INTO settings (key, value, encrypted) VALUES ('tmdb.api_key', 'abc', 1)`,
		"user":                `INSERT INTO users (username, password_hash) VALUES ('ryan', 'x')`,
		"vpn connection":      `INSERT INTO vpn_configs (label, private_key_encrypted, peer_public_key, endpoint, local_addresses) VALUES ('v', 'x', 'y', 'e:1', '10.0.0.2/32')`,
		"notification secret": `INSERT INTO notification_targets (name, type, secrets_encrypted) VALUES ('n', 'webhook', 'abc')`,
		"telegram bot token":  `INSERT INTO notification_targets (name, type, bot_token_encrypted) VALUES ('n', 'telegram', 'abc')`,
		"media server token":  `INSERT INTO media_servers (name, kind, base_url, token_encrypted) VALUES ('p', 'plex', 'http://p', 'abc')`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			db, err := store.Open(filepath.Join(dir, "app.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err := db.Exec(stmt); err != nil {
				t.Fatal(err)
			}
			keyPath := filepath.Join(dir, "secret.key")
			_, err = loadSecretKey(db, keyPath)
			if err == nil || !strings.Contains(err.Error(), "put your old secret.key back") {
				t.Fatalf("want the missing-key message, got %v", err)
			}
			if !strings.Contains(err.Error(), "move app.db away to start again") {
				t.Fatalf("the message should say how to start fresh: %v", err)
			}
			if !strings.Contains(err.Error(), "no secret.key in "+dir+":") {
				t.Fatalf("the message should name the folder in use (%s): %v", dir, err)
			}
			if strings.Count(err.Error(), "secret.key back") != 1 {
				t.Fatalf("the message says the same thing twice: %v", err)
			}
			if _, statErr := os.Stat(keyPath); statErr == nil {
				t.Fatal("a new key was written anyway")
			}
		})
	}
}

// Rows with nothing encrypted in them do not stop a fresh key.
func TestLoadSecretKeyIgnoresRowsWithoutSecrets(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, stmt := range []string{
		`INSERT INTO notification_targets (name, type) VALUES ('n', 'webhook')`,
		`INSERT INTO media_servers (name, kind, base_url) VALUES ('p', 'plex', 'http://p')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := loadSecretKey(db, filepath.Join(dir, "secret.key")); err != nil {
		t.Fatalf("rows without secrets blocked a new key: %v", err)
	}
}
