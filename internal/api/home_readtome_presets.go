package api

// Usherr: Read-to-Me presets, and Kokoro voice mixes. A preset is a named way
// of reading a book (the engine, the voice, the speed, the pauses, whether the
// title and the chapter titles are read), shared by the household; one can be
// the default the dialog opens with. A mix is two or three Kokoro voices
// blended by weight, written as Kokoro takes it: "af_heart(7)+af_bella(3)".

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ---- mixes ----

type mixPart struct {
	ID     string
	Weight int
}

var mixPartRe = regexp.MustCompile(`^([a-z]{2}_[a-z0-9]+)(?:\(([1-9])\))?$`)

// parseMix reads "a(7)+b(3)" (weights 1 to 9, 1 when left out); ok is false
// for a voice that isn't a mix, or a mix that isn't well made.
func parseMix(voice string) ([]mixPart, bool) {
	if !strings.Contains(voice, "+") {
		return nil, false
	}
	parts := strings.Split(voice, "+")
	if len(parts) < 2 || len(parts) > 3 {
		return nil, false
	}
	var out []mixPart
	seen := map[string]bool{}
	for _, p := range parts {
		m := mixPartRe.FindStringSubmatch(p)
		if m == nil || seen[m[1]] {
			return nil, false
		}
		seen[m[1]] = true
		w := 1
		if m[2] != "" {
			w, _ = strconv.Atoi(m[2])
		}
		out = append(out, mixPart{ID: m[1], Weight: w})
	}
	return out, true
}

// voiceName is how a voice is named on an audiobook: "Heart", or for a mix
// "Heart 70% + Bella 30%".
func voiceName(vs []rtvVoice, id string) string {
	name := func(id string) string {
		for _, v := range vs {
			if v.ID == id {
				return strings.SplitN(v.Label, " · ", 2)[0]
			}
		}
		return id
	}
	parts, ok := parseMix(id)
	if !ok {
		return name(id)
	}
	total := 0
	for _, p := range parts {
		total += p.Weight
	}
	var names []string
	for _, p := range parts {
		names = append(names, fmt.Sprintf("%s %d%%", name(p.ID), (p.Weight*100+total/2)/total))
	}
	return strings.Join(names, " + ")
}

// ---- presets ----

type rtvPreset struct {
	ID      int64      `json:"id"`
	Name    string     `json:"name"`
	Engine  string     `json:"engine"`
	Voice   string     `json:"voice"`
	Speed   float64    `json:"speed"`
	Options rtvOptions `json:"options"`
	Default bool       `json:"default"`
}

func (s *Server) presets(ctx context.Context) []rtvPreset {
	out := []rtvPreset{}
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, engine, voice, speed, options, is_default FROM readtome_presets ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var p rtvPreset
		var opts string
		if rows.Scan(&p.ID, &p.Name, &p.Engine, &p.Voice, &p.Speed, &opts, &p.Default) == nil {
			p.Options = parseOptions(opts)
			out = append(out, p)
		}
	}
	return out
}

func (s *Server) handleReadToMePresets(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"presets": s.presets(r.Context())})
}

// checkPreset checks a preset as sent: a name, a voice that exists, a speed
// from 0.8 to 1.3. The chapters are a book's, never a preset's.
func (s *Server) checkPreset(r *http.Request, p *rtvPreset) error {
	p.Name = strings.Join(strings.Fields(p.Name), " ")
	if p.Name == "" || len([]rune(p.Name)) > 40 {
		return errors.New("A preset needs a name, up to 40 letters.")
	}
	if p.Speed == 0 {
		p.Speed = 1
	}
	if p.Speed < 0.8 || p.Speed > 1.3 {
		return errors.New("The speed is from 0.8 to 1.3.")
	}
	p.Options.Chapters = nil
	p.Options.Cast = nil // a cast is a book's
	p.Options = p.Options.clamped()
	_, err := s.voiceExists(r.Context(), p.Engine, p.Voice)
	return err
}

// POST /api/home/readtome/presets {name, engine, voice, speed, options, default}:
// a new preset, or the one with that name saved again.
func (s *Server) handleSaveReadToMePreset(w http.ResponseWriter, r *http.Request) {
	var p rtvPreset
	if err := decodeJSON(r, &p); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	if err := s.checkPreset(r, &p); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	opts, _ := json.Marshal(p.Options)
	_, err := s.db.ExecContext(r.Context(), `INSERT INTO readtome_presets (name, engine, voice, speed, options, user_id, created_ms) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET engine = excluded.engine, voice = excluded.voice, speed = excluded.speed, options = excluded.options`,
		p.Name, p.Engine, p.Voice, p.Speed, string(opts), requestUserID(r), time.Now().UnixMilli())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if p.Default {
		s.setDefaultPreset(r.Context(), p.Name)
	}
	writeJSON(w, http.StatusOK, map[string]any{"presets": s.presets(r.Context())})
}

func (s *Server) setDefaultPreset(ctx context.Context, name string) {
	_, _ = s.db.ExecContext(ctx, `UPDATE readtome_presets SET is_default = CASE WHEN name = ? COLLATE NOCASE THEN 1 ELSE 0 END`, name)
}

// PUT /api/home/readtome/presets/{id} {default}: makes it the default (or none).
func (s *Server) handleReadToMePresetDefault(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		Default bool `json:"default"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	var name string
	if err := s.db.QueryRowContext(r.Context(), `SELECT name FROM readtome_presets WHERE id = ?`, id).Scan(&name); err != nil {
		writeError(w, http.StatusNotFound, "That preset isn't there.")
		return
	}
	if req.Default {
		s.setDefaultPreset(r.Context(), name)
	} else {
		_, _ = s.db.ExecContext(r.Context(), `UPDATE readtome_presets SET is_default = 0 WHERE id = ?`, id)
	}
	writeJSON(w, http.StatusOK, map[string]any{"presets": s.presets(r.Context())})
}

func (s *Server) handleDeleteReadToMePreset(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	_, _ = s.db.ExecContext(r.Context(), `DELETE FROM readtome_presets WHERE id = ?`, id)
	writeJSON(w, http.StatusOK, map[string]any{"presets": s.presets(r.Context())})
}
