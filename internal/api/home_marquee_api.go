package api

// Usherr (Marquee phase 4): steering the screens from anything, the phone
// remote, scripts with an API key and Home Assistant alike. A screen can be
// named by its id or its name ("tablet"), and "all" is every screen.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

var (
	errMarqueeNoScreen = errors.New("there is no screen by that name")
	errMarqueeOff      = errors.New("that screen isn't on right now")
	errMarqueeButton   = errors.New("that isn't a Marquee button")
	errMarqueeOnOff    = errors.New("say on or off")
	errMarqueeScene    = errors.New("that isn't a Marquee scene")
	errMarqueeLook     = errors.New("that isn't a Marquee look")
)

// marqueeResolve finds the screens a reference means: an id, a name (any
// case), or "all".
func (s *Server) marqueeResolve(ctx context.Context, ref string) ([]string, error) {
	ref = strings.TrimSpace(ref)
	list, err := s.marqueeScreens(ctx)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, sc := range list {
		if strings.EqualFold(ref, "all") || sc.ID == strings.ToLower(ref) {
			ids = append(ids, sc.ID)
		}
	}
	if len(ids) == 0 {
		for _, sc := range list {
			if strings.EqualFold(sc.Name, ref) {
				ids = append(ids, sc.ID)
			}
		}
	}
	if len(ids) == 0 {
		return nil, errMarqueeNoScreen
	}
	return ids, nil
}

// marqueeCheck says whether a button and its argument make sense, and
// cleans the argument.
func marqueeCheck(cmd, arg string) (string, error) {
	if !marqueeCommands[cmd] {
		return "", errMarqueeButton
	}
	arg = trimRunes(strings.ToLower(strings.TrimSpace(arg)), 30)
	switch cmd {
	case "pause", "pin", "dim", "blank", "info":
		if arg != "on" && arg != "off" {
			return "", errMarqueeOnOff
		}
	case "scene":
		if _, ok := marqueeSceneSeconds[arg]; !ok {
			return "", errMarqueeScene
		}
	default:
		arg = ""
	}
	return arg, nil
}

// marqueeDo presses a button on these screens; it says how many heard it.
func (s *Server) marqueeDo(ids []string, cmd, arg string) (int, error) {
	arg, err := marqueeCheck(cmd, arg)
	if err != nil {
		return 0, err
	}
	sent := 0
	for _, id := range ids {
		if s.marqueeLive.send(id, marqueeEvent{Type: "command", Cmd: cmd, Arg: arg}) > 0 {
			sent++
		}
	}
	if sent == 0 {
		return 0, errMarqueeOff
	}
	return sent, nil
}

// marqueeSetLook gives a screen another look. Only the look becomes its own
// (the shared look puts it back on the shared one); everything else still
// follows the shared settings.
func (s *Server) marqueeSetLook(ctx context.Context, id, style string) error {
	if oneOf(style, marqueeStyles, "") == "" {
		return errMarqueeLook
	}
	var raw string
	if err := s.db.QueryRowContext(ctx, `SELECT settings FROM marquee_screens WHERE id = ?`, id).Scan(&raw); err != nil {
		return errMarqueeNoScreen
	}
	shared := s.marqueeSettings()
	own := s.screenOwnSettings(ctx, id, raw, shared)
	if own == nil {
		own = screenOwn{}
	}
	if style == shared.Style {
		delete(own, "style")
	} else {
		v, _ := json.Marshal(style)
		own["style"] = v
	}
	if ok, err := s.saveScreenOwn(ctx, id, own); err != nil {
		return err
	} else if !ok {
		return errMarqueeNoScreen
	}
	s.marqueeLive.send(id, marqueeEvent{Type: "settings"})
	s.marqueeMQTTState(ctx, id)
	return nil
}

// marqueeAPIError answers with the right status for these errors.
func marqueeAPIError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errMarqueeNoScreen):
		writeError(w, http.StatusNotFound, "There is no screen by that name.")
	case errors.Is(err, errMarqueeOff):
		writeError(w, http.StatusConflict, "That screen isn't on right now.")
	case errors.Is(err, errMarqueeButton):
		writeError(w, http.StatusBadRequest, "That isn't a Marquee button.")
	case errors.Is(err, errMarqueeOnOff):
		writeError(w, http.StatusBadRequest, "Say on or off.")
	case errors.Is(err, errMarqueeScene):
		writeError(w, http.StatusBadRequest, "That isn't a Marquee scene.")
	case errors.Is(err, errMarqueeLook):
		writeError(w, http.StatusBadRequest, "That isn't a Marquee look.")
	default:
		writeError(w, http.StatusInternalServerError, "The screen couldn't be changed.")
	}
}

// POST /api/home/marquee/screens/{id}/command (admin): a remote button, for
// a screen by id or name, or "all".
func (s *Server) handleMarqueeCommand(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Cmd string `json:"cmd"`
		Arg string `json:"arg"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a Marquee button.")
		return
	}
	if _, err := marqueeCheck(body.Cmd, body.Arg); err != nil {
		marqueeAPIError(w, err)
		return
	}
	ids, err := s.marqueeResolve(r.Context(), r.PathValue("id"))
	if err != nil {
		// A screen not seen yet may still be listening (the remote's own id).
		if !errors.Is(err, errMarqueeNoScreen) {
			marqueeAPIError(w, err)
			return
		}
		ids = []string{strings.ToLower(r.PathValue("id"))}
	}
	n, err := s.marqueeDo(ids, body.Cmd, body.Arg)
	if err != nil {
		marqueeAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "screens": n})
}

// POST /api/home/marquee/screens/{id}/look (admin): {"style": "cinema"}.
func (s *Server) handleMarqueeLook(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Style string `json:"style"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	ids, err := s.marqueeResolve(r.Context(), r.PathValue("id"))
	if err != nil {
		marqueeAPIError(w, err)
		return
	}
	for _, id := range ids {
		if err := s.marqueeSetLook(r.Context(), id, strings.ToLower(strings.TrimSpace(body.Style))); err != nil {
			marqueeAPIError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "screens": len(ids)})
}

// GET /api/home/marquee/screens/{id}/poster (admin): the picture on that
// screen now (a redirect to it), for a Home Assistant card or a Stream Deck.
func (s *Server) handleMarqueePoster(w http.ResponseWriter, r *http.Request) {
	ids, err := s.marqueeResolve(r.Context(), r.PathValue("id"))
	if err != nil || len(ids) != 1 {
		http.NotFound(w, r)
		return
	}
	sh, ok := s.marqueeLive.nowShowing(ids[0])
	if !ok || sh.PosterURL == "" || sh.Blank {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, sh.PosterURL, http.StatusFound)
}
