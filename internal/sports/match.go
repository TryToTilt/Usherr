package sports

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Event is a game, race or fight card as Usherr keeps it (from TheSportsDB).
type Event struct {
	League League
	Title  string
	Home   string
	Away   string
	Round  string // as TheSportsDB has it ("4", "15"); "" or "0" when none
	Season string // "2026" or "2025-2026"
	At     time.Time
}

// Kind is the event's league's kind.
func (e Event) Kind() Kind { return e.League.Kind() }

// Number is the event's number, for a numbered fight card (UFC 300).
func (e Event) Number() int {
	m := numberRe.FindStringSubmatch(Words(e.Title))
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// LocalDay is the day the event happens where it is dated: an American
// league's evening game starts after midnight UTC, and its releases carry
// the US date.
func (e Event) LocalDay() time.Time {
	at := e.At.UTC()
	if e.League.American() {
		at = at.Add(-6 * time.Hour)
	}
	return time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC)
}

// Verdict is whether a release is an event (and one part of it).
type Verdict struct {
	Match  bool     `json:"match"`
	Score  int      `json:"score"`            // how surely, 0-100
	Reject string   `json:"reject,omitempty"` // why not, in plain words
	Part   string   `json:"part,omitempty"`   // the fight card's part the release is
	Why    []string `json:"why,omitempty"`    // what matched ("the date", "both teams")
}

func reject(format string, a ...any) Verdict {
	return Verdict{Reject: fmt.Sprintf(format, a...)}
}

// genericTeamWords say little on their own: "United", "City", "FC".
var genericTeamWords = map[string]bool{"fc": true, "afc": true, "cf": true, "sc": true, "ac": true, "club": true, "united": true, "city": true,
	"the": true, "of": true, "de": true, "real": true, "athletic": true, "sporting": true, "county": true, "town": true, "rovers": true, "wanderers": true,
	"state": true, "university": true, "and": true}

// teamWords are a team's distinctive words: "Philadelphia Eagles" is
// "philadelphia" and "eagles"; "Manchester United" is "manchester".
func teamWords(name string) []string {
	var out []string
	for _, w := range strings.Fields(Words(name)) {
		if len(w) >= 3 && !genericTeamWords[w] {
			out = append(out, w)
		}
	}
	return out
}

// TeamKey is the word searches name a team by: its last distinctive word
// (Eagles, Bears, Arsenal, Manchester).
func TeamKey(name string) string {
	w := teamWords(name)
	if len(w) == 0 {
		return Words(name)
	}
	return w[len(w)-1]
}

// teamIn says how a release names a team: 2 by its whole name, 1 by one of
// its distinctive words, 0 not at all.
func teamIn(words, team string) int {
	if has(words, Words(team)) {
		return 2
	}
	for _, w := range teamWords(team) {
		if has(words, w) {
			return 1
		}
	}
	return 0
}

// skipTitleWords are an event title's words that say nothing about which
// event it is.
var skipTitleWords = map[string]bool{"vs": true, "the": true, "and": true, "grand": true, "prix": true, "fight": true, "night": true, "race": true,
	"qualifying": true, "sprint": true, "practice": true, "prelims": true, "early": true, "main": true, "card": true, "event": true, "round": true,
	"week": true, "game": true, "match": true, "day": true, "gp": true, "live": true, "free": true, "shootout": true}

// Match says whether a release is the event, for the part asked (a fight
// card's part; "" for any other event, or the main card).
func Match(r Release, e Event, part string) Verdict {
	w := r.Words
	ew := Words(e.Title)
	kind := e.Kind()

	named := false
	for _, a := range e.League.Aliases() {
		if has(w, a) {
			named = true
			break
		}
	}
	if !named {
		if r.League != "" && r.League != e.League.Key() {
			return reject("it's from another league")
		}
		return reject("it doesn't name the league")
	}
	if sideOf(w, ew) {
		return reject("it's a programme around the event, not the event")
	}

	v := Verdict{Score: 10}

	// A fight card's part, a race weekend's session.
	if kind == KindFight {
		got := r.Part
		if got == "" {
			got = PartMainCard
		}
		want := part
		if want == "" {
			want = PartMainCard
		}
		if got != want {
			return reject("it's the %s, not the %s", strings.ToLower(got), strings.ToLower(want))
		}
		v.Part = got
	}
	if kind == KindMotor {
		want := SessionOf(e.Title)
		got := r.Session
		if got == "" {
			got = SessionRace
		}
		if got != want {
			return reject("it's the %s, not the %s", strings.ToLower(got), strings.ToLower(want))
		}
	}

	// The date: the event's day, a day either way for time zones.
	dateOK := false
	if !r.Date.IsZero() && !e.At.IsZero() {
		day := e.LocalDay()
		for _, d := range []time.Time{day, day.AddDate(0, 0, -1), day.AddDate(0, 0, 1)} {
			if r.Date.Equal(d) {
				dateOK = true
			}
		}
		if !dateOK {
			return reject("it's from another day (%s)", r.Date.Format("2006-01-02"))
		}
		v.Score += 40
		v.Why = append(v.Why, "the date")
	}
	if !dateOK && r.Year > 0 && !e.At.IsZero() && (r.Year < e.At.Year()-1 || r.Year > e.At.Year()) {
		return reject("it's from %d", r.Year)
	}

	// The round (or week).
	roundOK := false
	if n, _ := strconv.Atoi(strings.TrimSpace(e.Round)); n > 0 && n < 100 && r.Round > 0 {
		if r.Round != n && !dateOK {
			return reject("it's round %d, not round %d", r.Round, n)
		}
		if r.Round == n {
			roundOK = true
			v.Score += 15
			v.Why = append(v.Why, "the round")
		}
	}

	// A numbered fight card.
	if n := e.Number(); n > 0 {
		if r.Number > 0 && r.Number != n {
			return reject("it's number %d, not %d", r.Number, n)
		}
		if !has(w, strconv.Itoa(n)) {
			return reject("it doesn't have the event's number (%d)", n)
		}
		v.Score += 35
		v.Why = append(v.Why, "the event's number")
		return finish(v)
	}

	// Two teams.
	if kind == KindTeam && e.Home != "" && e.Away != "" {
		h, a := teamIn(w, e.Home), teamIn(w, e.Away)
		if r.Teams[0] != "" && h == 0 && a == 0 {
			return reject("it's another game (%s v %s)", r.Teams[0], r.Teams[1])
		}
		switch {
		case h > 0 && a > 0:
			v.Score += 35
			v.Why = append(v.Why, "both teams")
		case h > 0 || a > 0:
			if !dateOK {
				return reject("it doesn't name both teams")
			}
			v.Score += 10
			v.Why = append(v.Why, "one team")
		default:
			return reject("it doesn't name the teams")
		}
		if !dateOK && !roundOK {
			// The same two teams meet more than once a season: without the
			// date or the round, only the year says which.
			if r.Year == 0 {
				return reject("it doesn't say when (no date, round or year)")
			}
			v.Score -= 10
		}
		return finish(v)
	}

	// No teams (a race, a show, a card): the title's own words.
	want, hits := 0, 0
	aliases := " " + strings.Join(e.League.Aliases(), " ") + " "
	for _, x := range strings.Fields(ew) {
		if isNumber(x) {
			if len(x) < 4 && !has(w, x) && !roundOK {
				return reject("it doesn't have %s, as the event does", x)
			}
			continue
		}
		if len(x) < 3 || skipTitleWords[x] || strings.Contains(aliases, " "+x+" ") {
			continue
		}
		want++
		if wordIn(w, x) {
			hits++
		}
	}
	switch {
	case want == 0 && (dateOK || roundOK):
	case want > 0 && hits >= min(2, want):
		v.Score += 20 + 5*min(hits, 4)
		v.Why = append(v.Why, "the event's name")
	case want > 0 && hits >= 1 && (dateOK || roundOK):
		v.Score += 10
		v.Why = append(v.Why, "the event's name")
	default:
		return reject("it doesn't name the event")
	}
	if !dateOK && !roundOK && r.Year == 0 {
		return reject("it doesn't say when (no date, round or year)")
	}
	return finish(v)
}

func finish(v Verdict) Verdict {
	v.Match = true
	v.Score = max(1, min(v.Score, 100))
	return v
}
