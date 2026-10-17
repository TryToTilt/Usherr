package api

// Usherr: Live TV reminders. On a program in the guide, "Remind me" asks for
// a message (through the notification targets that take reminders, such as
// ntfy on a phone) five minutes before it starts.

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/notify"
)

const reminderLead = 5 * time.Minute

// POST /api/tv/remind {channelId, start} toggles a reminder; GET lists yours.
func (s *Server) handleTVRemind(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		writeError(w, http.StatusUnauthorized, "Sign in first.")
		return
	}
	var req struct {
		ChannelID int64  `json:"channelId"`
		Start     string `json:"start"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	start, err := time.Parse(time.RFC3339Nano, req.Start)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a program.")
		return
	}
	p, ok, err := s.tvRepo().At(req.ChannelID, start.Add(time.Second))
	if err != nil || !ok || !p.Start.Equal(start) {
		writeError(w, http.StatusNotFound, "That program isn't in the guide any more.")
		return
	}
	res, err := s.db.ExecContext(r.Context(), `DELETE FROM tv_reminders WHERE user_id = ? AND channel_id = ? AND start_ms = ?`, u.ID, req.ChannelID, start.UnixMilli())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		writeJSON(w, http.StatusOK, map[string]any{"on": false, "message": "Reminder taken off."})
		return
	}
	if _, err := s.db.ExecContext(r.Context(), `INSERT INTO tv_reminders (user_id, channel_id, start_ms, title, subtitle) VALUES (?, ?, ?, ?, ?)`,
		u.ID, req.ChannelID, start.UnixMilli(), p.Title, p.Subtitle); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"on": true, "message": fmt.Sprintf("You'll get a message five minutes before %s starts.", p.Title)})
}

func (s *Server) handleTVReminders(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFromContext(r.Context())
	out := []map[string]any{}
	if u != nil {
		rows, err := s.db.QueryContext(r.Context(), `SELECT channel_id, start_ms FROM tv_reminders WHERE user_id = ? AND sent = 0 AND start_ms > ?`, u.ID, time.Now().UnixMilli())
		if err == nil {
			for rows.Next() {
				var ch, ms int64
				if rows.Scan(&ch, &ms) == nil {
					out = append(out, map[string]any{"channelId": ch, "start": time.UnixMilli(ms)})
				}
			}
			rows.Close()
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// tvRemindJob sends the reminders that are due (run every minute).
func (s *Server) tvRemindJob(ctx context.Context) {
	due := time.Now().Add(reminderLead).UnixMilli()
	rows, err := s.db.QueryContext(ctx, `SELECT r.id, r.title, r.subtitle, r.start_ms, COALESCE(c.name, ''), COALESCE(c.number, 0)
		FROM tv_reminders r LEFT JOIN tv_channels c ON c.id = r.channel_id WHERE r.sent = 0 AND r.start_ms <= ?`, due)
	if err != nil {
		return
	}
	type rem struct {
		id             int64
		title, sub, ch string
		start          int64
		number         int
	}
	var list []rem
	for rows.Next() {
		var x rem
		if rows.Scan(&x.id, &x.title, &x.sub, &x.start, &x.ch, &x.number) == nil {
			list = append(list, x)
		}
	}
	rows.Close()
	for _, x := range list {
		_, _ = s.db.ExecContext(ctx, `UPDATE tv_reminders SET sent = 1 WHERE id = ?`, x.id)
		at := time.UnixMilli(x.start)
		if time.Since(at) > 10*time.Minute {
			continue // missed by far (the server was off): say nothing
		}
		msg := fmt.Sprintf("%s starts at %s on %d %s.", x.title, at.Local().Format("3:04 PM"), x.number, x.ch)
		if x.sub != "" {
			msg = fmt.Sprintf("%s (%s) starts at %s on %d %s.", x.title, x.sub, at.Local().Format("3:04 PM"), x.number, x.ch)
		}
		slog.Info("tv: reminder", "title", x.title)
		s.deliverNotification(notify.ComposeText(notify.EventReminder, "Starting soon: "+x.title, msg, s.publicLinks(), time.Now()))
	}
	// Old ones go.
	_, _ = s.db.ExecContext(ctx, `DELETE FROM tv_reminders WHERE start_ms < ?`, time.Now().Add(-24*time.Hour).UnixMilli())
}
