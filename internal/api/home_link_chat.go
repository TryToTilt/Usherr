package api

// Usherr: linked screenings, step 4. Chat and reactions over the link, in
// the party chat a linked screening already shows (the Showtime page, the
// phone page /showtime/chat/{id}, over the film), so a linked party needs no
// Discord. A showtime that's a linked screening chats over the link; any
// other watch party keeps its Discord thread (home_community_chat.go).
//
//   - Chat: plain text, at most 500 characters, maybe marked a spoiler
//     (blurred until tapped), pinned to the second of the film it was said
//     at. The last 500 are kept per screening on every Usherr at it.
//   - Reactions: HA, EEK, WOW, NOPE or CLAP, pinned to the second of the
//     film. Each floats up over the picture in every house at once, and
//     again for anyone who plays that part later; bars above the progress
//     bar show where the crew reacted. Kept per screening and per film
//     (TMDB id), so a film's most-reacted moment can be found
//     (screeningTopMoment, filmTopMoment; GET .../reactions).
//
// The host's Usherr is the hub (internal/link/screening/chat.go): a guest's
// people's messages go to the host, which keeps them and passes them on to
// everyone at the screening, sealed with the screening key. Each person is
// held to a rate here; each guest Usherr, for its household, at the host.
// What's said is never logged.

import (
	"context"
	"database/sql"
	"log/slog"
	"math"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/community"
	"github.com/rdborg/mediarium/internal/link"
	"github.com/rdborg/mediarium/internal/link/screening"
)

const (
	linkChatKeep  = 500  // messages kept per screening
	linkReactKeep = 5000 // reactions kept per screening
	reactBucket   = 30   // seconds: one heat bar, one moment
	reactSlack    = 15.0 // seconds a sender's position may be off the room's while it plays
	reactFilmMost = 3000 // reactions a film's replay carries at most
	linkChatSends = 15 * time.Second
)

// How fast a person here may chat and react: a burst, then one a second.
var (
	partySayEvery   = time.Second
	partySayBurst   = 6
	partyReactEvery = time.Second
	partyReactBurst = 5
)

// linkChatOut is a linked screening's message as the chat shows it (the
// shape of a Discord one, plus spoiler, position and house).
type linkChatOut struct {
	ID      string         `json:"id"`
	User    community.User `json:"user"`
	Text    string         `json:"text"`
	At      time.Time      `json:"at"`
	Spoiler bool           `json:"spoiler,omitempty"`
	Pos     float64        `json:"pos"`             // seconds into the film it was said at
	House   string         `json:"house,omitempty"` // the Usherr it was said at
}

// reactionOut is one reaction. A film's replay leaves out who sent it.
type reactionOut struct {
	ID    string  `json:"id"`
	Name  string  `json:"name,omitempty"`
	House string  `json:"house,omitempty"`
	Word  string  `json:"word"`
	Pos   float64 `json:"pos"`
	AtMs  int64   `json:"atMs,omitempty"`
}

// heatBar is one stretch of the film (reactBucket seconds from Pos) and how
// many reactions it got.
type heatBar struct {
	Pos   float64 `json:"pos"`
	Count int     `json:"count"`
	Top   string  `json:"top"` // the word sent most there
}

// reactMoment is the stretch of a film the crew reacted to most: from From
// to To seconds into the film (reactBucket long), Peak the second the
// reactions in it centre on, Total of them, Words by word, Top the word sent
// most. Pre-show reactions don't count. Ties go to the earlier stretch.
type reactMoment struct {
	From  float64        `json:"from"`
	To    float64        `json:"to"`
	Peak  float64        `json:"peak"`
	Total int            `json:"total"`
	Words map[string]int `json:"words"`
	Top   string         `json:"top"`
}

func chatOutOf(m screening.ChatMsg) linkChatOut {
	return linkChatOut{ID: m.ID, User: community.User{Name: m.Name}, Text: m.Text, At: time.UnixMilli(m.At), Spoiler: m.Spoiler, Pos: m.Pos, House: m.House}
}

func reactionOutOf(r screening.ReactMoment) reactionOut {
	return reactionOut{ID: r.ID, Name: r.Name, House: r.House, Word: r.Word, Pos: r.Pos, AtMs: r.At}
}

// ---- kept ----

// keepLinkChat keeps a message (once: false when it was here already) and
// lets the oldest go past linkChatKeep.
func (s *Server) keepLinkChat(ctx context.Context, m screening.ChatMsg, userID int64) bool {
	res, err := s.db.ExecContext(ctx, `INSERT INTO link_chat (sid, id, user_id, name, house, text, spoiler, pos, at_ms) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (sid, id) DO NOTHING`, m.SID, m.ID, userID, m.Name, m.House, m.Text, boolInt(m.Spoiler), m.Pos, m.At)
	if err != nil {
		slog.Warn("link: a chat message couldn't be kept", "err", err)
		return false
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false
	}
	_, _ = s.db.ExecContext(ctx, `DELETE FROM link_chat WHERE sid = ? AND at_ms < (SELECT at_ms FROM link_chat WHERE sid = ? ORDER BY at_ms DESC LIMIT 1 OFFSET ?)`, m.SID, m.SID, linkChatKeep-1)
	return true
}

// keepLinkReact keeps a reaction (once), with its film.
func (s *Server) keepLinkReact(ctx context.Context, r screening.ReactMoment, userID int64) bool {
	res, err := s.db.ExecContext(ctx, `INSERT INTO link_reactions (sid, id, tmdb_id, user_id, name, house, word, pos, at_ms) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (sid, id) DO NOTHING`, r.SID, r.ID, s.tmdbOfSID(ctx, r.SID), userID, r.Name, r.House, r.Word, r.Pos, r.At)
	if err != nil {
		slog.Warn("link: a reaction couldn't be kept", "err", err)
		return false
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false
	}
	_, _ = s.db.ExecContext(ctx, `DELETE FROM link_reactions WHERE sid = ? AND at_ms < (SELECT at_ms FROM link_reactions WHERE sid = ? ORDER BY at_ms DESC LIMIT 1 OFFSET ?)`, r.SID, r.SID, linkReactKeep-1)
	return true
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// linkChatHistory is a screening's chat so far, oldest first.
func (s *Server) linkChatHistory(ctx context.Context, sid string) []linkChatOut {
	out := []linkChatOut{}
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, house, text, spoiler, pos, at_ms FROM link_chat WHERE sid = ? ORDER BY at_ms, id`, sid)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var m screening.ChatMsg
		var spoiler int
		if rows.Scan(&m.ID, &m.Name, &m.House, &m.Text, &spoiler, &m.Pos, &m.At) == nil {
			m.Spoiler = spoiler == 1
			out = append(out, chatOutOf(m))
		}
	}
	return out
}

// screeningReactions is a screening's reactions, by the second of the film.
func (s *Server) screeningReactions(ctx context.Context, sid string) []reactionOut {
	return s.readReactions(ctx, `SELECT id, name, house, word, pos, at_ms FROM link_reactions WHERE sid = ? ORDER BY pos, at_ms`, sid)
}

func (s *Server) readReactions(ctx context.Context, q string, args ...any) []reactionOut {
	out := []reactionOut{}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var r reactionOut
		if rows.Scan(&r.ID, &r.Name, &r.House, &r.Word, &r.Pos, &r.AtMs) == nil {
			out = append(out, r)
		}
	}
	return out
}

// filmReactions is every reaction kept here to a film (TMDB id), from all
// its screenings, without who sent them: at most reactFilmMost, spread
// evenly over them.
func (s *Server) filmReactions(ctx context.Context, tmdb int) []reactionOut {
	list := s.readReactions(ctx, `SELECT id, '', '', word, pos, 0 FROM link_reactions WHERE tmdb_id = ? ORDER BY pos, at_ms`, tmdb)
	if len(list) <= reactFilmMost {
		return list
	}
	out := make([]reactionOut, 0, reactFilmMost)
	for i := range reactFilmMost {
		out = append(out, list[i*len(list)/reactFilmMost])
	}
	return out
}

// reactHeat makes the heat bars (stretches with a reaction, in order) and
// the most-reacted moment of a list of reactions.
func reactHeat(list []reactionOut) ([]heatBar, *reactMoment) {
	type stretch struct {
		words map[string]int
		pos   []float64
	}
	by := map[int]*stretch{}
	for _, r := range list {
		if r.Pos < 0 || !screening.ValidWord(r.Word) {
			continue
		}
		b := int(r.Pos) / reactBucket
		if by[b] == nil {
			by[b] = &stretch{words: map[string]int{}}
		}
		by[b].words[r.Word]++
		by[b].pos = append(by[b].pos, r.Pos)
	}
	keys := make([]int, 0, len(by))
	for b := range by {
		keys = append(keys, b)
	}
	sort.Ints(keys)
	bars := make([]heatBar, 0, len(keys))
	var best *reactMoment
	for _, b := range keys {
		st := by[b]
		top, most := "", 0
		for _, w := range screening.Words { // in the words' order: ties go to the first
			if st.words[w] > most {
				top, most = w, st.words[w]
			}
		}
		total := len(st.pos)
		bars = append(bars, heatBar{Pos: float64(b * reactBucket), Count: total, Top: top})
		if best == nil || total > best.Total {
			sort.Float64s(st.pos)
			best = &reactMoment{From: float64(b * reactBucket), To: float64((b + 1) * reactBucket), Peak: math.Round(st.pos[len(st.pos)/2]*10) / 10, Total: total, Words: st.words, Top: top}
		}
	}
	return bars, best
}

// screeningTopMoment is the most-reacted moment of a screening (by its
// id), for the crew's verdict and Wrapped; false when nobody reacted
// during the film.
func (s *Server) screeningTopMoment(ctx context.Context, sid string) (reactMoment, bool) {
	_, top := reactHeat(s.screeningReactions(ctx, sid))
	if top == nil {
		return reactMoment{}, false
	}
	return *top, true
}

// showtimeTopMoment is the same for a showtime here that's a linked
// screening (hosted here, or a guest's booking).
func (s *Server) showtimeTopMoment(ctx context.Context, showtimeID int64) (reactMoment, bool) {
	r, ok := s.roomOf(ctx, showtimeID)
	if !ok {
		return reactMoment{}, false
	}
	return s.screeningTopMoment(ctx, r.SID)
}

// filmTopMoment is a film's (TMDB id) most-reacted moment over all its
// screenings kept here.
func (s *Server) filmTopMoment(ctx context.Context, tmdb int) (reactMoment, bool) {
	_, top := reactHeat(s.readReactions(ctx, `SELECT id, '', '', word, pos, 0 FROM link_reactions WHERE tmdb_id = ? AND pos >= 0`, tmdb))
	if top == nil {
		return reactMoment{}, false
	}
	return *top, true
}

// screeningReactTotals is how many of each word a screening got (Wrapped's
// "most EEKs in one film").
func (s *Server) screeningReactTotals(ctx context.Context, sid string) map[string]int {
	out := map[string]int{}
	rows, err := s.db.QueryContext(ctx, `SELECT word, COUNT(*) FROM link_reactions WHERE sid = ? GROUP BY word`, sid)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var w string
		var n int
		if rows.Scan(&w, &n) == nil {
			out[w] = n
		}
	}
	return out
}

// ---- where a screening is ----

// showtimeOfSID is the showtime here a screening is (hosted here, or a
// guest's booking); false when there's none (not booked here).
func (s *Server) showtimeOfSID(ctx context.Context, sid string) (int64, bool) {
	var id sql.NullInt64
	if s.db.QueryRowContext(ctx, `SELECT showtime_id FROM link_screenings_out WHERE sid = ?`, sid).Scan(&id) == nil && id.Valid {
		return id.Int64, true
	}
	if s.db.QueryRowContext(ctx, `SELECT showtime_id FROM link_screenings_in WHERE sid = ?`, sid).Scan(&id) == nil && id.Valid {
		return id.Int64, true
	}
	return 0, false
}

// tmdbOfSID is a screening's film.
func (s *Server) tmdbOfSID(ctx context.Context, sid string) int {
	var tmdb int
	if s.db.QueryRowContext(ctx, `SELECT st.tmdb_id FROM link_screenings_out o JOIN showtimes st ON st.id = o.showtime_id WHERE o.sid = ?`, sid).Scan(&tmdb) == nil {
		return tmdb
	}
	_ = s.db.QueryRowContext(ctx, `SELECT tmdb_id FROM link_screenings_in WHERE sid = ?`, sid).Scan(&tmdb)
	return tmdb
}

// pinPos is the second of the film something said or sent is pinned to:
// where the sender said it was, unless the film is on and that's more than
// reactSlack off where the room is (then the room's). Someone playing it
// back after the screening pins to where they are.
func pinPos(r screeningRoom, said *float64, now time.Time) float64 {
	cur := r.now(now.UnixMilli())
	pos := cur.Position
	if said != nil && screening.ValidPos(*said) {
		switch cur.State {
		case screening.StateFilm, screening.StatePaused, screening.StatePreshow:
			if math.Abs(*said-cur.Position) <= reactSlack {
				pos = *said
			}
		default:
			pos = *said
		}
	}
	lo, hi := -float64(screening.MaxPreshow*60+60), float64(screening.MaxRuntime*60+3600)
	return math.Round(math.Max(lo, math.Min(hi, pos))*10) / 10
}

// linkFanout sends a message to everyone invited to a screening hosted
// here but except (the guest Usherr it came from, which has it).
func (s *Server) linkFanout(r screeningRoom, typ string, body any, except string) {
	n := s.linkNode()
	if n == nil || r.Role != "host" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		var to []string
		for _, g := range s.screeningGuests(ctx, r.ShowtimeID) {
			if g.Their != except {
				to = append(to, g.Their)
			}
		}
		if len(to) == 0 {
			return
		}
		if err := n.SendGroup(ctx, r.Key, to, typ, body); err != nil {
			slog.Debug("link: a party message couldn't be passed on", "type", typ, "err", err)
		}
	}()
}

// ---- what the messages do (screening.Chat) ----

// ChatFrom: someone at an invited Usherr said something. Kept, shown here,
// passed on to everyone else at the screening.
func (b linkBook) ChatFrom(ctx context.Context, guest link.Peer, cs screening.ChatSend) error {
	s := b.s
	r, ok := s.hostRoomBySID(ctx, cs.SID)
	if !ok {
		return screening.ErrNotAllowed
	}
	now := time.Now()
	m := screening.ChatMsg{V: screening.Version, SID: cs.SID, ID: cs.ID, Name: cs.Name, House: link.CleanName(guest.Name), Text: cs.Text, Spoiler: cs.Spoiler,
		Pos: pinPos(r, &cs.Pos, now), At: now.UnixMilli()}
	if !s.keepLinkChat(ctx, m, 0) {
		return nil
	}
	s.chat.send(r.ShowtimeID, chatEvent{Kind: "new", Message: chatOutOf(m)})
	s.linkFanout(r, screening.TypeChatMsg, m, guest.ID)
	return nil
}

// ReactFrom: someone at an invited Usherr reacted.
func (b linkBook) ReactFrom(ctx context.Context, guest link.Peer, rs screening.ReactSend) error {
	s := b.s
	r, ok := s.hostRoomBySID(ctx, rs.SID)
	if !ok {
		return screening.ErrNotAllowed
	}
	now := time.Now()
	m := screening.ReactMoment{V: screening.Version, SID: rs.SID, ID: rs.ID, Name: rs.Name, House: link.CleanName(guest.Name), Word: rs.Word, Pos: pinPos(r, &rs.Pos, now), At: now.UnixMilli()}
	if !s.keepLinkReact(ctx, m, 0) {
		return nil
	}
	s.chat.send(r.ShowtimeID, chatEvent{Kind: "react", Message: reactionOutOf(m)})
	s.linkFanout(r, screening.TypeReactMoment, m, guest.ID)
	return nil
}

// ChatIn: the host passes on something said at the screening.
func (b linkBook) ChatIn(ctx context.Context, _ link.Peer, m screening.ChatMsg) error {
	s := b.s
	if !s.keepLinkChat(ctx, m, 0) {
		return nil
	}
	if id, ok := s.showtimeOfSID(ctx, m.SID); ok {
		s.chat.send(id, chatEvent{Kind: "new", Message: chatOutOf(m)})
	}
	return nil
}

// ReactIn: the host passes on a reaction.
func (b linkBook) ReactIn(ctx context.Context, _ link.Peer, r screening.ReactMoment) error {
	s := b.s
	if !s.keepLinkReact(ctx, r, 0) {
		return nil
	}
	if id, ok := s.showtimeOfSID(ctx, r.SID); ok {
		s.chat.send(id, chatEvent{Kind: "react", Message: reactionOutOf(r)})
	}
	return nil
}

// ---- the pages ----

// linkChatRoom is the linked screening a chat request's showtime is.
func (s *Server) linkChatRoom(r *http.Request) (screeningRoom, bool) {
	id, ok := pathID(r)
	if !ok {
		return screeningRoom{}, false
	}
	return s.roomOf(r.Context(), id)
}

// handleLinkChat: GET /api/home/showtimes/{id}/chat for a linked screening:
// what's been said, in the Discord chat's shape.
func (s *Server) handleLinkChat(w http.ResponseWriter, r *http.Request, room screeningRoom) {
	ctx := r.Context()
	st, _ := s.showtimeByID(ctx, room.ShowtimeID)
	connected := false
	if n := s.linkNode(); n != nil {
		connected = n.Status().State == "connected"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"showtime":  st,
		"messages":  s.linkChatHistory(ctx, room.SID),
		"linked":    true,
		"connected": connected,
		"canPost":   s.linkNode() != nil,
		"discord":   "",
		"warnings":  []string{},
		"nowMs":     time.Now().UnixMilli(),
		"link":      true, // a linked screening: chat over the link, reactions, spoilers
		"role":      room.Role,
		"words":     screening.Words,
		"maxText":   screening.MaxChatText,
	})
}

func (s *Server) partyPerson(r *http.Request) (*auth.User, string, bool) {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		return nil, "", false
	}
	return u, link.CleanName(s.partyHost(u.ID)), true
}

// handleLinkChatSend: POST /api/home/showtimes/{id}/chat for a linked
// screening {"text", "spoiler"?, "pos"?}: say something to everyone at it.
func (s *Server) handleLinkChatSend(w http.ResponseWriter, r *http.Request, room screeningRoom) {
	u, name, ok := s.partyPerson(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "Sign in first.")
		return
	}
	var body struct {
		Text    string   `json:"text"`
		Spoiler bool     `json:"spoiler"`
		Pos     *float64 `json:"pos"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	text := screening.CleanChat(body.Text)
	if len([]rune(body.Text)) > screening.MaxChatText+20 { // a little room for the spaces it trims
		writeError(w, http.StatusBadRequest, "That's a bit long. Keep it under 500 characters.")
		return
	}
	if text == "" {
		writeError(w, http.StatusBadRequest, "Type a message first.")
		return
	}
	n := s.linkNode()
	if n == nil {
		writeError(w, http.StatusConflict, linkOffText)
		return
	}
	s.chat.init()
	if !s.chat.linkSays.Allow(strconv.FormatInt(u.ID, 10), time.Now()) {
		writeError(w, http.StatusTooManyRequests, "That's a lot of messages at once. Wait a moment.")
		return
	}
	now := time.Now()
	m := screening.ChatMsg{V: screening.Version, SID: room.SID, ID: link.NewMessageID(), Name: name, House: link.CleanName(s.linkName()), Text: text, Spoiler: body.Spoiler,
		Pos: pinPos(room, body.Pos, now), At: now.UnixMilli()}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), linkChatSends)
	defer cancel()
	if room.Role == "guest" {
		if err := n.Send(ctx, room.HostID, screening.TypeChatSend, screening.ChatSend{V: screening.Version, SID: m.SID, ID: m.ID, Name: m.Name, Text: m.Text, Spoiler: m.Spoiler, Pos: m.Pos}); err != nil {
			writeError(w, http.StatusBadGateway, "The host couldn't be reached right now. Try again in a moment.")
			return
		}
	}
	if s.keepLinkChat(ctx, m, u.ID) {
		s.chat.send(room.ShowtimeID, chatEvent{Kind: "new", Message: chatOutOf(m)})
	}
	s.linkFanout(room, screening.TypeChatMsg, m, "")
	writeJSON(w, http.StatusCreated, map[string]any{"message": chatOutOf(m)})
}

// POST /api/home/showtimes/{id}/react {"word", "pos"?} (member): a reaction
// (HA, EEK, WOW, NOPE or CLAP) at a linked screening, pinned to the second
// of the film; it floats up over the picture in every house at once.
func (s *Server) handleLinkReact(w http.ResponseWriter, r *http.Request) {
	room, ok := s.linkChatRoom(r)
	if !ok {
		writeError(w, http.StatusNotFound, "That showtime isn't a linked screening.")
		return
	}
	u, name, ok := s.partyPerson(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "Sign in first.")
		return
	}
	var body struct {
		Word string   `json:"word"`
		Pos  *float64 `json:"pos"`
	}
	if err := decodeJSON(r, &body); err != nil || !screening.ValidWord(body.Word) {
		writeError(w, http.StatusBadRequest, "Pick a reaction: HA, EEK, WOW, NOPE or CLAP.")
		return
	}
	n := s.linkNode()
	if n == nil {
		writeError(w, http.StatusConflict, linkOffText)
		return
	}
	s.chat.init()
	if !s.chat.linkReacts.Allow(strconv.FormatInt(u.ID, 10), time.Now()) {
		writeError(w, http.StatusTooManyRequests, "Easy there! One reaction a second.")
		return
	}
	now := time.Now()
	m := screening.ReactMoment{V: screening.Version, SID: room.SID, ID: link.NewMessageID(), Name: name, House: link.CleanName(s.linkName()), Word: body.Word, Pos: pinPos(room, body.Pos, now), At: now.UnixMilli()}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), linkChatSends)
	defer cancel()
	if room.Role == "guest" {
		if err := n.Send(ctx, room.HostID, screening.TypeReactSend, screening.ReactSend{V: screening.Version, SID: m.SID, ID: m.ID, Name: m.Name, Word: m.Word, Pos: m.Pos}); err != nil {
			writeError(w, http.StatusBadGateway, "The host couldn't be reached right now. Try again in a moment.")
			return
		}
	}
	if s.keepLinkReact(ctx, m, u.ID) {
		s.chat.send(room.ShowtimeID, chatEvent{Kind: "react", Message: reactionOutOf(m)})
	}
	s.linkFanout(room, screening.TypeReactMoment, m, "")
	writeJSON(w, http.StatusCreated, map[string]any{"reaction": reactionOutOf(m)})
}

// reactionsAnswer is the reactions of a screening or a film: each one (by
// the second of the film), the heat bars and the most-reacted moment.
type reactionsAnswer struct {
	Bucket    int           `json:"bucket"` // seconds each heat bar covers
	Runtime   int           `json:"runtime,omitempty"`
	Words     []string      `json:"words"`
	Heat      []heatBar     `json:"heat"`
	Top       *reactMoment  `json:"top"` // null when nobody reacted during the film
	Reactions []reactionOut `json:"reactions"`
}

// GET /api/home/showtimes/{id}/reactions (member): a linked screening's
// reactions, its heat bars and its most-reacted moment.
func (s *Server) handleLinkReactions(w http.ResponseWriter, r *http.Request) {
	room, ok := s.linkChatRoom(r)
	if !ok {
		writeError(w, http.StatusNotFound, "That showtime isn't a linked screening.")
		return
	}
	list := s.screeningReactions(r.Context(), room.SID)
	heat, top := reactHeat(list)
	writeJSON(w, http.StatusOK, reactionsAnswer{Bucket: reactBucket, Runtime: room.Runtime * 60, Words: screening.Words, Heat: heat, Top: top, Reactions: list})
}

// GET /api/home/reactions/film?tmdb=N or ?movie=<library id> (member): a
// film's reactions from all its linked screenings kept here, without who
// sent them, so someone watching it later sees them float at the same
// second; with its heat bars and most-reacted moment.
func (s *Server) handleFilmReactions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	tmdb, _ := strconv.Atoi(q.Get("tmdb"))
	if id, err := strconv.ParseInt(q.Get("movie"), 10, 64); err == nil && id > 0 && tmdb <= 0 && s.MovieRepo != nil {
		if m, err := s.MovieRepo.Get(id); err == nil {
			tmdb = m.TMDBID
		}
	}
	if tmdb <= 0 {
		writeError(w, http.StatusBadRequest, "Say which film.")
		return
	}
	list := s.filmReactions(r.Context(), tmdb)
	heat, top := reactHeat(list)
	writeJSON(w, http.StatusOK, reactionsAnswer{Bucket: reactBucket, Words: screening.Words, Heat: heat, Top: top, Reactions: list})
}
