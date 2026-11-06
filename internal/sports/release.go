package sports

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/quality"
)

// The parts of a fight card, earliest first.
const (
	PartEarlyPrelims = "Early Prelims"
	PartPrelims      = "Prelims"
	PartMainCard     = "Main Card"
)

// FightParts are a fight card's parts, earliest first.
var FightParts = []string{PartEarlyPrelims, PartPrelims, PartMainCard}

// The sessions of a race weekend.
const (
	SessionPractice         = "Practice"
	SessionQualifying       = "Qualifying"
	SessionSprintQualifying = "Sprint Qualifying"
	SessionSprint           = "Sprint"
	SessionRace             = "Race"
)

// Release is what a sports release name says.
type Release struct {
	Title   string
	Words   string    // the name in lower-case words
	League  string    // the known league the name starts with ("nfl", "f1"), "" when none
	Year    int       // a year standing alone, or the date's
	Date    time.Time // the date in the name; zero without one
	Round   int       // Round 15, R15, Rd15, Week 4, Matchweek 10, GW10; 0 without
	Number  int       // a numbered event (UFC 300, Fight Night 288); 0 without
	Teams   [2]string // the two sides around "vs", "v" or "at", when the name has them
	Part    string    // a fight card's part, when the name says one
	Session string    // a race weekend's session, when the name says one
	Side    bool      // a programme around the event (build-up, highlights, a press conference)

	Resolution string
	Source     string
	Tier       quality.Tier
}

var (
	dateYMD   = regexp.MustCompile(`\b((?:19|20)\d{2}) (0[1-9]|1[0-2]) (0[1-9]|[12]\d|3[01])\b`)
	dateDMY   = regexp.MustCompile(`\b(0[1-9]|[12]\d|3[01]) (0[1-9]|1[0-2]) ((?:19|20)\d{2})\b`)
	dateWords = regexp.MustCompile(`\b(\d{1,2})(?:st|nd|rd|th)? (jan|feb|mar|apr|may|jun|jul|aug|sep|sept|oct|nov|dec)[a-z]* ((?:19|20)\d{2})\b`)
	yearRe    = regexp.MustCompile(`\b((?:19|20)\d{2})\b`)
	roundRe   = regexp.MustCompile(`\b(?:round|rd|r|week|wk|matchweek|matchday|md|gameweek|gw)(?: )?(\d{1,2})\b`)
	numberRe  = regexp.MustCompile(`\b(?:ufc|bellator|pfl|night|espn|abc|fox|ksw|oktagon|warriors|lfa|rizin|bkfc|cwfc|championship|fc) (\d{1,3})\b`)
	// 1080p50 and 720p60 are the frame rate after the resolution.
	frameRate = regexp.MustCompile(`(?i)\b(2160|1080|720|576|480)p(?:25|30|50|60)\b`)
	months    = map[string]time.Month{"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6, "jul": 7, "aug": 8, "sep": 9, "sept": 9, "oct": 10, "nov": 11, "dec": 12}
)

// stopAfterTeams end the second side's name: quality, source and the like.
var stopAfterTeams = map[string]bool{
	"2160p": true, "1080p": true, "1080i": true, "720p": true, "576p": true, "480p": true, "4k": true, "uhd": true,
	"web": true, "webrip": true, "webdl": true, "dl": true, "hdtv": true, "pdtv": true, "sdtv": true, "x264": true, "h264": true, "x265": true, "h265": true, "hevc": true,
	"aac": true, "ac3": true, "eac3": true, "ddp": true, "dd": true, "ppv": true, "proper": true, "repack": true, "internal": true, "english": true, "multi": true,
	"prelims": true, "early": true, "main": true, "card": true, "full": true, "game": true, "match": true, "replay": true, "highlights": true, "mkv": true, "mp4": true,
}

// Clean is a release name as the general release parser reads it best:
// "1080p50" is "1080p".
func Clean(title string) string { return frameRate.ReplaceAllString(title, "${1}p") }

// Parse reads a sports release name.
func Parse(title string) Release {
	r := Release{Title: title}
	clean := Clean(title)
	r.Words = Words(clean)
	w := r.Words
	r.League = namedLeague(w)

	if m := dateYMD.FindStringSubmatch(w); m != nil {
		r.Date = ymd(m[1], m[2], m[3])
	} else if m := dateDMY.FindStringSubmatch(w); m != nil {
		r.Date = ymd(m[3], m[2], m[1])
	} else if m := dateWords.FindStringSubmatch(w); m != nil {
		if mo, ok := months[m[2]]; ok {
			d, _ := strconv.Atoi(m[1])
			y, _ := strconv.Atoi(m[3])
			r.Date = time.Date(y, mo, d, 0, 0, 0, 0, time.UTC)
		}
	}
	if !r.Date.IsZero() {
		r.Year = r.Date.Year()
	} else if m := yearRe.FindStringSubmatch(w); m != nil {
		r.Year, _ = strconv.Atoi(m[1])
	}
	if m := roundRe.FindStringSubmatch(w); m != nil {
		r.Round, _ = strconv.Atoi(m[1])
	}
	if m := numberRe.FindStringSubmatch(w); m != nil {
		if n, _ := strconv.Atoi(m[1]); n > 0 {
			r.Number = n
		}
	}
	r.Teams = teamsOf(w)
	r.Part = partOf(w)
	r.Session = sessionOf(w)
	r.Side = sideOf(w, "")

	p := parser.Parse(clean)
	r.Resolution, r.Source = p.Resolution, p.Source
	if r.Source == "" && (has(w, "web") || has(w, "f1tv") || has(w, "espn+")) {
		r.Source = "WEB"
	}
	r.Tier = quality.Classify(parser.Release{Resolution: r.Resolution, Source: r.Source})
	return r
}

func ymd(y, m, d string) time.Time {
	yy, _ := strconv.Atoi(y)
	mm, _ := strconv.Atoi(m)
	dd, _ := strconv.Atoi(d)
	t := time.Date(yy, time.Month(mm), dd, 0, 0, 0, 0, time.UTC)
	if t.Day() != dd { // 31 02 rolled over
		return time.Time{}
	}
	return t
}

// teamsOf finds the two sides of "A vs B": up to three words before the
// "vs" (after the date or the league) and the words after it up to the
// quality tags.
func teamsOf(w string) [2]string {
	f := strings.Fields(w)
	at := -1
	for i, x := range f {
		if x == "vs" || x == "v" || x == "versus" {
			at = i
			break
		}
	}
	if at < 0 { // "Bears at Eagles", only between two names
		for i, x := range f {
			if x == "at" && i > 0 && i < len(f)-1 && !isNumber(f[i-1]) && !isNumber(f[i+1]) {
				at = i
				break
			}
		}
	}
	if at <= 0 || at >= len(f)-1 {
		return [2]string{}
	}
	var left []string
	for i := at - 1; i >= 0 && len(left) < 3; i-- {
		if isNumber(f[i]) || roundRe.MatchString(f[i]) || namedLeague(f[i]) != "" {
			break
		}
		left = append([]string{f[i]}, left...)
	}
	var right []string
	for i := at + 1; i < len(f) && len(right) < 4; i++ {
		if stopAfterTeams[f[i]] || isNumber(f[i]) {
			break
		}
		right = append(right, f[i])
	}
	if len(left) == 0 || len(right) == 0 {
		return [2]string{}
	}
	return [2]string{strings.Join(left, " "), strings.Join(right, " ")}
}

func isNumber(s string) bool {
	_, err := strconv.Atoi(s)
	return err == nil
}

func partOf(w string) string {
	switch {
	case has(w, "early prelims") || has(w, "early prelim") || has(w, "early preliminary") || has(w, "early preliminaries"):
		return PartEarlyPrelims
	case has(w, "prelims") || has(w, "prelim") || has(w, "preliminary") || has(w, "preliminaries") || has(w, "preliminary card"):
		return PartPrelims
	case has(w, "main card") || has(w, "main event") || has(w, "ppv"):
		return PartMainCard
	}
	return ""
}

func sessionOf(w string) string {
	switch {
	case has(w, "sprint qualifying") || has(w, "sprint shootout") || has(w, "sprint quali"):
		return SessionSprintQualifying
	case has(w, "qualifying") || has(w, "quali") || has(w, "qualification"):
		return SessionQualifying
	case has(w, "sprint") || has(w, "sprint race"):
		return SessionSprint
	case has(w, "fp1") || has(w, "fp2") || has(w, "fp3") || has(w, "practice") || has(w, "free practice"):
		return SessionPractice
	case has(w, "race"):
		return SessionRace
	}
	return ""
}

// sideWords name the programmes around an event: the build-up, the shows
// after it, analysis, highlights.
var sideWords = []string{"show", "warm up", "warmup", "analysis", "notebook", "build up", "buildup", "highlights", "preview", "review",
	"pre qualifying", "post qualifying", "pre race", "post race", "pre sprint", "post sprint", "press conference", "interview", "onboard",
	"magazine", "countdown", "paddock", "debrief", "pre game", "post game", "pregame", "postgame", "weigh in", "weigh ins", "embedded",
	"ted kravitz", "teds notebook", "chequered flag", "f1 kids", "condensed", "all access", "behind the scenes", "documentary"}

// sideOf says the words name a programme around the event that the event's
// own title doesn't.
func sideOf(w, eventWords string) bool {
	for _, s := range sideWords {
		if has(w, s) && !has(eventWords, s) {
			return true
		}
	}
	return false
}

// SessionOf is the session an event's title names; a race weekend's event
// without one is the race.
func SessionOf(eventTitle string) string {
	if s := sessionOf(Words(eventTitle)); s != "" {
		return s
	}
	return SessionRace
}

// SessionChoice is the monitoring choice a session belongs to ("race",
// "qualifying", "sprint", "practice").
func SessionChoice(session string) string {
	switch session {
	case SessionQualifying:
		return "qualifying"
	case SessionSprint, SessionSprintQualifying:
		return "sprint"
	case SessionPractice:
		return "practice"
	}
	return "race"
}

// PartChoice is the monitoring choice a fight card's part belongs to
// ("early", "prelims", "main").
func PartChoice(part string) string {
	switch part {
	case PartEarlyPrelims:
		return "early"
	case PartPrelims:
		return "prelims"
	}
	return "main"
}
