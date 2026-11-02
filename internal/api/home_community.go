package api

// Usherr: Community (preview), step 1 of the community plan. A household
// makes its own Discord bot, gives Usherr the bot's token and invites it to
// their server; Usherr keeps the bot connected (going out from the home, so
// nothing has to reach in) and posts where it's told. Watch parties, reviews
// and chat build on this. Which server and channel is a setting, never fixed,
// so the same bot can move to another server later.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/brand"
	"github.com/rdborg/mediarium/internal/community"
	"github.com/rdborg/mediarium/internal/community/discord"
	"github.com/rdborg/mediarium/internal/netguard"
)

const (
	keyCommunity      = "home.community"       // JSON communitySettings
	keyCommunityToken = "home.community.token" // the bot token, kept encrypted
)

type communitySettings struct {
	On      bool   `json:"on"`
	Server  string `json:"server"`  // the Discord server Usherr posts in
	Channel string `json:"channel"` // the channel there (watch parties)
	// Usherr: step 6. Where review cards go: a text channel, or a forum (a
	// post per film); "" for the watch parties' channel. Its kind is looked
	// up when it's chosen.
	ReviewsChannel string `json:"reviewsChannel"`
	ReviewsKind    string `json:"reviewsKind"` // "text" or "forum"
}

// discordID reports whether v looks like a Discord id (a snowflake).
func discordID(v string) bool {
	if len(v) < 15 || len(v) > 22 {
		return false
	}
	for _, r := range v {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func (c communitySettings) clean() communitySettings {
	c.Server, c.Channel = strings.TrimSpace(c.Server), strings.TrimSpace(c.Channel)
	if !discordID(c.Server) {
		c.Server, c.Channel, c.ReviewsChannel = "", "", ""
	}
	if !discordID(c.Channel) {
		c.Channel = ""
	}
	if !discordID(c.ReviewsChannel) {
		c.ReviewsChannel = ""
	}
	if c.ReviewsChannel == "" || c.ReviewsKind != "forum" {
		c.ReviewsKind = "text"
	}
	return c
}

func (s *Server) communitySettings() communitySettings {
	var c communitySettings
	if raw, _ := s.Settings.Get(keyCommunity); raw != "" {
		_ = json.Unmarshal([]byte(raw), &c)
	}
	return c.clean()
}

// communityLink is the running bot. The zero value is off.
type communityLink struct {
	mu  sync.Mutex
	bot *discord.Bot
	// dial and api replace Discord's addresses (tests).
	dial    discord.DialFunc
	api     string
	gateway string
}

// StartCommunity connects the bot when Community is switched on. The returned
// function disconnects.
func (s *Server) StartCommunity() (stop func()) {
	if s.cfg.PauseAutomation {
		return func() {}
	}
	s.restartCommunity()
	return s.stopCommunity
}

func (s *Server) stopCommunity() {
	l := &s.communityL
	l.mu.Lock()
	bot := l.bot
	l.bot = nil
	l.mu.Unlock()
	if bot != nil {
		bot.Close()
	}
}

// restartCommunity (re)connects with the saved token, or stays off.
func (s *Server) restartCommunity() {
	s.stopCommunity()
	set := s.communitySettings()
	token, _ := s.Settings.Get(keyCommunityToken)
	if !set.On || token == "" {
		return
	}
	l := &s.communityL
	l.mu.Lock()
	defer l.mu.Unlock()
	l.bot = discord.Start(discord.Options{
		Token:   token,
		Agent:   discord.Agent(brand.URL, s.version),
		API:     l.api,
		Gateway: l.gateway,
		Dial:    l.dial,
		Client:  s.communityClient(),
		// Usherr: step 2, /link (home_community_link.go)
		Commands:      communityCommands(),
		OnInteraction: s.communityInteraction,
		OnMessage:     s.communityMessage, // Usherr: step 5, the party chat (home_community_chat.go)
		OnEvent:       s.communityEvent,   // Usherr: step 6, new posts in the reviews forum
	})
}

func (s *Server) communityClient() *http.Client {
	if s.communityL.api != "" {
		return http.DefaultClient // a test's pretend Discord, on this machine
	}
	return netguard.PublicClient(30 * time.Second)
}

// community is the connected bot, or nil when Community is off.
func (s *Server) community() community.Backend {
	l := &s.communityL
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.bot == nil {
		return nil
	}
	return l.bot
}

func (s *Server) communityAnswer() map[string]any {
	set := s.communitySettings()
	token, _ := s.Settings.Get(keyCommunityToken)
	st := community.Status{State: "off", Servers: []community.Server{}}
	if b := s.community(); b != nil {
		st = b.Status()
	}
	if st.Servers == nil {
		st.Servers = []community.Server{}
	}
	return map[string]any{
		"settings":  set,
		"tokenSet":  token != "",
		"status":    st,
		"inviteUrl": discord.InviteURL(st.AppID),
		"linked":    s.communityLinked(),
	}
}

// GET /api/home/community (admin): the bot and where it posts.
func (s *Server) handleGetCommunity(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.communityAnswer())
}

type communityBody struct {
	communitySettings
	Token *string `json:"token"` // nil keeps the saved one; "" removes it
}

// PUT /api/home/community (admin): save, and connect (or not).
func (s *Server) handlePutCommunity(w http.ResponseWriter, r *http.Request) {
	var body communityBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	set := body.communitySettings.clean()
	before := s.communitySettings()
	// The reviews channel's kind (text or forum) comes from Discord, not the page.
	if set.ReviewsChannel != "" {
		set.ReviewsKind = before.ReviewsKind
		if set.ReviewsChannel != before.ReviewsChannel || set.Server != before.Server {
			b := s.community()
			if b == nil {
				writeError(w, http.StatusConflict, "Connect the bot first.")
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
			list, err := b.Channels(ctx, set.Server)
			cancel()
			if err != nil {
				writeError(w, http.StatusBadGateway, communityProblem(err, ""))
				return
			}
			set.ReviewsKind = ""
			for _, c := range list {
				if c.ID == set.ReviewsChannel && (c.Kind == "text" || c.Kind == "announcement" || c.Kind == "forum") {
					set.ReviewsKind = map[bool]string{true: "forum", false: "text"}[c.Kind == "forum"]
				}
			}
			if set.ReviewsKind == "" {
				writeError(w, http.StatusBadRequest, "Choose a text channel or a forum for reviews.")
				return
			}
		}
	}
	oldToken, _ := s.Settings.Get(keyCommunityToken)
	token := oldToken
	if body.Token != nil {
		token = discord.CleanToken(*body.Token)
		if token != "" && !discord.LooksLikeToken(token) {
			writeError(w, http.StatusBadRequest, "That doesn't look like a bot token. In the Developer Portal, open your application, then Bot, then Reset Token, and copy the whole token.")
			return
		}
	}
	if set.On && token == "" {
		writeError(w, http.StatusBadRequest, "Paste your bot's token first.")
		return
	}
	b, _ := json.Marshal(set)
	if err := s.Settings.Set(keyCommunity, string(b), false); err != nil {
		writeError(w, http.StatusInternalServerError, "The settings couldn't be saved.")
		return
	}
	if token != oldToken {
		if err := s.Settings.Set(keyCommunityToken, token, true); err != nil {
			writeError(w, http.StatusInternalServerError, "The settings couldn't be saved.")
			return
		}
	}
	// Only the switch and the token change the connection; choosing another
	// server or channel doesn't.
	if set.On != before.On || token != oldToken || (set.On && s.community() == nil) {
		s.restartCommunity()
		if set.On {
			s.waitCommunityConnected(r.Context(), 5*time.Second)
		}
	}
	writeJSON(w, http.StatusOK, s.communityAnswer())
}

// waitCommunityConnected waits a little for a new connection to settle, so the
// answer to a save already says whether the token works.
func (s *Server) waitCommunityConnected(ctx context.Context, most time.Duration) {
	deadline := time.Now().Add(most)
	for time.Now().Before(deadline) && ctx.Err() == nil {
		b := s.community()
		if b == nil {
			return
		}
		if st := b.Status().State; st == "connected" || st == "error" {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// GET /api/home/community/channels?server= (admin): where the bot can post.
func (s *Server) handleCommunityChannels(w http.ResponseWriter, r *http.Request) {
	b := s.community()
	server := strings.TrimSpace(r.URL.Query().Get("server"))
	if b == nil || b.Status().State != "connected" {
		writeError(w, http.StatusConflict, "Connect the bot first.")
		return
	}
	if !discordID(server) {
		writeError(w, http.StatusBadRequest, "Choose a server.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	list, err := b.Channels(ctx, server)
	if err != nil {
		writeError(w, http.StatusBadGateway, communityProblem(err, ""))
		return
	}
	out := []community.Channel{}
	for _, c := range list {
		if c.Kind == "text" || c.Kind == "announcement" || c.Kind == "forum" || c.Kind == "category" {
			out = append(out, c)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"channels": out})
}

// communityProblem says in plain words why something couldn't be done in
// Discord.
func communityProblem(err error, where string) string {
	if where == "" {
		where = "there"
	}
	switch {
	case errors.Is(err, community.ErrBadToken):
		return "Discord didn't accept the bot token. In the Developer Portal, open your application, then Bot, then Reset Token, and paste the new token here."
	case errors.Is(err, community.ErrNotAllowed):
		return "The bot isn't allowed to post " + where + ". In Discord, open the channel's settings, then Permissions, and let the bot (or its role) View Channel, Send Messages and Embed Links."
	case errors.Is(err, community.ErrNotFound):
		return "That channel is gone, or the bot can't see it. Choose the channel again."
	case errors.Is(err, context.DeadlineExceeded):
		return "Discord took too long to answer. Try again in a minute."
	}
	return "Discord said no: " + err.Error()
}

// POST /api/home/community/test (admin): post a hello in the chosen channel.
func (s *Server) handleTestCommunity(w http.ResponseWriter, r *http.Request) {
	b := s.community()
	set := s.communitySettings()
	switch {
	case b == nil || b.Status().State != "connected":
		writeError(w, http.StatusConflict, "Connect the bot first.")
		return
	case set.Channel == "":
		writeError(w, http.StatusBadRequest, "Choose a channel first.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	_, err := b.Post(ctx, set.Channel, community.Post{Card: &community.Card{
		Title:       brand.Name + " is connected",
		Description: "Watch parties and reviews from " + brand.Name + " will appear in this channel.",
		Color:       0xF2C14E,
		Footer:      "A test message from " + brand.Name + " Settings",
		Time:        time.Now(),
	}})
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": communityProblem(err, "in that channel")})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "Posted. Look in the channel in Discord."})
}
