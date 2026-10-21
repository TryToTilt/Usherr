package api

import (
	"context"
	"testing"
	"time"
)

// The showtime clock: the pre-show starts when its minutes are left, the
// film's moment marks it playing, and long after it's done.
func TestShowtimeJob(t *testing.T) {
	s := newAuditServer(t)
	ctx := context.Background()
	add := func(at time.Time, pre int) int64 {
		res, err := s.db.ExecContext(ctx, `INSERT INTO showtimes (user_id, tmdb_id, title, runtime, at_ms, preshow_min, play_on) VALUES (1, 1, 'Film', 100, ?, ?, 'none')`, at.UnixMilli(), pre)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		return id
	}
	state := func(id int64) string {
		var st string
		_ = s.db.QueryRowContext(ctx, `SELECT state FROM showtimes WHERE id = ?`, id).Scan(&st)
		return st
	}
	soon := add(time.Now().Add(5*time.Minute), 10)   // pre-show time
	later := add(time.Now().Add(30*time.Minute), 10) // not yet
	now := add(time.Now().Add(-30*time.Second), 0)   // starting
	old := add(time.Now().Add(-5*time.Hour), 10)     // long over
	s.showtimeJob(ctx)
	for id, want := range map[int64]string{soon: "preshow", later: "booked", now: "playing", old: "done"} {
		if got := state(id); got != want {
			t.Errorf("showtime %d: %s, want %s", id, got, want)
		}
	}
}
