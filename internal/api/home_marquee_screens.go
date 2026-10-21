package api

// Usherr: the Marquee's screens and the phone remote. Every browser showing
// /marquee is a screen (table marquee_screens) with a name, and settings of
// its own or the shared ones. A screen keeps a server-sent-events stream
// open; settings changes and the remote's buttons arrive on it at once, and
// the screen reports back what it shows so the remote can preview it.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

var marqueeScreenIDRe = regexp.MustCompile(`^[a-z0-9-]{6,40}$`)

// The remote's buttons. "pause", "pin", "dim", "blank" and "info" take "on"
// or "off"; "scene" takes a scene kind; "preshow" starts the pre-show.
var marqueeCommands = map[string]bool{"next": true, "prev": true, "pause": true, "pin": true, "scene": true, "dim": true, "blank": true, "reload": true, "info": true, "preshow": true}

// A screen counts as online while its stream is open, or for a little while
// after it last asked for anything (a stream reconnecting).
const marqueeOnlineFor = 90 * time.Second

type marqueeEvent struct {
	Type string `json:"type"` // "settings" or "command"
	Cmd  string `json:"cmd,omitempty"`
	Arg  string `json:"arg,omitempty"`
}

// marqueeShowing is what a screen says it shows now.
type marqueeShowing struct {
	Scene     string `json:"scene"`
	Title     string `json:"title,omitempty"`
	PosterURL string `json:"posterUrl,omitempty"`
	Paused    bool   `json:"paused"`
	Pinned    bool   `json:"pinned"`
	Dim       bool   `json:"dim"`
	Blank     bool   `json:"blank"`
	Info      bool   `json:"info"`
	Quiet     bool   `json:"quiet"`
	AtMs      int64  `json:"atMs"`
}

// marqueeHub holds the open streams and what each screen shows. The zero
// value is ready to use.
type marqueeHub struct {
	mu      sync.Mutex
	subs    map[string]map[chan marqueeEvent]struct{}
	showing map[string]marqueeShowing
}

func (h *marqueeHub) subscribe(id string) chan marqueeEvent {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.subs == nil {
		h.subs = map[string]map[chan marqueeEvent]struct{}{}
	}
	if h.subs[id] == nil {
		h.subs[id] = map[chan marqueeEvent]struct{}{}
	}
	ch := make(chan marqueeEvent, 16)
	h.subs[id][ch] = struct{}{}
	return ch
}

func (h *marqueeHub) unsubscribe(id string, ch chan marqueeEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.subs[id], ch)
	if len(h.subs[id]) == 0 {
		delete(h.subs, id)
	}
}

// send passes an event to one screen ("" is every screen). A screen too slow
// to take it misses it; it asks for everything again on its next refresh.
func (h *marqueeHub) send(id string, ev marqueeEvent) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for sid, chans := range h.subs {
		if id != "" && sid != id {
			continue
		}
		for ch := range chans {
			select {
			case ch <- ev:
				n++
			default:
			}
		}
	}
	return n
}

func (h *marqueeHub) connected(id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs[id]) > 0
}

func (h *marqueeHub) setShowing(id string, sh marqueeShowing) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.showing == nil {
		h.showing = map[string]marqueeShowing{}
	}
	h.showing[id] = sh
}

func (h *marqueeHub) nowShowing(id string) (marqueeShowing, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	sh, ok := h.showing[id]
	return sh, ok
}

func (h *marqueeHub) forget(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.showing, id)
}

// marqueeScreenID reads ?screen= ("" when missing or not an id).
func marqueeScreenID(r *http.Request) string {
	id := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("screen")))
	if !marqueeScreenIDRe.MatchString(id) {
		return ""
	}
	return id
}

// marqueeSeen records a screen asking for the Marquee: a new one is added
// with a name, a known one gets its last-seen time and size.
func (s *Server) marqueeSeen(ctx context.Context, id string, width, height int) {
	if id == "" {
		return
	}
	now := time.Now().UnixMilli()
	if width < 0 || width > 20000 {
		width = 0
	}
	if height < 0 || height > 20000 {
		height = 0
	}
	res, err := s.db.ExecContext(ctx, `UPDATE marquee_screens SET seen_ms = ?,
		width = CASE WHEN ? > 0 THEN ? ELSE width END, height = CASE WHEN ? > 0 THEN ? ELSE height END WHERE id = ?`,
		now, width, width, height, height, id)
	if err != nil {
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return
	}
	var count int
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM marquee_screens`).Scan(&count)
	// A new screen goes at the end of the list.
	_, _ = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO marquee_screens (id, name, created_ms, seen_ms, width, height, position)
		VALUES (?, ?, ?, ?, ?, ?, (SELECT COALESCE(MAX(position), -1) + 1 FROM marquee_screens))`,
		id, fmt.Sprintf("Screen %d", count+1), now, now, width, height)
	s.marqueeMQTTState(context.WithoutCancel(ctx), id)
}

// marqueeSettingsFor is what a screen shows: the shared settings with the
// fields it keeps of its own on top; own says whether it keeps any.
func (s *Server) marqueeSettingsFor(ctx context.Context, id string) (marqueeSettings, bool) {
	shared := s.marqueeSettings()
	if id != "" {
		var raw string
		if s.db.QueryRowContext(ctx, `SELECT settings FROM marquee_screens WHERE id = ?`, id).Scan(&raw) == nil && raw != "" {
			if own := s.screenOwnSettings(ctx, id, raw, shared); len(own) > 0 {
				return withOwn(shared, own), true
			}
		}
	}
	return shared, false
}

// GET /api/home/marquee/events?screen=ID (member): the screen's live stream.
func (s *Server) handleMarqueeEvents(w http.ResponseWriter, r *http.Request) {
	id := marqueeScreenID(r)
	if id == "" {
		writeError(w, http.StatusBadRequest, "This screen has no id.")
		return
	}
	rc := http.NewResponseController(w)
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	ch := s.marqueeLive.subscribe(id)
	defer func() {
		s.marqueeLive.unsubscribe(id, ch)
		s.marqueeMQTTState(context.Background(), id)
	}()
	s.marqueeSeen(r.Context(), id, 0, 0)
	s.marqueeMQTTState(context.WithoutCancel(r.Context()), id)

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
			s.marqueeSeen(r.Context(), id, 0, 0)
		case ev := <-ch:
			b, _ := json.Marshal(ev)
			if !write("data: " + string(b) + "\n\n") {
				return
			}
		}
	}
}

// POST /api/home/marquee/showing?screen=ID (member): what the screen shows now.
func (s *Server) handleMarqueeShowing(w http.ResponseWriter, r *http.Request) {
	id := marqueeScreenID(r)
	if id == "" {
		writeError(w, http.StatusBadRequest, "This screen has no id.")
		return
	}
	var sh marqueeShowing
	if err := decodeJSON(r, &sh); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	sh.Scene = trimRunes(sh.Scene, 30)
	sh.Title = trimRunes(sh.Title, 200)
	if !strings.HasPrefix(sh.PosterURL, "https://") && !strings.HasPrefix(sh.PosterURL, "/") {
		sh.PosterURL = ""
	}
	sh.PosterURL = trimRunes(sh.PosterURL, 500)
	sh.AtMs = time.Now().UnixMilli()
	s.marqueeLive.setShowing(id, sh)
	s.marqueeMQTTState(context.WithoutCancel(r.Context()), id)
	w.WriteHeader(http.StatusNoContent)
}

type marqueeScreen struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Online   bool            `json:"online"`
	SeenMs   int64           `json:"seenMs"`
	Width    int             `json:"width"`
	Height   int             `json:"height"`
	Own      bool            `json:"own"`               // keeps some settings of its own
	OwnKeys  []string        `json:"ownKeys,omitempty"` // which (the settings' JSON names)
	Settings marqueeSettings `json:"settings"`          // what it uses (its own or the shared)
	Showing  *marqueeShowing `json:"showing,omitempty"`
}

func (s *Server) marqueeScreens(ctx context.Context) ([]marqueeScreen, error) {
	out := []marqueeScreen{}
	// In the order set on the Screens tab.
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, settings, seen_ms, width, height FROM marquee_screens ORDER BY position, created_ms, id`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	shared := s.marqueeSettings()
	now := time.Now()
	for rows.Next() {
		var sc marqueeScreen
		var raw string
		if err := rows.Scan(&sc.ID, &sc.Name, &raw, &sc.SeenMs, &sc.Width, &sc.Height); err != nil {
			return out, err
		}
		sc.Settings = shared
		if raw != "" {
			if own := s.screenOwnSettings(ctx, sc.ID, raw, shared); len(own) > 0 {
				sc.Settings, sc.Own, sc.OwnKeys = withOwn(shared, own), true, ownKeys(own)
			}
		}
		sc.Online = s.marqueeLive.connected(sc.ID) || now.Sub(time.UnixMilli(sc.SeenMs)) < marqueeOnlineFor
		if sh, ok := s.marqueeLive.nowShowing(sc.ID); ok && sc.Online {
			sc.Showing = &sh
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}

// PUT /api/home/marquee/screens/order (admin) {"order": [ids]}: the screens
// in this order; screens not named keep their place after them.
func (s *Server) handleMarqueeScreenOrder(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Order []string `json:"order"`
	}
	if err := decodeJSON(r, &body); err != nil || len(body.Order) == 0 {
		writeError(w, http.StatusBadRequest, "Send the screens in their new order.")
		return
	}
	ctx := r.Context()
	list, err := s.marqueeScreens(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The screens couldn't be listed.")
		return
	}
	known := map[string]bool{}
	for _, sc := range list {
		known[sc.ID] = true
	}
	order := make([]string, 0, len(list))
	placed := map[string]bool{}
	for _, id := range body.Order {
		id = strings.ToLower(strings.TrimSpace(id))
		if known[id] && !placed[id] {
			order, placed[id] = append(order, id), true
		}
	}
	for _, sc := range list {
		if !placed[sc.ID] {
			order = append(order, sc.ID)
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The order couldn't be saved.")
		return
	}
	defer tx.Rollback()
	for i, id := range order {
		if _, err := tx.ExecContext(ctx, `UPDATE marquee_screens SET position = ? WHERE id = ?`, i, id); err != nil {
			writeError(w, http.StatusInternalServerError, "The order couldn't be saved.")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "The order couldn't be saved.")
		return
	}
	s.handleMarqueeScreens(w, r)
}

// GET /api/home/marquee/screens (admin): every screen, with what it shows.
func (s *Server) handleMarqueeScreens(w http.ResponseWriter, r *http.Request) {
	list, err := s.marqueeScreens(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The screens couldn't be listed.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"screens": list, "shared": s.marqueeSettings(), "defaults": defaultMarquee(), "profiles": s.streamNZBProfileNames()})
}

// PUT /api/home/marquee/screens/{id} (admin): a new name, and/or settings of
// its own ("settings": null goes back to the shared ones).
func (s *Server) handlePutMarqueeScreen(w http.ResponseWriter, r *http.Request) {
	id := strings.ToLower(r.PathValue("id"))
	var body struct {
		Name     *string         `json:"name"`
		Settings json.RawMessage `json:"settings"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	ctx := r.Context()
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT 1 FROM marquee_screens WHERE id = ?`, id).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That screen isn't known.")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "The screen couldn't be saved.")
		return
	}
	if body.Name != nil {
		name := trimRunes(strings.TrimSpace(*body.Name), 40)
		if name == "" {
			writeError(w, http.StatusBadRequest, "Give the screen a name.")
			return
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE marquee_screens SET name = ? WHERE id = ?`, name, id); err != nil {
			writeError(w, http.StatusInternalServerError, "The screen couldn't be saved.")
			return
		}
	}
	if len(body.Settings) > 0 {
		// Only what differs from the shared settings is kept for the screen;
		// the rest follows them ("settings": null keeps nothing of its own).
		var own screenOwn
		if string(body.Settings) != "null" {
			var m marqueeSettings
			if err := json.Unmarshal(body.Settings, &m); err != nil {
				writeError(w, http.StatusBadRequest, "Those settings couldn't be read.")
				return
			}
			own = diffOwn(s.marqueeSettings(), m)
		}
		if _, err := s.saveScreenOwn(ctx, id, own); err != nil {
			writeError(w, http.StatusInternalServerError, "The screen couldn't be saved.")
			return
		}
		s.marqueeLive.send(id, marqueeEvent{Type: "settings"})
	}
	s.marqueeMQTTReannounce(r.Context(), id)
	s.handleMarqueeScreens(w, r)
}

// DELETE /api/home/marquee/screens/{id} (admin): forget a screen. If it is
// still on, it comes back as a new one the next time it asks.
func (s *Server) handleDeleteMarqueeScreen(w http.ResponseWriter, r *http.Request) {
	id := strings.ToLower(r.PathValue("id"))
	if list, err := s.marqueeScreens(r.Context()); err == nil {
		for _, sc := range list {
			if sc.ID == id {
				s.marqueeMQTTForget(sc)
			}
		}
	}
	if _, err := s.db.ExecContext(r.Context(), `DELETE FROM marquee_screens WHERE id = ?`, id); err != nil {
		writeError(w, http.StatusInternalServerError, "The screen couldn't be removed.")
		return
	}
	s.marqueeLive.forget(id)
	s.handleMarqueeScreens(w, r)
}
