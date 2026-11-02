package discord

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/rdborg/mediarium/internal/community"
	"github.com/rdborg/mediarium/internal/netguard"
)

// Conn is a websocket connection (gorilla's, or one in a test).
type Conn interface {
	ReadMessage() (int, []byte, error)
	WriteMessage(kind int, data []byte) error
	Close() error
}

// DialFunc opens a websocket connection.
type DialFunc func(ctx context.Context, url string) (Conn, error)

// defaultDial connects to the public internet only: Discord's gateway is
// never on the home network.
func defaultDial(ctx context.Context, url string) (Conn, error) {
	d := websocket.Dialer{
		HandshakeTimeout: 15 * time.Second,
		NetDialContext:   (&net.Dialer{Timeout: 15 * time.Second, Control: netguard.ControlPublic}).DialContext,
	}
	c, _, err := d.DialContext(ctx, url, nil)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// The gateway's opcodes.
const (
	opDispatch       = 0
	opHeartbeat      = 1
	opIdentify       = 2
	opResume         = 6
	opReconnect      = 7
	opInvalidSession = 9
	opHello          = 10
	opHeartbeatAck   = 11
)

type frame struct {
	Op int             `json:"op"`
	D  json.RawMessage `json:"d"`
	S  *int64          `json:"s"`
	T  string          `json:"t"`
}

const noContentText = "Message Content is off for this bot, so Usherr can't read what people write (chat and #review). Switch it on in the Developer Portal: your application, then Bot, then Message Content Intent."

// run keeps the bot connected until ctx ends or the connection fails for good.
func (b *Bot) run(ctx context.Context, token string) {
	wait := time.Second
	for ctx.Err() == nil {
		ready, err := b.connect(ctx, token)
		b.mu.Lock()
		fatal := b.fatal
		b.mu.Unlock()
		if fatal || ctx.Err() != nil {
			break
		}
		if ready {
			wait = time.Second
		}
		if err != nil {
			b.setState("connecting", "Reconnecting to Discord: "+err.Error())
		}
		t := time.NewTimer(wait + time.Duration(rand.Int64N(int64(wait))))
		select {
		case <-ctx.Done():
			t.Stop()
		case <-t.C:
		}
		wait = min(wait*2, 2*time.Minute)
	}
	b.mu.Lock()
	fatal := b.fatal
	b.mu.Unlock()
	if !fatal {
		b.setState("off", "")
	}
}

// gatewayURL is where to connect: where Discord said to resume, or where it
// says bots connect.
func (b *Bot) gatewayURL(ctx context.Context) (string, error) {
	b.mu.Lock()
	resume := b.resumeAt
	if b.session == "" {
		resume = ""
	}
	b.mu.Unlock()
	if resume != "" {
		return resume, nil
	}
	if b.gateway != "" {
		return b.gateway, nil
	}
	var out struct {
		URL string `json:"url"`
	}
	if err := b.rest.do(ctx, "GET", "/gateway/bot", nil, &out); err != nil {
		return "", err
	}
	if !strings.HasPrefix(out.URL, "wss://") {
		return "", errors.New("Discord gave no gateway address")
	}
	return out.URL, nil
}

// connect is one connection, from hello to whatever ends it. ready says it
// got as far as being connected.
func (b *Bot) connect(ctx context.Context, token string) (ready bool, err error) {
	url, err := b.gatewayURL(ctx)
	if err != nil {
		return false, err
	}
	sep := "?"
	if strings.Contains(url, "?") {
		sep = "&"
	}
	conn, err := b.dial(ctx, strings.TrimRight(url, "/")+"/"+sep+"v=10&encoding=json")
	if err != nil {
		return false, err
	}
	defer conn.Close()
	stopClosing := context.AfterFunc(ctx, func() { conn.Close() })
	defer stopClosing()

	var wmu sync.Mutex
	send := func(op int, d any) error {
		data, err := json.Marshal(map[string]any{"op": op, "d": d})
		if err != nil {
			return err
		}
		wmu.Lock()
		defer wmu.Unlock()
		return conn.WriteMessage(websocket.TextMessage, data)
	}

	read := func() (frame, error) {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return frame{}, err
		}
		var f frame
		if err := json.Unmarshal(data, &f); err != nil {
			return frame{}, fmt.Errorf("an unreadable message from Discord: %w", err)
		}
		if f.S != nil {
			b.mu.Lock()
			b.seq = *f.S
			b.mu.Unlock()
		}
		return f, nil
	}

	hello, err := read()
	if err != nil {
		return false, b.closed(err)
	}
	var h struct {
		Interval float64 `json:"heartbeat_interval"`
	}
	if hello.Op != opHello || json.Unmarshal(hello.D, &h) != nil || h.Interval <= 0 {
		return false, errors.New("Discord didn't say hello")
	}
	interval := time.Duration(h.Interval * float64(time.Millisecond))

	var acked sync.Mutex
	ack := true
	beat := func() error {
		b.mu.Lock()
		seq := b.seq
		b.mu.Unlock()
		var d any
		if seq > 0 {
			d = seq
		}
		return send(opHeartbeat, d)
	}
	beatCtx, stopBeating := context.WithCancel(ctx)
	defer stopBeating()
	go func() {
		// The first beat comes at a random point in the first interval, as
		// Discord asks, so many bots restarting at once don't beat together.
		t := time.NewTimer(time.Duration(rand.Float64() * float64(interval)))
		defer t.Stop()
		for {
			select {
			case <-beatCtx.Done():
				return
			case <-t.C:
			}
			acked.Lock()
			missed := !ack
			ack = false
			acked.Unlock()
			if missed {
				// No answer to the last beat: the connection is dead even
				// if it looks open. Close it and resume on a new one.
				conn.Close()
				return
			}
			if beat() != nil {
				return
			}
			t.Reset(interval)
		}
	}()

	b.mu.Lock()
	session, seq, intents := b.session, b.seq, b.intents
	b.mu.Unlock()
	if session != "" {
		err = send(opResume, map[string]any{"token": token, "session_id": session, "seq": seq})
	} else {
		err = send(opIdentify, map[string]any{
			"token":      token,
			"intents":    intents,
			"properties": map[string]string{"os": "linux", "browser": "usherr", "device": "usherr"},
		})
	}
	if err != nil {
		return false, b.closed(err)
	}

	for {
		f, err := read()
		if err != nil {
			return ready, b.closed(err)
		}
		switch f.Op {
		case opDispatch:
			if b.dispatch(f.T, f.D) {
				ready = true
			}
		case opHeartbeat:
			_ = beat()
		case opHeartbeatAck:
			acked.Lock()
			ack = true
			acked.Unlock()
		case opReconnect:
			return ready, errors.New("Discord asked Usherr to reconnect")
		case opInvalidSession:
			var resumable bool
			_ = json.Unmarshal(f.D, &resumable)
			if !resumable {
				b.forgetSession()
			}
			return ready, errors.New("Discord ended the session")
		}
	}
}

func (b *Bot) forgetSession() {
	b.mu.Lock()
	b.session, b.resumeAt, b.seq = "", "", 0
	b.mu.Unlock()
}

// closed makes sense of how a connection ended. Some close codes mean trying
// again can't help.
func (b *Bot) closed(err error) error {
	var ce *websocket.CloseError
	if !errors.As(err, &ce) {
		return err
	}
	switch ce.Code {
	case 4004:
		b.stopFor(refusedText)
	case 4014:
		// Message Content isn't switched on for the bot: connect without it.
		b.mu.Lock()
		b.intents &^= intentMessageContent
		b.mu.Unlock()
		b.forgetSession()
		b.warn(noContentText)
	case 4010, 4011, 4012, 4013:
		b.stopFor(fmt.Sprintf("Discord refused the connection (code %d: %s).", ce.Code, ce.Text))
	case 4007, 4009:
		b.forgetSession()
	}
	return fmt.Errorf("Discord closed the connection (code %d)", ce.Code)
}

// dispatch handles an event; it reports whether the bot is now connected.
func (b *Bot) dispatch(kind string, d json.RawMessage) (ready bool) {
	switch kind {
	case "READY":
		var r struct {
			Session  string `json:"session_id"`
			ResumeAt string `json:"resume_gateway_url"`
			User     struct {
				ID       string `json:"id"`
				Username string `json:"username"`
				Global   string `json:"global_name"`
			} `json:"user"`
			App struct {
				ID string `json:"id"`
			} `json:"application"`
		}
		if json.Unmarshal(d, &r) != nil {
			return false
		}
		name := r.User.Global
		if name == "" {
			name = r.User.Username
		}
		b.mu.Lock()
		b.session, b.resumeAt = r.Session, r.ResumeAt
		b.status.Name, b.status.ID, b.status.AppID = name, r.User.ID, r.App.ID
		b.servers = map[string]community.Server{}
		b.mu.Unlock()
		b.setState("connected", "")
		b.afterReady(r.App.ID)
		return true
	case "RESUMED":
		b.setState("connected", "")
		return true
	case "GUILD_CREATE", "GUILD_UPDATE":
		var g struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Icon        string `json:"icon"`
			Unavailable bool   `json:"unavailable"`
		}
		if json.Unmarshal(d, &g) == nil && g.ID != "" && !g.Unavailable {
			s := community.Server{ID: g.ID, Name: g.Name}
			if g.Icon != "" {
				s.Icon = "https://cdn.discordapp.com/icons/" + g.ID + "/" + g.Icon + ".png?size=64"
			}
			b.mu.Lock()
			b.servers[g.ID] = s
			b.mu.Unlock()
		}
	case "INTERACTION_CREATE":
		b.interacted(d)
		return false
	case "MESSAGE_CREATE", "MESSAGE_UPDATE", "MESSAGE_DELETE":
		b.messageEvent(kind, d)
	case "GUILD_DELETE":
		var g struct {
			ID          string `json:"id"`
			Unavailable bool   `json:"unavailable"`
		}
		if json.Unmarshal(d, &g) == nil && !g.Unavailable {
			b.mu.Lock()
			delete(b.servers, g.ID)
			b.mu.Unlock()
		}
	}
	if b.onEvent != nil {
		b.onEvent(kind, d)
	}
	return false
}
