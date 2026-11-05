package api

// Usherr: Community, step 5. The watch party's chat in Usherr. What's said in
// a party's thread in Discord reaches Usherr through the bot, is kept, and is
// pushed live to the Showtime page and the phone page (/showtime/chat/{id});
// what someone types there is posted in the thread under their own linked
// Discord name and picture (through the channel's webhook). Everyone sees the
// same thread, in Discord or in Usherr.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/community"
	"github.com/rdborg/mediarium/internal/community/discord"
)

// chatEvent is pushed to the pages showing a party's chat.
type chatEvent struct {
	Kind    string            `json:"kind"` // new, edit or delete
	Message community.Message `json:"message"`
}

// chatHub sends a party's messages to the pages open on its chat.
type chatHub struct {
	mu   sync.Mutex
	subs map[int64]map[chan chatEvent]struct{}
	// synced: the threads whose history was read since Usherr started.
	synced map[int64]bool
	sends  *auth.LoginLimiter
	once   sync.Once
}

func (h *chatHub) init() {
	h.once.Do(func() {
		h.subs = map[int64]map[chan chatEvent]struct{}{}
		h.synced = map[int64]bool{}
		h.sends = auth.NewLoginLimiter(20, time.Minute)
	})
}

func (h *chatHub) subscribe(id int64) chan chatEvent {
	h.init()
	ch := make(chan chatEvent, 32)
	h.mu.Lock()
	if h.subs[id] == nil {
		h.subs[id] = map[chan chatEvent]struct{}{}
	}
	h.subs[id][ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *chatHub) unsubscribe(id int64, ch chan chatEvent) {
	h.mu.Lock()
	delete(h.subs[id], ch)
	h.mu.Unlock()
}

func (h *chatHub) send(id int64, ev chatEvent) {
	h.init()
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[id] {
		select {
		case ch <- ev:
		default: // a page that stopped reading misses it; it reloads on reconnect
		}
	}
}

// partyOfThread is the watch party a Discord thread belongs to.
func (s *Server) partyOfThread(ctx context.Context, thread string) (int64, bool) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT showtime_id FROM community_parties WHERE thread_id = ? AND thread_id <> ''`, thread).Scan(&id)
	return id, err == nil
}

func (s *Server) keepMessage(ctx context.Context, showtimeID int64, m community.Message) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO community_messages (backend, message_id, showtime_id, author_id, author_name, avatar, text, images, at_ms, edited, from_bot)
		VALUES ('discord', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (backend, message_id) DO UPDATE SET text = excluded.text, images = excluded.images, edited = excluded.edited`,
		m.ID, showtimeID, m.User.ID, trimRunes(m.User.Name, 100), m.User.Avatar, m.Text, strings.Join(m.Images, "\n"), m.At.UnixMilli(), m.Edited, m.FromBot)
	return err
}

// communityMessage hears a message in Discord: one in a party's thread is
// kept and pushed to the pages showing that party's chat.
func (s *Server) communityMessage(ev community.MessageEvent) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	id, ok := s.partyOfThread(ctx, ev.Channel)
	joined := false
	if !ok {
		// A friend's party joined by its code, in their server (home_community_joined.go).
		id, joined = s.joinedOfThread(ctx, ev.Channel)
		ok = joined
	}
	if !ok {
		if s.joinedCardChanged(ctx, ev) {
			return
		}
		s.reviewsForumMessage(ctx, ev) // Usherr: step 6, a #review in a film's post (home_community_review.go)
		return
	}
	if joined {
		// The host's bot's alerts look like this bot's own; its reviews are the host's.
		if ev.Kind == "new" {
			s.joinedStamp(ctx, id, ev.Message)
		}
		ev.FromBot = ev.ByBot
	} else if st, ok := s.showtimeByID(ctx, id); ok {
		// Usherr: step 6, a #review in the party's thread is a review of its film.
		s.typedReview(ctx, "movie", st.TMDBID, st.Title, st.Year, st.PosterURL, "the "+st.Title+" watch party", ev)
	}
	switch ev.Kind {
	case "delete":
		_, _ = s.db.ExecContext(ctx, `DELETE FROM community_messages WHERE backend = 'discord' AND message_id = ?`, ev.ID)
	case "edit":
		// An edit carries the new text; keep the rest as it was.
		var at int64
		if s.db.QueryRowContext(ctx, `SELECT at_ms FROM community_messages WHERE backend = 'discord' AND message_id = ?`, ev.ID).Scan(&at) != nil {
			return
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE community_messages SET text = ?, images = ?, edited = 1 WHERE backend = 'discord' AND message_id = ?`,
			ev.Text, strings.Join(ev.Images, "\n"), ev.ID); err != nil {
			return
		}
		ev.At, ev.Edited = time.UnixMilli(at), true
	default:
		if ev.At.IsZero() {
			ev.At = time.Now()
		}
		if err := s.keepMessage(ctx, id, ev.Message); err != nil {
			slog.Warn("community: couldn't keep a chat message", "err", err)
			return
		}
	}
	s.chat.send(id, chatEvent{Kind: ev.Kind, Message: ev.Message})
}

// chatHistory reads a party's kept messages, oldest first.
func (s *Server) chatHistory(ctx context.Context, showtimeID int64) []community.Message {
	out := []community.Message{}
	rows, err := s.db.QueryContext(ctx, `SELECT message_id, author_id, author_name, avatar, text, images, at_ms, edited, from_bot
		FROM community_messages WHERE showtime_id = ? ORDER BY at_ms, message_id`, showtimeID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var m community.Message
		var images string
		var at int64
		if rows.Scan(&m.ID, &m.User.ID, &m.User.Name, &m.User.Avatar, &m.Text, &images, &at, &m.Edited, &m.FromBot) != nil {
			continue
		}
		if images != "" {
			m.Images = strings.Split(images, "\n")
		}
		m.At = time.UnixMilli(at)
		out = append(out, m)
	}
	return out
}

// syncChat reads what was said in a party's thread while Usherr wasn't
// listening (it was off, or the party is older than this run), once per run.
func (s *Server) syncChat(ctx context.Context, showtimeID int64, p party) {
	s.chat.init()
	s.chat.mu.Lock()
	done := s.chat.synced[showtimeID]
	s.chat.synced[showtimeID] = true
	s.chat.mu.Unlock()
	b := s.community()
	if done || b == nil || p.ThreadID == "" {
		return
	}
	var last string
	_ = s.db.QueryRowContext(ctx, `SELECT message_id FROM community_messages WHERE showtime_id = ? ORDER BY length(message_id) DESC, message_id DESC LIMIT 1`, showtimeID).Scan(&last)
	if last == "" {
		last = "0"
	}
	for range 5 { // up to 500 messages
		list, err := b.History(ctx, p.ThreadID, last, 100)
		if err != nil {
			slog.Warn("community: couldn't read the party's chat from Discord", "err", err)
			return
		}
		for _, m := range list {
			if m.At.IsZero() {
				m.At = time.Now()
			}
			if p.joined {
				s.joinedStamp(ctx, showtimeID, m) // stamped while this Usherr wasn't listening
				m.FromBot = m.ByBot
			}
			_ = s.keepMessage(ctx, showtimeID, m)
			if discord.SnowflakeLess(last, m.ID) {
				last = m.ID
			}
		}
		if len(list) < 100 {
			return
		}
	}
}

// chatParty reads the party a chat request is about.
func (s *Server) chatParty(w http.ResponseWriter, r *http.Request) (showtime, party, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "That watch party isn't known.")
		return showtime{}, party{}, false
	}
	st, ok := s.showtimeByID(r.Context(), id)
	p, ok2 := s.chatPartyOf(r.Context(), id) // Usherr: or a friend's, joined by its code
	if !ok || !ok2 {
		writeError(w, http.StatusNotFound, "That showtime isn't shared as a watch party.")
		return showtime{}, party{}, false
	}
	return st, p, true
}

// GET /api/home/showtimes/{id}/chat (member): the party's chat so far, and
// whether you can post in it.
func (s *Server) handlePartyChat(w http.ResponseWriter, r *http.Request) {
	st, p, ok := s.chatParty(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	s.syncChat(ctx, st.ID, p)
	cancel()
	u := auth.UserFromContext(r.Context())
	linked := u != nil && s.communityLinkOf(u.ID) != nil
	connected := false
	var warnings []string
	if b := s.community(); b != nil {
		bs := b.Status()
		connected = bs.State == "connected"
		warnings = bs.Warnings
	}
	link := ""
	if p.Server != "" && p.ThreadID != "" {
		link = fmt.Sprintf("https://discord.com/channels/%s/%s", p.Server, p.ThreadID)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"showtime":  st,
		"messages":  s.chatHistory(r.Context(), st.ID),
		"linked":    linked,
		"connected": connected,
		"canPost":   linked && connected && p.ThreadID != "",
		"discord":   link,
		"warnings":  append([]string{}, warnings...),
		"nowMs":     time.Now().UnixMilli(),
	})
}

// GET /api/home/showtimes/{id}/chat/events (member): the chat, live.
func (s *Server) handlePartyChatEvents(w http.ResponseWriter, r *http.Request) {
	st, _, ok := s.chatParty(w, r)
	if !ok {
		return
	}
	rc := http.NewResponseController(w)
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	ch := s.chat.subscribe(st.ID)
	defer s.chat.unsubscribe(st.ID, ch)
	write := func(line string) bool {
		if _, err := fmt.Fprint(w, line); err != nil {
			return false
		}
		return rc.Flush() == nil
	}
	if !write("retry: 5000\n: hello\n\n") {
		return
	}
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			if !write(": ping\n\n") {
				return
			}
		case ev := <-ch:
			b, _ := json.Marshal(ev)
			if !write("data: " + string(b) + "\n\n") {
				return
			}
		}
	}
}

// POST /api/home/showtimes/{id}/chat {"text"} (member): say something in the
// party's thread, under your linked Discord name.
func (s *Server) handlePartyChatSend(w http.ResponseWriter, r *http.Request) {
	st, p, ok := s.chatParty(w, r)
	if !ok {
		return
	}
	u := auth.UserFromContext(r.Context())
	if u == nil {
		writeError(w, http.StatusUnauthorized, "Sign in first.")
		return
	}
	link := s.communityLinkOf(u.ID)
	if link == nil {
		writeError(w, http.StatusForbidden, "Link your Discord account first (Settings, Linked services), so your messages carry your name.")
		return
	}
	var body struct {
		Text string `json:"text"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	text := strings.TrimSpace(body.Text)
	if text == "" {
		writeError(w, http.StatusBadRequest, "Type a message first.")
		return
	}
	if len([]rune(text)) > 2000 {
		writeError(w, http.StatusBadRequest, "That's longer than Discord allows (2,000 characters).")
		return
	}
	s.chat.init()
	if !s.chat.sends.Reserve(fmt.Sprint(u.ID)) {
		writeError(w, http.StatusTooManyRequests, "That's a lot of messages at once. Wait a moment.")
		return
	}
	b := s.community()
	if b == nil || p.ThreadID == "" {
		writeError(w, http.StatusConflict, "Community isn't connected right now.")
		return
	}
	var discordID string
	_ = s.db.QueryRowContext(r.Context(), `SELECT external_id FROM community_links WHERE user_id = ? AND backend = 'discord'`, u.ID).Scan(&discordID)
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	ref, err := b.PostAs(ctx, p.Channel, p.ThreadID, community.User{ID: discordID, Name: link.Name, Avatar: link.Avatar}, text)
	if err != nil {
		writeError(w, http.StatusBadGateway, communityProblem(err, "in the party's thread"))
		return
	}
	// The gateway also brings it back; keeping it now shows it at once.
	m := community.Message{ID: ref.ID, Channel: p.ThreadID, User: community.User{ID: discordID, Name: link.Name, Avatar: link.Avatar}, Text: text, At: time.Now()}
	if s.keepMessage(r.Context(), st.ID, m) == nil {
		s.chat.send(st.ID, chatEvent{Kind: "new", Message: m})
	}
	writeJSON(w, http.StatusCreated, map[string]any{"message": m})
}
