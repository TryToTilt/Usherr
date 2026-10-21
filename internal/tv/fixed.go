package tv

// Usherr: Real TV. A channel can replay a real network's day: each slot at
// its time, with the program the network aired when it can be played, or
// something in its spirit when it can't (news, sports). Each program fills
// its slot as on TV: the show in parts, with commercial breaks from the
// filler folder making up the rest of the time. The day repeats daily.

import (
	"math/rand"
	"strconv"
	"strings"
	"time"
)

// RealTVSource is the station a Real TV channel replays.
type RealTVSource struct {
	Zip      string `json:"zip"`
	Lineup   string `json:"lineup,omitempty"` // the provider's lineup; "" is over the air
	GuideID  string `json:"guideId"`          // the station in the listings
	Network  string `json:"network,omitempty"`
	CallSign string `json:"callSign,omitempty"`
	// Daily: each night the channel gets the day before, as it aired.
	Daily bool `json:"daily"`
}

// FixedItem is one slot of a fixed day.
type FixedItem struct {
	At      string `json:"at"`            // "19:00", local time
	Minutes int    `json:"minutes"`       // the slot's length
	Was     string `json:"was,omitempty"` // what the network aired, for the guide
	Exact   bool   `json:"exact,omitempty"`
	// What plays. One exact episode: StreamTMDB with Season and Episode, or a
	// library episode (SeriesID with EpisodeID). Or the next episodes of a
	// show: SeriesID (library) or StreamTMDB alone. Or a library movie.
	StreamTMDB int64  `json:"streamTmdb,omitempty"`
	Season     int    `json:"season,omitempty"`
	Episode    int    `json:"episode,omitempty"`
	SeriesID   int64  `json:"seriesId,omitempty"`
	EpisodeID  int64  `json:"episodeId,omitempty"`
	MovieID    int64  `json:"movieId,omitempty"`
	Show       string `json:"show,omitempty"` // its name, for the settings
	// Backup plays when the exact episode can't be streamed when it comes on
	// (not on Usenet yet): a show in the same spirit (SeriesID or StreamTMDB).
	Backup *FixedItem `json:"backup,omitempty"`
}

// fixedClock reads "19:00" as hours and minutes.
func fixedClock(at string) (int, int, bool) {
	h, m, ok := strings.Cut(at, ":")
	if !ok {
		return 0, 0, false
	}
	hh, err1 := strconv.Atoi(h)
	mm, err2 := strconv.Atoi(m)
	if err1 != nil || err2 != nil || hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return 0, 0, false
	}
	return hh, mm, true
}

// fixedItems are what a slot plays, before they're fitted to it.
func fixedItems(f FixedItem, room time.Duration, lib Library, probe Prober, st *State, cache map[int64][]Item, seed int64) []Item {
	episodes := func(id int64) []Item {
		list, ok := cache[id]
		if !ok {
			list, _ = lib.Episodes(id)
			cache[id] = list
		}
		return list
	}
	measure := func(it Item) time.Duration {
		if d, err := probe(it); err == nil && d > 0 {
			return d
		}
		return it.Runtime
	}
	switch {
	case f.MovieID > 0:
		list, _ := lib.Movies([]int64{f.MovieID}, false, nil, nil)
		return list
	case f.StreamTMDB > 0 && f.Season > 0:
		want := StreamPath(f.StreamTMDB, f.Season, f.Episode)
		for _, it := range episodes(-f.StreamTMDB) {
			if it.Path == want {
				return []Item{it}
			}
		}
		return nil
	case f.SeriesID > 0 && f.EpisodeID > 0:
		for _, it := range episodes(f.SeriesID) {
			if it.ID == f.EpisodeID {
				return []Item{it}
			}
		}
		return nil
	}
	// The next episodes of a show, as many as fit the slot (at least one).
	show := f.SeriesID
	if show == 0 && f.StreamTMDB > 0 {
		show = -f.StreamTMDB
	}
	list := episodes(show)
	if show == 0 || len(list) == 0 {
		return nil
	}
	var out []Item
	used := time.Duration(0)
	for i := 0; i < 6; i++ {
		if _, ok := st.Next[show]; !ok {
			// A show starts somewhere in its run, not always at the pilot,
			// and not at the same episode on every channel.
			h := uint64(show)*2654435761 + uint64(seed)*40503
			st.Next[show] = int(h % uint64(len(list)))
		}
		idx := st.Next[show] % len(list)
		it := list[idx]
		d := measure(it)
		if d <= 0 {
			d = 22 * time.Minute
		}
		if len(out) > 0 && used+d > room {
			break
		}
		st.Next[show] = (idx + 1) % len(list)
		it.Runtime = d
		out = append(out, it)
		used += d
		if used >= room {
			break
		}
	}
	return out
}

// buildFixed lays out a fixed day, repeated daily, from from until until.
func buildFixed(ch Channel, lib Library, probe Prober, st *State, from, until time.Time, rnd *rand.Rand) ([]Program, error) {
	r := ch.Rules
	var fillers []Item
	for _, f := range FillerItems(r.Filler.Folder) {
		if d, err := probe(f); err == nil && d >= 2*time.Second {
			f.Runtime = d
			fillers = append(fillers, f)
		}
	}
	rnd.Shuffle(len(fillers), func(a, b int) { fillers[a], fillers[b] = fillers[b], fillers[a] })
	fi := 0
	cache := map[int64][]Item{}
	var out []Program
	add := func(it Item, start time.Time, d time.Duration, note string, offset time.Duration) {
		sub := it.Subtitle
		if note != "" {
			sub = strings.TrimSuffix(strings.TrimSpace(sub+" · "+note), " ·")
			sub = strings.TrimPrefix(sub, "· ")
		}
		out = append(out, Program{ChannelID: ch.ID, Start: start, End: start.Add(d), Kind: it.Kind, ItemID: it.ID, SeriesID: it.SeriesID,
			Title: it.Title, Subtitle: sub, FilePath: it.Path, Offset: offset})
	}
	// ads covers [a, b) with commercials that fit, then an "Up next" card.
	ads := func(a, b time.Time) {
		for len(fillers) > 0 && b.Sub(a) > 10*time.Second {
			placed := false
			for tries := 0; tries < len(fillers); tries++ {
				f := fillers[fi%len(fillers)]
				fi++
				if f.Runtime <= b.Sub(a) {
					add(f, a, f.Runtime, "", 0)
					a = a.Add(f.Runtime)
					placed = true
					break
				}
			}
			if !placed {
				break
			}
		}
		if b.Sub(a) >= time.Second {
			out = append(out, Program{ChannelID: ch.ID, Start: a, End: b, Kind: "card", Title: "Up next"})
		}
	}
	// The slots in time order, each day from the one before from (a slot
	// can run past midnight) to until.
	day := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.Local).AddDate(0, 0, -1)
	t := from
	for ; day.Before(until); day = day.AddDate(0, 0, 1) {
		for _, f := range r.Fixed {
			h, m, ok := fixedClock(f.At)
			if !ok || f.Minutes <= 0 {
				continue
			}
			start := time.Date(day.Year(), day.Month(), day.Day(), h, m, 0, 0, time.Local)
			end := start.Add(time.Duration(f.Minutes) * time.Minute)
			// The slot on as the channel starts is joined part-way, as on TV.
			inProgress := len(out) == 0 && start.Before(t) && end.After(t)
			if (start.Before(t) && !inProgress) || !start.Before(until) {
				continue // already on, or past what's being built
			}
			if start.After(t) {
				ads(t, start) // a gap before the slot
			}
			mark := len(out)
			room := end.Sub(start)
			items := fixedItems(f, room, lib, probe, st, cache, ch.ID)
			note := ""
			if !f.Exact && f.Was != "" {
				note = "in place of " + f.Was
			}
			var content time.Duration
			for i := range items {
				if items[i].Runtime <= 0 {
					if d, err := probe(items[i]); err == nil {
						items[i].Runtime = d
					}
				}
				if items[i].Runtime <= 0 {
					items[i].Runtime = room / time.Duration(len(items))
				}
				content += items[i].Runtime
			}
			if len(items) == 0 {
				ads(start, end)
				if inProgress {
					out = clipFrom(out, mark, t)
				}
				t = end
				continue
			}
			// The time the shows leave over goes in breaks: two or three per
			// show, as on TV (none when the show fills the slot).
			spare := room - content
			breaksPer := 0
			if spare > time.Minute && len(fillers) > 0 {
				breaksPer = 2
				if room >= 50*time.Minute {
					breaksPer = 3
				}
			}
			nBreaks := breaksPer * len(items)
			at := start
			for _, it := range items {
				d := it.Runtime
				if at.Add(d).After(end) {
					d = end.Sub(at) // longer than its slot: cut at the slot's end
				}
				if d <= 0 {
					break
				}
				parts := breaksPer + 1
				if breaksPer == 0 || d < 8*time.Minute {
					parts = 1
				}
				part := d / time.Duration(parts)
				for i := 0; i < parts; i++ {
					pd := part
					if i == parts-1 {
						pd = d - part*time.Duration(parts-1)
					}
					add(it, at, pd, note, part*time.Duration(i))
					at = at.Add(pd)
					if i < parts-1 && nBreaks > 0 {
						b := spare / time.Duration(nBreaks)
						ads(at, at.Add(b))
						at = at.Add(b)
					}
				}
			}
			if at.Before(end) {
				ads(at, end) // the rest of the slot, before the next one
			}
			if inProgress {
				out = clipFrom(out, mark, t)
			}
			t = end
		}
	}
	if len(out) == 0 {
		return nil, errNothing(ch.Name)
	}
	return out, nil
}

// clipFrom drops the programs from mark on that are over by t, and starts
// the one on at t part-way through.
func clipFrom(out []Program, mark int, t time.Time) []Program {
	kept := out[:mark]
	for _, p := range out[mark:] {
		if !p.End.After(t) {
			continue
		}
		if p.Start.Before(t) {
			if p.Kind != "card" {
				p.Offset += t.Sub(p.Start)
			}
			p.Start = t
		}
		kept = append(kept, p)
	}
	return kept
}
