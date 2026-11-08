package api

// Usherr: linked screenings, step 3. Synced playback, and the screening as a
// real booking on every Usherr.
//
//   - A guest Usherr whose people say they're in gets the screening in its own
//     Showtime (the Marquee counts down to it), marked as hosted by the linked
//     Usherr; moves and cancels follow, and it goes when nobody is going.
//   - The host's household is first on the roster.
//   - The pre-show: shortly before it starts, the host fixes its pre-show
//     (the plan it will play) and sends its recipe, the items in order and
//     their lengths. Each guest builds the same pre-show from its own cards,
//     clips and trailers; what it can't find is a card of the same length,
//     so the film starts at the same moment everywhere.
//   - The room: the host's play, pause and seek go to everyone as "at relay
//     time T it was at position P, playing or paused" (screening.Playback),
//     again every few seconds while it plays. Each Usherr keeps the latest and
//     its browsers follow it (web: screeningRoom.ts); with nothing said yet,
//     the schedule holds. Only the host moves the film; with "Anyone can
//     pause" on, a guest may ask it to pause (pause.request).
//
// Times between Usherrs are by the relay's clock: each Usherr knows how far
// its own clock is from the relay's (link status, skewMs).

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/link"
	"github.com/rdborg/mediarium/internal/link/screening"
)

var (
	screeningRecipeAhead = 15 * time.Minute // the pre-show is fixed and sent this long before it starts
	roomBeat             = 5 * time.Second  // the host says where the film is this often while it plays
	roomBeatPaused       = 15 * time.Second // ... and while it's paused
	roomTick             = time.Second      // how often the heartbeats are looked at
)

const (
	roomOpenBefore = 15 * time.Minute // "Join the screening" from this long before the pre-show
	roomWaiting    = "waiting"        // the pre-show hasn't started (not a message state)
)

// roomHub tells the browsers in a screening room that something changed
// (they ask for the room again).
type roomHub struct {
	mu   sync.Mutex
	subs map[int64]map[chan struct{}]bool
}

func (h *roomHub) subscribe(id int64) chan struct{} {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.subs == nil {
		h.subs = map[int64]map[chan struct{}]bool{}
	}
	if h.subs[id] == nil {
		h.subs[id] = map[chan struct{}]bool{}
	}
	ch := make(chan struct{}, 1)
	h.subs[id][ch] = true
	return ch
}

func (h *roomHub) unsubscribe(id int64, ch chan struct{}) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.subs[id], ch)
	if len(h.subs[id]) == 0 {
		delete(h.subs, id)
	}
}

func (h *roomHub) changed(id int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[id] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

type roomBeatSent struct {
	at    time.Time
	seq   int64
	state string
}

// ---- the room ----

// screeningRoom is a linked screening as this Usherr plays it: hosted here
// (a showtime shared with linked Usherrs) or a guest's booking.
type screeningRoom struct {
	Role        string // host, guest
	ShowtimeID  int64
	UserID      int64 // who booked it here
	SID         string
	Key         []byte // host: the screening key
	InID        int64  // guest: its link_screenings_in row
	HostID      string // guest: the host's identity
	HostRelay   string
	HostLink    string // guest: what the host Usherr is called here
	HostName    string // the person hosting
	Title       string
	AtMs        int64
	PreshowMin  int
	Runtime     int // minutes (the longest known: TMDB's, the copy here)
	Recipe      string
	AnyonePause bool
	Seq         int64
	State       string
	Pos         float64
	PlayAt      int64
	Heard       int64 // guest: when the host's last state came
}

// preshowSeconds is how long the pre-show runs: the recipe's length when
// it's fixed, else the booked minutes.
func (r screeningRoom) preshowSeconds() float64 {
	if r.Recipe != "" {
		if r.Role == "host" {
			var p preshowPlan
			if json.Unmarshal([]byte(r.Recipe), &p) == nil {
				return p.Seconds
			}
		} else {
			var items []screening.RecipeItem
			if json.Unmarshal([]byte(r.Recipe), &items) == nil {
				return screening.RecipeSeconds(items)
			}
		}
	}
	return float64(r.PreshowMin * 60)
}

type roomNow struct {
	State    string  `json:"state"`    // waiting, preshow, film, paused, ended
	Position float64 `json:"position"` // seconds into the film (negative in the pre-show)
	AtMs     int64   `json:"atMs"`     // when it was there (this Usherr's clock)
	Seq      int64   `json:"seq"`
}

// now is where the film is at now (this Usherr's clock): as the host last
// said, moved on with the clock, or by the schedule when nothing was said.
func (r screeningRoom) now(now int64) roomNow {
	var state string
	var pos float64
	if r.Seq == 0 {
		pos = float64(now-r.AtMs) / 1000
		state = screening.StateFilm
		if pos < 0 {
			state = screening.StatePreshow
		}
	} else {
		state, pos = screening.Project(r.State, r.Pos, r.PlayAt, now)
	}
	if state == screening.StatePreshow && pos < -r.preshowSeconds()-0.5 {
		state = roomWaiting
	}
	if state == screening.StateFilm && r.Runtime > 0 && pos >= float64(r.Runtime*60+30) {
		state, pos = screening.StateEnded, float64(r.Runtime*60+30)
	}
	return roomNow{State: state, Position: math.Round(pos*1000) / 1000, AtMs: now, Seq: r.Seq}
}

// roomOf finds the linked screening a showtime here is.
func (s *Server) roomOf(ctx context.Context, showtimeID int64) (screeningRoom, bool) {
	r := screeningRoom{ShowtimeID: showtimeID}
	var enc string
	var copyMin int
	var anyone int
	err := s.db.QueryRowContext(ctx, `SELECT o.sid, o.skey, o.copy_min, o.recipe, o.anyone_pause, o.play_seq, o.play_state, o.play_pos, o.play_at,
			st.user_id, st.title, st.at_ms, st.preshow_min, st.runtime
		FROM link_screenings_out o JOIN showtimes st ON st.id = o.showtime_id WHERE o.showtime_id = ?`, showtimeID).Scan(
		&r.SID, &enc, &copyMin, &r.Recipe, &anyone, &r.Seq, &r.State, &r.Pos, &r.PlayAt, &r.UserID, &r.Title, &r.AtMs, &r.PreshowMin, &r.Runtime)
	if err == nil {
		key, err := s.openScreeningKey(enc)
		if err != nil {
			return r, false
		}
		r.Role, r.Key, r.AnyonePause = "host", key, anyone == 1
		r.Runtime = max(r.Runtime, copyMin)
		r.HostName = s.partyHost(r.UserID)
		return r, true
	}
	err = s.db.QueryRowContext(ctx, `SELECT i.id, i.sid, p.their_id, p.relay, COALESCE(NULLIF(p.nickname, ''), p.name), i.host_name, i.title, i.at_ms, i.preshow_min,
			i.runtime, i.my_runtime, i.recipe, i.anyone_pause, i.play_seq, i.play_state, i.play_pos, i.play_at, i.play_heard, st.user_id
		FROM link_screenings_in i JOIN link_peers p ON p.id = i.peer_id JOIN showtimes st ON st.id = i.showtime_id
		WHERE i.showtime_id = ? AND p.status = 'linked'`, showtimeID).Scan(
		&r.InID, &r.SID, &r.HostID, &r.HostRelay, &r.HostLink, &r.HostName, &r.Title, &r.AtMs, &r.PreshowMin,
		&r.Runtime, &copyMin, &r.Recipe, &anyone, &r.Seq, &r.State, &r.Pos, &r.PlayAt, &r.Heard, &r.UserID)
	if err != nil {
		return r, false
	}
	r.Role, r.AnyonePause = "guest", anyone == 1
	r.Runtime = max(r.Runtime, copyMin)
	return r, true
}

// hostRoomBySID finds a screening hosted here by its id.
func (s *Server) hostRoomBySID(ctx context.Context, sid string) (screeningRoom, bool) {
	var id int64
	if s.db.QueryRowContext(ctx, `SELECT showtime_id FROM link_screenings_out WHERE sid = ?`, sid).Scan(&id) != nil {
		return screeningRoom{}, false
	}
	return s.roomOf(ctx, id)
}

// relaySkew is how far the relay's clock is ahead of this Usherr's (ms), on
// a relay (the own relay's for "").
func (s *Server) relaySkew(relay string) int64 {
	n := s.linkNode()
	if n == nil {
		return 0
	}
	if relay != "" {
		if st := n.RelayStatus(relay); st.State != "off" {
			return st.SkewMs
		}
	}
	return n.Status().SkewMs
}

// ---- the host ----

// roomSet is the host moving the film: state at position, now. It's kept,
// shown in the browsers here, and sent to everyone at once.
func (s *Server) roomSet(ctx context.Context, r screeningRoom, state string, pos float64) (screeningRoom, error) {
	now := time.Now().UnixMilli()
	seq := max(r.Seq+1, now)
	if _, err := s.db.ExecContext(ctx, `UPDATE link_screenings_out SET play_seq = ?, play_state = ?, play_pos = ?, play_at = ? WHERE showtime_id = ?`,
		seq, state, pos, now, r.ShowtimeID); err != nil {
		return r, err
	}
	r.Seq, r.State, r.Pos, r.PlayAt = seq, state, pos, now
	s.linkS.scr.room.changed(r.ShowtimeID)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		s.roomSend(ctx, r, r.now(time.Now().UnixMilli()))
	}()
	return r, nil
}

// roomSend tells everyone invited where the film is (a change, or a
// heartbeat).
func (s *Server) roomSend(ctx context.Context, r screeningRoom, cur roomNow) {
	n := s.linkNode()
	if n == nil || cur.State == roomWaiting {
		return
	}
	var to []string
	for _, g := range s.screeningGuests(ctx, r.ShowtimeID) {
		to = append(to, g.Their)
	}
	l := &s.linkS.scr
	l.mu.Lock()
	if l.beats == nil {
		l.beats = map[int64]roomBeatSent{}
	}
	l.beats[r.ShowtimeID] = roomBeatSent{at: time.Now(), seq: r.Seq, state: cur.State}
	l.mu.Unlock()
	if len(to) == 0 {
		return
	}
	pos := math.Max(-float64(screening.MaxPreshow*60+60), math.Min(cur.Position, float64(screening.MaxRuntime*60+3600)))
	p := screening.Playback{V: screening.Version, SID: r.SID, Seq: max(r.Seq, 1), State: cur.State, Position: pos,
		AtWallTime: cur.AtMs + s.relaySkew(""), AnyonePause: r.AnyonePause}
	_ = n.SendGroup(ctx, r.Key, to, screening.TypePlayback, p)
}

// roomBeats sends where the film is to the screenings hosted here that are
// on: every few seconds while it plays, less often while it's paused, once
// when it ends.
func (s *Server) roomBeats(ctx context.Context) {
	if s.linkNode() == nil {
		return
	}
	now := time.Now()
	var ids []int64
	rows, err := s.db.QueryContext(ctx, `SELECT o.showtime_id FROM link_screenings_out o JOIN showtimes st ON st.id = o.showtime_id
		WHERE st.state <> 'done' AND st.at_ms BETWEEN ? AND ?`, now.Add(-14*time.Hour).UnixMilli(), now.Add(2*time.Hour).UnixMilli())
	if err != nil {
		return
	}
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	l := &s.linkS.scr
	for _, id := range ids {
		r, ok := s.roomOf(ctx, id)
		if !ok {
			continue
		}
		cur := r.now(now.UnixMilli())
		l.mu.Lock()
		last := l.beats[id]
		l.mu.Unlock()
		var every time.Duration
		switch cur.State {
		case screening.StatePreshow, screening.StateFilm:
			every = roomBeat
		case screening.StatePaused:
			every = roomBeatPaused
		case screening.StateEnded:
			if last.state == screening.StateEnded {
				continue
			}
		default:
			continue
		}
		if last.state == cur.State && last.seq == r.Seq && now.Sub(last.at) < every {
			continue
		}
		if last.state != cur.State && last.state != "" {
			s.linkS.scr.room.changed(id) // the pre-show ran into the film, the film ended
		}
		s.roomSend(ctx, r, cur)
		if cur.State == screening.StatePaused {
			s.interSend(ctx, r, false) // the intermission, while it's on (home_link_party.go)
		}
	}
}

// ---- the pre-show recipe ----

// screeningPlan is the pre-show of a screening hosted here, as it plays here
// and everywhere: fixed the first time it's asked for from shortly before it
// starts (build), and kept.
func (s *Server) screeningPlan(ctx context.Context, showtimeID int64, build bool) (preshowPlan, bool) {
	var raw string
	var at int64
	var pre int
	var theme int64
	var tmdb int
	if s.db.QueryRowContext(ctx, `SELECT o.recipe, st.at_ms, st.preshow_min, st.preshow_theme, st.tmdb_id FROM link_screenings_out o JOIN showtimes st ON st.id = o.showtime_id
		WHERE o.showtime_id = ?`, showtimeID).Scan(&raw, &at, &pre, &theme, &tmdb) != nil || pre <= 0 {
		return preshowPlan{}, false
	}
	if raw != "" {
		var p preshowPlan
		if json.Unmarshal([]byte(raw), &p) == nil {
			return p, true
		}
	}
	now := time.Now()
	if !build || now.Before(time.UnixMilli(at).Add(-time.Duration(pre)*time.Minute-screeningRecipeAhead)) {
		return preshowPlan{}, false
	}
	var libraryID int64
	if m, ok, err := s.MovieRepo.GetByTMDBID(tmdb); err == nil && ok && m.FilePath != "" {
		libraryID = m.ID
	}
	film := s.preshowFilmFor(ctx, "movie", libraryID, tmdb, "")
	film.Showtime, film.AtMs = true, at
	plan := s.planPreshow(ctx, film, theme, float64(pre*60), now)
	b, err := json.Marshal(plan)
	if err != nil {
		return plan, true
	}
	res, err := s.db.ExecContext(ctx, `UPDATE link_screenings_out SET recipe = ? WHERE showtime_id = ? AND recipe = ''`, string(b), showtimeID)
	if n, _ := res.RowsAffected(); err == nil && n == 1 {
		s.recordPlayed(ctx, plan)
		s.linkS.scr.room.changed(showtimeID)
		return plan, true
	}
	// Made at the same moment elsewhere: that one holds.
	return s.screeningPlan(ctx, showtimeID, false)
}

// recipeOf is a plan as the guests get it: what each piece is and how long
// it runs, never a file of the host's.
func recipeOf(plan preshowPlan) []screening.RecipeItem {
	items := []screening.RecipeItem{}
	poster := func(u string) string {
		if strings.HasPrefix(u, "https://image.tmdb.org/") {
			return u
		}
		return ""
	}
	for _, st := range plan.Steps {
		if len(items) == screening.MaxRecipeItems {
			break
		}
		it := screening.RecipeItem{Kind: st.Kind, Seconds: math.Max(0.1, math.Min(math.Round(st.Seconds*10)/10, screening.MaxItemSeconds))}
		switch st.Kind {
		case "card":
			it.Card = st.Card
			switch st.Card {
			case "announce":
				it.Text = st.Title
			case "soon":
				it.TMDB, it.TV, it.Season, it.Title, it.Poster, it.Release, it.Why = st.TMDBID, st.TV, st.Season, st.Title, poster(st.PosterURL), st.ReleaseDate, st.Why
			}
		case "clip":
			it.Category = st.Category
		case "trailer":
			it.TMDB, it.TV, it.Season, it.YouTube, it.Title, it.Poster, it.Release, it.Why = st.TMDBID, st.TV, st.Season, st.YouTube, st.Title, poster(st.PosterURL), st.ReleaseDate, st.Why
		default:
			continue
		}
		items = append(items, it)
	}
	return items
}

// recipeFill builds a pre-show from the host's recipe with what this Usherr
// has: find gives this Usherr's own piece for an item; whatever it can't
// find is a card of the same length (the coming-soon card of a trailer it
// doesn't have, else the welcome card). Every piece runs exactly as long as
// the host's, so the whole is as long too.
func recipeFill(items []screening.RecipeItem, find func(screening.RecipeItem) (preshowStep, bool)) []preshowStep {
	out := make([]preshowStep, 0, len(items))
	for _, it := range items {
		st, ok := find(it)
		if !ok {
			st = preshowStep{Kind: "card", Card: "welcome", Filled: true}
			if it.TMDB > 0 && it.Title != "" && (it.Kind == "trailer" || it.Card == "soon") {
				st = preshowStep{Kind: "card", Card: "soon", Title: it.Title, TMDBID: it.TMDB, TV: it.TV, Season: it.Season, PosterURL: it.Poster, ReleaseDate: it.Release, Why: it.Why, Filled: it.Kind == "trailer"}
			}
		}
		st.Seconds = it.Seconds
		out = append(out, st)
	}
	return out
}

// guestPlan is a guest's pre-show for a screening: the host's recipe, built
// here. Before the recipe comes, one welcome card for the booked minutes.
func (s *Server) guestPlan(ctx context.Context, r screeningRoom) preshowPlan {
	now := time.Now()
	var tmdb int
	_ = s.db.QueryRowContext(ctx, `SELECT tmdb_id FROM link_screenings_in WHERE id = ?`, r.InID).Scan(&tmdb)
	var libraryID int64
	if m, ok, err := s.MovieRepo.GetByTMDBID(tmdb); err == nil && ok && m.FilePath != "" {
		libraryID = m.ID
	}
	film := s.preshowFilmFor(ctx, "movie", libraryID, tmdb, "")
	film.Title = firstNonEmptyStr(film.Title, r.Title)
	film.Showtime, film.AtMs = true, r.AtMs
	set := s.preshowSettingsNow()
	plan := preshowPlan{Theme: s.preshowThemeFor(ctx, 0, now), Film: film, Theater: set.Theater, Holiday: preshowHolidayNow(now)}
	var items []screening.RecipeItem
	if r.Recipe == "" || json.Unmarshal([]byte(r.Recipe), &items) != nil {
		items = nil
		if r.PreshowMin > 0 {
			items = []screening.RecipeItem{{Kind: "card", Card: "welcome", Seconds: float64(r.PreshowMin * 60)}}
		}
	}
	plan.Steps = recipeFill(items, s.recipeFinder(ctx, film, now))
	for _, st := range plan.Steps {
		plan.Seconds += st.Seconds
	}
	return plan
}

// recipeFinder finds this Usherr's own piece for a recipe item.
func (s *Server) recipeFinder(ctx context.Context, film preshowFilm, now time.Time) func(screening.RecipeItem) (preshowStep, bool) {
	var clips []preshowClip
	clipsRead := false
	var trailers []preshowTrailer
	trailersRead := false
	used := map[string]bool{}
	played := s.lastPlayed(ctx)
	return func(it screening.RecipeItem) (preshowStep, bool) {
		switch it.Kind {
		case "card":
			st := preshowStep{Kind: "card", Card: it.Card, Title: film.Title, PosterURL: film.PosterURL, BackdropURL: film.BackdropURL}
			switch it.Card {
			case "rating":
				return st, film.Rated != ""
			case "announce":
				st.Title = it.Text
				return st, it.Text != ""
			case "qr":
				st.Title = s.preshowQRDefault()
				return st, st.Title != ""
			case "reel":
				st.Reel = s.preshowReel(ctx, now)
				return st, len(st.Reel) > 0
			case "soon":
				return preshowStep{Kind: "card", Card: "soon", Title: it.Title, TMDBID: it.TMDB, TV: it.TV, Season: it.Season, PosterURL: it.Poster, ReleaseDate: it.Release, Why: it.Why}, it.Title != ""
			}
			return st, true
		case "clip":
			if !clipsRead {
				clips, _ = s.preshowClips(ctx)
				clipsRead = true
			}
			var pool []preshowClip
			for _, c := range clips {
				if c.Enabled && c.Seconds > 0 && sameName(c.Category, it.Category) && !used["clip:"+strconv.FormatInt(c.ID, 10)] {
					pool = append(pool, c)
				}
			}
			if len(pool) == 0 {
				return preshowStep{}, false
			}
			pool = leastLately(pool, func(c preshowClip) string { return "clip:" + strconv.FormatInt(c.ID, 10) }, played)
			// Of the ones that waited longest, the closest in length.
			best := pool[0]
			for _, c := range pool[:min(len(pool), 5)] {
				if math.Abs(c.Seconds-it.Seconds) < math.Abs(best.Seconds-it.Seconds) {
					best = c
				}
			}
			used["clip:"+strconv.FormatInt(best.ID, 10)] = true
			return preshowStep{Kind: "clip", URL: best.FileURL, Title: best.Title, Category: best.Category}, true
		case "trailer":
			if !trailersRead {
				trailers = s.readyTrailers(ctx, now)
				trailersRead = true
			}
			for _, t := range trailers {
				if t.TMDBID == it.TMDB && (t.Kind == "tv") == it.TV && (!it.TV || t.Season == it.Season) {
					return preshowStep{Kind: "trailer", URL: t.FileURL, YouTube: t.YouTube, Title: t.Title, TMDBID: t.TMDBID, TV: it.TV, Season: t.Season, Why: t.Why,
						PosterURL: firstNonEmptyStr(t.PosterURL, it.Poster), ReleaseDate: t.ReleaseDate}, true
				}
			}
			if it.YouTube != "" {
				return preshowStep{Kind: "trailer", YouTube: it.YouTube, Title: it.Title, TMDBID: it.TMDB, TV: it.TV, Season: it.Season, Why: it.Why, PosterURL: it.Poster, ReleaseDate: it.Release}, true
			}
		}
		return preshowStep{}, false
	}
}

// roomPlan is the pre-show of a linked screening here (the host's fixed
// plan, or a guest's built from the recipe); false for any other showtime.
func (s *Server) roomPlan(ctx context.Context, showtimeID int64) (preshowPlan, bool) {
	r, ok := s.roomOf(ctx, showtimeID)
	if !ok {
		return preshowPlan{}, false
	}
	if r.Role == "host" {
		if r.PreshowMin <= 0 {
			return preshowPlan{}, false
		}
		return s.screeningPlan(ctx, showtimeID, true)
	}
	return s.guestPlan(ctx, r), true
}

// screeningRecipes fixes the pre-shows of screenings hosted here that start
// soon, and sends their recipes to everyone invited (every minute until
// the film starts, so an Usherr that was offline gets it too).
func (s *Server) screeningRecipes(ctx context.Context) {
	n := s.linkNode()
	if n == nil {
		return
	}
	now := time.Now().UnixMilli()
	var ids []int64
	rows, err := s.db.QueryContext(ctx, `SELECT o.showtime_id FROM link_screenings_out o JOIN showtimes st ON st.id = o.showtime_id
		WHERE st.state <> 'done' AND st.preshow_min > 0 AND st.at_ms > ? AND st.at_ms - st.preshow_min * 60000 - ? <= ?`, now, screeningRecipeAhead.Milliseconds(), now)
	if err != nil {
		return
	}
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		_ = s.sendRecipe(ctx, id)
	}
}

func (s *Server) sendRecipe(ctx context.Context, showtimeID int64) error {
	n := s.linkNode()
	plan, ok := s.screeningPlan(ctx, showtimeID, true)
	o, ok2 := s.screeningOutOf(ctx, showtimeID)
	if n == nil || !ok || !ok2 {
		return nil
	}
	var to []string
	for _, g := range s.screeningGuests(ctx, showtimeID) {
		to = append(to, g.Their)
	}
	if len(to) == 0 {
		return nil
	}
	return n.SendGroup(ctx, o.Key, to, screening.TypeRecipe, screening.Recipe{V: screening.Version, SID: o.SID, Rev: o.Rev, Items: recipeOf(plan)})
}

// ---- what the messages do (screening.Room) ----

// PlaybackIn: the host says where the film is. A newer word than the last
// is kept (heartbeats repeat the same one), its time put on this Usherr's
// clock.
func (b linkBook) PlaybackIn(ctx context.Context, host link.Peer, p screening.Playback) error {
	s := b.s
	now := time.Now().UnixMilli()
	at := p.AtWallTime - s.relaySkew(host.Relay)
	anyone := 0
	if p.AnyonePause {
		anyone = 1
	}
	res, err := s.db.ExecContext(ctx, `UPDATE link_screenings_in SET play_seq = ?, play_state = ?, play_pos = ?, play_at = ?, play_heard = ?, anyone_pause = ?
		WHERE sid = ? AND play_seq <= ?`, p.Seq, p.State, p.Position, at, now, anyone, p.SID, p.Seq)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		var id sql.NullInt64
		_ = s.db.QueryRowContext(ctx, `SELECT showtime_id FROM link_screenings_in WHERE sid = ?`, p.SID).Scan(&id)
		if id.Valid {
			s.linkS.scr.room.changed(id.Int64)
		}
	}
	return nil
}

// RecipeIn: the host's pre-show, to build here.
func (b linkBook) RecipeIn(ctx context.Context, host link.Peer, r screening.Recipe) error {
	s := b.s
	raw, _ := json.Marshal(r.Items)
	var id sql.NullInt64
	var rev int
	if err := s.db.QueryRowContext(ctx, `SELECT showtime_id, rev FROM link_screenings_in WHERE sid = ?`, r.SID).Scan(&id, &rev); err != nil {
		return err
	}
	if r.Rev < rev {
		return nil // made before a change this Usherr has already heard of
	}
	res, err := s.db.ExecContext(ctx, `UPDATE link_screenings_in SET recipe = ? WHERE sid = ? AND recipe <> ?`, string(raw), r.SID, string(raw))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 && id.Valid {
		s.linkS.scr.room.changed(id.Int64)
	}
	return nil
}

var errNoPausing = errors.New("only the host pauses this screening")

// PauseIn: a guest asks to pause; honoured when the host lets anyone pause
// and the film is playing.
func (b linkBook) PauseIn(ctx context.Context, guest link.Peer, sid string) error {
	s := b.s
	r, ok := s.hostRoomBySID(ctx, sid)
	if !ok {
		return screening.ErrNotAllowed
	}
	if !r.AnyonePause {
		return errNoPausing
	}
	cur := r.now(time.Now().UnixMilli())
	if cur.State != screening.StateFilm {
		return nil
	}
	_, err := s.roomSet(ctx, r, screening.StatePaused, cur.Position)
	return err
}

// ---- guests: the booking ----

// screeningBook keeps a guest's booking in step with its people: booked in
// this Usherr's Showtime while anyone here says they're in (moved when the
// host moves it), gone when nobody is.
func (s *Server) screeningBook(ctx context.Context, id int64) {
	var booked sql.NullInt64
	var tmdb, year, runtime, pre int
	var title, poster string
	var at int64
	if s.db.QueryRowContext(ctx, `SELECT showtime_id, tmdb_id, title, year, poster_url, runtime, at_ms, preshow_min FROM link_screenings_in WHERE id = ?`, id).Scan(
		&booked, &tmdb, &title, &year, &poster, &runtime, &at, &pre) != nil {
		return
	}
	exists := false
	if booked.Valid {
		var one int
		exists = s.db.QueryRowContext(ctx, `SELECT 1 FROM showtimes WHERE id = ?`, booked.Int64).Scan(&one) == nil
	}
	answer, _ := s.household(ctx, id)
	switch {
	case answer != screening.Going:
		if exists {
			_, _ = s.db.ExecContext(ctx, `DELETE FROM showtimes WHERE id = ?`, booked.Int64)
		}
		if booked.Valid {
			_, _ = s.db.ExecContext(ctx, `UPDATE link_screenings_in SET showtime_id = NULL WHERE id = ?`, id)
		}
		if !exists {
			return
		}
	case !exists:
		var user int64
		if s.db.QueryRowContext(ctx, `SELECT user_id FROM link_screening_people WHERE screening_id = ? AND answer = ? ORDER BY at_ms LIMIT 1`, id, screening.Going).Scan(&user) != nil {
			return
		}
		res, err := s.db.ExecContext(ctx, `INSERT INTO showtimes (user_id, tmdb_id, title, year, poster_url, runtime, at_ms, preshow_min, play_on, created_ms)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'here', ?)`, user, tmdb, title, year, poster, runtime, at, pre, time.Now().UnixMilli())
		if err != nil {
			return
		}
		stID, _ := res.LastInsertId()
		_, _ = s.db.ExecContext(ctx, `UPDATE link_screenings_in SET showtime_id = ? WHERE id = ?`, stID, id)
		go s.artForFilm(tmdb)
	default:
		// A new time starts it over.
		_, _ = s.db.ExecContext(ctx, `UPDATE showtimes SET state = CASE WHEN at_ms <> ? OR preshow_min <> ? THEN 'booked' ELSE state END,
			title = ?, year = ?, poster_url = ?, runtime = ?, at_ms = ?, preshow_min = ? WHERE id = ?`,
			at, pre, title, year, poster, runtime, at, pre, booked.Int64)
		s.linkS.scr.room.changed(booked.Int64)
	}
	s.marqueeLive.send("", marqueeEvent{Type: "settings"})
}

// screeningUnbook takes a guest's booking away (the screening was called
// off, or the link ended).
func (s *Server) screeningUnbook(ctx context.Context, where string, arg any) {
	_, _ = s.db.ExecContext(ctx, `DELETE FROM showtimes WHERE id IN (SELECT showtime_id FROM link_screenings_in WHERE showtime_id IS NOT NULL AND `+where+`)`, arg)
	s.marqueeLive.send("", marqueeEvent{Type: "settings"})
}

type hostedInfo struct {
	Host      string `json:"host"`               // the linked Usherr hosting it
	HostName  string `json:"hostName,omitempty"` // the person
	Screening int64  `json:"screening"`          // its id under Linked screenings
}

// withHosted marks the bookings that are a linked Usherr's screenings.
func (s *Server) withHosted(ctx context.Context, list []showtime) {
	if len(list) == 0 {
		return
	}
	got := map[int64]*hostedInfo{}
	rows, err := s.db.QueryContext(ctx, `SELECT i.showtime_id, i.id, COALESCE(NULLIF(p.nickname, ''), p.name), i.host_name FROM link_screenings_in i
		JOIN link_peers p ON p.id = i.peer_id WHERE i.showtime_id IS NOT NULL`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		h := &hostedInfo{}
		if rows.Scan(&id, &h.Screening, &h.Host, &h.HostName) == nil {
			got[id] = h
		}
	}
	for i := range list {
		list[i].Hosted = got[list[i].ID]
	}
}

// hostedBy is the linked Usherr hosting a booking here ("" when it's this
// Usherr's own).
func (s *Server) hostedBy(ctx context.Context, showtimeID int64) string {
	var name string
	_ = s.db.QueryRowContext(ctx, `SELECT COALESCE(NULLIF(p.nickname, ''), p.name) FROM link_screenings_in i JOIN link_peers p ON p.id = i.peer_id WHERE i.showtime_id = ?`, showtimeID).Scan(&name)
	return name
}

// ---- the host's household ----

// hostHousehold is who in the host's household is coming: whoever booked
// it unless they said otherwise, and everyone who said I'm in.
func (s *Server) hostHousehold(ctx context.Context, st showtime) map[int64]string {
	answers := map[int64]string{st.UserID: screening.Going}
	rows, err := s.db.QueryContext(ctx, `SELECT user_id, answer FROM link_screening_host_people WHERE showtime_id = ?`, st.ID)
	if err != nil {
		return answers
	}
	defer rows.Close()
	for rows.Next() {
		var u int64
		var a string
		if rows.Scan(&u, &a) == nil {
			answers[u] = a
		}
	}
	return answers
}

func goingOf(answers map[int64]string) int {
	n := 0
	for _, a := range answers {
		if a == screening.Going {
			n++
		}
	}
	return n
}

// PUT /api/home/showtimes/{id}/linked/rsvp {"answer": "going"|"not"}: someone
// in the host's household says whether they're coming to a shared showtime.
func (s *Server) handleShowtimeHostRSVP(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	var body struct {
		Answer string `json:"answer"`
	}
	if !ok || decodeJSON(r, &body) != nil || (body.Answer != screening.Going && body.Answer != screening.NotGoing) {
		writeError(w, http.StatusBadRequest, "Say I'm in or Can't make it.")
		return
	}
	ctx := r.Context()
	st, found := s.showtimeByID(ctx, id)
	if _, shared := s.screeningOutOf(ctx, id); !found || !shared {
		writeError(w, http.StatusNotFound, "That showtime isn't shared with linked Usherrs.")
		return
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO link_screening_host_people (showtime_id, user_id, answer, at_ms) VALUES (?, ?, ?, ?)
		ON CONFLICT (showtime_id, user_id) DO UPDATE SET answer = excluded.answer, at_ms = excluded.at_ms`, id, requestUserID(r), body.Answer, time.Now().UnixMilli()); err != nil {
		writeError(w, http.StatusInternalServerError, "Your answer couldn't be saved.")
		return
	}
	s.screeningRosterSoon(id)
	go s.rewardLate(id) // step 5: a stub for someone who comes once it started
	answers := s.hostHousehold(ctx, st)
	writeJSON(w, http.StatusOK, map[string]any{"mine": answers[requestUserID(r)], "hostGoing": goingOf(answers)})
}

// ---- the room's pages ----

type roomOut struct {
	Role           string  `json:"role"` // host, guest
	ShowtimeID     int64   `json:"showtimeId"`
	Title          string  `json:"title"`
	AtMs           int64   `json:"atMs"`
	PreshowSeconds float64 `json:"preshowSeconds"`
	Runtime        int     `json:"runtime,omitempty"`
	Now            roomNow `json:"now"`
	OpenMs         int64   `json:"openMs"` // "Join the screening" from then
	AnyonePause    bool    `json:"anyonePause"`
	CanControl     bool    `json:"canControl"` // moves the film for everyone
	CanPause       bool    `json:"canPause"`   // may pause (or ask the host to)
	Host           string  `json:"host"`       // the Usherr hosting
	HostName       string  `json:"hostName,omitempty"`
	Following      int     `json:"following"` // host: linked Usherrs at the screening
	Relay          string  `json:"relay"`     // the relay connection: connected, connecting…
	HeardMs        int64   `json:"heardMs,omitempty"`
	NowMs          int64   `json:"nowMs"`
}

func (s *Server) roomAnswer(ctx context.Context, r screeningRoom, req *http.Request) roomOut {
	now := time.Now().UnixMilli()
	pre := r.preshowSeconds()
	out := roomOut{Role: r.Role, ShowtimeID: r.ShowtimeID, Title: r.Title, AtMs: r.AtMs, PreshowSeconds: pre, Runtime: r.Runtime, Now: r.now(now),
		OpenMs: r.AtMs - int64(pre*1000) - roomOpenBefore.Milliseconds(), AnyonePause: r.AnyonePause, HostName: r.HostName, Relay: "off", NowMs: now}
	if n := s.linkNode(); n != nil {
		out.Relay = n.Status().State
	}
	if r.Role == "host" {
		out.Host = s.linkName()
		out.CanControl = s.canControlRoom(req, r)
		out.Following = len(s.screeningGuests(ctx, r.ShowtimeID))
	} else {
		out.Host, out.HeardMs = r.HostLink, r.Heard
	}
	out.CanPause = out.CanControl || r.AnyonePause
	return out
}

// canControlRoom: whoever booked a screening hosted here, or an
// administrator, moves the film for everyone.
func (s *Server) canControlRoom(req *http.Request, r screeningRoom) bool {
	if r.Role != "host" {
		return false
	}
	u := auth.UserFromContext(req.Context())
	return r.UserID == requestUserID(req) || (u != nil && u.IsAdmin)
}

func (s *Server) roomFor(w http.ResponseWriter, r *http.Request) (screeningRoom, bool) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "That showtime isn't known.")
		return screeningRoom{}, false
	}
	room, ok := s.roomOf(r.Context(), id)
	if !ok {
		writeError(w, http.StatusNotFound, "That showtime isn't a linked screening.")
		return room, false
	}
	return room, true
}

// GET /api/home/showtimes/{id}/room: a linked screening's room: where the
// film is (this Usherr's clock), who controls it, and the relay.
func (s *Server) handleRoom(w http.ResponseWriter, r *http.Request) {
	room, ok := s.roomFor(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, s.roomAnswer(r.Context(), room, r))
}

// GET /api/home/showtimes/{id}/room/events: "changed" whenever the host
// moves the film (the page asks for the room again).
func (s *Server) handleRoomEvents(w http.ResponseWriter, r *http.Request) {
	room, ok := s.roomFor(w, r)
	if !ok {
		return
	}
	s.roomEventStream(w, r, room.ShowtimeID) // home_link_party.go
}

// POST /api/home/showtimes/{id}/room/playback {"state": "film"|"paused"|"ended", "position"?}:
// the host plays, pauses or seeks, for everyone. Without a position, from
// where it is now.
func (s *Server) handleRoomPlayback(w http.ResponseWriter, r *http.Request) {
	room, ok := s.roomFor(w, r)
	if !ok {
		return
	}
	var body struct {
		State    string   `json:"state"`
		Position *float64 `json:"position"`
	}
	if decodeJSON(r, &body) != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	if !s.canControlRoom(r, room) {
		if room.Role == "guest" {
			writeError(w, http.StatusForbidden, "The host runs the film. You're following along.")
		} else {
			writeError(w, http.StatusForbidden, "Whoever booked it (or an administrator) runs the film.")
		}
		return
	}
	if body.State != screening.StateFilm && body.State != screening.StatePaused && body.State != screening.StateEnded {
		writeError(w, http.StatusBadRequest, "Say play, pause or end.")
		return
	}
	cur := room.now(time.Now().UnixMilli())
	pos := cur.Position
	if body.Position != nil {
		pos = *body.Position
	}
	if math.IsNaN(pos) || math.IsInf(pos, 0) {
		writeError(w, http.StatusBadRequest, "That position couldn't be read.")
		return
	}
	end := float64(screening.MaxRuntime * 60)
	if room.Runtime > 0 {
		end = float64(room.Runtime*60 + 30)
	}
	pos = math.Max(0, math.Min(pos, end)) // the host's controls are the film's; skipping the pre-show is a play from 0
	if body.State == screening.StatePaused && cur.State == screening.StatePreshow && body.Position == nil {
		writeError(w, http.StatusConflict, "The pre-show can't be paused. Skip to the film, or let it run.")
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 20*time.Second)
	defer cancel()
	room, err := s.roomSet(ctx, room, body.State, pos)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "That couldn't be saved.")
		return
	}
	writeJSON(w, http.StatusOK, s.roomAnswer(ctx, room, r))
}

// POST /api/home/showtimes/{id}/room/pause: pause, when the host lets
// anyone: at the host, it pauses; at a guest, the host is asked.
func (s *Server) handleRoomPause(w http.ResponseWriter, r *http.Request) {
	room, ok := s.roomFor(w, r)
	if !ok {
		return
	}
	if !room.AnyonePause && !s.canControlRoom(r, room) {
		writeError(w, http.StatusForbidden, "Only the host pauses this screening.")
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 20*time.Second)
	defer cancel()
	if room.Role == "host" {
		cur := room.now(time.Now().UnixMilli())
		if cur.State != screening.StateFilm {
			writeJSON(w, http.StatusOK, s.roomAnswer(ctx, room, r))
			return
		}
		room, err := s.roomSet(ctx, room, screening.StatePaused, cur.Position)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "That couldn't be saved.")
			return
		}
		writeJSON(w, http.StatusOK, s.roomAnswer(ctx, room, r))
		return
	}
	n := s.linkNode()
	if n == nil {
		writeError(w, http.StatusConflict, linkOffText)
		return
	}
	if err := n.Send(ctx, room.HostID, screening.TypePause, screening.PauseRequest{V: screening.Version, SID: room.SID}); err != nil {
		writeError(w, http.StatusBadGateway, "The host couldn't be asked right now. Try again in a moment.")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"asked": true})
}

// PUT /api/home/showtimes/{id}/room/settings {"anyonePause": bool}: the host
// lets guests pause (they ask; the host's Usherr pauses for everyone).
func (s *Server) handleRoomSettings(w http.ResponseWriter, r *http.Request) {
	room, ok := s.roomFor(w, r)
	if !ok {
		return
	}
	if !s.canControlRoom(r, room) {
		writeError(w, http.StatusForbidden, "Whoever booked it (or an administrator) sets this.")
		return
	}
	var body struct {
		AnyonePause bool `json:"anyonePause"`
	}
	if decodeJSON(r, &body) != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	v := 0
	if body.AnyonePause {
		v = 1
	}
	ctx := r.Context()
	if _, err := s.db.ExecContext(ctx, `UPDATE link_screenings_out SET anyone_pause = ? WHERE showtime_id = ?`, v, room.ShowtimeID); err != nil {
		writeError(w, http.StatusInternalServerError, "That couldn't be saved.")
		return
	}
	room.AnyonePause = body.AnyonePause
	s.linkS.scr.room.changed(room.ShowtimeID)
	// The guests hear it with where the film is, now.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		s.roomSend(ctx, room, room.now(time.Now().UnixMilli()))
	}()
	writeJSON(w, http.StatusOK, s.roomAnswer(ctx, room, r))
}
