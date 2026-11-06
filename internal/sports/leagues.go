// Package sports reads sports release names and tells whether a release is a
// given game, race or fight card, the way Usherr downloads sports without
// Sportarr. It knows how releases name the big leagues (NFL.2026.09.28...,
// Formula1.2026.Round15..., UFC.300.Prelims...), builds the searches for an
// event, and names the imported files the way Plex and Jellyfin read a TV
// library (League/Season 2026/League - S2026E05 - Event.mkv), like Sportarr.
//
// The rules follow Sportarr's (github.com/Sportarr/Sportarr, GPL-3.0, by its
// own authors): fight cards in parts, race weekends in sessions, date-based
// games. They are rewritten here in Go.
package sports

import (
	"regexp"
	"sort"
	"strings"
)

// Kind is how a sport's events are told apart in release names.
type Kind string

const (
	KindTeam      Kind = "team"      // two sides and a date: NFL, NBA, football
	KindFight     Kind = "fight"     // numbered cards in parts: UFC, Bellator
	KindMotor     Kind = "motor"     // race weekends in sessions: F1, MotoGP
	KindWrestling Kind = "wrestling" // weekly shows and big nights: WWE, AEW
	KindOther     Kind = "other"
)

// known is a league as releases name it.
type known struct {
	key      string
	names    []string // as TheSportsDB (and people) name it, normalized
	aliases  []string // as release names write it, the most usual first
	kind     Kind
	american bool // dated by the US day (an evening game is the next day in UTC)
}

var knownLeagues = []known{
	{key: "nfl", names: []string{"nfl", "national football league"}, aliases: []string{"nfl"}, kind: KindTeam, american: true},
	{key: "nba", names: []string{"nba", "national basketball association"}, aliases: []string{"nba"}, kind: KindTeam, american: true},
	{key: "nhl", names: []string{"nhl", "national hockey league"}, aliases: []string{"nhl"}, kind: KindTeam, american: true},
	{key: "mlb", names: []string{"mlb", "major league baseball"}, aliases: []string{"mlb"}, kind: KindTeam, american: true},
	{key: "mls", names: []string{"mls", "american major league soccer", "major league soccer"}, aliases: []string{"mls"}, kind: KindTeam, american: true},
	{key: "wnba", names: []string{"wnba"}, aliases: []string{"wnba"}, kind: KindTeam, american: true},
	{key: "ncaaf", names: []string{"ncaa division i fbs", "ncaaf", "college football"}, aliases: []string{"ncaaf", "ncaa football", "college football", "cfb"}, kind: KindTeam, american: true},
	{key: "ncaab", names: []string{"ncaa division i basketball mens", "ncaab", "college basketball"}, aliases: []string{"ncaab", "ncaa basketball", "college basketball"}, kind: KindTeam, american: true},
	{key: "epl", names: []string{"english premier league", "premier league"}, aliases: []string{"epl", "premier league", "english premier league"}, kind: KindTeam},
	{key: "laliga", names: []string{"spanish la liga", "la liga"}, aliases: []string{"la liga", "laliga"}, kind: KindTeam},
	{key: "bundesliga", names: []string{"german bundesliga", "bundesliga"}, aliases: []string{"bundesliga"}, kind: KindTeam},
	{key: "seriea", names: []string{"italian serie a", "serie a"}, aliases: []string{"serie a"}, kind: KindTeam},
	{key: "ligue1", names: []string{"french ligue 1", "ligue 1"}, aliases: []string{"ligue 1", "ligue1"}, kind: KindTeam},
	{key: "ucl", names: []string{"uefa champions league", "champions league"}, aliases: []string{"uefa champions league", "ucl", "champions league"}, kind: KindTeam},
	{key: "uel", names: []string{"uefa europa league", "europa league"}, aliases: []string{"uefa europa league", "europa league", "uel"}, kind: KindTeam},
	{key: "ipl", names: []string{"indian premier league"}, aliases: []string{"ipl", "indian premier league"}, kind: KindTeam},
	{key: "afl", names: []string{"australian afl", "afl"}, aliases: []string{"afl"}, kind: KindTeam},
	{key: "nrl", names: []string{"australian national rugby league", "nrl"}, aliases: []string{"nrl"}, kind: KindTeam},
	{key: "f1", names: []string{"formula 1", "formula one"}, aliases: []string{"formula1", "f1", "formula 1"}, kind: KindMotor},
	{key: "f2", names: []string{"formula 2", "fia formula 2 championship"}, aliases: []string{"formula2", "f2", "formula 2"}, kind: KindMotor},
	{key: "f3", names: []string{"formula 3", "fia formula 3 championship"}, aliases: []string{"formula3", "f3", "formula 3"}, kind: KindMotor},
	{key: "formulae", names: []string{"formula e"}, aliases: []string{"formula e", "formulae"}, kind: KindMotor},
	{key: "motogp", names: []string{"motogp"}, aliases: []string{"motogp"}, kind: KindMotor},
	{key: "moto2", names: []string{"moto2"}, aliases: []string{"moto2"}, kind: KindMotor},
	{key: "moto3", names: []string{"moto3"}, aliases: []string{"moto3"}, kind: KindMotor},
	{key: "indycar", names: []string{"indycar series", "indycar"}, aliases: []string{"indycar"}, kind: KindMotor, american: true},
	{key: "nascar", names: []string{"nascar cup series", "nascar"}, aliases: []string{"nascar cup series", "nascar"}, kind: KindMotor, american: true},
	{key: "wec", names: []string{"fia world endurance championship", "world endurance championship"}, aliases: []string{"wec", "fia wec"}, kind: KindMotor},
	{key: "wrc", names: []string{"world rally championship", "wrc"}, aliases: []string{"wrc"}, kind: KindMotor},
	{key: "ufc", names: []string{"ufc", "ultimate fighting championship"}, aliases: []string{"ufc"}, kind: KindFight, american: true},
	{key: "bellator", names: []string{"bellator mma", "bellator"}, aliases: []string{"bellator"}, kind: KindFight, american: true},
	{key: "pfl", names: []string{"professional fighters league", "pfl"}, aliases: []string{"pfl"}, kind: KindFight, american: true},
	{key: "one", names: []string{"one championship"}, aliases: []string{"one championship", "one fc"}, kind: KindFight},
	{key: "wwe", names: []string{"wwe", "world wrestling entertainment"}, aliases: []string{"wwe"}, kind: KindWrestling, american: true},
	{key: "aew", names: []string{"aew", "all elite wrestling"}, aliases: []string{"aew"}, kind: KindWrestling, american: true},
}

var nonWord = regexp.MustCompile(`[^\p{L}\p{N}]+`)

// Words is a name in lower-case words: "NFL.2026.09.28.Eagles.Vs.Bears" is
// "nfl 2026 09 28 eagles vs bears"; "&" is "and", "@" is "at".
func Words(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "&", " and ")
	s = strings.ReplaceAll(s, "@", " at ")
	return strings.TrimSpace(nonWord.ReplaceAllString(s, " "))
}

// has says whether the words hold the phrase as whole words.
func has(words, phrase string) bool {
	return phrase != "" && strings.Contains(" "+words+" ", " "+phrase+" ")
}

// League is a league as Usherr keeps it (TheSportsDB's name, its other names
// comma separated, and its sport).
type League struct {
	Name      string
	Alternate string
	Sport     string
}

func (l League) known() *known {
	cands := []string{Words(l.Name)}
	for _, a := range strings.Split(l.Alternate, ",") {
		if w := Words(a); w != "" {
			cands = append(cands, w)
		}
	}
	for i := range knownLeagues {
		k := &knownLeagues[i]
		for _, c := range cands {
			for _, n := range append(append([]string{}, k.names...), k.aliases...) {
				if c == n {
					return k
				}
			}
		}
	}
	return nil
}

// Key is the league's known key ("nfl", "f1"), or "" for one Usherr has no
// rules for.
func (l League) Key() string {
	if k := l.known(); k != nil {
		return k.key
	}
	return ""
}

// Kind is how the league's events are told apart.
func (l League) Kind() Kind {
	if k := l.known(); k != nil {
		return k.kind
	}
	return kindOfSport(l.Sport)
}

// American says the league's games are dated by the US day.
func (l League) American() bool {
	k := l.known()
	return k != nil && k.american
}

// kindOfSport reads TheSportsDB's sport names.
func kindOfSport(sport string) Kind {
	switch Words(sport) {
	case "fighting", "mma", "boxing", "kickboxing":
		return KindFight
	case "wrestling":
		return KindWrestling
	case "motorsport", "motor sport", "motorsports":
		return KindMotor
	case "soccer", "american football", "basketball", "ice hockey", "baseball", "rugby", "australian football",
		"handball", "volleyball", "cricket", "netball", "field hockey", "water polo", "futsal", "gaelic football", "hurling", "lacrosse":
		return KindTeam
	}
	return KindOther
}

// Aliases are the league's names as release names write them, in words,
// shortest first: "Formula 1" is also "formula1" and "f1".
func (l League) Aliases() []string {
	seen := map[string]bool{}
	var out []string
	add := func(v string) {
		v = Words(v)
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	add(l.Name)
	add(strings.ReplaceAll(Words(l.Name), " ", ""))
	for _, a := range strings.Split(l.Alternate, ",") {
		add(a)
	}
	if n := Words(l.Name); strings.HasPrefix(n, "formula ") {
		add("f" + strings.TrimPrefix(n, "formula "))
	}
	if k := l.known(); k != nil {
		for _, a := range k.aliases {
			add(a)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return len(out[i]) < len(out[j]) })
	return out
}

// SearchNames are the names searches use, the most usual in release names
// first (at most two).
func (l League) SearchNames() []string {
	if k := l.known(); k != nil {
		return k.aliases[:min(2, len(k.aliases))]
	}
	// An unknown league: its shortest other name (an abbreviation) first,
	// then its name.
	var out []string
	name := Words(l.Name)
	if a := l.Aliases(); len(a) > 0 && a[0] != name && a[0] != strings.ReplaceAll(name, " ", "") && len(a[0]) >= 2 && !strings.Contains(name, a[0]) {
		out = append(out, a[0])
	}
	if name != "" {
		out = append(out, name)
	}
	return out
}

// places are how race weekends' release names write a place: "Australian
// Grand Prix" is "Australia", "Saudi Arabian" is "Saudi Arabia".
var places = map[string][]string{
	"australian": {"australia"}, "austrian": {"austria"}, "italian": {"italy", "italia"}, "british": {"britain", "great britain", "silverstone"},
	"spanish": {"spain", "espana"}, "belgian": {"belgium", "spa"}, "dutch": {"netherlands", "zandvoort"}, "hungarian": {"hungary"},
	"brazilian": {"brazil", "sao paulo"}, "mexican": {"mexico"}, "canadian": {"canada"}, "japanese": {"japan"}, "chinese": {"china"},
	"french": {"france"}, "german": {"germany"}, "portuguese": {"portugal"}, "argentine": {"argentina"}, "argentinian": {"argentina"},
	"malaysian": {"malaysia"}, "thai": {"thailand"}, "indonesian": {"indonesia"}, "indian": {"india"}, "catalan": {"catalunya", "catalonia"},
	"valencian": {"valencia"}, "qatari": {"qatar"}, "aragon": {"aragon"}, "monegasque": {"monaco"},
}

// placePhrases are places of two words, and how releases write them.
var placePhrases = [][2]string{{"united states", "united states"}, {"saudi arabian", "saudi arabia"}, {"mexico city", "mexico"}, {"sao paulo", "sao paulo"},
	{"abu dhabi", "abu dhabi"}, {"las vegas", "las vegas"}, {"emilia romagna", "emilia romagna"}, {"great britain", "great britain"}, {"san marino", "san marino"}}

// placeOf is the place a race weekend's title names, as releases write it
// ("" when it names none).
func placeOf(titleWords string, distinct []string) string {
	for _, p := range placePhrases {
		if has(titleWords, p[0]) {
			return p[1]
		}
	}
	if len(distinct) == 0 {
		return ""
	}
	if alt, ok := places[distinct[0]]; ok {
		return alt[0]
	}
	return distinct[0]
}

// wordIn says whether the release's words have a title's word, written as
// it is, as its place's name ("australian" as "australia"), or a little
// shorter or longer (at least five letters in common).
func wordIn(words, x string) bool {
	if has(words, x) {
		return true
	}
	for _, alt := range places[x] {
		if has(words, alt) {
			return true
		}
	}
	if len(x) < 5 {
		return false
	}
	for _, w := range strings.Fields(words) {
		if len(w) >= 5 && (strings.HasPrefix(x, w) || strings.HasPrefix(w, x)) {
			return true
		}
	}
	return false
}

// namedLeague is the known league a release name starts with (or names
// near its start), by key.
func namedLeague(words string) string {
	f := strings.Fields(words)
	head := strings.Join(f[:min(len(f), 4)], " ")
	best, bestAt := "", 1<<30
	for _, k := range knownLeagues {
		for _, a := range k.aliases {
			if i := strings.Index(" "+head+" ", " "+a+" "); i >= 0 && i < bestAt {
				best, bestAt = k.key, i
			}
		}
	}
	return best
}
