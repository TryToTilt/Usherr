package ereader

import (
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"net/mail"
	"strings"

	"github.com/rdborg/mediarium/internal/kosync"
)

// Device is an address an account can email its books to.
type Device struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Email   string `json:"email"`
	Kindle  bool   `json:"kindle"`
	Created string `json:"created,omitempty"`
}

// MaxDevices is how many addresses one account can save.
const MaxDevices = 8

var (
	// ErrBadDevice is returned for a name or address that can't be saved.
	ErrBadDevice = errors.New("enter a name and one valid email address")
	// ErrTooManyDevices is returned when the account already has MaxDevices.
	ErrTooManyDevices = fmt.Errorf("you can save up to %d addresses. Remove one first", MaxDevices)
	// ErrDuplicateDevice is returned for an address the account already saved.
	ErrDuplicateDevice = errors.New("you already saved that address")
)

// IsKindleAddress reports whether an address belongs to Amazon's Send to Kindle.
func IsKindleAddress(email string) bool {
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return false
	}
	switch strings.ToLower(email[at+1:]) {
	case "kindle.com", "free.kindle.com", "kindle.cn", "pbsync.com":
		return true
	}
	return false
}

// Devices lists an account's addresses.
func (r *Repo) Devices(userID int64) ([]Device, error) {
	rows, err := r.db.Query(`SELECT id, name, email, created_at FROM ereader_devices WHERE user_id = ? ORDER BY id`, userID)
	if err != nil {
		return nil, fmt.Errorf("read device addresses: %w", err)
	}
	defer rows.Close()
	out := []Device{}
	for rows.Next() {
		var d Device
		if err := rows.Scan(&d.ID, &d.Name, &d.Email, &d.Created); err != nil {
			return nil, fmt.Errorf("read device addresses: %w", err)
		}
		d.Kindle = IsKindleAddress(d.Email)
		out = append(out, d)
	}
	return out, rows.Err()
}

// Device returns one of the account's own addresses. Someone else's is
// sql.ErrNoRows, as if it did not exist.
func (r *Repo) Device(userID, id int64) (Device, error) {
	var d Device
	err := r.db.QueryRow(`SELECT id, name, email, created_at FROM ereader_devices WHERE id = ? AND user_id = ?`, id, userID).Scan(&d.ID, &d.Name, &d.Email, &d.Created)
	if err != nil {
		return Device{}, err
	}
	d.Kindle = IsKindleAddress(d.Email)
	return d, nil
}

// AddDevice saves an address for the account.
func (r *Repo) AddDevice(userID int64, name, email string) error {
	name = strings.Join(strings.Fields(name), " ")
	email = strings.TrimSpace(email)
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email || name == "" || len(name) > 60 || len(email) > 254 {
		return ErrBadDevice
	}
	var n int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM ereader_devices WHERE user_id = ?`, userID).Scan(&n); err != nil {
		return fmt.Errorf("count device addresses: %w", err)
	}
	if n >= MaxDevices {
		return ErrTooManyDevices
	}
	if _, err := r.db.Exec(`INSERT INTO ereader_devices (user_id, name, email) VALUES (?, ?, ?)`, userID, name, strings.ToLower(email)); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return ErrDuplicateDevice
		}
		return fmt.Errorf("save the device address: %w", err)
	}
	return nil
}

// RemoveDevice removes one of the account's own addresses.
func (r *Repo) RemoveDevice(userID, id int64) error {
	res, err := r.db.Exec(`DELETE FROM ereader_devices WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return fmt.Errorf("remove the device address: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// CheckKOSyncKey reports whether key signs the account in on KOSync. KOReader
// and Readest send the MD5 of the password as it was typed, which is why the
// password has to be readable (a password made before that was kept can't sign
// in this way until a new one is made). With or without its dashes both count,
// and so does the password itself, for an app that sends it as HTTP Basic.
func (r *Repo) CheckKOSyncKey(userID int64, key string) bool {
	secret, err := r.OPDSSecret(userID)
	if err != nil {
		secret = ""
	}
	given := []byte(strings.ToLower(strings.TrimSpace(key)))
	matched := 0
	if secret != "" {
		matched |= subtle.ConstantTimeCompare(given, []byte(kosync.MD5Hex(secret)))
		matched |= subtle.ConstantTimeCompare(given, []byte(kosync.MD5Hex(NormalizePassword(secret))))
	}
	if r.CheckOPDS(userID, key) {
		matched = 1
	}
	return matched == 1
}

// KOSyncReady reports whether the account's app password can sign in on KOSync.
func (r *Repo) KOSyncReady(userID int64) bool {
	secret, err := r.OPDSSecret(userID)
	return err == nil && secret != ""
}
