package api

// Usherr: Crews, the crew space between parties. A crew is a named group of
// linked Usherrs ("houses"), and the people in each who opted in: an ongoing
// chat, a shared watchlist with "Have it?" per house, a vote on the next
// film (the winner is booked in Showtime as a linked screening), film clubs
// (a recurring night with a rotation of who picks) and, for people who turn
// it on themselves, what the crew is watching.
//
// The Usherr that made a crew owns it: it invites houses it is linked with,
// keeps the crew's list, votes and clubs, and sends each house the latest.
// Every house talks to the owner alone (internal/link/crew decides who may
// send what). A house only ever says yes, getting or no about a film; no
// message asks for files, library listings or settings.
//
// home_crews_http.go is the page's API; home_crews_jobs.go the light job
// (votes that close, club nights that come up, Have it?, sharing).

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rdborg/mediarium/internal/link"
	"github.com/rdborg/mediarium/internal/link/crew"
	"github.com/rdborg/mediarium/internal/notify"
)

const (
	crewChatKeep     = 500 // chat lines kept per crew
	crewActivityKeep = 200 // activity lines kept per crew
	crewMax          = 20  // crews one Usherr is in
)

// linkCrewState is the crews' share of the link's running state.
type linkCrewState struct {
	mu   sync.Mutex         // owner side: one change at a time
	rate map[string][]int64 // chat lines sent lately, by person or house
	stop chan struct{}      // ends the crew job
	tick int
}

// crewSeqLast is the last sequence number given to a list, ballot or list
// of clubs sent: microseconds, never the same twice.
var crewSeqLast atomic.Int64

func crewSeq() int64 {
	for {
		last, now := crewSeqLast.Load(), time.Now().UnixMicro()
		if now <= last {
			now = last + 1
		}
		if crewSeqLast.CompareAndSwap(last, now) {
			return now
		}
	}
}

// crewNewer (house side) records a list or clubs sequence from the owner,
// and says whether it is newer than the one taken before.
func (s *Server) crewNewer(ctx context.Context, crewID int64, col string, seq int64) bool {
	res, err := s.db.ExecContext(ctx, `UPDATE crews SET `+col+` = ? WHERE id = ? AND `+col+` < ?`, seq, crewID, seq)
	n, _ := rowsAffected(res, err)
	return n == 1
}

// crewRow is a crew known here.
type crewRow struct {
	ID        int64
	CID       string
	Name      string
	OwnerPeer int64 // 0: this Usherr owns it
	ByName    string
	ByUser    int64
	Rev       int
	MyHid     int
	State     string // invited, joined
	Houses    string // house side: JSON []crew.House
}

func (c crewRow) mine() bool { return c.OwnerPeer == 0 }

const crewCols = `id, cid, name, COALESCE(owner_peer, 0), by_name, by_user, rev, my_hid, state, houses`

func scanCrew(sc interface{ Scan(...any) error }) (crewRow, error) {
	var c crewRow
	err := sc.Scan(&c.ID, &c.CID, &c.Name, &c.OwnerPeer, &c.ByName, &c.ByUser, &c.Rev, &c.MyHid, &c.State, &c.Houses)
	return c, err
}

func (s *Server) crewByID(ctx context.Context, id int64) (crewRow, bool) {
	c, err := scanCrew(s.db.QueryRowContext(ctx, `SELECT `+crewCols+` FROM crews WHERE id = ?`, id))
	return c, err == nil
}

func (s *Server) crewByCID(ctx context.Context, cid string) (crewRow, bool) {
	c, err := scanCrew(s.db.QueryRowContext(ctx, `SELECT `+crewCols+` FROM crews WHERE cid = ?`, cid))
	return c, err == nil
}

// crewOwnerID is the link identity of a crew's owner ("" when it's this
// Usherr, or the link ended).
func (s *Server) crewOwnerID(ctx context.Context, c crewRow) string {
	if c.mine() {
		return ""
	}
	var their string
	_ = s.db.QueryRowContext(ctx, `SELECT their_id FROM link_peers WHERE id = ? AND status = 'linked'`, c.OwnerPeer).Scan(&their)
	return their
}

// crewPersonName is what a person is called in a crew: their first name, or
// their user name.
func (s *Server) crewPersonName(ctx context.Context, uid int64) string {
	var first, user string
	_ = s.db.QueryRowContext(ctx, `SELECT first_name, username FROM users WHERE id = ?`, uid).Scan(&first, &user)
	if n := crew.CleanText(first, crew.MaxWho); n != "" {
		return n
	}
	if n := crew.CleanText(user, crew.MaxWho); n != "" {
		return n
	}
	return "Someone"
}

// crewPeopleHere are the names of the people here who opted into a crew.
func (s *Server) crewPeopleHere(ctx context.Context, crewID int64) []string {
	out := []string{}
	rows, err := s.db.QueryContext(ctx, `SELECT user_id FROM crew_people WHERE crew_id = ? ORDER BY joined_ms, user_id`, crewID)
	if err != nil {
		return out
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		if len(out) < crew.MaxPeople {
			out = append(out, s.crewPersonName(ctx, id))
		}
	}
	return out
}

// crewIn reports whether a person here opted into a crew.
func (s *Server) crewIn(ctx context.Context, crewID, uid int64) bool {
	var one int
	return s.db.QueryRowContext(ctx, `SELECT 1 FROM crew_people WHERE crew_id = ? AND user_id = ?`, crewID, uid).Scan(&one) == nil
}

// crewUserNamed finds the person here a name in a crew belongs to (who
// hosts a vote, whose pick a club night is): one who opted in, else the
// first administrator.
func (s *Server) crewUserNamed(ctx context.Context, crewID int64, name string) int64 {
	rows, err := s.db.QueryContext(ctx, `SELECT user_id FROM crew_people WHERE crew_id = ? ORDER BY joined_ms`, crewID)
	if err == nil {
		var ids []int64
		for rows.Next() {
			var id int64
			if rows.Scan(&id) == nil {
				ids = append(ids, id)
			}
		}
		rows.Close()
		for _, id := range ids {
			if s.crewPersonName(ctx, id) == name {
				return id
			}
		}
		if len(ids) > 0 {
			return ids[0]
		}
	}
	var id int64
	_ = s.db.QueryRowContext(ctx, `SELECT id FROM users WHERE is_admin = 1 ORDER BY id LIMIT 1`).Scan(&id)
	return id
}

// ---- the houses (owner side) ----

type crewHouseRow struct {
	Peer   int64
	Their  string
	Hid    int
	State  string
	Name   string
	People []string
	Seen   int64
}

func (s *Server) crewHouseRows(ctx context.Context, crewID int64, joinedOnly bool) []crewHouseRow {
	var out []crewHouseRow
	q := `SELECT h.peer_id, p.their_id, h.hid, h.state, COALESCE(NULLIF(p.nickname, ''), p.name), h.people, p.last_seen FROM crew_houses h JOIN link_peers p ON p.id = h.peer_id
		WHERE h.crew_id = ? AND p.status = 'linked' AND p.their_id <> ''`
	if joinedOnly {
		q += ` AND h.state = 'joined'`
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY h.hid`, crewID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var h crewHouseRow
		var people string
		if rows.Scan(&h.Peer, &h.Their, &h.Hid, &h.State, &h.Name, &people, &h.Seen) != nil {
			continue
		}
		_ = json.Unmarshal([]byte(people), &h.People)
		out = append(out, h)
	}
	return out
}

// crewHouses is a crew's houses as everyone sees them: this Usherr first
// when it owns the crew.
func (s *Server) crewHouses(ctx context.Context, c crewRow) []crew.House {
	if !c.mine() {
		var hs []crew.House
		_ = json.Unmarshal([]byte(c.Houses), &hs)
		return hs
	}
	me := ""
	if id, err := s.linkIdentity(); err == nil {
		me = id.ID()
	}
	hs := []crew.House{{H: 1, ID: me, Name: link.CleanName(s.linkName()), State: "joined", People: s.crewPeopleHere(ctx, c.ID)}}
	for _, h := range s.crewHouseRows(ctx, c.ID, false) {
		var self string
		_ = s.db.QueryRowContext(ctx, `SELECT name FROM link_peers WHERE id = ?`, h.Peer).Scan(&self)
		hs = append(hs, crew.House{H: h.Hid, ID: h.Their, Name: link.CleanName(self), State: h.State, People: h.People})
	}
	return hs
}

// crewChanged: the crew itself changed (its name, its houses, who's in);
// every house is told.
func (s *Server) crewChanged(ctx context.Context, c crewRow) {
	_, _ = s.db.ExecContext(ctx, `UPDATE crews SET rev = rev + 1, updated_ms = ? WHERE id = ?`, time.Now().UnixMilli(), c.ID)
	c.Rev++
	s.crewSendInvites(ctx, c, 0)
}

// crewSendInvites sends the crew as it is now to its houses (only house
// only, when not 0).
func (s *Server) crewSendInvites(ctx context.Context, c crewRow, only int) {
	n := s.linkNode()
	if n == nil || !c.mine() {
		return
	}
	var rev int
	_ = s.db.QueryRowContext(ctx, `SELECT rev FROM crews WHERE id = ?`, c.ID).Scan(&rev)
	houses := s.crewHouses(ctx, c)
	for _, h := range s.crewHouseRows(ctx, c.ID, false) {
		if only != 0 && h.Hid != only {
			continue
		}
		inv := crew.Invite{V: crew.Version, CID: c.CID, Rev: max(rev, 1), Name: c.Name, By: c.ByName, You: h.Hid, Houses: houses}
		_ = n.Send(ctx, h.Their, crew.TypeInvite, inv)
	}
}

// crewToHouses sends a message to a crew's joined houses, but not to except.
func (s *Server) crewToHouses(ctx context.Context, c crewRow, except int, typ string, body any) {
	n := s.linkNode()
	if n == nil || !c.mine() {
		return
	}
	for _, h := range s.crewHouseRows(ctx, c.ID, true) {
		if h.Hid != except {
			_ = n.Send(ctx, h.Their, typ, body)
		}
	}
}

// crewSync sends one house (or every joined house, hid 0) everything: the
// crew, the list, the votes and the clubs.
func (s *Server) crewSync(ctx context.Context, c crewRow, hid int) {
	n := s.linkNode()
	if n == nil || !c.mine() {
		return
	}
	s.crewSendInvites(ctx, c, hid)
	list := s.crewListMsg(ctx, c)
	clubs := s.crewClubsMsg(ctx, c)
	ballots := s.crewBallots(ctx, c, 6)
	now := time.Now().UnixMilli()
	for _, h := range s.crewHouseRows(ctx, c.ID, true) {
		if hid != 0 && h.Hid != hid {
			continue
		}
		_ = n.Send(ctx, h.Their, crew.TypeList, list)
		_ = n.Send(ctx, h.Their, crew.TypeClub, clubs)
		for _, b := range ballots {
			_ = n.Send(ctx, h.Their, crew.TypeVote, crew.Vote{V: crew.Version, CID: c.CID, Op: crew.VoteBallot, Ballot: b})
		}
		_, _ = s.db.ExecContext(ctx, `UPDATE crew_houses SET sent_ms = ? WHERE crew_id = ? AND hid = ?`, now, c.ID, h.Hid)
	}
}

// ---- what the messages do (crew.Book) ----

type crewBook struct{ s *Server }

func (b crewBook) OwnerOf(ctx context.Context, cid string) (string, bool, error) {
	c, ok := b.s.crewByCID(ctx, cid)
	if !ok {
		return "", false, nil
	}
	if c.mine() {
		return "", true, nil
	}
	if their := b.s.crewOwnerID(ctx, c); their != "" {
		return their, false, nil
	}
	return "unlinked", false, nil // never the sender
}

func (b crewBook) HouseOf(ctx context.Context, cid, peer string) (int, string, error) {
	var hid int
	var state string
	err := b.s.db.QueryRowContext(ctx, `SELECT h.hid, h.state FROM crew_houses h JOIN crews c ON c.id = h.crew_id JOIN link_peers p ON p.id = h.peer_id
		WHERE c.cid = ? AND c.owner_peer IS NULL AND p.their_id = ? AND p.their_id <> '' AND p.status = 'linked'`, cid, peer).Scan(&hid, &state)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", nil
	}
	return hid, state, err
}

// Invited: a linked Usherr invites this one to a crew, or says how it is now.
func (b crewBook) Invited(ctx context.Context, owner link.Peer, inv crew.Invite) error {
	s := b.s
	peer, ok := s.peerRow(ctx, owner.ID)
	if !ok {
		return link.ErrNotLinked
	}
	houses, _ := json.Marshal(inv.Houses)
	now := time.Now().UnixMilli()
	c, known := s.crewByCID(ctx, inv.CID)
	if !known {
		var count int
		_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM crews`).Scan(&count)
		if count >= crewMax {
			return errors.New("too many crews")
		}
		if _, err := s.db.ExecContext(ctx, `INSERT INTO crews (cid, name, owner_peer, by_name, rev, my_hid, state, houses, created_ms, updated_ms) VALUES (?, ?, ?, ?, ?, ?, 'invited', ?, ?, ?)`,
			inv.CID, inv.Name, peer, inv.By, inv.Rev, inv.You, string(houses), now, now); err != nil {
			return err
		}
		who := s.linkPeerName(ctx, peer)
		s.notifyEvent(notify.EventCrew, "Crew invite: "+inv.Name, fmt.Sprintf("%s invites you to the crew %s. Open Crews to join.", who, inv.Name))
		return nil
	}
	if inv.Rev < c.Rev {
		return nil // an older one
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE crews SET name = ?, by_name = ?, rev = ?, my_hid = ?, houses = ?, updated_ms = ? WHERE id = ?`,
		inv.Name, inv.By, inv.Rev, inv.You, string(houses), now, c.ID); err != nil {
		return err
	}
	// The owner lost this house's answer: said again.
	for _, h := range inv.Houses {
		if h.H == inv.You && h.State == "invited" && c.State == "joined" {
			go s.crewAnswer(c.ID, crew.Join)
		}
	}
	return nil
}

func (b crewBook) Ended(ctx context.Context, owner link.Peer, cid string) error {
	c, ok := b.s.crewByCID(ctx, cid)
	if !ok {
		return nil
	}
	_, err := b.s.db.ExecContext(ctx, `DELETE FROM crews WHERE id = ?`, c.ID)
	if err == nil && c.State == "joined" {
		b.s.notifyEvent(notify.EventCrew, "Crew ended: "+c.Name, fmt.Sprintf("The crew %s ended, or this Usherr isn't in it anymore.", c.Name))
	}
	return err
}

// Answered: a house joined (and who opted in there), declined or left.
func (b crewBook) Answered(ctx context.Context, house link.Peer, hid int, a crew.Answer) error {
	s := b.s
	c, ok := s.crewByCID(ctx, a.CID)
	if !ok || !c.mine() {
		return crew.ErrNotAllowed
	}
	var was string
	_ = s.db.QueryRowContext(ctx, `SELECT state FROM crew_houses WHERE crew_id = ? AND hid = ?`, c.ID, hid).Scan(&was)
	switch a.Answer {
	case crew.Join:
		people, _ := json.Marshal(a.People)
		if _, err := s.db.ExecContext(ctx, `UPDATE crew_houses SET state = 'joined', people = ? WHERE crew_id = ? AND hid = ?`, string(people), c.ID, hid); err != nil {
			return err
		}
		s.crewChanged(ctx, c)
		if was != "joined" {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
				defer cancel()
				s.crewSync(ctx, c, hid)
			}()
		}
	default:
		if _, err := s.db.ExecContext(ctx, `DELETE FROM crew_houses WHERE crew_id = ? AND hid = ?`, c.ID, hid); err != nil {
			return err
		}
		s.crewChanged(ctx, c)
	}
	return nil
}

// crewRateOK: at most n lines in per; key is a person or a house.
func (s *Server) crewRateOK(key string, n int, per time.Duration) bool {
	l := &s.linkS.crew
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.rate == nil {
		l.rate = map[string][]int64{}
	}
	now := time.Now().UnixMilli()
	kept := l.rate[key][:0]
	for _, t := range l.rate[key] {
		if now-t < per.Milliseconds() {
			kept = append(kept, t)
		}
	}
	if len(kept) >= n {
		l.rate[key] = kept
		return false
	}
	l.rate[key] = append(kept, now)
	if len(l.rate) > 5000 {
		l.rate = map[string][]int64{}
	}
	return true
}

func (b crewBook) ChatIn(ctx context.Context, from link.Peer, hid int, m crew.Chat) error {
	s := b.s
	c, ok := s.crewByCID(ctx, m.CID)
	if !ok {
		return crew.ErrNotAllowed
	}
	if hid > 1 && !s.crewRateOK(fmt.Sprintf("house/%d/%d", c.ID, hid), 30, time.Minute) {
		return nil // too many: dropped
	}
	if !s.crewSaveChat(ctx, c, m) {
		return nil // had it already
	}
	if hid > 0 {
		s.crewToHouses(ctx, c, hid, crew.TypeChat, m) // the owner passes it on
	}
	if m.Card != nil && m.Card.To == c.MyHid && m.H != c.MyHid {
		film := m.Card.Title
		if m.Card.Year > 0 {
			film = fmt.Sprintf("%s (%d)", m.Card.Title, m.Card.Year)
		}
		s.notifyEvent(notify.EventCrew, "A film for you: "+film, fmt.Sprintf("%s sent you %s in the crew %s.", m.Who, film, c.Name))
	}
	return nil
}

// crewSaveChat keeps a chat line (once), and the crew's last lines only.
func (s *Server) crewSaveChat(ctx context.Context, c crewRow, m crew.Chat) bool {
	card := ""
	if m.Card != nil {
		raw, _ := json.Marshal(m.Card)
		card = string(raw)
	}
	at := min(m.At, time.Now().UnixMilli())
	res, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO crew_chat (crew_id, mid, hid, who, text, card, at_ms) VALUES (?, ?, ?, ?, ?, ?, ?)`, c.ID, m.MID, m.H, m.Who, m.Text, card, at)
	if n, _ := rowsAffected(res, err); n == 0 {
		return false
	}
	_, _ = s.db.ExecContext(ctx, `DELETE FROM crew_chat WHERE crew_id = ? AND id NOT IN (SELECT id FROM crew_chat WHERE crew_id = ? ORDER BY id DESC LIMIT ?)`, c.ID, c.ID, crewChatKeep)
	return true
}

func (b crewBook) ListIn(ctx context.Context, from link.Peer, hid int, l crew.List) error {
	s := b.s
	c, ok := s.crewByCID(ctx, l.CID)
	if !ok {
		return crew.ErrNotAllowed
	}
	if hid == 0 { // from the owner: the whole list
		if !s.crewNewer(ctx, c.ID, "list_seq", l.Seq) {
			return nil // an older one that came late
		}
		if _, err := s.db.ExecContext(ctx, `DELETE FROM crew_list WHERE crew_id = ?`, c.ID); err != nil {
			return err
		}
		for _, e := range l.Items {
			have, _ := json.Marshal(e.Have)
			_, _ = s.db.ExecContext(ctx, `INSERT OR REPLACE INTO crew_list (crew_id, tmdb_id, title, year, poster, by_name, by_hid, added_ms, have) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				c.ID, e.TMDB, e.Title, e.Year, e.Poster, e.By, e.ByH, e.Added, string(have))
		}
		go s.crewTellHave(c.ID)
		return nil
	}
	l2 := &s.linkS.crew
	l2.mu.Lock()
	err := s.crewListChange(ctx, c, hid, l)
	l2.mu.Unlock()
	if err != nil {
		return err
	}
	if l.Op == crew.ListHave {
		s.crewSendList(ctx, c)
		return nil
	}
	s.crewOwnHave(ctx, c)
	s.crewSendList(ctx, c)
	return nil
}

// crewListChange changes the list of a crew owned here, as house hid asked.
func (s *Server) crewListChange(ctx context.Context, c crewRow, hid int, l crew.List) error {
	switch l.Op {
	case crew.ListAdd:
		var n int
		_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM crew_list WHERE crew_id = ?`, c.ID).Scan(&n)
		if n >= crew.MaxList {
			return errCrewListFull
		}
		_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO crew_list (crew_id, tmdb_id, title, year, poster, by_name, by_hid, added_ms) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			c.ID, l.Film.TMDB, l.Film.Title, l.Film.Year, l.Film.Poster, l.Who, hid, time.Now().UnixMilli())
		return err
	case crew.ListRemove:
		// Whoever added it (their house), or anyone here when this Usherr owns it.
		q := `DELETE FROM crew_list WHERE crew_id = ? AND tmdb_id = ?`
		args := []any{c.ID, l.Film.TMDB}
		if hid > 1 {
			q += ` AND by_hid = ?`
			args = append(args, hid)
		}
		_, err := s.db.ExecContext(ctx, q, args...)
		return err
	case crew.ListHave:
		for _, h := range l.Has {
			s.crewSetHave(ctx, c.ID, h.TMDB, hid, h.Has)
		}
	}
	return nil
}

var errCrewListFull = errors.New("The shared watchlist is full (40 films). Take one off first.")

// crewSetHave records whether house hid has a film on a crew's list.
func (s *Server) crewSetHave(ctx context.Context, crewID int64, tmdb, hid int, has string) {
	var raw string
	if s.db.QueryRowContext(ctx, `SELECT have FROM crew_list WHERE crew_id = ? AND tmdb_id = ?`, crewID, tmdb).Scan(&raw) != nil {
		return
	}
	var hs []crew.Have
	_ = json.Unmarshal([]byte(raw), &hs)
	found := false
	for i := range hs {
		if hs[i].H == hid {
			hs[i].Has, found = has, true
		}
	}
	if !found {
		hs = append(hs, crew.Have{H: hid, Has: has})
	}
	sort.Slice(hs, func(i, j int) bool { return hs[i].H < hs[j].H })
	out, _ := json.Marshal(hs)
	_, _ = s.db.ExecContext(ctx, `UPDATE crew_list SET have = ? WHERE crew_id = ? AND tmdb_id = ?`, string(out), crewID, tmdb)
}

// crewOwnHave: whether this Usherr (house 1) has each film on a list it owns.
func (s *Server) crewOwnHave(ctx context.Context, c crewRow) {
	for _, e := range s.crewListRows(ctx, c.ID) {
		s.crewSetHave(ctx, c.ID, e.TMDB, 1, s.crewHave(ctx, e.TMDB))
	}
}

func (s *Server) crewListRows(ctx context.Context, crewID int64) []crew.Entry {
	out := []crew.Entry{}
	rows, err := s.db.QueryContext(ctx, `SELECT tmdb_id, title, year, poster, by_name, by_hid, added_ms, have FROM crew_list WHERE crew_id = ? ORDER BY added_ms DESC, tmdb_id`, crewID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var e crew.Entry
		var have string
		if rows.Scan(&e.TMDB, &e.Title, &e.Year, &e.Poster, &e.By, &e.ByH, &e.Added, &have) != nil {
			continue
		}
		_ = json.Unmarshal([]byte(have), &e.Have)
		out = append(out, e)
	}
	return out
}

// crewListMsg is the whole list, made to fit one message (the oldest films
// left out when it doesn't).
func (s *Server) crewListMsg(ctx context.Context, c crewRow) crew.List {
	items := s.crewListRows(ctx, c.ID)
	for {
		m := crew.List{V: crew.Version, CID: c.CID, Op: crew.ListAll, Items: items, Seq: crewSeq()}
		raw, _ := json.Marshal(m)
		if len(raw) <= crew.MaxBodyList || len(items) == 0 {
			return m
		}
		items = items[:len(items)-1]
	}
}

func (s *Server) crewSendList(ctx context.Context, c crewRow) {
	s.crewToHouses(ctx, c, 0, crew.TypeList, s.crewListMsg(ctx, c))
}

// crewHave says whether this Usherr has a film, as screenings' "Have it?"
// does but without reading the file (light): yes (a file, or a saved
// stream), getting (on its way, or asked for) or no.
func (s *Server) crewHave(ctx context.Context, tmdb int) string {
	m, ok, err := s.MovieRepo.GetByTMDBID(tmdb)
	if err == nil && ok && m.FilePath != "" {
		return crew.HasYes
	}
	var one int
	if s.db.QueryRowContext(ctx, `SELECT 1 FROM streamed_files f JOIN streamed_titles t ON t.id = f.title_id WHERE t.kind = 'movie' AND t.tmdb_id = ? LIMIT 1`, tmdb).Scan(&one) == nil {
		return crew.HasYes
	}
	if err == nil && ok && (m.Monitored || m.Status == "downloading") {
		return crew.HasGetting
	}
	if s.db.QueryRowContext(ctx, `SELECT 1 FROM requests WHERE kind = 'movie' AND status IN ('pending', 'approved') AND json_extract(payload, '$.tmdbId') = ? LIMIT 1`, tmdb).Scan(&one) == nil {
		return crew.HasGetting
	}
	return crew.HasNo
}

// crewTellHave (house side): tells the owner whether this Usherr has the
// films on a crew's list, when that changed.
func (s *Server) crewTellHave(crewID int64) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c, ok := s.crewByID(ctx, crewID)
	n := s.linkNode()
	if !ok || n == nil || c.State != "joined" {
		return
	}
	if c.mine() {
		before, _ := json.Marshal(s.crewListRows(ctx, c.ID))
		s.linkS.crew.mu.Lock()
		s.crewOwnHave(ctx, c)
		s.linkS.crew.mu.Unlock()
		if after, _ := json.Marshal(s.crewListRows(ctx, c.ID)); string(after) != string(before) {
			s.crewSendList(ctx, c)
		}
		return
	}
	var has []crew.HasOf
	for _, e := range s.crewListRows(ctx, c.ID) {
		has = append(has, crew.HasOf{TMDB: e.TMDB, Has: s.crewHave(ctx, e.TMDB)})
	}
	if len(has) == 0 {
		return
	}
	sort.Slice(has, func(i, j int) bool { return has[i].TMDB < has[j].TMDB })
	raw, _ := json.Marshal(has)
	var told string
	_ = s.db.QueryRowContext(ctx, `SELECT told_have FROM crews WHERE id = ?`, c.ID).Scan(&told)
	if told == string(raw) {
		return
	}
	if n.Send(ctx, s.crewOwnerID(ctx, c), crew.TypeList, crew.List{V: crew.Version, CID: c.CID, Op: crew.ListHave, Has: has}) == nil {
		_, _ = s.db.ExecContext(ctx, `UPDATE crews SET told_have = ? WHERE id = ?`, string(raw), c.ID)
	}
}

func (b crewBook) VoteIn(ctx context.Context, from link.Peer, hid int, v crew.Vote) error {
	s := b.s
	c, ok := s.crewByCID(ctx, v.CID)
	if !ok {
		return crew.ErrNotAllowed
	}
	if hid == 0 { // from the owner: a ballot as it stands
		return s.crewTakeBallot(ctx, c, *v.Ballot)
	}
	l := &s.linkS.crew
	l.mu.Lock()
	id, err := s.crewVoteChange(ctx, c, hid, v, 0, 0)
	l.mu.Unlock()
	if err != nil {
		return err
	}
	s.crewSendBallot(ctx, c, id)
	return nil
}

// crewVoteChange opens a vote or takes one person's vote, in a crew owned
// here. hostUser and theme are only known when the host is here.
func (s *Server) crewVoteChange(ctx context.Context, c crewRow, hid int, v crew.Vote, hostUser, theme int64) (int64, error) {
	now := time.Now()
	switch v.Op {
	case crew.VoteOpen:
		if msg := crewVoteTimes(v.Closes, v.At, now); msg != "" {
			return 0, errors.New(msg)
		}
		var open int
		_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM crew_votes WHERE crew_id = ? AND state = 'open'`, c.ID).Scan(&open)
		if open >= 3 {
			return 0, errors.New("Three votes are open already. Wait for one to close.")
		}
		choices := make([]crew.Choice, 0, len(v.Films))
		for _, f := range v.Films {
			choices = append(choices, crew.Choice{Film: f})
		}
		raw, _ := json.Marshal(choices)
		res, err := s.db.ExecContext(ctx, `INSERT INTO crew_votes (crew_id, vid, host_hid, host_name, host_user, closes_ms, at_ms, preshow_min, preshow_theme, state, choices, created_ms)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'open', ?, ?)`, c.ID, v.VID, hid, v.Who, hostUser, v.Closes, v.At, v.Pre, theme, string(raw), now.UnixMilli())
		if err != nil {
			return 0, err
		}
		return res.LastInsertId()
	case crew.VoteCast:
		var id, closes int64
		var state, choices string
		if s.db.QueryRowContext(ctx, `SELECT id, closes_ms, state, choices FROM crew_votes WHERE vid = ? AND crew_id = ?`, v.VID, c.ID).Scan(&id, &closes, &state, &choices) != nil {
			return 0, errors.New("That vote isn't here anymore.")
		}
		if state != crew.VoteIsOpen || closes <= now.UnixMilli() {
			return 0, errors.New("That vote is closed.")
		}
		if v.TMDB == 0 {
			_, err := s.db.ExecContext(ctx, `DELETE FROM crew_ballots WHERE vote_id = ? AND hid = ? AND who = ?`, id, hid, v.Who)
			return id, err
		}
		var list []crew.Choice
		_ = json.Unmarshal([]byte(choices), &list)
		on := false
		for _, ch := range list {
			on = on || ch.TMDB == v.TMDB
		}
		if !on {
			return 0, errors.New("That film isn't on the ballot.")
		}
		_, err := s.db.ExecContext(ctx, `INSERT INTO crew_ballots (vote_id, hid, who, tmdb_id, at_ms) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (vote_id, hid, who) DO UPDATE SET tmdb_id = excluded.tmdb_id, at_ms = excluded.at_ms`, id, hid, v.Who, v.TMDB, now.UnixMilli())
		return id, err
	}
	return 0, crew.ErrNotAllowed
}

// crewVoteTimes checks when a vote closes and when its winner plays.
func crewVoteTimes(closes, at int64, now time.Time) string {
	switch {
	case closes < now.Add(10*time.Minute).UnixMilli():
		return "Let the vote run at least 10 minutes."
	case at < closes+30*time.Minute.Milliseconds():
		return "The film starts at least half an hour after voting closes."
	case at > now.AddDate(0, 0, 60).UnixMilli():
		return "Pick a time up to two months ahead."
	}
	return ""
}

// crewBallot is one vote as it stands (owner side: with who voted for what).
func (s *Server) crewBallot(ctx context.Context, c crewRow, voteID int64) (*crew.Ballot, bool) {
	b := &crew.Ballot{Seq: crewSeq()}
	var choices string
	if s.db.QueryRowContext(ctx, `SELECT vid, host_hid, host_name, closes_ms, at_ms, preshow_min, club_kid, state, winner_tmdb, choices FROM crew_votes WHERE id = ? AND crew_id = ?`, voteID, c.ID).
		Scan(&b.VID, &b.HostH, &b.Host, &b.Closes, &b.At, &b.Preshow, &b.Club, &b.State, &b.Winner, &choices) != nil {
		return nil, false
	}
	_ = json.Unmarshal([]byte(choices), &b.Choices)
	if !c.mine() {
		return b, true
	}
	have := map[int][]crew.Have{}
	for _, e := range s.crewListRows(ctx, c.ID) {
		have[e.TMDB] = e.Have
	}
	voters := map[int][]crew.Seat{}
	rows, err := s.db.QueryContext(ctx, `SELECT hid, who, tmdb_id FROM crew_ballots WHERE vote_id = ? ORDER BY at_ms`, voteID)
	if err == nil {
		for rows.Next() {
			var seat crew.Seat
			var tmdb int
			if rows.Scan(&seat.H, &seat.Who, &tmdb) == nil && len(voters[tmdb]) < crew.MaxVoters {
				voters[tmdb] = append(voters[tmdb], seat)
			}
		}
		rows.Close()
	}
	for i := range b.Choices {
		ch := &b.Choices[i]
		ch.Voters = voters[ch.TMDB]
		if h, ok := have[ch.TMDB]; ok {
			ch.Have = h
		}
	}
	return b, true
}

// crewBallots are a crew's open votes and its latest closed ones.
func (s *Server) crewBallots(ctx context.Context, c crewRow, most int) []*crew.Ballot {
	var ids []int64
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM crew_votes WHERE crew_id = ? ORDER BY state = 'open' DESC, closes_ms DESC LIMIT ?`, c.ID, most)
	if err != nil {
		return nil
	}
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	var out []*crew.Ballot
	for _, id := range ids {
		if b, ok := s.crewBallot(ctx, c, id); ok {
			out = append(out, b)
		}
	}
	return out
}

// crewSendBallot tells every house how a vote stands, and keeps the voters
// on this side too.
func (s *Server) crewSendBallot(ctx context.Context, c crewRow, voteID int64) {
	b, ok := s.crewBallot(ctx, c, voteID)
	if !ok {
		return
	}
	raw, _ := json.Marshal(b.Choices)
	_, _ = s.db.ExecContext(ctx, `UPDATE crew_votes SET choices = ? WHERE id = ?`, string(raw), voteID)
	s.crewToHouses(ctx, c, 0, crew.TypeVote, crew.Vote{V: crew.Version, CID: c.CID, Op: crew.VoteBallot, Ballot: b})
}

// crewTakeBallot (house side): the owner says how a vote stands. When it
// closed with a winner and this house hosts it, the winner is booked here.
func (s *Server) crewTakeBallot(ctx context.Context, c crewRow, b crew.Ballot) error {
	raw, _ := json.Marshal(b.Choices)
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM crew_votes WHERE vid = ? AND crew_id = ?`, b.VID, c.ID).Scan(&id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		res, err := s.db.ExecContext(ctx, `INSERT INTO crew_votes (crew_id, vid, host_hid, host_name, closes_ms, at_ms, preshow_min, club_kid, state, winner_tmdb, choices, created_ms, seq)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, c.ID, b.VID, b.HostH, b.Host, b.Closes, b.At, b.Preshow, b.Club, b.State, b.Winner, string(raw), time.Now().UnixMilli(), b.Seq)
		if err != nil {
			return err
		}
		id, _ = res.LastInsertId()
	case err != nil:
		return err
	default:
		res, err := s.db.ExecContext(ctx, `UPDATE crew_votes SET host_hid = ?, host_name = ?, closes_ms = ?, at_ms = ?, preshow_min = ?, club_kid = ?,
			state = CASE WHEN state = 'booked' AND ? = 'closed' THEN 'booked' ELSE ? END, winner_tmdb = ?, choices = ?, seq = ? WHERE id = ? AND seq < ?`,
			b.HostH, b.Host, b.Closes, b.At, b.Preshow, b.Club, b.State, b.State, b.Winner, string(raw), b.Seq, id, b.Seq)
		if n, err := rowsAffected(res, err); err != nil {
			return err
		} else if n == 0 {
			return nil // an older one that came late
		}
	}
	_, _ = s.db.ExecContext(ctx, `DELETE FROM crew_votes WHERE crew_id = ? AND id NOT IN (SELECT id FROM crew_votes WHERE crew_id = ? ORDER BY closes_ms DESC LIMIT 100)`, c.ID, c.ID)
	if (b.State == crew.VoteClosed || b.State == crew.VoteBooked) && b.Winner > 0 && b.HostH == c.MyHid {
		go s.crewBookWinner(c.ID, id)
	}
	return nil
}

// crewBookWinner books a vote's winner in this Usherr's Showtime for its host, and
// shares it with the crew's houses this Usherr is linked with, as a linked
// screening. Once.
func (s *Server) crewBookWinner(crewID, voteID int64) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c, ok := s.crewByID(ctx, crewID)
	if !ok {
		return
	}
	res, err := s.db.ExecContext(ctx, `UPDATE crew_votes SET showtime_id = -1 WHERE id = ? AND showtime_id = 0 AND winner_tmdb > 0`, voteID)
	if n, _ := rowsAffected(res, err); n == 0 {
		return // booked already
	}
	var hostUser, theme, at int64
	var hostName, choices string
	var pre, winner int
	if s.db.QueryRowContext(ctx, `SELECT host_user, host_name, preshow_theme, at_ms, preshow_min, winner_tmdb, choices FROM crew_votes WHERE id = ?`, voteID).
		Scan(&hostUser, &hostName, &theme, &at, &pre, &winner, &choices) != nil {
		return
	}
	var list []crew.Choice
	_ = json.Unmarshal([]byte(choices), &list)
	var film crew.Film
	for _, ch := range list {
		if ch.TMDB == winner {
			film = ch.Film
		}
	}
	if film.TMDB == 0 {
		return
	}
	if hostUser == 0 {
		hostUser = s.crewUserNamed(ctx, c.ID, hostName)
	}
	body := showtimeBody{TMDBID: film.TMDB, Title: film.Title, Year: film.Year, AtMs: at, PlayOn: "here", Theme: &theme}
	if film.Poster != "" {
		body.PosterURL = "https://image.tmdb.org/t/p/w500" + film.Poster
	}
	pm := pre
	body.PreshowMin = &pm
	if msg := body.check(); msg != "" || hostUser == 0 {
		_, _ = s.db.ExecContext(ctx, `UPDATE crew_votes SET showtime_id = 0 WHERE id = ?`, voteID)
		return
	}
	stID, err := s.saveShowtime(ctx, hostUser, body, pre)
	if err != nil {
		_, _ = s.db.ExecContext(ctx, `UPDATE crew_votes SET showtime_id = 0 WHERE id = ?`, voteID)
		return
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE crew_votes SET showtime_id = ?, state = 'booked' WHERE id = ?`, stID, voteID)
	// The crew's houses this Usherr is linked with get the screening.
	var peers []int64
	me := ""
	if id, err := s.linkIdentity(); err == nil {
		me = id.ID()
	}
	for _, h := range s.crewHouses(ctx, c) {
		if h.State != "joined" || h.ID == "" || h.ID == me {
			continue
		}
		if p, ok := s.peerRow(ctx, h.ID); ok {
			peers = append(peers, p)
		}
	}
	if st, ok := s.showtimeByID(ctx, stID); ok && len(peers) > 0 {
		_ = s.inviteLinked(ctx, st, peers)
	}
	if c.mine() {
		s.crewSendBallot(ctx, c, voteID)
	}
}

func (b crewBook) ClubIn(ctx context.Context, from link.Peer, hid int, m crew.Club) error {
	s := b.s
	c, ok := s.crewByCID(ctx, m.CID)
	if !ok {
		return crew.ErrNotAllowed
	}
	if hid == 0 { // from the owner: every club
		if !s.crewNewer(ctx, c.ID, "club_seq", m.Seq) {
			return nil
		}
		return s.crewTakeClubs(ctx, c, m.Clubs)
	}
	l := &s.linkS.crew
	l.mu.Lock()
	err := s.crewClubChange(ctx, c, hid, m, false)
	l.mu.Unlock()
	if err != nil {
		return err
	}
	s.crewSendClubs(ctx, c)
	return nil
}

func (s *Server) crewTakeClubs(ctx context.Context, c crewRow, clubs []crew.ClubState) error {
	keep := []any{c.ID}
	marks := []string{}
	for _, k := range clubs {
		rot, _ := json.Marshal(k.Rotation)
		pick := ""
		if k.Pick != nil {
			raw, _ := json.Marshal(k.Pick)
			pick = string(raw)
		}
		if _, err := s.db.ExecContext(ctx, `INSERT INTO crew_clubs (crew_id, kid, name, weekday, minute, every, mode, preshow_min, by_name, by_hid, next_ms, rotation, turn, pick, streak, nights, created_ms)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (kid) DO UPDATE SET name = excluded.name, weekday = excluded.weekday, minute = excluded.minute, every = excluded.every, mode = excluded.mode,
				preshow_min = excluded.preshow_min, by_name = excluded.by_name, by_hid = excluded.by_hid, next_ms = excluded.next_ms, rotation = excluded.rotation,
				turn = excluded.turn, pick = excluded.pick, streak = excluded.streak, nights = excluded.nights, ended_ms = 0`,
			c.ID, k.KID, k.Name, k.Weekday, k.Minute, k.Every, k.Mode, k.Preshow, k.By, k.ByH, k.Next, string(rot), k.Turn, pick, k.Streak, k.Nights, time.Now().UnixMilli()); err != nil {
			return err
		}
		keep = append(keep, k.KID)
		marks = append(marks, "?")
	}
	q := `UPDATE crew_clubs SET ended_ms = ? WHERE crew_id = ? AND ended_ms = 0`
	args := []any{time.Now().UnixMilli()}
	args = append(args, keep...)
	if len(marks) > 0 {
		q += ` AND kid NOT IN (` + strings.Join(marks, ", ") + `)`
	}
	_, err := s.db.ExecContext(ctx, q, args...)
	return err
}

// crewClubRow is a club of a crew owned here.
type crewClubRow struct {
	ID int64
	crew.ClubState
}

func (s *Server) crewClubRows(ctx context.Context, crewID int64) []crewClubRow {
	var out []crewClubRow
	rows, err := s.db.QueryContext(ctx, `SELECT id, kid, name, weekday, minute, every, mode, preshow_min, by_name, by_hid, next_ms, rotation, turn, pick, streak, nights
		FROM crew_clubs WHERE crew_id = ? AND ended_ms = 0 ORDER BY created_ms, id`, crewID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var k crewClubRow
		var rot, pick string
		if rows.Scan(&k.ID, &k.KID, &k.Name, &k.Weekday, &k.Minute, &k.Every, &k.Mode, &k.Preshow, &k.By, &k.ByH, &k.Next, &rot, &k.Turn, &pick, &k.Streak, &k.Nights) != nil {
			continue
		}
		_ = json.Unmarshal([]byte(rot), &k.Rotation)
		if pick != "" {
			var f crew.Film
			if json.Unmarshal([]byte(pick), &f) == nil {
				k.Pick = &f
			}
		}
		out = append(out, k)
	}
	return out
}

func (s *Server) saveClub(ctx context.Context, k crewClubRow) error {
	rot, _ := json.Marshal(k.Rotation)
	pick := ""
	if k.Pick != nil {
		raw, _ := json.Marshal(k.Pick)
		pick = string(raw)
	}
	if k.Turn >= len(k.Rotation) {
		k.Turn = 0
	}
	_, err := s.db.ExecContext(ctx, `UPDATE crew_clubs SET next_ms = ?, rotation = ?, turn = ?, pick = ?, streak = ?, nights = ? WHERE id = ?`,
		k.Next, string(rot), k.Turn, pick, k.Streak, k.Nights, k.ID)
	return err
}

// crewClubChange changes a club of a crew owned here, as a person of house
// hid asked. admin: the person is an administrator here.
func (s *Server) crewClubChange(ctx context.Context, c crewRow, hid int, m crew.Club, admin bool) error {
	now := time.Now()
	if m.Op == crew.ClubAdd {
		var n int
		_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM crew_clubs WHERE crew_id = ? AND ended_ms = 0`, c.ID).Scan(&n)
		if n >= crew.MaxClubs {
			return errors.New("A crew can have up to six clubs.")
		}
		d := *m.Def
		rot, _ := json.Marshal([]crew.Seat{{H: hid, Who: m.Who}})
		_, err := s.db.ExecContext(ctx, `INSERT INTO crew_clubs (crew_id, kid, name, weekday, minute, every, mode, preshow_min, by_name, by_hid, next_ms, rotation, created_ms)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, c.ID, crew.NewID(), d.Name, d.Weekday, d.Minute, d.Every, d.Mode, d.Preshow, m.Who, hid,
			clubFirstNight(d, now).UnixMilli(), string(rot), now.UnixMilli())
		return err
	}
	var k *crewClubRow
	for _, x := range s.crewClubRows(ctx, c.ID) {
		if x.KID == m.KID {
			k = &x
		}
	}
	if k == nil {
		return errors.New("That club isn't here anymore.")
	}
	me := crew.Seat{H: hid, Who: m.Who}
	at := -1
	for i, seat := range k.Rotation {
		if seat == me {
			at = i
		}
	}
	switch m.Op {
	case crew.ClubJoin:
		if at < 0 {
			if len(k.Rotation) >= crew.MaxRotation {
				return errors.New("That club is full.")
			}
			k.Rotation = append(k.Rotation, me)
		}
	case crew.ClubLeave:
		if at >= 0 {
			k.Rotation = append(k.Rotation[:at], k.Rotation[at+1:]...)
			switch {
			case at < k.Turn:
				k.Turn--
			case at == k.Turn:
				k.Pick = nil // the picker left: the next one picks
			}
		}
	case crew.ClubPick:
		if at < 0 || at != k.Turn || k.Mode != crew.ModePick {
			return errors.New("It's someone else's turn to pick.")
		}
		f := *m.Film
		k.Pick = &f
	case crew.ClubRemove:
		if k.ByH != hid || (k.By != m.Who && !(hid == 1 && admin)) {
			return errors.New("Only whoever started a club can end it.")
		}
		_, err := s.db.ExecContext(ctx, `UPDATE crew_clubs SET ended_ms = ? WHERE id = ?`, now.UnixMilli(), k.ID)
		return err
	}
	return s.saveClub(ctx, *k)
}

// clubFirstNight is a new club's first night: its weekday and time, at least
// six hours from now (so there's time to vote).
func clubFirstNight(d crew.ClubDef, now time.Time) time.Time {
	l := now.Local()
	day := time.Date(l.Year(), l.Month(), l.Day(), d.Minute/60, d.Minute%60, 0, 0, time.Local)
	for day.Weekday() != time.Weekday(d.Weekday) || day.Before(now.Add(6*time.Hour)) {
		day = day.AddDate(0, 0, 1)
		day = time.Date(day.Year(), day.Month(), day.Day(), d.Minute/60, d.Minute%60, 0, 0, time.Local)
	}
	return day
}

// clubNextNight is the night after one: a week or two later, same time.
func clubNextNight(night time.Time, every int) time.Time {
	l := night.Local()
	n := l.AddDate(0, 0, 7*max(every, 1))
	return time.Date(n.Year(), n.Month(), n.Day(), l.Hour(), l.Minute(), 0, 0, time.Local)
}

func (s *Server) crewClubsMsg(ctx context.Context, c crewRow) crew.Club {
	clubs := []crew.ClubState{}
	for _, k := range s.crewClubRows(ctx, c.ID) {
		if k.Turn >= len(k.Rotation) {
			k.Turn = 0
		}
		clubs = append(clubs, k.ClubState)
	}
	return crew.Club{V: crew.Version, CID: c.CID, Op: crew.ClubAll, Clubs: clubs, Seq: crewSeq()}
}

func (s *Server) crewSendClubs(ctx context.Context, c crewRow) {
	s.crewToHouses(ctx, c, 0, crew.TypeClub, s.crewClubsMsg(ctx, c))
}

func (b crewBook) ActivityIn(ctx context.Context, from link.Peer, hid int, a crew.Activity) error {
	s := b.s
	c, ok := s.crewByCID(ctx, a.CID)
	if !ok {
		return crew.ErrNotAllowed
	}
	if hid > 1 && !s.crewRateOK(fmt.Sprintf("activity/%d/%d", c.ID, hid), 30, time.Hour) {
		return nil
	}
	var kept []crew.Line
	for _, l := range a.Lines {
		if adultLibrary.MatchString(l.Title) {
			continue // NSFW never appears
		}
		at := min(l.At, time.Now().UnixMilli())
		if _, err := s.db.ExecContext(ctx, `INSERT INTO crew_activity (crew_id, hid, who, kind, title, part, stars, at_ms) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			c.ID, l.H, l.Who, l.Kind, l.Title, l.Part, l.Stars, at); err == nil {
			kept = append(kept, l)
		}
	}
	_, _ = s.db.ExecContext(ctx, `DELETE FROM crew_activity WHERE crew_id = ? AND id NOT IN (SELECT id FROM crew_activity WHERE crew_id = ? ORDER BY at_ms DESC, id DESC LIMIT ?)`, c.ID, c.ID, crewActivityKeep)
	if hid > 0 && len(kept) > 0 {
		s.crewToHouses(ctx, c, hid, crew.TypeActivity, crew.Activity{V: crew.Version, CID: c.CID, Lines: kept})
	}
	return nil
}

// ---- what a person here does ----

// crewAct does what a person here asked: in a crew owned here, as house 1;
// otherwise it's sent to the owner (who tells everyone).
func (s *Server) crewAct(ctx context.Context, c crewRow, typ string, body interface{ Check() bool }, hostUser, theme int64, admin bool) error {
	if !body.Check() {
		return errors.New("That couldn't be read.")
	}
	n := s.linkNode()
	if n == nil {
		return errLinkOff
	}
	if !c.mine() {
		their := s.crewOwnerID(ctx, c)
		if their == "" {
			return errors.New("The Usherr that runs this crew isn't linked anymore.")
		}
		return n.Send(ctx, their, typ, body)
	}
	b := crewBook{s}
	l := &s.linkS.crew
	switch m := body.(type) {
	case *crew.Chat:
		m.H = 1
		return b.ChatIn(ctx, link.Peer{}, 1, *m)
	case *crew.List:
		l.mu.Lock()
		err := s.crewListChange(ctx, c, 1, *m)
		if err == nil {
			s.crewOwnHave(ctx, c)
		}
		l.mu.Unlock()
		if err == nil {
			s.crewSendList(ctx, c)
		}
		return err
	case *crew.Vote:
		l.mu.Lock()
		id, err := s.crewVoteChange(ctx, c, 1, *m, hostUser, theme)
		l.mu.Unlock()
		if err == nil {
			s.crewSendBallot(ctx, c, id)
		}
		return err
	case *crew.Club:
		l.mu.Lock()
		err := s.crewClubChange(ctx, c, 1, *m, admin)
		l.mu.Unlock()
		if err == nil {
			s.crewSendClubs(ctx, c)
		}
		return err
	case *crew.Activity:
		for i := range m.Lines {
			m.Lines[i].H = 1
		}
		return b.ActivityIn(ctx, link.Peer{}, 1, *m)
	}
	return crew.ErrNotAllowed
}

// crewAnswer (house side) tells the owner this house joins (with who opted
// in here), declines or leaves.
func (s *Server) crewAnswer(crewID int64, answer string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, ok := s.crewByID(ctx, crewID)
	n := s.linkNode()
	if !ok || n == nil || c.mine() {
		return
	}
	a := crew.Answer{V: crew.Version, CID: c.CID, Answer: answer}
	if answer == crew.Join {
		a.People = s.crewPeopleHere(ctx, c.ID)
	}
	_ = n.Send(ctx, s.crewOwnerID(ctx, c), crew.TypeAnswer, a)
}

// crewsForgetPeer: a link ended. The crews it owns go; it leaves the crews
// owned here.
func (s *Server) crewsForgetPeer(ctx context.Context, peer int64) {
	_, _ = s.db.ExecContext(ctx, `DELETE FROM crews WHERE owner_peer = ?`, peer)
	var ids []int64
	if rows, err := s.db.QueryContext(ctx, `SELECT crew_id FROM crew_houses WHERE peer_id = ?`, peer); err == nil {
		for rows.Next() {
			var id int64
			if rows.Scan(&id) == nil {
				ids = append(ids, id)
			}
		}
		rows.Close()
	}
	_, _ = s.db.ExecContext(ctx, `DELETE FROM crew_houses WHERE peer_id = ?`, peer)
	for _, id := range ids {
		if c, ok := s.crewByID(ctx, id); ok {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				s.crewChanged(ctx, c)
			}()
		}
	}
}
