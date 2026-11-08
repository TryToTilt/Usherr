package api

// Usherr: rewards. Collectible ticket stubs (the film's poster from the
// library, your stars on the stamp, a picture of the stub made in the
// browser) and badges: earned from each person's own stubs, here and from
// linked Usherrs' parties, shown in a cabinet under You, Ticket stubs. The
// badges are worked out here each time, from the stubs; nothing is posted
// anywhere.

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/link/crew"
	"github.com/rdborg/mediarium/internal/metadata"
)

// stubsDress finishes a person's stubs for the page: one per party (a
// party's stub kept both by the bot and here shows once, the stamped one
// first), the library's poster when the film is there, their stars, and the
// name of the linked Usherr a stub came from.
func (s *Server) stubsDress(ctx context.Context, userID int64, list []stub) []stub {
	out := make([]stub, 0, len(list))
	seen := map[int64]int{}
	for _, sb := range list {
		if i, dup := seen[sb.ShowtimeID]; dup && sb.ShowtimeID > 0 {
			if sb.Stamped && !out[i].Stamped {
				out[i] = sb
			}
			continue
		}
		seen[sb.ShowtimeID] = len(out)
		out = append(out, sb)
	}
	posters := map[int]string{}
	names := map[string]string{}
	for i := range out {
		sb := &out[i]
		if p, ok := posters[sb.TMDBID]; ok {
			if p != "" {
				sb.Poster = p
			}
		} else if m, ok, err := s.MovieRepo.GetByTMDBID(sb.TMDBID); err == nil && ok && m.PosterPath != "" {
			posters[sb.TMDBID] = metadata.PosterURL(m.PosterPath)
			sb.Poster = posters[sb.TMDBID]
		} else {
			posters[sb.TMDBID] = ""
		}
		sb.Stars = s.myStars(ctx, userID, sb.TMDBID)
		if sb.Linked != "" {
			if _, ok := names[sb.Linked]; !ok {
				names[sb.Linked] = s.linkedNameOf(ctx, sb.Linked)
			}
			sb.Linked = names[sb.Linked]
		}
	}
	return out
}

// myStars are someone's stars for a film (1 to 10), from their Diary or
// their review; 0 when they didn't rate it.
func (s *Server) myStars(ctx context.Context, userID int64, tmdb int) int {
	var stars int
	_ = s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(stars), 0) FROM (
		SELECT stars FROM home_diary WHERE user_id = ? AND kind = 'movie' AND tmdb_id = ? AND stars > 0 AND hidden = 0
		UNION ALL SELECT stars FROM community_reviews WHERE user_id = ? AND kind = 'movie' AND tmdb_id = ? AND stars > 0)`, userID, tmdb, userID, tmdb).Scan(&stars)
	return min(max(stars, 0), 10)
}

// linkedNameOf is what a linked Usherr is called here ("A linked Usherr"
// once unlinked).
func (s *Server) linkedNameOf(ctx context.Context, their string) string {
	var name string
	if s.db.QueryRowContext(ctx, `SELECT COALESCE(NULLIF(nickname, ''), name) FROM link_peers WHERE their_id = ? AND their_id <> ''`, their).Scan(&name) != nil || name == "" {
		return "A linked Usherr"
	}
	return name
}

// GET /api/home/stubs/{from}/{id}/poster (member): the poster on one of
// your stubs, from this server, so the browser can draw it into a picture
// of the stub. The library's poster when the film is there.
func (s *Server) handleStubPoster(w http.ResponseWriter, r *http.Request) {
	backend, ext, id, ok := s.myStub(w, r)
	if !ok {
		return
	}
	var tmdb int
	var poster string
	if s.db.QueryRowContext(r.Context(), `SELECT tmdb_id, poster FROM community_stubs WHERE backend = ? AND external_id = ? AND showtime_id = ?`, backend, ext, id).Scan(&tmdb, &poster) != nil {
		writeError(w, http.StatusNotFound, "That ticket stub isn't known.")
		return
	}
	if m, ok, err := s.MovieRepo.GetByTMDBID(tmdb); err == nil && ok && m.PosterPath != "" {
		poster = metadata.PosterURL(m.PosterPath)
	}
	if strings.HasPrefix(poster, "/api/") && !strings.HasPrefix(poster, "//") {
		http.Redirect(w, r, poster, http.StatusFound) // a poster this server keeps
		return
	}
	if !artHostOK(poster) {
		http.NotFound(w, r)
		return
	}
	data, typ, err := artFetch(r.Context(), poster, "poster")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", typ)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	_, _ = w.Write(data)
}

// ---- badges ----

// badgeNeeds are the parties each badge takes.
var badgeNeeds = map[string]int{"first": 1, "five": 5, "regular": 10, "season": 25, "host": 5, "perfect": perfectMin, "nightowl": 1, "double": 2}

// The badges worked out from stubs, in the cabinet's order. The reaction,
// vote and star badges after them are worked out by moreBadges.
var badgeOrder = []string{"first", "five", "regular", "season", "perfect", "nightowl", "host", "double"}

const (
	perfectMin   = 3                 // parties in a month, at least, for Perfect attendance
	screamMin    = 3                 // EEKs in one screening, at least, for Loudest scream
	clownMin     = 10                // HAs in a month, at least, for Class clown
	tasteMin     = 3                 // vote wins for Good taste
	generousMin  = 10                // five-star ratings for Generous
	nightOwlFrom = 2                 // a party that ended at or after 2 AM...
	nightOwlTo   = 6                 // ... and before 6 AM
	nightTurn    = 6 * time.Hour     // a night runs from 6 AM to 6 AM (two films after midnight are one night)
	defaultFilm  = 120 * time.Minute // a film's length when it isn't known
)

type badgeOut struct {
	ID     string `json:"id"`
	Earned int64  `json:"earned,omitempty"` // when (unix ms); 0: not yet
	Have   int    `json:"have"`             // progress
	Need   int    `json:"need"`
}

// badgeStub is what the badges read of one stub.
type badgeStub struct {
	Showtime int64
	TMDB     int
	At       int64
	Runtime  int
	Hosted   bool
}

// earnBadges works out a person's badges from their stubs (any order) and
// the parties they were invited to (showtime -> when). now decides which
// months are over.
func earnBadges(stubs []badgeStub, invites map[int64]int64, now time.Time) []badgeOut {
	sort.Slice(stubs, func(i, j int) bool { return stubs[i].At < stubs[j].At })
	got := map[string]*badgeOut{}
	for _, id := range badgeOrder {
		got[id] = &badgeOut{ID: id, Need: max(badgeNeeds[id], 1)}
	}
	count := func(id string, n int, at int64) {
		b := got[id]
		if b.Have < b.Need {
			b.Have = n
			if n >= b.Need {
				b.Earned = at
			}
		}
	}
	hosted := 0
	nights := map[string]map[int]bool{} // night -> films
	for i, sb := range stubs {
		for _, id := range []string{"first", "five", "regular", "season"} {
			count(id, i+1, sb.At)
		}
		if sb.Hosted {
			hosted++
			count("host", hosted, sb.At)
		}
		run := defaultFilm
		if sb.Runtime > 0 {
			run = time.Duration(sb.Runtime) * time.Minute
		}
		end := time.UnixMilli(sb.At).Add(run).Local()
		if h := end.Hour(); h >= nightOwlFrom && h < nightOwlTo && got["nightowl"].Earned == 0 {
			got["nightowl"].Have, got["nightowl"].Earned = 1, end.UnixMilli()
		}
		night := time.UnixMilli(sb.At).Local().Add(-nightTurn).Format("2006-01-02")
		if nights[night] == nil {
			nights[night] = map[int]bool{}
		}
		nights[night][sb.TMDB] = true
		if d := got["double"]; d.Earned == 0 {
			d.Have = max(d.Have, len(nights[night]))
			if len(nights[night]) >= 2 {
				d.Have, d.Earned = 2, sb.At
			}
		}
	}

	// Perfect attendance: a month that's over, with at least three parties
	// you were invited to, and a stub from every one.
	went := map[int64]bool{}
	for _, sb := range stubs {
		went[sb.Showtime] = true
	}
	type month struct {
		invited, came int
		last          int64
	}
	months := map[string]*month{}
	for st, at := range invites {
		key := time.UnixMilli(at).Local().Format("2006-01")
		m := months[key]
		if m == nil {
			m = &month{}
			months[key] = m
		}
		m.invited++
		if went[st] {
			m.came++
		}
		m.last = max(m.last, at)
	}
	thisMonth := now.Local().Format("2006-01")
	keys := make([]string, 0, len(months))
	for k := range months {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	p := got["perfect"]
	for _, k := range keys {
		m := months[k]
		if k >= thisMonth {
			continue
		}
		if m.invited >= perfectMin && m.came == m.invited {
			p.Have, p.Earned = p.Need, m.last
			break
		}
		if m.invited == m.came {
			p.Have = max(p.Have, min(m.came, p.Need-1))
		}
	}
	out := make([]badgeOut, 0, len(badgeOrder))
	for _, id := range badgeOrder {
		out = append(out, *got[id])
	}
	return out
}

// GET /api/home/party-badges (member): your badges, from your ticket stubs
// (the bot's, through your linked Discord account, and the ones here,
// including linked Usherrs' parties).
func (s *Server) handlePartyBadges(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		writeError(w, http.StatusUnauthorized, "Sign in first.")
		return
	}
	ctx := r.Context()
	who := [][2]string{{"here", strconv.FormatInt(u.ID, 10)}}
	if ext := s.myDiscordID(ctx, u.ID); ext != "" {
		who = append(who, [2]string{"discord", ext})
	}
	var stubs []badgeStub
	seen := map[int64]bool{}
	invites := map[int64]int64{}
	for _, p := range who {
		rows, err := s.db.QueryContext(ctx, `SELECT showtime_id, tmdb_id, at_ms, runtime, hosted FROM community_stubs WHERE backend = ? AND external_id = ? AND kind = 'movie'`, p[0], p[1])
		if err == nil {
			for rows.Next() {
				var b badgeStub
				if rows.Scan(&b.Showtime, &b.TMDB, &b.At, &b.Runtime, &b.Hosted) == nil {
					if b.Showtime > 0 && seen[b.Showtime] {
						continue
					}
					seen[b.Showtime] = true
					stubs = append(stubs, b)
					invites[b.Showtime] = b.At
				}
			}
			rows.Close()
		}
		rows, err = s.db.QueryContext(ctx, `SELECT showtime_id, at_ms FROM reward_invites WHERE person = ?`, p[0]+":"+p[1])
		if err == nil {
			for rows.Next() {
				var id, at int64
				if rows.Scan(&id, &at) == nil {
					invites[id] = at
				}
			}
			rows.Close()
		}
	}
	now := time.Now()
	badges := append(earnBadges(stubs, invites, now), s.moreBadges(ctx, u.ID, now)...)
	writeJSON(w, http.StatusOK, map[string]any{"badges": badges, "parties": len(stubs)})
}

// ---- reaction, vote and star badges ----

// badgeTally is how many of something one person did in one group (a
// screening, or a month).
type badgeTally struct {
	Group string
	Who   string // "u<id>" for a person here; the house and name of anyone else
	N     int
	At    int64 // their latest
}

// mostOfBadge: earned in the first group that's over (done) where me did it
// at least need times and nobody did it more (a tie counts).
func mostOfBadge(id string, tallies []badgeTally, me string, need int, done func(group string) bool) badgeOut {
	type group struct {
		mine, most int
		at         int64
	}
	groups := map[string]*group{}
	for _, t := range tallies {
		g := groups[t.Group]
		if g == nil {
			g = &group{}
			groups[t.Group] = g
		}
		if t.Who == me {
			g.mine, g.at = t.N, t.At
		} else {
			g.most = max(g.most, t.N)
		}
	}
	b := badgeOut{ID: id, Need: need}
	for name, g := range groups {
		if g.mine == 0 {
			continue
		}
		if g.mine >= need && g.mine >= g.most && done(name) {
			if b.Earned == 0 || g.at < b.Earned {
				b.Earned = g.at
			}
			continue
		}
		b.Have = max(b.Have, min(g.mine, need-1))
	}
	if b.Earned > 0 {
		b.Have = need
	}
	return b
}

// countBadge: earned at the need-th of times (unix ms, any order).
func countBadge(id string, times []int64, need int) badgeOut {
	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
	b := badgeOut{ID: id, Need: need, Have: min(len(times), need)}
	if len(times) >= need {
		b.Earned = times[need-1]
	}
	return b
}

// moreBadges are a person's reaction, vote and star badges, worked out from
// what this Usherr keeps: Loudest scream and Class clown from the
// reactions they sent from here (a reaction keeps who sent it, by their
// account here; anyone else by house and name), Good taste from their
// crews' votes (a voter is their house and their name in the crew),
// Generous from their Diary and reviews.
func (s *Server) moreBadges(ctx context.Context, uid int64, now time.Time) []badgeOut {
	me := "u" + strconv.FormatInt(uid, 10)
	tallies := func(word, group string) []badgeTally {
		var out []badgeTally
		rows, err := s.db.QueryContext(ctx, `SELECT `+group+`, CASE WHEN user_id > 0 THEN 'u' || user_id ELSE 'n' || house || '/' || name END, COUNT(*), MAX(at_ms)
			FROM link_reactions WHERE word = ? AND pos >= 0 GROUP BY 1, 2`, word)
		if err != nil {
			return out
		}
		defer rows.Close()
		for rows.Next() {
			var t badgeTally
			if rows.Scan(&t.Group, &t.Who, &t.N, &t.At) == nil {
				out = append(out, t)
			}
		}
		return out
	}
	scream := mostOfBadge("scream", tallies("EEK", "sid"), me, screamMin, func(string) bool { return true })
	// Months by this Usherr's clock; only a month that's over counts.
	thisMonth := now.Local().Format("2006-01")
	clown := mostOfBadge("clown", tallies("HA", `strftime('%Y-%m', at_ms / 1000, 'unixepoch', 'localtime')`), me, clownMin, func(m string) bool { return m < thisMonth })
	return []badgeOut{scream, clown, countBadge("taste", s.tasteWins(ctx, uid), tasteMin), countBadge("generous", s.fiveStars(ctx, uid), generousMin)}
}

// tasteWins are when a film someone picked won one of their crews' votes:
// they voted for the winner, or it was their pick for a club night.
func (s *Server) tasteWins(ctx context.Context, uid int64) []int64 {
	var out []int64
	rows, err := s.db.QueryContext(ctx, `SELECT c.id, c.my_hid FROM crews c JOIN crew_people p ON p.crew_id = c.id WHERE p.user_id = ? AND c.state = 'joined'`, uid)
	if err != nil {
		return out
	}
	type mine struct {
		crew int64
		hid  int
	}
	var crews []mine
	for rows.Next() {
		var m mine
		if rows.Scan(&m.crew, &m.hid) == nil {
			crews = append(crews, m)
		}
	}
	rows.Close()
	name := s.crewPersonName(ctx, uid)
	for _, m := range crews {
		rows, err := s.db.QueryContext(ctx, `SELECT host_hid, host_name, club_kid, winner_tmdb, choices, closes_ms FROM crew_votes
			WHERE crew_id = ? AND winner_tmdb > 0 AND state IN ('closed', 'booked')`, m.crew)
		if err != nil {
			continue
		}
		for rows.Next() {
			var hostH, winner int
			var host, club, raw string
			var at int64
			if rows.Scan(&hostH, &host, &club, &winner, &raw, &at) != nil {
				continue
			}
			var choices []crew.Choice
			_ = json.Unmarshal([]byte(raw), &choices)
			for _, seat := range crewVoteWinners(hostH, host, club, winner, choices) {
				if seat.H == m.hid && seat.Who == name {
					out = append(out, at)
					break
				}
			}
		}
		rows.Close()
	}
	return out
}

// fiveStars are when someone gave a film or show five stars (10 of 10), in
// their Diary or a review: once per title, the first time.
func (s *Server) fiveStars(ctx context.Context, uid int64) []int64 {
	first := map[string]int64{}
	note := func(kind string, tmdb int, at int64) {
		k := kind + "/" + strconv.Itoa(tmdb)
		if old, ok := first[k]; !ok || at < old {
			first[k] = at
		}
	}
	if rows, err := s.db.QueryContext(ctx, `SELECT kind, tmdb_id, MIN(watched_on) FROM home_diary WHERE user_id = ? AND stars >= 10 AND hidden = 0 GROUP BY kind, tmdb_id`, uid); err == nil {
		for rows.Next() {
			var kind, day string
			var tmdb int
			if rows.Scan(&kind, &tmdb, &day) != nil {
				continue
			}
			if d, err := time.ParseInLocation("2006-01-02", day, time.Local); err == nil {
				note(kind, tmdb, d.Add(12*time.Hour).UnixMilli())
			}
		}
		rows.Close()
	}
	if rows, err := s.db.QueryContext(ctx, `SELECT kind, tmdb_id, updated_ms FROM community_reviews WHERE user_id = ? AND stars >= 10`, uid); err == nil {
		for rows.Next() {
			var kind string
			var tmdb int
			var at int64
			if rows.Scan(&kind, &tmdb, &at) == nil {
				note(kind, tmdb, at)
			}
		}
		rows.Close()
	}
	out := make([]int64, 0, len(first))
	for _, at := range first {
		out = append(out, at)
	}
	return out
}
