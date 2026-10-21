package auth

import (
	"database/sql"
	"fmt"
	"time"
)

// Signing out after being idle. An administrator can choose that a browser
// session which has not been used for some days ends. It is off by default.
// It only touches browser sessions: API keys, calendar feeds and the OPDS and
// e-reader passwords never go through here.

// touchEvery is the shortest time between two writes of one session's
// last-used time, so a busy page does not write to the database on every call.
const touchEvery = time.Minute

// SetIdleTimeout tells the service how long a browser session may go unused
// before it ends. f is asked on every check, so a change of the setting takes
// effect at once; a result of zero or less means no limit. Call it before the
// server starts answering.
func (s *Service) SetIdleTimeout(f func() time.Duration) { s.idle = f }

func (s *Service) now() time.Time {
	if s.clock != nil {
		return s.clock()
	}
	return time.Now()
}

func (s *Service) idleLimit() time.Duration {
	if s.idle == nil {
		return 0
	}
	return s.idle()
}

// parseStored reads a time the database holds.
func parseStored(v string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339Nano, v)
	return t, err == nil
}

// lastUsedOf is when the session was last used: its last-used time, or the
// time it was created for a session from before that was kept.
func lastUsedOf(created string, lastUsed sql.NullString) (time.Time, bool) {
	if lastUsed.Valid {
		if t, ok := parseStored(lastUsed.String); ok {
			return t, true
		}
	}
	return parseStored(created)
}

// idleOver reports whether the session has gone unused for longer than the
// limit that is set now.
func (s *Service) idleOver(created string, lastUsed sql.NullString, now time.Time) bool {
	limit := s.idleLimit()
	if limit <= 0 {
		return false
	}
	t, ok := lastUsedOf(created, lastUsed)
	return ok && now.Sub(t) > limit
}

// checkIdle ends a session that was idle for too long (it stays marked, so the
// person is told why and the session cannot come back by a change of the
// setting), and otherwise notes that it was used just now.
func (s *Service) checkIdle(token, created string, lastUsed, idleEnded sql.NullString, now time.Time) error {
	hash := hashToken(token)
	if idleEnded.Valid {
		return ErrSessionIdle
	}
	if s.idleOver(created, lastUsed, now) {
		if _, err := s.db.Exec(`UPDATE sessions SET idle_ended_at = ? WHERE token_hash = ? AND idle_ended_at IS NULL`, now.UTC().Format(time.RFC3339Nano), hash); err != nil {
			return fmt.Errorf("mark idle session: %w", err)
		}
		return ErrSessionIdle
	}
	// Used: remember when, at most once a minute.
	if t, ok := lastUsedOf(created, lastUsed); !ok || now.Sub(t) >= touchEvery {
		_, _ = s.db.Exec(`UPDATE sessions SET last_used_at = ? WHERE token_hash = ?`, now.UTC().Format(time.RFC3339Nano), hash)
	}
	return nil
}

// PurgeSessions removes the sessions that have run out (a session ended for
// being idle is removed once it would have run out anyway) and returns how many.
func (s *Service) PurgeSessions() (int64, error) {
	res, err := s.db.Exec(`DELETE FROM sessions WHERE expires_at < ?`, s.now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, fmt.Errorf("remove expired sessions: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
