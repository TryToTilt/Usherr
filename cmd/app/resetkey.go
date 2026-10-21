package main

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/config"
	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/store"
)

const resetKeyUsage = `usage: app reset-key [--yes]

For when secret.key is lost or does not belong to this app.db, and the saved
passwords and keys can no longer be read. It clears every one of them that can't
be read (the things they belong to are switched off until you enter them again),
keeps a copy of the database and the old key first, and makes a new secret.key
when there is none that works. Stop Mediarium before you run it and start it again
afterwards.

  --yes   do not ask for a confirmation

In Docker:  docker run --rm -it -v /your/config:/config <image> /app/app reset-key
`

// secretColumn is one column that holds an encrypted value.
type secretColumn struct {
	name  string // the column
	empty string // what a cleared value becomes: NULL or ''
	what  string // the secret in plain words, for the report
}

// secretTable is a table that holds encrypted columns, and how to clear a row
// whose values can't be read.
type secretTable struct {
	table    string
	idCol    string
	label    string // an SQL expression (the table is aliased t) naming the row for the report
	what     string // the row in plain words: "Usenet server"
	where    string // an extra condition
	cols     []secretColumn
	also     string // extra SET to clear a row with: "enabled = 0"
	delete   bool   // remove the row instead (it is useless without its secret)
	switched string // what happens to the row, for the report
}

// secretTables is every place Mediarium keeps an encrypted value. A new one has
// to be added here (a test checks every table and column exists).
var secretTables = []secretTable{
	{table: "settings", idCol: "key", label: "t.key", what: "Setting", where: "t.encrypted = 1",
		cols: []secretColumn{{"value", "", "saved value"}}, delete: true},
	{table: "indexers", idCol: "id", label: "t.name", what: "Indexer",
		cols: []secretColumn{{"api_key_encrypted", "NULL", "key"}, {"secrets_encrypted", "NULL", "login details"}}, also: "enabled = 0"},
	{table: "download_clients", idCol: "id", label: "t.name", what: "Usenet server",
		cols: []secretColumn{{"password_encrypted", "NULL", "password"}}, also: "enabled = 0"},
	{table: "vpn_configs", idCol: "id", label: "t.label", what: "VPN connection",
		cols: []secretColumn{{"private_key_encrypted", "''", "private key"}, {"preshared_key_encrypted", "NULL", "preshared key"}}, delete: true},
	{table: "notification_targets", idCol: "id", label: "t.name", what: "Notification",
		cols: []secretColumn{{"bot_token_encrypted", "NULL", "bot token"}, {"secrets_encrypted", "''", "passwords and addresses"}}, also: "enabled = 0"},
	{table: "media_servers", idCol: "id", label: "t.name", what: "Media server",
		cols: []secretColumn{{"token_encrypted", "''", "token"}}, also: "enabled = 0"},
	{table: "import_lists", idCol: "id", label: "t.name", what: "Import list",
		cols: []secretColumn{{"secret", "''", "key"}}, also: "enabled = 0"},
	{table: "irc_connections", idCol: "id", label: "t.name", what: "IRC announce connection",
		cols: []secretColumn{{"auth_password_enc", "''", "login password"}, {"invite_enc", "''", "invite command"}, {"secrets_enc", "''", "channel and tracker keys"}}, also: "enabled = 0"},
	{table: "users", idCol: "id", label: "t.username", what: "Two-step sign-in of",
		cols: []secretColumn{{"totp_secret", "''", "authenticator code"}}, also: "totp_enabled = 0, totp_last_step = 0, recovery_codes = ''",
		switched: "two-step sign-in was switched off for this account"},
	{table: "ereader_access", idCol: "user_id", label: "(SELECT username FROM users WHERE id = t.user_id)", what: "Kobo sync address of",
		cols: []secretColumn{{"kobo_token_enc", "''", "address"}}, also: "kobo_hash = '', kobo_created_at = ''"},
	{table: "music_listenbrainz", idCol: "user_id", label: "(SELECT username FROM users WHERE id = t.user_id)", what: "ListenBrainz token of",
		cols: []secretColumn{{"token", "''", "token"}}, also: "enabled = 0"},
	{table: "plex_watchlists", idCol: "user_id", label: "(SELECT username FROM users WHERE id = t.user_id)", what: "Plex watchlist link of",
		cols: []secretColumn{{"token", "''", "token"}}, delete: true},
	{table: "user_push_devices", idCol: "id", label: "(SELECT username FROM users WHERE id = t.user_id) || ' / ' || t.name", what: "Phone alerts of",
		cols: []secretColumn{{"subscription", "''", "subscription"}}, delete: true},
}

// cleared is one row found with a secret that can't be read.
type cleared struct {
	spec  *secretTable
	id    any
	label string
	bad   []secretColumn // the columns that can't be read
}

// describe is the report line for the row. It never holds a value.
func (c cleared) describe() string {
	what := make([]string, len(c.bad))
	for i, col := range c.bad {
		what[i] = col.what
	}
	line := fmt.Sprintf("%s %q: %s", c.spec.what, c.label, strings.Join(what, ", "))
	switch {
	case c.spec.switched != "":
		line += " (" + c.spec.switched + ")"
	case c.spec.delete:
		line += " (removed, add it again)"
	case strings.Contains(c.spec.also, "enabled = 0"):
		line += " (switched off until you enter it again)"
	default:
		line += " (enter it again)"
	}
	return line
}

// findUnreadable lists every row of secretTables with a value that box can't
// read; a nil box can read nothing.
func findUnreadable(db *sql.DB, box *crypto.Box) ([]cleared, error) {
	var out []cleared
	for i := range secretTables {
		spec := &secretTables[i]
		names := make([]string, len(spec.cols))
		for j, c := range spec.cols {
			names[j] = "COALESCE(t." + c.name + ", '')"
		}
		where := "(" + strings.Join(names, " <> '' OR ") + " <> '')"
		if spec.where != "" {
			where += " AND " + spec.where
		}
		q := "SELECT t." + spec.idCol + ", COALESCE(" + spec.label + ", ''), " + strings.Join(names, ", ") + " FROM " + spec.table + " t WHERE " + where + " ORDER BY t." + spec.idCol
		rows, err := db.Query(q)
		if err != nil {
			return nil, fmt.Errorf("look at the saved secrets in %s: %w", spec.table, err)
		}
		for rows.Next() {
			var id any
			var label string
			vals := make([]string, len(spec.cols))
			dest := []any{&id, &label}
			for j := range vals {
				dest = append(dest, &vals[j])
			}
			if err := rows.Scan(dest...); err != nil {
				rows.Close()
				return nil, fmt.Errorf("read the saved secrets in %s: %w", spec.table, err)
			}
			c := cleared{spec: spec, id: id, label: label}
			for j, v := range vals {
				if v == "" {
					continue
				}
				if box == nil {
					c.bad = append(c.bad, spec.cols[j])
					continue
				}
				if _, err := box.Decrypt(v); err != nil {
					c.bad = append(c.bad, spec.cols[j])
				}
			}
			if len(c.bad) > 0 {
				out = append(out, c)
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}

// clearSecrets clears the rows found, all in one transaction.
func clearSecrets(db *sql.DB, found []cleared) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, c := range found {
		var stmt string
		if c.spec.delete {
			stmt = "DELETE FROM " + c.spec.table + " WHERE " + c.spec.idCol + " = ?"
		} else {
			sets := make([]string, 0, len(c.bad)+1)
			for _, col := range c.bad {
				sets = append(sets, col.name+" = "+col.empty)
			}
			if c.spec.also != "" {
				sets = append(sets, c.spec.also)
			}
			stmt = "UPDATE " + c.spec.table + " SET " + strings.Join(sets, ", ") + " WHERE " + c.spec.idCol + " = ?"
		}
		if _, err := tx.Exec(stmt, c.id); err != nil {
			return fmt.Errorf("clear %s %v: %w", c.spec.table, c.id, err)
		}
	}
	return tx.Commit()
}

// backupBeforeReset keeps a copy of the database (a consistent snapshot) and of
// the key file, if there is one, next to them. It returns the paths made.
func backupBeforeReset(ctx context.Context, db *sql.DB, cfg config.Config, now time.Time) ([]string, error) {
	stamp := now.Format("20060102-150405")
	dir := filepath.Dir(cfg.DBPath)
	// VACUUM INTO makes the file with the default mode and will not overwrite
	// one: it goes into a private folder first, and is moved out from there.
	tmp, err := os.MkdirTemp(dir, ".reset-key-")
	if err != nil {
		return nil, fmt.Errorf("make a folder for the backup: %w", err)
	}
	defer os.RemoveAll(tmp)
	snap := filepath.Join(tmp, "app.db")
	if _, err := db.ExecContext(ctx, "VACUUM INTO '"+strings.ReplaceAll(snap, "'", "''")+"'"); err != nil {
		return nil, fmt.Errorf("copy the database: %w", err)
	}
	var made []string
	dbCopy := filepath.Join(dir, filepath.Base(cfg.DBPath)+".before-reset-key-"+stamp)
	if err := os.Rename(snap, dbCopy); err != nil {
		return nil, fmt.Errorf("keep the database copy: %w", err)
	}
	_ = os.Chmod(dbCopy, 0o600)
	made = append(made, dbCopy)
	if data, err := os.ReadFile(cfg.SecretKeyPath); err == nil {
		keyCopy := filepath.Join(filepath.Dir(cfg.SecretKeyPath), filepath.Base(cfg.SecretKeyPath)+".before-reset-key-"+stamp)
		if err := os.WriteFile(keyCopy, data, 0o600); err != nil {
			return nil, fmt.Errorf("keep the key copy: %w", err)
		}
		made = append(made, keyCopy)
	}
	return made, nil
}

// currentKey loads the key file. A missing or damaged file gives a nil box and
// a note about why.
func currentKey(path string) (*crypto.Box, string) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil, "There is no secret.key in " + filepath.Dir(path) + "."
	}
	box, err := crypto.LoadOrCreateKey(path)
	if err != nil {
		return nil, "The secret.key in " + filepath.Dir(path) + " can't be used: it is damaged."
	}
	return box, ""
}

// resetKey is `app reset-key`. in is where the confirmation is read from.
func resetKey(cfg config.Config, yes bool, in *bufio.Reader, out io.Writer, now time.Time) error {
	if _, err := os.Stat(cfg.DBPath); err != nil {
		return fmt.Errorf("there is no database at %s (is CONFIG_DIR the right folder?)", cfg.DBPath)
	}
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open database at %s: %w", cfg.DBPath, err)
	}
	defer db.Close()

	box, keyNote := currentKey(cfg.SecretKeyPath)
	found, err := findUnreadable(db, box)
	if err != nil {
		return err
	}
	if len(found) == 0 {
		if box != nil {
			fmt.Fprintln(out, "Every saved password and key can be read with the secret.key in "+filepath.Dir(cfg.SecretKeyPath)+". Nothing was changed.")
		} else {
			fmt.Fprintln(out, keyNote+" Nothing saved needs it, so nothing was changed: Mediarium makes a new key by itself when it starts.")
		}
		return nil
	}

	lines := make([]string, len(found))
	for i, c := range found {
		lines[i] = "  - " + c.describe()
	}
	sort.Strings(lines)
	fmt.Fprintf(out, "%d saved passwords or keys can't be read with the secret.key in %s:\n%s\n\n", len(found), filepath.Dir(cfg.SecretKeyPath), strings.Join(lines, "\n"))
	if box == nil {
		fmt.Fprintln(out, keyNote+" A new one will be made.")
	} else {
		fmt.Fprintln(out, "The secret.key that is there works for the rest, so it is kept.")
	}
	fmt.Fprintln(out, "Clearing them cannot be undone, but a copy of the database (and the old key) is saved first.")
	fmt.Fprintln(out, "Stop Mediarium before you go on, and start it again afterwards.")
	if !yes {
		fmt.Fprint(out, "Type yes to clear these and carry on: ")
		answer, _ := in.ReadString('\n')
		if strings.ToLower(strings.TrimSpace(answer)) != "yes" {
			return errors.New("cancelled, nothing was changed")
		}
	}

	made, err := backupBeforeReset(context.Background(), db, cfg, now)
	if err != nil {
		return fmt.Errorf("nothing was changed, because the backup failed: %w", err)
	}
	for _, p := range made {
		fmt.Fprintln(out, "Saved a copy: "+p)
	}
	if err := clearSecrets(db, found); err != nil {
		return fmt.Errorf("clear the saved secrets (nothing was changed): %w", err)
	}
	if box == nil {
		if _, err := crypto.CreateKey(cfg.SecretKeyPath); err != nil {
			return fmt.Errorf("the secrets were cleared, but the new key could not be written: %w", err)
		}
		fmt.Fprintln(out, "Made a new secret.key.")
	}
	fmt.Fprintf(out, "Cleared %d saved passwords or keys. Start Mediarium and enter these again in Settings (the Health list on the dashboard shows what is still missing):\n%s\n", len(found), strings.Join(lines, "\n"))
	return nil
}

// runResetKey parses the arguments of `app reset-key`.
func runResetKey(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	yes := false
	for _, a := range args {
		switch a {
		case "--yes", "-y":
			yes = true
		case "-h", "--help", "help":
			fmt.Fprint(stdout, resetKeyUsage)
			return 0
		default:
			fmt.Fprint(stderr, resetKeyUsage)
			return 2
		}
	}
	if err := resetKey(config.Load(), yes, bufio.NewReader(stdin), stdout, time.Now()); err != nil {
		fmt.Fprintf(stderr, "mediarium: %v\n", err)
		return 1
	}
	return 0
}
