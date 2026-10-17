package main

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rdborg/mediarium/internal/crypto"
)

// loadSecretKey loads the encryption key. A missing key file is only replaced
// by a new one when the database holds nothing that was encrypted with the old
// one: otherwise a new key would be written, nothing could be decrypted and
// every later start would fail the same way.
func loadSecretKey(db *sql.DB, path string) (*crypto.Box, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		used, err := databaseHoldsSecrets(db)
		if err != nil {
			return nil, fmt.Errorf("check the database for stored secrets: %w", err)
		}
		if used {
			return nil, fmt.Errorf("app.db has saved passwords and keys but there is no secret.key in %s: put your old secret.key back in that folder, or if it is gone for good, move app.db away to start again", filepath.Dir(path))
		}
	}
	return crypto.LoadOrCreateKey(path)
}

// databaseHoldsSecrets reports whether the database already has encrypted
// settings, indexers, download clients (Usenet servers among them), VPN
// connections, notification targets with a secret or media servers with a
// token, or any user.
func databaseHoldsSecrets(db *sql.DB) (bool, error) {
	for _, q := range []string{
		`SELECT EXISTS(SELECT 1 FROM settings WHERE encrypted = 1)`,
		`SELECT EXISTS(SELECT 1 FROM users)`,
		`SELECT EXISTS(SELECT 1 FROM indexers)`,
		`SELECT EXISTS(SELECT 1 FROM download_clients)`,
		`SELECT EXISTS(SELECT 1 FROM vpn_configs)`,
		`SELECT EXISTS(SELECT 1 FROM notification_targets WHERE secrets_encrypted <> '' OR COALESCE(bot_token_encrypted, '') <> '')`,
		`SELECT EXISTS(SELECT 1 FROM media_servers WHERE token_encrypted <> '')`,
	} {
		var found bool
		if err := db.QueryRow(q).Scan(&found); err != nil {
			return false, err
		}
		if found {
			return true, nil
		}
	}
	return false, nil
}
