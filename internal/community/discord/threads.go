package discord

import (
	"context"
	"time"

	"github.com/rdborg/mediarium/internal/community"
)

// StartThread opens a public thread under a message the bot posted. It stays
// open a day after the last message in it.
func (b *Bot) StartThread(ctx context.Context, under community.Ref, name string) (string, error) {
	var out struct {
		ID string `json:"id"`
	}
	body := map[string]any{"name": trim(name, 100), "auto_archive_duration": 1440}
	if err := b.rest.do(ctx, "POST", "/channels/"+under.Channel+"/messages/"+under.ID+"/threads", body, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

// StartForumPost opens a post in a forum channel with its first message. It
// stays open a week after the last message in it, and opens again by itself
// when something is posted in it.
func (b *Bot) StartForumPost(ctx context.Context, forum, name string, first community.Post) (string, error) {
	var out struct {
		ID string `json:"id"`
	}
	body := map[string]any{"name": trim(name, 100), "auto_archive_duration": 10080, "message": toMessage(first)}
	if err := b.rest.do(ctx, "POST", "/channels/"+forum+"/threads", body, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

// AddToThread adds someone to a thread; Discord then tells them about what's
// said in it, as their own notification settings say.
func (b *Bot) AddToThread(ctx context.Context, thread, user string) error {
	return b.rest.do(ctx, "PUT", "/channels/"+thread+"/thread-members/"+user, nil, nil)
}

// event is a scheduled event as Discord's API wants it: an EXTERNAL one (it
// happens somewhere other than a voice channel), which starts and ends by
// itself at its times.
func event(e community.Event) map[string]any {
	end := e.End
	if !end.After(e.Start) {
		end = e.Start.Add(2 * time.Hour)
	}
	return map[string]any{
		"name":                 trim(e.Name, 100),
		"description":          trim(e.Description, 1000),
		"privacy_level":        2, // the server's members
		"entity_type":          3, // external
		"entity_metadata":      map[string]string{"location": trim(e.Location, 100)},
		"scheduled_start_time": e.Start.UTC().Format(time.RFC3339),
		"scheduled_end_time":   end.UTC().Format(time.RFC3339),
	}
}

// CreateEvent puts an event on a server's calendar.
func (b *Bot) CreateEvent(ctx context.Context, server string, e community.Event) (string, error) {
	var out struct {
		ID string `json:"id"`
	}
	if err := b.rest.do(ctx, "POST", "/guilds/"+server+"/scheduled-events", event(e), &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

// EditEvent changes an event's name, times and words.
func (b *Bot) EditEvent(ctx context.Context, server, id string, e community.Event) error {
	return b.rest.do(ctx, "PATCH", "/guilds/"+server+"/scheduled-events/"+id, event(e), nil)
}

// DeleteEvent takes an event off the calendar.
func (b *Bot) DeleteEvent(ctx context.Context, server, id string) error {
	return b.rest.do(ctx, "DELETE", "/guilds/"+server+"/scheduled-events/"+id, nil, nil)
}
