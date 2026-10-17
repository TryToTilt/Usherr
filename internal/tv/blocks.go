package tv

// Usherr: programming like a network. A channel can start its programs on a
// grid (on the hour, and every 15, 30 or 60 minutes), give parts of the day
// line-ups of their own (dayparts), pin shows or movies to times (slots),
// and fill the time between programs with bumpers from a folder and "Up
// next" cards. Channels that use none of it are built as before (Build).

import (
	"io/fs"
	"math/rand"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// DaypartHours are the parts of the day, in local time. Late night runs past
// midnight (hours 24 and 25 are 0:00 and 1:00).
var DaypartHours = []struct {
	Part     string
	From, To int
}{
	{"overnight", 2, 6},
	{"morning", 6, 9},
	{"midmorning", 9, 12},
	{"afternoon", 12, 17},
	{"evening", 17, 20},
	{"prime", 20, 23},
	{"latenight", 23, 26},
}

// DaypartOf is the part of the day t falls in.
func DaypartOf(t time.Time) string {
	h := t.Hour()
	if h < 2 {
		h += 24
	}
	for _, d := range DaypartHours {
		if h >= d.From && h < d.To {
			return d.Part
		}
	}
	return "overnight"
}

// usesBlocks says whether a channel is built by buildBlocks.
func usesBlocks(r Rules) bool {
	return r.Grid > 0 || len(r.Dayparts) > 0 || len(r.Slots) > 0 || strings.TrimSpace(r.Filler.Folder) != "" ||
		r.Holidays || r.Premieres || r.Breaks > 0 || r.FamilyHours
}

// PartState is where a daypart's own line-up is in its rotation.
type PartState struct {
	Round     []int64 `json:"round,omitempty"`
	ShowIndex int     `json:"showIndex,omitempty"`
	MovieDeck []int64 `json:"movieDeck,omitempty"`
	MovieDay  string  `json:"movieDay,omitempty"`
	MoviesDay int     `json:"moviesDay,omitempty"`
	Since     int     `json:"since,omitempty"`
}

// pool is a line-up: the channel's own, or a daypart's.
type pool struct {
	key    string // "" for the channel's own
	shows  []int64
	eps    map[int64][]Item
	movies []Item
	byID   map[int64]Item
}

func makePool(key string, r Rules, lib Library, cache map[int64][]Item) (*pool, error) {
	shows, err := channelShows(r, lib)
	if err != nil {
		return nil, err
	}
	p := &pool{key: key, eps: map[int64][]Item{}, byID: map[int64]Item{}}
	for _, s := range shows {
		list, ok := cache[s]
		if !ok {
			if list, err = lib.Episodes(s); err != nil {
				return nil, err
			}
			cache[s] = list
		}
		if len(list) > 0 {
			p.eps[s] = list
		}
	}
	p.shows = keepWith(shows, p.eps)
	if p.movies, err = lib.Movies(r.Movies, r.AllMovies, r.Genres, r.Decades); err != nil {
		return nil, err
	}
	for _, m := range p.movies {
		p.byID[m.ID] = m
	}
	return p, nil
}

func (p *pool) empty() bool { return len(p.shows) == 0 && len(p.movies) == 0 }

// familyAge is the oldest age family hours allow (about PG).
const familyAge = 10

// family is the pool without anything for older than familyAge; it keeps
// its own rotation (key "fam:...").
func (p *pool) family() *pool {
	f := &pool{key: "fam:" + p.key, eps: map[int64][]Item{}, byID: map[int64]Item{}}
	for _, s := range p.shows {
		var keep []Item
		for _, e := range p.eps[s] {
			if e.Age <= familyAge {
				keep = append(keep, e)
			}
		}
		if len(keep) > 0 {
			f.shows = append(f.shows, s)
			f.eps[s] = keep
		}
	}
	for _, m := range p.movies {
		if m.Age <= familyAge {
			f.movies = append(f.movies, m)
			f.byID[m.ID] = m
		}
	}
	return f
}

// familyTime is when family hours are on: 6 AM to 9 PM.
func familyTime(t time.Time) bool { return t.Hour() >= 6 && t.Hour() < 21 }

// holiday is a season and the words its episodes and movies have in their titles.
type holiday struct {
	name  string
	words []string
}

// holidayAt is the holiday season t falls in, if any.
func holidayAt(t time.Time) *holiday {
	m, d := t.Month(), t.Day()
	switch {
	case m == time.October:
		return &holiday{"halloween", []string{"halloween", "trick or treat", "treehouse of horror", "haunted", "ghost", "pumpkin", "monster"}}
	case m == time.November && d >= 15:
		return &holiday{"thanksgiving", []string{"thanksgiving", "turkey", "pilgrim"}}
	case m == time.December && d <= 26:
		return &holiday{"christmas", []string{"christmas", "xmas", "santa", "noel", "holiday", "hanukkah", "snow", "reindeer", "mistletoe"}}
	case (m == time.December && d >= 27) || (m == time.January && d == 1):
		return &holiday{"newyear", []string{"new year", "auld lang", "countdown", "resolution"}}
	case m == time.February && d >= 7 && d <= 14:
		return &holiday{"valentine", []string{"valentine", "cupid", "love"}}
	case m == time.July && d <= 4:
		return &holiday{"july4", []string{"fourth of july", "4th of july", "independence day", "fireworks"}}
	}
	if e := easter(t.Year()); !t.Before(e.AddDate(0, 0, -7)) && t.Before(e.AddDate(0, 0, 1)) {
		return &holiday{"easter", []string{"easter", "bunny", "egg hunt"}}
	}
	return nil
}

// easter is Easter Sunday (the Western date) of a year.
func easter(y int) time.Time {
	a, b, c := y%19, y/100, y%100
	d, e := b/4, b%4
	f := (b + 8) / 25
	g := (b - f + 1) / 3
	h := (19*a + b - d - g + 15) % 30
	i, k := c/4, c%4
	l := (32 + 2*e + 2*i - h - k) % 7
	m := (a + 11*h + 22*l) / 451
	month := (h + l - 7*m + 114) / 31
	day := (h+l-7*m+114)%31 + 1
	return time.Date(y, time.Month(month), day, 0, 0, 0, 0, time.Local)
}

func (h *holiday) matches(it Item) bool {
	text := strings.ToLower(it.Title + " " + it.Subtitle)
	if it.Kind == "episode" {
		text = strings.ToLower(it.Subtitle) // the episode's own name, not the show's
	}
	for _, w := range h.words {
		if strings.Contains(text, w) {
			return true
		}
	}
	return false
}

// holidayPick is the pool's next holiday episode or movie, in turn.
func holidayPick(pl *pool, st *State, h *holiday) (Item, bool) {
	var list []Item
	for _, s := range pl.shows {
		for _, e := range pl.eps[s] {
			if h.matches(e) {
				list = append(list, e)
			}
		}
	}
	for _, m := range pl.movies {
		if h.matches(m) {
			list = append(list, m)
		}
	}
	if len(list) == 0 {
		return Item{}, false
	}
	if st.Holiday == nil {
		st.Holiday = map[string]int{}
	}
	i := st.Holiday[h.name] % len(list)
	st.Holiday[h.name] = i + 1
	return list[i], true
}

// premierePick is an episode that came out in the two weeks before t and
// hasn't had its premiere on this channel yet.
func premierePick(pl *pool, st *State, t time.Time) (Item, bool) {
	from := t.AddDate(0, 0, -14).Format("2006-01-02")
	today := t.Format("2006-01-02")
	for _, s := range pl.shows {
		for _, e := range pl.eps[s] {
			if e.AirDate == "" || e.AirDate < from || e.AirDate > today {
				continue
			}
			if _, done := st.Premiered[e.Path]; !done {
				return e, true
			}
		}
	}
	return Item{}, false
}

// stateFor is the rotation state a pool uses: the channel's own, or a
// daypart's (sharing where each show is up to, so a show in two parts carries on).
func stateFor(st *State, key string) (*State, func()) {
	if key == "" {
		return st, func() {}
	}
	if st.Parts == nil {
		st.Parts = map[string]*PartState{}
	}
	ps := st.Parts[key]
	if ps == nil {
		ps = &PartState{}
		st.Parts[key] = ps
	}
	tmp := &State{Next: st.Next, Round: ps.Round, ShowIndex: ps.ShowIndex, MovieDeck: ps.MovieDeck, MovieDay: ps.MovieDay, MoviesDay: ps.MoviesDay}
	return tmp, func() {
		ps.Round, ps.ShowIndex, ps.MovieDeck, ps.MovieDay, ps.MoviesDay = tmp.Round, tmp.ShowIndex, tmp.MovieDeck, tmp.MovieDay, tmp.MoviesDay
	}
}

// slotRun is one airing of a pinned slot.
type slotRun struct {
	at   time.Time
	slot Slot
}

// slotRuns lists the airings of slots in [from, to), in time order.
func slotRuns(slots []Slot, from, to time.Time) []slotRun {
	var out []slotRun
	day := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, from.Location())
	for ; day.Before(to); day = day.AddDate(0, 0, 1) {
		for _, s := range slots {
			h, m, ok := clockOf(s.At)
			if !ok || !onDay(s.Days, day.Weekday()) {
				continue
			}
			at := time.Date(day.Year(), day.Month(), day.Day(), h, m, 0, 0, day.Location())
			if !at.Before(from) && at.Before(to) {
				out = append(out, slotRun{at: at, slot: s})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].at.Before(out[j].at) })
	return out
}

func clockOf(s string) (int, int, bool) {
	hs, ms, ok := strings.Cut(strings.TrimSpace(s), ":")
	h, err1 := strconv.Atoi(hs)
	m, err2 := strconv.Atoi(ms)
	if !ok || err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, false
	}
	return h, m, true
}

func onDay(days []int, wd time.Weekday) bool {
	if len(days) == 0 {
		return true
	}
	for _, d := range days {
		if d == int(wd) {
			return true
		}
	}
	return false
}

// nextGrid is the first grid line at or after t.
func nextGrid(t time.Time, grid int) time.Time {
	if grid <= 0 {
		return t
	}
	step := time.Duration(grid) * time.Minute
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	since := t.Sub(day)
	n := since / step
	if since%step != 0 {
		n++
	}
	return day.Add(n * step)
}

// FillerItems lists the videos in a filler folder that a browser can play.
func FillerItems(folder string) []Item {
	folder = strings.TrimSpace(folder)
	if folder == "" {
		return nil
	}
	var out []Item
	_ = filepath.WalkDir(folder, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(p)) {
		case ".mp4", ".m4v", ".webm", ".mov":
		default:
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		name := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
		out = append(out, Item{Kind: "filler", Title: "Break", Subtitle: name, Path: p, Size: info.Size()})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// buildBlocks lays out a channel that uses a grid, dayparts, slots or filler.
func buildBlocks(ch Channel, lib Library, probe Prober, st *State, from, until time.Time, rnd *rand.Rand) ([]Program, error) {
	r := ch.Rules
	cache := map[int64][]Item{}
	main, err := makePool("", r, lib, cache)
	if err != nil {
		return nil, err
	}
	parts := map[string]*pool{}
	for _, dp := range r.Dayparts {
		pr := Rules{Series: dp.Series, Movies: dp.Movies, Genres: dp.Genres, Decades: dp.Decades, AllMovies: dp.AllMovies, Stream: dp.Stream}
		p, err := makePool(dp.Part, pr, lib, cache)
		if err != nil {
			return nil, err
		}
		if !p.empty() {
			parts[dp.Part] = p
		}
	}
	if main.empty() && len(parts) == 0 && len(r.Slots) == 0 {
		return nil, errNothing(ch.Name)
	}
	order := r.Order
	if order == "" {
		order = "rotate"
	}

	// Filler: the folder's videos (measured once), shuffled, then cards.
	var fillers []Item
	for _, f := range FillerItems(r.Filler.Folder) {
		if d, err := probe(f); err == nil && d >= 2*time.Second {
			f.Runtime = d
			fillers = append(fillers, f)
		}
	}
	rnd.Shuffle(len(fillers), func(a, b int) { fillers[a], fillers[b] = fillers[b], fillers[a] })
	fi := 0

	var out []Program
	add := func(it Item, start time.Time, d time.Duration, block string) {
		sub := it.Subtitle
		if block != "" {
			sub = strings.TrimSpace(block + " · " + sub)
			sub = strings.TrimSuffix(sub, " ·")
		}
		out = append(out, Program{ChannelID: ch.ID, Start: start, End: start.Add(d), Kind: it.Kind, ItemID: it.ID, SeriesID: it.SeriesID, Title: it.Title, Subtitle: sub, FilePath: it.Path})
	}
	// fill covers [a, b) with filler videos that fit, then a card.
	fill := func(a, b time.Time) {
		for len(fillers) > 0 && b.Sub(a) > 15*time.Second {
			placed := false
			for tries := 0; tries < len(fillers); tries++ {
				f := fillers[fi%len(fillers)]
				fi++
				if f.Runtime <= b.Sub(a) {
					add(f, a, f.Runtime, "")
					a = a.Add(f.Runtime)
					placed = true
					break
				}
			}
			if !placed {
				break
			}
		}
		if b.Sub(a) >= time.Second && !r.Filler.NoCards {
			out = append(out, Program{ChannelID: ch.ID, Start: a, End: b, Kind: "card", Title: "Up next"})
		}
	}
	measure := func(it Item) (time.Duration, bool) {
		d, err := probe(it)
		return d, err == nil && d >= 30*time.Second
	}
	// Commercial breaks: a show of 12 minutes or more is split into parts,
	// with ads from the filler folder between them.
	breakLen := time.Duration(max(r.BreakMinutes, 2)) * time.Minute
	breaksFor := func(it Item, d time.Duration) int {
		if r.Breaks <= 0 || len(fillers) == 0 || it.Kind == "stream" || d < 12*time.Minute {
			return 0
		}
		return min(r.Breaks, 4)
	}
	total := func(it Item, d time.Duration) time.Duration { return d + time.Duration(breaksFor(it, d))*breakLen }
	// breakAt fills one break from a with ads, up to breakLen; it returns where it ends.
	breakAt := func(a time.Time) time.Time {
		end := a.Add(breakLen)
		for tries := 0; tries < len(fillers)*2 && end.Sub(a) > 5*time.Second; tries++ {
			f := fillers[fi%len(fillers)]
			fi++
			if f.Runtime <= end.Sub(a) {
				add(f, a, f.Runtime, "")
				a = a.Add(f.Runtime)
			}
		}
		return a
	}
	// place puts an item on at start, split for its breaks; it returns where it ends.
	place := func(it Item, start time.Time, d time.Duration, block string) time.Time {
		n := breaksFor(it, d)
		if n == 0 {
			add(it, start, d, block)
			return start.Add(d)
		}
		part := d / time.Duration(n+1)
		at := start
		for i := 0; i <= n; i++ {
			pd := part
			if i == n {
				pd = d - part*time.Duration(n)
			}
			add(it, at, pd, block)
			out[len(out)-1].Offset = part * time.Duration(i)
			at = at.Add(pd)
			if i < n {
				at = breakAt(at)
			}
		}
		return at
	}
	famPools := map[string]*pool{}
	familyOf := func(p *pool) *pool {
		if f, ok := famPools[p.key]; ok {
			return f
		}
		f := p.family()
		famPools[p.key] = f
		return f
	}
	if st.Premiered == nil {
		st.Premiered = map[string]string{}
	}
	holidayTurn := 0

	runs := slotRuns(r.Slots, from.Add(-time.Minute), until.Add(24*time.Hour))
	t := from
	if r.Grid > 0 {
		next := nextGrid(t, r.Grid)
		fill(t, next)
		t = next
	}
	sinceMovie := map[string]int{}
	failures := 0
	for t.Before(until) {
		if failures > 50 {
			return out, errMeasure(ch.Name)
		}
		// A slot due now (one we already ran past is skipped).
		for len(runs) > 0 && runs[0].at.Before(t.Add(-time.Minute)) {
			runs = runs[1:]
		}
		if len(runs) > 0 && !runs[0].at.After(t.Add(time.Minute)) {
			run := runs[0]
			runs = runs[1:]
			for _, it := range slotItems(run.slot, main, lib, cache, st, rnd) {
				if d, ok := measure(it); ok {
					t = place(it, t, d, run.slot.Name)
				}
			}
			if r.Grid > 0 {
				next := nextGrid(t, r.Grid)
				fill(t, next)
				t = next
			}
			continue
		}
		var pin time.Time
		if len(runs) > 0 {
			pin = runs[0].at
		}
		pl := main
		if p, ok := parts[DaypartOf(t)]; ok {
			pl = p
		}
		if r.FamilyHours && familyTime(t) {
			pl = familyOf(pl)
		}
		if pl.empty() {
			// Nothing of its own and no line-up: fill to the next grid line or slot.
			next := t.Add(30 * time.Minute)
			if r.Grid > 0 {
				next = nextGrid(t.Add(time.Second), r.Grid)
			}
			if !pin.IsZero() && pin.Before(next) {
				next = pin
			}
			fill(t, next)
			t = next
			continue
		}
		pst, done := stateFor(st, pl.key)
		since := sinceMovie[pl.key]
		var it Item
		var d time.Duration
		ok := false
		block := ""
		fits := func(c Item, cd time.Duration) bool { return pin.IsZero() || !t.Add(total(c, cd)).After(pin) }
		// A premiere in prime time, or (every other show) a holiday episode in its season.
		if r.Premieres && DaypartOf(t) == "prime" {
			if c, found := premierePick(pl, st, t); found {
				if cd, good := measure(c); good && fits(c, cd) {
					it, d, ok, block = c, cd, true, "NEW"
					st.Premiered[c.Path] = t.Format("2006-01-02")
				}
			}
		}
		if !ok && r.Holidays {
			if h := holidayAt(t); h != nil {
				holidayTurn++
				if holidayTurn%2 == 1 {
					if c, found := holidayPick(pl, st, h); found {
						if cd, good := measure(c); good && fits(c, cd) {
							it, d, ok = c, cd, true
						}
					}
				}
			}
		}
		if ok {
			// a premiere or a holiday episode, chosen above
		} else if !pin.IsZero() && pin.Sub(t) < 90*time.Minute {
			// Close to a slot: only something that fits before it.
			it, d, ok = fitPick(pl, pst, pin.Sub(t), func(c Item) (time.Duration, bool) {
				cd, good := measure(c)
				return cd, good && total(c, cd) <= pin.Sub(t)
			})
		} else if it, ok = nextItem(order, pl.shows, pl.eps, pl.movies, pl.byID, pst, rnd, &since, r.MaxMoviesPerDay, t); ok {
			var good bool
			if d, good = measure(it); !good {
				failures++
				sinceMovie[pl.key] = since
				done()
				continue
			}
			if !fits(it, d) {
				unpick(pl, pst, it)
				ok = false
			}
		}
		sinceMovie[pl.key] = since
		done()
		if !ok {
			next := pin
			if next.IsZero() {
				break
			}
			fill(t, next)
			t = next
			continue
		}
		failures = 0
		t = place(it, t, d, block)
		if r.Grid > 0 {
			next := nextGrid(t, r.Grid)
			if !pin.IsZero() && pin.Before(next) {
				next = pin
			}
			fill(t, next)
			t = next
		}
	}
	return out, nil
}

// slotItems are what a slot plays: its show's next episodes, or a movie.
func slotItems(s Slot, main *pool, lib Library, cache map[int64][]Item, st *State, rnd *rand.Rand) []Item {
	if s.Movie {
		if len(main.movies) == 0 {
			return nil
		}
		if m, ok := nextMovie(main.movies, main.byID, st, rnd); ok {
			return []Item{m}
		}
		return nil
	}
	show := s.SeriesID
	if show == 0 && s.StreamTMDB > 0 {
		show = -s.StreamTMDB
	}
	if show == 0 {
		return nil
	}
	list, ok := cache[show]
	if !ok {
		list, _ = lib.Episodes(show)
		cache[show] = list
	}
	if len(list) == 0 {
		return nil
	}
	n := max(s.Count, 1)
	var out []Item
	for i := 0; i < n; i++ {
		idx := st.Next[show] % len(list)
		st.Next[show] = (idx + 1) % len(list)
		out = append(out, list[idx])
	}
	return out
}

// fitPick takes the first show (in the pool's order) whose next episode fits
// in room, without moving the others on.
func fitPick(pl *pool, st *State, room time.Duration, measure func(Item) (time.Duration, bool)) (Item, time.Duration, bool) {
	for _, show := range pl.shows {
		list := pl.eps[show]
		idx := st.Next[show] % len(list)
		it := list[idx]
		if d, ok := measure(it); ok && d <= room {
			st.Next[show] = (idx + 1) % len(list)
			return it, d, true
		}
	}
	return Item{}, 0, false
}

// unpick puts back an item nextItem handed out, so it comes on next time.
func unpick(pl *pool, st *State, it Item) {
	if it.Kind == "movie" {
		st.MovieDeck = append([]int64{it.ID}, st.MovieDeck...)
		if st.MoviesDay > 0 {
			st.MoviesDay--
		}
		return
	}
	list := pl.eps[it.SeriesID]
	for i, e := range list {
		if e.ID == it.ID && e.Path == it.Path {
			st.Next[it.SeriesID] = i
			break
		}
	}
	st.Round = append([]int64{it.SeriesID}, st.Round...)
}
