// Package releaselang spots a book or comic release in another language by the
// tags in its name ("Japanese", "raw", "ger", "T25"). Comics and Manga both ask.
package releaselang

import (
	"regexp"
	"strings"
)

// Only words that stand for a language, between separators: "raw" inside
// "Strawberry" isn't one, and short codes that are also ordinary words ("pol",
// "ara", "chi", "ind") are left out, as a wrong rejection costs a good release.
var tag = regexp.MustCompile(`(?i)(?:^|[\s._\-\[\(])(japanese|jpn|jap|raw|raws|korean|kor|chinese|chs|cht|ger|german|deutsch|fre|french|fra|francais|français|vostfr|ita|italian|italiano|spa|esp|spanish|espanol|español|castellano|latino|dut|dutch|ned|nl|por|portuguese|pt-?br|russian|rus|polish|polski|turkish|arabic|vietnamese|thai|indonesian|bahasa)(?:$|[\s._\-\]\)])`)

// "T25", "Tome 25": how French editions number their volumes. Single digits are
// left to Tome(): "Terminator T2" is a title.
var (
	tome    = regexp.MustCompile(`(?:^|[\s._\-\[\(])T\.?\d{2,3}(?:$|[\s._\-\]\)])`)
	tomeAny = regexp.MustCompile(`(?:^|[\s._\-\[\(])T\.?\d{1,3}(?:$|[\s._\-\]\)])`)
	tomeTag = regexp.MustCompile(`(?i)(?:^|[\s._\-\[\(])(tome|tomo|tomes)\s*\.?\s*\d{1,3}(?:$|[\s._\-\]\)])`)
)

var names = map[string]string{
	"japanese": "Japanese", "jpn": "Japanese", "jap": "Japanese", "raw": "Japanese", "raws": "Japanese",
	"korean": "Korean", "kor": "Korean",
	"chinese": "Chinese", "chs": "Chinese", "cht": "Chinese",
	"ger": "German", "german": "German", "deutsch": "German",
	"fre": "French", "french": "French", "fra": "French", "francais": "French", "français": "French", "vostfr": "French",
	"ita": "Italian", "italian": "Italian", "italiano": "Italian",
	"spa": "Spanish", "esp": "Spanish", "spanish": "Spanish", "espanol": "Spanish", "español": "Spanish", "castellano": "Spanish", "latino": "Spanish",
	"dut": "Dutch", "dutch": "Dutch", "ned": "Dutch", "nl": "Dutch",
	"por": "Portuguese", "portuguese": "Portuguese", "pt-br": "Portuguese", "ptbr": "Portuguese",
	"russian": "Russian", "rus": "Russian",
	"polish": "Polish", "polski": "Polish",
	"turkish": "Turkish", "arabic": "Arabic", "vietnamese": "Vietnamese", "thai": "Thai",
	"indonesian": "Indonesian", "bahasa": "Indonesian",
}

// Other names the other language a release's title says it's in, or "" when it
// says none. A tag that's also in the series' own name doesn't count ("Raw Hero",
// "The French Dispatch").
func Other(title, seriesName string) string {
	m := tag.FindStringSubmatch(title)
	if m != nil && !tag.MatchString(seriesName) {
		return names[strings.ToLower(m[1])]
	}
	if tome.MatchString(title) && !tome.MatchString(seriesName) {
		return "French"
	}
	if tomeTag.MatchString(title) && !tomeTag.MatchString(seriesName) {
		return "French or Spanish"
	}
	return ""
}

// OtherVolume is Other for a name counted in volumes, where even "T2" is a
// foreign edition's number.
func OtherVolume(title, seriesName string) string {
	if lang := Other(title, seriesName); lang != "" {
		return lang
	}
	if tomeAny.MatchString(title) && !tomeAny.MatchString(seriesName) {
		return "French"
	}
	return ""
}
