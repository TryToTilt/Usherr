package api

// Usherr: Community. Ticket stubs for a guest: someone who joined a friend's
// watch party with its showtime code, in their own Usherr. The host's bot
// keeps their Discord stub, but their own Usherr can't see it (the two aren't
// connected), so it hands out its own: at the door when the joined showtime
// starts, and stamped when this Usherr's player reaches the film's credits.
// Someone whose film played elsewhere (the TV), or who stopped when the host
// ended it early, can stamp it themselves for half an hour after the film's
// end, like the bot's button. These are community_stubs rows with backend
// 'here' and the account's id, shown with the rest under You, Ticket stubs.

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// guestStubWindow is how long after its start a joined film can still be
// stamped by playing it to the end (a long film, started late).
const guestStubWindow = 8 * time.Hour

// markJoined remembers that a showtime came from a friend's showtime code.
func (s *Server) markJoined(ctx context.Context, showtimeID int64, code string) {
	_, _ = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO community_joined (showtime_id, code, joined_ms) VALUES (?, ?, ?)`, showtimeID, code, time.Now().UnixMilli())
}

func (s *Server) isJoined(ctx context.Context, showtimeID int64) bool {
	var n int
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM community_joined WHERE showtime_id = ?`, showtimeID).Scan(&n)
	return n > 0
}

// guestDoor hands the stub out when a joined showtime's film starts.
func (s *Server) guestDoor(ctx context.Context, st showtime) {
	if !s.isJoined(ctx, st.ID) {
		return
	}
	_, _ = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO community_stubs (backend, external_id, showtime_id, name, kind, tmdb_id, title, year, poster, host, seat, guests, at_ms)
		VALUES ('here', ?, ?, '', 'movie', ?, ?, ?, ?, '', 1, '', ?)`,
		strconv.FormatInt(st.UserID, 10), st.ID, st.TMDBID, st.Title, st.Year, st.PosterURL, st.AtMs)
	s.guestStubPeople(ctx, st) // who hosted, who else: when found in the host's server
	if _, ok := s.joinedOf(ctx, st.ID); ok {
		go s.readJoinedCard(st.ID)
	}
}

// guestCredits opens the stamping at a joined film's credits, for half an hour.
func (s *Server) guestCredits(ctx context.Context, st showtime) {
	_, _ = s.db.ExecContext(ctx, `UPDATE community_stubs SET stamp_until = ? WHERE backend = 'here' AND showtime_id = ? AND stamp_until = 0`,
		creditsAt(st)+stampFor.Milliseconds(), st.ID)
}

// positionTMDB is the film a player position is for: a library movie
// ("movie:12") or a streamed one ("title:movie:603"). 0 for anything else.
func (s *Server) positionTMDB(ctx context.Context, item string) int {
	if rest, ok := strings.CutPrefix(item, "title:movie:"); ok {
		n, _ := strconv.Atoi(rest)
		return n
	}
	if rest, ok := strings.CutPrefix(item, "movie:"); ok {
		id, err := strconv.ParseInt(rest, 10, 64)
		if err != nil {
			return 0
		}
		var tmdb int
		_ = s.db.QueryRowContext(ctx, `SELECT tmdb_id FROM movies WHERE id = ?`, id).Scan(&tmdb)
		return tmdb
	}
	return 0
}

// reachedCredits says the player got to a film's end (not a play that barely
// began, which positionFinished also calls finished).
func reachedCredits(position, duration float64) bool {
	return position >= positionMinStart && duration > 0 && (position >= duration*positionDoneShare || duration-position <= positionDoneTail)
}

// guestPlayed stamps the account's stub for a joined film it just played to
// the credits (handlePositionPut).
func (s *Server) guestPlayed(ctx context.Context, userID int64, item string, position, duration float64) {
	if !reachedCredits(position, duration) {
		return
	}
	tmdb := s.positionTMDB(ctx, item)
	if tmdb <= 0 {
		return
	}
	now := time.Now().UnixMilli()
	_, _ = s.db.ExecContext(ctx, `UPDATE community_stubs SET stamped_ms = ? WHERE backend = 'here' AND external_id = ? AND tmdb_id = ? AND stamped_ms = 0 AND at_ms <= ? AND at_ms >= ?`,
		now, strconv.FormatInt(userID, 10), tmdb, now, now-guestStubWindow.Milliseconds())
}

// POST /api/home/stubs/{id}/stamp (member): stamp your own stub for a joined
// party yourself, in the half hour after its film ends.
func (s *Server) handleStampStub(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a ticket stub.")
		return
	}
	var until, stamped int64
	if s.db.QueryRowContext(r.Context(), `SELECT stamp_until, stamped_ms FROM community_stubs WHERE backend = 'here' AND external_id = ? AND showtime_id = ?`,
		strconv.FormatInt(requestUserID(r), 10), id).Scan(&until, &stamped) != nil {
		writeError(w, http.StatusNotFound, "You don't have that ticket stub.")
		return
	}
	now := time.Now().UnixMilli()
	switch {
	case stamped > 0:
		w.WriteHeader(http.StatusNoContent)
		return
	case until == 0:
		writeError(w, http.StatusConflict, "Stamping opens at the credits: come back when the film ends.")
		return
	case now > until:
		writeError(w, http.StatusConflict, "The stamping is over for this film: it works for 30 minutes after the credits.")
		return
	}
	_, _ = s.db.ExecContext(r.Context(), `UPDATE community_stubs SET stamped_ms = ? WHERE backend = 'here' AND external_id = ? AND showtime_id = ?`, now, strconv.FormatInt(requestUserID(r), 10), id)
	w.WriteHeader(http.StatusNoContent)
}
