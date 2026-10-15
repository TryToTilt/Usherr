package comics

// Reading comic release names and file names, and naming the files Mediarium
// writes. The layout matches what Kapowarr wrote, so an existing library
// keeps its folders:
//
//	<Series>/Volume 01 (<Year>)/<Series> (<Year>) Volume 01 Issue 001.cbz

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Name is what a release or file name says.
type Name struct {
	Series     string
	Number     string // "" when no issue number was found
	Year       int
	Volume     int
	Collection bool // TPB, omnibus, "Vol. 3" without an issue: not a single issue
}

var (
	yearParen   = regexp.MustCompile(`[\(\[](\d{4})[\)\]]`)
	yearToken   = regexp.MustCompile(`(?:^|[\s._\-])((?:19[3-9]|20\d)\d)(?:[\s._\-]|$)`)
	bracketTag  = regexp.MustCompile(`[\(\[][^\)\]]*[\)\]]`)
	issueWord   = regexp.MustCompile(`(?i)\b(?:issue|no\.?)\s*#?\s*(\d+(?:\.\d+|[.·]now)?|½)`)
	hashNum     = regexp.MustCompile(`(?i)#\s*(\d+(?:\.\d+|[.·]now)?|½)`)
	volWord     = regexp.MustCompile(`(?i)\b(?:volume|vol\.?|v)\s*(\d{1,3})\b`)
	plainNumber = regexp.MustCompile(`(?i)^\d{1,4}(?:\.\d{1,2}|[.·]now)?$`)
	nowDot      = regexp.MustCompile(`(?i)(\d)\.now\b`) // "015.NOW", kept whole when dots are spaces
	nowTitle    = regexp.MustCompile(`(?i)^\s*\.now\b[\s:,-]*`)
	collection  = regexp.MustCompile(`(?i)\b(tpb|trade paperback|omnibus|hc|hardcover|collection|compendium|deluxe edition|absolute edition)\b`) // home fork: "Absolute Carnage" and "Absolute Batman" are series names; only an Absolute Edition is a collection
	comicExt    = regexp.MustCompile(`(?i)\.(cbz|cbr|cb7|cbt|pdf|epub|zip|rar|nzb)$`)
	nonWord     = regexp.MustCompile(`[^a-z0-9]+`)
)

// ParseName reads a release or file name.
func ParseName(name string) Name {
	s := comicExt.ReplaceAllString(strings.TrimSpace(name), "")
	if !strings.Contains(s, " ") {
		s = nowDot.ReplaceAllString(s, "${1}·now")
		s = strings.NewReplacer(".", " ", "_", " ").Replace(s)
	} else {
		s = strings.ReplaceAll(s, "_", " ")
	}
	var n Name
	if m := yearParen.FindStringSubmatch(s); m != nil {
		n.Year, _ = strconv.Atoi(m[1])
	}
	n.Collection = collection.MatchString(s)
	s = bracketTag.ReplaceAllString(s, " ")
	if m := volWord.FindStringSubmatch(s); m != nil {
		n.Volume, _ = strconv.Atoi(m[1])
	}
	cut := -1
	if m := issueWord.FindStringSubmatchIndex(s); m != nil {
		n.Number, cut = s[m[2]:m[3]], m[0]
	} else if m := hashNum.FindStringSubmatchIndex(s); m != nil {
		n.Number, cut = s[m[2]:m[3]], m[0]
	} else {
		// No "#" or "Issue": the first number that isn't a year is the
		// issue; what comes before it is the series ("Spider-Man 2099 001").
		rest := strings.Fields(volWord.ReplaceAllString(s, " "))
		for k, tok := range rest {
			if !plainNumber.MatchString(tok) || k == 0 {
				continue
			}
			if len(tok) == 4 && yearLike(tok) && k+1 < len(rest) && plainNumber.MatchString(rest[k+1]) {
				continue // a year or a number in the title, the issue follows
			}
			if len(tok) == 4 && yearLike(tok) {
				if n.Year == 0 {
					n.Year, _ = strconv.Atoi(tok)
				}
				rest = rest[:k]
				break
			}
			n.Number = tok
			for _, after := range rest[k+1:] {
				if len(after) == 4 && yearLike(after) && n.Year == 0 {
					n.Year, _ = strconv.Atoi(after)
				}
			}
			rest = rest[:k]
			break
		}
		s, cut = strings.Join(rest, " "), -1
	}
	series := s
	if cut >= 0 {
		series = s[:cut]
	}
	series = volWord.ReplaceAllString(series, " ")
	series = collection.ReplaceAllString(series, " ")
	// A year left in the name without brackets ("Batman 2016 001").
	if f := strings.Fields(series); len(f) > 1 {
		if v, err := strconv.Atoi(f[len(f)-1]); err == nil && v >= 1930 && v <= 2100 && n.Year == 0 {
			n.Year = v
			series = strings.Join(f[:len(f)-1], " ")
		}
	}
	n.Series = strings.Trim(strings.Join(strings.Fields(series), " "), " -–:,")
	if n.Year == 0 { // a bare year anywhere after the series ("… 001 2025 digital")
		rest := strings.TrimPrefix(s, series)
		if m := yearToken.FindStringSubmatch(rest); m != nil {
			n.Year, _ = strconv.Atoi(m[1])
		}
	}
	if n.Number != "" {
		n.Collection = false
	} else if n.Volume > 0 {
		n.Collection = true
	}
	return n
}

func yearLike(tok string) bool {
	v, err := strconv.Atoi(tok)
	return err == nil && v >= 1930 && v <= 2100
}

// Key normalises a series name for comparing: lower case, letters and
// digits only, without a leading "the", "&" as "and".
func Key(name string) string {
	s := strings.ToLower(strings.ReplaceAll(name, "&", " and "))
	s = strings.TrimPrefix(strings.TrimSpace(s), "the ")
	return nonWord.ReplaceAllString(s, "")
}

// AltNumber is the number an issue was printed with when ComicVine numbers
// it differently: Marvel's ".NOW" issues ("Captain America #15.NOW") are
// listed as the next whole issue with ".NOW" at the front of the title.
// "" when there's none.
func AltNumber(i Issue) string {
	if !nowTitle.MatchString(i.Title) {
		return ""
	}
	v, err := strconv.Atoi(NormalNumber(i.Number))
	if err != nil || v < 2 {
		return ""
	}
	return strconv.Itoa(v-1) + ".now"
}

// DisplayTitle is an issue's title without ComicVine's ".NOW" prefix.
func DisplayTitle(i Issue) string { return strings.TrimSpace(nowTitle.ReplaceAllString(i.Title, "")) }

// Matches reports whether a release is issue i of series s.
func Matches(n Name, s Series, i Issue) bool {
	if n.Collection || n.Number == "" {
		return false
	}
	if alt := AltNumber(i); alt != "" {
		// Only the printed ".NOW" number: a plain number is another issue.
		if NormalNumber(n.Number) != alt {
			return false
		}
	} else if NormalNumber(n.Number) != NormalNumber(i.Number) {
		return false
	}
	if Key(n.Series) != Key(s.Name) {
		return false
	}
	if n.Year > 0 {
		// Releases carry the issue's cover or store year, or the series'
		// first year ("Batman 050 (2016)" for a 2018 issue of the 2016 run).
		ok := n.Year == s.StartYear
		for _, d := range []string{i.CoverDate, i.StoreDate} {
			if len(d) >= 4 && d[:4] == strconv.Itoa(n.Year) {
				ok = true
			}
		}
		if !ok && issueYear(i) == 0 && s.StartYear == 0 {
			ok = true
		}
		if !ok {
			return false
		}
	}
	return true
}

// YearDistance is how far a release's year is from the issue's (0 is best;
// 9 when either is unknown), for ranking matches.
func YearDistance(n Name, i Issue) int {
	y := issueYear(i)
	if n.Year == 0 || y == 0 {
		return 9
	}
	d := n.Year - y
	if d < 0 {
		d = -d
	}
	return d
}

// IsReprint spots facsimile editions and other reprints of an issue.
func IsReprint(title string) bool { return reprint.MatchString(title) }

var reprint = regexp.MustCompile(`(?i)\b(facsimile|true believers|reprint|2nd print|second printing|anniversary edition)\b`)

func issueYear(i Issue) int {
	for _, d := range []string{i.StoreDate, i.CoverDate} {
		if len(d) >= 4 {
			if y, err := strconv.Atoi(d[:4]); err == nil {
				return y
			}
		}
	}
	return 0
}

// SearchQueries are the indexer searches for an issue, most specific first.
func SearchQueries(s Series, i Issue) []string {
	num := NormalNumber(i.Number)
	padded := num
	if v, err := strconv.Atoi(num); err == nil {
		padded = fmt.Sprintf("%03d", v)
	}
	name := strings.Join(strings.Fields(nonWordKeepSpace.ReplaceAllString(s.Name, " ")), " ")
	if alt := AltNumber(i); alt != "" {
		v, _ := strconv.Atoi(strings.TrimSuffix(alt, ".now"))
		return []string{fmt.Sprintf("%s %03d.NOW", name, v), fmt.Sprintf("%s %d.NOW", name, v), fmt.Sprintf("%s %d NOW", name, v)}
	}
	qs := []string{name + " " + padded}
	if padded != num {
		qs = append(qs, name+" "+num)
	}
	return qs
}

var nonWordKeepSpace = regexp.MustCompile(`[^\p{L}\p{N}' ]+`)

// Folder is a series' folder under the comics root, when it has none yet.
func Folder(s Series) string {
	name := strings.TrimSpace(s.Name)
	if s.StartYear > 0 {
		return filepath.Join(name, fmt.Sprintf("Volume 01 (%d)", s.StartYear))
	}
	return filepath.Join(name, "Volume 01")
}

// FileName is an issue's file name, without extension.
func FileName(s Series, i Issue) string {
	num := NormalNumber(i.Number)
	if v, err := strconv.Atoi(num); err == nil {
		num = fmt.Sprintf("%03d", v)
	}
	if s.StartYear > 0 {
		return fmt.Sprintf("%s (%d) Volume 01 Issue %s", s.Name, s.StartYear, num)
	}
	return fmt.Sprintf("%s Volume 01 Issue %s", s.Name, num)
}

// IsComicFile reports whether a file name is a comic.
func IsComicFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".cbz", ".cbr", ".cb7", ".cbt", ".pdf":
		return true
	}
	return false
}
