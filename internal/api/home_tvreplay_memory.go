package api

// Usherr: Real TV remembers the AI's choices (table tv_replay_picks). A slot
// the network can't replay (its 6 PM local news) is the same every weekday,
// so what the AI chose for it once is used again on the next build and on the
// nightly refresh; only slots never seen before go to the AI. The same goes
// for the backup of each exact show.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/tv"
)

// replayPickKey names a slot's choice: the station, what aired, and its hour
// (a show at 6 PM and again at 1 AM may want different stand-ins). A backup
// is the same for a show whenever it airs.
func replayPickKey(n *replayNetwork, sl replaySlot) string {
	title := strings.ToLower(strings.Join(strings.Fields(sl.Title), " "))
	if sl.Exact {
		return fmt.Sprintf("%s|%s|backup", strings.ToUpper(n.CallSign), title)
	}
	hour := sl.At
	if len(hour) >= 2 {
		hour = hour[:2]
	}
	return fmt.Sprintf("%s|%s|%s", strings.ToUpper(n.CallSign), title, hour)
}

// replayRecall fills a network's open slots and missing backups from the
// choices remembered for them. It says how many it filled.
func (s *Server) replayRecall(ctx context.Context, n *replayNetwork) int {
	filled := 0
	for k := range n.Slots {
		sl := &n.Slots[k]
		needs := (!sl.Exact && sl.Plays == "") || (sl.Exact && sl.From == "stream" && sl.Backup == nil)
		if !needs {
			continue
		}
		var seriesID, streamTMDB int64
		var show, why string
		if err := s.db.QueryRowContext(ctx, `SELECT series_id, stream_tmdb, show, why FROM tv_replay_picks WHERE key = ?`, replayPickKey(n, *sl)).
			Scan(&seriesID, &streamTMDB, &show, &why); err != nil {
			continue
		}
		if seriesID > 0 {
			// Still in the library?
			var title string
			if err := s.db.QueryRowContext(ctx, `SELECT title FROM series WHERE id = ?`, seriesID).Scan(&title); err != nil {
				continue
			}
			show = title
		} else if streamTMDB <= 0 {
			continue
		}
		from := "library"
		if seriesID == 0 {
			from = "stream"
		}
		if sl.Exact {
			sl.Backup, sl.BackupName = &tv.FixedItem{SeriesID: seriesID, StreamTMDB: streamTMDB, Show: show}, show
		} else {
			sl.SeriesID, sl.StreamTMDB, sl.From, sl.Plays, sl.Show, sl.Why = seriesID, streamTMDB, from, show, show, why
		}
		filled++
	}
	return filled
}

// replayRemember keeps a network's choices for next time (not the stopgap
// "more of the show before", which isn't a choice).
func (s *Server) replayRemember(ctx context.Context, n *replayNetwork) {
	now := time.Now().UnixMilli()
	for _, sl := range n.Slots {
		var seriesID, streamTMDB int64
		var show, why string
		switch {
		case sl.Exact && sl.Backup != nil:
			seriesID, streamTMDB, show = sl.Backup.SeriesID, sl.Backup.StreamTMDB, sl.Backup.Show
		case !sl.Exact && sl.Plays != "" && sl.Why != "more of the show before":
			seriesID, streamTMDB, show, why = sl.SeriesID, sl.StreamTMDB, sl.Show, sl.Why
		default:
			continue
		}
		if seriesID == 0 && streamTMDB == 0 {
			continue
		}
		_, _ = s.db.ExecContext(ctx, `INSERT INTO tv_replay_picks (key, series_id, stream_tmdb, show, why, updated_ms) VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(key) DO UPDATE SET series_id = excluded.series_id, stream_tmdb = excluded.stream_tmdb, show = excluded.show, why = excluded.why, updated_ms = excluded.updated_ms`,
			replayPickKey(n, sl), seriesID, streamTMDB, show, why, now)
	}
}
