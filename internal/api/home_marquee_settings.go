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
var marqueeSceneKinds = []string{"playing", "new", "trailer", "theaters-film", "arrivals", "coming-film", "theaters", "coming", "tvnow", "tonight", "spotlight", "trivia", "slides", "preshow", "scores", "sports-tonight"}

var marqueeSceneSeconds = map[string]int{"playing": 20, "new": 16, "trailer": 75, "theaters-film": 16, "arrivals": 18, "coming-film": 16, "theaters": 18, "coming": 22, "tvnow": 18,
	"tonight": 24, "spotlight": 18, "trivia": 14, "slides": 12, "preshow": 90, "scores": 20, "sports-tonight": 22}

// Scenes added in phase 3 that start switched on for a Marquee set up before
// them (the others need something of yours first).
var marqueePhase3On = map[string]bool{"tonight": true, "spotlight": true, "trivia": true, "slides": true}

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
	TrailerFit   string         `json:"trailerFit"` // Usherr: "letterbox", "poster" (zoomed to a poster's size) or "fill"
	FromJellyfin bool           `json:"fromJellyfin"`
	FromStream   bool           `json:"fromStreamNZB"`
	NoIMDb       bool           `json:"noImdb"`     // Usherr: don't ask IMDb for cinema formats (TMDB's notes only)
	HideUsherr   bool           `json:"hideUsherr"` // Usherr: leave out what Usherr itself plays (off, so it's shown)
	Profiles     []string       `json:"profiles"`   // StreamNZB profiles to show; empty is all
	QuietOn      bool           `json:"quietOn"`
	QuietFrom    string         `json:"quietFrom"` // "23:00"
	QuietTo      string         `json:"quietTo"`   // "14:00"
	QuietMode    string         `json:"quietMode"` // "clock" (a dim clock) or "black"
	Palette      string         `json:"palette"`   // "" follows the browser's choice

	// The look (Marquee phase 2): a style, and what dresses it.
	Style        string `json:"style"`        // see marqueeStyles
	Theme        string `json:"theme"`        // see marqueeThemes
	Accent       string `json:"accent"`       // "#rrggbb", or "" for the theme's
	Background   string `json:"background"`   // "#rrggbb", or "" for the theme's
	Corners      string `json:"corners"`      // "square", "soft" or "round"
	Font         string `json:"font"`         // see marqueeFonts
	Frame        string `json:"frame"`        // "bulbs", "gold", "neon" or "none"
	BulbSize     int    `json:"bulbSize"`     // percent, 60-160
	Framing      string `json:"framing"`      // "fill", "letterbox" or "matted"
	Transition   string `json:"transition"`   // see marqueeTransitions
	TransitionMs int    `json:"transitionMs"` // 0-4000
	KenBurns     bool   `json:"kenBurns"`     // a slow pan and zoom over pictures
	Grain        bool   `json:"grain"`        // film grain
	Vintage      bool   `json:"vintage"`      // faded colour and a vignette
	Ambilight    bool   `json:"ambilight"`    // the poster's colours glow around it
	Footer       string `json:"footer"`       // "details", "title", "tagline" or "none"
	WallLayout   string `json:"wallLayout"`   // "grid" or "hero"
	WallColumns  int    `json:"wallColumns"`  // 2-8
	WallSwap     int    `json:"wallSwap"`     // seconds between tile swaps, 2-120
	PlayingFirst bool   `json:"playingFirst"` // while something plays, show only that
	Nudge        bool   `json:"nudge"`        // shift everything a few pixels now and then (burn-in)
	NightDimOn   bool   `json:"nightDimOn"`
	NightFrom    string `json:"nightFrom"`
	NightTo      string `json:"nightTo"`
	NightLevel   int    `json:"nightLevel"` // brightness at night, percent 15-90

	// What it shows (phase 3).
	Badges          string   `json:"badges"`          // "on" or "off": 4K, HDR, Dolby Vision, Atmos on what's playing
	InfoPanel       string   `json:"infoPanel"`       // "off", "always" or "held" (when paused or held by the remote)
	QR              string   `json:"qr"`              // "off", "trailer" or "usherr"
	PreShowOnStart  bool     `json:"preShowOnStart"`  // the pre-show when something starts playing
	PreShowBuiltins []string `json:"preShowBuiltins"` // the built-in pieces, see marqueeBuiltins
	PreShowTrailers int      `json:"preShowTrailers"` // trailers in the pre-show, 0-3
	ThemeMusic      bool     `json:"themeMusic"`      // a show's or film's theme.mp3, while it is on screen
	ThemeVolume     int      `json:"themeVolume"`     // percent, 5-100
	Version         int      `json:"version"`         // which settings it has seen (3: phase 3)

	// Usherr: Sports (the "scores" and "sports-tonight" scenes, and these).
	SportsScores    bool     `json:"sportsScores"`    // show scores (off: nothing that gives a result away)
	SportsTicker    bool     `json:"sportsTicker"`    // a ticker of the day's games along the bottom
	SportsCountdown bool     `json:"sportsCountdown"` // a band counting down to the next kick-off
	SportsTeamsOnly bool     `json:"sportsTeamsOnly"` // only games of the teams followed
	SportsLeagues   []string `json:"sportsLeagues"`   // the leagues to show, by name; empty is all
}

// The pre-show's built-in pieces, in the order they play.
var marqueeBuiltins = []string{"popcorn", "silence", "coming", "countdown", "feature"}

// The looks a screen can wear.
var (
	marqueeStyles      = []string{"usherr", "cinema", "wall", "screensaver", "board", "posterr", "onesheet"}
	marqueeThemes      = []string{"usherr", "deco", "noir", "neon", "ivory", "midnight", "drivein", "velvet"}
	marqueeFonts       = []string{"classic", "deco", "neon", "poster", "roman", "retro", "bold", "condensed", "elegant", "modern"}
	marqueeTransitions = []string{"fade", "slide", "push", "zoom", "flip", "rotate", "blur", "cut", "random"}
	marqueeColorRe     = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
)

// oneOf keeps v when it is one of the choices, else the fallback.
func oneOf(v string, choices []string, fallback string) string {
	for _, c := range choices {
		if v == c {
			return v
		}
	}
	return fallback
}

func marqueeClamp(v, lo, hi, fallback int) int {
	if v < lo || v > hi {
		return fallback
	}
	return v
}

func defaultMarquee() marqueeSettings {
	m := marqueeSettings{ShowIcon: true, ShowName: true, ShowClock: true, SignMode: "auto", SignLine1: "Now Showing",
		TextScale: 100, PosterScale: 100, ScrollSpeed: "normal", FromJellyfin: true, FromStream: true,
		QuietFrom: "23:30", QuietTo: "14:00", QuietMode: "clock", Profiles: []string{}, SportsLeagues: []string{},
		Style: "usherr", Theme: "usherr", Corners: "soft", Font: "classic", Frame: "bulbs", BulbSize: 100, Framing: "fill",
		Transition: "fade", TransitionMs: 900, Footer: "details", WallLayout: "grid", WallColumns: 3, WallSwap: 6,
		NightFrom: "23:00", NightTo: "07:00", NightLevel: 40,
		Badges: "on", InfoPanel: "held", QR: "off", PreShowBuiltins: append([]string(nil), marqueeBuiltins...), PreShowTrailers: 1, ThemeVolume: 40, Version: 3}
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
			scenes = append(scenes, marqueeScene{Kind: k, On: len(m.Scenes) == 0 || (m.Version < 3 && marqueePhase3On[k]), Seconds: marqueeSceneSeconds[k]})
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
	if m.TrailerFit != "poster" && m.TrailerFit != "fill" {
		m.TrailerFit = "letterbox"
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
	// The look. A value saved before these existed is empty or zero: the defaults.
	m.Style = oneOf(m.Style, marqueeStyles, d.Style)
	m.Theme = oneOf(m.Theme, marqueeThemes, d.Theme)
	if !marqueeColorRe.MatchString(m.Accent) {
		m.Accent = ""
	}
	if !marqueeColorRe.MatchString(m.Background) {
		m.Background = ""
	}
	m.Corners = oneOf(m.Corners, []string{"square", "soft", "round"}, d.Corners)
	m.Font = oneOf(m.Font, marqueeFonts, d.Font)
	m.Frame = oneOf(m.Frame, []string{"bulbs", "gold", "neon", "none"}, d.Frame)
	m.BulbSize = marqueeClamp(m.BulbSize, 60, 160, d.BulbSize)
	m.Framing = oneOf(m.Framing, []string{"fill", "letterbox", "matted"}, d.Framing)
	m.Transition = oneOf(m.Transition, marqueeTransitions, d.Transition)
	if m.Transition == "cut" {
		m.TransitionMs = 0
	} else if m.TransitionMs == 0 {
		m.TransitionMs = d.TransitionMs
	}
	m.TransitionMs = marqueeClamp(m.TransitionMs, 0, 4000, d.TransitionMs)
	m.Footer = oneOf(m.Footer, []string{"details", "title", "tagline", "none"}, d.Footer)
	m.WallLayout = oneOf(m.WallLayout, []string{"grid", "hero"}, d.WallLayout)
	m.WallColumns = marqueeClamp(m.WallColumns, 2, 8, d.WallColumns)
	m.WallSwap = marqueeClamp(m.WallSwap, 2, 120, d.WallSwap)
	if !marqueeTimeRe.MatchString(m.NightFrom) {
		m.NightFrom = d.NightFrom
	}
	if !marqueeTimeRe.MatchString(m.NightTo) {
		m.NightTo = d.NightTo
	}
	m.NightLevel = marqueeClamp(m.NightLevel, 15, 90, d.NightLevel)
	m.Badges = oneOf(m.Badges, []string{"on", "off"}, d.Badges)
	m.InfoPanel = oneOf(m.InfoPanel, []string{"off", "always", "held"}, d.InfoPanel)
	m.QR = oneOf(m.QR, []string{"off", "trailer", "usherr"}, d.QR)
	if m.PreShowBuiltins == nil {
		m.PreShowBuiltins = d.PreShowBuiltins
	}
	pieces := []string{}
	for _, b := range marqueeBuiltins { // known ones, once, in their order
		for _, x := range m.PreShowBuiltins {
			if x == b {
				pieces = append(pieces, b)
				break
			}
		}
	}
	m.PreShowBuiltins = pieces
	m.PreShowTrailers = marqueeClamp(m.PreShowTrailers, 0, 3, d.PreShowTrailers)
	m.ThemeVolume = marqueeClamp(m.ThemeVolume, 5, 100, d.ThemeVolume)
	leagues := []string{} // never null: the settings page lists it
	for _, l := range m.SportsLeagues {
		if l = trimRunes(strings.TrimSpace(l), 80); l != "" && len(leagues) < 50 {
			leagues = append(leagues, l)
		}
	}
	m.SportsLeagues = leagues
	m.Version = 3
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
	s.marqueeLive.send("", marqueeEvent{Type: "settings"}) // every screen: those with some of their own keep those
	s.marqueeMQTTState(r.Context(), "")                    // their look, in Home Assistant
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
