package download

// Usherr: choosing the right video in a release. A film's release has one that
// matters: the biggest. A season pack holds many, and playing the biggest would
// play the wrong episode, so for an episode the release's files are matched to
// it by name (the name in the file's yEnc header or the post's subject, or the
// file inside a RAR set) and by what the file itself says at its start. When the
// names say nothing (an obfuscated post), the order of the files and their sizes
// are the only clue, and they're used only if everything about them adds up.
//
// Anything less than that is ErrNotInRelease: the next release gets a turn. A
// wrong episode that plays is worse than a release skipped.

import (
	"errors"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/rdborg/mediarium/internal/parser"
)

// ErrNotInRelease means the release doesn't clearly hold what's wanted.
var ErrNotInRelease = errors.New("this release doesn't clearly hold that episode")

// StreamWant says what's wanted from a release. The zero value wants its
// biggest video.
type StreamWant struct {
	InnerFile string // the video's name, kept from an earlier open
	Season    int
	Episode   int    // 0: not an episode
	AirDate   string // "2026-10-16", for a daily show
	Release   string // the release's title
	// SeasonEpisodes is how many episodes the season has, when known. Only with
	// it can the place of a file in an obfuscated pack say which episode it is.
	SeasonEpisodes int
}

func (w StreamWant) wantsEpisode() bool { return w.Episode > 0 || w.AirDate != "" }

// A name's answer to "is this the episode?".
type nameSays int

const (
	saysNothing nameSays = iota
	saysYes
	saysNo
)

func baseName(name string) string {
	name = strings.ReplaceAll(name, `\`, "/")
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	return name
}

// says reads a file's or a release's name for the episode wanted.
func (w StreamWant) says(name string) nameSays {
	r := parser.Parse(baseName(name))
	eps := r.Episodes
	if len(eps) == 0 && r.Episode > 0 {
		eps = []int{r.Episode}
	}
	switch {
	case r.AirDate != "":
		if w.AirDate == "" {
			return saysNothing
		}
		if r.AirDate == w.AirDate {
			return saysYes
		}
		return saysNo
	case len(eps) > 0:
		if w.Episode > 0 && r.Season == w.Season && slices.Contains(eps, w.Episode) {
			return saysYes
		}
		if w.Episode == 0 {
			return saysNothing // a daily show posted by season and episode: nothing to compare
		}
		return saysNo
	case len(r.Absolute) > 0 && w.Episode > 0:
		if slices.Contains(r.Absolute, w.Episode) {
			return saysYes
		}
		return saysNo
	}
	return saysNothing
}

// releaseIsPack says whether a release's title names a whole season of the
// wanted episode's show, with no episode.
func (w StreamWant) releaseIsPack() bool {
	if w.Release == "" {
		return false
	}
	r := parser.Parse(w.Release)
	return r.Season == w.Season && r.Season > 0 && r.Episode == 0 && len(r.Episodes) == 0 && r.SeasonTo == 0
}

// videoCand is a video found in a release: a file of the NZB or a file in a RAR
// set.
type videoCand struct {
	name    string // the name it's played under
	subject string // the post's file name, "" for a file in a RAR set
	size    int64
	head    []byte // its first bytes, as far as they've been read
	post    int    // its place in the post's own count ("[3/24]"), else 0
	order   int    // its place in the NZB
	file    *StreamFile
}

var (
	sampleName = regexp.MustCompile(`(?i)(^|[^a-z0-9])sample([^a-z0-9]|$)`)
	// An episode's name, as a muxer leaves it in a Matroska title or an MP4 tag.
	headEpisode = regexp.MustCompile(`(?i)\bS\d{1,2}E\d{1,3}\b`)
	postCount   = regexp.MustCompile(`\[(\d+)/(\d+)\]`)
	digitsOnly  = regexp.MustCompile(`^\d+$`)
)

// postPlace reads a post's own count from its subject: 3 from `[3/24] "x"`.
func postPlace(subject string) int {
	if m := postCount.FindStringSubmatch(subject); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil {
			return n
		}
	}
	return 0
}

// pickVideo chooses among a release's videos. missing says some files of the
// release couldn't be opened because their first article is gone, and how many
// bytes they would hold.
func pickVideo(cands []videoCand, w StreamWant, missing int64, narrowed bool) (*videoCand, error) {
	notThere := func(reason error) (*videoCand, error) {
		if missing > 0 {
			return nil, ErrArticleMissing // it may well have been one of those
		}
		return nil, reason
	}
	if len(cands) == 0 {
		return notThere(errors.New("no video in the release"))
	}
	biggest := func(list []*videoCand) *videoCand {
		best := list[0]
		for _, c := range list[1:] {
			if c.size > best.size {
				best = c
			}
		}
		return best
	}
	all := make([]*videoCand, len(cands))
	for i := range cands {
		all[i] = &cands[i]
	}
	// A name kept from before, unless the name itself says it's another episode
	// (kept by a version that always took a pack's biggest video).
	if w.InnerFile != "" {
		for _, c := range all {
			if strings.EqualFold(baseName(c.name), baseName(w.InnerFile)) && (!w.wantsEpisode() || w.says(c.name) != saysNo) {
				return c, nil
			}
		}
	}
	if !w.wantsEpisode() {
		best := biggest(all)
		if missing > best.size {
			return nil, ErrArticleMissing // the real video is among those that won't open
		}
		return best, nil
	}

	// The episode: by name first.
	var yes, unknown []*videoCand
	saidNo := false
	for _, c := range all {
		if sampleName.MatchString(baseName(c.name)) {
			continue
		}
		says := w.says(c.name)
		if says == saysNothing && c.subject != "" {
			says = w.says(c.subject)
		}
		if says == saysNothing {
			says = w.saysInHead(c.head)
		}
		switch says {
		case saysYes:
			yes = append(yes, c)
		case saysNo:
			saidNo = true
		default:
			unknown = append(unknown, c)
		}
	}
	if len(yes) > 0 {
		return biggest(yes), nil
	}
	if len(unknown) == 0 {
		return notThere(ErrNotInRelease)
	}
	// Some files name other episodes and these name nothing: they're extras or
	// something else, not the episode.
	if saidNo {
		return notThere(ErrNotInRelease)
	}
	// Nothing names anything. Small files don't count: a release's extras.
	top := biggest(unknown).size
	var big []*videoCand
	for _, c := range unknown {
		if c.size >= top/5 {
			big = append(big, c)
		}
	}
	// The release is this one episode, or there's only one file it can be.
	pack := w.releaseIsPack()
	if w.Release != "" && w.says(w.Release) == saysYes && missing == 0 {
		return biggest(big), nil
	}
	if len(big) == 1 && missing == 0 && (!pack || w.SeasonEpisodes == 1 || narrowed) {
		return big[0], nil
	}
	if narrowed {
		return biggest(big), nil // the posts' names said these are the episode
	}
	if missing > 0 {
		return nil, ErrArticleMissing
	}
	if pack {
		if c := pickByOrder(big, w); c != nil {
			return c, nil
		}
	}
	return nil, ErrNotInRelease
}

// saysInHead looks for an episode's name in a file's first bytes.
func (w StreamWant) saysInHead(head []byte) nameSays {
	if w.Episode == 0 || len(head) == 0 {
		return saysNothing
	}
	found := headEpisode.FindAll(head[:min(len(head), 256<<10)], 8)
	out := saysNothing
	for _, m := range found {
		switch w.says(string(m) + ".mkv") {
		case saysYes:
			out = saysYes
		case saysNo:
			return saysNo // it names another: never mind what else it says
		}
	}
	return out
}

// pickByOrder picks an episode of a fully obfuscated season pack by its place:
// the 3rd video for episode 3. It answers only when everything adds up, nil
// otherwise:
//   - the release says it's the whole season, and holds exactly as many videos
//     as the season has episodes,
//   - their sizes are alike: a season, not a mix,
//   - their order means something: the posts number themselves, or the names
//     count up, or the NZB isn't simply sorted by those scrambled names (which
//     says nothing about the episodes).
func pickByOrder(list []*videoCand, w StreamWant) *videoCand {
	if w.Episode < 1 || w.SeasonEpisodes != len(list) || w.Episode > len(list) {
		return nil
	}
	smallest, largest := list[0].size, list[0].size
	for _, c := range list {
		smallest, largest = min(smallest, c.size), max(largest, c.size)
	}
	if largest > smallest*5/2 {
		return nil
	}
	ordered := append([]*videoCand(nil), list...)
	counted := true
	seen := map[int]bool{}
	for _, c := range ordered {
		if c.post == 0 || seen[c.post] {
			counted = false
			break
		}
		seen[c.post] = true
	}
	switch {
	case counted:
		sort.Slice(ordered, func(a, b int) bool { return ordered[a].post < ordered[b].post })
	case !orderMeansSomething(ordered):
		return nil
	default:
		sort.Slice(ordered, func(a, b int) bool { return ordered[a].order < ordered[b].order })
	}
	return ordered[w.Episode-1]
}

// orderMeansSomething says whether a list in NZB order can be read as posted:
// names that are plain numbers count up, and scrambled names in sorted order
// only show the NZB was sorted.
func orderMeansSomething(list []*videoCand) bool {
	inOrder := append([]*videoCand(nil), list...)
	sort.Slice(inOrder, func(a, b int) bool { return inOrder[a].order < inOrder[b].order })
	stem := func(c *videoCand) string {
		n := baseName(c.name)
		if i := strings.LastIndexByte(n, '.'); i > 0 {
			n = n[:i]
		}
		if i := strings.LastIndexByte(n, '.'); i > 0 { // a name given ".mkv" on top of its own
			n = n[:i]
		}
		return n
	}
	numbered, sortedByName := true, true
	last := -1
	for i, c := range inOrder {
		s := stem(c)
		if !digitsOnly.MatchString(s) {
			numbered = false
		} else if n, err := strconv.Atoi(s); err != nil || n <= last {
			numbered = false
		} else {
			last = n
		}
		if i > 0 && strings.ToLower(s) < strings.ToLower(stem(inOrder[i-1])) {
			sortedByName = false
		}
	}
	return numbered || !sortedByName
}

// narrowBySubject keeps only the files whose post names the episode, or the
// kept video, when any does: nothing else of a pack needs opening, and a bad
// article in another episode doesn't matter. RAR volumes count when their set is
// named after the episode.
func narrowBySubject(files []NZBFile, w StreamWant) ([]NZBFile, bool) {
	if !w.wantsEpisode() && w.InnerFile == "" {
		return files, false
	}
	var keep []NZBFile
	for _, f := range files {
		name := articleFilename(f.Subject, 0)
		if w.InnerFile != "" && strings.EqualFold(baseName(name), baseName(w.InnerFile)) && (!w.wantsEpisode() || w.says(name) != saysNo) {
			keep = append(keep, f)
			continue
		}
		if w.wantsEpisode() && w.says(name) == saysYes {
			keep = append(keep, f)
		}
	}
	if len(keep) == 0 {
		return files, false
	}
	return keep, true
}
