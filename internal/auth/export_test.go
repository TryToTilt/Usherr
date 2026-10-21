package auth

import (
	"database/sql"
	"time"
)

// SetHashCostForTest changes the bcrypt work factor for new hashes and returns
// a function that puts it back.
func SetHashCostForTest(cost int) (restore func()) {
	old := hashCost
	hashCost = cost
	return func() { hashCost = old }
}

// Size is how many keys the limiter is tracking.
func (l *LoginLimiter) Size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.attempts)
}

// SetClockForTest makes the service read the time from now instead of the clock.
func (s *Service) SetClockForTest(now func() time.Time) { s.clock = now }

// SessionLastUsedForTest is the last-used time stored for a session ("" when none).
func (s *Service) SessionLastUsedForTest(token string) string {
	var v sql.NullString
	if err := s.db.QueryRow(`SELECT last_used_at FROM sessions WHERE token_hash = ?`, hashToken(token)).Scan(&v); err != nil {
		return ""
	}
	return v.String
}

// ClearSessionUsedForTest puts a session back as it was before last-used
// times were kept (the column empty).
func (s *Service) ClearSessionUsedForTest(token string) {
	_, _ = s.db.Exec(`UPDATE sessions SET last_used_at = NULL WHERE token_hash = ?`, hashToken(token))
}

// SessionCountForTest is how many sessions are stored.
func (s *Service) SessionCountForTest() int {
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&n)
	return n
}

// StoredHashForTest is the password hash stored for an account.
func (s *Service) StoredHashForTest(id int64) string {
	var h string
	if err := s.db.QueryRow(`SELECT password_hash FROM users WHERE id = ?`, id).Scan(&h); err != nil {
		return ""
	}
	return h
}
