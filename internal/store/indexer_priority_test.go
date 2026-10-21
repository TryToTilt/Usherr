package store_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/store"
)

// The three old indexer levels become numbers, and the old column is left
// as it was so an older build still reads its own values.
func TestIndexerPriorityMigration(t *testing.T) {
	before := 0
	for i, n := range migrationNames(t) {
		if strings.HasPrefix(n, "0212_") {
			before = i
		}
	}
	if before == 0 {
		t.Fatal("migration 0212 not found")
	}
	cases := []struct {
		name     string
		old      int
		wantRank int
	}{
		{"preferred", 1, 10},
		{"normal", 2, 25},
		{"last resort", 3, 40},
		{"anything else counts as normal", 0, 25},
	}
	path := oldInstall(t, before, func(db *sql.DB) {
		for _, tc := range cases {
			mustExec(t, db, `INSERT INTO indexers (name, definition_id, base_url, protocol, priority) VALUES (?, 'newznab', 'https://a', 'usenet', ?)`, tc.name, tc.old)
		}
	})
	db, err := store.Open(path)
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	defer db.Close()
	for _, tc := range cases {
		var rank, old int
		if err := db.QueryRow(`SELECT priority_rank, priority FROM indexers WHERE name = ?`, tc.name).Scan(&rank, &old); err != nil {
			t.Fatal(err)
		}
		if rank != tc.wantRank || old != tc.old {
			t.Errorf("%s: rank %d (want %d), old column %d (want %d)", tc.name, rank, tc.wantRank, old, tc.old)
		}
	}
}
