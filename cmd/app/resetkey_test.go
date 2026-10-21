package main

import (
	"bufio"
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/config"
	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/store"
)

// oneRowEach puts one row with an encrypted value into every table reset-key
// knows about. enc is the value stored in every secret column.
func oneRowEach(t *testing.T, db *sql.DB, enc string) {
	t.Helper()
	for _, stmt := range []string{
		`INSERT INTO settings (key, value, encrypted) VALUES ('debrid.api_key', ?, 1)`,
		`INSERT INTO indexers (name, definition_id, base_url, api_key_encrypted, enabled) VALUES ('Geek', 'd', 'http://x', ?, 1)`,
		`INSERT INTO download_clients (name, host, port, password_encrypted, enabled) VALUES ('Eweka', 'news.example.com', 563, ?, 1)`,
		`INSERT INTO vpn_configs (label, private_key_encrypted, peer_public_key, endpoint, local_addresses) VALUES ('Mullvad', ?, 'p', 'e:1', '10.0.0.2/32')`,
		`INSERT INTO notification_targets (name, type, secrets_encrypted, enabled) VALUES ('Phone', 'webhook', ?, 1)`,
		`INSERT INTO media_servers (name, kind, base_url, token_encrypted, enabled) VALUES ('Plex', 'plex', 'http://p', ?, 1)`,
		`INSERT INTO import_lists (name, source, secret, enabled) VALUES ('Trakt list', 'trakt', ?, 1)`,
		`INSERT INTO irc_connections (name, server, nick, auth_password_enc, enabled) VALUES ('Tracker', 'irc.example.com', 'me', ?, 1)`,
		`INSERT INTO users (username, password_hash, totp_secret, totp_enabled) VALUES ('ryan', 'x', ?, 1)`,
		`INSERT INTO ereader_access (user_id, kobo_token_enc, kobo_hash) VALUES ((SELECT id FROM users LIMIT 1), ?, 'h')`,
		`INSERT INTO music_listenbrainz (user_id, token) VALUES ((SELECT id FROM users LIMIT 1), ?)`,
		`INSERT INTO plex_watchlists (user_id, token) VALUES ((SELECT id FROM users LIMIT 1), ?)`,
		`INSERT INTO user_push_devices (user_id, name, subscription) VALUES ((SELECT id FROM users LIMIT 1), 'Pixel', ?)`,
	} {
		if _, err := db.Exec(stmt, enc); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
}

func resetLayout(t *testing.T) (config.Config, *sql.DB) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Config{ConfigDir: dir, DBPath: filepath.Join(dir, "app.db"), SecretKeyPath: filepath.Join(dir, "secret.key")}
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return cfg, db
}

func count(t *testing.T, db *sql.DB, q string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Every table in the list exists with the columns named, and a value no key can
// read is found in each of them.
func TestResetKeyKnowsEverySecretTable(t *testing.T) {
	_, db := resetLayout(t)
	oneRowEach(t, db, "not-a-real-ciphertext")
	found, err := findUnreadable(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, c := range found {
		seen[c.spec.table] = true
	}
	for _, spec := range secretTables {
		if !seen[spec.table] {
			t.Errorf("%s: its unreadable row was not found", spec.table)
		}
	}
}

func TestResetKeyWithAMissingKeyClearsEverythingAndMakesANewOne(t *testing.T) {
	cfg, db := resetLayout(t)
	oneRowEach(t, db, "ciphertext-of-a-lost-key")

	var out bytes.Buffer
	err := resetKey(cfg, true, bufio.NewReader(strings.NewReader("")), &out, time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("resetKey: %v\n%s", err, out.String())
	}

	// A new key works and nothing is left unreadable.
	box, err := crypto.LoadOrCreateKey(cfg.SecretKeyPath)
	if err != nil {
		t.Fatalf("the new key: %v", err)
	}
	if left, err := findUnreadable(db, box); err != nil || len(left) != 0 {
		t.Fatalf("still unreadable after the reset: %v %v", left, err)
	}
	for q, want := range map[string]int{
		`SELECT COUNT(*) FROM indexers WHERE enabled = 0 AND api_key_encrypted IS NULL`:          1,
		`SELECT COUNT(*) FROM download_clients WHERE enabled = 0 AND password_encrypted IS NULL`: 1,
		`SELECT COUNT(*) FROM media_servers WHERE enabled = 0 AND token_encrypted = ''`:          1,
		`SELECT COUNT(*) FROM notification_targets WHERE enabled = 0 AND secrets_encrypted = ''`: 1,
		`SELECT COUNT(*) FROM users WHERE totp_enabled = 0 AND totp_secret = ''`:                 1,
		`SELECT COUNT(*) FROM vpn_configs`:                                                       0,
		`SELECT COUNT(*) FROM settings WHERE encrypted = 1`:                                      0,
		`SELECT COUNT(*) FROM plex_watchlists`:                                                   0,
		`SELECT COUNT(*) FROM user_push_devices`:                                                 0,
		`SELECT COUNT(*) FROM ereader_access WHERE kobo_token_enc = '' AND kobo_hash = ''`:       1,
		`SELECT COUNT(*) FROM irc_connections WHERE enabled = 0 AND auth_password_enc = ''`:      1,
		`SELECT COUNT(*) FROM import_lists WHERE enabled = 0 AND secret = ''`:                    1,
		`SELECT COUNT(*) FROM music_listenbrainz WHERE enabled = 0 AND token = ''`:               1,
	} {
		if got := count(t, db, q); got != want {
			t.Errorf("%s = %d, want %d", q, got, want)
		}
	}
	// The rows themselves (names, addresses) are kept where they can be fixed.
	if got := count(t, db, `SELECT COUNT(*) FROM download_clients WHERE name = 'Eweka'`); got != 1 {
		t.Error("the Usenet server row should stay, so its password can be entered again")
	}

	// A copy of the database was kept, and it still holds the old values.
	copies, _ := filepath.Glob(filepath.Join(cfg.ConfigDir, "app.db.before-reset-key-*"))
	if len(copies) != 1 {
		t.Fatalf("expected one database copy, got %v", copies)
	}
	old, err := store.Open(copies[0])
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	if got := count(t, old, `SELECT COUNT(*) FROM vpn_configs`); got != 1 {
		t.Error("the database copy should still hold the VPN connection")
	}

	// The report names things and never shows a value.
	text := out.String()
	for _, want := range []string{`Usenet server "Eweka"`, `VPN connection "Mullvad"`, "Made a new secret.key"} {
		if !strings.Contains(text, want) {
			t.Errorf("the report should say %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "ciphertext-of-a-lost-key") {
		t.Error("the report shows a stored value")
	}

	// Run again: nothing to do.
	out.Reset()
	if err := resetKey(cfg, true, bufio.NewReader(strings.NewReader("")), &out, time.Now()); err != nil || !strings.Contains(out.String(), "Nothing was changed") {
		t.Errorf("second run: %v %q", err, out.String())
	}
}

func TestResetKeyKeepsAKeyThatWorksAndOnlyClearsWhatItCannotRead(t *testing.T) {
	cfg, db := resetLayout(t)
	box, err := crypto.LoadOrCreateKey(cfg.SecretKeyPath)
	if err != nil {
		t.Fatal(err)
	}
	keyBefore, _ := os.ReadFile(cfg.SecretKeyPath)
	good, err := box.Encrypt("good-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO download_clients (name, host, port, password_encrypted, enabled) VALUES ('Readable', 'a.example.com', 563, ?, 1)`, good); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO download_clients (name, host, port, password_encrypted, enabled) VALUES ('Lost', 'b.example.com', 563, 'from-another-key', 1)`); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := resetKey(cfg, true, bufio.NewReader(strings.NewReader("")), &out, time.Now()); err != nil {
		t.Fatal(err)
	}
	keyAfter, _ := os.ReadFile(cfg.SecretKeyPath)
	if !bytes.Equal(keyBefore, keyAfter) {
		t.Error("a key that works was replaced")
	}
	var enabled int
	var pw sql.NullString
	if err := db.QueryRow(`SELECT enabled, password_encrypted FROM download_clients WHERE name = 'Readable'`).Scan(&enabled, &pw); err != nil || enabled != 1 || pw.String != good {
		t.Errorf("the readable server was changed: %d %q %v", enabled, pw.String, err)
	}
	if err := db.QueryRow(`SELECT enabled, password_encrypted FROM download_clients WHERE name = 'Lost'`).Scan(&enabled, &pw); err != nil || enabled != 0 || pw.Valid {
		t.Errorf("the unreadable server should be switched off and empty: %d %+v %v", enabled, pw, err)
	}
	if strings.Contains(out.String(), "Made a new secret.key") {
		t.Errorf("no new key was needed:\n%s", out.String())
	}
	// The old key is kept as a copy too.
	if keys, _ := filepath.Glob(filepath.Join(cfg.ConfigDir, "secret.key.before-reset-key-*")); len(keys) != 1 {
		t.Errorf("expected a copy of the key, got %v", keys)
	}
}

func TestResetKeyAsksFirst(t *testing.T) {
	for _, tc := range []struct {
		name, answer string
		wantErr      bool
	}{
		{"no", "no\n", true},
		{"nothing typed", "", true},
		{"yes", "yes\n", false},
		{"capitals and spaces", "  YES \n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, db := resetLayout(t)
			oneRowEach(t, db, "lost")
			var out bytes.Buffer
			err := resetKey(cfg, false, bufio.NewReader(strings.NewReader(tc.answer)), &out, time.Now())
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if !strings.Contains(out.String(), "Type yes") {
				t.Errorf("the prompt should be shown:\n%s", out.String())
			}
			if tc.wantErr {
				if copies, _ := filepath.Glob(filepath.Join(cfg.ConfigDir, "*before-reset-key*")); len(copies) != 0 {
					t.Error("a refused reset still made a backup")
				}
				if _, statErr := os.Stat(cfg.SecretKeyPath); statErr == nil {
					t.Error("a refused reset still wrote a key")
				}
				if got := count(t, db, `SELECT COUNT(*) FROM vpn_configs`); got != 1 {
					t.Error("a refused reset changed the database")
				}
			}
		})
	}
}

func TestResetKeyDoesNothingWhenThereIsNothingToClear(t *testing.T) {
	cfg, _ := resetLayout(t)
	var out bytes.Buffer
	if err := resetKey(cfg, true, bufio.NewReader(strings.NewReader("")), &out, time.Now()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Nothing saved needs it") {
		t.Errorf("unexpected report: %q", out.String())
	}
	if _, err := os.Stat(cfg.SecretKeyPath); err == nil {
		t.Error("a key was written although nothing needed it")
	}
}

func TestResetKeyNeedsADatabase(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{ConfigDir: dir, DBPath: filepath.Join(dir, "app.db"), SecretKeyPath: filepath.Join(dir, "secret.key")}
	err := resetKey(cfg, true, bufio.NewReader(strings.NewReader("")), &bytes.Buffer{}, time.Now())
	if err == nil || !strings.Contains(err.Error(), "no database") {
		t.Fatalf("err = %v", err)
	}
	if _, statErr := os.Stat(cfg.DBPath); statErr == nil {
		t.Error("an empty database was created")
	}
}

func TestRunResetKeyArguments(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runResetKey([]string{"--nope"}, strings.NewReader(""), &out, &errOut); code != 2 || !strings.Contains(errOut.String(), "usage: app reset-key") {
		t.Errorf("bad flag: code %d %q", code, errOut.String())
	}
	out.Reset()
	if code := runResetKey([]string{"--help"}, strings.NewReader(""), &out, &errOut); code != 0 || !strings.Contains(out.String(), "--yes") {
		t.Errorf("help: code %d %q", code, out.String())
	}
}
