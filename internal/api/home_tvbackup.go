package api

// Usherr: Real TV backups. Each exact show of a replayed day has a backup in
// its spirit, chosen when the day is built; when the show can't be streamed
// as it comes on (not posted yet), the backup plays in its slot. A different
// episode each day.

import (
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/tv"
)

// tvBackup is what plays instead of p, when p's slot has a backup.
func (s *Server) tvBackup(ch tv.Channel, p tv.Program) (tv.Program, bool) {
	tmdbID, season, episode, ok := tv.ParseStreamPath(p.FilePath)
	if !ok {
		return tv.Program{}, false
	}
	var b *tv.FixedItem
	for _, f := range ch.Rules.Fixed {
		if f.Backup != nil && f.StreamTMDB == tmdbID && f.Season == season && f.Episode == episode {
			b = f.Backup
		}
	}
	if b == nil {
		return tv.Program{}, false
	}
	show := b.SeriesID
	if show == 0 && b.StreamTMDB > 0 {
		show = -b.StreamTMDB
	}
	if show == 0 {
		return tv.Program{}, false
	}
	list, err := s.tvLibrary().Episodes(show)
	if err != nil || len(list) == 0 {
		return tv.Program{}, false
	}
	// The same episode all through the slot, another one tomorrow.
	day := p.Start.Local()
	idx := (day.YearDay() + day.Year()*7 + int(tmdbID%97)) % len(list)
	it := list[idx]
	sub := strings.TrimSpace(strings.TrimPrefix(it.Subtitle+" · in place of "+p.Title, " · "))
	return tv.Program{ChannelID: p.ChannelID, Start: p.Start, End: p.End, Kind: it.Kind, ItemID: it.ID, SeriesID: it.SeriesID,
		Title: it.Title, Subtitle: sub, FilePath: it.Path, Offset: p.Offset}, true
}

var _ = time.Now
