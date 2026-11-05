// Package servicecheck is Usherr's own service watch: a list of web
// addresses (Jellyfin, a download client, Prowlarr, anything) that Usherr asks
// every so often, the state of each, and a "down" and a "back" message through
// the notification targets. It does what a separate watchdog container did:
// a service counts as down after a few failed checks in a row (three by
// default), so a restart or a blip stays quiet, and it is told once per
// outage, with one message when it answers again.
//
// Usherr: the whole package is a fork addition.
package servicecheck

import (
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/rdborg/mediarium/internal/inputcheck"
)

// What counts as a good answer.
const (
	Expect200 = "200" // exactly 200
	ExpectOK  = "ok"  // any 2xx or 3xx
	ExpectAny = "any" // any answer at all: the program runs, even if it asks for a login
)

// The states of a service.
const (
	StateUnknown = "unknown" // not answered yet since it was added
	StateUp      = "up"
	StateDown    = "down"
)

// Limits of the settings of one check.
const (
	MinInterval     = 30 * time.Second
	MaxInterval     = 24 * time.Hour
	DefaultInterval = time.Minute
	DefaultFailures = 3
	MaxFailures     = 20
	MaxChecks       = 50
	maxNameLen      = 60
	maxURLLen       = 500
	maxKeywordLen   = 100
)

// Check is one watched service: what to ask, and the last word on it.
type Check struct {
	ID        int64
	Name      string
	URL       string // may hold a key in its query, so it is shown to administrators only and never put in a message
	Expect    string // Expect200, ExpectOK or ExpectAny
	Keyword   string // when set, the answer's body must contain it (case is ignored)
	Interval  time.Duration
	FailAfter int // failed checks in a row before it counts as down
	Enabled   bool
	Created   time.Time

	State     string    // StateUnknown, StateUp or StateDown
	Fails     int       // failed checks in a row
	FailSince time.Time // when the first of those failed; zero while it answers
	Alerted   bool      // the "down" message of this outage was sent
	Changed   time.Time // when State last changed
	Checked   time.Time // the last check; zero when never checked
	LastMs    int       // how long the last answer took, in milliseconds
	LastCode  int       // the last HTTP status, 0 when there was no answer
	LastError string    // why the last check failed, in plain words; "" when it worked
}

// Result is one check of one service.
type Result struct {
	At    time.Time
	OK    bool
	Ms    int    // how long the answer took
	Code  int    // the HTTP status; 0 when nothing answered
	Error string // why it failed, in plain words; "" when it worked
}

// Change is a service going down or coming back.
type Change struct {
	At     time.Time
	State  string // StateDown or StateUp
	Detail string
}

// Verdict is what one result means for the messages.
type Verdict int

const (
	Same     Verdict = iota // nothing to tell
	WentDown                // send the "down" message
	CameBack                // send the "back" message
)

// Step applies one result to a check and says whether a message should go
// out. It is the whole rule: FailAfter failures in a row make the service
// down and send one "down" message; the first good answer after that makes it
// up again and sends one "back" message. A good answer before the count is
// reached starts the count over and says nothing.
func Step(c Check, r Result) (Check, Verdict) {
	c.Checked = r.At
	c.LastMs, c.LastCode, c.LastError = r.Ms, r.Code, r.Error
	failAfter := c.FailAfter
	if failAfter < 1 {
		failAfter = DefaultFailures
	}
	if r.OK {
		v := Same
		if c.State == StateDown && c.Alerted {
			v = CameBack
		}
		if c.State != StateUp {
			c.State, c.Changed = StateUp, r.At
		}
		c.Fails, c.FailSince, c.Alerted = 0, time.Time{}, false
		return c, v
	}
	if c.Fails == 0 {
		c.FailSince = r.At
	}
	c.Fails++
	if c.Fails >= failAfter && c.State != StateDown {
		c.State, c.Changed, c.Alerted = StateDown, r.At, true
		return c, WentDown
	}
	return c, Same
}

// Outage is how long the service has been failing, as of at.
func (c Check) Outage(at time.Time) time.Duration {
	if c.FailSince.IsZero() || at.Before(c.FailSince) {
		return 0
	}
	return at.Sub(c.FailSince)
}

// Normalize checks what a person typed for one service and fills in the
// defaults. The message is plain English, ready for a 400 answer.
func Normalize(c Check) (Check, error) {
	c.Name = strings.TrimSpace(c.Name)
	c.URL = strings.TrimSpace(c.URL)
	c.Keyword = strings.TrimSpace(c.Keyword)
	c.Expect = strings.TrimSpace(strings.ToLower(c.Expect))
	switch {
	case c.Name == "":
		return c, fmt.Errorf("Give the service a name, for example Jellyfin.")
	case len([]rune(c.Name)) > maxNameLen:
		return c, fmt.Errorf("The name can be at most %d characters.", maxNameLen)
	case hasControl(c.Name) || hasControl(c.Keyword):
		return c, fmt.Errorf("The name and the word to look for can't contain line breaks or tabs.")
	case c.URL == "":
		return c, fmt.Errorf("Give the address to check, for example http://192.168.1.10:8096/health.")
	case len(c.URL) > maxURLLen:
		return c, fmt.Errorf("The address can be at most %d characters.", maxURLLen)
	case len([]rune(c.Keyword)) > maxKeywordLen:
		return c, fmt.Errorf("The word to look for can be at most %d characters.", maxKeywordLen)
	}
	if msg := inputcheck.HTTPURL(c.URL, "http://192.168.1.10:8096/health", true); msg != "" {
		return c, fmt.Errorf("%s", msg)
	}
	if u, err := url.Parse(c.URL); err != nil || u.User != nil {
		return c, fmt.Errorf("Leave the user name and password out of the address.")
	}
	switch c.Expect {
	case "":
		c.Expect = ExpectOK
	case Expect200, ExpectOK, ExpectAny:
	default:
		return c, fmt.Errorf("Choose what counts as working: 200 only, any 2xx or 3xx, or any answer.")
	}
	if c.Interval == 0 {
		c.Interval = DefaultInterval
	}
	if c.Interval < MinInterval || c.Interval > MaxInterval {
		return c, fmt.Errorf("Check every 30 seconds at the most often, and at least once a day.")
	}
	if c.FailAfter == 0 {
		c.FailAfter = DefaultFailures
	}
	if c.FailAfter < 1 || c.FailAfter > MaxFailures {
		return c, fmt.Errorf("Failed checks before a message: choose 1 to %d.", MaxFailures)
	}
	return c, nil
}

func hasControl(s string) bool {
	return strings.IndexFunc(s, unicode.IsControl) >= 0
}
