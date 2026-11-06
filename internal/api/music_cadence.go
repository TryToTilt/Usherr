package api

// Adapted from DroppedNeedle (AGPL-3.0),
// backend/services/native/wanted_watcher_service.py (_interval_days): a
// wanted album is searched again on a cadence set by its age. A new release
// turns up on indexers within days, so it is looked for every two days; an
// old one that wasn't found is unlikely to appear tomorrow, so every two
// weeks. Each wait is jittered by a fifth either way so the albums don't
// all come due together.

import (
	"math/rand/v2"
	"strconv"
	"strings"
	"time"
)

// albumSearchInterval is how long to wait before searching again for an
// album first released on releaseDate ("YYYY[-MM[-DD]]"; unknown counts as old).
func albumSearchInterval(releaseDate string, now time.Time) time.Duration {
	day := 24 * time.Hour
	released, ok := partialDate(releaseDate)
	if !ok {
		return 14 * day
	}
	switch age := now.Sub(released); {
	case age < 30*day:
		return 2 * day
	case age < 90*day:
		return 4 * day
	case age < 365*day:
		return 7 * day
	}
	return 14 * day
}

// partialDate reads a MusicBrainz date as the start of its period.
func partialDate(s string) (time.Time, bool) {
	parts := strings.Split(strings.TrimSpace(s), "-")
	nums := []int{0, 1, 1}
	for i := 0; i < len(parts) && i < 3; i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return time.Time{}, false
		}
		nums[i] = n
	}
	if len(parts[0]) != 4 || nums[1] < 1 || nums[1] > 12 || nums[2] < 1 || nums[2] > 31 {
		return time.Time{}, false
	}
	return time.Date(nums[0], time.Month(nums[1]), nums[2], 0, 0, 0, 0, time.UTC), true
}

// albumSearchDue reports whether the scheduled hunt may search for an album
// now. An album never searched since Usherr started is due.
func (s *Server) albumSearchDue(albumID int64, now time.Time) bool {
	s.music.mu.Lock()
	defer s.music.mu.Unlock()
	next, ok := s.music.nextSearch[albumID]
	return !ok || !now.Before(next)
}

// albumSearched sets when the hunt next looks for an album.
func (s *Server) albumSearched(albumID int64, releaseDate string, now time.Time) {
	wait := albumSearchInterval(releaseDate, now)
	wait = time.Duration(float64(wait) * (0.8 + 0.4*rand.Float64()))
	s.music.mu.Lock()
	defer s.music.mu.Unlock()
	if s.music.nextSearch == nil {
		s.music.nextSearch = map[int64]time.Time{}
	}
	s.music.nextSearch[albumID] = now.Add(wait)
}
