package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/rdborg/mediarium/internal/problems"
	"modernc.org/sqlite"
)

// driverName is the SQLite driver with call timing added. It behaves exactly
// like the plain "sqlite" driver; it only measures how long each statement
// takes, so a statement stuck behind a lock is reported with its SQL instead
// of just making the page hang.
const driverName = "mediarium-sqlite"

func init() {
	sql.Register(driverName, &watchedDriver{inner: &sqlite.Driver{}})
}

// slowCall is how long a statement may take before it is logged.
var slowCall atomic.Int64

// SlowCallThreshold is what a statement or a wait for a free connection may
// take before it is logged.
const SlowCallThreshold = 3 * time.Second

func init() { slowCall.Store(int64(SlowCallThreshold)) }

// SetSlowCallThreshold changes the logging threshold (tests use a short one).
func SetSlowCallThreshold(d time.Duration) { slowCall.Store(int64(d)) }

// slowNotifier is told about every slow statement (see OnSlowCall).
var slowNotifier atomic.Pointer[func(sql string, took time.Duration)]

// OnSlowCall registers fn to hear about every statement that exceeded the
// threshold, in addition to the log line. Used by tests; pass nil to remove.
func OnSlowCall(fn func(sql string, took time.Duration)) {
	if fn == nil {
		slowNotifier.Store(nil)
		return
	}
	slowNotifier.Store(&fn)
}

// sqlName shortens a statement to something safe and short to log: the first
// words of the SQL, never any argument values.
func sqlName(q string) string {
	q = strings.Join(strings.Fields(q), " ")
	if len(q) > 90 {
		q = q[:90] + "..."
	}
	return q
}

func noteCall(query string, started time.Time) {
	took := time.Since(started)
	if took < time.Duration(slowCall.Load()) {
		return
	}
	log.Printf("database: slow call took=%s sql=%q", took.Round(time.Millisecond), sqlName(query))
	// The problem log's own statements are left out, or a slow database would
	// fill the log with the log's own trouble.
	if !problems.Saving() && !strings.Contains(query, "problems") {
		problems.Record(problems.Problem{
			Code:    problems.CodeDatabaseSlow,
			Message: fmt.Sprintf("A database call took %s.", took.Round(100*time.Millisecond)),
			Detail:  fmt.Sprintf("took %s: %s", took.Round(time.Millisecond), sqlName(query)),
		})
	}
	if fn := slowNotifier.Load(); fn != nil {
		(*fn)(sqlName(query), took)
	}
}

// libraryVersion goes up with every write to the movie and TV tables
// (movies, series, episodes, and the other names of titles), so a count worked out from them can be kept
// until it changes. See LibraryVersion.
var libraryVersion atomic.Uint64

// LibraryVersion is a number that changes whenever a movie, show or episode
// is added, changed or removed (and again when the transaction that did it
// ends). Callers keep an answer together with the number it was made at, and
// work it out again once the number has moved.
func LibraryVersion() uint64 { return libraryVersion.Load() }

// libraryWrite matches a statement that writes to one of the library tables.
var libraryWrite = regexp.MustCompile(`(?is)^\s*(insert|update|delete|replace)\b.*\b(movies|series|episodes|title_names)\b`)

// noteWrite moves the library version on for a statement that changes the
// library, and marks the connection so the end of its transaction does too.
func (c *watchedConn) noteWrite(query string) {
	if libraryWrite.MatchString(query) {
		libraryVersion.Add(1)
		c.touched = true
	}
}

type watchedDriver struct{ inner driver.Driver }

func (d *watchedDriver) Open(name string) (driver.Conn, error) {
	c, err := d.inner.Open(name)
	if err != nil {
		return nil, err
	}
	return &watchedConn{inner: c}, nil
}

type watchedConn struct {
	inner driver.Conn
	// touched: the open transaction wrote to the library tables.
	touched bool
}

func (c *watchedConn) Prepare(query string) (driver.Stmt, error) {
	s, err := c.inner.Prepare(query)
	if err != nil {
		return nil, err
	}
	return &watchedStmt{inner: s, query: query, conn: c}, nil
}

func (c *watchedConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	if p, ok := c.inner.(driver.ConnPrepareContext); ok {
		s, err := p.PrepareContext(ctx, query)
		if err != nil {
			return nil, err
		}
		return &watchedStmt{inner: s, query: query, conn: c}, nil
	}
	return c.Prepare(query)
}

func (c *watchedConn) Close() error { return c.inner.Close() }

func (c *watchedConn) Begin() (driver.Tx, error) { //nolint:staticcheck
	tx, err := c.inner.Begin() //nolint:staticcheck
	if err != nil {
		return nil, err
	}
	return &watchedTx{inner: tx, conn: c}, nil
}

func (c *watchedConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	defer noteCall("BEGIN", time.Now())
	var tx driver.Tx
	var err error
	if b, ok := c.inner.(driver.ConnBeginTx); ok {
		tx, err = b.BeginTx(ctx, opts)
	} else {
		tx, err = c.inner.Begin() //nolint:staticcheck
	}
	if err != nil {
		return nil, err
	}
	return &watchedTx{inner: tx, conn: c}, nil
}

// watchedTx moves the library version on once more when a transaction that
// changed the library ends: a count made while it was still open could not
// see its changes yet.
type watchedTx struct {
	inner driver.Tx
	conn  *watchedConn
}

func (t *watchedTx) Commit() error {
	defer t.done()
	return t.inner.Commit()
}

func (t *watchedTx) Rollback() error {
	defer t.done()
	return t.inner.Rollback()
}

func (t *watchedTx) done() {
	if t.conn.touched {
		t.conn.touched = false
		libraryVersion.Add(1)
	}
}

func (c *watchedConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	e, ok := c.inner.(driver.ExecerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	defer noteCall(query, time.Now())
	defer c.noteWrite(query)
	return e.ExecContext(ctx, query, args)
}

func (c *watchedConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	q, ok := c.inner.(driver.QueryerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	defer noteCall(query, time.Now())
	return q.QueryContext(ctx, query, args)
}

func (c *watchedConn) Ping(ctx context.Context) error {
	if p, ok := c.inner.(driver.Pinger); ok {
		return p.Ping(ctx)
	}
	return nil
}

func (c *watchedConn) ResetSession(ctx context.Context) error {
	if r, ok := c.inner.(driver.SessionResetter); ok {
		return r.ResetSession(ctx)
	}
	return nil
}

func (c *watchedConn) IsValid() bool {
	if v, ok := c.inner.(driver.Validator); ok {
		return v.IsValid()
	}
	return true
}

type watchedStmt struct {
	inner driver.Stmt
	query string
	conn  *watchedConn
}

func (s *watchedStmt) Close() error  { return s.inner.Close() }
func (s *watchedStmt) NumInput() int { return s.inner.NumInput() }

func (s *watchedStmt) Exec(args []driver.Value) (driver.Result, error) {
	defer noteCall(s.query, time.Now())
	defer s.conn.noteWrite(s.query)
	return s.inner.Exec(args) //nolint:staticcheck
}

func (s *watchedStmt) Query(args []driver.Value) (driver.Rows, error) {
	defer noteCall(s.query, time.Now())
	return s.inner.Query(args) //nolint:staticcheck
}

func (s *watchedStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	defer noteCall(s.query, time.Now())
	defer s.conn.noteWrite(s.query)
	if e, ok := s.inner.(driver.StmtExecContext); ok {
		return e.ExecContext(ctx, args)
	}
	return s.inner.Exec(namedToValues(args)) //nolint:staticcheck
}

func (s *watchedStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	defer noteCall(s.query, time.Now())
	if q, ok := s.inner.(driver.StmtQueryContext); ok {
		return q.QueryContext(ctx, args)
	}
	return s.inner.Query(namedToValues(args)) //nolint:staticcheck
}

func namedToValues(args []driver.NamedValue) []driver.Value {
	out := make([]driver.Value, len(args))
	for i, a := range args {
		out[i] = a.Value
	}
	return out
}

// watchPool logs when callers have been waiting a long time for a free
// connection (every connection busy), which is how a stuck query shows up
// for everyone else. It stops when the database is closed.
func watchPool(db *sql.DB) {
	const every = time.Second
	tick := time.NewTicker(every)
	defer tick.Stop()
	var (
		lastWait  time.Duration
		lastCount int64
		lastLog   time.Time
		blocked   time.Duration // how long in a row a probe could not get a connection
	)
	for range tick.C {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		err := db.PingContext(ctx)
		cancel()
		if err != nil && strings.Contains(err.Error(), "database is closed") {
			return
		}
		if err != nil {
			blocked += every
		} else {
			blocked = 0
		}
		st := db.Stats()
		delta := st.WaitDuration - lastWait
		count := st.WaitCount - lastCount
		lastWait, lastCount = st.WaitDuration, st.WaitCount
		threshold := time.Duration(slowCall.Load())
		if (delta >= threshold || blocked >= threshold) && time.Since(lastLog) >= 10*time.Second {
			lastLog = time.Now()
			log.Printf("database: callers are waiting for a free connection in_use=%d max=%d waited=%s waits=%d blocked_for=%s",
				st.InUse, st.MaxOpenConnections, delta.Round(time.Millisecond), count, blocked)
		}
	}
}
