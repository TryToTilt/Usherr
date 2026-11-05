package servicecheck

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// How long the history is kept.
const (
	KeepResults = 8 * 24 * time.Hour  // a week of uptime, and a day to spare
	KeepChanges = 90 * 24 * time.Hour // the list of outages
)

// ErrNotFound is returned for a check that does not exist.
var ErrNotFound = errors.New("service check not found")

// Repo keeps the checks, their results and their changes (migration 9065).
type Repo struct{ db *sql.DB }

func NewRepo(db *sql.DB) *Repo { return &Repo{db: db} }

const checkColumns = `id, name, url, expect, keyword, interval_sec, fail_after, enabled, created_ms,
	state, fails, fail_since_ms, alerted, changed_ms, checked_ms, last_ms, last_code, last_error`

func msTime(ms int64) time.Time {
	if ms == 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}

func timeMs(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

type scanner interface{ Scan(...any) error }

func scanCheck(sc scanner) (Check, error) {
	var c Check
	var intervalSec, created, failSince, changed, checked int64
	err := sc.Scan(&c.ID, &c.Name, &c.URL, &c.Expect, &c.Keyword, &intervalSec, &c.FailAfter, &c.Enabled, &created,
		&c.State, &c.Fails, &failSince, &c.Alerted, &changed, &checked, &c.LastMs, &c.LastCode, &c.LastError)
	c.Interval = time.Duration(intervalSec) * time.Second
	c.Created, c.FailSince, c.Changed, c.Checked = msTime(created), msTime(failSince), msTime(changed), msTime(checked)
	return c, err
}

// List returns every check in the order they were added.
func (r *Repo) List(ctx context.Context) ([]Check, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+checkColumns+` FROM service_checks ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list service checks: %w", err)
	}
	defer rows.Close()
	out := []Check{}
	for rows.Next() {
		c, err := scanCheck(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Get returns one check.
func (r *Repo) Get(ctx context.Context, id int64) (Check, error) {
	c, err := scanCheck(r.db.QueryRowContext(ctx, `SELECT `+checkColumns+` FROM service_checks WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

// Count is how many checks there are.
func (r *Repo) Count(ctx context.Context) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM service_checks`).Scan(&n)
	return n, err
}

// Create adds a check (already Normalized). It starts as unknown.
func (r *Repo) Create(ctx context.Context, c Check, now time.Time) (Check, error) {
	res, err := r.db.ExecContext(ctx, `INSERT INTO service_checks (name, url, expect, keyword, interval_sec, fail_after, enabled, created_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, c.Name, c.URL, c.Expect, c.Keyword, int64(c.Interval/time.Second), c.FailAfter, c.Enabled, now.UnixMilli())
	if err != nil {
		return c, fmt.Errorf("add service check: %w", err)
	}
	id, _ := res.LastInsertId()
	return r.Get(ctx, id)
}

// Update saves the settings of a check (already Normalized). A new address
// starts the state over: what was known was about the old one.
func (r *Repo) Update(ctx context.Context, c Check) (Check, error) {
	old, err := r.Get(ctx, c.ID)
	if err != nil {
		return c, err
	}
	if _, err := r.db.ExecContext(ctx, `UPDATE service_checks SET name = ?, url = ?, expect = ?, keyword = ?, interval_sec = ?, fail_after = ?, enabled = ? WHERE id = ?`,
		c.Name, c.URL, c.Expect, c.Keyword, int64(c.Interval/time.Second), c.FailAfter, c.Enabled, c.ID); err != nil {
		return c, fmt.Errorf("save service check: %w", err)
	}
	if old.URL != c.URL || (old.Enabled && !c.Enabled) {
		// Paused or pointed elsewhere: no "back" message is owed any more.
		if _, err := r.db.ExecContext(ctx, `UPDATE service_checks SET state = 'unknown', fails = 0, fail_since_ms = 0, alerted = 0, changed_ms = 0 WHERE id = ?`, c.ID); err != nil {
			return c, err
		}
	}
	if old.URL != c.URL {
		_, _ = r.db.ExecContext(ctx, `DELETE FROM service_check_results WHERE check_id = ?`, c.ID)
	}
	return r.Get(ctx, c.ID)
}

// Delete removes a check and its history.
func (r *Repo) Delete(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM service_checks WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete service check: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	_, _ = r.db.ExecContext(ctx, `DELETE FROM service_check_results WHERE check_id = ?`, id)
	_, _ = r.db.ExecContext(ctx, `DELETE FROM service_check_changes WHERE check_id = ?`, id)
	return nil
}

// Record saves one result: the check's new state, the result itself and,
// when the state changed, the change.
func (r *Repo) Record(ctx context.Context, c Check, res Result, change *Change) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	upd, err := tx.ExecContext(ctx, `UPDATE service_checks SET state = ?, fails = ?, fail_since_ms = ?, alerted = ?, changed_ms = ?, checked_ms = ?, last_ms = ?, last_code = ?, last_error = ? WHERE id = ?`,
		c.State, c.Fails, timeMs(c.FailSince), c.Alerted, timeMs(c.Changed), timeMs(c.Checked), c.LastMs, c.LastCode, c.LastError, c.ID)
	if err != nil {
		return fmt.Errorf("save service check state: %w", err)
	}
	if n, _ := upd.RowsAffected(); n == 0 {
		return ErrNotFound // removed while it was being checked
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO service_check_results (check_id, at_ms, ok, ms, code, error) VALUES (?, ?, ?, ?, ?, ?)`,
		c.ID, res.At.UnixMilli(), res.OK, res.Ms, res.Code, res.Error); err != nil {
		return fmt.Errorf("save service check result: %w", err)
	}
	if change != nil {
		if _, err := tx.ExecContext(ctx, `INSERT INTO service_check_changes (check_id, at_ms, state, detail) VALUES (?, ?, ?, ?)`,
			c.ID, change.At.UnixMilli(), change.State, change.Detail); err != nil {
			return fmt.Errorf("save service check change: %w", err)
		}
	}
	return tx.Commit()
}

// Recent returns the newest results of a check, newest last.
func (r *Repo) Recent(ctx context.Context, id int64, limit int) ([]Result, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT at_ms, ok, ms, code, error FROM
		(SELECT at_ms, ok, ms, code, error FROM service_check_results WHERE check_id = ? ORDER BY at_ms DESC LIMIT ?) ORDER BY at_ms`, id, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Result{}
	for rows.Next() {
		var x Result
		var at int64
		if err := rows.Scan(&at, &x.OK, &x.Ms, &x.Code, &x.Error); err != nil {
			return nil, err
		}
		x.At = time.UnixMilli(at)
		out = append(out, x)
	}
	return out, rows.Err()
}

// Changes returns the newest changes of a check, newest first.
func (r *Repo) Changes(ctx context.Context, id int64, limit int) ([]Change, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT at_ms, state, detail FROM service_check_changes WHERE check_id = ? ORDER BY at_ms DESC LIMIT ?`, id, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Change{}
	for rows.Next() {
		var x Change
		var at int64
		if err := rows.Scan(&at, &x.State, &x.Detail); err != nil {
			return nil, err
		}
		x.At = time.UnixMilli(at)
		out = append(out, x)
	}
	return out, rows.Err()
}

// Uptime is how a check did since a moment.
type Uptime struct {
	Checks  int     // how many checks there were
	Percent float64 // the share that worked, 0-100; meaningless when Checks is 0
	AvgMs   int     // the average time of the answers that worked
}

// UptimeSince sums up the results of a check since a moment.
func (r *Repo) UptimeSince(ctx context.Context, id int64, since time.Time) (Uptime, error) {
	var u Uptime
	var good int
	var avg sql.NullFloat64
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(ok), 0), AVG(CASE WHEN ok = 1 THEN ms END) FROM service_check_results WHERE check_id = ? AND at_ms >= ?`,
		id, since.UnixMilli()).Scan(&u.Checks, &good, &avg)
	if err != nil {
		return u, err
	}
	if u.Checks > 0 {
		u.Percent = float64(good) * 100 / float64(u.Checks)
	}
	if avg.Valid {
		u.AvgMs = int(avg.Float64 + 0.5)
	}
	return u, nil
}

// Prune forgets results and changes older than they are kept.
func (r *Repo) Prune(ctx context.Context, now time.Time) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM service_check_results WHERE at_ms < ?`, now.Add(-KeepResults).UnixMilli()); err != nil {
		return err
	}
	_, err := r.db.ExecContext(ctx, `DELETE FROM service_check_changes WHERE at_ms < ?`, now.Add(-KeepChanges).UnixMilli())
	return err
}
