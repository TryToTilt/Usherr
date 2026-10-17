package api

// Usherr: the Marquee's settings (Settings > Extras > Marquee). One JSON
// value, read by the tablet with every refresh, so a change shows there
// within a couple of minutes without touching it.

import (
	"encoding/json"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
)

const keyMarquee = "home.marquee" // JSON marqueeSettings

// The scenes, in their default order. "new", "theaters-film" and "coming-film"
// are one film at a time; "arrivals", "theaters" and "coming" are lists.
var marqueeSceneKinds = []string{"playing", "new", "trailer", "theaters-film", "arrivals", "coming-film", "theaters", "coming", "tvnow"}

var marqueeSceneSeconds = map[string]int{"playing": 20, "new": 16, "trailer": 75, "theaters-film": 16, "arrivals": 18, "coming-film": 16, "theaters": 18, "coming": 22, "tvnow": 18}

type marqueeScene struct {
	Kind    string `json:"kind"`
	On      bool   `json:"on"`
	Seconds int    `json:"seconds"`
}

type marqueeSettings struct {
	Scenes       []marqueeScene `json:"scenes"`
	ShowIcon     bool           `json:"showIcon"`
	ShowName     bool           `json:"showName"`
	ShowClock    bool           `json:"showClock"`
	SignMode     string         `json:"signMode"` // "auto" (the scene's name) or "custom"
	SignLine1    string         `json:"signLine1"`
	SignLine2    string         `json:"signLine2"`
	TextScale    int            `json:"textScale"`   // percent, 70-140
	PosterScale  int            `json:"posterScale"` // percent, 60-120
	ScrollSpeed  string         `json:"scrollSpeed"` // "slow", "normal" or "fast"
	TrailerSound bool           `json:"trailerSound"`
	FromJellyfin bool           `json:"fromJellyfin"`
	FromStream   bool           `json:"fromStreamNZB"`
	Profiles     []string       `json:"profiles"` // StreamNZB profiles to show; empty is all
	QuietOn      bool           `json:"quietOn"`
	QuietFrom    string         `json:"quietFrom"` // "23:00"
	QuietTo      string         `json:"quietTo"`   // "14:00"
	QuietMode    string         `json:"quietMode"` // "clock" (a dim clock) or "black"
	Palette      string         `json:"palette"`   // "" follows the browser's choice
}

func defaultMarquee() marqueeSettings {
	m := marqueeSettings{ShowIcon: true, ShowName: true, ShowClock: true, SignMode: "auto", SignLine1: "Now Showing",
		TextScale: 100, PosterScale: 100, ScrollSpeed: "normal", FromJellyfin: true, FromStream: true,
		QuietFrom: "23:30", QuietTo: "14:00", QuietMode: "clock", Profiles: []string{}}
	for _, k := range marqueeSceneKinds {
		m.Scenes = append(m.Scenes, marqueeScene{Kind: k, On: true, Seconds: marqueeSceneSeconds[k]})
	}
	return m
}

var marqueeTimeRe = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

// clean keeps a settings value sensible: every scene once, known kinds only,
// numbers in range. Scenes missing from a saved list (a newer Usherr added
// one) are added at the end, switched off.
func (m marqueeSettings) clean() marqueeSettings {
	d := defaultMarquee()
	seen := map[string]bool{}
	var scenes []marqueeScene
	for _, sc := range m.Scenes {
		if _, ok := marqueeSceneSeconds[sc.Kind]; !ok || seen[sc.Kind] {
			continue
		}
		seen[sc.Kind] = true
		if sc.Seconds < 5 || sc.Seconds > 600 {
			sc.Seconds = marqueeSceneSeconds[sc.Kind]
		}
		scenes = append(scenes, sc)
	}
	for _, k := range marqueeSceneKinds {
		if !seen[k] {
			scenes = append(scenes, marqueeScene{Kind: k, On: len(m.Scenes) == 0, Seconds: marqueeSceneSeconds[k]})
		}
	}
	m.Scenes = scenes
	if m.SignMode != "custom" {
		m.SignMode = "auto"
	}
	m.SignLine1, m.SignLine2 = trimRunes(strings.TrimSpace(m.SignLine1), 40), trimRunes(strings.TrimSpace(m.SignLine2), 40)
	if m.TextScale < 70 || m.TextScale > 140 {
		m.TextScale = d.TextScale
	}
	if m.PosterScale < 60 || m.PosterScale > 120 {
		m.PosterScale = d.PosterScale
	}
	if m.ScrollSpeed != "slow" && m.ScrollSpeed != "fast" {
		m.ScrollSpeed = "normal"
	}
	profiles := []string{} // never null: the settings page lists it
	for _, p := range m.Profiles {
		if p = trimRunes(strings.TrimSpace(p), 60); p != "" {
			profiles = append(profiles, p)
		}
	}
	m.Profiles = profiles
	if !marqueeTimeRe.MatchString(m.QuietFrom) {
		m.QuietFrom = d.QuietFrom
	}
	if !marqueeTimeRe.MatchString(m.QuietTo) {
		m.QuietTo = d.QuietTo
	}
	if m.QuietMode != "black" {
		m.QuietMode = "clock"
	}
	if m.Palette != "" && !regexp.MustCompile(`^[a-z]{2,12}$`).MatchString(m.Palette) {
		m.Palette = ""
	}
	return m
}

func trimRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

func (s *Server) marqueeSettings() marqueeSettings {
	raw, _ := s.Settings.Get(keyMarquee)
	if raw == "" {
		return defaultMarquee()
	}
	var m marqueeSettings
	if json.Unmarshal([]byte(raw), &m) != nil {
		return defaultMarquee()
	}
	return m.clean()
}

// GET /api/home/marquee/settings (admin), with the StreamNZB profiles to choose from.
func (s *Server) handleGetMarqueeSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"settings": s.marqueeSettings(), "defaults": defaultMarquee(), "profiles": s.streamNZBProfileNames()})
}

// PUT /api/home/marquee/settings (admin): the whole settings value.
func (s *Server) handlePutMarqueeSettings(w http.ResponseWriter, r *http.Request) {
	var m marqueeSettings
	if err := decodeJSON(r, &m); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	m = m.clean()
	b, _ := json.Marshal(m)
	if err := s.Settings.Set(keyMarquee, string(b), false); err != nil {
		writeError(w, http.StatusInternalServerError, "The marquee settings couldn't be saved.")
		return
	}
	s.handleGetMarqueeSettings(w, r)
}

// streamNZBProfileNames lists StreamNZB's stream profiles from its config
// file (names only).
func (s *Server) streamNZBProfileNames() []string {
	names := []string{}
	b, err := os.ReadFile(streamNZBConfig)
	if err != nil {
		return names
	}
	var c struct {
		Streams map[string]json.RawMessage `json:"streams"`
	}
	if json.Unmarshal(b, &c) != nil {
		return names
	}
	for n := range c.Streams {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
