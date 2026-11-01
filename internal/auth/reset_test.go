package auth_test

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/store"
)

func resetService(t *testing.T) (*auth.Service, *sql.DB, *auth.User) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	svc := auth.New(db)
	if _, err := svc.CreateUser("ryan", "hunter2hunter2"); err != nil {
		t.Fatal(err)
	}
	u, err := svc.CreateUserWithProfile("sam", "old-password", "Sam", "", "Sam@Example.com")
	if err != nil {
		t.Fatal(err)
	}
	return svc, db, u
}

// Only an account that's on, has a password here and an email address can
// be reset by email: found by its name or its address, in any letter case.
func TestUserForReset(t *testing.T) {
	svc, db, sam := resetService(t)
	for _, who := range []string{"sam", "SAM", " sam@example.com "} {
		if u, err := svc.UserForReset(who); err != nil || u.ID != sam.ID {
			t.Errorf("%q: %v %v", who, u, err)
		}
	}
	for _, who := range []string{"ryan", "nobody", "", "nobody@example.com"} { // ryan has no email
		if _, err := svc.UserForReset(who); !errors.Is(err, auth.ErrUserNotFound) {
			t.Errorf("%q: %v, want no account", who, err)
		}
	}
	if _, err := db.Exec(`UPDATE users SET disabled = 1 WHERE id = ?`, sam.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UserForReset("sam"); !errors.Is(err, auth.ErrUserNotFound) {
		t.Errorf("a turned-off account: %v", err)
	}
}

// A link works once, for 30 minutes; only its hash is kept; a newer link and
// a password set another way end it; a reset ends every session.
func TestResetLinks(t *testing.T) {
	svc, db, sam := resetService(t)
	first, err := svc.CreateResetLink(sam.ID)
	if err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := db.QueryRow(`SELECT token_hash FROM password_resets WHERE user_id = ?`, sam.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == first || strings.Contains(stored, first) {
		t.Fatal("the link's secret is in the database")
	}
	second, err := svc.CreateResetLink(sam.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ResetLinkUser(first); !errors.Is(err, auth.ErrResetLinkBad) {
		t.Fatalf("an older link still works: %v", err)
	}
	session, _, err := svc.CreateSession(sam.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ResetWithLink(second, "a-new-password"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UserForSession(session); err == nil {
		t.Fatal("a session outlived the reset")
	}
	if _, err := svc.Authenticate("sam", "a-new-password"); err != nil {
		t.Fatalf("the new password: %v", err)
	}
	if _, err := svc.ResetWithLink(second, "yet-another-one"); !errors.Is(err, auth.ErrResetLinkBad) {
		t.Fatalf("a link worked twice: %v", err)
	}

	expired, _ := svc.CreateResetLink(sam.ID)
	if _, err := db.Exec(`UPDATE password_resets SET expires_at = '2000-01-01T00:00:00.000Z' WHERE used_at IS NULL`); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ResetWithLink(expired, "yet-another-one"); !errors.Is(err, auth.ErrResetLinkBad) {
		t.Fatalf("an expired link worked: %v", err)
	}

	open, _ := svc.CreateResetLink(sam.ID)
	if err := svc.ChangePassword(sam.ID, "a-new-password", "changed-by-sam"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ResetLinkUser(open); !errors.Is(err, auth.ErrResetLinkBad) {
		t.Fatalf("a link outlived a password change: %v", err)
	}
}
