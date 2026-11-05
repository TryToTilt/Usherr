package quality

// Usherr: the profile options the editor's Advanced mode adds on top of
// Mediarium's (and the two Simple mode size preferences). They are stored
// together as one JSON object in quality_profiles.extras, so a profile with
// none of them set behaves exactly as before.

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
	"sync"

	"github.com/rdborg/mediarium/internal/parser"
)

// TierSize is a profile's own size range for one quality, in GB: per file for
// a movie, per episode for a show. 0 means no limit of its own.
type TierSize struct {
	MinGB       float64 `json:"minGB,omitempty"`
	PreferredGB float64 `json:"preferredGB,omitempty"`
	MaxGB       float64 `json:"maxGB,omitempty"`
}

func (s TierSize) empty() bool { return s.MinGB == 0 && s.PreferredGB == 0 && s.MaxGB == 0 }

// Protocols a profile can be held to.
const (
	ProtocolAny     = ""
	ProtocolUsenet  = "usenet"
	ProtocolTorrent = "torrent"
)

// Kinds a profile can be marked for (a label for the pickers; it limits
// nothing).
var profileKinds = map[string]bool{"": true, "movies": true, "tv": true, "anime": true}

// Extras are the Advanced options. The zero value changes nothing.
type Extras struct {
	// Kind is what the profile is meant for: "", "movies", "tv" or "anime".
	Kind string `json:"kind,omitempty"`
	// TierSizes are sizes per quality, for this profile only.
	TierSizes map[Tier]TierSize `json:"tierSizes,omitempty"`
	// PreferSmaller picks the smaller of two equally good releases (off: the
	// bigger one, or the one nearest the preferred size).
	PreferSmaller bool `json:"preferSmaller,omitempty"`
	// PreferHEVC picks x265/HEVC over x264 at the same quality.
	PreferHEVC bool `json:"preferHevc,omitempty"`
	// MinUpgradeGain is the smallest custom-format score gain worth replacing
	// a file of the same quality for (0: any gain).
	MinUpgradeGain int `json:"minUpgradeGain,omitempty"`
	// UpgradeDays stops upgrades this many days after the title's release
	// (0: upgrade whenever).
	UpgradeDays int `json:"upgradeDays,omitempty"`
	// PreferGroups win between releases of the same quality; NeverGroups are
	// never taken. Release group names, matched ignoring capitals.
	PreferGroups []string `json:"preferGroups,omitempty"`
	NeverGroups  []string `json:"neverGroups,omitempty"`
	// Language replaces the audio languages under Settings, Quality for this
	// profile ("" keeps them): names joined by commas, like the setting.
	AudioLanguage string `json:"audioLanguage,omitempty"`
	// WantSubs prefers releases that say they carry English subtitles.
	WantSubs bool `json:"wantSubs,omitempty"`
	// Indexers limits automatic searches to these indexers (by id); empty is
	// all of them.
	Indexers []int64 `json:"indexers,omitempty"`
	// Protocol holds the profile to Usenet or torrents ("" is either).
	Protocol string `json:"protocol,omitempty"`
	// MinAgeMinutes skips releases posted less than this long ago; MaxAgeDays
	// skips releases older than this (0: no limit).
	MinAgeMinutes int `json:"minAgeMinutes,omitempty"`
	MaxAgeDays    int `json:"maxAgeDays,omitempty"`
}

const (
	maxGroups      = 50
	maxGroupLen    = 40
	maxSizeGB      = 1000
	maxUpgradeDays = 3650
	maxAgeMinutes  = 7 * 24 * 60
	maxAgeDays     = 20000
)

// cleanExtras checks and tidies the Advanced options.
func cleanExtras(e Extras) (Extras, error) {
	e.Kind = strings.ToLower(strings.TrimSpace(e.Kind))
	if !profileKinds[e.Kind] {
		return e, fmt.Errorf("%w: %q is not a kind of profile (movies, tv or anime)", ErrInvalid, e.Kind)
	}
	sizes := map[Tier]TierSize{}
	valid := map[Tier]bool{}
	for _, t := range AllTiers() {
		valid[t] = true
	}
	for t, s := range e.TierSizes {
		if !valid[t] {
			return e, fmt.Errorf("%w: %q is not a quality tier", ErrInvalid, t)
		}
		for _, v := range []float64{s.MinGB, s.PreferredGB, s.MaxGB} {
			if v < 0 || v > maxSizeGB || math.IsNaN(v) {
				return e, fmt.Errorf("%w: sizes for %s must be 0 to %d GB", ErrInvalid, t, maxSizeGB)
			}
		}
		if s.MaxGB > 0 && s.MinGB > s.MaxGB {
			return e, fmt.Errorf("%w: the smallest size for %s is above its largest", ErrInvalid, t)
		}
		if s.PreferredGB > 0 && (s.PreferredGB < s.MinGB || (s.MaxGB > 0 && s.PreferredGB > s.MaxGB)) {
			return e, fmt.Errorf("%w: the preferred size for %s must be between its smallest and largest", ErrInvalid, t)
		}
		if !s.empty() {
			sizes[t] = s
		}
	}
	e.TierSizes = nil
	if len(sizes) > 0 {
		e.TierSizes = sizes
	}
	if e.MinUpgradeGain < 0 || e.MinUpgradeGain > 2*maxFormatScoreTotal {
		return e, fmt.Errorf("%w: the smallest score gain must be 0 or more", ErrInvalid)
	}
	if e.UpgradeDays < 0 || e.UpgradeDays > maxUpgradeDays {
		return e, fmt.Errorf("%w: upgrade days must be 0 to %d", ErrInvalid, maxUpgradeDays)
	}
	var err error
	if e.PreferGroups, err = cleanGroups(e.PreferGroups); err != nil {
		return e, err
	}
	if e.NeverGroups, err = cleanGroups(e.NeverGroups); err != nil {
		return e, err
	}
	e.AudioLanguage = strings.TrimSpace(e.AudioLanguage)
	if len(e.AudioLanguage) > 200 {
		return e, fmt.Errorf("%w: too many languages", ErrInvalid)
	}
	ids := []int64{}
	seen := map[int64]bool{}
	for _, id := range e.Indexers {
		if id > 0 && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	e.Indexers = nil
	if len(ids) > 0 {
		e.Indexers = ids
	}
	if e.Protocol != ProtocolAny && e.Protocol != ProtocolUsenet && e.Protocol != ProtocolTorrent {
		return e, fmt.Errorf("%w: protocol must be usenet, torrent or empty", ErrInvalid)
	}
	if e.MinAgeMinutes < 0 || e.MinAgeMinutes > maxAgeMinutes {
		return e, fmt.Errorf("%w: the newest age must be 0 to %d minutes", ErrInvalid, maxAgeMinutes)
	}
	if e.MaxAgeDays < 0 || e.MaxAgeDays > maxAgeDays {
		return e, fmt.Errorf("%w: the oldest age must be 0 to %d days", ErrInvalid, maxAgeDays)
	}
	return e, nil
}

// maxFormatScoreTotal bounds the upgrade gain; any real score fits.
const maxFormatScoreTotal = 1000000

func cleanGroups(in []string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, g := range in {
		g = strings.TrimSpace(g)
		if g == "" || seen[strings.ToLower(g)] {
			continue
		}
		if len(g) > maxGroupLen || strings.ContainsAny(g, "\n\r") {
			return nil, fmt.Errorf("%w: a release group name must be under %d characters", ErrInvalid, maxGroupLen)
		}
		seen[strings.ToLower(g)] = true
		out = append(out, g)
	}
	if len(out) > maxGroups {
		return nil, fmt.Errorf("%w: at most %d release groups", ErrInvalid, maxGroups)
	}
	return out, nil
}

func encodeExtras(e Extras) string {
	b, err := json.Marshal(e)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func decodeExtras(s string) Extras {
	var e Extras
	if s != "" {
		_ = json.Unmarshal([]byte(s), &e)
	}
	return e
}

// ---- groups ----

func inGroups(group string, list []string) bool {
	if group == "" {
		return false
	}
	for _, g := range list {
		if strings.EqualFold(g, group) {
			return true
		}
	}
	return false
}

// groupOf is the release group, also when the parser left it off a name
// that ends in "-GROUP".
func groupOf(title string, rel parser.Release) string {
	if rel.Group != "" {
		return rel.Group
	}
	if i := strings.LastIndexByte(title, '-'); i >= 0 && i < len(title)-1 {
		g := title[i+1:]
		if j := strings.IndexAny(g, ". []()"); j >= 0 {
			g = g[:j]
		}
		return g
	}
	return ""
}

// NeverGroupReason is why the profile refuses a release for its group, "".
func (p Profile) neverGroupReason(l *lazyRel) string {
	if len(p.NeverGroups) == 0 {
		return ""
	}
	if g := groupOf(l.title, l.get()); inGroups(g, p.NeverGroups) {
		return fmt.Sprintf("is by %s, a group the %q profile never takes", g, p.Name)
	}
	return ""
}

// PreferredGroup reports whether a release is by one of the profile's
// preferred groups.
func (p Profile) PreferredGroup(title string, rel parser.Release) bool {
	return inGroups(groupOf(title, rel), p.PreferGroups)
}

// ---- codec and subtitles ----

var (
	hevcRe = regexp.MustCompile(`(?i)\b(x265|h[ .]?265|hevc)\b`)
	subsRe = regexp.MustCompile(`(?i)\b(eng(lish)?[ ._-]?subs?|subbed|multi[ ._-]?subs?|eng[ ._-]?sub(title)?s?)\b`)
)

// HEVC reports whether a release name says it is x265/HEVC.
func HEVC(title string) bool { return hevcRe.MatchString(title) }

// HasSubs reports whether a release name says it carries English subtitles.
func HasSubs(title string) bool { return subsRe.MatchString(title) }

// ---- sizes ----

const gib = float64(int64(1) << 30)

// sizeEach is a release's size per item: per episode for a show (episodes is
// how many it holds, 0 for a movie). ok is false when it can't be told (a
// season pack of unknown length, or no size).
func sizeEach(size int64, episodes int) (float64, bool) {
	if size <= 0 || episodes == SeasonPack {
		return 0, false
	}
	gb := float64(size) / gib
	if episodes > 1 {
		gb /= float64(episodes)
	}
	return gb, true
}

// TierSizeReason is why a release's size is outside the profile's own range
// for its quality, "" when it fits (or the profile has no range for it).
func (p Profile) TierSizeReason(t Tier, size int64, episodes int) string {
	s, ok := p.TierSizes[t]
	if !ok {
		return ""
	}
	gb, ok := sizeEach(size, episodes)
	if !ok {
		return ""
	}
	per := ""
	if episodes > 0 {
		per = " an episode"
	}
	if s.MinGB > 0 && gb < s.MinGB {
		return fmt.Sprintf("%.1f GB%s is below the %q profile's smallest %s size (%g GB)", gb, per, p.Name, t, s.MinGB)
	}
	if s.MaxGB > 0 && gb > s.MaxGB {
		return fmt.Sprintf("%.1f GB%s is above the %q profile's largest %s size (%g GB)", gb, per, p.Name, t, s.MaxGB)
	}
	return ""
}

// SizeFits is SizeAllowed with the profile's own range for the quality.
func (p Profile) SizeFits(t Tier, size int64, episodes int) bool {
	return p.SizeAllowed(size) && p.TierSizeReason(t, size, episodes) == ""
}

// SizePreference ranks releases of the same quality by size: higher is
// better. It is near the preferred size when the profile has one for the
// quality, smaller with PreferSmaller, else bigger. Unknown sizes rank last.
func (p Profile) SizePreference(t Tier, size int64) int {
	if size <= 0 {
		return math.MinInt32
	}
	gb := float64(size) / gib
	if s, ok := p.TierSizes[t]; ok && s.PreferredGB > 0 {
		return -int(math.Abs(gb-s.PreferredGB) * 100)
	}
	if p.PreferSmaller {
		return -int(gb * 100)
	}
	return 0 // the indexer's priority and protocol decide, as before
}

// ---- upgrade window ----

// WithinUpgradeWindow reports whether a title released on released
// ("YYYY-MM-DD", "" when unknown) may still be upgraded, today being today.
func (p Profile) WithinUpgradeWindow(released, today string) bool {
	if p.UpgradeDays <= 0 || len(released) < 10 || len(today) < 10 {
		return true
	}
	r, err1 := parseDay(released[:10])
	n, err2 := parseDay(today[:10])
	if err1 != nil || err2 != nil {
		return true
	}
	return n-r <= p.UpgradeDays
}

// parseDay turns "YYYY-MM-DD" into a day number.
func parseDay(s string) (int, error) {
	var y, m, d int
	if _, err := fmt.Sscanf(s, "%d-%d-%d", &y, &m, &d); err != nil {
		return 0, err
	}
	// days from civil (Howard Hinnant)
	if m <= 2 {
		y--
	}
	era := y / 400
	yoe := y - era*400
	mp := (m + 9) % 12
	doy := (153*mp+2)/5 + d - 1
	doe := yoe*365 + yoe/4 - yoe/100 + doy
	return era*146097 + doe, nil
}

// ---- patterns ----

// A term written between slashes, like /\bAMZN\b/, is a pattern matched
// against the release name as it is, ignoring capitals.
func isPattern(term string) bool {
	return len(term) >= 3 && strings.HasPrefix(term, "/") && strings.HasSuffix(term, "/")
}

var patterns sync.Map // term -> *regexp.Regexp (nil when it doesn't compile)

func patternOf(term string) *regexp.Regexp {
	if v, ok := patterns.Load(term); ok {
		re, _ := v.(*regexp.Regexp)
		return re
	}
	re, err := regexp.Compile("(?i)" + term[1:len(term)-1])
	if err != nil {
		re = nil
	}
	patterns.Store(term, re)
	return re
}

// checkPattern reports why a term written as a pattern can't be used.
func checkPattern(term string) error {
	if !isPattern(term) {
		return nil
	}
	if _, err := regexp.Compile("(?i)" + term[1:len(term)-1]); err != nil {
		return fmt.Errorf("%w: %s isn't a pattern Usherr can read: %v", ErrInvalid, term, err)
	}
	return nil
}

// matchTerm is containsTerm with patterns: title is the release name as it
// is, norm the name normalized for plain terms.
func matchTerm(title, norm, term string) bool {
	if isPattern(term) {
		re := patternOf(term)
		return re != nil && re.MatchString(title)
	}
	return containsTerm(norm, term)
}
