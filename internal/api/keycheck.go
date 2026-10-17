package api

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/rdborg/mediarium/internal/crypto"
)

// ErrKeyMismatch is what CheckSecretKey returns when the database holds saved
// keys and passwords but none of the ones it tried can be read with the
// encryption key in use. It is written for the person reading it.
var ErrKeyMismatch = errors.New("secret.key doesn't belong to this app.db, so the saved keys and passwords can't be read. Put back the secret.key that was next to this app.db, or re-enter your keys")

// CheckSecretKey tries to decrypt a few stored secrets with box. It returns nil
// when at least one can be read or when nothing is stored, and ErrKeyMismatch
// when some are stored and none can be read. One damaged value among readable
// ones does not count: that is reported for the item itself.
func CheckSecretKey(db *sql.DB, box *crypto.Box) error {
	tried := 0
	for _, q := range []string{
		`SELECT value FROM settings WHERE encrypted = 1 AND value <> '' LIMIT 3`,
		`SELECT api_key_encrypted FROM indexers WHERE COALESCE(api_key_encrypted, '') <> '' LIMIT 3`,
		`SELECT secrets_encrypted FROM indexers WHERE COALESCE(secrets_encrypted, '') <> '' LIMIT 3`,
		`SELECT password_encrypted FROM download_clients WHERE COALESCE(password_encrypted, '') <> '' LIMIT 3`,
	} {
		values, err := sampleColumn(db, q)
		if err != nil {
			return fmt.Errorf("check the encryption key: %w", err)
		}
		for _, v := range values {
			tried++
			if _, err := box.Decrypt(v); err == nil {
				return nil
			}
		}
	}
	if tried == 0 {
		return nil
	}
	return ErrKeyMismatch
}

func sampleColumn(db *sql.DB, query string) ([]string, error) {
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
