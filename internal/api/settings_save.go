package api

import (
	"github.com/rdborg/mediarium/internal/settings"
)

// settingSetter is what setBool and the other small helpers need: somewhere to
// put a setting. Both the settings store (written at once) and a settingsSave
// (written together with the rest of an update) fit.
type settingSetter interface {
	Set(key, value string, encrypt bool) error
}

// settingsSave is one settings update on its way: the values to store, and what
// has to happen once they are stored (rebuilding a client that uses a new key,
// kicking the download queue). Nothing is written, and nothing happens, until
// commitSettings: an update that is refused or fails part way leaves every
// setting, secrets included, as it was.
type settingsSave struct {
	settings.Batch
	after []func()
}

// then adds something to do after the values are stored.
func (t *settingsSave) then(f func()) { t.after = append(t.after, f) }

// commitSettings stores everything in one database transaction and then runs
// what was waiting for it.
func (s *Server) commitSettings(t *settingsSave) error {
	if err := s.Settings.Apply(&t.Batch); err != nil {
		return err
	}
	for _, f := range t.after {
		f()
	}
	return nil
}

// stageTMDBAPIKey, stageOpenSubtitlesAccount and the others below put a secret
// into an update and say what to do with the live client afterwards. The
// exported Set... methods are these with an immediate commit.

func (s *Server) stageTMDBAPIKey(t *settingsSave, key string) {
	_ = t.Set(settings.KeyTMDBAPIKey, key, true)
	t.then(func() {
		s.tmdbMu.Lock()
		s.tmdb.SetAPIKey(key)
		s.tmdbMu.Unlock()
	})
}

// stageOpenSubtitlesAccount stores the login. keepPassword leaves the saved
// password alone (the person kept the same user name and left the password
// blank, or the saved one can't be read and so can't be carried over).
func (s *Server) stageOpenSubtitlesAccount(t *settingsSave, username, password string, keepPassword bool) {
	_ = t.Set(settings.KeyOpenSubtitlesUsername, username, false)
	if !keepPassword {
		_ = t.Set(settings.KeyOpenSubtitlesPassword, password, true)
	}
	t.then(s.rebuildSubtitles)
}

func (s *Server) stageOpenSubtitlesAPIKey(t *settingsSave, key string) {
	_ = t.Set(settings.KeyOpenSubtitlesAPIKey, key, true)
	t.then(s.rebuildSubtitles)
}

func (s *Server) stageSubDLAPIKey(t *settingsSave, key string) {
	_ = t.Set(settings.KeySubDLAPIKey, key, true)
	t.then(s.rebuildSubtitles)
}

func (s *Server) stageTraktClientID(t *settingsSave, id string) {
	_ = t.Set(settings.KeyTraktClientID, id, true)
	if id == "" {
		id = s.builtin.TraktClientID
	}
	t.then(func() {
		s.traktMu.Lock()
		s.trakt.SetClientID(id)
		s.traktMu.Unlock()
	})
}

func (s *Server) stageSimklClientID(t *settingsSave, id string) {
	_ = t.Set(settings.KeySimklClientID, id, true)
	if id == "" {
		id = s.builtin.SimklClientID
	}
	t.then(func() {
		s.simklMu.Lock()
		s.simkl.SetClientID(id)
		s.simklMu.Unlock()
	})
}

func (s *Server) stageTorrentsEnabled(t *settingsSave, enabled bool) {
	value := "1"
	if !enabled {
		value = "0"
	}
	_ = t.Set(settings.KeyTorrentEnabled, value, false)
	if !enabled {
		t.then(func() { s.torrents.stopAll() })
	}
}
