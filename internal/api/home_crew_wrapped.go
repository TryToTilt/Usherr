package api

// Usherr: Crew Wrapped. A crew's year in review, worked out on request from
// what this Usherr already keeps of the crew: its votes and club nights
// (the parties: each vote that had a winner), the reactions at those
// parties' screenings, the stars the crew's people chose to share, and who
// picked the winners. Nothing is sent over the link for it, and nothing is
// posted anywhere: the page draws each slide into a picture for the person
// to keep.
//
// Privacy: only the crew's own people. Stars come from "Share what I watch"
// (the crew's activity, and the Diary of people here who turned it on);
// NSFW titles never appear.

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/link/crew"
	"github.com/rdborg/mediarium/internal/link/screening"
)

const (
	crewWrappedVotes   = 400 // votes read for a year, at most
	crewWrappedPickers = 8   // people on "Who picked best"
)

type crewWrappedFilm struct {
	TMDBID int     `json:"tmdbId,omitempty"`
	Title  string  `json:"title"`
	Poster string  `json:"poster,omitempty"` // a TMDB image path
	Stars  float64 `json:"stars,omitempty"`  // the average, out of 10
	Low    int     `json:"low,omitempty"`    // the lowest and highest stars it got
	High   int     `json:"high,omitempty"`
	Count  int     `json:"count"` // ratings, or reactions
}

type crewWrappedMoment struct {
	TMDBID int     `json:"tmdbId"`
	Title  string  `json:"title"`
	Pos    float64 `json:"pos"`   // seconds into the film
	Word   string  `json:"word"`  // the word sent most then
	Count  int     `json:"count"` // how many of it
	Total  int     `json:"total"` // all the reactions then
}

type crewWrappedPicker struct {
	Who   string `json:"who"`
	House string `json:"house,omitempty"`
	Wins  int    `json:"wins"`
	You   bool   `json:"you,omitempty"`
}

type crewWrappedOut struct {
	Crew      string              `json:"crew"`
	Year      int                 `json:"year"`
	Years     []int               `json:"years"`    // years with a party, newest first
	Featured  int                 `json:"featured"` // the year the Crews page offers (December and January), 0 otherwise
	Empty     bool                `json:"empty"`    // not enough parties that year
	Parties   int                 `json:"parties"`
	Minutes   int                 `json:"minutes"`
	About     bool                `json:"about"` // some running times guessed
	Houses    int                 `json:"houses"`
	Streak    int                 `json:"streak"`   // the longest run of club nights
	Weekdays  []int               `json:"weekdays"` // parties by weekday, 0 = Sunday
	Favourite *crewWrappedFilm    `json:"favourite,omitempty"`
	Divisive  *crewWrappedFilm    `json:"divisive,omitempty"`
	Lowest    *crewWrappedFilm    `json:"lowest,omitempty"`
	Ratings   int                 `json:"ratings"`
	StarsFrom string              `json:"starsFrom"` // crew (shared by the crew's houses), house (only people here), '' none
	Moment    *crewWrappedMoment  `json:"moment,omitempty"`
	MostHA    *crewWrappedFilm    `json:"mostHa,omitempty"`
	MostWOW   *crewWrappedFilm    `json:"mostWow,omitempty"`
	Pickers   []crewWrappedPicker `json:"pickers"`
	Clubs     []string            `json:"clubs"`
	NextYear  int                 `json:"nextYear"`
}

// crewParty is one party of a crew: a vote that had a winner.
type crewParty struct {
	TMDB    int
	Title   string
	Poster  string
	At      int64
	Runtime int
	SID     string
	Winners []crew.Seat
}

// crewWrappedFeatured is the year the Crews page offers: this year from
// December 1, last year through January; 0 the rest of the year.
func crewWrappedFeatured(now time.Time) int {
	switch now.Month() {
	case time.December:
		return now.Year()
	case time.January:
		return now.Year() - 1
	}
	return 0
}

// crewVoteWinners are who picked a vote's winner: the picker of a club
// night in pick mode (a ballot with the one film nobody voted on), else
// whoever voted for the winning film.
func crewVoteWinners(hostH int, host, club string, winner int, choices []crew.Choice) []crew.Seat {
	if winner == 0 {
		return nil
	}
	if club != "" && len(choices) == 1 && len(choices[0].Voters) == 0 && choices[0].TMDB == winner && host != "" {
		return []crew.Seat{{H: hostH, Who: host}}
	}
	for _, ch := range choices {
		if ch.TMDB == winner {
			return ch.Voters
		}
	}
	return nil
}

// crewYearParties are a crew's parties that year, oldest first (NSFW
// titles left out), and every year that had one.
func (s *Server) crewYearParties(ctx context.Context, crewID int64, year int, now time.Time) ([]crewParty, []int) {
	from := time.Date(year, 1, 1, 0, 0, 0, 0, time.Local).UnixMilli()
	to := time.Date(year+1, 1, 1, 0, 0, 0, 0, time.Local).UnixMilli()
	years := []int{}
	seenYear := map[int]bool{}
	var parties []crewParty
	rows, err := s.db.QueryContext(ctx, `SELECT host_hid, host_name, club_kid, winner_tmdb, choices, at_ms, showtime_id FROM crew_votes
		WHERE crew_id = ? AND winner_tmdb > 0 AND state IN ('closed', 'booked') AND at_ms <= ? ORDER BY at_ms LIMIT ?`, crewID, now.UnixMilli(), crewWrappedVotes*10)
	if err != nil {
		return nil, years
	}
	type voteRow struct {
		hostH        int
		host, club   string
		winner       int
		choices      string
		at, showtime int64
	}
	var list []voteRow
	for rows.Next() {
		var v voteRow
		if rows.Scan(&v.hostH, &v.host, &v.club, &v.winner, &v.choices, &v.at, &v.showtime) == nil {
			list = append(list, v)
		}
	}
	rows.Close()
	for _, v := range list {
		var choices []crew.Choice
		_ = json.Unmarshal([]byte(v.choices), &choices)
		p := crewParty{TMDB: v.winner, At: v.at, Winners: crewVoteWinners(v.hostH, v.host, v.club, v.winner, choices)}
		for _, ch := range choices {
			if ch.TMDB == v.winner {
				p.Title, p.Poster = ch.Title, ch.Poster
			}
		}
		if p.Title == "" || adultLibrary.MatchString(p.Title) || s.crewAdultTitle(ctx, "movie", p.TMDB) {
			continue // NSFW never appears
		}
		if y := time.UnixMilli(v.at).Local().Year(); !seenYear[y] {
			seenYear[y] = true
			years = append(years, y)
		}
		if v.at < from || v.at >= to || len(parties) >= crewWrappedVotes {
			continue
		}
		p.Runtime, p.SID = s.crewPartyScreening(ctx, v.showtime, v.winner, v.at)
		parties = append(parties, p)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(years)))
	return parties, years
}

// crewPartyScreening finds a party's screening here (its running time and
// the screening's id, for the reactions): the host's showtime, or the
// linked screening this Usherr was invited to for that film at that time.
func (s *Server) crewPartyScreening(ctx context.Context, showtimeID int64, tmdb int, at int64) (int, string) {
	var runtime int
	var sid string
	if showtimeID > 0 {
		var sidN sql.NullString
		if s.db.QueryRowContext(ctx, `SELECT st.runtime, o.sid FROM showtimes st LEFT JOIN link_screenings_out o ON o.showtime_id = st.id WHERE st.id = ?`, showtimeID).Scan(&runtime, &sidN) == nil {
			return runtime, sidN.String
		}
	}
	_ = s.db.QueryRowContext(ctx, `SELECT runtime, sid FROM link_screenings_in WHERE tmdb_id = ? AND ABS(at_ms - ?) <= ? ORDER BY ABS(at_ms - ?) LIMIT 1`,
		tmdb, at, time.Hour.Milliseconds(), at).Scan(&runtime, &sid)
	return runtime, sid
}

// GET /api/home/crews/{id}/wrapped?year= (member, in the crew): the crew's
// year in review (the year the page offers, else last year).
func (s *Server) handleCrewWrapped(w http.ResponseWriter, r *http.Request) {
	c, ok := s.crewMember(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	now := time.Now()
	year, _ := strconv.Atoi(r.URL.Query().Get("year"))
	if year == 0 {
		year = crewWrappedFeatured(now)
		if year == 0 {
			year = now.Year() - 1
		}
	}
	if year < 2000 || year > now.Year() {
		writeError(w, http.StatusBadRequest, "That year can't be shown.")
		return
	}
	writeJSON(w, http.StatusOK, s.crewWrapped(ctx, c, requestUserID(r), year, now))
}

func (s *Server) crewWrapped(ctx context.Context, c crewRow, uid int64, year int, now time.Time) crewWrappedOut {
	out := crewWrappedOut{Crew: c.Name, Year: year, Featured: crewWrappedFeatured(now), Weekdays: make([]int, 7), Pickers: []crewWrappedPicker{}, Clubs: []string{}, NextYear: year + 1}
	parties, years := s.crewYearParties(ctx, c.ID, year, now)
	out.Years = years
	houseName := map[int]string{}
	for _, h := range s.crewHouses(ctx, c) {
		if h.State == "joined" {
			out.Houses++
			houseName[h.H] = h.Name
		}
	}
	if rows, err := s.db.QueryContext(ctx, `SELECT name FROM crew_clubs WHERE crew_id = ? AND ended_ms = 0 ORDER BY created_ms`, c.ID); err == nil {
		for rows.Next() {
			var n string
			if rows.Scan(&n) == nil && !adultLibrary.MatchString(n) {
				out.Clubs = append(out.Clubs, n)
			}
		}
		rows.Close()
	}
	out.Parties = len(parties)
	if out.Parties == 0 {
		out.Empty = true
		return out
	}

	// You showed up.
	for _, p := range parties {
		mins := p.Runtime
		if mins <= 0 {
			mins, out.About = typicalFilm, true
		}
		out.Minutes += mins
		out.Weekdays[time.UnixMilli(p.At).Local().Weekday()]++
	}
	out.Streak = s.crewYearStreak(ctx, c.ID, year, now)

	// The crew's favourite.
	s.crewWrappedStars(ctx, c, parties, &out)

	// The moment.
	s.crewWrappedMoment(ctx, parties, &out)

	// Who picked best.
	me := s.crewPersonName(ctx, uid)
	type key struct {
		h   int
		who string
	}
	wins := map[key]int{}
	for _, p := range parties {
		seen := map[key]bool{}
		for _, seat := range p.Winners {
			k := key{seat.H, seat.Who}
			if seat.Who == "" || seen[k] {
				continue
			}
			seen[k] = true
			wins[k]++
		}
	}
	for k, n := range wins {
		out.Pickers = append(out.Pickers, crewWrappedPicker{Who: k.who, House: houseName[k.h], Wins: n, You: k.h == c.MyHid && k.who == me})
	}
	sort.Slice(out.Pickers, func(i, j int) bool {
		a, b := out.Pickers[i], out.Pickers[j]
		if a.Wins != b.Wins {
			return a.Wins > b.Wins
		}
		return a.Who < b.Who
	})
	out.Pickers = out.Pickers[:min(len(out.Pickers), crewWrappedPickers)]
	return out
}

// crewYearStreak is the longest run of club nights that happened that year:
// from the nights this Usherr arranged (it runs the crew), else, this year,
// the clubs' running streaks as the owner last said.
func (s *Server) crewYearStreak(ctx context.Context, crewID int64, year int, now time.Time) int {
	from := time.Date(year, 1, 1, 0, 0, 0, 0, time.Local).UnixMilli()
	to := time.Date(year+1, 1, 1, 0, 0, 0, 0, time.Local).UnixMilli()
	best := 0
	rows, err := s.db.QueryContext(ctx, `SELECT kid, held FROM crew_club_nights WHERE crew_id = ? AND night_ms >= ? AND night_ms < ? ORDER BY kid, night_ms`, crewID, from, to)
	if err == nil {
		run, last := 0, ""
		for rows.Next() {
			var kid string
			var held bool
			if rows.Scan(&kid, &held) != nil {
				continue
			}
			if kid != last {
				run, last = 0, kid
			}
			if held {
				run++
				best = max(best, run)
			} else {
				run = 0
			}
		}
		rows.Close()
	}
	if year == now.Year() {
		var streak int
		_ = s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(streak), 0) FROM crew_clubs WHERE crew_id = ?`, crewID).Scan(&streak)
		best = max(best, streak)
	}
	return best
}

// crewWrappedStars finds the crew's favourite, most divisive and lowest
// rated party film: from the stars the crew's people shared with it ("Share
// what I watch": the crew's activity, and the Diary of people here who
// turned it on), one rating per person.
func (s *Server) crewWrappedStars(ctx context.Context, c crewRow, parties []crewParty, out *crewWrappedOut) {
	type film struct {
		f     crewWrappedFilm
		stars map[string]int // person (house and name) -> stars
	}
	films := map[int]*film{}
	byTitle := map[string]*film{}
	var order []int
	for _, p := range parties {
		if films[p.TMDB] == nil {
			films[p.TMDB] = &film{f: crewWrappedFilm{TMDBID: p.TMDB, Title: p.Title, Poster: p.Poster}, stars: map[string]int{}}
			byTitle[strings.ToLower(p.Title)] = films[p.TMDB]
			order = append(order, p.TMDB)
		}
	}
	from := time.Date(out.Year, 1, 1, 0, 0, 0, 0, time.Local).UnixMilli()
	other := false
	rows, err := s.db.QueryContext(ctx, `SELECT hid, who, title, stars FROM crew_activity WHERE crew_id = ? AND kind = ? AND stars > 0 AND at_ms >= ? ORDER BY at_ms, id`, c.ID, crew.DidRate, from)
	if err == nil {
		for rows.Next() {
			var hid, stars int
			var who, title string
			if rows.Scan(&hid, &who, &title, &stars) != nil || adultLibrary.MatchString(title) {
				continue
			}
			if f := byTitle[strings.ToLower(title)]; f != nil {
				f.stars[strconv.Itoa(hid)+"/"+who] = min(stars, 10)
				if hid != c.MyHid {
					other = true
				}
			}
		}
		rows.Close()
	}
	// People here who share what they watch: their Diary's stars.
	var sharers []int64
	if rows, err := s.db.QueryContext(ctx, `SELECT p.user_id FROM crew_people p JOIN crew_sharing sh ON sh.user_id = p.user_id WHERE p.crew_id = ? AND sh.share = 1`, c.ID); err == nil {
		for rows.Next() {
			var id int64
			if rows.Scan(&id) == nil {
				sharers = append(sharers, id)
			}
		}
		rows.Close()
	}
	for _, uid := range sharers {
		name := strconv.Itoa(c.MyHid) + "/" + s.crewPersonName(ctx, uid)
		for _, id := range order {
			if st := s.myStars(ctx, uid, id); st > 0 {
				films[id].stars[name] = st
			}
		}
	}
	var rated []crewWrappedFilm
	for _, id := range order {
		f := films[id]
		if len(f.stars) == 0 {
			continue
		}
		sum, lo, hi := 0, 10, 0
		for _, st := range f.stars {
			sum += st
			lo, hi = min(lo, st), max(hi, st)
		}
		f.f.Count, f.f.Low, f.f.High = len(f.stars), lo, hi
		f.f.Stars = float64(int(float64(sum)/float64(len(f.stars))*10+0.5)) / 10
		out.Ratings += len(f.stars)
		rated = append(rated, f.f)
	}
	if len(rated) == 0 {
		return
	}
	out.StarsFrom = "house"
	if other {
		out.StarsFrom = "crew"
	}
	sort.SliceStable(rated, func(i, j int) bool {
		if rated[i].Stars != rated[j].Stars {
			return rated[i].Stars > rated[j].Stars
		}
		return rated[i].Count > rated[j].Count
	})
	top := rated[0]
	out.Favourite = &top
	if len(rated) > 1 {
		low := rated[len(rated)-1]
		if low.Stars < top.Stars {
			out.Lowest = &low
		}
	}
	var split *crewWrappedFilm
	for i := range rated {
		f := rated[i]
		if f.Count >= 2 && f.High-f.Low >= 2 && (split == nil || f.High-f.Low > split.High-split.Low) {
			split = &f
		}
	}
	out.Divisive = split
}

// crewWrappedMoment finds the year's most-reacted moment over the crew's
// parties, and the films with the most HAs and WOWs.
func (s *Server) crewWrappedMoment(ctx context.Context, parties []crewParty, out *crewWrappedOut) {
	words := map[string]map[int]int{"HA": {}, "WOW": {}}
	title := map[int]crewParty{}
	seen := map[string]bool{}
	for _, p := range parties {
		if p.SID == "" || seen[p.SID] {
			continue
		}
		seen[p.SID] = true
		title[p.TMDB] = p
		if m, ok := s.screeningTopMoment(ctx, p.SID); ok && (out.Moment == nil || m.Total > out.Moment.Total) {
			out.Moment = &crewWrappedMoment{TMDBID: p.TMDB, Title: p.Title, Pos: m.Peak, Word: m.Top, Count: m.Words[m.Top], Total: m.Total}
		}
		for w, n := range s.screeningReactTotals(ctx, p.SID) {
			if words[w] != nil && screening.ValidWord(w) {
				words[w][p.TMDB] += n
			}
		}
	}
	most := func(w string) *crewWrappedFilm {
		best, n := 0, 0
		for id, c := range words[w] {
			if c > n || (c == n && id < best) {
				best, n = id, c
			}
		}
		if n == 0 {
			return nil
		}
		p := title[best]
		return &crewWrappedFilm{TMDBID: best, Title: p.Title, Poster: p.Poster, Count: n}
	}
	out.MostHA, out.MostWOW = most("HA"), most("WOW")
}
