package api

// Usherr: Usherr's streaming engine learns which releases play. Each release
// it opens counts for its release group and its indexer; one that won't open,
// breaks part-way or is passed over with "Try another release" counts
// against them. When a group or an indexer has failed at least three times
// and mostly fails, its releases go to the back of the list. Everything else
// keeps its order (the quality profile's), nothing extra is asked of the
// indexers, and a release picked by hand is never moved.

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/parser"
)

const (
	reliabilityMinBad = 3    // failures before a group or indexer can be moved back
	reliabilityBad    = 0.35 // and its share of successes (smoothed) below this
)

// reliabilityKeys are a release's group and indexer keys.
func reliabilityKeys(title, indexer string) []string {
	var keys []string
	if g := strings.ToLower(strings.TrimSpace(parser.Parse(title).Group)); g != "" {
		keys = append(keys, "g:"+g)
	}
	if i := strings.ToLower(strings.TrimSpace(indexer)); i != "" {
		keys = append(keys, "i:"+i)
	}
	return keys
}

// noteStream counts a release's outcome.
func (s *Server) noteStream(title, indexer string, ok bool) {
	col := "bad"
	if ok {
		col = "ok"
	}
	now := time.Now().UnixMilli()
	for _, k := range reliabilityKeys(title, indexer) {
		_, _ = s.db.ExecContext(context.Background(), `INSERT INTO stream_reliability (key, `+col+`, updated_ms) VALUES (?, 1, ?)
			ON CONFLICT(key) DO UPDATE SET `+col+` = `+col+` + 1, updated_ms = excluded.updated_ms`, k, now)
	}
}

// unreliable is whether a release's group or indexer keeps failing.
func unreliable(stats map[string][2]int, title, indexer string) bool {
	for _, k := range reliabilityKeys(title, indexer) {
		st, ok := stats[k]
		if !ok {
			continue
		}
		good, bad := st[0], st[1]
		if bad >= reliabilityMinBad && float64(good+1)/float64(good+bad+2) < reliabilityBad {
			return true
		}
	}
	return false
}

// reliableFirst moves the releases of groups and indexers that keep failing
// to the back, keeping the order otherwise.
func (s *Server) reliableFirst(ctx context.Context, list []indexers.Result) []indexers.Result {
	if len(list) < 2 {
		return list
	}
	rows, err := s.db.QueryContext(ctx, `SELECT key, ok, bad FROM stream_reliability WHERE bad >= ?`, reliabilityMinBad)
	if err != nil {
		return list
	}
	stats := map[string][2]int{}
	for rows.Next() {
		var k string
		var good, bad int
		if rows.Scan(&k, &good, &bad) == nil {
			stats[k] = [2]int{good, bad}
		}
	}
	rows.Close()
	if len(stats) == 0 {
		return list
	}
	out := append([]indexers.Result(nil), list...)
	sort.SliceStable(out, func(i, j int) bool {
		return !unreliable(stats, out[i].Title, out[i].IndexerName) && unreliable(stats, out[j].Title, out[j].IndexerName)
	})
	return out
}
