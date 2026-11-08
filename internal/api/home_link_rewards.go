package api

// Usherr: linked screenings, step 5. Rewards over the link: at the door of a
// screening shared with linked Usherrs, everyone in the host's household
// who's coming gets a ticket stub here, and each linked Usherr that's coming
// gets a block of seats, one stub for each of its people, numbered by the
// host (reward.stub). A stamp made at a guest's house (Stamp it, or its
// player at 90%) is sent to the host (reward.stamp), which answers with the
// stamps it holds for that Usherr, so both sides agree. Someone who says
// they're in once the film has started gets a stub then.
//
// What the messages may do is decided in internal/link/rewards (only the
// host hands out stubs and confirms stamps; a guest only stamps its own
// seats). Nothing is shared beyond the screening's own Usherrs.

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/link"
	"github.com/rdborg/mediarium/internal/link/rewards"
	"github.com/rdborg/mediarium/internal/link/screening"
)

const (
	rewardResendFor  = 12 * time.Hour   // unsent stubs are tried again this long
	rewardReportWait = 10 * time.Minute // a stamp not yet confirmed is sent again this often
	rewardReportFor  = 48 * time.Hour   // ... for this long
)

// sqlNextStubNo is the next stub number this Usherr hands out: one more than
// the highest it gave anyone, here or at a linked Usherr.
const sqlNextStubNo = `(SELECT COALESCE(MAX(n), 0) + 1 FROM (SELECT MAX(serial) AS n FROM community_stubs WHERE issuer = ''
	UNION ALL SELECT MAX(first_no + count - 1) FROM reward_seats))`

// stubStampUntil is when a party's stubs can no longer be stamped by hand:
// half an hour after its film's end.
func stubStampUntil(st showtime) int64 {
	return creditsAt(st) + stampFor.Milliseconds()
}

// ---- at the door ----

// rewardDoor runs when a showtime's film starts: who was invited is kept
// (badges), and a screening shared with linked Usherrs hands out its stubs.
// It can run again: who already has a stub keeps it, and whoever came since
// gets one.
func (s *Server) rewardDoor(ctx context.Context, st showtime) {
	s.rewardInvites(ctx, st)
	if o, ok := s.screeningOutOf(ctx, st.ID); ok {
		s.linkedDoor(ctx, st, o)
	}
}

// rewardLate hands out the stubs of someone who says they're in once a
// shared screening's film has started.
func (s *Server) rewardLate(showtimeID int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	st, ok := s.showtimeByID(ctx, showtimeID)
	if !ok || st.State != "playing" || time.Now().UnixMilli() < st.AtMs || time.Now().UnixMilli() > creditsAt(st) {
		return
	}
	s.rewardDoor(ctx, st)
}

// rewardInvites keeps who was invited to a party (anyone who answered, and
// whoever booked it), for the Perfect attendance badge. A showtime that
// isn't a party (nobody else asked) keeps nothing.
func (s *Server) rewardInvites(ctx context.Context, st showtime) {
	var people []string
	add := func(q string, prefix string, args ...any) {
		rows, err := s.db.QueryContext(ctx, q, args...)
		if err != nil {
			return
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if rows.Scan(&id) == nil && id != "" {
				people = append(people, prefix+id)
			}
		}
	}
	add(`SELECT external_id FROM community_party_people WHERE showtime_id = ?`, "discord:", st.ID)
	add(`SELECT CAST(user_id AS TEXT) FROM link_screening_host_people WHERE showtime_id = ?`, "here:", st.ID)
	add(`SELECT CAST(sp.user_id AS TEXT) FROM link_screening_people sp JOIN link_screenings_in i ON i.id = sp.screening_id WHERE i.showtime_id = ?`, "here:", st.ID)
	var one int
	party := len(people) > 0 || s.isJoined(ctx, st.ID) ||
		s.db.QueryRowContext(ctx, `SELECT 1 FROM link_screenings_out WHERE showtime_id = ?`, st.ID).Scan(&one) == nil ||
		s.db.QueryRowContext(ctx, `SELECT 1 FROM community_parties WHERE showtime_id = ?`, st.ID).Scan(&one) == nil
	if !party {
		return
	}
	people = append(people, "here:"+strconv.FormatInt(st.UserID, 10))
	for _, p := range people {
		_, _ = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO reward_invites (person, showtime_id, at_ms) VALUES (?, ?, ?)`, p, st.ID, st.AtMs)
	}
}

type rewardGuest struct {
	Peer  int64
	Their string
	Name  string
	Count int
}

// linkedDoor hands out a shared screening's stubs: the host's household
// first (whoever booked it is the host, seat 0), then a block of seats for
// each linked Usherr that's coming, in the order they answered.
func (s *Server) linkedDoor(ctx context.Context, st showtime, o screeningOut) {
	if st.TMDBID <= 0 {
		return
	}
	host := s.partyHost(st.UserID)
	answers := s.hostHousehold(ctx, st)
	var mine []int64 // this household, coming, in the order they said so
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
	var guests []rewardGuest
	if rows, err := s.db.QueryContext(ctx, `SELECT g.peer_id, p.their_id, COALESCE(NULLIF(p.nickname, ''), p.name), g.count FROM link_screening_guests g JOIN link_peers p ON p.id = g.peer_id
		WHERE g.showtime_id = ? AND g.answer = ? AND g.count > 0 AND p.status = 'linked' AND p.their_id <> '' ORDER BY g.answered_ms, g.peer_id`, st.ID, screening.Going); err == nil {
		for rows.Next() {
			var g rewardGuest
			if rows.Scan(&g.Peer, &g.Their, &g.Name, &g.Count) == nil {
				g.Name = link.CleanName(g.Name)
				guests = append(guests, g)
			}
		}
		rows.Close()
	}
	names := map[int64]string{}
	var everyone []string // the "with" line: the household's names, then each Usherr's
	for _, u := range mine {
		names[u] = s.partyHost(u)
		everyone = append(everyone, names[u])
	}
	for _, g := range guests {
		everyone = append(everyone, g.Name)
	}
	others := func(but string) []string {
		var out []string
		for _, n := range everyone {
			if n != but && len(out) < rewards.MaxWith {
				out = append(out, n)
			}
		}
		return out
	}

	// The household's own stubs.
	for _, u := range mine {
		ext := strconv.FormatInt(u, 10)
		var one int
		if s.db.QueryRowContext(ctx, `SELECT 1 FROM community_stubs WHERE backend = 'here' AND external_id = ? AND showtime_id = ?`, ext, st.ID).Scan(&one) == nil {
			continue
		}
		seat := s.nextSeat(ctx, st.ID, o.SID)
		if u == st.UserID {
			seat = 0
		}
		_, _ = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO community_stubs (backend, external_id, showtime_id, name, kind, tmdb_id, title, year, poster, host, seat, guests, at_ms,
				stamp_until, runtime, hosted, sid, serial)
			VALUES ('here', ?, ?, ?, 'movie', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, `+sqlNextStubNo+`)`,
			ext, st.ID, names[u], st.TMDBID, st.Title, st.Year, st.PosterURL, host, seat, strings.Join(others(names[u]), "\n"), st.AtMs,
			stubStampUntil(st), st.Runtime, u == st.UserID, o.SID)
	}

	// Each linked Usherr's block (a new one for people who came since).
	now := time.Now().UnixMilli()
	for _, g := range guests {
		var have int
		_ = s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(count), 0) FROM reward_seats WHERE showtime_id = ? AND peer_id = ?`, st.ID, g.Peer).Scan(&have)
		more := min(g.Count, rewards.MaxBlock*4) - have
		if more <= 0 {
			continue
		}
		more = min(more, rewards.MaxBlock)
		first := s.nextSeat(ctx, st.ID, o.SID)
		if first+more-1 > rewards.MaxSeat {
			continue
		}
		if _, err := s.db.ExecContext(ctx, `INSERT INTO reward_seats (showtime_id, first_seat, count, peer_id, first_no, created_ms)
			VALUES (?, ?, ?, ?, `+sqlNextStubNo+`, ?)`, st.ID, first, more, g.Peer, now); err != nil {
			continue
		}
		s.sendStubs(ctx, st, o.SID, first)
	}
}

// nextSeat is the next free seat at a shared screening (1 and up).
func (s *Server) nextSeat(ctx context.Context, showtimeID int64, sid string) int {
	var n int
	_ = s.db.QueryRowContext(ctx, `SELECT MAX(COALESCE((SELECT MAX(seat) FROM community_stubs WHERE showtime_id = ? AND sid = ? AND issuer = ''), 0),
		COALESCE((SELECT MAX(first_seat + count - 1) FROM reward_seats WHERE showtime_id = ?), 0))`, showtimeID, sid, showtimeID).Scan(&n)
	return n + 1
}

// sendStubs sends one block of seats to its linked Usherr.
func (s *Server) sendStubs(ctx context.Context, st showtime, sid string, firstSeat int) {
	n := s.linkNode()
	if n == nil {
		return
	}
	var count, no int
	var peer int64
	var their string
	if s.db.QueryRowContext(ctx, `SELECT r.count, r.first_no, r.peer_id, p.their_id FROM reward_seats r JOIN link_peers p ON p.id = r.peer_id
		WHERE r.showtime_id = ? AND r.first_seat = ? AND p.status = 'linked' AND p.their_id <> ''`, st.ID, firstSeat).Scan(&count, &no, &peer, &their) != nil {
		return
	}
	poster := st.PosterURL
	if !strings.HasPrefix(poster, "https://image.tmdb.org/") {
		poster = ""
	}
	var with []string
	if rows, err := s.db.QueryContext(ctx, `SELECT name FROM community_stubs WHERE backend = 'here' AND showtime_id = ? AND sid = ? AND issuer = '' ORDER BY seat`, st.ID, sid); err == nil {
		for rows.Next() {
			var name string
			if rows.Scan(&name) == nil && name != "" {
				with = append(with, name)
			}
		}
		rows.Close()
	}
	if rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT COALESCE(NULLIF(p.nickname, ''), p.name) FROM reward_seats r JOIN link_peers p ON p.id = r.peer_id
		WHERE r.showtime_id = ? AND r.peer_id <> ?`, st.ID, peer); err == nil {
		for rows.Next() {
			var name string
			if rows.Scan(&name) == nil {
				with = append(with, link.CleanName(name))
			}
		}
		rows.Close()
	}
	if len(with) > rewards.MaxWith {
		with = with[:rewards.MaxWith]
	}
	body := rewards.Stub{
		V: rewards.Version, SID: sid,
		Film: rewards.Film{TMDB: st.TMDBID, Title: rewards.CleanText(st.Title, 200), Year: st.Year, Runtime: max(min(st.Runtime, rewards.MaxRuntime), 0), Poster: poster},
		At:   st.AtMs, Host: rewards.CleanText(s.partyHost(st.UserID), 60), Seat: firstSeat, Count: count, No: no, With: with,
	}
	if n.Send(ctx, their, rewards.TypeStub, body) == nil {
		_, _ = s.db.ExecContext(ctx, `UPDATE reward_seats SET sent_ms = ? WHERE showtime_id = ? AND first_seat = ?`, time.Now().UnixMilli(), st.ID, firstSeat)
	}
}

// ---- stamps ----

// stubStamped: one of this Usherr's stubs was just stamped. When it came
// from a linked Usherr, the host is told (in the background).
func (s *Server) stubStamped(ctx context.Context, backend, ext string, showtimeID int64) {
	var sid, issuer string
	if s.db.QueryRowContext(ctx, `SELECT sid, issuer FROM community_stubs WHERE backend = ? AND external_id = ? AND showtime_id = ?`, backend, ext, showtimeID).Scan(&sid, &issuer) != nil ||
		sid == "" || issuer == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		s.reportStamps(ctx, sid, issuer)
	}()
}

// reportStamps tells a screening's host which of the seats it gave this
// Usherr are stamped.
func (s *Server) reportStamps(ctx context.Context, sid, issuer string) {
	n := s.linkNode()
	if n == nil {
		return
	}
	seats := s.intList(ctx, `SELECT seat FROM community_stubs WHERE backend = 'here' AND sid = ? AND issuer = ? AND stamped_ms > 0 AND seat > 0 ORDER BY seat`, sid, issuer)
	if len(seats) == 0 {
		return
	}
	if len(seats) > rewards.MaxSeatsStamp {
		seats = seats[:rewards.MaxSeatsStamp]
	}
	if n.Send(ctx, issuer, rewards.TypeStamp, rewards.Stamp{V: rewards.Version, SID: sid, Seats: seats}) == nil {
		_, _ = s.db.ExecContext(ctx, `UPDATE community_stubs SET reported_ms = ? WHERE sid = ? AND issuer = ? AND stamped_ms > 0`, time.Now().UnixMilli(), sid, issuer)
	}
}

func (s *Server) intList(ctx context.Context, q string, args ...any) []int {
	out := []int{}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var n int
		if rows.Scan(&n) == nil {
			out = append(out, n)
		}
	}
	return out
}

// rewardJob (the screening job, every minute) sends again what didn't get
// through: stubs to linked Usherrs, and stamps the host hasn't confirmed.
func (s *Server) rewardJob(ctx context.Context) {
	now := time.Now()
	type block struct {
		showtime int64
		seat     int
	}
	var todo []block
	if rows, err := s.db.QueryContext(ctx, `SELECT showtime_id, first_seat FROM reward_seats WHERE sent_ms = 0 AND created_ms > ?`, now.Add(-rewardResendFor).UnixMilli()); err == nil {
		for rows.Next() {
			var b block
			if rows.Scan(&b.showtime, &b.seat) == nil {
				todo = append(todo, b)
			}
		}
		rows.Close()
	}
	for _, b := range todo {
		st, ok := s.showtimeByID(ctx, b.showtime)
		o, ok2 := s.screeningOutOf(ctx, b.showtime)
		if ok && ok2 {
			s.sendStubs(ctx, st, o.SID, b.seat)
		}
	}
	type report struct{ sid, issuer string }
	var reports []report
	if rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT sid, issuer FROM community_stubs WHERE issuer <> '' AND sid <> '' AND stamped_ms > ? AND agreed_ms = 0 AND reported_ms < ?`,
		now.Add(-rewardReportFor).UnixMilli(), now.Add(-rewardReportWait).UnixMilli()); err == nil {
		for rows.Next() {
			var r report
			if rows.Scan(&r.sid, &r.issuer) == nil {
				reports = append(reports, r)
			}
		}
		rows.Close()
	}
	for _, r := range reports {
		s.reportStamps(ctx, r.sid, r.issuer)
	}
}

// ---- what the messages do (rewards.Book; HostOf and Invited are
// screening.Book's) ----

// Holds: every one of seats was handed to that linked Usherr.
func (b linkBook) Holds(ctx context.Context, sid, guest string, seats []int) (bool, error) {
	peer, ok := b.s.peerRow(ctx, guest)
	if !ok {
		return false, nil
	}
	for _, n := range seats {
		var one int
		err := b.s.db.QueryRowContext(ctx, `SELECT 1 FROM reward_seats r JOIN link_screenings_out o ON o.showtime_id = r.showtime_id
			WHERE o.sid = ? AND r.peer_id = ? AND r.first_seat <= ? AND r.first_seat + r.count > ?`, sid, peer, n, n).Scan(&one)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
	}
	return true, nil
}

// StubsIn: the host of a screening this Usherr is invited to hands out its
// people's stubs. Each person here who said I'm in gets one, in the order
// they said so; who already has one keeps it.
func (b linkBook) StubsIn(ctx context.Context, host link.Peer, st rewards.Stub) error {
	s := b.s
	var id int64
	var booked sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT i.id, i.showtime_id FROM link_screenings_in i JOIN link_peers p ON p.id = i.peer_id
		WHERE i.sid = ? AND p.their_id = ? AND p.status = 'linked'`, st.SID, host.ID).Scan(&id, &booked); err != nil {
		return rewards.ErrNotAllowed
	}
	showtimeID := -id // no booking here: kept under the screening's own number
	if booked.Valid {
		showtimeID = booked.Int64
	}
	type person struct {
		id   int64
		name string
	}
	var going []person
	rows, err := s.db.QueryContext(ctx, `SELECT sp.user_id, COALESCE(NULLIF(TRIM(u.first_name), ''), u.username) FROM link_screening_people sp JOIN users u ON u.id = sp.user_id
		WHERE sp.screening_id = ? AND sp.answer = ? ORDER BY sp.at_ms, sp.user_id`, id, screening.Going)
	if err != nil {
		return err
	}
	for rows.Next() {
		var p person
		if rows.Scan(&p.id, &p.name) == nil {
			going = append(going, p)
		}
	}
	rows.Close()
	used := map[int]bool{}
	for _, n := range s.intList(ctx, `SELECT seat FROM community_stubs WHERE backend = 'here' AND sid = ? AND issuer = ?`, st.SID, host.ID) {
		used[n] = true
	}
	seat := st.Seat
	for _, p := range going {
		ext := strconv.FormatInt(p.id, 10)
		var one int
		if s.db.QueryRowContext(ctx, `SELECT 1 FROM community_stubs WHERE backend = 'here' AND external_id = ? AND sid = ? AND issuer = ?`, ext, st.SID, host.ID).Scan(&one) == nil {
			continue
		}
		for seat < st.Seat+st.Count && used[seat] {
			seat++
		}
		if seat >= st.Seat+st.Count {
			break
		}
		used[seat] = true
		var with []string
		for _, q := range going {
			if q.id != p.id {
				with = append(with, q.name)
			}
		}
		with = append(with, st.With...)
		at := showtime{AtMs: st.At, Runtime: st.Film.Runtime}
		if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO community_stubs (backend, external_id, showtime_id, name, kind, tmdb_id, title, year, poster, host, seat, guests, at_ms,
				stamp_until, runtime, sid, issuer, serial)
			VALUES ('here', ?, ?, ?, 'movie', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			ext, showtimeID, p.name, st.Film.TMDB, st.Film.Title, st.Film.Year, st.Film.Poster, st.Host, seat, strings.Join(with, "\n"), st.At,
			stubStampUntil(at), st.Film.Runtime, st.SID, host.ID, st.No+seat-st.Seat); err != nil {
			return err
		}
		_, _ = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO reward_invites (person, showtime_id, at_ms) VALUES (?, ?, ?)`, "here:"+ext, showtimeID, st.At)
	}
	return nil
}

// StampsIn: a linked Usherr says which of its seats were stamped at a
// screening hosted here. Kept, and the stamps held for it are said back.
func (b linkBook) StampsIn(ctx context.Context, guest link.Peer, st rewards.Stamp) error {
	s := b.s
	peer, ok := s.peerRow(ctx, guest.ID)
	if !ok {
		return link.ErrNotLinked
	}
	var showtimeID int64
	if err := s.db.QueryRowContext(ctx, `SELECT showtime_id FROM link_screenings_out WHERE sid = ?`, st.SID).Scan(&showtimeID); err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	for _, n := range st.Seats {
		if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO reward_stamps (showtime_id, seat, peer_id, stamped_ms) VALUES (?, ?, ?, ?)`, showtimeID, n, peer, now); err != nil {
			return err
		}
	}
	sid, their := st.SID, guest.ID
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		n := s.linkNode()
		if n == nil {
			return
		}
		seats := s.intList(ctx, `SELECT seat FROM reward_stamps WHERE showtime_id = ? AND peer_id = ? ORDER BY seat`, showtimeID, peer)
		if len(seats) > rewards.MaxSeatsStamp {
			seats = seats[:rewards.MaxSeatsStamp]
		}
		_ = n.Send(ctx, their, rewards.TypeStamp, rewards.Stamp{V: rewards.Version, SID: sid, Seats: seats})
	}()
	return nil
}

// StampsAgreed: the host's record of this Usherr's stamped seats. Those
// stubs are stamped here too (if they weren't), and marked confirmed.
func (b linkBook) StampsAgreed(ctx context.Context, host link.Peer, st rewards.Stamp) error {
	now := time.Now().UnixMilli()
	for _, n := range st.Seats {
		if _, err := b.s.db.ExecContext(ctx, `UPDATE community_stubs SET stamped_ms = CASE WHEN stamped_ms = 0 THEN ? ELSE stamped_ms END, agreed_ms = ?
			WHERE backend = 'here' AND sid = ? AND issuer = ? AND seat = ?`, now, now, st.SID, host.ID, n); err != nil {
			return err
		}
	}
	return nil
}
