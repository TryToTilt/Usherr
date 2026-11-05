package api

// Usherr: Community, step 7. Ticket stubs: one for each watch party someone
// went to, like a cinema's. "At the door", when a party's film starts,
// everyone who pressed I'm in gets one (their seat is the order they said
// so), and so does the host (linked to Discord); someone who says I'm in once
// it has started gets theirs then. "On the way out", at the credits (or when
// the host ends it), a button in the party's thread stamps it, for half an
// hour: a stamped stub says they stayed to the end. A stub keeps the film,
// the night, the host and who else was there. They show under You, Ticket
// stubs, in Usherr, and with /stubs in Discord.

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/brand"
	"github.com/rdborg/mediarium/internal/community"
)

type stub struct {
	ShowtimeID int64    `json:"showtimeId"`
	Kind       string   `json:"kind"`
	TMDBID     int      `json:"tmdbId"`
	Title      string   `json:"title"`
	Year       int      `json:"year,omitempty"`
	Poster     string   `json:"poster,omitempty"`
	Host       string   `json:"host"`
	Seat       int      `json:"seat"` // 0: the host
	Guests     []string `json:"guests"`
	AtMs       int64    `json:"atMs"`
	Stamped    bool     `json:"stamped"` // stayed to the end
	// Here: from a friend's party joined with its code, kept by this Usherr
	// (home_community_guest.go). StampUntil: it can be stamped by hand until then.
	Here       bool  `json:"here,omitempty"`
	StampUntil int64 `json:"stampUntil,omitempty"`
}

// stampFor is how long the stamp button works after the credits.
const stampFor = 30 * time.Minute

// creditsAt is when a showtime's film ends: its start plus its length (two
// hours when unknown).
func creditsAt(st showtime) int64 {
	minutes := st.Runtime
	if minutes <= 0 {
		minutes = 120
	}
	return st.AtMs + int64(minutes)*60_000
}

// partyWrap is the end of a party's film: the stamp button goes up in its
// thread, for half an hour. ended: the host stopped it before the credits.
func (s *Server) partyWrap(ctx context.Context, st showtime, p party, ended bool) {
	b := s.community()
	if b == nil {
		return
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE community_parties SET told = 'credits' WHERE showtime_id = ?`, st.ID)
	_, _ = s.db.ExecContext(ctx, `UPDATE community_stubs SET stamp_until = ? WHERE showtime_id = ? AND stamp_until = 0`, time.Now().Add(stampFor).UnixMilli(), st.ID)
	text := fmt.Sprintf("🎬 That's a wrap! Stayed to the end of **%s**? Get your ticket stub stamped: the button works for 30 minutes.", st.Title)
	if ended {
		text = fmt.Sprintf("🎬 The **%s** watch party has ended. Thanks for watching! Get your ticket stub stamped: the button works for 30 minutes.", st.Title)
	}
	where := p.ThreadID
	if where == "" {
		where = p.Channel
	}
	if _, err := b.Post(ctx, where, community.Post{Text: text, Buttons: []community.Button{
		{Label: "🎟️ Get my stub stamped", ID: fmt.Sprintf("stub:stamp:%d", st.ID), Style: "success"},
	}}); err != nil {
		slog.Warn("community: couldn't post the stamp button", "err", err)
	}
}

// partyCredits posts the stamp button when a party's film reaches its
// credits (showtimeJob), once.
func (s *Server) partyCredits(showtimeID int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	p, ok := s.partyOf(ctx, showtimeID)
	st, ok2 := s.showtimeByID(ctx, showtimeID)
	if !ok || !ok2 || p.Told == "credits" || time.Now().UnixMilli() > creditsAt(st)+stampFor.Milliseconds() {
		return
	}
	s.partyWrap(ctx, st, p, false)
}

// stampButton stamps the presser's stub, while the button works.
func (s *Server) stampButton(ctx context.Context, in community.Interaction) string {
	var id int64
	if _, err := fmt.Sscanf(in.Name, "stub:stamp:%d", &id); err != nil {
		return "That button isn't known."
	}
	var title string
	var until, stamped int64
	if s.db.QueryRowContext(ctx, `SELECT title, stamp_until, stamped_ms FROM community_stubs WHERE backend = 'discord' AND external_id = ? AND showtime_id = ?`,
		in.User.ID, id).Scan(&title, &until, &stamped) != nil {
		return "You don't have a stub for this one: stubs are handed out at the door, to everyone who pressed **I'm in** on the party's card."
	}
	switch {
	case stamped > 0:
		return fmt.Sprintf("Your **%s** stub is already stamped. ✅", title)
	case until == 0:
		return "Stamping opens at the credits: come back when the film ends."
	case time.Now().UnixMilli() > until:
		return "The stamping is over for this film: the button works for 30 minutes after the credits."
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE community_stubs SET stamped_ms = ? WHERE backend = 'discord' AND external_id = ? AND showtime_id = ?`, time.Now().UnixMilli(), in.User.ID, id)
	// Said in the thread too, so a guest's own Usherr (joined by the code, its
	// bot in this server) stamps their stub there (home_community_joined.go).
	if p, ok := s.partyOf(ctx, id); ok {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			s.partySay(ctx, p, stampLine(in.User.ID))
		}()
	}
	return fmt.Sprintf("Stamped! ✅ Your **%s** stub says you stayed to the end. /stubs shows them all.", title)
}

// giveStubs hands out a party's stubs when its film starts: to everyone who
// said I'm in (seats in that order) and to the host. Who already has one
// keeps it.
func (s *Server) giveStubs(ctx context.Context, st showtime, p party) []string {
	people := s.partyPeople(ctx, st.ID)
	type who struct{ id, name string }
	var going []who
	for _, pp := range people {
		if pp.Answer == "in" {
			going = append(going, who{pp.ID, pp.Name})
		}
	}
	var hostID string
	_ = s.db.QueryRowContext(ctx, `SELECT external_id FROM community_links WHERE backend = 'discord' AND user_id = ?`, st.UserID).Scan(&hostID)
	names := []string{}
	for _, g := range going {
		names = append(names, g.name)
	}
	give := func(id, name string, seat int) {
		var guests []string
		for _, n := range names {
			if n != name {
				guests = append(guests, n)
			}
		}
		if seat > 0 && p.Host != "" && p.Host != name && !containsString(guests, p.Host) {
			guests = append([]string{p.Host}, guests...)
		}
		_, _ = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO community_stubs (backend, external_id, showtime_id, name, kind, tmdb_id, title, year, poster, host, seat, guests, at_ms)
			VALUES ('discord', ?, ?, ?, 'movie', ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, st.ID, name, st.TMDBID, st.Title, st.Year, st.PosterURL, p.Host, seat, strings.Join(guests, "\n"), st.AtMs)
	}
	got := []string{}
	hostGoing := false
	for i, g := range going {
		if g.id == hostID {
			hostGoing = true
		}
		give(g.id, g.name, i+1)
		got = append(got, g.name)
	}
	if hostID != "" && !hostGoing {
		give(hostID, p.Host, 0)
		got = append([]string{p.Host}, got...)
	}
	return got
}

// lateStub gives a stub to someone who says I'm in once the film has started.
func (s *Server) lateStub(ctx context.Context, st showtime, p party, who community.User) {
	var n int
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM community_stubs WHERE showtime_id = ? AND seat > 0`, st.ID).Scan(&n)
	var guests []string
	rows, err := s.db.QueryContext(ctx, `SELECT name FROM community_stubs WHERE showtime_id = ? AND external_id <> ?`, st.ID, who.ID)
	if err == nil {
		for rows.Next() {
			var name string
			if rows.Scan(&name) == nil {
				guests = append(guests, name)
			}
		}
		rows.Close()
	}
	_, _ = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO community_stubs (backend, external_id, showtime_id, name, kind, tmdb_id, title, year, poster, host, seat, guests, at_ms)
		VALUES ('discord', ?, ?, ?, 'movie', ?, ?, ?, ?, ?, ?, ?, ?)`,
		who.ID, st.ID, who.Name, st.TMDBID, st.Title, st.Year, st.PosterURL, p.Host, n+1, strings.Join(guests, "\n"), st.AtMs)
}

// stubsOf are someone's stubs, newest first.
func (s *Server) stubsOf(ctx context.Context, externalID string) []stub {
	return s.stubsFrom(ctx, "discord", externalID)
}

// stubsFrom are the stubs one backend keeps for someone: 'discord' (the
// bot's), or 'here' (a guest's, by account id).
func (s *Server) stubsFrom(ctx context.Context, backend, externalID string) []stub {
	out := []stub{}
	rows, err := s.db.QueryContext(ctx, `SELECT showtime_id, kind, tmdb_id, title, year, poster, host, seat, guests, at_ms, stamped_ms > 0, stamp_until
		FROM community_stubs WHERE backend = ? AND external_id = ? AND hidden_ms = 0 ORDER BY at_ms DESC`, backend, externalID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var sb stub
		var guests string
		var until int64
		if rows.Scan(&sb.ShowtimeID, &sb.Kind, &sb.TMDBID, &sb.Title, &sb.Year, &sb.Poster, &sb.Host, &sb.Seat, &guests, &sb.AtMs, &sb.Stamped, &until) != nil {
			continue
		}
		if backend == "here" {
			sb.Here = true
			if !sb.Stamped && until > time.Now().UnixMilli() {
				sb.StampUntil = until
			}
		}
		sb.Guests = []string{}
		if guests != "" {
			sb.Guests = strings.Split(guests, "\n")
		}
		out = append(out, sb)
	}
	return out
}

// stubsReply answers /stubs: how many, and the latest few.
func (s *Server) stubsReply(ctx context.Context, who community.User) string {
	list := s.stubsOf(ctx, who.ID)
	if len(list) == 0 {
		return "🎟️ No ticket stubs yet. Press **I'm in** on a watch party: when its film starts, you get one."
	}
	var b strings.Builder
	stamped := 0
	for _, sb := range list {
		if sb.Stamped {
			stamped++
		}
	}
	if len(list) == 1 {
		b.WriteString("🎟️ **1 ticket stub**")
	} else {
		fmt.Fprintf(&b, "🎟️ **%d ticket stubs**", len(list))
	}
	fmt.Fprintf(&b, ", %d stamped (stayed to the end)\n", stamped)
	for i, sb := range list {
		if i == 10 {
			fmt.Fprintf(&b, "…and %d more. All of them are under You, Ticket stubs, in %s.", len(list)-10, brand.Name)
			break
		}
		seat := fmt.Sprintf("seat %d", sb.Seat)
		if sb.Seat == 0 {
			seat = "host"
		}
		mark := "🎟️"
		if sb.Stamped {
			mark = "✅"
		}
		fmt.Fprintf(&b, "%s **%s** · <t:%d:d> · %s", mark, titleYear(sb.Title, sb.Year), sb.AtMs/1000, seat)
		if len(sb.Guests) > 0 {
			fmt.Fprintf(&b, " · with %s", strings.Join(sb.Guests, ", "))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// GET /api/home/stubs (member): your ticket stubs: the bot's (through your
// linked Discord account) and the ones for friends' parties you joined with
// a code, newest first.
func (s *Server) handleStubs(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		writeError(w, http.StatusUnauthorized, "Sign in first.")
		return
	}
	ext := s.myDiscordID(r.Context(), u.ID)
	list := []stub{}
	if ext != "" {
		list = s.stubsOf(r.Context(), ext)
	}
	list = append(list, s.stubsFrom(r.Context(), "here", strconv.FormatInt(u.ID, 10))...)
	sort.SliceStable(list, func(i, j int) bool { return list[i].AtMs > list[j].AtMs })
	writeJSON(w, http.StatusOK, map[string]any{"stubs": list, "linked": ext != "", "nowMs": time.Now().UnixMilli()})
}

// myStub is the stub a request is about: one of yours, by where it's kept
// ("here": a friend's party joined by its code; "discord": this Usherr's
// bot's, through your linked Discord account) and its party's showtime.
func (s *Server) myStub(w http.ResponseWriter, r *http.Request) (backend, externalID string, id int64, ok bool) {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		writeError(w, http.StatusUnauthorized, "Sign in first.")
		return "", "", 0, false
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "That ticket stub isn't known.")
		return "", "", 0, false
	}
	switch r.PathValue("from") {
	case "here":
		return "here", strconv.FormatInt(u.ID, 10), id, true
	case "discord":
		if ext := s.myDiscordID(r.Context(), u.ID); ext != "" {
			return "discord", ext, id, true
		}
	}
	writeError(w, http.StatusNotFound, "That ticket stub isn't known.")
	return "", "", 0, false
}

// DELETE /api/home/stubs/{from}/{id} (member): remove one of your ticket
// stubs. It's kept, hidden (from here and from /stubs), so it can be brought
// back.
func (s *Server) handleHideStub(w http.ResponseWriter, r *http.Request) {
	s.setStubHidden(w, r, time.Now().UnixMilli())
}

// POST /api/home/stubs/{from}/{id}/restore (member): bring a removed stub back.
func (s *Server) handleRestoreStub(w http.ResponseWriter, r *http.Request) {
	s.setStubHidden(w, r, 0)
}

func (s *Server) setStubHidden(w http.ResponseWriter, r *http.Request, hiddenMs int64) {
	backend, ext, id, ok := s.myStub(w, r)
	if !ok {
		return
	}
	res, err := s.db.ExecContext(r.Context(), `UPDATE community_stubs SET hidden_ms = ? WHERE backend = ? AND external_id = ? AND showtime_id = ?`, hiddenMs, backend, ext, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The ticket stub couldn't be changed.")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "That ticket stub isn't known.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
