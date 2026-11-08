package api

// Usherr: the party at a showtime. Three moments around the film, for a
// linked screening (hosted here or a guest's booking) and, where it makes
// sense, for a showtime of this Usherr's own:
//
//   - The lobby, before the film: a countdown, the pre-show "playing in
//     step" (which piece is on), and who's seated. Everyone who said I'm in
//     has a seat, in the same order the ticket stubs get theirs (eight a
//     row, the host Row A Seat 1), so the seat you sit in is the one on your
//     stub. Take my seat marks you seated; until then you're on the way.
//   - The intermission (a linked screening, the host only): the film pauses
//     for everyone, a full screen counts down (10 minutes, the host can add
//     5) and says when it's back; guests can say what they're up to
//     ("getting snacks"); the host resumes it for everyone.
//   - The verdict at the credits: stars (half stars too) and maybe a line,
//     saved to your own Diary (and posted as your review, if you want). The
//     crew's card shows the average and how many voted; names and stars only
//     for people who chose to share. Then the most-reacted moment, and your
//     stub.
//
// Over the link, the host's Usherr is the hub (internal/link/screening,
// party.go): guests send their own people's seats, statuses and stars to the
// host; the host sends the seat map, the intermission and the verdict to
// everyone at the screening. Lines never leave this Usherr; a voter's name
// only when they share.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/link"
	"github.com/rdborg/mediarium/internal/link/rewards"
	"github.com/rdborg/mediarium/internal/link/screening"
)

const (
	partyLobbyAgain = 5 * time.Minute // a seat map or lobby.seat unchanged is sent again this often
	partyLobbyAhead = 3 * time.Hour   // the lobby is kept up to date from this long before the film
	partyVoteResend = 12 * time.Hour  // a guest's vote that didn't reach the host is tried again this long
	partyLineMax    = 140             // characters in a verdict's line
)

// partyRef is the party at a showtime here: a linked screening's room, or
// a showtime of this Usherr's own.
type partyRef struct {
	Key  string // the screening's id, or "st-<showtime id>"
	ST   showtime
	Room *screeningRoom
}

func (p partyRef) role() string {
	if p.Room == nil {
		return "local"
	}
	return p.Room.Role
}

func (s *Server) partyAt(ctx context.Context, id int64) (partyRef, bool) {
	st, ok := s.showtimeByID(ctx, id)
	if !ok {
		return partyRef{}, false
	}
	if r, ok := s.roomOf(ctx, id); ok {
		return partyRef{Key: r.SID, ST: st, Room: &r}, true
	}
	return partyRef{Key: "st-" + strconv.FormatInt(id, 10), ST: st}, true
}

// partyToken is someone here as the party knows them: never their user id.
// It's made with this Usherr's own key, so it's different at every Usherr
// and nobody else can work out whose it is.
func (s *Server) partyToken(key string, uid int64) string {
	msg := []byte("usherr-party|" + key + "|" + strconv.FormatInt(uid, 10))
	if id, err := s.linkIdentity(); err == nil && id != nil {
		msg = id.Sign(msg)
	} else {
		msg = append(msg, []byte("|"+s.linkName())...)
	}
	h := sha256.Sum256(msg)
	return hex.EncodeToString(h[:8])
}

// partyCanHost: whoever runs the film (a linked screening's host), or
// whoever booked a showtime of this Usherr's own (or an administrator).
func (s *Server) partyCanHost(r *http.Request, p partyRef) bool {
	if p.Room != nil {
		return s.canControlRoom(r, *p.Room)
	}
	u := auth.UserFromContext(r.Context())
	return p.ST.UserID == requestUserID(r) || (u != nil && u.IsAdmin)
}

// ---- kept ----

type partyStateRow struct {
	Roster     string
	RosterSeq  int64
	InterSeq   int64
	InterOn    bool
	InterUntil int64
	Statuses   string
	Tally      string
	TallySeq   int64
	CreditsMs  int64
}

func (s *Server) partyState(ctx context.Context, key string) partyStateRow {
	var p partyStateRow
	var on int
	_ = s.db.QueryRowContext(ctx, `SELECT roster, roster_seq, inter_seq, inter_on, inter_until, statuses, tally, tally_seq, credits_ms FROM party_state WHERE pkey = ?`, key).Scan(
		&p.Roster, &p.RosterSeq, &p.InterSeq, &on, &p.InterUntil, &p.Statuses, &p.Tally, &p.TallySeq, &p.CreditsMs)
	p.InterOn = on == 1
	return p
}

func (s *Server) partyStateRow(ctx context.Context, key string) {
	_, _ = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO party_state (pkey) VALUES (?)`, key)
}

func (s *Server) partyChanged(showtimeID int64) {
	s.linkS.scr.room.changed(showtimeID)
}

// partySeq is the next number of a host's seat map or verdict (col): it
// goes up with every one sent, so guests keep the latest.
func (s *Server) partySeq(ctx context.Context, key, col string) int64 {
	if col != "roster_seq" && col != "tally_seq" {
		return time.Now().UnixMilli()
	}
	s.partyStateRow(ctx, key)
	var seq int64
	_, _ = s.db.ExecContext(ctx, `UPDATE party_state SET `+col+` = MAX(`+col+` + 1, ?) WHERE pkey = ?`, time.Now().UnixMilli(), key)
	_ = s.db.QueryRowContext(ctx, `SELECT `+col+` FROM party_state WHERE pkey = ?`, key).Scan(&seq)
	return max(seq, 1)
}

// later does f in the background, with a little time to send what it sends.
func (s *Server) later(f func(ctx context.Context)) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		f(ctx)
	}()
}

// lobbySent remembers what was last sent about a lobby, so the same thing
// isn't sent again every minute.
type lobbySent struct {
	sum uint64
	at  time.Time
}

// lobbyFresh reports whether body differs from what was last sent under key
// (or that was a while ago), and remembers it as sent.
func (s *Server) lobbyFresh(key string, body any) bool {
	raw, _ := json.Marshal(body)
	h := fnv.New64a()
	_, _ = h.Write(raw)
	sum := h.Sum64()
	l := &s.linkS.scr
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.lobby == nil {
		l.lobby = map[string]lobbySent{}
	}
	last, ok := l.lobby[key]
	if ok && last.sum == sum && time.Since(last.at) < partyLobbyAgain {
		return false
	}
	l.lobby[key] = lobbySent{sum: sum, at: time.Now()}
	return true
}

func (s *Server) lobbyForget(key string) {
	l := &s.linkS.scr
	l.mu.Lock()
	delete(l.lobby, key)
	l.mu.Unlock()
}

// ---- the lobby: seats ----

func (s *Server) seatedHere(ctx context.Context, key string) map[int64]bool {
	out := map[int64]bool{}
	rows, err := s.db.QueryContext(ctx, `SELECT user_id FROM party_seated WHERE pkey = ?`, key)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var u int64
		if rows.Scan(&u) == nil {
			out[u] = true
		}
	}
	return out
}

// hostSeatMap is the seat map of a screening hosted here, in the order the
// stubs are handed out at the door (home_link_rewards.go, linkedDoor): the
// household first (whoever booked it seat 0, Row A Seat 1; then the others
// in the order they said I'm in), then a block for each linked Usherr
// coming, in the order they answered. Seats already handed out are kept;
// the rest are where the door will put them.
func (s *Server) hostSeatMap(ctx context.Context, st showtime, sid string) []screening.Seat {
	house := link.CleanName(s.linkName())
	answers := s.hostHousehold(ctx, st)
	var mine []int64
	if answers[st.UserID] == screening.Going {
		mine = append(mine, st.UserID)
	}
	if rows, err := s.db.QueryContext(ctx, `SELECT user_id FROM link_screening_host_people WHERE showtime_id = ? AND answer = ? AND user_id <> ? ORDER BY at_ms, user_id`,
		st.ID, screening.Going, st.UserID); err == nil {
		for rows.Next() {
			var u int64
			if rows.Scan(&u) == nil {
				mine = append(mine, u)
			}
		}
		rows.Close()
	}
	used := map[int]bool{}
	issued := map[int64]int{}
	if rows, err := s.db.QueryContext(ctx, `SELECT external_id, seat FROM community_stubs WHERE backend = 'here' AND showtime_id = ? AND sid = ? AND issuer = ''`, st.ID, sid); err == nil {
		for rows.Next() {
			var ext string
			var n int
			if rows.Scan(&ext, &n) == nil {
				if u, err := strconv.ParseInt(ext, 10, 64); err == nil {
					issued[u] = n
					used[n] = true
				}
			}
		}
		rows.Close()
	}
	type guest struct {
		peer   int64
		name   string
		count  int
		seats  []int
		people []screening.LobbyPerson
	}
	var guests []*guest
	if rows, err := s.db.QueryContext(ctx, `SELECT g.peer_id, COALESCE(NULLIF(p.nickname, ''), p.name), g.count FROM link_screening_guests g JOIN link_peers p ON p.id = g.peer_id
		WHERE g.showtime_id = ? AND g.answer = ? AND g.count > 0 AND p.status = 'linked' AND p.their_id <> '' ORDER BY g.answered_ms, g.peer_id`, st.ID, screening.Going); err == nil {
		for rows.Next() {
			g := &guest{}
			if rows.Scan(&g.peer, &g.name, &g.count) == nil {
				g.name = link.CleanName(g.name)
				guests = append(guests, g)
			}
		}
		rows.Close()
	}
	for _, g := range guests {
		if rows, err := s.db.QueryContext(ctx, `SELECT first_seat, count FROM reward_seats WHERE showtime_id = ? AND peer_id = ? ORDER BY first_seat`, st.ID, g.peer); err == nil {
			for rows.Next() {
				var first, n int
				if rows.Scan(&first, &n) == nil {
					for i := range n {
						g.seats = append(g.seats, first+i)
						used[first+i] = true
					}
				}
			}
			rows.Close()
		}
		var raw string
		if s.db.QueryRowContext(ctx, `SELECT people FROM party_guest_people WHERE sid = ? AND peer_id = ?`, sid, g.peer).Scan(&raw) == nil {
			_ = json.Unmarshal([]byte(raw), &g.people)
		}
	}
	next := 1
	for n := range used {
		next = max(next, n+1)
	}
	take := func() int {
		for used[next] {
			next++
		}
		used[next] = true
		next++
		return next - 1
	}
	seated := s.seatedHere(ctx, sid)
	var seats []screening.Seat
	for _, u := range mine {
		n, ok := issued[u]
		if !ok {
			if u == st.UserID && !used[0] {
				n, used[0] = 0, true
			} else {
				n = take()
			}
		}
		seats = append(seats, screening.Seat{Seat: n, ID: s.partyToken(sid, u), Name: link.CleanName(s.partyHost(u)), House: house, Seated: seated[u]})
	}
	for _, g := range guests {
		want := min(g.count, rewards.MaxBlock*4)
		for len(g.seats) < want {
			g.seats = append(g.seats, take())
		}
		for i, n := range g.seats {
			seat := screening.Seat{Seat: n, House: g.name}
			if i < len(g.people) {
				p := g.people[i]
				seat.ID, seat.Name, seat.Seated = p.ID, p.Name, p.Seated
			}
			seats = append(seats, seat)
		}
	}
	sort.Slice(seats, func(i, j int) bool { return seats[i].Seat < seats[j].Seat })
	out := seats[:0]
	for _, x := range seats {
		if x.Seat <= screening.MaxSeatNo && len(out) < screening.MaxSeats {
			out = append(out, x)
		}
	}
	return out
}

// localSeatMap is the seat map of a showtime of this Usherr's own: whoever
// booked it in Row A Seat 1, then everyone else here who took a seat, in
// the order they did.
func (s *Server) localSeatMap(ctx context.Context, p partyRef) []screening.Seat {
	seated := s.seatedHere(ctx, p.Key)
	seats := []screening.Seat{{Seat: 0, ID: s.partyToken(p.Key, p.ST.UserID), Name: link.CleanName(s.partyHost(p.ST.UserID)), Seated: seated[p.ST.UserID]}}
	rows, err := s.db.QueryContext(ctx, `SELECT user_id FROM party_seated WHERE pkey = ? AND user_id <> ? ORDER BY seated_ms, user_id LIMIT ?`, p.Key, p.ST.UserID, screening.MaxSeats-1)
	if err != nil {
		return seats
	}
	defer rows.Close()
	for rows.Next() {
		var u int64
		if rows.Scan(&u) == nil {
			seats = append(seats, screening.Seat{Seat: len(seats), ID: s.partyToken(p.Key, u), Name: link.CleanName(s.partyHost(u)), Seated: true})
		}
	}
	return seats
}

// seatMap is a party's seat map as this Usherr knows it.
func (s *Server) seatMap(ctx context.Context, p partyRef) []screening.Seat {
	switch p.role() {
	case "host":
		return s.hostSeatMap(ctx, p.ST, p.Key)
	case "guest":
		var seats []screening.Seat
		if raw := s.partyState(ctx, p.Key).Roster; raw != "" {
			_ = json.Unmarshal([]byte(raw), &seats)
		}
		return seats
	}
	return s.localSeatMap(ctx, p)
}

// guestPeople are a guest booking's people who said I'm in, in the order
// they said so (the order their stubs' seats go in).
func (s *Server) guestPeople(ctx context.Context, r screeningRoom) []screening.LobbyPerson {
	out := []screening.LobbyPerson{}
	seated := s.seatedHere(ctx, r.SID)
	rows, err := s.db.QueryContext(ctx, `SELECT sp.user_id, COALESCE(NULLIF(TRIM(u.first_name), ''), u.username) FROM link_screening_people sp JOIN users u ON u.id = sp.user_id
		WHERE sp.screening_id = ? AND sp.answer = ? ORDER BY sp.at_ms, sp.user_id LIMIT ?`, r.InID, screening.Going, screening.MaxHousehold)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var u int64
		var name string
		if rows.Scan(&u, &name) == nil {
			out = append(out, screening.LobbyPerson{ID: s.partyToken(r.SID, u), Name: link.CleanName(name), Seated: seated[u]})
		}
	}
	return out
}

// sendLobbySeat tells the host who here is coming and who's seated (when it
// changed, or force).
func (s *Server) sendLobbySeat(ctx context.Context, r screeningRoom, force bool) error {
	n := s.linkNode()
	if n == nil || r.Role != "guest" {
		return nil
	}
	m := screening.LobbySeat{V: screening.Version, SID: r.SID, People: s.guestPeople(ctx, r)}
	key := "g" + r.SID
	if force {
		s.lobbyForget(key)
	}
	if !s.lobbyFresh(key, m) {
		return nil
	}
	if err := n.Send(ctx, r.HostID, screening.TypeLobbySeat, m); err != nil {
		s.lobbyForget(key)
		return err
	}
	return nil
}

// sendLobbyRoster sends the seat map of a screening hosted here to everyone
// at it (when it changed, or force).
func (s *Server) sendLobbyRoster(ctx context.Context, showtimeID int64, force bool) {
	n := s.linkNode()
	o, ok := s.screeningOutOf(ctx, showtimeID)
	st, ok2 := s.showtimeByID(ctx, showtimeID)
	if n == nil || !ok || !ok2 {
		return
	}
	var to []string
	for _, g := range s.screeningGuests(ctx, showtimeID) {
		to = append(to, g.Their)
	}
	if len(to) == 0 {
		return
	}
	seats := s.hostSeatMap(ctx, st, o.SID)
	key := "h" + strconv.FormatInt(showtimeID, 10)
	if force {
		s.lobbyForget(key)
	}
	if !s.lobbyFresh(key, seats) {
		return
	}
	m := screening.LobbyRoster{V: screening.Version, SID: o.SID, Seq: s.partySeq(ctx, o.SID, "roster_seq"), Seats: seats}
	for len(m.Seats) > 0 && !fits(m, screening.MaxBodySeatMap) {
		m.Seats = m.Seats[:len(m.Seats)*9/10]
	}
	if err := n.SendGroup(ctx, o.Key, to, screening.TypeLobbyRoster, m); err != nil {
		s.lobbyForget(key)
		slog.Debug("link: the seat map couldn't be sent", "err", err)
	}
}

// lobbyJob (the screening job, every minute) keeps the lobbies of the
// screenings starting soon up to date: the seat maps hosted here, and what
// guests here tell their hosts. It also sends again the votes that didn't
// reach a host.
func (s *Server) lobbyJob(ctx context.Context) {
	now := time.Now()
	lo, hi := now.Add(-4*time.Hour).UnixMilli(), now.Add(partyLobbyAhead).UnixMilli()
	for _, id := range s.int64List(ctx, `SELECT o.showtime_id FROM link_screenings_out o JOIN showtimes st ON st.id = o.showtime_id WHERE st.state <> 'done' AND st.at_ms BETWEEN ? AND ?`, lo, hi) {
		s.sendLobbyRoster(ctx, id, false)
	}
	for _, id := range s.int64List(ctx, `SELECT showtime_id FROM link_screenings_in WHERE showtime_id IS NOT NULL AND at_ms BETWEEN ? AND ?`, lo, hi) {
		if r, ok := s.roomOf(ctx, id); ok && r.Role == "guest" {
			_ = s.sendLobbySeat(ctx, r, false)
		}
	}
	type vote struct {
		key   string
		uid   int64
		stars int
		share bool
		name  string
	}
	var votes []vote
	if rows, err := s.db.QueryContext(ctx, `SELECT v.pkey, v.user_id, v.stars, v.share, v.name FROM party_verdicts v JOIN link_screenings_in i ON i.sid = v.pkey
		WHERE v.user_id > 0 AND v.sent_ms = 0 AND v.at_ms > ?`, now.Add(-partyVoteResend).UnixMilli()); err == nil {
		for rows.Next() {
			var v vote
			if rows.Scan(&v.key, &v.uid, &v.stars, &v.share, &v.name) == nil {
				votes = append(votes, v)
			}
		}
		rows.Close()
	}
	for _, v := range votes {
		if id, ok := s.showtimeOfSID(ctx, v.key); ok {
			if r, ok := s.roomOf(ctx, id); ok && r.Role == "guest" {
				_ = s.sendVote(ctx, r, v.uid, v.stars, v.share, v.name)
			}
		}
	}
}

// fits reports whether a message's body is within limit bytes.
func fits(body any, limit int) bool {
	raw, err := json.Marshal(body)
	return err == nil && len(raw) <= limit
}

func (s *Server) int64List(ctx context.Context, q string, args ...any) []int64 {
	var out []int64
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var n int64
		if rows.Scan(&n) == nil {
			out = append(out, n)
		}
	}
	return out
}

// ---- the intermission ----

// partyStatuses is everyone's status at a party's intermission, as kept by
// the host (or here, for a showtime of this Usherr's own).
func (s *Server) partyStatuses(ctx context.Context, key string) []screening.Status {
	out := []screening.Status{}
	rows, err := s.db.QueryContext(ctx, `SELECT name, house, status FROM party_status WHERE pkey = ? ORDER BY at_ms, who LIMIT ?`, key, screening.MaxStatuses)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var st screening.Status
		if rows.Scan(&st.Name, &st.House, &st.Status) == nil {
			out = append(out, st)
		}
	}
	return out
}

// interSend tells everyone at a screening hosted here about its
// intermission: when it's on (always, when force: it just changed).
func (s *Server) interSend(ctx context.Context, r screeningRoom, force bool) {
	n := s.linkNode()
	if n == nil || r.Role != "host" {
		return
	}
	ps := s.partyState(ctx, r.SID)
	if !ps.InterOn && !force {
		return
	}
	var to []string
	for _, g := range s.screeningGuests(ctx, r.ShowtimeID) {
		to = append(to, g.Their)
	}
	if len(to) == 0 {
		return
	}
	m := screening.Intermission{V: screening.Version, SID: r.SID, Seq: max(ps.InterSeq, 1), On: ps.InterOn}
	if ps.InterOn {
		m.Until = ps.InterUntil + s.relaySkew("")
		m.Statuses = s.partyStatuses(ctx, r.SID)
		for len(m.Statuses) > 0 && !fits(m, screening.MaxBodyInter) {
			m.Statuses = m.Statuses[:len(m.Statuses)*9/10]
		}
	}
	if err := n.SendGroup(ctx, r.Key, to, screening.TypeIntermission, m); err != nil {
		slog.Debug("link: the intermission couldn't be sent", "err", err)
	}
}

// interSet turns a screening's intermission on (until then) or off.
func (s *Server) interSet(ctx context.Context, key string, on bool, until int64) error {
	s.partyStateRow(ctx, key)
	seq := max(s.partyState(ctx, key).InterSeq+1, time.Now().UnixMilli())
	if !on {
		until = 0
		_, _ = s.db.ExecContext(ctx, `DELETE FROM party_status WHERE pkey = ?`, key)
	}
	_, err := s.db.ExecContext(ctx, `UPDATE party_state SET inter_seq = ?, inter_on = ?, inter_until = ?, statuses = '[]' WHERE pkey = ?`, seq, b2i(on), until, key)
	return err
}

// ---- the verdict ----

type partyTally struct {
	Count  int
	Total  int
	Shared []screening.Shared
}

// tallyHere is the verdict from the votes kept here (the host's, or a
// showtime of this Usherr's own).
func (s *Server) tallyHere(ctx context.Context, key string) partyTally {
	t := partyTally{Shared: []screening.Shared{}}
	rows, err := s.db.QueryContext(ctx, `SELECT stars, share, name, house FROM party_verdicts WHERE pkey = ? ORDER BY at_ms, voter LIMIT ?`, key, screening.MaxVoters)
	if err != nil {
		return t
	}
	defer rows.Close()
	for rows.Next() {
		var stars, share int
		var name, house string
		if rows.Scan(&stars, &share, &name, &house) != nil || stars < 1 || stars > 10 {
			continue
		}
		t.Count++
		t.Total += stars
		if share == 1 && name != "" && len(t.Shared) < screening.MaxShared {
			t.Shared = append(t.Shared, screening.Shared{Name: name, House: house, Stars: stars})
		}
	}
	return t
}

// partyTallyOf is a party's verdict as this Usherr knows it: a guest's is
// the host's last word (its own votes until one comes).
func (s *Server) partyTallyOf(ctx context.Context, p partyRef) partyTally {
	if p.role() == "guest" {
		ps := s.partyState(ctx, p.Key)
		var m screening.VerdictTally
		if ps.TallySeq > 0 && json.Unmarshal([]byte(ps.Tally), &m) == nil {
			if m.Shared == nil {
				m.Shared = []screening.Shared{}
			}
			return partyTally{Count: m.Count, Total: m.Total, Shared: m.Shared}
		}
	}
	return s.tallyHere(ctx, p.Key)
}

// tallySend tells everyone at a screening hosted here the crew's verdict.
func (s *Server) tallySend(ctx context.Context, r screeningRoom) {
	n := s.linkNode()
	if n == nil || r.Role != "host" {
		return
	}
	var to []string
	for _, g := range s.screeningGuests(ctx, r.ShowtimeID) {
		to = append(to, g.Their)
	}
	if len(to) == 0 {
		return
	}
	t := s.tallyHere(ctx, r.SID)
	m := screening.VerdictTally{V: screening.Version, SID: r.SID, Seq: s.partySeq(ctx, r.SID, "tally_seq"), Count: t.Count, Total: t.Total, Shared: t.Shared}
	for len(m.Shared) > 0 && !fits(m, screening.MaxBodyTally) {
		m.Shared = m.Shared[:len(m.Shared)*9/10]
	}
	if err := n.SendGroup(ctx, r.Key, to, screening.TypeVerdictTally, m); err != nil {
		slog.Debug("link: the verdict couldn't be sent", "err", err)
	}
}

// keepVote keeps someone's stars for a party.
func (s *Server) keepVote(ctx context.Context, key, voter string, uid int64, tmdb int, name, house string, stars int, line string, share bool, sent int64) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO party_verdicts (pkey, voter, user_id, tmdb_id, name, house, stars, line, share, at_ms, sent_ms) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (pkey, voter) DO UPDATE SET stars = excluded.stars, line = excluded.line, share = excluded.share, name = excluded.name, house = excluded.house, sent_ms = excluded.sent_ms`,
		key, voter, uid, tmdb, name, house, stars, line, b2i(share), time.Now().UnixMilli(), sent)
	return err
}

// sendVote sends someone here's stars to the host (their name only when
// they share).
func (s *Server) sendVote(ctx context.Context, r screeningRoom, uid int64, stars int, share bool, name string) error {
	n := s.linkNode()
	if n == nil {
		return errors.New(linkOffText)
	}
	m := screening.VerdictVote{V: screening.Version, SID: r.SID, ID: s.partyToken(r.SID, uid), Stars: stars, Share: share}
	if share {
		m.Name = name
	}
	if err := n.Send(ctx, r.HostID, screening.TypeVerdictVote, m); err != nil {
		return err
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE party_verdicts SET sent_ms = ? WHERE pkey = ? AND user_id = ?`, time.Now().UnixMilli(), r.SID, uid)
	return nil
}

// verdictOpen: the credits are rolling (a linked screening ended; a
// showtime of this Usherr's own was ended by its host, or is past its end).
func (s *Server) verdictOpen(ctx context.Context, p partyRef, now int64) bool {
	if p.Room != nil {
		return p.Room.now(now).State == screening.StateEnded
	}
	if s.partyState(ctx, p.Key).CreditsMs > 0 {
		return true
	}
	return p.ST.State == "done" || now >= creditsAt(p.ST)
}

// verdictToDiary puts someone's stars (and line) on their Diary entry for
// the film, the day of the showtime: as rating it from the Diary does.
func (s *Server) verdictToDiary(ctx context.Context, uid int64, st showtime, stars int, line string) error {
	if st.TMDBID <= 0 {
		return nil
	}
	day := time.UnixMilli(st.AtMs).Local().Format("2006-01-02")
	if err := s.diaryPut(ctx, uid, diaryEntry{Kind: "movie", TMDBID: st.TMDBID, Title: st.Title, Year: st.Year, WatchedOn: day, Dated: true, Runtime: st.Runtime, Source: "manual"}); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE home_diary SET stars = ?, note = CASE WHEN ? <> '' THEN ? ELSE note END, hidden = 0
		WHERE user_id = ? AND kind = 'movie' AND tmdb_id = ? AND watched_on = ?`, stars, line, line, uid, st.TMDBID, day); err != nil {
		return err
	}
	s.markRewatches(ctx, uid)
	tasteChanged(uid)
	return nil
}

// verdictReview posts someone's stars and line as their review, as the
// film's page does; what went wrong, or "".
func (s *Server) verdictReview(ctx context.Context, u *auth.User, st showtime, stars int, line string) string {
	cl := s.communityLinkOf(u.ID)
	ext := s.myDiscordID(ctx, u.ID)
	if cl == nil || ext == "" {
		return "Link your Discord account first (Settings, Linked services) to post it as your review."
	}
	title, year, poster := s.filmFacts(ctx, "movie", st.TMDBID)
	if title == "" {
		title, year, poster = st.Title, st.Year, st.PosterURL
	}
	rctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if _, err := s.saveReview(rctx, review{Kind: "movie", TMDBID: st.TMDBID, ExternalID: ext, UserID: u.ID, Name: cl.Name, Avatar: cl.Avatar,
		Stars: stars, Text: line, Title: title, Year: year, Poster: poster, Source: "usherr"}); err != nil {
		if errors.Is(err, errCommunityOff) {
			return "Community isn't connected, so it wasn't posted as a review."
		}
		return communityProblem(err, "in the Community channel")
	}
	return ""
}

// ---- what the messages do (screening.Party) ----

func (s *Server) hostedShowtimeOf(ctx context.Context, sid string) (int64, bool) {
	var id int64
	if s.db.QueryRowContext(ctx, `SELECT showtime_id FROM link_screenings_out WHERE sid = ?`, sid).Scan(&id) != nil {
		return 0, false
	}
	return id, true
}

// LobbySeatIn: a guest Usherr's people at the lobby. Kept; the seat map
// goes to everyone.
func (b linkBook) LobbySeatIn(ctx context.Context, guest link.Peer, m screening.LobbySeat) error {
	s := b.s
	peer, ok := s.peerRow(ctx, guest.ID)
	id, ok2 := s.hostedShowtimeOf(ctx, m.SID)
	if !ok || !ok2 {
		return screening.ErrNotAllowed
	}
	people := m.People
	if people == nil {
		people = []screening.LobbyPerson{}
	}
	raw, _ := json.Marshal(people)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO party_guest_people (sid, peer_id, people, at_ms) VALUES (?, ?, ?, ?)
		ON CONFLICT (sid, peer_id) DO UPDATE SET people = excluded.people, at_ms = excluded.at_ms`, m.SID, peer, string(raw), time.Now().UnixMilli()); err != nil {
		return err
	}
	s.partyChanged(id)
	s.screeningRosterSoon(id)
	return nil
}

// StatusIn: someone at a guest Usherr says what they're up to at the
// intermission.
func (b linkBook) StatusIn(ctx context.Context, guest link.Peer, m screening.RoomStatus) error {
	s := b.s
	peer, ok := s.peerRow(ctx, guest.ID)
	r, ok2 := s.hostRoomBySID(ctx, m.SID)
	if !ok || !ok2 {
		return screening.ErrNotAllowed
	}
	if !s.partyState(ctx, m.SID).InterOn {
		return nil
	}
	who := "p" + strconv.FormatInt(peer, 10) + ":" + m.ID
	if err := s.keepStatus(ctx, m.SID, who, m.Name, s.linkPeerName(ctx, peer), m.Status); err != nil {
		return err
	}
	s.partyChanged(r.ShowtimeID)
	s.later(func(ctx context.Context) { s.interSend(ctx, r, false) })
	return nil
}

func (s *Server) keepStatus(ctx context.Context, key, who, name, house, status string) error {
	if status == "" {
		_, err := s.db.ExecContext(ctx, `DELETE FROM party_status WHERE pkey = ? AND who = ?`, key, who)
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO party_status (pkey, who, name, house, status, at_ms) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (pkey, who) DO UPDATE SET name = excluded.name, house = excluded.house, status = excluded.status`, key, who, name, link.CleanName(house), status, time.Now().UnixMilli())
	return err
}

// VoteIn: someone at a guest Usherr gave the film their stars. Kept; the
// verdict goes to everyone.
func (b linkBook) VoteIn(ctx context.Context, guest link.Peer, m screening.VerdictVote) error {
	s := b.s
	peer, ok := s.peerRow(ctx, guest.ID)
	r, ok2 := s.hostRoomBySID(ctx, m.SID)
	if !ok || !ok2 {
		return screening.ErrNotAllowed
	}
	house := ""
	if m.Share {
		house = link.CleanName(s.linkPeerName(ctx, peer))
	}
	voter := "p" + strconv.FormatInt(peer, 10) + ":" + m.ID
	if err := s.keepVote(ctx, m.SID, voter, 0, s.tmdbOfSID(ctx, m.SID), m.Name, house, m.Stars, "", m.Share, 0); err != nil {
		return err
	}
	s.partyChanged(r.ShowtimeID)
	s.later(func(ctx context.Context) { s.tallySend(ctx, r) })
	return nil
}

// LobbyRosterIn: the host's seat map.
func (b linkBook) LobbyRosterIn(ctx context.Context, _ link.Peer, m screening.LobbyRoster) error {
	s := b.s
	s.partyStateRow(ctx, m.SID)
	seats := m.Seats
	if seats == nil {
		seats = []screening.Seat{}
	}
	raw, _ := json.Marshal(seats)
	res, err := s.db.ExecContext(ctx, `UPDATE party_state SET roster = ?, roster_seq = ? WHERE pkey = ? AND roster_seq < ?`, string(raw), m.Seq, m.SID, m.Seq)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		if id, ok := s.showtimeOfSID(ctx, m.SID); ok {
			s.partyChanged(id)
		}
	}
	return nil
}

// IntermissionIn: the host's intermission, on or over.
func (b linkBook) IntermissionIn(ctx context.Context, host link.Peer, m screening.Intermission) error {
	s := b.s
	s.partyStateRow(ctx, m.SID)
	until := int64(0)
	if m.On {
		until = m.Until - s.relaySkew(host.Relay)
	}
	statuses := m.Statuses
	if statuses == nil {
		statuses = []screening.Status{}
	}
	raw, _ := json.Marshal(statuses)
	res, err := s.db.ExecContext(ctx, `UPDATE party_state SET inter_seq = ?, inter_on = ?, inter_until = ?, statuses = ? WHERE pkey = ? AND inter_seq <= ?`,
		m.Seq, b2i(m.On), until, string(raw), m.SID, m.Seq)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		if !m.On {
			_, _ = s.db.ExecContext(ctx, `DELETE FROM party_status WHERE pkey = ?`, m.SID)
		}
		if id, ok := s.showtimeOfSID(ctx, m.SID); ok {
			s.partyChanged(id)
		}
	}
	return nil
}

// TallyIn: the crew's verdict, from the host.
func (b linkBook) TallyIn(ctx context.Context, _ link.Peer, m screening.VerdictTally) error {
	s := b.s
	s.partyStateRow(ctx, m.SID)
	raw, _ := json.Marshal(m)
	res, err := s.db.ExecContext(ctx, `UPDATE party_state SET tally = ?, tally_seq = ? WHERE pkey = ? AND tally_seq < ?`, string(raw), m.Seq, m.SID, m.Seq)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		if id, ok := s.showtimeOfSID(ctx, m.SID); ok {
			s.partyChanged(id)
		}
	}
	return nil
}

// ---- the page ----

type partySeatOut struct {
	Seat   int    `json:"seat"`
	Name   string `json:"name,omitempty"`
	House  string `json:"house,omitempty"`
	Seated bool   `json:"seated"`
	Me     bool   `json:"me,omitempty"`
}

type partyPreshowItem struct {
	Kind    string  `json:"kind"`           // card, clip, trailer
	Card    string  `json:"card,omitempty"` // a card's kind
	Title   string  `json:"title,omitempty"`
	Seconds float64 `json:"seconds"`
}

type partyLobbyOut struct {
	Seats   []partySeatOut     `json:"seats"`
	CanSit  bool               `json:"canSit"` // said I'm in (or a showtime of this Usherr's own)
	Seated  bool               `json:"seated"`
	Preshow []partyPreshowItem `json:"preshow"` // the pre-show, piece by piece, when it's fixed
}

type partyInterOut struct {
	On       bool               `json:"on"`
	UntilMs  int64              `json:"untilMs,omitempty"` // this Usherr's clock
	Statuses []screening.Status `json:"statuses"`
	Mine     string             `json:"mine,omitempty"`
}

type partyVerdictMine struct {
	Stars int    `json:"stars"`
	Line  string `json:"line,omitempty"`
	Share bool   `json:"share"`
}

type partyVerdictOut struct {
	Open      bool               `json:"open"`
	Mine      *partyVerdictMine  `json:"mine"`
	Count     int                `json:"count"`
	Average   float64            `json:"average"` // out of 5
	Shared    []screening.Shared `json:"shared"`
	Top       *reactMoment       `json:"top"`
	Stub      bool               `json:"stub"`      // you have a stub for it
	CanReview bool               `json:"canReview"` // it can be posted as your review too
}

type partyOut struct {
	Role         string          `json:"role"` // host, guest, local
	ShowtimeID   int64           `json:"showtimeId"`
	AtMs         int64           `json:"atMs"`
	CanHost      bool            `json:"canHost"` // runs the intermission and the credits
	Lobby        partyLobbyOut   `json:"lobby"`
	Intermission partyInterOut   `json:"intermission"`
	Verdict      partyVerdictOut `json:"verdict"`
	Statuses     []string        `json:"statusChoices"`
	NowMs        int64           `json:"nowMs"`
}

func (s *Server) partyAnswer(ctx context.Context, p partyRef, req *http.Request) partyOut {
	now := time.Now().UnixMilli()
	uid := requestUserID(req)
	me := s.partyToken(p.Key, uid)
	out := partyOut{Role: p.role(), ShowtimeID: p.ST.ID, AtMs: p.ST.AtMs, CanHost: s.partyCanHost(req, p), Statuses: screening.Statuses, NowMs: now}

	// The lobby.
	seats := s.seatMap(ctx, p)
	out.Lobby.Seats = make([]partySeatOut, 0, len(seats))
	for _, x := range seats {
		o := partySeatOut{Seat: x.Seat, Name: x.Name, House: x.House, Seated: x.Seated, Me: x.ID != "" && x.ID == me}
		if o.Me {
			out.Lobby.Seated = o.Seated
		}
		out.Lobby.Seats = append(out.Lobby.Seats, o)
	}
	switch p.role() {
	case "host":
		out.Lobby.CanSit = s.hostHousehold(ctx, p.ST)[uid] == screening.Going
		if plan, ok := s.screeningPlan(ctx, p.ST.ID, false); ok {
			for _, st := range plan.Steps {
				out.Lobby.Preshow = append(out.Lobby.Preshow, partyPreshowItem{Kind: st.Kind, Card: st.Card, Title: st.Title, Seconds: st.Seconds})
			}
		}
	case "guest":
		var one int
		out.Lobby.CanSit = s.db.QueryRowContext(ctx, `SELECT 1 FROM link_screening_people WHERE screening_id = ? AND user_id = ? AND answer = ?`, p.Room.InID, uid, screening.Going).Scan(&one) == nil
		var items []screening.RecipeItem
		if p.Room.Recipe != "" && json.Unmarshal([]byte(p.Room.Recipe), &items) == nil {
			for _, it := range items {
				out.Lobby.Preshow = append(out.Lobby.Preshow, partyPreshowItem{Kind: it.Kind, Card: it.Card, Title: firstNonEmptyStr(it.Title, it.Text, it.Category), Seconds: it.Seconds})
			}
		}
		if len(seats) == 0 { // no seat map from the host yet: its own people
			for i, pp := range s.guestPeople(ctx, *p.Room) {
				out.Lobby.Seats = append(out.Lobby.Seats, partySeatOut{Seat: -1 - i, Name: pp.Name, Seated: pp.Seated, Me: pp.ID == me})
				if pp.ID == me {
					out.Lobby.Seated = pp.Seated
				}
			}
		}
	default:
		out.Lobby.CanSit = true
	}
	if out.Lobby.Preshow == nil {
		out.Lobby.Preshow = []partyPreshowItem{}
	}

	// The intermission.
	ps := s.partyState(ctx, p.Key)
	out.Intermission = partyInterOut{On: ps.InterOn, Statuses: []screening.Status{}}
	if ps.InterOn {
		out.Intermission.UntilMs = ps.InterUntil
		if p.role() == "guest" {
			_ = json.Unmarshal([]byte(ps.Statuses), &out.Intermission.Statuses)
		} else {
			out.Intermission.Statuses = s.partyStatuses(ctx, p.Key)
		}
		_ = s.db.QueryRowContext(ctx, `SELECT status FROM party_status WHERE pkey = ? AND who = ?`, p.Key, "u"+strconv.FormatInt(uid, 10)).Scan(&out.Intermission.Mine)
	}

	// The verdict.
	v := partyVerdictOut{Open: s.verdictOpen(ctx, p, now), Shared: []screening.Shared{}}
	var mine partyVerdictMine
	var share int
	if s.db.QueryRowContext(ctx, `SELECT stars, line, share FROM party_verdicts WHERE pkey = ? AND user_id = ?`, p.Key, uid).Scan(&mine.Stars, &mine.Line, &share) == nil {
		mine.Share = share == 1
		v.Mine = &mine
	}
	t := s.partyTallyOf(ctx, p)
	v.Count, v.Shared = t.Count, t.Shared
	if t.Count > 0 {
		v.Average = math.Round(float64(t.Total)/float64(t.Count)/2*10) / 10
	}
	if p.Room != nil && v.Open {
		if top, ok := s.screeningTopMoment(ctx, p.Key); ok {
			v.Top = &top
		}
	}
	var one int
	v.Stub = s.db.QueryRowContext(ctx, `SELECT 1 FROM community_stubs WHERE backend = 'here' AND external_id = ? AND (showtime_id = ? OR (sid <> '' AND sid = ?)) LIMIT 1`,
		strconv.FormatInt(uid, 10), p.ST.ID, p.Key).Scan(&one) == nil
	_, _, connected := s.communityCanShare()
	v.CanReview = connected && s.myDiscordID(ctx, uid) != ""
	out.Verdict = v
	return out
}

func (s *Server) partyFor(w http.ResponseWriter, r *http.Request) (partyRef, bool) {
	id, ok := pathID(r)
	if ok {
		if p, ok := s.partyAt(r.Context(), id); ok {
			return p, true
		}
	}
	writeError(w, http.StatusNotFound, "That showtime isn't known.")
	return partyRef{}, false
}

// GET /api/home/showtimes/{id}/party (member): a showtime's party: the
// lobby's seats and pre-show, the intermission, the verdict.
func (s *Server) handleParty(w http.ResponseWriter, r *http.Request) {
	p, ok := s.partyFor(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, s.partyAnswer(r.Context(), p, r))
}

// GET /api/home/showtimes/{id}/party/events (member): "changed" whenever
// the party (or a linked screening's room) changes.
func (s *Server) handlePartyEvents(w http.ResponseWriter, r *http.Request) {
	p, ok := s.partyFor(w, r)
	if !ok {
		return
	}
	s.roomEventStream(w, r, p.ST.ID)
}

// POST /api/home/showtimes/{id}/party/seat {"seated": bool} (member): Take
// my seat (or leave it).
func (s *Server) handlePartySeat(w http.ResponseWriter, r *http.Request) {
	p, ok := s.partyFor(w, r)
	if !ok {
		return
	}
	var body struct {
		Seated bool `json:"seated"`
	}
	if decodeJSON(r, &body) != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	ctx := r.Context()
	uid := requestUserID(r)
	out := s.partyAnswer(ctx, p, r)
	if !out.Lobby.CanSit {
		writeError(w, http.StatusConflict, "Say I’m in first, then take your seat.")
		return
	}
	var err error
	if body.Seated {
		_, err = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO party_seated (pkey, user_id, seated_ms) VALUES (?, ?, ?)`, p.Key, uid, time.Now().UnixMilli())
	} else {
		_, err = s.db.ExecContext(ctx, `DELETE FROM party_seated WHERE pkey = ? AND user_id = ?`, p.Key, uid)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "That couldn't be saved.")
		return
	}
	switch p.role() {
	case "host":
		s.screeningRosterSoon(p.ST.ID)
	case "guest":
		// Here at once; the host's seat map follows.
		me := s.partyToken(p.Key, uid)
		ps := s.partyState(ctx, p.Key)
		var seats []screening.Seat
		if ps.Roster != "" && json.Unmarshal([]byte(ps.Roster), &seats) == nil {
			for i := range seats {
				if seats[i].ID == me {
					seats[i].Seated = body.Seated
				}
			}
			raw, _ := json.Marshal(seats)
			_, _ = s.db.ExecContext(ctx, `UPDATE party_state SET roster = ? WHERE pkey = ?`, string(raw), p.Key)
		}
		room := *p.Room
		s.later(func(ctx context.Context) { _ = s.sendLobbySeat(ctx, room, true) })
	}
	s.partyChanged(p.ST.ID)
	writeJSON(w, http.StatusOK, s.partyAnswer(ctx, p, r))
}

// POST /api/home/showtimes/{id}/party/intermission {"action": "start"|"more"|"end"}
// (member; the host): an intermission for everyone at a linked screening:
// the film pauses, a 10-minute countdown (5 more each time), then the host
// resumes it.
func (s *Server) handlePartyIntermission(w http.ResponseWriter, r *http.Request) {
	p, ok := s.partyFor(w, r)
	if !ok {
		return
	}
	var body struct {
		Action string `json:"action"`
	}
	if decodeJSON(r, &body) != nil || (body.Action != "start" && body.Action != "more" && body.Action != "end") {
		writeError(w, http.StatusBadRequest, "Say start, more or end.")
		return
	}
	if p.role() != "host" {
		writeError(w, http.StatusConflict, "An intermission is for a linked screening hosted here.")
		return
	}
	if !s.partyCanHost(r, p) {
		writeError(w, http.StatusForbidden, "Whoever booked it (or an administrator) calls the intermission.")
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 20*time.Second)
	defer cancel()
	room := *p.Room
	now := time.Now()
	cur := room.now(now.UnixMilli())
	ps := s.partyState(ctx, p.Key)
	var err error
	switch body.Action {
	case "start":
		if cur.State != screening.StateFilm && cur.State != screening.StatePaused {
			writeError(w, http.StatusConflict, "The intermission is for once the film is on.")
			return
		}
		if err = s.interSet(ctx, p.Key, true, now.Add(screening.DefaultInterMins*time.Minute).UnixMilli()); err == nil && cur.State == screening.StateFilm {
			_, err = s.roomSet(ctx, room, screening.StatePaused, cur.Position)
		}
	case "more":
		if !ps.InterOn {
			writeError(w, http.StatusConflict, "There's no intermission on.")
			return
		}
		until := min(max(ps.InterUntil, now.UnixMilli())+(screening.MoreInterMins*time.Minute).Milliseconds(), now.Add(screening.MaxInterMinutes*time.Minute).UnixMilli())
		_, err = s.db.ExecContext(ctx, `UPDATE party_state SET inter_seq = ?, inter_until = ? WHERE pkey = ?`, max(ps.InterSeq+1, now.UnixMilli()), until, p.Key)
	case "end":
		if err = s.interSet(ctx, p.Key, false, 0); err == nil && cur.State == screening.StatePaused {
			_, err = s.roomSet(ctx, room, screening.StateFilm, cur.Position)
		}
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "That couldn't be saved.")
		return
	}
	s.later(func(ctx context.Context) { s.interSend(ctx, room, true) })
	s.partyChanged(p.ST.ID)
	if q, ok := s.partyAt(ctx, p.ST.ID); ok {
		p = q
	}
	writeJSON(w, http.StatusOK, s.partyAnswer(ctx, p, r))
}

// PUT /api/home/showtimes/{id}/party/status {"status": "snacks"|"drinks"|"break"|"back"|""}
// (member): what you're up to at the intermission, for everyone to see.
func (s *Server) handlePartyStatus(w http.ResponseWriter, r *http.Request) {
	p, ok := s.partyFor(w, r)
	if !ok {
		return
	}
	var body struct {
		Status string `json:"status"`
	}
	if decodeJSON(r, &body) != nil || !screening.ValidStatus(body.Status) {
		writeError(w, http.StatusBadRequest, "Pick one of the statuses.")
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 20*time.Second)
	defer cancel()
	if p.Room == nil || !s.partyState(ctx, p.Key).InterOn {
		writeError(w, http.StatusConflict, "There's no intermission on.")
		return
	}
	uid := requestUserID(r)
	name := link.CleanName(s.partyHost(uid))
	if err := s.keepStatus(ctx, p.Key, "u"+strconv.FormatInt(uid, 10), name, s.linkName(), body.Status); err != nil {
		writeError(w, http.StatusInternalServerError, "That couldn't be saved.")
		return
	}
	room := *p.Room
	if room.Role == "host" {
		s.later(func(ctx context.Context) { s.interSend(ctx, room, false) })
	} else {
		n := s.linkNode()
		if n == nil {
			writeError(w, http.StatusConflict, linkOffText)
			return
		}
		if err := n.Send(ctx, room.HostID, screening.TypeRoomStatus, screening.RoomStatus{V: screening.Version, SID: room.SID, ID: s.partyToken(room.SID, uid), Name: name, Status: body.Status}); err != nil {
			writeError(w, http.StatusBadGateway, "The host couldn't be reached right now. Try again in a moment.")
			return
		}
	}
	s.partyChanged(p.ST.ID)
	writeJSON(w, http.StatusOK, s.partyAnswer(ctx, p, r))
}

// POST /api/home/showtimes/{id}/party/credits (member; the host): roll the
// credits: the film ends for everyone and the verdict opens.
func (s *Server) handlePartyCredits(w http.ResponseWriter, r *http.Request) {
	p, ok := s.partyFor(w, r)
	if !ok {
		return
	}
	if !s.partyCanHost(r, p) {
		writeError(w, http.StatusForbidden, "Whoever booked it (or an administrator) rolls the credits.")
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 20*time.Second)
	defer cancel()
	var err error
	if p.Room != nil {
		room := *p.Room
		cur := room.now(time.Now().UnixMilli())
		if s.partyState(ctx, p.Key).InterOn {
			if err = s.interSet(ctx, p.Key, false, 0); err == nil {
				s.later(func(ctx context.Context) { s.interSend(ctx, room, true) })
			}
		}
		if err == nil && cur.State != screening.StateEnded {
			_, err = s.roomSet(ctx, room, screening.StateEnded, math.Max(0, cur.Position))
		}
	} else {
		s.partyStateRow(ctx, p.Key)
		_, err = s.db.ExecContext(ctx, `UPDATE party_state SET credits_ms = ? WHERE pkey = ? AND credits_ms = 0`, time.Now().UnixMilli(), p.Key)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "That couldn't be saved.")
		return
	}
	s.partyChanged(p.ST.ID)
	if q, ok := s.partyAt(ctx, p.ST.ID); ok {
		p = q
	}
	writeJSON(w, http.StatusOK, s.partyAnswer(ctx, p, r))
}

// POST /api/home/showtimes/{id}/party/verdict {"stars": 1-10, "line"?, "share"?, "review"?}
// (member): your verdict at the credits. Saved to your Diary (and posted as
// your review when you ask); the crew sees your stars in the average, and
// your name with them only when you share.
func (s *Server) handlePartyVerdict(w http.ResponseWriter, r *http.Request) {
	p, ok := s.partyFor(w, r)
	if !ok {
		return
	}
	u := auth.UserFromContext(r.Context())
	if u == nil {
		writeError(w, http.StatusUnauthorized, "Sign in first.")
		return
	}
	var body struct {
		Stars  int    `json:"stars"`
		Line   string `json:"line"`
		Share  bool   `json:"share"`
		Review bool   `json:"review"`
	}
	if decodeJSON(r, &body) != nil || body.Stars < 1 || body.Stars > 10 {
		writeError(w, http.StatusBadRequest, "Give it between half a star and five stars.")
		return
	}
	if len([]rune(body.Line)) > partyLineMax+20 {
		writeError(w, http.StatusBadRequest, "Keep it to one short line.")
		return
	}
	line := screening.CleanChat(body.Line)
	if rs := []rune(line); len(rs) > partyLineMax {
		line = strings.TrimSpace(string(rs[:partyLineMax]))
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 30*time.Second)
	defer cancel()
	if !s.verdictOpen(ctx, p, time.Now().UnixMilli()) {
		writeError(w, http.StatusConflict, "The verdict opens at the credits.")
		return
	}
	if err := s.verdictToDiary(ctx, u.ID, p.ST, body.Stars, line); err != nil {
		writeError(w, http.StatusInternalServerError, "Your stars couldn't be saved to your Diary.")
		return
	}
	name := link.CleanName(s.partyHost(u.ID))
	house := ""
	if p.Room != nil && body.Share {
		house = link.CleanName(s.linkName())
	}
	shownName := ""
	if body.Share {
		shownName = name
	}
	if err := s.keepVote(ctx, p.Key, "u"+strconv.FormatInt(u.ID, 10), u.ID, p.ST.TMDBID, shownName, house, body.Stars, line, body.Share, 0); err != nil {
		writeError(w, http.StatusInternalServerError, "That couldn't be saved.")
		return
	}
	var notes []string
	if body.Review {
		if problem := s.verdictReview(ctx, u, p.ST, body.Stars, line); problem != "" {
			notes = append(notes, problem)
		}
	}
	switch p.role() {
	case "host":
		room := *p.Room
		s.later(func(ctx context.Context) { s.tallySend(ctx, room) })
	case "guest":
		if err := s.sendVote(ctx, *p.Room, u.ID, body.Stars, body.Share, name); err != nil {
			notes = append(notes, "The host couldn't be reached right now. Your stars are saved, and go to the crew when it's back.")
		}
	}
	s.partyChanged(p.ST.ID)
	out := s.partyAnswer(ctx, p, r)
	writeJSON(w, http.StatusOK, map[string]any{"party": out, "notes": notesOrEmpty(notes)})
}

func notesOrEmpty(n []string) []string {
	if n == nil {
		return []string{}
	}
	return n
}

// roomEventStream sends "changed" to a browser whenever a showtime's room
// or party changes.
func (s *Server) roomEventStream(w http.ResponseWriter, r *http.Request, showtimeID int64) {
	rc := http.NewResponseController(w)
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	hub := &s.linkS.scr.room
	ch := hub.subscribe(showtimeID)
	defer hub.unsubscribe(showtimeID, ch)
	write := func(line string) bool {
		if _, err := fmt.Fprint(w, line); err != nil {
			return false
		}
		return rc.Flush() == nil
	}
	if !write("retry: 3000\n: hello\n\n") {
		return
	}
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			if !write(": ping\n\n") {
				return
			}
		case <-ch:
			if !write("data: {\"t\":\"changed\"}\n\n") {
				return
			}
		}
	}
}
