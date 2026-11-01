package auth

// Usherr: setting a new password by email (Forgot password? on the sign-in
// page). A link carries 32 random bytes; only their SHA-256 hash is kept. A
// link works once, for ResetLinkLife; a newer link, or the password being set
// any other way, ends the account's older ones. Setting the password through
// a link does what an administrator's reset does: every session ends and apps
// signed in over MCP lose their grants. Two-step sign-in stays on.

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// ResetLinkLife is how long a reset link works.
const ResetLinkLife = 30 * time.Minute

// ErrResetLinkBad is a reset link that is unknown, used, expired, or for an
// account that can no longer use it. Which of those is never said.
var ErrResetLinkBad = errors.New("this link has expired or was already used")

// resetTime is how reset times are stored: like strftime's
// '%Y-%m-%dT%H:%M:%fZ', so they compare as text.
const resetTime = "2006-01-02T15:04:05.000Z"

// endResetLinks ends every unused reset link of an account: a newer link was
// made, or the password was set another way.
func endResetLinks(exec func(string, ...any) (sql.Result, error), userID int64) error {
	if _, err := exec(`DELETE FROM password_resets WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("end reset links: %w", err)
	}
	return nil
}

// UserForReset is the account a Forgot password? request is about: who is
// its user name or, when exactly one account has it, its email address
// (letter case doesn't matter). Only an account that is turned on, has a
// password here and has an email address can be reset by email; for any
// other the answer is ErrUserNotFound, the same as for no account at all.
func (s *Service) UserForReset(who string) (*User, error) {
	who = strings.TrimSpace(who)
	if who == "" || len(who) > 254 {
		return nil, ErrUserNotFound
	}
	pick := func(where string) (*User, error) {
		rows, err := s.db.Query(`SELECT `+userCols+` FROM users WHERE `+where+` COLLATE NOCASE`, who)
		if err != nil {
			return nil, fmt.Errorf("look up account: %w", err)
		}
		defer rows.Close()
		var found []*User
		for rows.Next() {
			u := &User{}
			if err := rows.Scan(u.scanDest()...); err != nil {
				return nil, fmt.Errorf("look up account: %w", err)
			}
			found = append(found, u)
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("look up account: %w", err)
		}
		// Two accounts named alike in letter case, or sharing an address:
		// the exact name wins, otherwise nobody is guessed at.
		for _, u := range found {
			if u.Username == who {
				return u, nil
			}
		}
		if len(found) == 1 {
			return found[0], nil
		}
		return nil, ErrUserNotFound
	}
	u, err := pick(`username = ?`)
	if errors.Is(err, ErrUserNotFound) && strings.Contains(who, "@") {
		u, err = pick(`email = ?`)
	}
	if err != nil {
		return nil, err
	}
	if u.Disabled || !u.IsLocal() || strings.TrimSpace(u.Email) == "" {
		return nil, ErrUserNotFound
	}
	var hash string
	if err := s.db.QueryRow(`SELECT password_hash FROM users WHERE id = ?`, u.ID).Scan(&hash); err != nil || hash == "" {
		return nil, ErrUserNotFound
	}
	return u, nil
}

// CreateResetLink makes a reset link's secret for an account, ending its
// older links (and long-gone ones of every account). Only this call sees the
// secret: the database keeps its hash.
func (s *Service) CreateResetLink(userID int64) (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	tx, err := s.db.Begin()
	if err != nil {
		return "", fmt.Errorf("begin reset link: %w", err)
	}
	defer tx.Rollback()
	if err := endResetLinks(tx.Exec, userID); err != nil {
		return "", err
	}
	if _, err := tx.Exec(`DELETE FROM password_resets WHERE expires_at < ?`, now.Add(-24*time.Hour).Format(resetTime)); err != nil {
		return "", fmt.Errorf("tidy reset links: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO password_resets (user_id, token_hash, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		userID, hashToken(token), now.Format(resetTime), now.Add(ResetLinkLife).Format(resetTime)); err != nil {
		return "", fmt.Errorf("save reset link: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit reset link: %w", err)
	}
	return token, nil
}

// ResetLinkUser is the account a reset link is for, while the link still
// works and the account can still use it.
func (s *Service) ResetLinkUser(token string) (*User, error) {
	if token == "" || len(token) > 128 {
		return nil, ErrResetLinkBad
	}
	var id int64
	err := s.db.QueryRow(`SELECT user_id FROM password_resets WHERE token_hash = ? AND used_at IS NULL AND expires_at > ?`,
		hashToken(token), time.Now().UTC().Format(resetTime)).Scan(&id)
	if err != nil {
		return nil, ErrResetLinkBad
	}
	u, err := s.GetUser(id)
	if err != nil || u.Disabled || !u.IsLocal() {
		return nil, ErrResetLinkBad
	}
	return u, nil
}

// ResetWithLink sets a new password through a reset link. The link is used
// up in the same step, so two tries at once can't both set a password.
// Every session of the account ends and apps lose their MCP grants; two-step
// sign-in is left as it is.
func (s *Service) ResetWithLink(token, newPassword string) (*User, error) {
	if len(newPassword) > MaxPasswordBytes {
		return nil, ErrPasswordTooLong
	}
	u, err := s.ResetLinkUser(token)
	if err != nil {
		return nil, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), hashCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}
	now := time.Now().UTC().Format(resetTime)
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin reset by link: %w", err)
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE password_resets SET used_at = ? WHERE token_hash = ? AND user_id = ? AND used_at IS NULL AND expires_at > ?`,
		now, hashToken(token), u.ID, now)
	if err != nil {
		return nil, fmt.Errorf("use reset link: %w", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return nil, ErrResetLinkBad
	}
	if _, err := tx.Exec(`UPDATE users SET password_hash = ? WHERE id = ? AND auth_source = 'local' AND disabled = 0`, string(hash), u.ID); err != nil {
		return nil, fmt.Errorf("update password: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM sessions WHERE user_id = ?`, u.ID); err != nil {
		return nil, fmt.Errorf("clear sessions: %w", err)
	}
	if err := revokeUserGrants(tx.Exec, u.ID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`DELETE FROM password_resets WHERE user_id = ? AND used_at IS NULL`, u.ID); err != nil {
		return nil, fmt.Errorf("end other reset links: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit reset by link: %w", err)
	}
	return u, nil
}
