package api

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/rdborg/mediarium/internal/store"
)

// Opening a database runs every migration, which takes the better part of a
// second (several with the race detector) and used to be paid by each of the
// hundreds of tests that start a server. The migrated file is made once for
// the whole test run and copied for each test instead.
var (
	templateOnce sync.Once
	templateDir  string
	templateFile string
	templateErr  error
)

// OpenTestDB is store.Open for tests: a database at path that is already
// migrated, from a copy of one made on first use. A path that exists already
// is opened as it is.
func OpenTestDB(path string) (*sql.DB, error) {
	if _, err := os.Stat(path); err == nil {
		return store.Open(path)
	}
	templateOnce.Do(makeTemplateDB)
	if templateErr != nil {
		return nil, templateErr
	}
	data, err := os.ReadFile(templateFile)
	if err != nil {
		return nil, fmt.Errorf("read the migrated template database: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return nil, fmt.Errorf("copy the migrated template database: %w", err)
	}
	return store.Open(path)
}

func makeTemplateDB() {
	dir, err := os.MkdirTemp("", "mediarium-api-testdb-*")
	if err != nil {
		templateErr = err
		return
	}
	file := filepath.Join(dir, "template.db")
	db, err := store.Open(file)
	if err != nil {
		templateErr = fmt.Errorf("make the template database: %w", err)
		return
	}
	// Fold the write-ahead log into the file, so the file alone is the database.
	if _, err := db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		db.Close()
		templateErr = fmt.Errorf("checkpoint the template database: %w", err)
		return
	}
	if err := db.Close(); err != nil {
		templateErr = err
		return
	}
	templateDir, templateFile = dir, file
}

// RemoveTestDBTemplate deletes the template once the tests are done.
func RemoveTestDBTemplate() {
	if templateDir != "" {
		os.RemoveAll(templateDir)
	}
}
