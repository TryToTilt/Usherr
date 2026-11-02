// Package community is what Usherr's community features (watch parties,
// reviews, chat) talk to. A Backend is one place the community lives: today
// a Discord server through Usherr's own bot (package discord), later perhaps
// Usherr's own community server. Usherr keeps its own copy of everything it
// posts, so a household can move to another backend, or another Discord
// server, and post its history again there.
package community

import (
	"context"
	"errors"
	"time"
)

// Backend is a connection to where the community lives.
type Backend interface {
	// Status says whether the connection is up, as whom, and where.
	Status() Status
	// Channels lists the channels of one server a post can go to.
	Channels(ctx context.Context, server string) ([]Channel, error)
	// Post sends a message to a channel (or a thread).
	Post(ctx context.Context, channel string, p Post) (Ref, error)
	// Edit replaces a message that was posted.
	Edit(ctx context.Context, ref Ref, p Post) error
	// Delete removes a message that was posted.
	Delete(ctx context.Context, ref Ref) error
	// Reply answers a command or a button press. A private reply is seen
	// only by the person who pressed or typed.
	Reply(ctx context.Context, in Interaction, text string, private bool) error
	// Defer answers a command for now ("thinking…"), when the answer takes
	// longer than the few seconds allowed; Followup then gives it.
	Defer(ctx context.Context, in Interaction, private bool) error
	Followup(ctx context.Context, in Interaction, text string) error
	// AskForm answers a command with a pop-up form; what's typed in it comes
	// back as a "form" interaction (Name: the form's ID, Options: its boxes).
	AskForm(ctx context.Context, in Interaction, f Form) error
	// Suggest answers a "suggest" interaction with up to 25 picks.
	Suggest(ctx context.Context, in Interaction, picks []Choice) error
	// React puts an emoji under a message.
	React(ctx context.Context, ref Ref, emoji string) error
	// StartThread opens a thread under a posted message; it returns the
	// thread's ID, which Post takes as a channel.
	StartThread(ctx context.Context, under Ref, name string) (string, error)
	// StartForumPost opens a post in a forum channel, its first message
	// first; it returns the post's ID, which Post takes as a channel.
	StartForumPost(ctx context.Context, forum, name string, first Post) (string, error)
	// AddToThread adds a person to a thread, so they're told what's said in it.
	AddToThread(ctx context.Context, thread, user string) error
	// CreateEvent puts an event on a server's calendar; it returns its ID.
	CreateEvent(ctx context.Context, server string, e Event) (string, error)
	// EditEvent changes an event; DeleteEvent cancels it.
	EditEvent(ctx context.Context, server, id string, e Event) error
	DeleteEvent(ctx context.Context, server, id string) error
	// History reads a channel's (or thread's) messages after the one with
	// the given ID ("" for the latest), oldest first.
	History(ctx context.Context, channel, after string, limit int) ([]Message, error)
	// PostAs posts text in a thread of a channel under someone's own name and
	// picture (what they typed in Usherr).
	PostAs(ctx context.Context, channel, thread string, as User, text string) (Ref, error)
	// Close disconnects.
	Close()
}

// User is a person in the community.
type User struct {
	ID     string `json:"id"`
	Name   string `json:"name"`             // as people see it
	Avatar string `json:"avatar,omitempty"` // a picture's address, when they have one
}

// Command is a command people type (/link, /review).
type Command struct {
	Name        string
	Description string
	Options     []Option
}

// Option is something a command asks for.
type Option struct {
	Name         string
	Description  string
	Kind         string // "string" or "integer"
	Required     bool
	Autocomplete bool     // suggestions while typing (Suggest answers them)
	Choices      []Choice // a fixed list to pick from
	MaxLength    int
}

// Choice is one pick for an option.
type Choice struct {
	Name  string
	Value string
}

// Form is a pop-up form of text boxes.
type Form struct {
	ID, Title string
	Boxes     []Box
}

// Box is a text box in a form.
type Box struct {
	ID, Label, Placeholder string
	Long                   bool // several lines
	Required               bool
	Max                    int
}

// Interaction is a command typed or a button pressed. It must be answered
// (Reply) within a few seconds.
type Interaction struct {
	Kind    string            // "command", "button", "suggest" (an option being typed) or "form" (a form sent)
	Name    string            // the command's name, or the button's ID
	Options map[string]string // a command's options, as typed or picked
	Focused string            // "suggest": the option being typed
	Server  string
	Channel string
	User    User
	ID      string // the backend's own, to answer it
	Token   string
}

// Status is the state of a connection.
type Status struct {
	State    string    `json:"state"`           // "off", "connecting", "connected" or "error"
	Error    string    `json:"error,omitempty"` // why it's "error", in plain words
	Since    time.Time `json:"since"`
	Name     string    `json:"name,omitempty"`     // the bot's name, as people see it
	ID       string    `json:"id,omitempty"`       // the bot's user id
	AppID    string    `json:"appId,omitempty"`    // the application the bot belongs to
	Servers  []Server  `json:"servers"`            // the servers the bot is in
	Warnings []string  `json:"warnings,omitempty"` // it works, but something is missing
}

// Server is a Discord server (a "guild") the bot is in.
type Server struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Icon string `json:"icon,omitempty"` // a picture's address, when it has one
}

// Channel is a channel in a server.
type Channel struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`             // "text", "forum", "announcement", "voice" or "category"
	Parent   string `json:"parent,omitempty"` // the category it's in
	Position int    `json:"position"`
}

// Post is a message: some text, a card, and buttons under it.
type Post struct {
	Text    string
	Card    *Card
	Buttons []Button
}

// Card is a rich card (Discord calls it an embed): a title, a picture, a few
// short facts.
type Card struct {
	Title       string
	URL         string // where the title leads
	Description string
	Color       int // the stripe down its side, as 0xRRGGBB
	Image       string
	Thumbnail   string
	Fields      []Field
	Footer      string
	Time        time.Time // shown in each reader's own time zone
}

// Field is one short fact on a card.
type Field struct {
	Name, Value string
	Inline      bool
}

// Button is a button under a post: one that tells Usherr it was pressed (ID),
// or one that opens an address (URL).
type Button struct {
	Label string
	ID    string
	URL   string
	Style string // "primary", "secondary", "success" or "danger"; a URL button is always a link
}

// Message is something said in a channel or thread.
type Message struct {
	ID      string    `json:"id"`
	Channel string    `json:"-"`
	User    User      `json:"user"`
	Text    string    `json:"text"`
	Images  []string  `json:"images,omitempty"` // pictures and GIFs in it
	At      time.Time `json:"at"`
	Edited  bool      `json:"edited,omitempty"`
	FromBot bool      `json:"fromBot,omitempty"` // the bot's own (the party's alerts)
}

// MessageEvent is a message said, changed (Kind "edit") or taken back
// ("delete", with only ID and Channel).
type MessageEvent struct {
	Kind string
	Message
}

// Event is something on a server's calendar (Discord: a scheduled event, with
// Interested and a reminder when it starts).
type Event struct {
	Name        string
	Description string
	Location    string // where it happens, in words
	Start, End  time.Time
}

// Ref says where a posted message is.
type Ref struct {
	Channel string `json:"channel"`
	ID      string `json:"id"`
}

var (
	// ErrBadToken: the backend no longer accepts Usherr's credentials. Nothing
	// more is sent until they are changed.
	ErrBadToken = errors.New("the bot token was not accepted")
	// ErrNotAllowed: the bot may not do that there (a permission is missing).
	ErrNotAllowed = errors.New("the bot isn't allowed to do that")
	// ErrNotFound: the channel or message is gone.
	ErrNotFound = errors.New("not found")
)
