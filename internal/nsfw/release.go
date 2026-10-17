package nsfw

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Video releases are named "<Studio>.<YY>.<MM>.<DD>.<Performers or
// title>.XXX.<quality>-<GROUP>", for example
// "AcmeStudios.24.03.15.Jane.Doe.And.John.Smith.XXX.1080p.MP4-WRB".
// Some use a four-digit year, and a few "<Studio> - 2024-03-15 - <Title>".

// Release is what a video release name says about itself.
type Release struct {
	Studio string    // compacted studio words before the date ("acmestudios")
	Date   time.Time // zero when the name has no date
	Words  []string  // lower-case words after the date, up to the quality tags
	Height int       // 2160, 1080, 720, 480, or 0 when not given
}

var resolutionRe = regexp.MustCompile(`^(\d{3,4})p$`)

// stopWords end the descriptive part of a release name.
var stopWords = map[string]bool{
	"xxx": true, "mp4": true, "mkv": true, "wmv": true, "web": true, "webrip": true, "web-dl": true,
	"hevc": true, "x264": true, "x265": true, "h264": true, "h265": true, "4k": true, "uhd": true, "sd": true, "hd": true,
}

// splitWords breaks a release name into lower-case words on dots, spaces,
// underscores, dashes and brackets.
func splitWords(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return r == '.' || r == ' ' || r == '_' || r == '-' || r == '[' || r == ']' || r == '(' || r == ')' || r == '+' || r == ','
	})
}

// compact keeps only letters and digits, lower-cased: "Acme Studios" and
// "AcmeStudios" both become "acmestudios".
func compact(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isNum(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// dateAt reads a date from words[i:i+3] (YY MM DD or YYYY MM DD).
func dateAt(words []string, i int) (time.Time, bool) {
	if i+2 >= len(words) {
		return time.Time{}, false
	}
	y, m, d := words[i], words[i+1], words[i+2]
	if !(isNum(y, 2) || isNum(y, 4)) || !isNum(m, 2) || !isNum(d, 2) {
		return time.Time{}, false
	}
	year, _ := strconv.Atoi(y)
	if len(y) == 2 {
		year += 2000
		if year > time.Now().Year()+1 {
			year -= 100
		}
	}
	t, err := time.Parse("2006-01-02", fmt.Sprintf("%04d-%s-%s", year, m, d))
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// ParseRelease reads the studio, date, words and height of a release name.
func ParseRelease(name string) Release {
	// Drop the group ("-WRB") at the end before splitting on dashes.
	if i := strings.LastIndex(name, "-"); i > 0 && i > len(name)-16 && !strings.ContainsAny(name[i:], " .") {
		name = name[:i]
	}
	words := splitWords(name)
	var r Release
	dateIdx := -1
	for i := range words {
		if t, ok := dateAt(words, i); ok {
			r.Date, dateIdx = t, i
			break
		}
	}
	end := len(words)
	for i, w := range words {
		if m := resolutionRe.FindStringSubmatch(w); m != nil {
			if h, _ := strconv.Atoi(m[1]); h >= 240 && h <= 4320 {
				r.Height = h
				if i < end {
					end = i
				}
			}
		}
		if stopWords[w] && i < end && i > dateIdx {
			end = i
		}
	}
	if r.Height == 0 {
		for _, w := range words {
			if w == "4k" || w == "uhd" {
				r.Height = 2160
			}
		}
	}
	if dateIdx >= 0 {
		r.Studio = compact(strings.Join(words[:dateIdx], ""))
		if dateIdx+3 < end {
			r.Words = words[dateIdx+3 : end]
		}
	} else if end > 0 {
		r.Words = words[:end]
	}
	return r
}

// Quality is a short label for the release's resolution.
func (r Release) Quality() string {
	switch {
	case r.Height >= 2160:
		return "2160p"
	case r.Height >= 1080:
		return "1080p"
	case r.Height >= 720:
		return "720p"
	case r.Height > 0:
		return fmt.Sprintf("%dp", r.Height)
	}
	return ""
}

// studioMatches reports whether a release's studio is the video's studio or
// one of its aliases. A release may also use a longer form of the name
// ("AcmeStudios" for "Acme Studios") or the network for a sub-site.
func studioMatches(releaseStudio string, s Video) bool {
	if releaseStudio == "" {
		return false
	}
	names := append([]string{s.StudioName}, s.StudioAliases...)
	for _, n := range names {
		c := compact(n)
		if c == "" {
			continue
		}
		if c == releaseStudio {
			return true
		}
		// "acmestudios" vs "acme": allow a prefix only when the
		// shorter name is distinctive enough not to match by accident.
		short, long := c, releaseStudio
		if len(short) > len(long) {
			short, long = long, short
		}
		if len(short) >= 6 && strings.HasPrefix(long, short) {
			return true
		}
	}
	return false
}

// containsRun reports whether words contains name's words in a row.
func containsRun(words []string, name string) bool {
	nw := splitWords(name)
	if len(nw) == 0 || len(nw) > len(words) {
		return false
	}
	for i := 0; i+len(nw) <= len(words); i++ {
		ok := true
		for j := range nw {
			if words[i+j] != nw[j] {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// MatchScore says how well a release fits a video: 0 means it doesn't. The
// studio and the date must agree (one day either way, as sites and posters
// sometimes date a video differently); performers or title words then add
// confidence, which matters when a studio put out several videos that day.
func MatchScore(r Release, s Video) int {
	if r.Date.IsZero() || s.ReleaseDate == "" || !studioMatches(r.Studio, s) {
		return 0
	}
	sd, err := time.Parse("2006-01-02", s.ReleaseDate)
	if err != nil {
		return 0
	}
	diff := r.Date.Sub(sd)
	if diff < 0 {
		diff = -diff
	}
	if diff > 24*time.Hour {
		return 0
	}
	score := 10
	if diff == 0 {
		score += 5
	}
	for _, p := range s.Performers {
		if containsRun(r.Words, p.Name) || (p.As != "" && containsRun(r.Words, p.As)) {
			score += 10
		}
	}
	titleWords := splitWords(s.Title)
	if len(titleWords) > 0 {
		hit := 0
		set := map[string]bool{}
		for _, w := range r.Words {
			set[w] = true
		}
		for _, w := range titleWords {
			if len(w) > 2 && set[w] {
				hit++
			}
		}
		score += 10 * hit / len(titleWords)
	}
	return score
}

// Matches reports whether a release is for the video, given the other
// videos of the same studio on the same days (siblings), so that a release
// naming only the studio and date isn't taken for the wrong one.
func Matches(r Release, s Video, siblings []Video) bool {
	score := MatchScore(r, s)
	if score == 0 {
		return false
	}
	for _, o := range siblings {
		if o.StashID == s.StashID {
			continue
		}
		if MatchScore(r, o) >= score {
			return false // as good a fit for another video: not sure enough
		}
	}
	return true
}

// QualityRank orders releases: higher is better. Releases without a
// resolution rank lowest but still count.
func QualityRank(r Release) int {
	switch {
	case r.Height >= 2160:
		return 4
	case r.Height >= 1080:
		return 3
	case r.Height >= 720:
		return 2
	case r.Height > 0:
		return 1
	}
	return 0
}

// SearchQueries are the indexer searches for a video, most specific first:
// "<studio> <yy> <mm> <dd>", then the studio with the first performer.
func SearchQueries(s Video) []string {
	studio := compact(s.StudioName)
	var out []string
	if d, err := time.Parse("2006-01-02", s.ReleaseDate); err == nil && studio != "" {
		out = append(out, fmt.Sprintf("%s %s", studio, d.Format("06 01 02")))
	}
	if studio != "" && len(s.Performers) > 0 {
		out = append(out, strings.TrimSpace(studio+" "+s.Performers[0].Name))
	}
	if len(out) == 0 && s.Title != "" {
		out = append(out, s.Title)
	}
	return out
}

// FileName is the library file name for a video, without extension:
// "<Studio> - <YYYY-MM-DD> - <Title> [<Performers>]".
func FileName(s Video) string {
	var names []string
	for i, p := range s.Performers {
		if i == 4 {
			names = append(names, "et al")
			break
		}
		names = append(names, p.Name)
	}
	n := s.Name()
	if len(names) > 0 {
		n += " [" + strings.Join(names, ", ") + "]"
	}
	if len(n) > 180 {
		n = strings.TrimSpace(n[:180])
	}
	return n
}

// Folder is the video's folder below the library root: the studio's name.
func Folder(s Video) string {
	if strings.TrimSpace(s.StudioName) == "" {
		return "Unknown studio"
	}
	return s.StudioName
}
