package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/mediaservers"
)

func driveClock(t *testing.T, at *time.Time) {
	t.Helper()
	old := driveNow
	driveNow = func() time.Time { return *at }
	t.Cleanup(func() { driveNow = old })
}

func chicago(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestDriveWindowContains(t *testing.T) {
	at := func(h int) time.Time { return time.Date(2026, 11, 6, h, 30, 0, 0, time.UTC) }
	for _, c := range []struct {
		w     driveWindow
		hours int
		in    []int
		out   []int
	}{
		{driveWindow{2, 9}, 7, []int{2, 8}, []int{1, 9, 15}},
		{driveWindow{22, 6}, 8, []int{22, 23, 0, 5}, []int{6, 12, 21}},
		{driveWindow{5, 5}, 24, []int{0, 5, 23}, nil},
	} {
		if c.w.hours() != c.hours {
			t.Errorf("%+v: %d hours", c.w, c.w.hours())
		}
		for _, h := range c.in {
			if !c.w.contains(at(h)) {
				t.Errorf("%+v should hold %d:30", c.w, h)
			}
		}
		for _, h := range c.out {
			if c.w.contains(at(h)) {
				t.Errorf("%+v shouldn't hold %d:30", c.w, h)
			}
		}
	}
}

// A window past midnight is one night: its budget isn't handed out twice.
func TestDriveWindowPastMidnightIsOneBudgetDay(t *testing.T) {
	w := driveWindow{22, 6}
	evening := time.Date(2026, 11, 6, 23, 0, 0, 0, time.UTC)
	morning := time.Date(2026, 11, 7, 3, 0, 0, 0, time.UTC)
	if w.budgetDay(evening) != w.budgetDay(morning) {
		t.Errorf("one night counts as two days: %s, %s", w.budgetDay(evening), w.budgetDay(morning))
	}
	if got := w.end(evening); !got.Equal(time.Date(2026, 11, 7, 6, 0, 0, 0, time.UTC)) {
		t.Errorf("end from the evening: %v", got)
	}
	if got := w.end(morning); !got.Equal(time.Date(2026, 11, 7, 6, 0, 0, 0, time.UTC)) {
		t.Errorf("end from the morning: %v", got)
	}
	plain := driveWindow{2, 9}
	if plain.budgetDay(morning) != "2026-11-07" {
		t.Errorf("a plain window goes by the calendar: %s", plain.budgetDay(morning))
	}
}

// walkNight runs the job's decision once a tick over a whole day, spending a
// search each time it's due. It answers when each search started.
func walkNight(t *testing.T, s *Server, at *time.Time, start time.Time) []time.Time {
	t.Helper()
	var searches []time.Time
	for *at = start; at.Before(start.Add(24 * time.Hour)); *at = at.Add(driveTick) {
		if !s.driveDue(*at) {
			continue
		}
		if !s.driveSpend() {
			t.Fatalf("due at %v with no budget left", *at)
		}
		_ = s.Settings.Set(keyDriveLastAt, at.UTC().Format(time.RFC3339), false)
		searches = append(searches, *at)
	}
	return searches
}

// The household's night: 60 searches fit in the 7 hours from 2 to 9, evenly,
// with none outside them. At the old one per 10 minutes only 42 would.
func TestDriveBudgetIsReachedInsideTheWindow(t *testing.T) {
	s, db := optionsServer(t)
	s.db = db
	_ = s.Settings.Set(keyDriveBudget, "60", false)
	var at time.Time
	driveClock(t, &at)
	loc := chicago(t)
	searches := walkNight(t, s, &at, time.Date(2026, 11, 6, 0, 0, 0, 0, loc))
	if len(searches) != 60 {
		t.Fatalf("%d searches, want 60", len(searches))
	}
	for i, when := range searches {
		if h := when.Hour(); h < 2 || h >= 9 {
			t.Errorf("a search at %v, outside 2 to 9", when)
		}
		if i > 0 && when.Sub(searches[i-1]) < 5*time.Minute {
			t.Errorf("a burst: %v after the one before", when.Sub(searches[i-1]))
		}
	}
	if first := searches[0]; first.Hour() != 2 || first.Minute() != 0 {
		t.Errorf("the night starts at %v", first)
	}
}

func TestDriveHugeBudgetNeverBursts(t *testing.T) {
	s, db := optionsServer(t)
	s.db = db
	_ = s.Settings.Set(keyDriveBudget, "500", false)
	var at time.Time
	driveClock(t, &at)
	searches := walkNight(t, s, &at, time.Date(2026, 11, 6, 0, 0, 0, 0, chicago(t)))
	if len(searches) > (driveWindow{2, 9}).fits() {
		t.Errorf("%d searches", len(searches))
	}
	for i := 1; i < len(searches); i++ {
		if searches[i].Sub(searches[i-1]) < driveMinGap {
			t.Fatalf("closer than a minute at %v", searches[i])
		}
	}
}

// The night the clocks go back: 2 to 9 is still one window and the budget is
// still reached.
func TestDriveWindowOverAClockChange(t *testing.T) {
	s, db := optionsServer(t)
	s.db = db
	_ = s.Settings.Set(keyDriveBudget, "60", false)
	_ = s.Settings.Set(keyDriveFrom, "0", false)
	_ = s.Settings.Set(keyDriveTo, "9", false)
	var at time.Time
	driveClock(t, &at)
	searches := walkNight(t, s, &at, time.Date(2026, 11, 1, 0, 0, 0, 0, chicago(t)))
	if len(searches) != 60 {
		t.Fatalf("%d searches, want 60", len(searches))
	}
}

// Searches a person started use up the same budget: the rest of the night
// spreads what's left.
func TestDrivePaceSpreadsWhatIsLeft(t *testing.T) {
	if got := drivePace(60, 7*time.Hour); got < 6*time.Minute || got > 7*time.Minute {
		t.Errorf("60 in 7 hours: %v", got)
	}
	if got := drivePace(10, 7*time.Hour); got < 41*time.Minute || got > 42*time.Minute {
		t.Errorf("10 in 7 hours: %v", got)
	}
	if got := drivePace(500, time.Hour); got != driveMinGap {
		t.Errorf("never closer than the shortest gap: %v", got)
	}
}

// The job stays still outside the hours: file 11 has no release and is next in
// line, and with no Usenet server in this test, going any further would fail
// loudly. Inside them, a search is due.
func TestDriveJobKeepsToTheWindow(t *testing.T) {
	s, _, _ := driveTestServer(t)
	var at time.Time
	driveClock(t, &at)
	loc := chicago(t)
	at = time.Date(2026, 11, 6, 15, 0, 0, 0, loc)
	s.driveJob(context.Background())
	if v, _ := s.Settings.Get(keyDriveLastAt); v != "" || s.driveUsedToday() != 0 {
		t.Errorf("the job acted at 3 PM: %q", v)
	}
	if !s.driveDue(time.Date(2026, 11, 6, 3, 0, 0, 0, loc)) {
		t.Errorf("no search is due at 3 AM")
	}
}

func TestDriveWindowSetting(t *testing.T) {
	s, db := optionsServer(t)
	s.db = db
	box, err := crypto.LoadOrCreateKey(filepath.Join(t.TempDir(), "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	s.MediaServers = mediaservers.NewRepo(db, box)
	if w := s.driveWindowNow(); w != (driveWindow{2, 9}) {
		t.Errorf("a fresh install gets 2 to 9, got %+v", w)
	}
	put := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.handlePutDrive(rec, httptest.NewRequest(http.MethodPut, "/api/home/drive", strings.NewReader(body)))
		return rec
	}
	if rec := put(`{"windowFrom":1,"windowTo":6}`); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"windowFrom":1`) {
		t.Errorf("saving: %d %s", rec.Code, rec.Body)
	}
	if w := s.driveWindowNow(); w != (driveWindow{1, 6}) {
		t.Errorf("saved: %+v", w)
	}
	if rec := put(`{"windowTo":24}`); rec.Code != http.StatusBadRequest {
		t.Errorf("an hour of 24: %d", rec.Code)
	}
}

func TestDriveTimeZoneNamesUTC(t *testing.T) {
	t.Setenv("TZ", "America/Chicago")
	if name, utc := driveTimeZone(time.Date(2026, 11, 6, 3, 0, 0, 0, chicago(t))); name != "America/Chicago" || utc {
		t.Errorf("%s %v", name, utc)
	}
	t.Setenv("TZ", "Etc/UTC")
	if _, utc := driveTimeZone(time.Date(2026, 11, 6, 3, 0, 0, 0, time.UTC)); !utc {
		t.Errorf("UTC isn't noticed")
	}
}
