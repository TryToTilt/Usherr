package api

// Usherr: Crews, the light job (home_crews.go). Every minute while linking
// is on: votes that closed get their winner (booked in the host's
// Showtime), club nights coming up get a vote or the picker's pick, and
// every few minutes each house says again whether it has the list's films,
// what people chose to share goes out, and crews owned here are sent again
// to houses that haven't had them in an hour. Only database reads and small
// messages: nothing heavy, so it doesn't wait for the 2 to 9 AM window.

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/rdborg/mediarium/internal/link/crew"
)

const (
	crewClubLead  = 48 * time.Hour      // a club night is arranged this long before
	crewClubClose = 3 * time.Hour       // a club night's vote closes this long before it
	crewResend    = time.Hour           // everything is sent again this often
	crewShareKeep = 14 * time.Hour * 24 // shared lines older than this aren't sent
)

func (s *Server) startCrewJob() {
	l := &s.linkS.crew
	l.mu.Lock()
	if l.stop != nil {
		close(l.stop)
	}
	stop := make(chan struct{})
	l.stop = stop
	l.mu.Unlock()
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				s.crewJob(ctx, time.Now())
				cancel()
			}
		}
	}()
}

func (s *Server) stopCrewJob() {
	l := &s.linkS.crew
	l.mu.Lock()
	if l.stop != nil {
		close(l.stop)
		l.stop = nil
	}
	l.mu.Unlock()
}

func (s *Server) crewJob(ctx context.Context, now time.Time) {
	if s.linkNode() == nil {
		return
	}
	l := &s.linkS.crew
	l.mu.Lock()
	l.tick++
	every5 := l.tick%5 == 1
	l.mu.Unlock()
	var crews []crewRow
	if rows, err := s.db.QueryContext(ctx, `SELECT `+crewCols+` FROM crews WHERE state = 'joined'`); err == nil {
		for rows.Next() {
			if c, err := scanCrew(rows); err == nil {
				crews = append(crews, c)
			}
		}
		rows.Close()
	}
	for _, c := range crews {
		if c.mine() {
			s.crewCloseVotes(ctx, c, now)
			s.crewClubNights(ctx, c, now)
			if every5 {
				s.crewResendStale(ctx, c, now)
			}
		}
		if every5 {
			s.crewTellHave(c.ID)
			if !c.mine() {
				// A vote opened here that the owner never took (it said no).
				_, _ = s.db.ExecContext(ctx, `DELETE FROM crew_votes WHERE crew_id = ? AND choices = '[]' AND created_ms < ?`, c.ID, now.Add(-10*time.Minute).UnixMilli())
			}
		}
	}
	if every5 {
		s.crewShareJob(ctx, now)
	}
}

// crewCloseVotes closes the votes of a crew owned here whose time is up: the
// film with the most votes wins (on a tie, one everyone has, then the
// first on the ballot). The host's Usherr books it.
func (s *Server) crewCloseVotes(ctx context.Context, c crewRow, now time.Time) {
	var ids []int64
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM crew_votes WHERE crew_id = ? AND state = 'open' AND closes_ms <= ?`, c.ID, now.UnixMilli())
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
	houses := 1 + len(s.crewHouseRows(ctx, c.ID, true))
	for _, id := range ids {
		b, ok := s.crewBallot(ctx, c, id)
		if !ok {
			continue
		}
		winner := crewWinner(b.Choices, houses)
		state := crew.VoteNobody
		if winner > 0 {
			state = crew.VoteClosed
		}
		_, _ = s.db.ExecContext(ctx, `UPDATE crew_votes SET state = ?, winner_tmdb = ?, closed_ms = ? WHERE id = ? AND state = 'open'`, state, winner, now.UnixMilli(), id)
		s.crewSendBallot(ctx, c, id)
		if winner > 0 && b.HostH == 1 {
			go s.crewBookWinner(c.ID, id)
		}
		if b.Club != "" {
			s.crewClubNightResult(ctx, c, b.Club, b.At, winner, b.Choices)
		}
	}
}

// crewWinner is the winning film of a ballot (0: nobody voted).
func crewWinner(choices []crew.Choice, houses int) int {
	best, bestVotes, bestAll := 0, 0, false
	for _, ch := range choices {
		all := crewCan(ch.Have, houses) == "all"
		v := len(ch.Voters)
		if v > bestVotes || (v == bestVotes && v > 0 && all && !bestAll) {
			best, bestVotes, bestAll = ch.TMDB, v, all
		}
	}
	return best
}

// crewCan says who can play a film: all (every house said yes), some, none.
func crewCan(have []crew.Have, houses int) string {
	yes := 0
	for _, h := range have {
		if h.Has == crew.HasYes {
			yes++
		}
	}
	switch {
	case yes >= houses && houses > 0:
		return "all"
	case yes > 0:
		return "some"
	}
	return "none"
}

// crewClubNights arranges the club nights of a crew owned here that come up
// within two days: the picker's pick is booked, or the crew votes (from the
// shared watchlist). A night with nobody in the club, or nothing to vote
// on, is skipped (and the streak starts again).
func (s *Server) crewClubNights(ctx context.Context, c crewRow, now time.Time) {
	l := &s.linkS.crew
	for _, k := range s.crewClubRows(ctx, c.ID) {
		night := time.UnixMilli(k.Next)
		if k.Next == 0 || now.Before(night.Add(-crewClubLead)) {
			continue
		}
		l.mu.Lock()
		voteID := s.crewArrangeNight(ctx, c, k, night, now)
		l.mu.Unlock()
		s.crewSendClubs(ctx, c)
		if voteID > 0 {
			s.crewSendBallot(ctx, c, voteID)
			var hostH, winner int
			_ = s.db.QueryRowContext(ctx, `SELECT host_hid, winner_tmdb FROM crew_votes WHERE id = ?`, voteID).Scan(&hostH, &winner)
			if hostH == 1 && winner > 0 {
				go s.crewBookWinner(c.ID, voteID)
			}
		}
	}
}

// crewArrangeNight arranges one club night and moves the club on to the
// next. It returns the vote it made (a pick is a closed vote with one film),
// 0 for none.
func (s *Server) crewArrangeNight(ctx context.Context, c crewRow, k crewClubRow, night, now time.Time) int64 {
	next := clubNextNight(night, k.Every)
	for !next.After(now) {
		next = clubNextNight(next, k.Every)
	}
	var picker crew.Seat
	if len(k.Rotation) > 0 {
		picker = k.Rotation[min(k.Turn, len(k.Rotation)-1)]
	}
	held := func(mode string, tmdb int, title, vid string) {
		_, _ = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO crew_club_nights (crew_id, kid, club_name, night_ms, mode, picker_hid, picker, tmdb_id, title, vid, held, created_ms)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, c.ID, k.KID, k.Name, night.UnixMilli(), mode, picker.H, picker.Who, tmdb, title, vid, b2i(vid != ""), now.UnixMilli())
	}
	k.Next = next.UnixMilli()
	var voteID int64
	switch {
	case night.Before(now.Add(time.Hour)) || picker.Who == "":
		held(k.Mode, 0, "", "") // too late, or nobody's in the club
	case k.Mode == crew.ModePick && k.Pick != nil:
		vid := crew.NewID()
		raw, _ := json.Marshal([]crew.Choice{{Film: *k.Pick}})
		res, err := s.db.ExecContext(ctx, `INSERT INTO crew_votes (crew_id, vid, host_hid, host_name, closes_ms, at_ms, preshow_min, club_kid, state, winner_tmdb, choices, created_ms, closed_ms)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'closed', ?, ?, ?, ?)`, c.ID, vid, picker.H, picker.Who, now.UnixMilli(), night.UnixMilli(), k.Preshow, k.KID, k.Pick.TMDB, string(raw), now.UnixMilli(), now.UnixMilli())
		if err == nil {
			voteID, _ = res.LastInsertId()
			held(crew.ModePick, k.Pick.TMDB, k.Pick.Title, vid)
		}
	default:
		// The crew votes (also when the picker didn't pick in time).
		films := crewBallotFilms(s.crewListRows(ctx, c.ID), 1+len(s.crewHouseRows(ctx, c.ID, true)))
		closes := night.Add(-crewClubClose)
		if len(films) < 2 || closes.Before(now.Add(10*time.Minute)) {
			held(crew.ModeVote, 0, "", "")
			break
		}
		vid := crew.NewID()
		choices := make([]crew.Choice, 0, len(films))
		for _, f := range films {
			choices = append(choices, crew.Choice{Film: f})
		}
		raw, _ := json.Marshal(choices)
		res, err := s.db.ExecContext(ctx, `INSERT INTO crew_votes (crew_id, vid, host_hid, host_name, closes_ms, at_ms, preshow_min, club_kid, state, choices, created_ms)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'open', ?, ?)`, c.ID, vid, picker.H, picker.Who, closes.UnixMilli(), night.UnixMilli(), k.Preshow, k.KID, string(raw), now.UnixMilli())
		if err == nil {
			voteID, _ = res.LastInsertId()
			held(crew.ModeVote, 0, "", vid)
		}
	}
	if voteID > 0 {
		k.Streak++
		k.Nights++
	} else {
		k.Streak = 0
	}
	if len(k.Rotation) > 0 {
		k.Turn = (k.Turn + 1) % len(k.Rotation)
	}
	k.Pick = nil
	_ = s.saveClub(ctx, k)
	return voteID
}

// crewClubNightResult notes a club night's winner (for the year in review).
func (s *Server) crewClubNightResult(ctx context.Context, c crewRow, kid string, night int64, winner int, choices []crew.Choice) {
	title := ""
	for _, ch := range choices {
		if ch.TMDB == winner {
			title = ch.Title
		}
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE crew_club_nights SET tmdb_id = ?, title = ? WHERE crew_id = ? AND kid = ? AND night_ms = ?`, winner, title, c.ID, kid, night)
}

// crewBallotFilms picks a club night's ballot from the shared watchlist:
// films everyone can play first, then the newest, up to six.
func crewBallotFilms(list []crew.Entry, houses int) []crew.Film {
	sort.SliceStable(list, func(i, j int) bool {
		a, b := crewCan(list[i].Have, houses) == "all", crewCan(list[j].Have, houses) == "all"
		if a != b {
			return a
		}
		return list[i].Added > list[j].Added
	})
	var out []crew.Film
	for _, e := range list {
		if len(out) < 6 {
			out = append(out, e.Film)
		}
	}
	return out
}

// crewResendStale sends everything again to houses of a crew owned here that
// haven't had it in an hour (a message waits only so long at the relay).
func (s *Server) crewResendStale(ctx context.Context, c crewRow, now time.Time) {
	for _, h := range s.crewHouseRows(ctx, c.ID, false) {
		var sent int64
		_ = s.db.QueryRowContext(ctx, `SELECT sent_ms FROM crew_houses WHERE crew_id = ? AND hid = ?`, c.ID, h.Hid).Scan(&sent)
		if now.UnixMilli()-sent < crewResend.Milliseconds() {
			continue
		}
		if h.State == "joined" {
			s.crewSync(ctx, c, h.Hid)
		} else {
			s.crewSendInvites(ctx, c, h.Hid)
			_, _ = s.db.ExecContext(ctx, `UPDATE crew_houses SET sent_ms = ? WHERE crew_id = ? AND hid = ?`, now.UnixMilli(), c.ID, h.Hid)
		}
	}
}

// crewShareJob sends what people who turned on "Share what I watch with my
// crews" watched since: to the crews they joined, nothing else. Never NSFW:
// only films and shows from the Diary, and nothing whose title or library
// reads as adult.
func (s *Server) crewShareJob(ctx context.Context, now time.Time) {
	type sharer struct {
		uid  int64
		last int64
	}
	var who []sharer
	rows, err := s.db.QueryContext(ctx, `SELECT user_id, last_diary FROM crew_sharing WHERE share = 1`)
	if err != nil {
		return
	}
	for rows.Next() {
		var x sharer
		if rows.Scan(&x.uid, &x.last) == nil {
			who = append(who, x)
		}
	}
	rows.Close()
	for _, x := range who {
		s.crewShareOne(ctx, x.uid, x.last, now)
	}
}

func (s *Server) crewShareOne(ctx context.Context, uid, last int64, now time.Time) {
	type entry struct {
		id                    int64
		kind, title, ep       string
		stars, episodes, tmdb int
		day                   string
	}
	var list []entry
	rows, err := s.db.QueryContext(ctx, `SELECT id, kind, tmdb_id, title, episode, stars, episodes, watched_on FROM home_diary
		WHERE user_id = ? AND id > ? AND hidden = 0 AND kind IN ('movie', 'tv') ORDER BY id LIMIT ?`, uid, last, crew.MaxLines)
	if err != nil {
		return
	}
	for rows.Next() {
		var e entry
		if rows.Scan(&e.id, &e.kind, &e.tmdb, &e.title, &e.ep, &e.stars, &e.episodes, &e.day) == nil {
			list = append(list, e)
		}
	}
	rows.Close()
	if len(list) == 0 {
		return
	}
	name := s.crewPersonName(ctx, uid)
	var lines []crew.Line
	for _, e := range list {
		if e.title == "" || adultLibrary.MatchString(e.title) || s.crewAdultTitle(ctx, e.kind, e.tmdb) {
			continue
		}
		day, err := time.ParseInLocation("2006-01-02", e.day, time.Local)
		if err != nil || now.Sub(day) > crewShareKeep {
			continue // old history imported later isn't news
		}
		l := crew.Line{Who: name, Title: e.title, At: now.UnixMilli()}
		switch {
		case e.stars > 0:
			l.Kind, l.Stars = crew.DidRate, min(e.stars, 10)
		case e.kind == "movie":
			l.Kind = crew.DidFinish
		default:
			l.Kind, l.Part = crew.DidWatch, crewEpisode(e.ep)
		}
		lines = append(lines, l)
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE crew_sharing SET last_diary = ? WHERE user_id = ?`, list[len(list)-1].id, uid)
	if len(lines) == 0 {
		return
	}
	var crews []int64
	if rows, err := s.db.QueryContext(ctx, `SELECT p.crew_id FROM crew_people p JOIN crews c ON c.id = p.crew_id WHERE p.user_id = ? AND c.state = 'joined'`, uid); err == nil {
		for rows.Next() {
			var id int64
			if rows.Scan(&id) == nil {
				crews = append(crews, id)
			}
		}
		rows.Close()
	}
	for _, id := range crews {
		c, ok := s.crewByID(ctx, id)
		if !ok {
			continue
		}
		a := &crew.Activity{V: crew.Version, CID: c.CID, Lines: append([]crew.Line(nil), lines...)}
		if !c.mine() {
			// Kept here too, as the owner passes it to the others only.
			for _, l := range a.Lines {
				l.H = c.MyHid
				_, _ = s.db.ExecContext(ctx, `INSERT INTO crew_activity (crew_id, hid, who, kind, title, part, stars, at_ms) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
					c.ID, l.H, l.Who, l.Kind, l.Title, l.Part, l.Stars, l.At)
			}
		}
		_ = s.crewAct(ctx, c, crew.TypeActivity, a, 0, 0, false)
	}
}

// crewAdultTitle: a film in the library that sits in an adult (NSFW)
// library folder.
func (s *Server) crewAdultTitle(ctx context.Context, kind string, tmdb int) bool {
	if kind != "movie" {
		return false
	}
	m, ok, err := s.MovieRepo.GetByTMDBID(tmdb)
	return err == nil && ok && adultLibrary.MatchString(m.FilePath)
}

// crewEpisode turns a Diary's "S2E3" or "S2E3–S2E5" into a short part.
func crewEpisode(ep string) string {
	if ep == "" {
		return ""
	}
	if r := []rune(ep); len(r) > 20 {
		return string(r[:20])
	}
	return ep
}
