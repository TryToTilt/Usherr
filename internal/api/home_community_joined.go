package api

// Usherr: Community. A friend's watch party, joined with its showtime code,
// found in the friend's Discord server. When this Usherr's own bot is in that
// server too (the host adds it with its invite link), it finds the party's
// card and thread by the code. Then the party is linked here:
//   - its chat shows on the Showtime page and the phone's chat page, and what
//     is typed here goes into the host's thread under your linked Discord name;
//   - the ticket stub here says who hosted and who else was there, read from
//     the card (and read again whenever the host's bot changes it);
//   - the host's bot says "@you stayed to the end" in the thread when you press
//     its stamp button, and this Usherr stamps your stub here when it reads it.
// Nothing is posted as the host's party from here: its card, thread, event
// and alerts stay the host's (partyOf is only ever the host's own parties).

import (
	"context"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/community"
)

// joinedLink is where a joined party lives in the host's server.
type joinedLink struct {
	Server, Channel, MessageID, ThreadID, Host string
	Going                                      []string
}

// joinedOf is a joined party found in the host's server (ok false when it
// isn't joined, or wasn't found yet).
func (s *Server) joinedOf(ctx context.Context, showtimeID int64) (joinedLink, bool) {
	var j joinedLink
	var going string
	err := s.db.QueryRowContext(ctx, `SELECT server, channel, message_id, thread_id, host, going FROM community_joined WHERE showtime_id = ? AND thread_id <> ''`, showtimeID).
		Scan(&j.Server, &j.Channel, &j.MessageID, &j.ThreadID, &j.Host, &going)
	if going != "" {
		j.Going = strings.Split(going, "\n")
	}
	return j, err == nil
}

// asParty is a joined party in the shape the chat code takes.
func (j joinedLink) asParty(showtimeID int64) party {
	return party{ShowtimeID: showtimeID, Server: j.Server, Channel: j.Channel, MessageID: j.MessageID, ThreadID: j.ThreadID, Host: j.Host, joined: true}
}

// chatPartyOf is the party whose chat a showtime has: its own, shared from
// here, or a friend's it joined.
func (s *Server) chatPartyOf(ctx context.Context, showtimeID int64) (party, bool) {
	if p, ok := s.partyOf(ctx, showtimeID); ok {
		return p, true
	}
	if j, ok := s.joinedOf(ctx, showtimeID); ok {
		return j.asParty(showtimeID), true
	}
	return party{}, false
}

func (s *Server) joinedOfThread(ctx context.Context, thread string) (int64, bool) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT showtime_id FROM community_joined WHERE thread_id = ? AND thread_id <> ''`, thread).Scan(&id)
	return id, err == nil
}

// cardCode reads the showtime code on a watch party's card.
func cardCode(c *community.Card) (showtimeCode, bool) {
	if c == nil {
		return showtimeCode{}, false
	}
	for _, f := range c.Fields {
		if f.Name == "Showtime code" {
			code, err := findShowtimeCode(f.Value)
			return code, err == nil
		}
	}
	return showtimeCode{}, false
}

var cardHostRe = regexp.MustCompile(`^\*\*(.+?)\*\*`)

// cardPeople reads who hosts a watch party's card, and who's going.
func cardPeople(c *community.Card) (host string, going []string) {
	if c == nil {
		return "", nil
	}
	if m := cardHostRe.FindStringSubmatch(c.Description); m != nil {
		host = m[1]
	}
	for _, f := range c.Fields {
		if !strings.HasPrefix(f.Name, "Going (") || strings.HasPrefix(f.Value, "Nobody") {
			continue
		}
		for _, n := range strings.Split(f.Value, ", ") {
			if n = strings.TrimSpace(n); n != "" {
				going = append(going, n)
			}
		}
	}
	return host, going
}

// joinedLooks keeps the search for a joined party's card to once every two
// minutes each: it reads every open thread in every server the bot is in.
var joinedLooks = struct {
	sync.Mutex
	at map[int64]time.Time
}{at: map[int64]time.Time{}}

// findJoinedParty looks for a joined showtime's party in the servers this
// Usherr's bot is in: an open "… watch party" thread whose card (another
// Usherr's) has the same showtime code. It reports whether it's linked now.
func (s *Server) findJoinedParty(ctx context.Context, st showtime) bool {
	if _, ok := s.joinedOf(ctx, st.ID); ok {
		return true
	}
	if !s.isJoined(ctx, st.ID) {
		return false
	}
	b := s.community()
	if b == nil || b.Status().State != "connected" {
		return false
	}
	joinedLooks.Lock()
	if time.Since(joinedLooks.at[st.ID]) < 2*time.Minute {
		joinedLooks.Unlock()
		return false
	}
	joinedLooks.at[st.ID] = time.Now()
	joinedLooks.Unlock()
	for _, srv := range b.Status().Servers {
		threads, err := b.Threads(ctx, srv.ID)
		if err != nil {
			continue
		}
		for _, th := range threads {
			if th.Parent == "" || !strings.HasSuffix(th.Name, " watch party") {
				continue
			}
			// A thread opened under a message has that message's id: the card.
			m, err := b.ReadMessage(ctx, community.Ref{Channel: th.Parent, ID: th.ID})
			if err != nil || !m.ByBot || m.FromBot {
				continue
			}
			code, ok := cardCode(m.Card)
			if !ok || code.TMDBID != st.TMDBID || code.AtMs != st.AtMs {
				continue
			}
			host, going := cardPeople(m.Card)
			if _, err := s.db.ExecContext(ctx, `UPDATE community_joined SET server = ?, channel = ?, message_id = ?, thread_id = ?, host = ?, going = ? WHERE showtime_id = ?`,
				srv.ID, th.Parent, th.ID, th.ID, host, strings.Join(going, "\n"), st.ID); err != nil {
				return false
			}
			slog.Info("community: found the watch party joined by its code", "title", st.Title, "host", host)
			s.guestStubPeople(ctx, st)
			return true
		}
	}
	return false
}

// findJoinedSoon looks for a joined party in the background.
func (s *Server) findJoinedSoon(showtimeID int64) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if st, ok := s.showtimeByID(ctx, showtimeID); ok {
			s.findJoinedParty(ctx, st)
		}
	}()
}

// readJoinedCard reads a joined party's card again: who hosts, who's going.
func (s *Server) readJoinedCard(showtimeID int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	j, ok := s.joinedOf(ctx, showtimeID)
	b := s.community()
	if !ok || b == nil {
		return
	}
	m, err := b.ReadMessage(ctx, community.Ref{Channel: j.Channel, ID: j.MessageID})
	if err != nil || m.Card == nil {
		return
	}
	host, going := cardPeople(m.Card)
	if host == "" {
		host = j.Host
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE community_joined SET host = ?, going = ? WHERE showtime_id = ?`, host, strings.Join(going, "\n"), showtimeID)
	if st, ok := s.showtimeByID(ctx, showtimeID); ok {
		s.guestStubPeople(ctx, st)
	}
}

// guestStubPeople writes who hosted and who else was there on the stub of a
// joined party found in the host's server. Your seat is your place among
// those going (your linked Discord name), or 1.
func (s *Server) guestStubPeople(ctx context.Context, st showtime) {
	j, ok := s.joinedOf(ctx, st.ID)
	if !ok {
		return
	}
	me := ""
	if l := s.communityLinkOf(st.UserID); l != nil {
		me = l.Name
	}
	seat := 1
	others := []string{}
	for i, n := range j.Going {
		if me != "" && n == me {
			seat = i + 1
			continue
		}
		if n != j.Host {
			others = append(others, n)
		}
	}
	if j.Host != "" && j.Host != me {
		others = append([]string{j.Host}, others...)
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE community_stubs SET host = ?, seat = ?, guests = ? WHERE backend = 'here' AND showtime_id = ? AND external_id = ?`,
		j.Host, seat, strings.Join(others, "\n"), st.ID, strconv.FormatInt(st.UserID, 10))
}

// stampLineRe is the host's bot saying someone's stub was stamped (stampLine).
var stampLineRe = regexp.MustCompile(`<@!?(\d+)> stayed to the end`)

// stampLine is what the host's bot says in the thread when someone's stub is
// stamped there; a guest's Usherr reads it to stamp theirs.
func stampLine(discordID string) string {
	return "🎟️ <@" + discordID + "> stayed to the end: stub stamped."
}

// joinedStamp reads a message in a joined party's thread: when it's another
// bot saying that the person who booked it here stayed to the end, their stub
// here is stamped too.
func (s *Server) joinedStamp(ctx context.Context, showtimeID int64, m community.Message) {
	if !m.ByBot || m.FromBot {
		return
	}
	found := stampLineRe.FindAllStringSubmatch(m.Text, -1)
	if len(found) == 0 {
		return
	}
	st, ok := s.showtimeByID(ctx, showtimeID)
	if !ok || st.State == "booked" || st.State == "preshow" {
		return
	}
	var mine string
	_ = s.db.QueryRowContext(ctx, `SELECT external_id FROM community_links WHERE user_id = ? AND backend = 'discord'`, st.UserID).Scan(&mine)
	for _, f := range found {
		if mine == "" || f[1] != mine {
			continue
		}
		s.guestDoor(ctx, st) // the stub, if the door was missed
		s.guestStubPeople(ctx, st)
		_, _ = s.db.ExecContext(ctx, `UPDATE community_stubs SET stamped_ms = ? WHERE backend = 'here' AND external_id = ? AND showtime_id = ? AND stamped_ms = 0`,
			time.Now().UnixMilli(), strconv.FormatInt(st.UserID, 10), st.ID)
		return
	}
}

// joinedCardChanged hears an edit of a joined party's card (someone said
// I'm in, or it's over) and reads it again. It reports whether it was one.
func (s *Server) joinedCardChanged(ctx context.Context, ev community.MessageEvent) bool {
	if ev.Kind != "edit" {
		return false
	}
	var id int64
	if s.db.QueryRowContext(ctx, `SELECT showtime_id FROM community_joined WHERE message_id = ? AND channel = ? AND message_id <> ''`, ev.ID, ev.Channel).Scan(&id) != nil {
		return false
	}
	go s.readJoinedCard(id)
	return true
}
