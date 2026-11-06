package api

// Usherr: linked screenings, step 2. Screenings over the link: a showtime
// booked here can be shared with linked Usherrs ("Invite linked Usherrs" in
// Showtime). Each invited Usherr gets the screening (film, start, pre-show,
// who hosts) and a screening key in its own link; it shows the screening in
// its Showtime with the host's name, its people answer I'm in / Can't make
// it / Maybe, and it says whether it has the film and how long its copy
// runs. The host sees each Usherr's answer, and tells everyone who's coming
// in one message sealed with the screening key (the relay fans it out).
//
// What the messages may do is decided in internal/link/screening (only the
// host offers, changes or cancels; only invited Usherrs answer). Every row
// hangs off a link, so unlinking takes the screenings away on both sides.

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/link"
	"github.com/rdborg/mediarium/internal/link/screening"
	"github.com/rdborg/mediarium/internal/notify"
)

const (
	screeningCutDiff    = 2                // minutes: more than this off, "may be a different cut"
	screeningResendSoon = 10 * time.Minute // sent again this often until the guest says it saw the latest
	screeningResendAll  = time.Hour        // and this often anyway, until it starts
	screeningKeep       = 6 * time.Hour    // a past screening is listed this long
)

// screeningRosterWait gathers answers that come close together into one
// roster message (tests make it short).
var screeningRosterWait = 1500 * time.Millisecond

// screeningProbe is how long a film file runs, in minutes (0 when it can't be
// read).
var screeningProbe = func(ctx context.Context, file string) int {
	secs, _, _ := probeClip(ctx, file)
	return int(math.Round(secs / 60))
}

// linkScreeningState is the screenings' share of the link's running state.
type linkScreeningState struct {
	mu     sync.Mutex
	roster map[int64]*time.Timer // showtime -> roster waiting to go
	stop   chan struct{}         // ends the screening job
}

// ---- keys ----

func (s *Server) sealScreeningKey(key []byte) (string, error) {
	if s.box == nil {
		return "", errors.New("no server key")
	}
	return s.box.Encrypt(base64.StdEncoding.EncodeToString(key))
}

func (s *Server) openScreeningKey(enc string) ([]byte, error) {
	if s.box == nil {
		return nil, errors.New("no server key")
	}
	plain, err := s.box.Decrypt(enc)
	if err != nil {
		return nil, err
	}
	key, err := base64.StdEncoding.DecodeString(plain)
	if err != nil || len(key) != link.GroupKeySize {
		return nil, errors.New("a screening key that can't be read")
	}
	return key, nil
}

// linkGroupKey finds a screening key this Usherr was given (link.NodeOptions).
func (s *Server) linkGroupKey(ctx context.Context, kid []byte) (link.GroupKey, bool) {
	var enc, sid, owner string
	err := s.db.QueryRowContext(ctx, `SELECT i.skey, i.sid, p.their_id FROM link_screenings_in i JOIN link_peers p ON p.id = i.peer_id
		WHERE i.kid = ? AND p.status = 'linked' AND p.their_id <> ''`, hex.EncodeToString(kid)).Scan(&enc, &sid, &owner)
	if err != nil {
		return link.GroupKey{}, false
	}
	key, err := s.openScreeningKey(enc)
	if err != nil {
		return link.GroupKey{}, false
	}
	return link.GroupKey{Key: key, Owner: owner, Scope: sid}, true
}

// peerRow is a linked Usherr's row id here.
func (s *Server) peerRow(ctx context.Context, their string) (int64, bool) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM link_peers WHERE their_id = ? AND their_id <> '' AND status = 'linked'`, their).Scan(&id)
	return id, err == nil
}

// ---- what the messages do (screening.Book) ----

type linkBook struct{ s *Server }

func (b linkBook) HostOf(ctx context.Context, sid string) (string, bool, error) {
	var one int
	if err := b.s.db.QueryRowContext(ctx, `SELECT 1 FROM link_screenings_out WHERE sid = ?`, sid).Scan(&one); err == nil {
		return "", true, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return "", false, err
	}
	var host string
	err := b.s.db.QueryRowContext(ctx, `SELECT p.their_id FROM link_screenings_in i JOIN link_peers p ON p.id = i.peer_id WHERE i.sid = ?`, sid).Scan(&host)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err == nil && host == "" {
		host = "unlinked" // never the sender
	}
	return host, false, err
}

func (b linkBook) Invited(ctx context.Context, sid, guest string) (bool, error) {
	var one int
	err := b.s.db.QueryRowContext(ctx, `SELECT 1 FROM link_screening_guests g
		JOIN link_screenings_out o ON o.showtime_id = g.showtime_id
		JOIN link_peers p ON p.id = g.peer_id
		WHERE o.sid = ? AND p.their_id = ? AND p.their_id <> '' AND p.status = 'linked'`, sid, guest).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// Offered: a linked Usherr offers a screening, or says it changed.
func (b linkBook) Offered(ctx context.Context, host link.Peer, o screening.Offer, key []byte) error {
	s := b.s
	peer, ok := s.peerRow(ctx, host.ID)
	if !ok {
		return link.ErrNotLinked
	}
	enc, err := s.sealScreeningKey(key)
	if err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	var id int64
	var rev int
	var at int64
	var pre int
	err = s.db.QueryRowContext(ctx, `SELECT id, rev, at_ms, preshow_min FROM link_screenings_in WHERE sid = ?`, o.SID).Scan(&id, &rev, &at, &pre)
	isNew := errors.Is(err, sql.ErrNoRows)
	switch {
	case isNew:
		res, err := s.db.ExecContext(ctx, `INSERT INTO link_screenings_in (peer_id, sid, kid, skey, rev, tmdb_id, title, year, poster_url, runtime, copy_min, edition, at_ms, preshow_min, host_name, created_ms, updated_ms)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			peer, o.SID, hex.EncodeToString(link.GroupKID(key)), enc, o.Rev, o.Film.TMDB, o.Film.Title, o.Film.Year, o.Film.Poster, o.Film.Runtime, o.Film.Copy, o.Film.Edition, o.At, o.Preshow, o.Host, now, now)
		if err != nil {
			return err
		}
		id, _ = res.LastInsertId()
	case err != nil:
		return err
	case o.Rev < rev:
		// An older one (sent again before a change): only said it was seen.
	default:
		if _, err := s.db.ExecContext(ctx, `UPDATE link_screenings_in SET kid = ?, skey = ?, rev = ?, tmdb_id = ?, title = ?, year = ?, poster_url = ?, runtime = ?, copy_min = ?, edition = ?,
			at_ms = ?, preshow_min = ?, host_name = ?, updated_ms = ? WHERE id = ?`,
			hex.EncodeToString(link.GroupKID(key)), enc, o.Rev, o.Film.TMDB, o.Film.Title, o.Film.Year, o.Film.Poster, o.Film.Runtime, o.Film.Copy, o.Film.Edition,
			o.At, o.Preshow, o.Host, now, id); err != nil {
			return err
		}
	}
	who := s.linkPeerName(ctx, peer)
	film := o.Film.Title
	if o.Film.Year > 0 {
		film = fmt.Sprintf("%s (%d)", o.Film.Title, o.Film.Year)
	}
	switch {
	case isNew:
		s.notifyEvent(notify.EventScreening, "Screening: "+film,
			fmt.Sprintf("%s invites you to %s, %s. Open Showtime to say if you're in.", who, film, screeningWhen(o.At)))
	case o.Rev >= rev && (o.At != at || o.Preshow != pre):
		s.notifyEvent(notify.EventScreening, "Screening moved: "+film,
			fmt.Sprintf("%s moved %s to %s.", who, film, screeningWhen(o.At)))
	}
	// Said back: whether this Usherr has the film (which also tells the host
	// the screening arrived), and the household's answer when there is one.
	go s.screeningTell(id, true)
	return nil
}

func (b linkBook) Cancelled(ctx context.Context, host link.Peer, sid string) error {
	s := b.s
	var title string
	var year int
	var peer int64
	if err := s.db.QueryRowContext(ctx, `SELECT title, year, peer_id FROM link_screenings_in WHERE sid = ?`, sid).Scan(&title, &year, &peer); err != nil {
		return nil // never had it
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM link_screenings_in WHERE sid = ?`, sid); err != nil {
		return err
	}
	film := title
	if year > 0 {
		film = fmt.Sprintf("%s (%d)", title, year)
	}
	s.notifyEvent(notify.EventScreening, "Screening called off: "+film, fmt.Sprintf("%s called off %s.", s.linkPeerName(ctx, peer), film))
	return nil
}

func (b linkBook) RosterIn(ctx context.Context, host link.Peer, r screening.Roster) error {
	guests := r.Guests
	if guests == nil {
		guests = []screening.Guest{}
	}
	raw, _ := json.Marshal(guests)
	_, err := b.s.db.ExecContext(ctx, `UPDATE link_screenings_in SET roster = ? WHERE sid = ?`, string(raw), r.SID)
	return err
}

func (b linkBook) RSVPIn(ctx context.Context, guest link.Peer, r screening.RSVP) error {
	s := b.s
	peer, ok := s.peerRow(ctx, guest.ID)
	if !ok {
		return link.ErrNotLinked
	}
	var showtime int64
	if err := s.db.QueryRowContext(ctx, `SELECT showtime_id FROM link_screenings_out WHERE sid = ?`, r.SID).Scan(&showtime); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE link_screening_guests SET answer = ?, count = ?, answered_ms = ? WHERE showtime_id = ? AND peer_id = ?`,
		r.Answer, r.Count, time.Now().UnixMilli(), showtime, peer); err != nil {
		return err
	}
	s.screeningRosterSoon(showtime)
	return nil
}

func (b linkBook) ReadyIn(ctx context.Context, guest link.Peer, r screening.Ready) error {
	s := b.s
	peer, ok := s.peerRow(ctx, guest.ID)
	if !ok {
		return link.ErrNotLinked
	}
	_, err := s.db.ExecContext(ctx, `UPDATE link_screening_guests SET has = ?, runtime = ?, ready_rev = MAX(ready_rev, ?)
		WHERE peer_id = ? AND showtime_id = (SELECT showtime_id FROM link_screenings_out WHERE sid = ?)`, r.Has, r.Runtime, r.Rev, peer, r.SID)
	return err
}

// linkPeerName is what a linked Usherr is called here: its nickname, or the
// name it gives itself.
func (s *Server) linkPeerName(ctx context.Context, peer int64) string {
	var name, nick string
	_ = s.db.QueryRowContext(ctx, `SELECT name, nickname FROM link_peers WHERE id = ?`, peer).Scan(&name, &nick)
	if nick != "" {
		return nick
	}
	if name == "" {
		return "A linked Usherr"
	}
	return name
}

// screeningWhen is a start time in a notification: "Sat Nov 7, 8:00 PM".
func screeningWhen(ms int64) string {
	return time.UnixMilli(ms).Local().Format("Mon Jan 2, 3:04 PM")
}

// ---- host side ----

type screeningOut struct {
	ShowtimeID int64
	SID        string
	Key        []byte
	Rev        int
	Copy       int
}

func (s *Server) screeningOutOf(ctx context.Context, showtimeID int64) (screeningOut, bool) {
	o := screeningOut{ShowtimeID: showtimeID}
	var enc string
	if s.db.QueryRowContext(ctx, `SELECT sid, skey, rev, copy_min FROM link_screenings_out WHERE showtime_id = ?`, showtimeID).Scan(&o.SID, &enc, &o.Rev, &o.Copy) != nil {
		return o, false
	}
	key, err := s.openScreeningKey(enc)
	if err != nil {
		return o, false
	}
	o.Key = key
	return o, true
}

// filmCopy is how long this Usherr's own copy of a film runs (0 when there's
// no file, or it can't be read), with the file.
func (s *Server) filmCopy(ctx context.Context, tmdb int, file string, known int) (minutes int, path string) {
	m, ok, err := s.MovieRepo.GetByTMDBID(tmdb)
	if err != nil || !ok || m.FilePath == "" {
		return 0, ""
	}
	if m.FilePath == file && known > 0 {
		return known, file
	}
	pctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	return screeningProbe(pctx, m.FilePath), m.FilePath
}

func (s *Server) offerOf(st showtime, o screeningOut) screening.Offer {
	poster := st.PosterURL
	if !strings.HasPrefix(poster, "https://image.tmdb.org/") {
		poster = ""
	}
	return screening.Offer{
		V: screening.Version, SID: o.SID, Rev: o.Rev,
		Film: screening.Film{TMDB: st.TMDBID, Title: st.Title, Year: st.Year, Runtime: max(min(st.Runtime, screening.MaxRuntime), 0), Copy: min(o.Copy, screening.MaxRuntime), Poster: poster},
		At:   st.AtMs, Preshow: min(st.PreshowMin, screening.MaxPreshow), Host: s.partyHost(st.UserID),
		Key: base64.StdEncoding.EncodeToString(o.Key),
	}
}

type screeningGuestRow struct {
	Peer  int64
	Their string
	Name  string
}

func (s *Server) screeningGuests(ctx context.Context, showtimeID int64) []screeningGuestRow {
	var out []screeningGuestRow
	rows, err := s.db.QueryContext(ctx, `SELECT g.peer_id, p.their_id, p.name FROM link_screening_guests g JOIN link_peers p ON p.id = g.peer_id
		WHERE g.showtime_id = ? AND p.status = 'linked' AND p.their_id <> '' ORDER BY g.peer_id`, showtimeID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var g screeningGuestRow
		if rows.Scan(&g.Peer, &g.Their, &g.Name) == nil {
			out = append(out, g)
		}
	}
	return out
}

// sendScreening sends the screening as it is now to one guest.
func (s *Server) sendScreening(ctx context.Context, n *link.Node, typ string, st showtime, o screeningOut, g screeningGuestRow) error {
	if err := n.Send(ctx, g.Their, typ, s.offerOf(st, o)); err != nil {
		return err
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE link_screening_guests SET sent_ms = ? WHERE showtime_id = ? AND peer_id = ?`, time.Now().UnixMilli(), o.ShowtimeID, g.Peer)
	return nil
}

var errLinkOff = errors.New(linkOffText)

// inviteLinked sets which linked Usherrs a showtime is shared with: the new
// ones get the screening, the ones taken off are told it's off for them
// (and the others get a new key, so they can't read on).
func (s *Server) inviteLinked(ctx context.Context, st showtime, want []int64) error {
	n := s.linkNode()
	if n == nil {
		return errLinkOff
	}
	wanted := map[int64]bool{}
	for _, id := range want {
		var status, their string
		if s.db.QueryRowContext(ctx, `SELECT status, their_id FROM link_peers WHERE id = ?`, id).Scan(&status, &their) != nil || status != "linked" || their == "" {
			return errors.New("That Usherr isn't linked anymore.")
		}
		wanted[id] = true
	}
	if len(wanted) > link.MaxFanout {
		return fmt.Errorf("A screening can have up to %d linked Usherrs.", link.MaxFanout)
	}
	o, had := s.screeningOutOf(ctx, st.ID)
	if !had && len(wanted) == 0 {
		return nil
	}
	if !had {
		key := link.NewGroupKey()
		enc, err := s.sealScreeningKey(key)
		if err != nil {
			return err
		}
		copyMin, _ := s.filmCopy(ctx, st.TMDBID, "", 0)
		o = screeningOut{ShowtimeID: st.ID, SID: screening.NewSID(), Key: key, Rev: 1, Copy: copyMin}
		if _, err := s.db.ExecContext(ctx, `INSERT INTO link_screenings_out (showtime_id, sid, skey, rev, copy_min, created_ms) VALUES (?, ?, ?, 1, ?, ?)`,
			st.ID, o.SID, enc, o.Copy, time.Now().UnixMilli()); err != nil {
			return err
		}
	}
	current := s.screeningGuests(ctx, st.ID)
	have := map[int64]bool{}
	var gone []screeningGuestRow
	for _, g := range current {
		have[g.Peer] = true
		if !wanted[g.Peer] {
			gone = append(gone, g)
		}
	}
	for _, g := range gone {
		_ = n.Send(ctx, g.Their, screening.TypeCancel, screening.Cancel{V: screening.Version, SID: o.SID})
		_, _ = s.db.ExecContext(ctx, `DELETE FROM link_screening_guests WHERE showtime_id = ? AND peer_id = ?`, st.ID, g.Peer)
	}
	// Rows of links that ended are gone already (screeningsForgetPeer).
	_, _ = s.db.ExecContext(ctx, `DELETE FROM link_screening_guests WHERE showtime_id = ? AND peer_id NOT IN (SELECT id FROM link_peers WHERE status = 'linked')`, st.ID)
	if len(wanted) == 0 {
		_, err := s.db.ExecContext(ctx, `DELETE FROM link_screenings_out WHERE showtime_id = ?`, st.ID)
		return err
	}
	for id := range wanted {
		if !have[id] {
			if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO link_screening_guests (showtime_id, peer_id) VALUES (?, ?)`, st.ID, id); err != nil {
				return err
			}
		}
	}
	if len(gone) > 0 {
		// The ones still invited get a new key and are told about it.
		return s.screeningChanged(ctx, st, true)
	}
	for _, g := range s.screeningGuests(ctx, st.ID) {
		if !have[g.Peer] {
			_ = s.sendScreening(ctx, n, screening.TypeOffer, st, o, g)
		}
	}
	s.screeningRosterSoon(st.ID)
	return nil
}

// screeningChanged tells every guest of a shared showtime what it is now
// (moved, or a new key when newKey).
func (s *Server) screeningChanged(ctx context.Context, st showtime, newKey bool) error {
	o, ok := s.screeningOutOf(ctx, st.ID)
	if !ok {
		return nil
	}
	o.Rev++
	if newKey {
		o.Key = link.NewGroupKey()
		enc, err := s.sealScreeningKey(o.Key)
		if err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE link_screenings_out SET skey = ?, rev = ? WHERE showtime_id = ?`, enc, o.Rev, st.ID); err != nil {
			return err
		}
	} else if _, err := s.db.ExecContext(ctx, `UPDATE link_screenings_out SET rev = ? WHERE showtime_id = ?`, o.Rev, st.ID); err != nil {
		return err
	}
	n := s.linkNode()
	if n == nil {
		return nil // sent when linking is on again (the screening job)
	}
	for _, g := range s.screeningGuests(ctx, st.ID) {
		_ = s.sendScreening(ctx, n, screening.TypeUpdate, st, o, g)
	}
	if newKey {
		s.screeningRosterSoon(st.ID)
	}
	return nil
}

// screeningMoved: a shared showtime has another time or pre-show (PUT).
func (s *Server) screeningMoved(showtimeID int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if st, ok := s.showtimeByID(ctx, showtimeID); ok {
		_ = s.screeningChanged(ctx, st, false)
	}
}

// screeningCalledOff tells the guests of a showtime about to be cancelled.
func (s *Server) screeningCalledOff(ctx context.Context, showtimeID int64) {
	o, ok := s.screeningOutOf(ctx, showtimeID)
	n := s.linkNode()
	if !ok || n == nil {
		return
	}
	for _, g := range s.screeningGuests(ctx, showtimeID) {
		_ = n.Send(ctx, g.Their, screening.TypeCancel, screening.Cancel{V: screening.Version, SID: o.SID})
	}
}

// screeningRosterSoon sends who's coming to everyone at a screening, a
// moment from now (answers close together go as one).
func (s *Server) screeningRosterSoon(showtimeID int64) {
	l := &s.linkS.scr
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.roster == nil {
		l.roster = map[int64]*time.Timer{}
	}
	if _, waiting := l.roster[showtimeID]; waiting {
		return
	}
	l.roster[showtimeID] = time.AfterFunc(screeningRosterWait, func() {
		l.mu.Lock()
		delete(l.roster, showtimeID)
		l.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = s.sendRoster(ctx, showtimeID)
	})
}

func (s *Server) sendRoster(ctx context.Context, showtimeID int64) error {
	o, ok := s.screeningOutOf(ctx, showtimeID)
	n := s.linkNode()
	if !ok || n == nil {
		return nil
	}
	r := screening.Roster{V: screening.Version, SID: o.SID, Rev: o.Rev, Guests: []screening.Guest{}}
	var to []string
	rows, err := s.db.QueryContext(ctx, `SELECT p.their_id, p.name, g.answer, g.count FROM link_screening_guests g JOIN link_peers p ON p.id = g.peer_id
		WHERE g.showtime_id = ? AND p.status = 'linked' AND p.their_id <> '' ORDER BY g.answer = '', g.answered_ms, g.peer_id`, showtimeID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var their string
		var g screening.Guest
		if rows.Scan(&their, &g.Name, &g.Answer, &g.Count) == nil {
			g.Name = link.CleanName(g.Name)
			r.Guests = append(r.Guests, g)
			to = append(to, their)
		}
	}
	rows.Close()
	if len(to) == 0 {
		return nil
	}
	return n.SendGroup(ctx, o.Key, to, screening.TypeRoster, r)
}

// screeningsForgetPeer: a link ended. Its screenings go on both sides; the
// screenings it was a guest at get a new key for the others.
func (s *Server) screeningsForgetPeer(ctx context.Context, peer int64) {
	var showtimes []int64
	if rows, err := s.db.QueryContext(ctx, `SELECT showtime_id FROM link_screening_guests WHERE peer_id = ?`, peer); err == nil {
		for rows.Next() {
			var id int64
			if rows.Scan(&id) == nil {
				showtimes = append(showtimes, id)
			}
		}
		rows.Close()
	}
	_, _ = s.db.ExecContext(ctx, `DELETE FROM link_screening_guests WHERE peer_id = ?`, peer)
	_, _ = s.db.ExecContext(ctx, `DELETE FROM link_screenings_in WHERE peer_id = ?`, peer)
	if len(showtimes) == 0 {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		for _, id := range showtimes {
			if st, ok := s.showtimeByID(ctx, id); ok {
				_ = s.screeningChanged(ctx, st, true)
			}
		}
	}()
}

// ---- guest side ----

// screeningHave checks whether this Usherr has a film: a file in the library
// (with its runtime), a saved stream, or on its way (in the library without
// a file yet, or asked for).
func (s *Server) screeningHave(ctx context.Context, tmdb int, file string, known int) (has string, runtime int, path string) {
	if mins, p := s.filmCopy(ctx, tmdb, file, known); p != "" {
		return screening.HasYes, mins, p
	}
	var one int
	if s.db.QueryRowContext(ctx, `SELECT 1 FROM streamed_files f JOIN streamed_titles t ON t.id = f.title_id WHERE t.kind = 'movie' AND t.tmdb_id = ? LIMIT 1`, tmdb).Scan(&one) == nil {
		return screening.HasYes, 0, ""
	}
	if m, ok, err := s.MovieRepo.GetByTMDBID(tmdb); err == nil && ok && (m.Monitored || m.Status == "downloading") {
		return screening.HasGetting, 0, ""
	}
	if s.db.QueryRowContext(ctx, `SELECT 1 FROM requests WHERE kind = 'movie' AND status IN ('pending', 'approved') AND json_extract(payload, '$.tmdbId') = ? LIMIT 1`, tmdb).Scan(&one) == nil {
		return screening.HasGetting, 0, ""
	}
	return screening.HasNo, 0, ""
}

// household is what this Usherr answers for its people: going (how many),
// else maybe (how many), else not going; "" when nobody answered.
func (s *Server) household(ctx context.Context, screeningID int64) (string, int) {
	counts := map[string]int{}
	rows, err := s.db.QueryContext(ctx, `SELECT answer, COUNT(*) FROM link_screening_people WHERE screening_id = ? GROUP BY answer`, screeningID)
	if err != nil {
		return "", 0
	}
	defer rows.Close()
	for rows.Next() {
		var a string
		var c int
		if rows.Scan(&a, &c) == nil {
			counts[a] = c
		}
	}
	switch {
	case counts[screening.Going] > 0:
		return screening.Going, min(counts[screening.Going], screening.MaxHousehold)
	case counts[screening.Maybe] > 0:
		return screening.Maybe, min(counts[screening.Maybe], screening.MaxHousehold)
	case counts[screening.NotGoing] > 0:
		return screening.NotGoing, 0
	}
	return "", 0
}

// screeningTell checks whether this Usherr has the film of a screening it's
// invited to, and tells the host when that changed (always, with the
// household's answer, when force: an offer just came).
func (s *Server) screeningTell(id int64, force bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	var sid, their, file, told string
	var tmdb, rev, mine int
	err := s.db.QueryRowContext(ctx, `SELECT i.sid, p.their_id, i.tmdb_id, i.rev, i.my_file, i.my_runtime, i.told FROM link_screenings_in i JOIN link_peers p ON p.id = i.peer_id
		WHERE i.id = ? AND p.status = 'linked'`, id).Scan(&sid, &their, &tmdb, &rev, &file, &mine, &told)
	if err != nil {
		return
	}
	has, runtime, path := s.screeningHave(ctx, tmdb, file, mine)
	now := fmt.Sprintf("%s/%d/%d", has, runtime, rev)
	_, _ = s.db.ExecContext(ctx, `UPDATE link_screenings_in SET has = ?, my_runtime = ?, my_file = ? WHERE id = ?`, has, runtime, path, id)
	n := s.linkNode()
	if n == nil || (!force && now == told) {
		return
	}
	if n.Send(ctx, their, screening.TypeReady, screening.Ready{V: screening.Version, SID: sid, Rev: rev, Has: has, Runtime: min(runtime, screening.MaxRuntime)}) == nil {
		_, _ = s.db.ExecContext(ctx, `UPDATE link_screenings_in SET told = ? WHERE id = ?`, now, id)
	}
	if force {
		if a, c := s.household(ctx, id); a != "" {
			_ = n.Send(ctx, their, screening.TypeRSVP, screening.RSVP{V: screening.Version, SID: sid, Answer: a, Count: c})
		}
	}
}

// ---- the screening job ----

// startScreeningJob runs while linking is on: screenings are sent again to
// guests that haven't said they saw the latest (a message waits only so long
// at the relay), and each Usherr looks again whether it has the films it's
// invited to (a download may have finished).
func (s *Server) startScreeningJob() {
	l := &s.linkS.scr
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
				s.screeningJob(context.Background())
			}
		}
	}()
}

func (s *Server) stopScreeningJob() {
	l := &s.linkS.scr
	l.mu.Lock()
	if l.stop != nil {
		close(l.stop)
		l.stop = nil
	}
	l.mu.Unlock()
}

func (s *Server) screeningJob(ctx context.Context) {
	n := s.linkNode()
	if n == nil {
		return
	}
	now := time.Now()
	nowMs := now.UnixMilli()
	// Host side.
	type resend struct {
		showtime int64
		g        screeningGuestRow
	}
	var todo []resend
	rows, err := s.db.QueryContext(ctx, `SELECT g.showtime_id, g.peer_id, p.their_id, p.name FROM link_screening_guests g
		JOIN link_screenings_out o ON o.showtime_id = g.showtime_id
		JOIN showtimes st ON st.id = g.showtime_id
		JOIN link_peers p ON p.id = g.peer_id
		WHERE p.status = 'linked' AND p.their_id <> '' AND st.at_ms > ? AND st.state <> 'done'
			AND ((g.ready_rev < o.rev AND g.sent_ms < ?) OR g.sent_ms < ?)`,
		nowMs, nowMs-screeningResendSoon.Milliseconds(), nowMs-screeningResendAll.Milliseconds())
	if err == nil {
		for rows.Next() {
			var r resend
			if rows.Scan(&r.showtime, &r.g.Peer, &r.g.Their, &r.g.Name) == nil {
				todo = append(todo, r)
			}
		}
		rows.Close()
	}
	for _, r := range todo {
		st, ok := s.showtimeByID(ctx, r.showtime)
		o, ok2 := s.screeningOutOf(ctx, r.showtime)
		if ok && ok2 {
			_ = s.sendScreening(ctx, n, screening.TypeUpdate, st, o, r.g)
		}
	}
	// Guest side.
	var ids []int64
	if rows, err := s.db.QueryContext(ctx, `SELECT id FROM link_screenings_in WHERE at_ms > ?`, nowMs); err == nil {
		for rows.Next() {
			var id int64
			if rows.Scan(&id) == nil {
				ids = append(ids, id)
			}
		}
		rows.Close()
	}
	for _, id := range ids {
		s.screeningTell(id, false)
	}
	_, _ = s.db.ExecContext(ctx, `DELETE FROM link_screenings_in WHERE at_ms < ?`, now.Add(-2*screeningKeep).UnixMilli())
}

// ---- what the pages show ----

type linkedSummary struct {
	Invited int `json:"invited"` // linked Usherrs invited
	Going   int `json:"going"`   // of them, coming
	People  int `json:"people"`  // how many people they bring
}

// withLinked adds who's invited from linked Usherrs to each shared showtime.
func (s *Server) withLinked(ctx context.Context, list []showtime) {
	if len(list) == 0 {
		return
	}
	sums := map[int64]*linkedSummary{}
	rows, err := s.db.QueryContext(ctx, `SELECT g.showtime_id, g.answer, g.count FROM link_screening_guests g JOIN link_peers p ON p.id = g.peer_id WHERE p.status = 'linked'`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var answer string
		var count int
		if rows.Scan(&id, &answer, &count) != nil {
			continue
		}
		sum := sums[id]
		if sum == nil {
			sum = &linkedSummary{}
			sums[id] = sum
		}
		sum.Invited++
		if answer == screening.Going {
			sum.Going++
			sum.People += count
		}
	}
	for i := range list {
		list[i].Linked = sums[list[i].ID]
	}
}

// linkedPeersOn reports whether linking is on and there's an Usherr to
// invite.
func (s *Server) linkedPeersOn(ctx context.Context) bool {
	if s.linkNode() == nil {
		return false
	}
	var n int
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM link_peers WHERE status = 'linked' AND their_id <> ''`).Scan(&n)
	return n > 0
}

type linkedGuestOut struct {
	ID           int64  `json:"id"` // the link
	Name         string `json:"name"`
	Invited      bool   `json:"invited"`
	Answer       string `json:"answer,omitempty"` // going, not, maybe
	Count        int    `json:"count,omitempty"`
	Has          string `json:"has,omitempty"` // yes, getting, no
	Runtime      int    `json:"runtime,omitempty"`
	DifferentCut bool   `json:"differentCut,omitempty"`
	Seen         bool   `json:"seen"` // it said it has the latest
}

// differentCut: two copies' runtimes more than ~2 minutes apart (both known).
func differentCut(a, b int) bool {
	return a > 0 && b > 0 && (a-b > screeningCutDiff || b-a > screeningCutDiff)
}

func (s *Server) linkedAnswer(ctx context.Context, st showtime) map[string]any {
	o, shared := s.screeningOutOf(ctx, st.ID)
	ref := st.Runtime
	if shared && o.Copy > 0 {
		ref = o.Copy
	}
	out := []linkedGuestOut{}
	rows, err := s.db.QueryContext(ctx, `SELECT p.id, COALESCE(NULLIF(p.nickname, ''), p.name), g.peer_id IS NOT NULL, COALESCE(g.answer, ''), COALESCE(g.count, 0),
			COALESCE(g.has, ''), COALESCE(g.runtime, 0), COALESCE(g.ready_rev, 0)
		FROM link_peers p LEFT JOIN link_screening_guests g ON g.peer_id = p.id AND g.showtime_id = ?
		WHERE p.status = 'linked' AND p.their_id <> '' ORDER BY COALESCE(NULLIF(p.nickname, ''), p.name) COLLATE NOCASE, p.id`, st.ID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var g linkedGuestOut
			var readyRev int
			if rows.Scan(&g.ID, &g.Name, &g.Invited, &g.Answer, &g.Count, &g.Has, &g.Runtime, &readyRev) != nil {
				continue
			}
			g.DifferentCut = g.Has == screening.HasYes && differentCut(g.Runtime, ref)
			g.Seen = g.Invited && shared && readyRev >= o.Rev
			out = append(out, g)
		}
	}
	return map[string]any{"on": s.linkNode() != nil, "peers": out, "runtime": ref}
}

// GET /api/home/showtimes/{id}/linked: the linked Usherrs, which are invited
// and what they answered (whoever booked it, or an administrator).
func (s *Server) handleShowtimeLinked(w http.ResponseWriter, r *http.Request) {
	st, code, msg := s.showtimeFor(r)
	if code != 0 {
		writeError(w, code, msg)
		return
	}
	writeJSON(w, http.StatusOK, s.linkedAnswer(r.Context(), st))
}

// PUT /api/home/showtimes/{id}/linked {"peers":[link ids]}: share it with
// these linked Usherrs (and no others).
func (s *Server) handlePutShowtimeLinked(w http.ResponseWriter, r *http.Request) {
	st, code, msg := s.showtimeFor(r)
	if code != 0 {
		writeError(w, code, msg)
		return
	}
	var body struct {
		Peers []int64 `json:"peers"`
	}
	if err := decodeJSON(r, &body); err != nil || len(body.Peers) > 1000 {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	if st.State == "done" {
		writeError(w, http.StatusConflict, "That showtime is over.")
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 45*time.Second)
	defer cancel()
	if err := s.inviteLinked(ctx, st, body.Peers); err != nil {
		if errors.Is(err, errLinkOff) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.linkedAnswer(r.Context(), st))
}

type screeningPersonOut struct {
	Name   string `json:"name"`
	Answer string `json:"answer"`
}

type screeningInOut struct {
	ID           int64                `json:"id"`
	Host         string               `json:"host"`               // the linked Usherr
	HostName     string               `json:"hostName,omitempty"` // the person hosting
	TMDBID       int                  `json:"tmdbId"`
	Title        string               `json:"title"`
	Year         int                  `json:"year,omitempty"`
	PosterURL    string               `json:"posterUrl,omitempty"`
	Runtime      int                  `json:"runtime,omitempty"`
	Edition      string               `json:"edition,omitempty"`
	AtMs         int64                `json:"atMs"`
	PreshowMin   int                  `json:"preshowMin"`
	Mine         string               `json:"mine,omitempty"` // this person's answer
	People       []screeningPersonOut `json:"people"`         // this household's answers
	Roster       []screening.Guest    `json:"roster"`         // who's coming, from the host
	Has          string               `json:"has,omitempty"`  // yes, getting, no
	MyRuntime    int                  `json:"myRuntime,omitempty"`
	DifferentCut bool                 `json:"differentCut,omitempty"`
}

func (s *Server) screeningsIn(ctx context.Context, user int64) []screeningInOut {
	out := []screeningInOut{}
	rows, err := s.db.QueryContext(ctx, `SELECT i.id, COALESCE(NULLIF(p.nickname, ''), p.name), i.host_name, i.tmdb_id, i.title, i.year, i.poster_url, i.runtime, i.copy_min, i.edition,
			i.at_ms, i.preshow_min, i.roster, i.has, i.my_runtime
		FROM link_screenings_in i JOIN link_peers p ON p.id = i.peer_id
		WHERE p.status = 'linked' AND i.at_ms > ? ORDER BY i.at_ms, i.id`, time.Now().Add(-screeningKeep).UnixMilli())
	if err != nil {
		return out
	}
	var copies []int
	for rows.Next() {
		var x screeningInOut
		var roster string
		var copyMin int
		if rows.Scan(&x.ID, &x.Host, &x.HostName, &x.TMDBID, &x.Title, &x.Year, &x.PosterURL, &x.Runtime, &copyMin, &x.Edition, &x.AtMs, &x.PreshowMin, &roster, &x.Has, &x.MyRuntime) != nil {
			continue
		}
		if json.Unmarshal([]byte(roster), &x.Roster) != nil || x.Roster == nil {
			x.Roster = []screening.Guest{}
		}
		out = append(out, x)
		copies = append(copies, copyMin)
	}
	rows.Close()
	for i := range out {
		x := &out[i]
		ref := x.Runtime
		if copies[i] > 0 {
			ref = copies[i]
		}
		x.DifferentCut = x.Has == screening.HasYes && differentCut(x.MyRuntime, ref)
		x.People = []screeningPersonOut{}
		prow, err := s.db.QueryContext(ctx, `SELECT sp.user_id, COALESCE(NULLIF(TRIM(u.first_name), ''), u.username), sp.answer FROM link_screening_people sp JOIN users u ON u.id = sp.user_id
			WHERE sp.screening_id = ? ORDER BY sp.at_ms`, x.ID)
		if err != nil {
			continue
		}
		for prow.Next() {
			var uid int64
			var p screeningPersonOut
			if prow.Scan(&uid, &p.Name, &p.Answer) == nil {
				x.People = append(x.People, p)
				if uid == user {
					x.Mine = p.Answer
				}
			}
		}
		prow.Close()
	}
	return out
}

// GET /api/home/screenings: the screenings linked Usherrs invited this one
// to, with who's coming and whether this Usherr has the film.
func (s *Server) handleScreeningsIn(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"screenings": s.screeningsIn(r.Context(), requestUserID(r)), "on": s.linkNode() != nil})
}

// PUT /api/home/screenings/{id}/rsvp {"answer": "going"|"not"|"maybe"}: this
// person's answer; the household's goes to the host.
func (s *Server) handleScreeningRSVP(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	var body struct {
		Answer string `json:"answer"`
	}
	if !ok || decodeJSON(r, &body) != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	if body.Answer != screening.Going && body.Answer != screening.NotGoing && body.Answer != screening.Maybe {
		writeError(w, http.StatusBadRequest, "Say I'm in, Can't make it or Maybe.")
		return
	}
	ctx := r.Context()
	var sid, their string
	var at int64
	if err := s.db.QueryRowContext(ctx, `SELECT i.sid, p.their_id, i.at_ms FROM link_screenings_in i JOIN link_peers p ON p.id = i.peer_id WHERE i.id = ? AND p.status = 'linked'`, id).Scan(&sid, &their, &at); err != nil {
		writeError(w, http.StatusNotFound, "That screening isn't here anymore.")
		return
	}
	n := s.linkNode()
	if n == nil {
		writeError(w, http.StatusConflict, "Linking with other Usherrs is switched off here, so your answer can't reach the host.")
		return
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO link_screening_people (screening_id, user_id, answer, at_ms) VALUES (?, ?, ?, ?)
		ON CONFLICT (screening_id, user_id) DO UPDATE SET answer = excluded.answer, at_ms = excluded.at_ms`, id, requestUserID(r), body.Answer, time.Now().UnixMilli()); err != nil {
		writeError(w, http.StatusInternalServerError, "Your answer couldn't be saved.")
		return
	}
	if a, c := s.household(ctx, id); a != "" {
		if err := n.Send(ctx, their, screening.TypeRSVP, screening.RSVP{V: screening.Version, SID: sid, Answer: a, Count: c}); err != nil {
			writeError(w, http.StatusBadGateway, "Your answer is saved, but it couldn't be sent to the host yet.")
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"screenings": s.screeningsIn(ctx, requestUserID(r))})
}

// POST /api/home/screenings/{id}/check: look again whether this Usherr has
// the film (after asking for it), and tell the host.
func (s *Server) handleScreeningCheck(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	s.screeningTell(id, false)
	writeJSON(w, http.StatusOK, map[string]any{"screenings": s.screeningsIn(r.Context(), requestUserID(r))})
}
