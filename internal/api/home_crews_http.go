package api

// Usherr: Crews, the page's API (home_crews.go). Anyone signed in may see
// the crews this Usherr is in; a person takes part (chat, list, vote,
// clubs) only after joining a crew themselves. "Share what I watch" is each
// person's own switch, off until they turn it on.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/link"
	"github.com/rdborg/mediarium/internal/link/crew"
)

type crewHouseOut struct {
	H      int      `json:"h"`
	Name   string   `json:"name"`
	State  string   `json:"state"`
	People []string `json:"people"`
	Me     bool     `json:"me"`
	Online bool     `json:"online"` // seen on the link in the last few minutes (when linked with it)
}

type crewSummaryOut struct {
	ID      int64          `json:"id"`
	Name    string         `json:"name"`
	By      string         `json:"by,omitempty"`
	Mine    bool           `json:"mine"`   // this Usherr runs it
	State   string         `json:"state"`  // invited, joined
	Joined  bool           `json:"joined"` // the asker opted in
	MyHid   int            `json:"myHid"`
	Houses  []crewHouseOut `json:"houses"`
	CanEdit bool           `json:"canEdit"` // rename, invite, end
}

func (s *Server) crewSummary(ctx context.Context, c crewRow, uid int64, admin bool) crewSummaryOut {
	out := crewSummaryOut{ID: c.ID, Name: c.Name, By: c.ByName, Mine: c.mine(), State: c.State, Joined: s.crewIn(ctx, c.ID, uid), MyHid: c.MyHid, Houses: []crewHouseOut{}}
	out.CanEdit = c.mine() && (admin || c.ByUser == uid)
	seen := map[string]int64{}
	if rows, err := s.db.QueryContext(ctx, `SELECT their_id, last_seen FROM link_peers WHERE status = 'linked' AND their_id <> ''`); err == nil {
		for rows.Next() {
			var id string
			var at int64
			if rows.Scan(&id, &at) == nil {
				seen[id] = at
			}
		}
		rows.Close()
	}
	soon := time.Now().Add(-5 * time.Minute).UnixMilli()
	for _, h := range s.crewHouses(ctx, c) {
		people := h.People
		if people == nil {
			people = []string{}
		}
		me := h.H == c.MyHid
		out.Houses = append(out.Houses, crewHouseOut{H: h.H, Name: h.Name, State: h.State, People: people, Me: me, Online: me || seen[h.ID] > soon})
	}
	return out
}

type crewPeerOut struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// GET /api/home/crews: the crews this Usherr is in or invited to, the linked
// Usherrs a crew can be made with, and the asker's sharing switch.
func (s *Server) handleCrews(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid, admin := requestUserID(r), isAdminRequest(r)
	crews := []crewSummaryOut{}
	if rows, err := s.db.QueryContext(ctx, `SELECT `+crewCols+` FROM crews ORDER BY state = 'invited' DESC, name COLLATE NOCASE, id`); err == nil {
		var list []crewRow
		for rows.Next() {
			if c, err := scanCrew(rows); err == nil {
				list = append(list, c)
			}
		}
		rows.Close()
		for _, c := range list {
			crews = append(crews, s.crewSummary(ctx, c, uid, admin))
		}
	}
	peers := []crewPeerOut{}
	if rows, err := s.db.QueryContext(ctx, `SELECT id, COALESCE(NULLIF(nickname, ''), name) FROM link_peers WHERE status = 'linked' AND their_id <> '' ORDER BY 2 COLLATE NOCASE, id`); err == nil {
		for rows.Next() {
			var p crewPeerOut
			if rows.Scan(&p.ID, &p.Name) == nil {
				peers = append(peers, p)
			}
		}
		rows.Close()
	}
	writeJSON(w, http.StatusOK, map[string]any{"on": s.linkNode() != nil, "crews": crews, "peers": peers, "sharing": s.crewSharing(ctx, uid)})
}

func (s *Server) crewSharing(ctx context.Context, uid int64) bool {
	var on int
	_ = s.db.QueryRowContext(ctx, `SELECT share FROM crew_sharing WHERE user_id = ?`, uid).Scan(&on)
	return on == 1
}

type crewEditBody struct {
	Name  string  `json:"name"`
	Peers []int64 `json:"peers"`
}

// crewSetHouses sets which linked Usherrs a crew owned here has: new ones
// are invited, the ones taken off are told.
func (s *Server) crewSetHouses(ctx context.Context, c crewRow, want []int64) error {
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
	if len(wanted) == 0 {
		return errors.New("Pick at least one linked Usherr.")
	}
	if len(wanted) > crew.MaxHouses-1 {
		return errors.New("A crew can have up to 11 linked Usherrs.")
	}
	used := map[int]bool{1: true}
	have := map[int64]bool{}
	for _, h := range s.crewHouseRows(ctx, c.ID, false) {
		have[h.Peer] = true
		if !wanted[h.Peer] {
			_ = n.Send(ctx, h.Their, crew.TypeEnd, crew.End{V: crew.Version, CID: c.CID})
			_, _ = s.db.ExecContext(ctx, `DELETE FROM crew_houses WHERE crew_id = ? AND peer_id = ?`, c.ID, h.Peer)
			continue
		}
		used[h.Hid] = true
	}
	for _, id := range want {
		if have[id] {
			continue
		}
		have[id] = true
		hid := 2
		for used[hid] {
			hid++
		}
		used[hid] = true
		if _, err := s.db.ExecContext(ctx, `INSERT INTO crew_houses (crew_id, peer_id, hid, sent_ms) VALUES (?, ?, ?, ?)`, c.ID, id, hid, time.Now().UnixMilli()); err != nil {
			return err
		}
	}
	s.crewChanged(ctx, c)
	return nil
}

// POST /api/home/crews {"name", "peers": [link ids]}: make a crew with
// linked Usherrs. They're sent an invite; the one who made it is in.
func (s *Server) handleCreateCrew(w http.ResponseWriter, r *http.Request) {
	var body crewEditBody
	if err := decodeJSON(r, &body); err != nil || len(body.Peers) > 100 {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	name := crew.CleanText(body.Name, crew.MaxName)
	if name == "" {
		writeError(w, http.StatusBadRequest, "Give the crew a name.")
		return
	}
	if s.linkNode() == nil {
		writeError(w, http.StatusConflict, linkOffText)
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 45*time.Second)
	defer cancel()
	var count int
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM crews`).Scan(&count)
	if count >= crewMax {
		writeError(w, http.StatusConflict, "This Usherr is in 20 crews already.")
		return
	}
	uid := requestUserID(r)
	now := time.Now().UnixMilli()
	res, err := s.db.ExecContext(ctx, `INSERT INTO crews (cid, name, by_name, by_user, rev, my_hid, state, created_ms, updated_ms) VALUES (?, ?, ?, ?, 1, 1, 'joined', ?, ?)`,
		crew.NewID(), name, s.crewPersonName(ctx, uid), uid, now, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The crew couldn't be saved.")
		return
	}
	id, _ := res.LastInsertId()
	_, _ = s.db.ExecContext(ctx, `INSERT INTO crew_people (crew_id, user_id, joined_ms) VALUES (?, ?, ?)`, id, uid, now)
	c, _ := s.crewByID(ctx, id)
	if err := s.crewSetHouses(ctx, c, body.Peers); err != nil {
		_, _ = s.db.ExecContext(ctx, `DELETE FROM crews WHERE id = ?`, id)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, s.crewSummary(ctx, c, uid, isAdminRequest(r)))
}

// crewFor reads the crew in the path.
func (s *Server) crewFor(w http.ResponseWriter, r *http.Request) (crewRow, bool) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return crewRow{}, false
	}
	c, ok := s.crewByID(r.Context(), id)
	if !ok {
		writeError(w, http.StatusNotFound, "That crew isn't here anymore.")
		return crewRow{}, false
	}
	return c, true
}

// crewMember reads the crew in the path, for a person who joined it.
func (s *Server) crewMember(w http.ResponseWriter, r *http.Request) (crewRow, bool) {
	c, ok := s.crewFor(w, r)
	if !ok {
		return c, false
	}
	if c.State != "joined" || !s.crewIn(r.Context(), c.ID, requestUserID(r)) {
		writeError(w, http.StatusForbidden, "Join this crew first.")
		return c, false
	}
	return c, true
}

// PUT /api/home/crews/{id} {"name", "peers"}: rename a crew run here, or
// change who's in it (whoever made it, or an administrator).
func (s *Server) handleEditCrew(w http.ResponseWriter, r *http.Request) {
	c, ok := s.crewFor(w, r)
	if !ok {
		return
	}
	if !c.mine() || (c.ByUser != requestUserID(r) && !isAdminRequest(r)) {
		writeError(w, http.StatusForbidden, "Only whoever made this crew can change it.")
		return
	}
	var body crewEditBody
	if err := decodeJSON(r, &body); err != nil || len(body.Peers) > 100 {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 45*time.Second)
	defer cancel()
	if name := crew.CleanText(body.Name, crew.MaxName); name != "" && name != c.Name {
		_, _ = s.db.ExecContext(ctx, `UPDATE crews SET name = ? WHERE id = ?`, name, c.ID)
		c.Name = name
	}
	if body.Peers != nil {
		if err := s.crewSetHouses(ctx, c, body.Peers); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	} else {
		s.crewChanged(ctx, c)
	}
	writeJSON(w, http.StatusOK, s.crewSummary(ctx, c, requestUserID(r), isAdminRequest(r)))
}

// DELETE /api/home/crews/{id}: end a crew run here (whoever made it, or an
// administrator). Its houses are told.
func (s *Server) handleEndCrew(w http.ResponseWriter, r *http.Request) {
	c, ok := s.crewFor(w, r)
	if !ok {
		return
	}
	if !c.mine() || (c.ByUser != requestUserID(r) && !isAdminRequest(r)) {
		writeError(w, http.StatusForbidden, "Only whoever made this crew can end it.")
		return
	}
	ctx := r.Context()
	if n := s.linkNode(); n != nil {
		for _, h := range s.crewHouseRows(ctx, c.ID, false) {
			_ = n.Send(ctx, h.Their, crew.TypeEnd, crew.End{V: crew.Version, CID: c.CID})
		}
	}
	_, _ = s.db.ExecContext(ctx, `DELETE FROM crews WHERE id = ?`, c.ID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// POST /api/home/crews/{id}/join: the asker joins a crew (and, for an
// invite, this Usherr says yes).
func (s *Server) handleJoinCrew(w http.ResponseWriter, r *http.Request) {
	c, ok := s.crewFor(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	if !c.mine() && s.linkNode() == nil {
		writeError(w, http.StatusConflict, "Linking with other Usherrs is switched off here, so the crew can't be told.")
		return
	}
	if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO crew_people (crew_id, user_id, joined_ms) VALUES (?, ?, ?)`, c.ID, requestUserID(r), time.Now().UnixMilli()); err != nil {
		writeError(w, http.StatusInternalServerError, "That couldn't be saved.")
		return
	}
	if c.mine() {
		go s.crewChangedSoon(c.ID)
	} else {
		_, _ = s.db.ExecContext(ctx, `UPDATE crews SET state = 'joined' WHERE id = ?`, c.ID)
		go s.crewAnswer(c.ID, crew.Join)
	}
	c, _ = s.crewByID(ctx, c.ID)
	writeJSON(w, http.StatusOK, s.crewSummary(ctx, c, requestUserID(r), isAdminRequest(r)))
}

func (s *Server) crewChangedSoon(crewID int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if c, ok := s.crewByID(ctx, crewID); ok {
		s.crewChanged(ctx, c)
	}
}

// POST /api/home/crews/{id}/leave: the asker leaves a crew. When nobody
// here is left in a crew run elsewhere, this Usherr leaves it.
func (s *Server) handleLeaveCrew(w http.ResponseWriter, r *http.Request) {
	c, ok := s.crewFor(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	uid := requestUserID(r)
	_, _ = s.db.ExecContext(ctx, `DELETE FROM crew_people WHERE crew_id = ? AND user_id = ?`, c.ID, uid)
	// Out of the club rotations too.
	name := s.crewPersonName(ctx, uid)
	for _, k := range s.crewClubRows(ctx, c.ID) {
		for _, seat := range k.Rotation {
			if seat.H == c.MyHid && seat.Who == name {
				_ = s.crewAct(ctx, c, crew.TypeClub, &crew.Club{V: crew.Version, CID: c.CID, Op: crew.ClubLeave, KID: k.KID, Who: name}, 0, 0, false)
			}
		}
	}
	left := len(s.crewPeopleHere(ctx, c.ID))
	switch {
	case c.mine():
		go s.crewChangedSoon(c.ID)
	case left == 0:
		s.crewAnswer(c.ID, crew.Leave)
		_, _ = s.db.ExecContext(ctx, `DELETE FROM crews WHERE id = ?`, c.ID)
	default:
		go s.crewAnswer(c.ID, crew.Join) // who's in here changed
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "gone": !c.mine() && left == 0})
}

// POST /api/home/crews/{id}/decline: say no to a crew invite.
func (s *Server) handleDeclineCrew(w http.ResponseWriter, r *http.Request) {
	c, ok := s.crewFor(w, r)
	if !ok {
		return
	}
	if c.mine() || c.State != "invited" {
		writeError(w, http.StatusConflict, "That crew isn't waiting for an answer.")
		return
	}
	s.crewAnswer(c.ID, crew.Decline)
	_, _ = s.db.ExecContext(r.Context(), `DELETE FROM crews WHERE id = ?`, c.ID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- the crew space ----

type crewChatOut struct {
	ID    int64      `json:"id"`
	H     int        `json:"h"`
	House string     `json:"house"`
	Who   string     `json:"who"`
	Text  string     `json:"text"`
	Card  *crew.Card `json:"card,omitempty"`
	At    int64      `json:"at"`
}

type crewEntryOut struct {
	crew.Film
	By    string      `json:"by"`
	ByH   int         `json:"byH"`
	Added int64       `json:"added"`
	Have  []crew.Have `json:"have"`
	Can   string      `json:"can"` // all, some, none
	Mine  bool        `json:"mine"`
}

type crewChoiceOut struct {
	crew.Film
	Voters []crew.Seat `json:"voters"`
	Have   []crew.Have `json:"have"`
	Can    string      `json:"can"`
}

type crewVoteOut struct {
	VID        string          `json:"vid"`
	HostH      int             `json:"hostH"`
	Host       string          `json:"host"`
	Closes     int64           `json:"closes"`
	At         int64           `json:"at"`
	Preshow    int             `json:"preshow"`
	Club       string          `json:"club,omitempty"`
	State      string          `json:"state"`
	Winner     int             `json:"winner,omitempty"`
	Choices    []crewChoiceOut `json:"choices"`
	MyVote     int             `json:"myVote,omitempty"`
	ShowtimeID int64           `json:"showtimeId,omitempty"` // booked in this Usherr's Showtime
}

type crewClubOut struct {
	crew.ClubState
	In     bool       `json:"in"`               // the asker is in the rotation
	MyTurn bool       `json:"myTurn"`           // and it's their pick
	Picker *crew.Seat `json:"picker,omitempty"` // whose pick the next night is
}

type crewLineOut struct {
	crew.Line
	House string `json:"house"`
}

// GET /api/home/crews/{id}: the crew space: chat, shared watchlist, votes,
// clubs and what the crew is watching.
func (s *Server) handleCrew(w http.ResponseWriter, r *http.Request) {
	c, ok := s.crewFor(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	uid := requestUserID(r)
	sum := s.crewSummary(ctx, c, uid, isAdminRequest(r))
	out := map[string]any{"crew": sum, "on": s.linkNode() != nil, "me": s.crewPersonName(ctx, uid), "sharing": s.crewSharing(ctx, uid)}
	if !sum.Joined {
		writeJSON(w, http.StatusOK, out) // nothing of the crew until they join
		return
	}
	houseName := map[int]string{}
	joined := 0
	for _, h := range sum.Houses {
		houseName[h.H] = h.Name
		if h.State == "joined" {
			joined++
		}
	}
	chat := []crewChatOut{}
	if rows, err := s.db.QueryContext(ctx, `SELECT id, hid, who, text, card, at_ms FROM (SELECT * FROM crew_chat WHERE crew_id = ? ORDER BY id DESC LIMIT 150) ORDER BY id`, c.ID); err == nil {
		for rows.Next() {
			var m crewChatOut
			var card string
			if rows.Scan(&m.ID, &m.H, &m.Who, &m.Text, &card, &m.At) != nil {
				continue
			}
			if card != "" {
				var cd crew.Card
				if json.Unmarshal([]byte(card), &cd) == nil {
					m.Card = &cd
				}
			}
			m.House = houseName[m.H]
			chat = append(chat, m)
		}
		rows.Close()
	}
	me := s.crewPersonName(ctx, uid)
	list := []crewEntryOut{}
	for _, e := range s.crewListRows(ctx, c.ID) {
		have := e.Have
		if have == nil {
			have = []crew.Have{}
		}
		list = append(list, crewEntryOut{Film: e.Film, By: e.By, ByH: e.ByH, Added: e.Added, Have: have, Can: crewCan(e.Have, joined), Mine: e.ByH == c.MyHid && (c.mine() || e.By == me)})
	}
	votes := []crewVoteOut{}
	if rows, err := s.db.QueryContext(ctx, `SELECT id, showtime_id FROM crew_votes WHERE crew_id = ? ORDER BY state = 'open' DESC, closes_ms DESC LIMIT 6`, c.ID); err == nil {
		type vr struct{ id, st int64 }
		var ids []vr
		for rows.Next() {
			var x vr
			if rows.Scan(&x.id, &x.st) == nil {
				ids = append(ids, x)
			}
		}
		rows.Close()
		for _, x := range ids {
			b, ok := s.crewBallot(ctx, c, x.id)
			if !ok || len(b.Choices) == 0 {
				continue // opened here, not taken by the owner yet
			}
			v := crewVoteOut{VID: b.VID, HostH: b.HostH, Host: b.Host, Closes: b.Closes, At: b.At, Preshow: b.Preshow, Club: b.Club, State: b.State, Winner: b.Winner, Choices: []crewChoiceOut{}, ShowtimeID: max(x.st, 0)}
			for _, ch := range b.Choices {
				voters, have := ch.Voters, ch.Have
				if voters == nil {
					voters = []crew.Seat{}
				}
				if have == nil {
					have = []crew.Have{}
				}
				for _, seat := range voters {
					if seat.H == c.MyHid && seat.Who == me {
						v.MyVote = ch.TMDB
					}
				}
				v.Choices = append(v.Choices, crewChoiceOut{Film: ch.Film, Voters: voters, Have: have, Can: crewCan(ch.Have, joined)})
			}
			votes = append(votes, v)
		}
	}
	clubs := []crewClubOut{}
	for _, k := range s.crewClubRows(ctx, c.ID) {
		o := crewClubOut{ClubState: k.ClubState}
		for i, seat := range k.Rotation {
			if seat.H == c.MyHid && seat.Who == me {
				o.In = true
				o.MyTurn = i == k.Turn
			}
		}
		if len(k.Rotation) > 0 {
			p := k.Rotation[min(k.Turn, len(k.Rotation)-1)]
			o.Picker = &p
		}
		clubs = append(clubs, o)
	}
	activity := []crewLineOut{}
	if rows, err := s.db.QueryContext(ctx, `SELECT hid, who, kind, title, part, stars, at_ms FROM crew_activity WHERE crew_id = ? ORDER BY at_ms DESC, id DESC LIMIT 30`, c.ID); err == nil {
		for rows.Next() {
			var l crewLineOut
			if rows.Scan(&l.H, &l.Who, &l.Kind, &l.Title, &l.Part, &l.Stars, &l.At) == nil {
				l.House = houseName[l.H]
				activity = append(activity, l)
			}
		}
		rows.Close()
	}
	out["chat"], out["list"], out["votes"], out["clubs"], out["activity"] = chat, list, votes, clubs, activity
	writeJSON(w, http.StatusOK, out)
}

// crewProblem writes why a crew action couldn't be done.
func crewProblem(w http.ResponseWriter, err error) {
	var msg string
	switch {
	case errors.Is(err, errLinkOff):
		writeError(w, http.StatusConflict, "Linking with other Usherrs is switched off here.")
		return
	case errors.Is(err, link.ErrNotLinked), errors.Is(err, crew.ErrNotAllowed):
		writeError(w, http.StatusConflict, "The Usherr that runs this crew can't be reached.")
		return
	default:
		msg = err.Error()
	}
	if msg == "" || strings.HasPrefix(msg, "link:") || strings.HasPrefix(msg, "crew:") {
		writeError(w, http.StatusBadGateway, "That couldn't be sent to the crew.")
		return
	}
	writeError(w, http.StatusBadRequest, msg)
}

// POST /api/home/crews/{id}/chat {"text"}: a line in the crew chat (plain
// text, up to 500 characters).
func (s *Server) handleCrewChat(w http.ResponseWriter, r *http.Request) {
	c, ok := s.crewMember(w, r)
	if !ok {
		return
	}
	var body struct {
		Text string `json:"text"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	if len([]rune(strings.TrimSpace(body.Text))) > crew.MaxText {
		writeError(w, http.StatusBadRequest, "A message can be up to 500 characters.")
		return
	}
	s.crewSay(w, r, c, crew.Chat{Text: body.Text})
}

// crewSay sends a chat line from the asker, kept here at once.
func (s *Server) crewSay(w http.ResponseWriter, r *http.Request, c crewRow, m crew.Chat) {
	ctx := r.Context()
	uid := requestUserID(r)
	if !s.crewRateOK("person/"+strconv.FormatInt(uid, 10), 8, 30*time.Second) {
		writeError(w, http.StatusTooManyRequests, "Slow down a little: try again in a few seconds.")
		return
	}
	m.V, m.CID, m.MID, m.Who, m.At, m.H = crew.Version, c.CID, crew.NewID(), s.crewPersonName(ctx, uid), time.Now().UnixMilli(), c.MyHid
	if !m.Check() {
		writeError(w, http.StatusBadRequest, "Write something first.")
		return
	}
	if !c.mine() {
		if s.linkNode() == nil {
			writeError(w, http.StatusConflict, "Linking with other Usherrs is switched off here.")
			return
		}
		s.crewSaveChat(ctx, c, m)
	}
	if err := s.crewAct(ctx, c, crew.TypeChat, &m, 0, 0, false); err != nil {
		crewProblem(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type crewFilmBody struct {
	TMDBID int    `json:"tmdbId"`
	Title  string `json:"title"`
	Year   int    `json:"year"`
	Poster string `json:"poster"` // a TMDB image path, or an image.tmdb.org address
}

func (b crewFilmBody) film() crew.Film {
	p := b.Poster
	if i := strings.LastIndex(p, "/"); strings.HasPrefix(p, "https://image.tmdb.org/") && i > 0 {
		p = p[i:]
	}
	return crew.Film{TMDB: b.TMDBID, Title: b.Title, Year: b.Year, Poster: p}
}

// POST /api/home/crews/{id}/send {"tmdbId", "title", "year", "poster", "to",
// "toWho", "note"}: send a film to a friend in the crew: it shows in the
// crew chat as a card, and their Usherr lets them know.
func (s *Server) handleCrewSend(w http.ResponseWriter, r *http.Request) {
	c, ok := s.crewMember(w, r)
	if !ok {
		return
	}
	var body struct {
		crewFilmBody
		To    int    `json:"to"`
		ToWho string `json:"toWho"`
		Note  string `json:"note"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	f := body.film()
	if f.TMDB <= 0 || strings.TrimSpace(f.Title) == "" {
		writeError(w, http.StatusBadRequest, "Say which film.")
		return
	}
	found := false
	for _, h := range s.crewHouses(r.Context(), c) {
		found = found || (h.H == body.To && h.State == "joined")
	}
	if !found {
		writeError(w, http.StatusBadRequest, "That friend isn't in this crew.")
		return
	}
	s.crewSay(w, r, c, crew.Chat{Card: &crew.Card{Film: f, Note: body.Note, To: body.To, ToWho: body.ToWho}})
}

// POST /api/home/crews/{id}/list {"tmdbId", "title", "year", "poster"}: add
// a film to the shared watchlist.
func (s *Server) handleCrewListAdd(w http.ResponseWriter, r *http.Request) {
	c, ok := s.crewMember(w, r)
	if !ok {
		return
	}
	var body crewFilmBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	f := body.film()
	m := &crew.List{V: crew.Version, CID: c.CID, Op: crew.ListAdd, Film: &f, Who: s.crewPersonName(r.Context(), requestUserID(r))}
	if err := s.crewAct(r.Context(), c, crew.TypeList, m, 0, 0, false); err != nil {
		crewProblem(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "sent": !c.mine()})
}

// DELETE /api/home/crews/{id}/list/{tmdb}: take a film you added off the
// shared watchlist.
func (s *Server) handleCrewListRemove(w http.ResponseWriter, r *http.Request) {
	c, ok := s.crewMember(w, r)
	if !ok {
		return
	}
	tmdb, err := strconv.Atoi(r.PathValue("tmdb"))
	if err != nil || tmdb <= 0 {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	var title string
	if s.db.QueryRowContext(r.Context(), `SELECT title FROM crew_list WHERE crew_id = ? AND tmdb_id = ?`, c.ID, tmdb).Scan(&title) != nil {
		writeError(w, http.StatusNotFound, "That film isn't on the list.")
		return
	}
	m := &crew.List{V: crew.Version, CID: c.CID, Op: crew.ListRemove, Film: &crew.Film{TMDB: tmdb, Title: title}}
	if err := s.crewAct(r.Context(), c, crew.TypeList, m, 0, 0, false); err != nil {
		crewProblem(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// POST /api/home/crews/{id}/votes {"films": [tmdb], "closesMs", "atMs",
// "preshowMin", "preshowTheme"}: open a vote on the next film, from the
// shared watchlist. Whoever opens it hosts: the winner is booked in their
// Showtime, with their pre-show.
func (s *Server) handleCrewOpenVote(w http.ResponseWriter, r *http.Request) {
	c, ok := s.crewMember(w, r)
	if !ok {
		return
	}
	var body struct {
		Films      []int `json:"films"`
		ClosesMs   int64 `json:"closesMs"`
		AtMs       int64 `json:"atMs"`
		PreshowMin int   `json:"preshowMin"`
		Theme      int64 `json:"preshowTheme"`
	}
	if err := decodeJSON(r, &body); err != nil || len(body.Films) > 100 {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	if len(body.Films) < 2 || len(body.Films) > crew.MaxBallot {
		writeError(w, http.StatusBadRequest, "Pick two to eight films from the shared watchlist.")
		return
	}
	if msg := crewVoteTimes(body.ClosesMs, body.AtMs, time.Now()); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if body.PreshowMin < 0 || body.PreshowMin > crew.MaxPreshow {
		writeError(w, http.StatusBadRequest, "The pre-show can be up to an hour.")
		return
	}
	onList := map[int]crew.Film{}
	for _, e := range s.crewListRows(r.Context(), c.ID) {
		onList[e.TMDB] = e.Film
	}
	var films []crew.Film
	seen := map[int]bool{}
	for _, id := range body.Films {
		f, ok := onList[id]
		if !ok || seen[id] {
			writeError(w, http.StatusBadRequest, "Pick films from the shared watchlist.")
			return
		}
		seen[id] = true
		films = append(films, f)
	}
	uid := requestUserID(r)
	m := &crew.Vote{V: crew.Version, CID: c.CID, Op: crew.VoteOpen, VID: crew.NewID(), Who: s.crewPersonName(r.Context(), uid), Films: films,
		Closes: body.ClosesMs, At: body.AtMs, Pre: body.PreshowMin}
	if !c.mine() {
		// Kept here, so the winner is booked for this person with their
		// pre-show when the owner says it closed.
		raw, _ := json.Marshal([]crew.Choice{})
		_, _ = s.db.ExecContext(r.Context(), `INSERT INTO crew_votes (crew_id, vid, host_hid, host_name, host_user, closes_ms, at_ms, preshow_min, preshow_theme, state, choices, created_ms)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'open', ?, ?)`, c.ID, m.VID, c.MyHid, m.Who, uid, m.Closes, m.At, m.Pre, max(body.Theme, 0), string(raw), time.Now().UnixMilli())
	}
	if err := s.crewAct(r.Context(), c, crew.TypeVote, m, uid, max(body.Theme, 0), false); err != nil {
		if !c.mine() {
			_, _ = s.db.ExecContext(r.Context(), `DELETE FROM crew_votes WHERE vid = ?`, m.VID)
		}
		crewProblem(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "vid": m.VID})
}

// PUT /api/home/crews/{id}/votes/{vid} {"tmdbId"}: the asker's vote (0 takes
// it back). One vote each.
func (s *Server) handleCrewCastVote(w http.ResponseWriter, r *http.Request) {
	c, ok := s.crewMember(w, r)
	if !ok {
		return
	}
	var body struct {
		TMDBID int `json:"tmdbId"`
	}
	vid := r.PathValue("vid")
	if err := decodeJSON(r, &body); err != nil || !crew.ValidID(vid) || body.TMDBID < 0 {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	var state string
	var closes int64
	if s.db.QueryRowContext(r.Context(), `SELECT state, closes_ms FROM crew_votes WHERE vid = ? AND crew_id = ?`, vid, c.ID).Scan(&state, &closes) != nil {
		writeError(w, http.StatusNotFound, "That vote isn't here anymore.")
		return
	}
	if state != crew.VoteIsOpen || closes <= time.Now().UnixMilli() {
		writeError(w, http.StatusConflict, "That vote is closed.")
		return
	}
	m := &crew.Vote{V: crew.Version, CID: c.CID, Op: crew.VoteCast, VID: vid, Who: s.crewPersonName(r.Context(), requestUserID(r)), TMDB: body.TMDBID}
	if err := s.crewAct(r.Context(), c, crew.TypeVote, m, 0, 0, false); err != nil {
		crewProblem(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// POST /api/home/crews/{id}/clubs {"name", "weekday", "minute", "every",
// "mode", "preshowMin"}: start a film club: a night, how often, and who
// picks (whose turn it is, or the crew's vote). Whoever starts it is in.
func (s *Server) handleCrewAddClub(w http.ResponseWriter, r *http.Request) {
	c, ok := s.crewMember(w, r)
	if !ok {
		return
	}
	var body struct {
		Name       string `json:"name"`
		Weekday    int    `json:"weekday"`
		Minute     int    `json:"minute"`
		Every      int    `json:"every"`
		Mode       string `json:"mode"`
		PreshowMin int    `json:"preshowMin"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	d := &crew.ClubDef{Name: body.Name, Weekday: body.Weekday, Minute: body.Minute, Every: body.Every, Mode: body.Mode, Preshow: body.PreshowMin}
	if !d.Check() {
		writeError(w, http.StatusBadRequest, "Give the club a name, a night and a time, and say who picks.")
		return
	}
	m := &crew.Club{V: crew.Version, CID: c.CID, Op: crew.ClubAdd, Who: s.crewPersonName(r.Context(), requestUserID(r)), Def: d}
	if err := s.crewAct(r.Context(), c, crew.TypeClub, m, 0, 0, isAdminRequest(r)); err != nil {
		crewProblem(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// PUT /api/home/crews/{id}/clubs/{kid} {"op": "join"|"leave"|"pick", film}:
// join or leave a club, or (on your turn) pick the next night's film.
func (s *Server) handleCrewClub(w http.ResponseWriter, r *http.Request) {
	c, ok := s.crewMember(w, r)
	if !ok {
		return
	}
	var body struct {
		Op string `json:"op"`
		crewFilmBody
	}
	kid := r.PathValue("kid")
	if err := decodeJSON(r, &body); err != nil || !crew.ValidID(kid) {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	m := &crew.Club{V: crew.Version, CID: c.CID, KID: kid, Who: s.crewPersonName(r.Context(), requestUserID(r))}
	switch body.Op {
	case crew.ClubJoin, crew.ClubLeave:
		m.Op = body.Op
	case crew.ClubPick:
		f := body.film()
		m.Op, m.Film = crew.ClubPick, &f
	default:
		writeError(w, http.StatusBadRequest, "Say join, leave or pick.")
		return
	}
	if err := s.crewAct(r.Context(), c, crew.TypeClub, m, 0, 0, isAdminRequest(r)); err != nil {
		crewProblem(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// DELETE /api/home/crews/{id}/clubs/{kid}: end a club you started.
func (s *Server) handleCrewEndClub(w http.ResponseWriter, r *http.Request) {
	c, ok := s.crewMember(w, r)
	if !ok {
		return
	}
	kid := r.PathValue("kid")
	if !crew.ValidID(kid) {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	m := &crew.Club{V: crew.Version, CID: c.CID, Op: crew.ClubRemove, KID: kid, Who: s.crewPersonName(r.Context(), requestUserID(r))}
	if err := s.crewAct(r.Context(), c, crew.TypeClub, m, 0, 0, isAdminRequest(r)); err != nil {
		crewProblem(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// PUT /api/home/crews/sharing {"on"}: the asker's own "Share what I watch
// with my crews". Only ever the asker's: nobody turns it on for someone else.
func (s *Server) handleCrewSharing(w http.ResponseWriter, r *http.Request) {
	var body struct {
		On bool `json:"on"`
	}
	u := auth.UserFromContext(r.Context())
	if err := decodeJSON(r, &body); err != nil || u == nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	ctx := r.Context()
	var last int64
	_ = s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(id), 0) FROM home_diary WHERE user_id = ?`, u.ID).Scan(&last)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO crew_sharing (user_id, share, set_ms, last_diary) VALUES (?, ?, ?, ?)
		ON CONFLICT (user_id) DO UPDATE SET share = excluded.share, set_ms = excluded.set_ms, last_diary = excluded.last_diary`,
		u.ID, b2i(body.On), time.Now().UnixMilli(), last); err != nil {
		writeError(w, http.StatusInternalServerError, "That couldn't be saved.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sharing": body.On})
}
