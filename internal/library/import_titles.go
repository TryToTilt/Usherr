package library

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	// The season and episode part of a name (S22E41, S01E01E02, S01E01-E03).
	titleSeasonEpisode = regexp.MustCompile(`(?i)\bS\d{1,3}[ ._-]?E\d{1,4}(?:[ ._-]?(?:E|-E?)\d{1,4})*`)
	// Release tags that follow a title: [WEBDL-1080p], {tag}, (tag).
	titleTrailingTag = regexp.MustCompile(`\s*(\[[^\[\]]*\]|\{[^{}]*\}|\([^()]*\))\s*$`)
	// A release group after a dash: "-GROUP".
	titleTrailingGroup = regexp.MustCompile(`\s?-\s?[A-Za-z0-9]+$`)
	// A language or subtitle tag before the extension: ".en", ".pt-BR".
	titleLanguageTag = regexp.MustCompile(`(?i)\.[a-z]{2,3}(-[a-z]{2,4})?$`)
	// Titles that say nothing: "Episode 41", "41".
	titleGeneric = regexp.MustCompile(`^((episode|ep|folge|part|chapter|capitulo|episodio|aflevering) )?\d+$`)
)

// titleKey normalises an episode title to compare it with another: lower
// case, "&" read as "and", apostrophes dropped, every other punctuation mark a
// space, spaces collapsed. Empty for a title that is too short or too generic
// to tell an episode apart.
func titleKey(title string) string {
	var b strings.Builder
	for _, c := range strings.ToLower(title) {
		switch {
		case c == '&':
			b.WriteString(" and ")
		case c == '\'' || c == '’' || c == '`':
		case unicode.IsLetter(c) || unicode.IsDigit(c):
			b.WriteRune(c)
		default:
			b.WriteByte(' ')
		}
	}
	key := strings.Join(strings.Fields(b.String()), " ")
	if len([]rune(key)) < 4 || titleGeneric.MatchString(key) {
		return ""
	}
	return key
}

// fileTitleKeys are the normalised titles a file name may carry: what follows
// the season and episode, as it is and with the language tag, release tags and
// group taken off the end. Empty when the name has no title.
func fileTitleKeys(path string) []string {
	name := path
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	if i := strings.LastIndex(name, "."); i > 0 && i >= len(name)-6 && !strings.Contains(name[i:], " ") {
		name = name[:i]
	}
	loc := titleSeasonEpisode.FindStringIndex(name)
	if loc == nil {
		return nil
	}
	tail := strings.Trim(name[loc[1]:], " -_.:")
	if tail == "" {
		return nil
	}
	var out []string
	add := func(t string) {
		k := titleKey(t)
		if k == "" {
			return
		}
		for _, o := range out {
			if o == k {
				return
			}
		}
		out = append(out, k)
	}
	for _, t := range []string{tail, titleLanguageTag.ReplaceAllString(tail, "")} {
		add(t)
		// Take the tags and the group off the end, in whatever order they come.
		for {
			next := titleTrailingTag.ReplaceAllString(t, "")
			if next == t {
				next = titleTrailingGroup.ReplaceAllString(t, "")
			}
			if next == t {
				break
			}
			t = next
			add(t)
		}
	}
	return out
}

// matchTitle is the one episode of the show whose title the file name carries
// exactly (after normalising). It answers only when one episode does: a title
// that two episodes share, or several readings of the name that point at
// different episodes, are no match.
func (r *importRules) matchTitle(f ImportFile) ([2]int, bool) {
	if len(r.titles) == 0 {
		return [2]int{}, false
	}
	var found [2]int
	n := 0
	for _, key := range fileTitleKeys(f.Path) {
		eps := r.titles[key]
		if len(eps) != 1 {
			continue
		}
		if n > 0 && eps[0] != found {
			return [2]int{}, false
		}
		found, n = eps[0], 1
	}
	return found, n == 1
}

// resolveByTitle is the last rule, for an anime named by TheTVDB's seasons
// that nothing else placed: the file's title is exactly one episode's title,
// in the whole show, and no other rule or file has that episode.
func (r *importRules) resolveByTitle(f ImportFile) (importTarget, bool) {
	if !r.anime || f.Season <= 0 || len(f.Episodes) != 1 {
		return importTarget{}, false
	}
	m, ok := r.matchTitle(f)
	if !ok || r.claimed[m] || r.taken[m] || r.titleCount[m] > 1 {
		return importTarget{}, false
	}
	return importTarget{season: m[0], episodes: []int{m[1]}}, true
}

// calibrate reads the titles in the file names against the rules that count
// by season. A season where a title says another episode than the offset rule
// or the file-set rule would pick is one that TheTVDB starts somewhere else
// (One Piece's season 22 starts three episodes before the movie database's),
// so those rules are off for it. It then notes which episodes the other rules
// fill and which ones several files claim by title.
func (r *importRules) calibrate(files []ImportFile) {
	if len(r.titles) == 0 {
		return
	}
	r.offsetBad, r.inferBad = map[int]bool{}, map[int]bool{}
	for _, f := range files {
		if f.Season <= 0 || len(f.Episodes) != 1 {
			continue
		}
		m, ok := r.matchTitle(f)
		if !ok {
			continue
		}
		if t, ok := r.offsetTarget(f); ok && (t.season != m[0] || t.episodes[0] != m[1]) {
			r.offsetBad[f.Season] = true
		}
		if t, ok := r.inferTarget(f); ok && (t.season != m[0] || t.episodes[0] != m[1]) {
			r.inferBad[f.Season] = true
		}
	}
	r.taken, r.titleCount = map[[2]int]bool{}, map[[2]int]int{}
	for _, f := range files {
		if t, ok, _ := r.resolveStrong(f); ok {
			for _, n := range t.episodes {
				r.taken[[2]int{t.season, n}] = true
			}
			continue
		}
		if f.Season > 0 && len(f.Episodes) == 1 {
			if m, ok := r.matchTitle(f); ok {
				r.titleCount[m]++
			}
		}
	}
}
