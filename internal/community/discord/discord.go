// Package discord is Usherr's own Discord bot: the household makes a bot in
// Discord's Developer Portal, gives Usherr its token and invites it to their
// server. Usherr then keeps one connection open to Discord's gateway (it goes
// out from the home, nothing has to reach in) to hear messages and button
// presses, and posts through Discord's HTTP API. No library: what Usherr
// needs is small, like its MQTT client.
package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/community"
)

// Intents say which events the gateway sends. Message Content is one Discord
// makes a bot owner switch on in the Developer Portal; without it the bot
// still works, but can't read what people write (chat and #review).
const (
	intentGuilds         = 1 << 0
	intentGuildMessages  = 1 << 9
	intentMessageContent = 1 << 15
	intentScheduled      = 1 << 16
)

// Permissions asks for, in the invite link: see channels, read their history,
// send messages and cards (with links and pictures), react, make and send in
// threads, manage those threads, post through webhooks (as the person who
// wrote it in Usherr), and make events.
const Permissions int64 = 1<<6 | 1<<10 | 1<<11 | 1<<14 | 1<<15 | 1<<16 | 1<<29 | 1<<33 | 1<<34 | 1<<35 | 1<<38 | 1<<44

// Options set up a Bot.
type Options struct {
	Token string
	Agent string // the User-Agent: "DiscordBot (https://…, 2.37.0)"
	// API and Gateway replace Discord's addresses (tests).
	API     string
	Gateway string
	Client  *http.Client
	Dial    DialFunc
	// OnEvent hears every event from the gateway that the bot doesn't handle
	// itself (MESSAGE_CREATE, …), with its raw data.
	OnEvent func(kind string, data json.RawMessage)
	// Commands are the slash commands the bot offers; they're registered with
	// Discord once connected, when they differ from what Discord has.
	Commands []community.Command
	// OnInteraction hears commands and button presses, each on its own
	// goroutine. It must answer (Reply) within three seconds.
	OnInteraction func(community.Interaction)
	// OnMessage hears messages said, changed and taken back, in order.
	OnMessage func(community.MessageEvent)
}

// Bot is a connection to Discord. Its methods may be called from any
// goroutine.
type Bot struct {
	rest      *rest
	gateway   string
	dial      DialFunc
	onEvent   func(string, json.RawMessage)
	onInter   func(community.Interaction)
	onMessage func(community.MessageEvent)
	hooks     webhooks
	cmds      []community.Command
	cancel    context.CancelFunc
	done      chan struct{}

	mu       sync.Mutex
	status   community.Status
	servers  map[string]community.Server
	intents  int
	session  string
	resumeAt string
	seq      int64
	fatal    bool
}

// Start connects, and keeps reconnecting until Close (or until Discord
// refuses the token).
func Start(o Options) *Bot {
	if o.API == "" {
		o.API = "https://discord.com/api/v10"
	}
	if o.Client == nil {
		o.Client = http.DefaultClient
	}
	if o.Dial == nil {
		o.Dial = defaultDial
	}
	token := CleanToken(o.Token)
	b := &Bot{
		gateway:   o.Gateway,
		dial:      o.Dial,
		onEvent:   o.OnEvent,
		onInter:   o.OnInteraction,
		onMessage: o.OnMessage,
		cmds:      o.Commands,
		done:      make(chan struct{}),
		servers:   map[string]community.Server{},
		intents:   intentGuilds | intentGuildMessages | intentMessageContent | intentScheduled,
	}
	b.rest = &rest{base: strings.TrimRight(o.API, "/"), token: token, agent: o.Agent, client: o.Client}
	b.rest.onRefused = func() { b.stopFor(refusedText) }
	b.status = community.Status{State: "connecting", Since: time.Now()}
	ctx, cancel := context.WithCancel(context.Background())
	b.cancel = cancel
	go func() {
		defer close(b.done)
		b.run(ctx, token)
	}()
	return b
}

const refusedText = "Discord didn't accept the bot token. In the Developer Portal, open your application, then Bot, then Reset Token, and paste the new token here."

// CleanToken takes away what people paste around a token by mistake.
func CleanToken(t string) string {
	t = strings.TrimSpace(t)
	t = strings.TrimPrefix(t, "Bot ")
	return strings.Trim(strings.TrimSpace(t), "\"'")
}

// LooksLikeToken reports whether t has the shape of a bot token (three parts
// joined by dots). It can't say whether Discord accepts it.
func LooksLikeToken(t string) bool {
	parts := strings.Split(CleanToken(t), ".")
	return len(parts) == 3 && len(parts[0]) >= 16 && parts[1] != "" && len(parts[2]) >= 20 && !strings.ContainsAny(t, " \t\n")
}

// InviteURL is the link that adds the bot to a server, asking for what it
// needs.
func InviteURL(appID string) string {
	if appID == "" {
		return ""
	}
	return "https://discord.com/oauth2/authorize?client_id=" + appID + "&scope=bot+applications.commands&permissions=" + strconv.FormatInt(Permissions, 10)
}

// Close disconnects and waits until the connection is gone.
func (b *Bot) Close() {
	b.cancel()
	<-b.done
}

// Status says how the connection is.
func (b *Bot) Status() community.Status {
	b.mu.Lock()
	defer b.mu.Unlock()
	st := b.status
	st.Servers = make([]community.Server, 0, len(b.servers))
	for _, s := range b.servers {
		st.Servers = append(st.Servers, s)
	}
	sort.Slice(st.Servers, func(i, j int) bool { return strings.ToLower(st.Servers[i].Name) < strings.ToLower(st.Servers[j].Name) })
	st.Warnings = append([]string(nil), b.status.Warnings...)
	return st
}

func (b *Bot) setState(state, err string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.status.State != state || b.status.Error != err {
		b.status.Since = time.Now()
	}
	b.status.State, b.status.Error = state, err
}

// stopFor ends the connection for good with this reason.
func (b *Bot) stopFor(reason string) {
	b.mu.Lock()
	b.fatal = true
	b.mu.Unlock()
	b.setState("error", reason)
	b.cancel()
}

func (b *Bot) warn(w string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, have := range b.status.Warnings {
		if have == w {
			return
		}
	}
	b.status.Warnings = append(b.status.Warnings, w)
}

// Channels lists a server's channels, in the order Discord shows them.
func (b *Bot) Channels(ctx context.Context, server string) ([]community.Channel, error) {
	var raw []struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Type     int    `json:"type"`
		Parent   string `json:"parent_id"`
		Position int    `json:"position"`
	}
	if err := b.rest.do(ctx, "GET", "/guilds/"+server+"/channels", nil, &raw); err != nil {
		return nil, err
	}
	kinds := map[int]string{0: "text", 2: "voice", 4: "category", 5: "announcement", 13: "stage", 15: "forum"}
	out := make([]community.Channel, 0, len(raw))
	for _, c := range raw {
		kind, ok := kinds[c.Type]
		if !ok {
			continue
		}
		out = append(out, community.Channel{ID: c.ID, Name: c.Name, Kind: kind, Parent: c.Parent, Position: c.Position})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Position < out[j].Position })
	return out, nil
}

// message is a post as Discord's API wants it.
type message struct {
	Content         string           `json:"content"`
	Embeds          []map[string]any `json:"embeds"`
	Components      []map[string]any `json:"components"`
	AllowedMentions map[string]any   `json:"allowed_mentions"`
}

// toMessage turns a Post into Discord's form. Nobody is ever pinged by what
// Usherr posts: text people typed could hold @everyone.
func toMessage(p community.Post) message {
	m := message{Content: trim(p.Text, 2000), Embeds: []map[string]any{}, Components: []map[string]any{},
		AllowedMentions: map[string]any{"parse": []string{}}}
	if c := p.Card; c != nil {
		e := map[string]any{}
		set := func(k, v string, n int) {
			if v = strings.TrimSpace(v); v != "" {
				e[k] = trim(v, n)
			}
		}
		set("title", c.Title, 256)
		set("description", c.Description, 4096)
		if strings.HasPrefix(c.URL, "https://") {
			e["url"] = c.URL
		}
		if c.Color > 0 {
			e["color"] = c.Color & 0xFFFFFF
		}
		if strings.HasPrefix(c.Image, "https://") {
			e["image"] = map[string]string{"url": c.Image}
		}
		if strings.HasPrefix(c.Thumbnail, "https://") {
			e["thumbnail"] = map[string]string{"url": c.Thumbnail}
		}
		if f := strings.TrimSpace(c.Footer); f != "" {
			e["footer"] = map[string]string{"text": trim(f, 2048)}
		}
		if !c.Time.IsZero() {
			e["timestamp"] = c.Time.UTC().Format(time.RFC3339)
		}
		fields := []map[string]any{}
		for i, f := range c.Fields {
			if i == 25 {
				break
			}
			fields = append(fields, map[string]any{"name": trim(f.Name, 256), "value": trim(f.Value, 1024), "inline": f.Inline})
		}
		if len(fields) > 0 {
			e["fields"] = fields
		}
		m.Embeds = append(m.Embeds, e)
	}
	var row []map[string]any
	styles := map[string]int{"primary": 1, "secondary": 2, "success": 3, "danger": 4}
	for _, btn := range p.Buttons {
		c := map[string]any{"type": 2, "label": trim(btn.Label, 80)}
		switch {
		case strings.HasPrefix(btn.URL, "https://"):
			c["style"], c["url"] = 5, btn.URL
		case btn.ID != "":
			style := styles[btn.Style]
			if style == 0 {
				style = 2
			}
			c["style"], c["custom_id"] = style, trim(btn.ID, 100)
		default:
			continue
		}
		row = append(row, c)
		if len(row) == 5 {
			m.Components = append(m.Components, map[string]any{"type": 1, "components": row})
			row = nil
		}
	}
	if len(row) > 0 {
		m.Components = append(m.Components, map[string]any{"type": 1, "components": row})
	}
	if len(m.Components) > 5 {
		m.Components = m.Components[:5]
	}
	return m
}

func trim(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// Post sends a message to a channel or thread.
func (b *Bot) Post(ctx context.Context, channel string, p community.Post) (community.Ref, error) {
	var out struct {
		ID string `json:"id"`
	}
	if err := b.rest.do(ctx, "POST", "/channels/"+channel+"/messages", toMessage(p), &out); err != nil {
		return community.Ref{}, err
	}
	return community.Ref{Channel: channel, ID: out.ID}, nil
}

// Edit replaces a message the bot posted.
func (b *Bot) Edit(ctx context.Context, ref community.Ref, p community.Post) error {
	return b.rest.do(ctx, "PATCH", "/channels/"+ref.Channel+"/messages/"+ref.ID, toMessage(p), nil)
}

// Delete removes a message.
func (b *Bot) Delete(ctx context.Context, ref community.Ref) error {
	return b.rest.do(ctx, "DELETE", "/channels/"+ref.Channel+"/messages/"+ref.ID, nil, nil)
}

// Agent is the User-Agent Discord asks bots to send.
func Agent(site, version string) string {
	return fmt.Sprintf("DiscordBot (%s, %s)", site, version)
}

var _ community.Backend = (*Bot)(nil)
