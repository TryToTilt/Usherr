package api

// Usherr (Marquee phase 4): Home Assistant through MQTT. When it is set up,
// Usherr connects to the broker (Mosquitto, or Home Assistant's own add-on)
// and each Marquee screen appears in Home Assistant by itself (MQTT
// discovery) as a device: buttons (next, previous, pre-show, reload),
// switches (pause, pin, dim, blank, info), a look and a scene to choose,
// what it shows, whether it is on, and the poster as a picture.

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/mqtt"
)

const (
	keyMarqueeMQTT         = "home.marquee.mqtt"          // JSON marqueeMQTTSettings
	keyMarqueeMQTTPassword = "home.marquee.mqtt.password" // kept encrypted
)

type marqueeMQTTSettings struct {
	On        bool   `json:"on"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	TLS       bool   `json:"tls"`
	Username  string `json:"username"`
	Prefix    string `json:"prefix"`    // Usherr's topics: "usherr"
	Discovery string `json:"discovery"` // Home Assistant's: "homeassistant"
}

func (m marqueeMQTTSettings) clean() marqueeMQTTSettings {
	m.Host = strings.TrimSpace(m.Host)
	m.Host = strings.TrimPrefix(strings.TrimPrefix(m.Host, "mqtt://"), "tcp://")
	if m.Port <= 0 || m.Port > 65535 {
		m.Port = 1883
		if m.TLS {
			m.Port = 8883
		}
	}
	m.Username = trimRunes(strings.TrimSpace(m.Username), 100)
	topic := func(v, d string) string {
		v = strings.Trim(strings.TrimSpace(v), "/")
		if v == "" || strings.ContainsAny(v, "+# ") {
			return d
		}
		return trimRunes(v, 60)
	}
	m.Prefix, m.Discovery = topic(m.Prefix, "usherr"), topic(m.Discovery, "homeassistant")
	return m
}

func (s *Server) marqueeMQTTSettings() marqueeMQTTSettings {
	var m marqueeMQTTSettings
	if raw, _ := s.Settings.Get(keyMarqueeMQTT); raw != "" {
		_ = json.Unmarshal([]byte(raw), &m)
	}
	return m.clean()
}

// marqueeBridge is the connection to the broker. The zero value is off.
type marqueeBridge struct {
	mu        sync.Mutex
	client    *mqtt.Client
	cancel    context.CancelFunc
	prefix    string
	discovery string
	state     string // "off", "connecting", "connected" or "error"
	err       string
	since     time.Time
	announced map[string]bool // screens Home Assistant was told about
	// sending is held while a screen's state is read and sent, so a state
	// read before a change can't go out after the one read after it (Home
	// Assistant would be left showing the old look).
	sending sync.Mutex
}

func (b *marqueeBridge) setState(state, err string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state != state || b.err != err {
		b.since = time.Now()
	}
	b.state, b.err = state, err
}

// StartMarqueeMQTT connects to the broker when that is set up. The returned
// function disconnects.
func (s *Server) StartMarqueeMQTT() (stop func()) {
	if s.cfg.PauseAutomation {
		return func() {}
	}
	s.restartMarqueeMQTT()
	return s.stopMarqueeMQTT
}

func (s *Server) stopMarqueeMQTT() {
	b := &s.marqueeMQTT
	b.mu.Lock()
	cancel := b.cancel
	b.cancel = nil
	b.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// restartMarqueeMQTT (re)connects with the saved settings, or stays off.
func (s *Server) restartMarqueeMQTT() {
	s.stopMarqueeMQTT()
	set := s.marqueeMQTTSettings()
	b := &s.marqueeMQTT
	if !set.On || set.Host == "" {
		b.setState("off", "")
		return
	}
	password, _ := s.Settings.Get(keyMarqueeMQTTPassword)
	ctx, cancel := context.WithCancel(context.Background())
	b.mu.Lock()
	b.cancel = cancel
	b.prefix, b.discovery = set.Prefix, set.Discovery
	b.announced = map[string]bool{}
	b.mu.Unlock()
	go s.runMarqueeMQTT(ctx, set, password)
}

func marqueeMQTTOptions(set marqueeMQTTSettings, password string) mqtt.Options {
	return mqtt.Options{
		Addr: net.JoinHostPort(set.Host, strconv.Itoa(set.Port)), TLS: set.TLS, Username: set.Username, Password: password,
		ClientID: "usherr-marquee", KeepAlive: 30 * time.Second,
		Will: &mqtt.Message{Topic: set.Prefix + "/marquee/status", Payload: []byte("offline"), Retain: true},
	}
}

func (s *Server) runMarqueeMQTT(ctx context.Context, set marqueeMQTTSettings, password string) {
	b := &s.marqueeMQTT
	wait := 5 * time.Second
	for ctx.Err() == nil {
		b.setState("connecting", "")
		c, err := mqtt.Dial(ctx, marqueeMQTTOptions(set, password), s.marqueeMQTTMessage)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			b.setState("error", err.Error())
			slog.Debug("marquee mqtt: connect", "err", err)
			select {
			case <-ctx.Done():
			case <-time.After(wait):
			}
			wait = min(wait*2, 2*time.Minute)
			continue
		}
		wait = 5 * time.Second
		b.mu.Lock()
		b.client = c
		b.announced = map[string]bool{}
		b.mu.Unlock()
		b.setState("connected", "")
		_ = c.Publish(set.Prefix+"/marquee/status", []byte("online"), true)
		_ = c.Subscribe(set.Prefix+"/marquee/+/cmd/+", set.Prefix+"/marquee/+/set/+")
		s.marqueeMQTTAnnounceAll(ctx)
		select {
		case <-ctx.Done():
			_ = c.Publish(set.Prefix+"/marquee/status", []byte("offline"), true)
			c.Close()
		case <-c.Done():
			msg := "the connection was lost"
			if e := c.Err(); e != nil {
				msg += ": " + e.Error()
			}
			b.setState("error", msg)
		}
		b.mu.Lock()
		b.client = nil
		b.mu.Unlock()
	}
	b.setState("off", "")
}

// marqueeMQTTPub publishes when connected; otherwise nothing.
func (s *Server) marqueeMQTTPub(topic string, payload []byte, retain bool) {
	b := &s.marqueeMQTT
	b.mu.Lock()
	c := b.client
	b.mu.Unlock()
	if c != nil {
		_ = c.Publish(topic, payload, retain)
	}
}

func (s *Server) marqueeMQTTTopics() (prefix, discovery string, on bool) {
	b := &s.marqueeMQTT
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.prefix, b.discovery, b.client != nil
}

var marqueeMQTTButtons = []struct{ cmd, name, icon string }{
	{"next", "Next", "mdi:skip-next"}, {"prev", "Previous", "mdi:skip-previous"}, {"preshow", "Start the pre-show", "mdi:filmstrip"}, {"reload", "Reload", "mdi:refresh"},
}

var marqueeMQTTSwitches = []struct{ key, name, icon string }{
	{"paused", "Pause", "mdi:pause"}, {"pinned", "Pin", "mdi:pin"}, {"dim", "Dim", "mdi:brightness-4"}, {"blank", "Blank", "mdi:monitor-off"}, {"info", "Info panel", "mdi:information-outline"},
}

// The switches' buttons are named a little differently from their states.
var marqueeSwitchCmd = map[string]string{"paused": "pause", "pinned": "pin", "dim": "dim", "blank": "blank", "info": "info"}

// marqueeDiscovery is what Home Assistant is told about a screen: a config
// topic for each of its entities.
func marqueeDiscovery(sc marqueeScreen, prefix, disc string) map[string]map[string]any {
	base := prefix + "/marquee/" + sc.ID
	dev := map[string]any{"identifiers": []string{"usherr_marquee_" + sc.ID}, "name": "Marquee " + sc.Name, "manufacturer": "Usherr", "model": "Marquee screen"}
	avail := []map[string]string{{"topic": prefix + "/marquee/status"}}
	out := map[string]map[string]any{}
	add := func(component, obj, name, icon string, extra map[string]any) {
		cfg := map[string]any{"name": name, "unique_id": "usherr_marquee_" + sc.ID + "_" + obj, "object_id": "marquee_" + sc.ID + "_" + obj, "device": dev, "availability": avail}
		if icon != "" {
			cfg["icon"] = icon
		}
		for k, v := range extra {
			cfg[k] = v
		}
		out[disc+"/"+component+"/usherr_marquee_"+sc.ID+"/"+obj+"/config"] = cfg
	}
	for _, b := range marqueeMQTTButtons {
		add("button", b.cmd, b.name, b.icon, map[string]any{"command_topic": base + "/cmd/" + b.cmd, "payload_press": "PRESS"})
	}
	for _, sw := range marqueeMQTTSwitches {
		add("switch", sw.key, sw.name, sw.icon, map[string]any{
			"command_topic": base + "/set/" + marqueeSwitchCmd[sw.key], "state_topic": base + "/state",
			"value_template": "{{ 'ON' if value_json." + sw.key + " else 'OFF' }}", "payload_on": "ON", "payload_off": "OFF",
		})
	}
	add("select", "look", "Look", "mdi:palette", map[string]any{"command_topic": base + "/set/look", "state_topic": base + "/state", "value_template": "{{ value_json.look }}", "options": marqueeStyles})
	add("select", "scene", "Show now", "mdi:movie-roll", map[string]any{"command_topic": base + "/cmd/scene", "options": marqueeSceneKinds, "optimistic": true})
	add("sensor", "showing", "Showing", "mdi:movie-open", map[string]any{"state_topic": base + "/state", "value_template": "{{ value_json.title }}", "json_attributes_topic": base + "/state"})
	add("binary_sensor", "online", "On", "", map[string]any{"state_topic": base + "/state", "value_template": "{{ 'ON' if value_json.online else 'OFF' }}", "device_class": "connectivity"})
	add("image", "poster", "Poster", "", map[string]any{"url_topic": base + "/poster"})
	return out
}

// marqueeMQTTAnnounce tells Home Assistant about a screen, and its state.
func (s *Server) marqueeMQTTAnnounce(sc marqueeScreen) {
	prefix, disc, on := s.marqueeMQTTTopics()
	if !on {
		return
	}
	for topic, cfg := range marqueeDiscovery(sc, prefix, disc) {
		payload, _ := json.Marshal(cfg)
		s.marqueeMQTTPub(topic, payload, true)
	}
	b := &s.marqueeMQTT
	b.mu.Lock()
	b.announced[sc.ID] = true
	b.mu.Unlock()
	s.marqueeMQTTPublishState(sc, prefix)
}

// marqueeMQTTForget takes a screen out of Home Assistant.
func (s *Server) marqueeMQTTForget(sc marqueeScreen) {
	prefix, disc, on := s.marqueeMQTTTopics()
	if !on {
		return
	}
	for topic := range marqueeDiscovery(sc, prefix, disc) {
		s.marqueeMQTTPub(topic, nil, true)
	}
	s.marqueeMQTTPub(prefix+"/marquee/"+sc.ID+"/state", nil, true)
	s.marqueeMQTTPub(prefix+"/marquee/"+sc.ID+"/poster", nil, true)
}

func (s *Server) marqueeMQTTAnnounceAll(ctx context.Context) {
	s.marqueeMQTT.sending.Lock()
	defer s.marqueeMQTT.sending.Unlock()
	list, err := s.marqueeScreens(ctx)
	if err != nil {
		return
	}
	for _, sc := range list {
		s.marqueeMQTTAnnounce(sc)
	}
}

type marqueeMQTTStateMsg struct {
	Online bool   `json:"online"`
	Name   string `json:"name"`
	Look   string `json:"look"`
	Scene  string `json:"scene"`
	Title  string `json:"title"`
	Poster string `json:"poster"`
	Paused bool   `json:"paused"`
	Pinned bool   `json:"pinned"`
	Dim    bool   `json:"dim"`
	Blank  bool   `json:"blank"`
	Info   bool   `json:"info"`
	Quiet  bool   `json:"quiet"`
}

func (s *Server) marqueeMQTTPublishState(sc marqueeScreen, prefix string) {
	st := marqueeMQTTStateMsg{Online: sc.Online, Name: sc.Name, Look: sc.Settings.Style}
	if sh := sc.Showing; sh != nil {
		st.Scene, st.Title, st.Paused, st.Pinned, st.Dim, st.Blank, st.Info, st.Quiet = sh.Scene, sh.Title, sh.Paused, sh.Pinned, sh.Dim, sh.Blank, sh.Info, sh.Quiet
		if strings.HasPrefix(sh.PosterURL, "https://") {
			st.Poster = sh.PosterURL
		}
	}
	payload, _ := json.Marshal(st)
	s.marqueeMQTTPub(prefix+"/marquee/"+sc.ID+"/state", payload, true)
	s.marqueeMQTTPub(prefix+"/marquee/"+sc.ID+"/poster", []byte(st.Poster), true)
}

// marqueeMQTTState sends a screen's state to Home Assistant (announcing a
// screen it hasn't heard of first). Nothing happens when not connected.
func (s *Server) marqueeMQTTState(ctx context.Context, id string) {
	prefix, _, on := s.marqueeMQTTTopics()
	if !on {
		return
	}
	s.marqueeMQTT.sending.Lock()
	defer s.marqueeMQTT.sending.Unlock()
	list, err := s.marqueeScreens(ctx)
	if err != nil {
		return
	}
	for _, sc := range list {
		if id != "" && sc.ID != id {
			continue
		}
		b := &s.marqueeMQTT
		b.mu.Lock()
		known := b.announced[sc.ID]
		b.mu.Unlock()
		if !known {
			s.marqueeMQTTAnnounce(sc)
			continue
		}
		s.marqueeMQTTPublishState(sc, prefix)
	}
}

// marqueeMQTTMessage acts on what Home Assistant sends: a button pressed,
// a switch turned, a look chosen.
func (s *Server) marqueeMQTTMessage(m mqtt.Message) {
	prefix, _, _ := s.marqueeMQTTTopics()
	rest, ok := strings.CutPrefix(m.Topic, prefix+"/marquee/")
	if !ok {
		return
	}
	parts := strings.Split(rest, "/")
	if len(parts) != 3 || !marqueeScreenIDRe.MatchString(parts[0]) {
		return
	}
	id, kind, what := parts[0], parts[1], parts[2]
	payload := strings.TrimSpace(string(m.Payload))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var err error
	switch {
	case kind == "cmd" && what == "scene":
		_, err = s.marqueeDo([]string{id}, "scene", payload)
	case kind == "cmd":
		_, err = s.marqueeDo([]string{id}, what, "")
	case kind == "set" && what == "look":
		err = s.marqueeSetLook(ctx, id, strings.ToLower(payload))
	case kind == "set":
		_, err = s.marqueeDo([]string{id}, what, strings.ToLower(payload))
	}
	if err != nil {
		slog.Debug("marquee mqtt: command", "topic", m.Topic, "err", err)
	}
}

type marqueeMQTTBody struct {
	marqueeMQTTSettings
	Password *string `json:"password"` // nil keeps the saved one
}

func (s *Server) marqueeMQTTAnswer() map[string]any {
	b := &s.marqueeMQTT
	b.mu.Lock()
	state, errText, since := b.state, b.err, b.since
	b.mu.Unlock()
	if state == "" {
		state = "off"
	}
	pw, _ := s.Settings.Get(keyMarqueeMQTTPassword)
	status := map[string]any{"state": state, "error": errText}
	if !since.IsZero() {
		status["sinceMs"] = since.UnixMilli()
	}
	return map[string]any{"settings": s.marqueeMQTTSettings(), "passwordSet": pw != "", "status": status}
}

// GET /api/home/marquee/mqtt (admin): the Home Assistant connection.
func (s *Server) handleGetMarqueeMQTT(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.marqueeMQTTAnswer())
}

// PUT /api/home/marquee/mqtt (admin): save it, and connect (or not).
func (s *Server) handlePutMarqueeMQTT(w http.ResponseWriter, r *http.Request) {
	var body marqueeMQTTBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	set := body.marqueeMQTTSettings.clean()
	if set.On && set.Host == "" {
		writeError(w, http.StatusBadRequest, "Give the broker's address, such as 192.168.1.20 or homeassistant.local.")
		return
	}
	b, _ := json.Marshal(set)
	if err := s.Settings.Set(keyMarqueeMQTT, string(b), false); err != nil {
		writeError(w, http.StatusInternalServerError, "The settings couldn't be saved.")
		return
	}
	if body.Password != nil {
		if err := s.Settings.Set(keyMarqueeMQTTPassword, *body.Password, true); err != nil {
			writeError(w, http.StatusInternalServerError, "The settings couldn't be saved.")
			return
		}
	}
	s.restartMarqueeMQTT()
	writeJSON(w, http.StatusOK, s.marqueeMQTTAnswer())
}

// POST /api/home/marquee/mqtt/test (admin): try these settings once.
func (s *Server) handleTestMarqueeMQTT(w http.ResponseWriter, r *http.Request) {
	var body marqueeMQTTBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	set := body.marqueeMQTTSettings.clean()
	if set.Host == "" {
		writeError(w, http.StatusBadRequest, "Give the broker's address, such as 192.168.1.20 or homeassistant.local.")
		return
	}
	password := ""
	if body.Password != nil {
		password = *body.Password
	} else {
		password, _ = s.Settings.Get(keyMarqueeMQTTPassword)
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	o := marqueeMQTTOptions(set, password)
	o.ClientID = "usherr-marquee-test"
	o.Will = nil
	c, err := mqtt.Dial(ctx, o, nil)
	if err != nil {
		msg := err.Error()
		var nerr net.Error
		if errors.As(err, &nerr) || strings.Contains(msg, "refused") || strings.Contains(msg, "no such host") {
			msg = "Usherr couldn't reach the broker at " + o.Addr + " (" + msg + ")."
		} else {
			msg = "The broker answered, but " + msg + "."
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": msg})
		return
	}
	c.Close()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "Connected to the broker at " + o.Addr + "."})
}

// marqueeMQTTReannounce tells Home Assistant about a screen again (a new name).
func (s *Server) marqueeMQTTReannounce(ctx context.Context, id string) {
	b := &s.marqueeMQTT
	b.mu.Lock()
	if b.announced != nil {
		delete(b.announced, id)
	}
	b.mu.Unlock()
	s.marqueeMQTTState(ctx, id)
}
