package sports

import (
	"strconv"
	"strings"
)

// maxQueries keeps an event's searches few: each one is a call to every
// indexer, and their daily limits are low.
const maxQueries = 3

// Queries are the searches for an event (and a fight card's part), the most
// likely to find it first. A search that finds the event makes the others
// unneeded.
func Queries(e Event, part string) []string {
	names := e.League.SearchNames()
	if len(names) == 0 {
		return nil
	}
	name := names[0]
	var out []string
	add := func(parts ...string) {
		q := strings.Join(strings.Fields(strings.Join(parts, " ")), " ")
		if q == "" {
			return
		}
		for _, have := range out {
			if have == q {
				return
			}
		}
		out = append(out, q)
	}
	day := ""
	if !e.At.IsZero() {
		day = e.LocalDay().Format("2006 01 02")
	}
	year := ""
	if !e.At.IsZero() {
		year = strconv.Itoa(e.At.Year())
	}
	titleWords := distinctWords(e)

	switch e.Kind() {
	case KindFight:
		partWords := ""
		switch part {
		case PartEarlyPrelims:
			partWords = "early prelims"
		case PartPrelims:
			partWords = "prelims"
		}
		if n := e.Number(); n > 0 {
			ev := strconv.Itoa(n)
			if has(Words(e.Title), "fight night "+ev) {
				ev = "fight night " + ev
			}
			add(name, ev, partWords)
		}
		if len(titleWords) >= 2 {
			add(name, strings.Join(titleWords[:min(len(titleWords), 4)], " "), partWords)
		}
		if day != "" {
			add(name, day, partWords)
		}
	case KindMotor:
		where := placeOf(Words(e.Title), titleWords)
		session := ""
		if s := SessionOf(e.Title); s != SessionRace {
			session = strings.ToLower(s)
			if s == SessionPractice {
				session = "" // FP1, FP2, Practice: found with the weekend
			}
		}
		for _, n := range names {
			add(n, year, where, session)
		}
	case KindTeam:
		if day != "" {
			add(name, day)
		}
		if e.Home != "" && e.Away != "" {
			add(name, TeamKey(e.Home), TeamKey(e.Away))
		}
		if len(names) > 1 && day != "" {
			add(names[1], day)
		}
	default:
		if day != "" {
			add(name, day)
		}
		title := Words(e.Title)
		if !has(title, name) {
			title = name + " " + title
		}
		add(title)
	}
	if len(out) > maxQueries {
		out = out[:maxQueries]
	}
	return out
}

// distinctWords are the words of an event's title that tell it apart, in
// order: "Azerbaijan Grand Prix" is "azerbaijan"; "UFC 300 Pereira vs Hill"
// is "pereira", "hill".
func distinctWords(e Event) []string {
	aliases := " " + strings.Join(e.League.Aliases(), " ") + " "
	var out []string
	for _, x := range strings.Fields(Words(e.Title)) {
		if isNumber(x) || len(x) < 3 || skipTitleWords[x] || strings.Contains(aliases, " "+x+" ") {
			continue
		}
		out = append(out, x)
	}
	return out
}
