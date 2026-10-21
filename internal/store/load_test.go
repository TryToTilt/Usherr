package store_test

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// loadResult is what a run of mixed work against one database came to.
type loadResult struct {
	ops    int64
	errs   int64
	locked int64 // errors that say the database is locked or busy
	first  error
}

// runMixedLoad does, for the given time: transactions that read a row and then
// write it (the shape that fails at once with "database is locked" when a
// transaction starts deferred and another writer committed in between), plain
// writes outside a transaction, and plain reads.
func runMixedLoad(db *sql.DB, d time.Duration) loadResult {
	var res loadResult
	var mu sync.Mutex
	note := func(err error) {
		if err == nil {
			atomic.AddInt64(&res.ops, 1)
			return
		}
		atomic.AddInt64(&res.errs, 1)
		msg := err.Error()
		if strings.Contains(msg, "locked") || strings.Contains(strings.ToLower(msg), "busy") {
			atomic.AddInt64(&res.locked, 1)
		}
		mu.Lock()
		if res.first == nil {
			res.first = err
		}
		mu.Unlock()
	}
	stop := time.Now().Add(d)
	var wg sync.WaitGroup
	run := func(n int, f func(id, i int) error) {
		for g := 0; g < n; g++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				for i := 0; time.Now().Before(stop); i++ {
					note(f(id, i))
				}
			}(g)
		}
	}
	run(8, func(id, i int) error { // read, then write, in one transaction
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		var v string
		key := fmt.Sprintf("rw%d", i%5)
		if err := tx.QueryRow(`SELECT COALESCE((SELECT value FROM settings WHERE key = ?), '')`, key).Scan(&v); err != nil {
			tx.Rollback()
			return err
		}
		if _, err := tx.Exec(`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, fmt.Sprint(id, ":", len(v))); err != nil {
			tx.Rollback()
			return err
		}
		return tx.Commit()
	})
	run(4, func(id, i int) error { // a plain write
		_, err := db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
			fmt.Sprintf("plain%d", id), fmt.Sprint(i))
		return err
	})
	run(4, func(id, i int) error { // a plain read
		var n int
		return db.QueryRow(`SELECT COUNT(*) FROM settings`).Scan(&n)
	})
	wg.Wait()
	return res
}

// Read-then-write transactions, plain writes and reads at the same time must
// never fail with "database is locked": write transactions begin immediately
// (the DSN's _txlock=immediate), so they queue for the lock.
func TestMixedReadWriteLoadNeverSeesDatabaseIsLocked(t *testing.T) {
	db := openTemp(t)
	res := runMixedLoad(db, 1500*time.Millisecond)
	t.Logf("store.Open: %d operations, %d errors (%d locked)", res.ops, res.errs, res.locked)
	if res.errs != 0 {
		t.Fatalf("%d errors under load, the first: %v", res.errs, res.first)
	}
	if res.ops < 100 {
		t.Fatalf("only %d operations ran, the database is far too slow", res.ops)
	}
}

// Not a test of the program: set MEDIARIUM_LOAD_COMPARE=1 to see what the same
// load does when transactions start deferred (the driver's default) instead.
func TestCompareDeferredTransactions(t *testing.T) {
	if os.Getenv("MEDIARIUM_LOAD_COMPARE") == "" {
		t.Skip("set MEDIARIUM_LOAD_COMPARE=1 to compare")
	}
	path := filepath.Join(t.TempDir(), "deferred.db")
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(8)
	if _, err := db.Exec(`CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	res := runMixedLoad(db, 1500*time.Millisecond)
	t.Logf("deferred transactions: %d operations, %d errors (%d locked), first: %v", res.ops, res.errs, res.locked, res.first)
}
