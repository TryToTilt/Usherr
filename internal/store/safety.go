package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// keepSafetyCopies is how many of the copies made before schema changes are
// kept next to the database.
const keepSafetyCopies = 2

const safetyCopyPrefix = "pre-migration-"

// appliedVersions returns the schema changes the database says it has had.
func appliedVersions(conn *sql.DB) (map[string]bool, error) {
	rows, err := conn.Query(`SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("read schema_migrations: %w", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("read schema_migrations: %w", err)
		}
		out[v] = true
	}
	return out, rows.Err()
}

// NewerMessage is the plain sentence for a database that a newer version of
// Mediarium has already changed. versions are the changes this program does not
// know.
func NewerMessage(versions []string) string {
	return fmt.Sprintf("This database was updated by a newer version of Mediarium (it has changes this version doesn't know, the newest is %s). Update to the newer version again to keep using it safely, or restore a backup made before that update.", versions[len(versions)-1])
}

func isMemory(path string) bool {
	return path == ":memory:" || path == "" || strings.Contains(path, "mode=memory")
}

// snapshotBeforeMigrating writes a copy of the database to
// pre-migration-<version>-<YYYYMMDD-HHMMSS>.db next to it, readable by the
// owner only, and keeps the newest keepSafetyCopies of them. version is the
// last change about to be applied. The time stamp keeps a second update to the
// same change (after a backup is restored, say) from replacing the copy made
// at the first one.
func snapshotBeforeMigrating(conn *sql.DB, path, version string) (string, error) {
	dir := filepath.Dir(path)
	base := safetyCopyPrefix + version + "-" + time.Now().Format("20060102-150405")
	final := filepath.Join(dir, base+".db")
	for i := 2; ; i++ { // two starts within the same second
		if _, err := os.Lstat(final); os.IsNotExist(err) {
			break
		}
		final = filepath.Join(dir, fmt.Sprintf("%s-%d.db", base, i))
	}
	// VACUUM INTO makes the file itself with the default mode, so it goes into
	// a folder only the owner can open and is moved out once it is complete.
	work, err := os.MkdirTemp(dir, ".pre-migration-")
	if err != nil {
		return "", fmt.Errorf("make a folder for the copy: %w", err)
	}
	defer os.RemoveAll(work)
	tmp := filepath.Join(work, "copy.db")
	if _, err := conn.Exec("VACUUM INTO '" + strings.ReplaceAll(tmp, "'", "''") + "'"); err != nil {
		return "", fmt.Errorf("copy the database: %w", err)
	}
	_ = os.Chmod(tmp, 0o600)
	if err := os.Rename(tmp, final); err != nil {
		return "", fmt.Errorf("save the copy: %w", err)
	}
	pruneSafetyCopies(dir)
	return final, nil
}

// pruneSafetyCopies removes all but the newest keepSafetyCopies copies.
func pruneSafetyCopies(dir string) {
	matches, _ := filepath.Glob(filepath.Join(dir, safetyCopyPrefix+"*.db"))
	type file struct {
		path string
		mod  int64
	}
	var files []file
	for _, m := range matches {
		if info, err := os.Stat(m); err == nil {
			files = append(files, file{m, info.ModTime().UnixNano()})
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod > files[j].mod })
	for _, f := range files[min(keepSafetyCopies, len(files)):] {
		_ = os.Remove(f.path)
	}
}
