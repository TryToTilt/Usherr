package discord

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/community"
)

// dPicture is a picture in an embed. proxy_url is Discord's own copy (a GIF
// from Tenor, say, comes from Discord rather than Tenor).
type dPicture struct {
	URL   string `json:"url"`
	Proxy string `json:"proxy_url"`
}

func (p *dPicture) src() string {
	if p.Proxy != "" {
		return p.Proxy
	}
	return p.URL
}

// dMessage is a message as Discord sends it.
type dMessage struct {
	ID        string `json:"id"`
	Channel   string `json:"channel_id"`
	Content   string `json:"content"`
	Timestamp string `json:"timestamp"`
	Edited    string `json:"edited_timestamp"`
	WebhookID string `json:"webhook_id"`
	Author    *dUser `json:"author"`
	Member    *struct {
		Nick string `json:"nick"`
	} `json:"member"`
	Attachments []struct {
		URL         string `json:"url"`
		ContentType string `json:"content_type"`
	} `json:"attachments"`
	Embeds []struct {
		Type      string    `json:"type"`
		Image     *dPicture `json:"image"`
		Thumbnail *dPicture `json:"thumbnail"`
	} `json:"embeds"`
}

func (m dMessage) message(botID string) community.Message {
	out := community.Message{ID: m.ID, Channel: m.Channel, Text: m.Content, Edited: m.Edited != ""}
	if t, err := time.Parse(time.RFC3339, m.Timestamp); err == nil {
		out.At = t
	}
	if m.Author != nil {
		out.User = m.Author.user()
		if m.Member != nil && m.Member.Nick != "" {
			out.User.Name = m.Member.Nick
		}
		out.FromBot = m.Author.ID == botID && m.WebhookID == ""
	}
	for _, a := range m.Attachments {
		if strings.HasPrefix(a.ContentType, "image/") {
			out.Images = append(out.Images, a.URL)
		}
	}
	for _, e := range m.Embeds {
		// A GIF or picture link Discord shows as a picture.
		if (e.Type == "gifv" || e.Type == "image") && e.Thumbnail != nil {
			out.Images = append(out.Images, e.Thumbnail.src())
		} else if e.Image != nil && e.Type != "rich" {
			out.Images = append(out.Images, e.Image.src())
		}
	}
	return out
}

// messageEvent reads MESSAGE_CREATE, MESSAGE_UPDATE and MESSAGE_DELETE.
func (b *Bot) messageEvent(kind string, d json.RawMessage) {
	if b.onMessage == nil {
		return
	}
	var m dMessage
	if json.Unmarshal(d, &m) != nil || m.ID == "" {
		return
	}
	b.mu.Lock()
	botID := b.status.ID
	b.mu.Unlock()
	ev := community.MessageEvent{Message: m.message(botID)}
	switch kind {
	case "MESSAGE_CREATE":
		ev.Kind = "new"
	case "MESSAGE_UPDATE":
		if m.Author == nil { // only an embed was added: nothing a person changed
			return
		}
		ev.Kind = "edit"
	default:
		ev.Kind = "delete"
	}
	b.onMessage(ev)
}

// History reads messages after one, oldest first.
func (b *Bot) History(ctx context.Context, channel, after string, limit int) ([]community.Message, error) {
	q := url.Values{"limit": {strconv.Itoa(min(max(limit, 1), 100))}}
	if after != "" {
		q.Set("after", after)
	}
	var raw []dMessage
	if err := b.rest.do(ctx, "GET", "/channels/"+channel+"/messages?"+q.Encode(), nil, &raw); err != nil {
		return nil, err
	}
	b.mu.Lock()
	botID := b.status.ID
	b.mu.Unlock()
	out := make([]community.Message, 0, len(raw))
	for _, m := range raw {
		out = append(out, m.message(botID))
	}
	sort.Slice(out, func(i, j int) bool { return SnowflakeLess(out[i].ID, out[j].ID) })
	return out, nil
}

// SnowflakeLess orders Discord ids (numbers too big for some tools, sent as
// text): a shorter one is older.
func SnowflakeLess(a, b string) bool {
	if len(a) != len(b) {
		return len(a) < len(b)
	}
	return a < b
}

// webhooks are the bot's own webhook in each channel (one each), found or
// made the first time someone posts from Usherr.
type webhooks struct {
	mu   sync.Mutex
	hook map[string][2]string // channel → id, token
}

// webhook finds the bot's webhook in a channel, or makes one.
func (b *Bot) webhook(ctx context.Context, channel string) (id, token string, err error) {
	b.hooks.mu.Lock()
	defer b.hooks.mu.Unlock()
	if h, ok := b.hooks.hook[channel]; ok {
		return h[0], h[1], nil
	}
	b.mu.Lock()
	appID := b.status.AppID
	b.mu.Unlock()
	var list []struct {
		ID    string `json:"id"`
		Type  int    `json:"type"`
		Token string `json:"token"`
		App   string `json:"application_id"`
	}
	if err := b.rest.do(ctx, "GET", "/channels/"+channel+"/webhooks", nil, &list); err != nil {
		return "", "", err
	}
	for _, h := range list {
		if h.Type == 1 && h.Token != "" && h.App != "" && h.App == appID {
			id, token = h.ID, h.Token
		}
	}
	if id == "" {
		var made struct {
			ID    string `json:"id"`
			Token string `json:"token"`
		}
		if err := b.rest.do(ctx, "POST", "/channels/"+channel+"/webhooks", map[string]string{"name": "Usherr chat"}, &made); err != nil {
			return "", "", err
		}
		id, token = made.ID, made.Token
	}
	if b.hooks.hook == nil {
		b.hooks.hook = map[string][2]string{}
	}
	b.hooks.hook[channel] = [2]string{id, token}
	return id, token, nil
}

// PostAs posts in a thread through the channel's webhook, under the person's
// own name and picture. Nobody is pinged.
func (b *Bot) PostAs(ctx context.Context, channel, thread string, as community.User, text string) (community.Ref, error) {
	id, token, err := b.webhook(ctx, channel)
	if err != nil {
		return community.Ref{}, err
	}
	body := map[string]any{
		"content":          trim(text, 2000),
		"username":         trim(as.Name, 80),
		"allowed_mentions": map[string]any{"parse": []string{}},
	}
	if strings.HasPrefix(as.Avatar, "https://") {
		body["avatar_url"] = as.Avatar
	}
	q := url.Values{"wait": {"true"}}
	if thread != "" {
		q.Set("thread_id", thread)
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := b.rest.do(ctx, "POST", "/webhooks/"+id+"/"+token+"?"+q.Encode(), body, &out); err != nil {
		if errors.Is(err, community.ErrNotFound) {
			// Someone deleted the webhook in Discord: make a new one next time.
			b.hooks.mu.Lock()
			delete(b.hooks.hook, channel)
			b.hooks.mu.Unlock()
		}
		return community.Ref{}, err
	}
	where := thread
	if where == "" {
		where = channel
	}
	return community.Ref{Channel: where, ID: out.ID}, nil
}
