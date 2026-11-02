package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/community"
)

// interaction reads an INTERACTION_CREATE: a slash command (type 2) or a
// button press (type 3). Anything else is left alone.
func interaction(d json.RawMessage) (community.Interaction, bool) {
	var raw struct {
		ID      string `json:"id"`
		Token   string `json:"token"`
		Type    int    `json:"type"`
		Guild   string `json:"guild_id"`
		Channel string `json:"channel_id"`
		Data    struct {
			Name     string `json:"name"`
			CustomID string `json:"custom_id"`
			Options  []struct {
				Name    string          `json:"name"`
				Value   json.RawMessage `json:"value"`
				Focused bool            `json:"focused"`
			} `json:"options"`
			// A form sent: its rows of text boxes.
			Components []struct {
				Components []struct {
					CustomID string `json:"custom_id"`
					Value    string `json:"value"`
				} `json:"components"`
			} `json:"components"`
		} `json:"data"`
		Member struct {
			Nick string `json:"nick"`
			User *dUser `json:"user"`
		} `json:"member"`
		User *dUser `json:"user"` // in a direct message, there's no member
	}
	if json.Unmarshal(d, &raw) != nil || raw.ID == "" || raw.Token == "" {
		return community.Interaction{}, false
	}
	in := community.Interaction{Server: raw.Guild, Channel: raw.Channel, ID: raw.ID, Token: raw.Token}
	switch raw.Type {
	case 2:
		in.Kind, in.Name = "command", raw.Data.Name
	case 3:
		in.Kind, in.Name = "button", raw.Data.CustomID
	case 4:
		in.Kind, in.Name = "suggest", raw.Data.Name
	case 5:
		in.Kind, in.Name = "form", raw.Data.CustomID
		in.Options = map[string]string{}
		for _, row := range raw.Data.Components {
			for _, c := range row.Components {
				in.Options[c.CustomID] = c.Value
			}
		}
	default:
		return community.Interaction{}, false
	}
	if len(raw.Data.Options) > 0 {
		in.Options = map[string]string{}
		for _, o := range raw.Data.Options {
			var v any
			_ = json.Unmarshal(o.Value, &v)
			switch x := v.(type) {
			case string:
				in.Options[o.Name] = x
			case float64:
				in.Options[o.Name] = strconv.FormatFloat(x, 'f', -1, 64)
			}
			if o.Focused {
				in.Focused = o.Name
			}
		}
	}
	u := raw.User
	if raw.Member.User != nil {
		u = raw.Member.User
	}
	if u == nil || u.ID == "" {
		return community.Interaction{}, false
	}
	in.User = u.user()
	if raw.Member.Nick != "" {
		in.User.Name = raw.Member.Nick
	}
	return in, true
}

// dUser is a Discord user as the API sends it.
type dUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Global   string `json:"global_name"`
	Avatar   string `json:"avatar"`
}

func (u dUser) user() community.User {
	out := community.User{ID: u.ID, Name: u.Global}
	if out.Name == "" {
		out.Name = u.Username
	}
	if u.Avatar != "" {
		out.Avatar = "https://cdn.discordapp.com/avatars/" + u.ID + "/" + u.Avatar + ".png?size=64"
	}
	return out
}

// Reply answers a command or button press. Discord gives three seconds.
func (b *Bot) Reply(ctx context.Context, in community.Interaction, text string, private bool) error {
	data := map[string]any{"content": trim(text, 2000), "allowed_mentions": map[string]any{"parse": []string{}}}
	if private {
		data["flags"] = 64 // ephemeral: only they see it
	}
	return b.rest.do(ctx, "POST", "/interactions/"+in.ID+"/"+in.Token+"/callback", map[string]any{"type": 4, "data": data}, nil)
}

// Defer answers for now: Discord shows "thinking…" until Followup.
func (b *Bot) Defer(ctx context.Context, in community.Interaction, private bool) error {
	data := map[string]any{}
	if private {
		data["flags"] = 64
	}
	return b.rest.do(ctx, "POST", "/interactions/"+in.ID+"/"+in.Token+"/callback", map[string]any{"type": 5, "data": data}, nil)
}

// Followup gives a deferred answer (it replaces "thinking…").
func (b *Bot) Followup(ctx context.Context, in community.Interaction, text string) error {
	b.mu.Lock()
	app := b.status.AppID
	b.mu.Unlock()
	return b.rest.do(ctx, "PATCH", "/webhooks/"+app+"/"+in.Token+"/messages/@original",
		map[string]any{"content": trim(text, 2000), "allowed_mentions": map[string]any{"parse": []string{}}}, nil)
}

// AskForm answers with a pop-up form of text boxes.
func (b *Bot) AskForm(ctx context.Context, in community.Interaction, f community.Form) error {
	rows := []map[string]any{}
	for _, box := range f.Boxes {
		style := 1 // one line
		if box.Long {
			style = 2
		}
		c := map[string]any{"type": 4, "custom_id": box.ID, "label": trim(box.Label, 45), "style": style, "required": box.Required}
		if box.Placeholder != "" {
			c["placeholder"] = trim(box.Placeholder, 100)
		}
		if box.Max > 0 {
			c["max_length"] = box.Max
		}
		rows = append(rows, map[string]any{"type": 1, "components": []any{c}})
	}
	return b.rest.do(ctx, "POST", "/interactions/"+in.ID+"/"+in.Token+"/callback",
		map[string]any{"type": 9, "data": map[string]any{"custom_id": trim(f.ID, 100), "title": trim(f.Title, 45), "components": rows}}, nil)
}

// Suggest answers an option being typed with picks (Discord shows up to 25).
func (b *Bot) Suggest(ctx context.Context, in community.Interaction, picks []community.Choice) error {
	choices := []map[string]string{}
	for i, c := range picks {
		if i == 25 {
			break
		}
		choices = append(choices, map[string]string{"name": trim(c.Name, 100), "value": trim(c.Value, 100)})
	}
	return b.rest.do(ctx, "POST", "/interactions/"+in.ID+"/"+in.Token+"/callback", map[string]any{"type": 8, "data": map[string]any{"choices": choices}}, nil)
}

// React puts an emoji (a plain one, like ⭐) under a message.
func (b *Bot) React(ctx context.Context, ref community.Ref, emoji string) error {
	return b.rest.do(ctx, "PUT", "/channels/"+ref.Channel+"/messages/"+ref.ID+"/reactions/"+url.PathEscape(emoji)+"/@me", nil, nil)
}

// commandJSON is a command as Discord's API wants it.
func commandJSON(c community.Command) map[string]any {
	opts := []map[string]any{}
	for _, o := range c.Options {
		kind := 3 // string
		if o.Kind == "integer" {
			kind = 4
		}
		m := map[string]any{"name": strings.ToLower(o.Name), "description": trim(o.Description, 100), "type": kind, "required": o.Required}
		if o.Autocomplete {
			m["autocomplete"] = true
		}
		if o.MaxLength > 0 {
			m["max_length"] = o.MaxLength
		}
		if len(o.Choices) > 0 {
			ch := []map[string]any{}
			for _, c := range o.Choices {
				if kind == 4 {
					n, _ := strconv.Atoi(c.Value)
					ch = append(ch, map[string]any{"name": c.Name, "value": n})
				} else {
					ch = append(ch, map[string]any{"name": c.Name, "value": c.Value})
				}
			}
			m["choices"] = ch
		}
		opts = append(opts, m)
	}
	return map[string]any{
		"name": strings.ToLower(c.Name), "description": trim(c.Description, 100), "type": 1, "options": opts,
		"contexts": []int{0}, "integration_types": []int{0}, // in servers only, as a server's bot
	}
}

// commandSig is what decides whether a command changed: its name,
// description and options (with their choices).
func commandSig(name, description string, options any) string {
	b, _ := json.Marshal(options)
	var norm []map[string]any
	_ = json.Unmarshal(b, &norm)
	keep := []string{}
	for _, o := range norm {
		// Only what Usherr sets: Discord's answer adds fields of its own.
		var choices []string
		list, _ := o["choices"].([]any)
		for _, c := range list {
			if m, ok := c.(map[string]any); ok {
				choices = append(choices, fmt.Sprintf("%v=%v", m["name"], m["value"]))
			}
		}
		keep = append(keep, fmt.Sprintf("%v|%v|%v|%v|%s", o["name"], o["description"], o["required"] == true, o["autocomplete"] == true, strings.Join(choices, ",")))
	}
	return name + "|" + description + "|" + strings.Join(keep, ";")
}

// syncCommands registers the bot's slash commands, only when Discord's list
// differs (Discord limits how often commands may be created).
func (b *Bot) syncCommands(ctx context.Context, appID string) {
	if appID == "" {
		return
	}
	var have []struct {
		Name        string           `json:"name"`
		Description string           `json:"description"`
		Options     []map[string]any `json:"options"`
	}
	if err := b.rest.do(ctx, "GET", "/applications/"+appID+"/commands", nil, &have); err != nil {
		slog.Warn("community: couldn't read the bot's commands from Discord", "err", err)
		return
	}
	list := make([]map[string]any, 0, len(b.cmds))
	want := map[string]string{}
	for _, c := range b.cmds {
		j := commandJSON(c)
		list = append(list, j)
		want[j["name"].(string)] = commandSig(j["name"].(string), j["description"].(string), j["options"])
	}
	same := len(have) == len(want)
	for _, c := range have {
		if want[c.Name] != commandSig(c.Name, c.Description, c.Options) {
			same = false
		}
	}
	if same {
		return
	}
	if err := b.rest.do(ctx, "PUT", "/applications/"+appID+"/commands", list, nil); err != nil {
		slog.Warn("community: couldn't register the bot's commands with Discord", "err", err)
	}
}

// interacted hands a command or button press on, on its own goroutine so the
// gateway keeps reading.
func (b *Bot) interacted(d json.RawMessage) {
	in, ok := interaction(d)
	if !ok || b.onInter == nil {
		return
	}
	go b.onInter(in)
}

// afterReady does what needs a connection, once connected.
func (b *Bot) afterReady(appID string) {
	if len(b.cmds) == 0 {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		b.syncCommands(ctx, appID)
	}()
}
