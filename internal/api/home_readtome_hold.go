package api

// Usherr: when Read-to-Me must wait. Speaking a book takes the graphics card, so
// a job holds back (and says so) while
//   - "night only" is on and it isn't night yet (two times of day, which may
//     cross midnight), and
//   - something is playing: in Usherr's own player, or on Jellyfin or Emby (a
//     paused picture doesn't count), so a film never stutters for an audiobook.
//
// A job that waits keeps what it has spoken and goes on when the way is clear.

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"
)

// rtvHoldPoll is how often a held job looks again.
var rtvHoldPoll = 30 * time.Second

// rtvClockMinutes reads "23:30" as minutes after midnight.
func rtvClockMinutes(s string) (int, bool) {
	if !validClock(s) {
		return 0, false
	}
	h, _ := strconv.Atoi(s[:2])
	m, _ := strconv.Atoi(s[3:])
	return h*60 + m, true
}

// inNightWindow says whether now is between from and to. A window whose end is
// before its start crosses midnight (23:00 to 07:00); equal times mean all day.
func inNightWindow(now time.Time, from, to string) bool {
	f, ok1 := rtvClockMinutes(from)
	t, ok2 := rtvClockMinutes(to)
	if !ok1 || !ok2 || f == t {
		return true
	}
	n := now.Hour()*60 + now.Minute()
	if f < t {
		return n >= f && n < t
	}
	return n >= f || n < t
}

// readToMeHold says why audiobooks shouldn't be made right now; "" means go ahead.
func (s *Server) readToMeHold(ctx context.Context) string { return s.readToMeHoldAt(ctx, time.Now()) }

func (s *Server) readToMeHoldAt(ctx context.Context, now time.Time) string {
	if night, _ := s.Settings.Get(keyRTNightOnly); night == "1" {
		from, _ := s.Settings.Get(keyRTNightFrom)
		to, _ := s.Settings.Get(keyRTNightTo)
		if !validClock(from) {
			from = defaultNightFrom
		}
		if !validClock(to) {
			to = defaultNightTo
		}
		if !inNightWindow(now, from, to) {
			return fmt.Sprintf("Waiting for the night hours (%s to %s)", from, to)
		}
	}
	if s.somethingPlaying(ctx) {
		return "Waiting while something is playing"
	}
	return ""
}

var playingAsk = struct {
	sync.Mutex
	at time.Time
	on bool
}{}

// somethingPlaying: Usherr's own player, or a media server, is showing
// something that isn't paused. Asked at most every 15 seconds.
func (s *Server) somethingPlaying(ctx context.Context) bool {
	for _, p := range usherrNowList() {
		if !p.paused {
			return true
		}
	}
	playingAsk.Lock()
	defer playingAsk.Unlock()
	if time.Since(playingAsk.at) < 15*time.Second {
		return playingAsk.on
	}
	on := false
	if list, err := s.MediaServers.List(); err == nil {
		for _, m := range list {
			if !m.Enabled {
				continue
			}
			ask, cancel := context.WithTimeout(ctx, 4*time.Second)
			playing, err := s.mediaClient.NowPlaying(ask, m)
			cancel()
			if err != nil {
				continue
			}
			for _, p := range playing {
				if !p.Paused {
					on = true
				}
			}
		}
	}
	playingAsk.at, playingAsk.on = time.Now(), on
	return on
}
