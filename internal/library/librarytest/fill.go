// Package librarytest builds big libraries for tests and benchmarks, quickly:
// rows go straight into the tables in a few large inserts instead of through
// the repository one title at a time.
package librarytest

import (
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Size says how big a library Fill makes.
type Size struct {
	Movies          int
	Shows           int
	EpisodesPerShow int
	// Monitored marks every movie, show and episode monitored. Off, nothing is
	// wanted (what an import leaves before "Start monitoring").
	Monitored bool
	// DownloadedEvery gives every nth movie and episode a file (0: none).
	DownloadedEvery int
	// FileDir is the folder the file paths point into. The files are not made.
	FileDir string
}

// Big is about the size of the biggest libraries we have heard of: thousands
// of movies and a few hundred shows with long runs.
var Big = Size{Movies: 5000, Shows: 500, EpisodesPerShow: 200, Monitored: true, DownloadedEvery: 20, FileDir: "/library"}

// Small is for the tests that run every time.
var Small = Size{Movies: 200, Shows: 20, EpisodesPerShow: 50, Monitored: true, DownloadedEvery: 10, FileDir: "/library"}

// ScaleEnv is the environment variable that switches the slow scale tests on.
const ScaleEnv = "MEDIARIUM_SCALE"

// NeedScale skips t unless ScaleEnv is set: building and timing a big library
// takes too long for every test run.
func NeedScale(t testing.TB) {
	t.Helper()
	if os.Getenv(ScaleEnv) == "" {
		t.Skipf("set %s=1 to run the scale test", ScaleEnv)
	}
}

// BigSize is Big, with the episodes per show taken from ScaleEnv when it is a
// number above 1 (MEDIARIUM_SCALE=780 makes 390,000 episodes).
func BigSize() Size {
	sz := Big
	if n, err := strconv.Atoi(os.Getenv(ScaleEnv)); err == nil && n > 1 {
		sz.EpisodesPerShow = n
	}
	return sz
}

// Plan is SQLite's query plan for query, one step per line.
func Plan(db *sql.DB, query string, args ...any) (string, error) {
	r, err := db.Query("EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		return "", fmt.Errorf("explain: %w", err)
	}
	defer r.Close()
	var lines []string
	for r.Next() {
		var id, parent, unused int
		var detail string
		if err := r.Scan(&id, &parent, &unused, &detail); err != nil {
			return "", fmt.Errorf("read the plan: %w", err)
		}
		lines = append(lines, detail)
	}
	return strings.Join(lines, "\n"), r.Err()
}

// rowsPerInsert is how many rows go into one INSERT statement.
const rowsPerInsert = 200

// Fill adds the movies, shows and episodes of sz to db, which must be
// migrated and have no titles yet.
func Fill(db *sql.DB, sz Size) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin filling: %w", err)
	}
	defer tx.Rollback()
	b := 0 // monitored
	if sz.Monitored {
		b = 1
	}
	has := func(n int) bool { return sz.DownloadedEvery > 0 && n%sz.DownloadedEvery == 0 }
	added := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	var rows [][]any
	for i := 1; i <= sz.Movies; i++ {
		status, path, quality := "missing", any(nil), any(nil)
		if has(i) {
			status, path, quality = "downloaded", fmt.Sprintf("%s/movies/Movie %d (2001)/Movie %d.mkv", sz.FileDir, i, i), "Bluray-1080p"
		}
		rows = append(rows, []any{i, fmt.Sprintf("Movie %d", i), "A film.", "", 1960 + i%60, status, b, path, quality, added.Add(time.Duration(i) * time.Minute).Format(time.RFC3339)})
	}
	if err := insert(tx, "movies", "tmdb_id, title, overview, poster_path, year, status, monitored, file_path, quality, added_at", rows); err != nil {
		return err
	}

	rows = rows[:0]
	for i := 1; i <= sz.Shows; i++ {
		rows = append(rows, []any{i, fmt.Sprintf("Show %d", i), 1990 + i%35, b, added.Add(time.Duration(i) * time.Minute).Format(time.RFC3339)})
	}
	if err := insert(tx, "series", "tmdb_id, title, year, monitored, added_at", rows); err != nil {
		return err
	}
	// The ids are 1..Shows in a fresh database, but ask rather than assume.
	ids, err := seriesIDs(tx)
	if err != nil {
		return err
	}

	// Episode overviews from TMDB are a few hundred characters each, and they
	// make the table big enough that reading every row is slow.
	overview := strings.Repeat("Somebody does something unexpected and the others have to deal with it. ", 6)
	rows = rows[:0]
	first := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	n := 0
	for _, id := range ids {
		for e := 0; e < sz.EpisodesPerShow; e++ {
			n++
			status, path, quality := "missing", any(nil), any(nil)
			if has(n) {
				status, path, quality = "downloaded", fmt.Sprintf("%s/tv/Show %d/S%02dE%02d.mkv", sz.FileDir, id, e/20+1, e%20+1), "WEBDL-1080p"
			}
			air := first.AddDate(0, 0, e*7).Format("2006-01-02")
			rows = append(rows, []any{id, e/20 + 1, e%20 + 1, fmt.Sprintf("Episode %d", e+1), overview, air, status, b, path, quality})
			if len(rows) == rowsPerInsert*10 {
				if err := insert(tx, "episodes", "series_id, season, episode, title, overview, air_date, status, monitored, file_path, quality", rows); err != nil {
					return err
				}
				rows = rows[:0]
			}
		}
	}
	if err := insert(tx, "episodes", "series_id, season, episode, title, overview, air_date, status, monitored, file_path, quality", rows); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit filling: %w", err)
	}
	return nil
}

// ImportedShows records every show in db as added by one finished TV import,
// as if they had all come in through the library import, and returns the
// import's id.
func ImportedShows(db *sql.DB) (int64, error) {
	res, err := db.Exec(`INSERT INTO import_batches (kind, root, finished_at, monitor, monitor_missing, no_upgrade)
		VALUES ('tv', '/tv', strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), 0, 0, 1)`)
	if err != nil {
		return 0, fmt.Errorf("add the import: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("read the import id: %w", err)
	}
	if _, err := db.Exec(`INSERT INTO import_items (batch_id, kind, item_id, tmdb_id, title, outcome, state, created)
		SELECT ?, 'series', id, tmdb_id, title, 'added', 'done', 1 FROM series`, id); err != nil {
		return 0, fmt.Errorf("add the imported shows: %w", err)
	}
	return id, nil
}

func seriesIDs(tx *sql.Tx) ([]int64, error) {
	r, err := tx.Query(`SELECT id FROM series ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list the shows: %w", err)
	}
	defer r.Close()
	var ids []int64
	for r.Next() {
		var id int64
		if err := r.Scan(&id); err != nil {
			return nil, fmt.Errorf("read a show id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, r.Err()
}

// insert writes rows into table, rowsPerInsert at a time.
func insert(tx *sql.Tx, table, columns string, rows [][]any) error {
	if len(rows) == 0 {
		return nil
	}
	width := len(rows[0])
	one := "(" + strings.TrimSuffix(strings.Repeat("?,", width), ",") + ")"
	for start := 0; start < len(rows); start += rowsPerInsert {
		end := min(start+rowsPerInsert, len(rows))
		args := make([]any, 0, (end-start)*width)
		for _, r := range rows[start:end] {
			args = append(args, r...)
		}
		q := "INSERT INTO " + table + " (" + columns + ") VALUES " + strings.TrimSuffix(strings.Repeat(one+",", end-start), ",")
		if _, err := tx.Exec(q, args...); err != nil {
			return fmt.Errorf("fill %s: %w", table, err)
		}
	}
	return nil
}
