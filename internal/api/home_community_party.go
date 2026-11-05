package api

// Usherr: Community, step 3. Watch parties. A Showtime shared to the
// community becomes a card in the channel Usherr posts in: the poster, the
// times (shown in each reader's own time zone), who's going, and I'm in /
// Can't make it buttons. A thread under it is the party's chat, and an event
// on the server's calendar lets people mark Interested and be reminded.
// Everyone watches in their own Usherr at the same time. When the Showtime
// moves, starts or is cancelled, the card, the thread and the event follow.
//
// The countdown is Discord's own: a <t:…:R> time ("in 2 hours") counts down
// by itself in every reader's Discord, so the card is only edited when
// something changes, well inside Discord's limits.

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/brand"
	"github.com/rdborg/mediarium/internal/community"
	"github.com/rdborg/mediarium/internal/community/discord"
)

type party struct {
	ShowtimeID int64
	Server     string
	Channel    string
	MessageID  string
	ThreadID   string
	EventID    string
	Host       string
	Told       string
	joined     bool // a friend's party joined by its code (chatPartyOf): not ours to change
}

type partyPerson struct {
	ID, Name, Answer string
}

// partyLock keeps one change to a party's card at a time.
var partyLock sync.Mutex

func (s *Server) partyOf(ctx context.Context, showtimeID int64) (party, bool) {
	var p party
	err := s.db.QueryRowContext(ctx, `SELECT showtime_id, server, channel, message_id, thread_id, event_id, host, told
		FROM community_parties WHERE showtime_id = ?`, showtimeID).
		Scan(&p.ShowtimeID, &p.Server, &p.Channel, &p.MessageID, &p.ThreadID, &p.EventID, &p.Host, &p.Told)
	return p, err == nil
}

func (s *Server) partyPeople(ctx context.Context, showtimeID int64) []partyPerson {
	out := []partyPerson{}
	rows, err := s.db.QueryContext(ctx, `SELECT external_id, name, answer FROM community_party_people WHERE showtime_id = ? ORDER BY at_ms`, showtimeID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var p partyPerson
		if rows.Scan(&p.ID, &p.Name, &p.Answer) == nil {
			out = append(out, p)
		}
	}
	return out
}

// partyHost is how the person who booked it is named on the card: their
// linked Discord name, or their name in Usherr.
func (s *Server) partyHost(userID int64) string {
	var name string
	if s.db.QueryRow(`SELECT name FROM community_links WHERE user_id = ? AND backend = 'discord'`, userID).Scan(&name) == nil && name != "" {
		return name
	}
	var first, user string
	_ = s.db.QueryRow(`SELECT first_name, username FROM users WHERE id = ?`, userID).Scan(&first, &user)
	if strings.TrimSpace(first) != "" {
		return strings.TrimSpace(first)
	}
	return user
}

func filmName(st showtime) string {
	if st.Year > 0 {
		return fmt.Sprintf("%s (%d)", st.Title, st.Year)
	}
	return st.Title
}

func runtimeText(min int) string {
	if min <= 0 {
		return ""
	}
	if min < 60 {
		return fmt.Sprintf("%d min", min)
	}
	return fmt.Sprintf("%d h %02d min", min/60, min%60)
}

// stamp is a Discord time: shown in each reader's own time zone ("t": 8:00
// PM, "F": Saturday, November 7, 2026 8:00 PM, "R": in 2 hours).
// "R" counts on by itself, so it suits the card (edited as things change)
// but not a message, which would read "a minute ago" later.
func stamp(ms int64, style string) string {
	return fmt.Sprintf("<t:%d:%s>", ms/1000, style)
}

func preshowAt(st showtime) int64 {
	return st.AtMs - int64(st.PreshowMin)*60_000
}

func showtimeEndMs(st showtime) int64 {
	return st.AtMs + int64(max(st.Runtime, 120))*60_000
}

// partyCard is the card for a party, as things stand. state "cancelled" is
// a Showtime called off.
func partyCard(st showtime, p party, people []partyPerson, state string) community.Post {
	var going, out []string
	for _, pp := range people {
		if pp.Answer == "in" {
			going = append(going, pp.Name)
		} else {
			out = append(out, pp.Name)
		}
	}
	card := &community.Card{
		Title:     filmName(st),
		URL:       fmt.Sprintf("https://www.themoviedb.org/movie/%d", st.TMDBID),
		Color:     0xF2C14E,
		Thumbnail: st.PosterURL,
		Footer:    brand.Name + " watch party",
	}
	switch state {
	case "cancelled":
		card.Color = 0x777777
		card.Description = fmt.Sprintf("**%s** called this watch party off.", p.Host)
	case "done":
		card.Color = 0x777777
		card.Description = fmt.Sprintf("**%s** hosted this watch party. Thanks for coming!", p.Host)
	default:
		card.Description = fmt.Sprintf("**%s** is hosting a watch party. Everyone watches in their own %s at the same time, and talks about it in the thread below.", p.Host, brand.Name)
	}
	if state != "cancelled" && state != "done" {
		if st.PreshowMin > 0 {
			card.Fields = append(card.Fields, community.Field{Name: "Pre-show", Value: stamp(preshowAt(st), "t"), Inline: true})
		}
		card.Fields = append(card.Fields, community.Field{Name: "Feature", Value: stamp(st.AtMs, "F"), Inline: true})
		switch state {
		case "playing":
			card.Fields = append(card.Fields, community.Field{Name: "Now", Value: "Playing", Inline: true})
		case "preshow":
			card.Fields = append(card.Fields, community.Field{Name: "Now", Value: "The pre-show is on", Inline: true})
		default:
			card.Fields = append(card.Fields, community.Field{Name: "Starts", Value: stamp(st.AtMs, "R"), Inline: true})
		}
	}
	if rt := runtimeText(st.Runtime); rt != "" {
		card.Fields = append(card.Fields, community.Field{Name: "Runtime", Value: rt, Inline: true})
	}
	if state != "cancelled" && state != "done" {
		// Step 4: pasted into another Usherr, it books the same moment there.
		card.Fields = append(card.Fields, community.Field{Name: "Showtime code", Value: "`" + codeFor(st) + "` In your own " + brand.Name + ": Showtime, Join a watch party."})
	}
	g := "Nobody yet. Press I'm in."
	if len(going) > 0 {
		g = strings.Join(going, ", ")
	}
	if state == "cancelled" || state == "done" {
		if len(going) == 0 {
			g = "Nobody"
		}
	}
	card.Fields = append(card.Fields, community.Field{Name: fmt.Sprintf("Going (%d)", len(going)), Value: g})
	if len(out) > 0 && state != "done" && state != "cancelled" {
		card.Fields = append(card.Fields, community.Field{Name: "Can't make it", Value: strings.Join(out, ", ")})
	}
	post := community.Post{Card: card}
	if state != "cancelled" && state != "done" {
		id := strconv.FormatInt(st.ID, 10)
		post.Buttons = []community.Button{
			{Label: "I'm in", ID: "party:" + id + ":in", Style: "success"},
			{Label: "Can't make it", ID: "party:" + id + ":out", Style: "secondary"},
		}
	}
	post.Buttons = append(post.Buttons, community.Button{Label: "About the film", URL: card.URL})
	return post
}

func partyEvent(st showtime, host string) community.Event {
	desc := fmt.Sprintf("%s is showing %s. Everyone watches in their own %s at the same time; the chat is in the watch party's thread.", host, filmName(st), brand.Name)
	if st.PreshowMin > 0 {
		desc += fmt.Sprintf(" The pre-show runs for up to %d minutes before the film.", st.PreshowMin)
	}
	return community.Event{
		Name:        st.Title + " watch party",
		Description: desc,
		Location:    "In your own " + brand.Name,
		Start:       time.UnixMilli(preshowAt(st)),
		End:         time.UnixMilli(showtimeEndMs(st)),
	}
}

// communityCanShare says whether a Showtime can be shared now: the bot is
// connected and there's a channel to post in.
func (s *Server) communityCanShare() (community.Backend, communitySettings, bool) {
	b := s.community()
	set := s.communitySettings()
	if b == nil || set.Channel == "" || b.Status().State != "connected" {
		return nil, set, false
	}
	return b, set, true
}

// shareShowtime posts a Showtime as a watch party: the card, a thread under
// it and an event on the calendar.
func (s *Server) shareShowtime(ctx context.Context, st showtime) error {
	b, set, ok := s.communityCanShare()
	if !ok {
		return errCommunityOff
	}
	partyLock.Lock()
	defer partyLock.Unlock()
	if _, shared := s.partyOf(ctx, st.ID); shared {
		return nil
	}
	p := party{ShowtimeID: st.ID, Server: set.Server, Channel: set.Channel, Host: s.partyHost(st.UserID)}
	ref, err := b.Post(ctx, set.Channel, partyCard(st, p, nil, st.State))
	if err != nil {
		return err
	}
	p.MessageID = ref.ID
	if id, err := b.StartThread(ctx, ref, st.Title+" watch party"); err == nil {
		p.ThreadID = id
	} else {
		slog.Warn("community: couldn't open the watch party's thread", "title", st.Title, "err", err)
	}
	if set.Server != "" && time.UnixMilli(preshowAt(st)).After(time.Now().Add(time.Minute)) {
		if id, err := b.CreateEvent(ctx, set.Server, partyEvent(st, p.Host)); err == nil {
			p.EventID = id
		} else {
			slog.Warn("community: couldn't put the watch party on the server's calendar", "title", st.Title, "err", err)
		}
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO community_parties (showtime_id, backend, server, channel, message_id, thread_id, event_id, host, created_ms)
		VALUES (?, 'discord', ?, ?, ?, ?, ?, ?, ?)`, st.ID, p.Server, p.Channel, p.MessageID, p.ThreadID, p.EventID, p.Host, time.Now().UnixMilli())
	return err
}

var errCommunityOff = fmt.Errorf("community isn't connected")

// showtimeByID reads a showtime.
func (s *Server) showtimeByID(ctx context.Context, id int64) (showtime, bool) {
	st, err := scanShowtime(s.db.QueryRowContext(ctx, `SELECT `+showtimeCols+` FROM showtimes WHERE id = ?`, id))
	return st, err == nil
}

// partyRefresh edits a party's card to match the Showtime and who's going.
func (s *Server) partyRefresh(ctx context.Context, showtimeID int64) {
	b := s.community()
	if b == nil {
		return
	}
	partyLock.Lock()
	defer partyLock.Unlock()
	p, ok := s.partyOf(ctx, showtimeID)
	st, ok2 := s.showtimeByID(ctx, showtimeID)
	if !ok || !ok2 {
		return
	}
	if err := b.Edit(ctx, community.Ref{Channel: p.Channel, ID: p.MessageID}, partyCard(st, p, s.partyPeople(ctx, showtimeID), st.State)); err != nil {
		slog.Warn("community: couldn't update the watch party's card", "title", st.Title, "err", err)
	}
}

// partySay posts in the party's thread (or under the card, without one).
func (s *Server) partySay(ctx context.Context, p party, text string) {
	b := s.community()
	if b == nil {
		return
	}
	where := p.ThreadID
	if where == "" {
		where = p.Channel
	}
	if _, err := b.Post(ctx, where, community.Post{Text: text}); err != nil {
		slog.Warn("community: couldn't post in the watch party's thread", "err", err)
	}
}

// partyStateChanged follows a Showtime as it runs (showtimeJob): an alert in
// the thread when the pre-show and the film start, and the card says so.
// missed: the server was off at the time, so nothing is announced late.
func (s *Server) partyStateChanged(showtimeID int64, state string, missed bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	p, ok := s.partyOf(ctx, showtimeID)
	st, ok2 := s.showtimeByID(ctx, showtimeID)
	if !ok || !ok2 || p.Told == state {
		return
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE community_parties SET told = ? WHERE showtime_id = ?`, state, showtimeID)
	var stubs []string
	if state == "playing" {
		stubs = s.giveStubs(ctx, st, p) // Usherr: step 7 (home_community_stubs.go)
	}
	if !missed {
		switch state {
		case "preshow":
			s.partySay(ctx, p, fmt.Sprintf("🍿 The pre-show is starting. **%s** begins at %s.", st.Title, stamp(st.AtMs, "t")))
		case "playing":
			say := fmt.Sprintf("🎬 Feature presentation: **%s** is starting now. Enjoy the film!", st.Title)
			if len(stubs) > 0 {
				say += "\n🎟️ Ticket stubs for " + strings.Join(stubs, ", ") + ". (/stubs shows yours.)"
			}
			s.partySay(ctx, p, say)
		}
	}
	s.partyRefresh(ctx, showtimeID)
}

// partyMoved follows a Showtime moved to another time.
func (s *Server) partyMoved(showtimeID int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	p, ok := s.partyOf(ctx, showtimeID)
	st, ok2 := s.showtimeByID(ctx, showtimeID)
	b := s.community()
	if !ok || !ok2 || b == nil {
		return
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE community_parties SET told = '' WHERE showtime_id = ?`, showtimeID)
	if p.EventID != "" {
		if err := b.EditEvent(ctx, p.Server, p.EventID, partyEvent(st, p.Host)); err != nil {
			slog.Warn("community: couldn't move the watch party's event", "title", st.Title, "err", err)
		}
	}
	s.partySay(ctx, p, fmt.Sprintf("🕒 **%s** moved: the film now starts %s.", st.Title, stamp(st.AtMs, "F")))
	s.partyRefresh(ctx, showtimeID)
}

// partyCancelled follows a Showtime called off: the card says so, the event
// goes, and the thread is told. Called before the Showtime is deleted. One
// stopped while its film plays has ended rather than been called off.
func (s *Server) partyCancelled(ctx context.Context, st showtime) {
	b := s.community()
	p, ok := s.partyOf(ctx, st.ID)
	if !ok || b == nil {
		return
	}
	people := s.partyPeople(ctx, st.ID)
	state, said := "cancelled", fmt.Sprintf("❌ The **%s** watch party is off.", st.Title)
	wrap := false
	if st.State == "playing" {
		// Ended during the film: stubs get stamped now (unless the credits came).
		state, said, wrap = "done", "", p.Told != "credits"
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		partyLock.Lock()
		defer partyLock.Unlock()
		if err := b.Edit(ctx, community.Ref{Channel: p.Channel, ID: p.MessageID}, partyCard(st, p, people, state)); err != nil {
			slog.Warn("community: couldn't mark the watch party's card cancelled", "title", st.Title, "err", err)
		}
		if p.EventID != "" {
			_ = b.DeleteEvent(ctx, p.Server, p.EventID)
		}
		if wrap {
			s.partyWrap(ctx, st, p, true)
		} else if said != "" {
			s.partySay(ctx, p, said)
		}
	}()
}

// partyButton answers I'm in and Can't make it.
func (s *Server) partyButton(in community.Interaction) string {
	parts := strings.Split(in.Name, ":")
	if len(parts) != 3 || (parts[2] != "in" && parts[2] != "out") {
		return "That button isn't known."
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return "That button isn't known."
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	st, ok := s.showtimeByID(ctx, id)
	p, ok2 := s.partyOf(ctx, id)
	if !ok || !ok2 {
		return "This watch party is over or was called off."
	}
	if st.State == "done" {
		return "This watch party is over."
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO community_party_people (showtime_id, external_id, name, answer, at_ms) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (showtime_id, external_id) DO UPDATE SET name = excluded.name, answer = excluded.answer, at_ms = excluded.at_ms`,
		id, in.User.ID, trimRunes(in.User.Name, 100), parts[2], time.Now().UnixMilli()); err != nil {
		return "Something went wrong. Press it again."
	}
	b := s.community()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if parts[2] == "in" && p.ThreadID != "" && b != nil {
			_ = b.AddToThread(ctx, p.ThreadID, in.User.ID)
		}
		if parts[2] == "in" && st.State == "playing" {
			s.lateStub(ctx, st, p, in.User) // Usherr: step 7, in after it started
		}
		s.partyRefresh(ctx, id)
	}()
	if parts[2] == "in" && st.State == "playing" {
		return fmt.Sprintf("You're in! **%s** has already started: start it in your own %s to catch up. 🎟️ You get a ticket stub (/stubs).", st.Title, brand.Name)
	}
	if parts[2] == "out" {
		return fmt.Sprintf("Got it: you can't make **%s** this time. Changed your mind? Press I'm in.", st.Title)
	}
	when := "The film starts " + stamp(st.AtMs, "t")
	if st.PreshowMin > 0 {
		when = fmt.Sprintf("The pre-show starts %s, the film %s", stamp(preshowAt(st), "t"), stamp(st.AtMs, "t"))
	}
	return fmt.Sprintf("You're in for **%s**! %s. To watch along in your own %s, open Showtime there and paste this code under Join a watch party: `%s`. The chat is in the thread under the card.", st.Title, when, brand.Name, codeFor(st))
}

type partyInfo struct {
	Going    []string `json:"going"`
	CantMake int      `json:"cantMake"`
	// Usherr: a friend's party joined by its code. Host: who hosts it, once
	// found in their server; Waiting: not found there yet (this Usherr's bot
	// isn't in their server, or the party isn't shared).
	Joined  bool   `json:"joined,omitempty"`
	Host    string `json:"host,omitempty"`
	Waiting bool   `json:"waiting,omitempty"`
	// What a joined party still needs here: Bot "off" (this Usherr has no
	// bot connected yet) or "on"; Invite: the bot's invite link, to send the
	// host; NeedLink: whoever booked it hasn't linked their Discord account.
	Bot      string `json:"bot,omitempty"`
	Invite   string `json:"invite,omitempty"`
	NeedLink bool   `json:"needLink,omitempty"`
}

// withParties adds each shared Showtime's party: who's going.
func (s *Server) withParties(ctx context.Context, list []showtime) map[int64]partyInfo {
	out := map[int64]partyInfo{}
	for _, st := range list {
		if _, ok := s.partyOf(ctx, st.ID); !ok {
			if j, ok := s.joinedOf(ctx, st.ID); ok {
				out[st.ID] = partyInfo{Going: append([]string{}, j.Going...), Joined: true, Host: j.Host, NeedLink: s.communityLinkOf(st.UserID) == nil}
			} else if s.isJoined(ctx, st.ID) {
				info := partyInfo{Going: []string{}, Joined: true, Waiting: true, Bot: "off", NeedLink: s.communityLinkOf(st.UserID) == nil}
				if b := s.community(); b != nil && b.Status().State == "connected" {
					info.Bot, info.Invite = "on", discord.InviteURL(b.Status().AppID)
				}
				out[st.ID] = info
			}
			continue
		}
		info := partyInfo{Going: []string{}}
		for _, p := range s.partyPeople(ctx, st.ID) {
			if p.Answer == "in" {
				info.Going = append(info.Going, p.Name)
			} else {
				info.CantMake++
			}
		}
		out[st.ID] = info
	}
	return out
}

// POST /api/home/showtimes/{id}/share (member): share a booked Showtime as a
// watch party. Whoever booked it, or an administrator.
func (s *Server) handleShareShowtime(w http.ResponseWriter, r *http.Request) {
	st, code, msg := s.showtimeFor(r)
	if code != 0 {
		writeError(w, code, msg)
		return
	}
	if st.State == "done" || st.State == "playing" {
		writeError(w, http.StatusBadRequest, "That showtime has already started.")
		return
	}
	if s.isJoined(r.Context(), st.ID) {
		writeError(w, http.StatusBadRequest, "That's a friend's watch party, joined with its code: it's shared in their server already.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := s.shareShowtime(ctx, st); err != nil {
		if err == errCommunityOff {
			writeError(w, http.StatusConflict, "Community isn't connected, or has no channel to post in (Settings, Movie night, Community).")
			return
		}
		writeError(w, http.StatusBadGateway, communityProblem(err, "in the Community channel"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
