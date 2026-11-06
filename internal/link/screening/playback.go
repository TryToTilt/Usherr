package screening

// Linked screenings, step 3: synced playback (docs/home/link-protocol.md,
// Playback). The host sends where the film is (playing, paused, a seek) in
// one message sealed with the screening key, which the relay fans out; each
// Usherr plays its own copy and follows. The host also sends the pre-show's
// recipe (the items in order and their lengths), so each Usherr builds the
// same pre-show from its own trailers and cards and the film starts at the
// same moment everywhere.
//
// Only the host's key may send playback: anything else is dropped (by the
// node, when it isn't the key's owner; here, when it isn't this
// screening's host). A guest may ask the host to pause (pause.request); the
// host honours it only when it lets anyone pause.

import (
	"context"
	"math"
	"regexp"
	"strings"

	"github.com/rdborg/mediarium/internal/link"
)

// The message types.
const (
	TypePlayback = "playback"          // host → everyone (screening key, fanout): where the film is
	TypePause    = "pause.request"     // guest → host: please pause (when the host lets anyone pause)
	TypeRecipe   = "screening.preshow" // host → everyone (screening key, fanout): the pre-show, item by item
)

// Playback states.
const (
	StatePreshow = "preshow" // the pre-show is on (position < 0)
	StateFilm    = "film"    // the film is playing
	StatePaused  = "paused"  // stopped at position
	StateEnded   = "ended"   // over
)

// Limits.
const (
	MaxBodyRecipe  = 12 << 10
	MaxRecipeItems = 80
	MaxItemSeconds = 600
)

// Playback is where the film is: at AtWallTime (unix ms by the relay's
// clock) it was at Position (seconds into the film; negative in the
// pre-show, -60 is a minute before the film), in State. Seq goes up with
// every change the host makes; the heartbeats between changes repeat it.
type Playback struct {
	V           int     `json:"v"`
	SID         string  `json:"sid"`
	Seq         int64   `json:"seq"`
	State       string  `json:"state"`
	Position    float64 `json:"position"`
	AtWallTime  int64   `json:"atWallTime"`
	AnyonePause bool    `json:"anyonePause,omitempty"` // the host lets guests ask it to pause
}

// PauseRequest is a guest asking the host to pause.
type PauseRequest struct {
	V   int    `json:"v"`
	SID string `json:"sid"`
}

// Recipe is the pre-show as the host will play it: each item in order, with
// how long it runs. Nothing in it points at the host's files: a clip is
// only its category, a trailer its film (and its YouTube video, a public
// one).
type Recipe struct {
	V     int          `json:"v"`
	SID   string       `json:"sid"`
	Rev   int          `json:"rev"` // the screening's revision it was made for
	Items []RecipeItem `json:"items"`
}

// RecipeItem is one piece of the pre-show.
type RecipeItem struct {
	Kind     string  `json:"kind"`               // card, clip or trailer
	Card     string  `json:"card,omitempty"`     // a card's kind (welcome, soon, rating…)
	Text     string  `json:"text,omitempty"`     // an announcement's words
	Category string  `json:"category,omitempty"` // a clip's category
	TMDB     int     `json:"tmdb,omitempty"`     // a trailer's (or a coming-soon card's) film or show
	TV       bool    `json:"tv,omitempty"`       // ... a show's
	Season   int     `json:"season,omitempty"`   // ... its new season
	YouTube  string  `json:"youtube,omitempty"`  // a trailer's YouTube video
	Title    string  `json:"title,omitempty"`
	Poster   string  `json:"poster,omitempty"`  // an image.tmdb.org address only
	Release  string  `json:"release,omitempty"` // YYYY-MM-DD
	Why      string  `json:"why,omitempty"`     // wanted, cinema or season
	Seconds  float64 `json:"seconds"`
}

// Cards a recipe may name.
var recipeCards = map[string]bool{"welcome": true, "popcorn": true, "silence": true, "coming": true, "rating": true, "countdown": true,
	"feature": true, "soon": true, "qr": true, "announce": true, "reel": true}

var (
	youTubeID = regexp.MustCompile(`^[A-Za-z0-9_-]{6,20}$`)
	dateRe    = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

// Room is what playback messages do on this Usherr. Every call is made only
// after the sender was found allowed.
type Room interface {
	// Guest side: the host's playback, and its pre-show.
	PlaybackIn(ctx context.Context, host link.Peer, p Playback) error
	RecipeIn(ctx context.Context, host link.Peer, r Recipe) error
	// Host side: a guest asks to pause.
	PauseIn(ctx context.Context, guest link.Peer, sid string) error
}

// RegisterRoom adds the playback messages to a node's registry; b says who
// hosts and who is invited, as for the screening messages.
func RegisterRoom(reg *link.Registry, b Book, room Room) error {
	kinds := []link.Kind{
		{Type: TypePlayback, MaxBody: MaxBodySmall, Group: true, Handle: func(ctx context.Context, in link.Incoming) error {
			var p Playback
			if err := decode(in.Msg.Body, &p); err != nil || !p.Check() {
				return errBody
			}
			if p.SID != in.Group {
				return ErrNotAllowed // sealed with another screening's key
			}
			if err := mustHost(ctx, b, p.SID, in.Peer.ID, false); err != nil {
				return err
			}
			return room.PlaybackIn(ctx, in.Peer, p)
		}},
		{Type: TypeRecipe, MaxBody: MaxBodyRecipe, Group: true, Handle: func(ctx context.Context, in link.Incoming) error {
			var r Recipe
			if err := decode(in.Msg.Body, &r); err != nil || !r.check() {
				return errBody
			}
			if r.SID != in.Group {
				return ErrNotAllowed
			}
			if err := mustHost(ctx, b, r.SID, in.Peer.ID, false); err != nil {
				return err
			}
			return room.RecipeIn(ctx, in.Peer, r)
		}},
		{Type: TypePause, MaxBody: MaxBodySmall, Handle: func(ctx context.Context, in link.Incoming) error {
			var p PauseRequest
			if err := decode(in.Msg.Body, &p); err != nil || p.V != Version || !validSID(p.SID) {
				return errBody
			}
			if err := mustInvited(ctx, b, p.SID, in.Peer.ID); err != nil {
				return err
			}
			return room.PauseIn(ctx, in.Peer, p.SID)
		}},
	}
	for _, k := range kinds {
		if err := reg.Register(k); err != nil {
			return err
		}
	}
	return nil
}

// Check reads a playback message: a known state, a position in range.
func (p *Playback) Check() bool {
	if p.V != Version || !validSID(p.SID) || p.Seq <= 0 || p.AtWallTime <= 0 {
		return false
	}
	if math.IsNaN(p.Position) || math.IsInf(p.Position, 0) || p.Position < -float64(MaxPreshow*60+60) || p.Position > float64(MaxRuntime*60+3600) {
		return false
	}
	switch p.State {
	case StatePreshow, StateFilm, StatePaused, StateEnded:
		return true
	}
	return false
}

func (r *Recipe) check() bool {
	if r.V != Version || !validSID(r.SID) || r.Rev < 1 || len(r.Items) > MaxRecipeItems {
		return false
	}
	total := 0.0
	for i := range r.Items {
		it := &r.Items[i]
		if math.IsNaN(it.Seconds) || it.Seconds <= 0 || it.Seconds > MaxItemSeconds || it.TMDB < 0 || it.TMDB >= 1<<24 || it.Season < 0 || it.Season > 200 {
			return false
		}
		total += it.Seconds
		switch it.Kind {
		case "card":
			if !recipeCards[it.Card] {
				return false
			}
		case "clip", "trailer":
			it.Card = ""
		default:
			return false
		}
		it.Text = cleanText(it.Text, 200)
		it.Category = cleanText(it.Category, 60)
		it.Title = cleanText(it.Title, 200)
		it.Why = cleanText(it.Why, 20)
		if it.YouTube != "" && !youTubeID.MatchString(it.YouTube) {
			it.YouTube = ""
		}
		if it.Poster != "" && (len(it.Poster) > 300 || !strings.HasPrefix(it.Poster, "https://image.tmdb.org/") || strings.ContainsAny(it.Poster, "\"'<> \\")) {
			it.Poster = ""
		}
		if it.Release != "" && !dateRe.MatchString(it.Release) {
			it.Release = ""
		}
	}
	return total <= float64(MaxPreshow*60)+1
}

// RecipeSeconds is how long a recipe's pre-show runs.
func RecipeSeconds(items []RecipeItem) float64 {
	t := 0.0
	for _, it := range items {
		t += it.Seconds
	}
	return t
}

// Project is where a playback state has got to at now (unix ms, the same
// clock as at): a playing film or pre-show moves on with the clock (a
// pre-show that has run out is the film), a paused or ended one stays.
func Project(state string, position float64, at, now int64) (string, float64) {
	switch state {
	case StateFilm, StatePreshow:
		pos := position + float64(now-at)/1000
		if state == StatePreshow && pos >= 0 {
			state = StateFilm
		}
		return state, pos
	}
	return state, position
}

// How a follower keeps in step.
const (
	InStep    = 0.1  // seconds: closer than this, nothing to do
	SeekOver  = 1.0  // seconds: further than this, seek
	RateGain  = 0.08 // speed change for each second off
	MaxNudge  = 0.06 // at most 6% faster or slower
	SeekAhead = 0.25 // a seek lands this far ahead (a seek takes a moment)
)

// Correct says how a follower catches up: drift is how far behind it is
// (seconds; negative when ahead). Close: play on at normal speed. A little
// off: play slightly faster (behind) or slower (ahead) until it's back in
// step. Far off: seek (seek true), to where it should be plus SeekAhead.
func Correct(drift float64) (rate float64, seek bool) {
	a := math.Abs(drift)
	switch {
	case math.IsNaN(drift):
		return 1, false
	case a < InStep:
		return 1, false
	case a > SeekOver:
		return 1, true
	}
	nudge := math.Max(-MaxNudge, math.Min(MaxNudge, drift*RateGain))
	return math.Round((1+nudge)*1000) / 1000, false
}
