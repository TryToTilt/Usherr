package api

// Usherr: when the drive's own search may run. Heavy background work keeps to
// the night (2 to 9 by default), because during the day streams need the
// Usenet connections. The window is on the settings card and is read in the
// container's local time: Go takes it from the TZ variable, so a compose file
// that leaves TZ at Etc/UTC gets a window in UTC. The card shows Usherr's own
// clock and warns about that, so it can't go unnoticed.
//
// What a person starts ("Add as stream", "Put it on the virtual drive now",
// "Get one ready now") isn't held back: they asked for it now. It still counts
// against the day's searches.

import (
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	keyDriveFrom   = "home.drive.from"    // the hour the drive's search may start (local time)
	keyDriveTo     = "home.drive.to"      // the hour it stops; the same as from: all day
	keyDriveLastAt = "home.drive.last_at" // when the drive's search last looked for a title

	driveFromDefault = 2
	driveToDefault   = 9
	// driveTick is how often the job asks whether a search is due; driveMinGap
	// is the shortest time ever left between two searches, whatever the budget.
	driveTick   = time.Minute
	driveMinGap = time.Minute
)

// driveNow is the clock the drive's search goes by (a test sets it).
var driveNow = time.Now

// driveWindow is the hours the search may run, past midnight too.
type driveWindow struct{ From, To int }

func (s *Server) driveWindowNow() driveWindow {
	hour := func(key string, def int) int {
		v, _ := s.Settings.Get(key)
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n >= 0 && n <= 23 {
			return n
		}
		return def
	}
	return driveWindow{From: hour(keyDriveFrom, driveFromDefault), To: hour(keyDriveTo, driveToDefault)}
}

func (w driveWindow) allDay() bool { return w.From == w.To }
func (w driveWindow) wraps() bool  { return w.From > w.To }

// contains says whether t is inside the window.
func (w driveWindow) contains(t time.Time) bool {
	h := t.Hour()
	switch {
	case w.allDay():
		return true
	case w.wraps():
		return h >= w.From || h < w.To
	}
	return h >= w.From && h < w.To
}

// hours is how long the window is.
func (w driveWindow) hours() int {
	switch {
	case w.allDay():
		return 24
	case w.wraps():
		return 24 - w.From + w.To
	}
	return w.To - w.From
}

// end is when the window around t closes. An all-day window closes at the next
// midnight, when the day's searches start over.
func (w driveWindow) end(t time.Time) time.Time {
	y, m, d := t.Date()
	switch {
	case w.allDay():
		return time.Date(y, m, d+1, 0, 0, 0, 0, t.Location())
	case w.wraps() && t.Hour() >= w.From:
		return time.Date(y, m, d+1, w.To, 0, 0, 0, t.Location())
	}
	return time.Date(y, m, d, w.To, 0, 0, 0, t.Location())
}

// budgetDay is the day t's searches count against. A window past midnight is
// one night: its hours after midnight still count against the evening it
// started, or it could spend the budget twice in a night.
func (w driveWindow) budgetDay(t time.Time) string {
	if w.wraps() && t.Hour() < w.To {
		t = t.AddDate(0, 0, -1)
	}
	return t.Format("2006-01-02")
}

// fits is how many searches the window holds at the shortest gap.
func (w driveWindow) fits() int { return w.hours() * int(time.Hour/driveMinGap) }

// drivePace is the gap until the next search: what's left of the budget spread
// over what's left of the window, so the budget is reached however the night
// goes (a restart, slow searches, searches a person started), and never
// tighter than driveMinGap, so there's no burst. A tick is taken off so the
// last search of the night falls inside the window.
func drivePace(left int, remaining time.Duration) time.Duration {
	if left <= 0 {
		return remaining
	}
	return max((remaining-driveTick)/time.Duration(left), driveMinGap)
}

// driveDue says whether a search should start at now.
func (s *Server) driveDue(now time.Time) bool {
	w := s.driveWindowNow()
	if !w.contains(now) {
		return false
	}
	left := s.driveBudget() - s.driveUsedToday()
	if left <= 0 {
		return false
	}
	v, _ := s.Settings.Get(keyDriveLastAt)
	last, err := time.Parse(time.RFC3339, v)
	return err != nil || now.Sub(last) >= drivePace(left, w.end(now).Sub(now))
}

// driveTimeZone names the zone Usherr's clock is on: the TZ variable when it
// names one, else the short name, and says whether that's UTC, which for most
// homes means TZ was never set.
func driveTimeZone(now time.Time) (name string, utc bool) {
	short, offset := now.Zone()
	name = strings.TrimSpace(os.Getenv("TZ"))
	if name == "" {
		name = short
	}
	return name, offset == 0 && (short == "UTC" || short == "GMT")
}
