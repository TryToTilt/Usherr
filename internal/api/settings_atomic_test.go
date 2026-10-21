package api_test

import (
	"net/http"
	"path/filepath"
	"testing"

	"github.com/rdborg/mediarium/internal/api"
	"github.com/rdborg/mediarium/internal/settings"
)

// A settings update is saved whole or not at all: when a later value can't be
// written, the earlier ones (secrets included) are not left behind.
func TestSettingsUpdateIsAllOrNothing(t *testing.T) {
	server, base, admin := loginNewServer(t)
	movies := filepath.ToSlash(filepath.Join(filepath.Dir(server.TestConfigDir()), "atomic-movies"))

	// A second connection puts a snag in the way of one late value.
	db, err := api.OpenTestDB(filepath.Join(server.TestConfigDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TRIGGER fail_one BEFORE INSERT ON settings WHEN NEW.key = 'library.unmonitor_finished_shows'
		BEGIN SELECT RAISE(ABORT, 'refused for the test'); END`); err != nil {
		t.Fatal(err)
	}

	update := map[string]any{
		"moviesPath":        movies,
		"tmdbApiKey":        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"subdlApiKey":       "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"unmonitorFinished": true,
		"seasonPacks":       true,
		"resumeInterrupted": true,
	}
	status, body := doJSONStatus(t, admin, http.MethodPut, base+"/api/settings", update)
	if status != http.StatusInternalServerError {
		t.Fatalf("status %d %v, want 500 from the failing write", status, body)
	}
	for _, key := range []string{settings.KeyMoviesPath, settings.KeyTMDBAPIKey, settings.KeySubDLAPIKey, settings.KeyUnmonitorFinished, settings.KeySeasonPacks, settings.KeyResumeInterrupted} {
		if v, _ := server.Settings.Get(key); v != "" {
			t.Errorf("%s = %q after a refused update, want nothing saved", key, v)
		}
	}

	// Without the snag the same update is saved in full, the secrets readable.
	if _, err := db.Exec(`DROP TRIGGER fail_one`); err != nil {
		t.Fatal(err)
	}
	if status, body := doJSONStatus(t, admin, http.MethodPut, base+"/api/settings", update); status != http.StatusOK {
		t.Fatalf("status %d %v, want 200", status, body)
	}
	for key, want := range map[string]string{
		settings.KeyMoviesPath:        movies,
		settings.KeyTMDBAPIKey:        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		settings.KeySubDLAPIKey:       "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		settings.KeyUnmonitorFinished: "1",
	} {
		if got, err := server.Settings.Get(key); err != nil || got != want {
			t.Errorf("%s = %q (%v), want %q", key, got, err, want)
		}
	}
}

// A refused value in the middle (checked before anything is written) saves
// nothing either.
func TestSettingsUpdateRefusedSavesNothing(t *testing.T) {
	server, base, admin := loginNewServer(t)
	status, body := doJSONStatus(t, admin, http.MethodPut, base+"/api/settings", map[string]any{
		"tmdbApiKey": "cccccccccccccccccccccccccccccccc",
		"moviesPath": "relative/not/allowed",
	})
	if status != http.StatusBadRequest {
		t.Fatalf("status %d %v, want 400", status, body)
	}
	if v, _ := server.Settings.Get(settings.KeyTMDBAPIKey); v != "" {
		t.Errorf("the TMDB key was saved (%q) although the update was refused", v)
	}
}
