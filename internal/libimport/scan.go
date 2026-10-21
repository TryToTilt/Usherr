package libimport

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/internal/organizer"
	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/quality"
)

type Kind string

const (
	KindMovie Kind = "movie"
	KindTV    Kind = "tv"
)

// File is one video file found on disk. Season and Episodes are only set
// for TV.
type File struct {
	Path string `json:"path"`
	// Title is the title read from the file name ("" when the name has none).
	// Only ScanSeriesFolder fills it in.
	Title     string `json:"-"`
	SizeBytes int64  `json:"sizeBytes"`
	Season    int    `json:"season,omitempty"`
	Episodes  []int  `json:"episodes,omitempty"`
	// Absolute holds the episode numbers counted from the start of the show
	// that the file name carries: "One Piece - S23E10 - 1100 - Title" or
	// "[Group] One Piece - 1100". A file named only that way has no Season or
	// Episodes. Which episodes these are is settled against the show's own
	// episode list when the import runs.
	Absolute []int `json:"absolute,omitempty"`
	// AirDate is the day a daily show's episode aired, for a file named by
	// it ("Show - 2024-01-15 - Guest", "Show.2024.01.15.720p"), and DailyPart
	// the N of "Part N" after it. Such a file has no Season or Episodes: it is
	// placed on the episode that aired that day when the import runs.
	AirDate   string `json:"airDate,omitempty"`
	DailyPart int    `json:"dailyPart,omitempty"`
	Quality   string `json:"quality"`
	// ModTime is when the file was last changed; an import uses it as the
	// date the title was added. Zero when unknown.
	ModTime time.Time `json:"-"`
}

// Group is everything on disk that belongs to one movie or one series.
type Group struct {
	Key   string `json:"key"`
	Kind  Kind   `json:"kind"`
	Title string `json:"title"`
	Year  int    `json:"year"`
	Files []File `json:"files"`
}

// Result is a finished scan. Skipped lists video files that were found but
// could not be attributed to anything, with a short reason each.
type Result struct {
	Groups  []Group
	Skipped []string
}

var (
	// Plex-style extras live in these folders or carry these suffixes; none
	// of them is the main feature.
	extraDirs   = map[string]bool{"extras": true, "featurettes": true, "trailers": true, "behind the scenes": true, "deleted scenes": true, "interviews": true, "scenes": true, "shorts": true, "other": true, "sample": true, "samples": true, "subs": true, "subtitles": true}
	extraSuffix = regexp.MustCompile(`(?i)-(trailer|featurette|behindthescenes|deleted|interview|scene|short|other)$`)
	// "Season 1", "Season 01", "Specials", "S01".
	seasonFolder = regexp.MustCompile(`(?i)^(season[ ._-]*\d+|specials|s\d{1,2})$`)
)

type videoFile struct {
	path string
	rel  []string // path components relative to the scan root; the last is the file name
	size int64
	mod  time.Time
}

// The most video files a scan collects: of a whole library, and of one
// title's own folder (a folder with more is not one title's).
const (
	maxLibraryFiles = 500_000
	maxTitleFiles   = 20_000
)

// errTooManyFiles is returned when a scan finds more video files than its limit.
var errTooManyFiles = errors.New("too many video files")

func walkVideos(root string, limit int) ([]videoFile, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("scan folder: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("scan folder: %s is not a directory", root)
	}
	var out []videoFile
	// Symlinked folders (and a symlinked root) are followed, as libraries
	// spread over several drives often use them; visited holds the real
	// folders seen so far, so a link back up the tree is not a loop. Paths are
	// kept as they are under root, through the links.
	visited := map[string]bool{}
	var walk func(dir string) error
	walk = func(dir string) error {
		if real, err := filepath.EvalSymlinks(dir); err == nil {
			if visited[real] {
				return nil
			}
			visited[real] = true
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			if dir == root {
				return err // nothing at all could be read
			}
			return nil // an unreadable subfolder shouldn't abort the whole scan
		}
		for _, d := range entries {
			name := d.Name()
			path := filepath.Join(dir, name)
			isDir := d.IsDir()
			var info fs.FileInfo
			if d.Type()&fs.ModeSymlink != 0 {
				if info, err = os.Stat(path); err != nil {
					continue // a broken link
				}
				isDir = info.IsDir()
			}
			if isDir {
				if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "@") || extraDirs[strings.ToLower(name)] {
					continue
				}
				if err := walk(path); err != nil {
					return err
				}
				continue
			}
			// Hidden files are not videos: macOS leaves "._Movie.mkv" stubs next
			// to the real file on drives it has written to.
			if strings.HasPrefix(name, ".") || !organizer.IsVideoFile(path) {
				continue
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				continue
			}
			// Judged by the path below root: a folder called "sample" that the
			// library itself sits in must not hide every file in it.
			if organizer.IsSample(rel) {
				continue
			}
			// "Movie (2010)-trailer.mkv" is an extra, but a release name that
			// ends in a group called OTHER or SHORT ("Movie.2010.720p.BluRay.
			// x264-OTHER") is the film itself.
			if base := strings.TrimSuffix(name, filepath.Ext(name)); extraSuffix.MatchString(base) && quality.Classify(parser.Parse(base)) == quality.TierUnknown {
				continue
			}
			if info == nil {
				if info, err = d.Info(); err != nil {
					continue
				}
			}
			if len(out) >= limit {
				return errTooManyFiles
			}
			out = append(out, videoFile{path: path, rel: strings.Split(filepath.ToSlash(rel), "/"), size: info.Size(), mod: info.ModTime()})
		}
		return nil
	}
	if err := walk(root); errors.Is(err, errTooManyFiles) {
		return nil, fmt.Errorf("there are more than %d video files in %s, so it was not read: check it is the right folder", limit, root)
	} else if err != nil {
		return nil, fmt.Errorf("walk %s: %w", root, err)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].path < out[j].path })
	return out, nil
}

// normalize is the title as it is compared: accents folded, letters of any
// alphabet kept, no spaces or punctuation. A title with no letter or digit at
// all falls back to its lower-cased text, so two different titles never share
// an empty key.
func normalize(s string) string {
	if k := strings.ReplaceAll(metadata.TitleKey(s), " ", ""); k != "" {
		return k
	}
	return strings.ToLower(strings.TrimSpace(s))
}

func fileBase(name string) string { return strings.TrimSuffix(name, filepath.Ext(name)) }

// fixFutureYear handles titles that end in a number that looks like a year
// ("Blade Runner 2049"): a year further out than next year is part of the
// title, not a release year.
func fixFutureYear(title string, year int) (string, int) {
	if year > time.Now().Year()+1 {
		return strings.TrimSpace(fmt.Sprintf("%s %d", title, year)), 0
	}
	return title, year
}

func groupKey(title string, year int) string { return fmt.Sprintf("%s|%d", normalize(title), year) }

// ScanMovies finds every movie file under root. The title comes from the
// file name, falling back to (or being completed by) the containing folder:
// "Inception (2010)/movie.mkv" is as common as a flat folder of release names.
func ScanMovies(root string) (Result, error) {
	files, err := walkVideos(root, maxLibraryFiles)
	if err != nil {
		return Result{}, err
	}
	var res Result
	groups := map[string]*Group{}
	var order []string
	for _, f := range files {
		title, year, tier := movieIdentity(f.rel)
		if title == "" {
			res.Skipped = append(res.Skipped, f.path+" - couldn't work out a title")
			continue
		}
		key := groupKey(title, year)
		g, ok := groups[key]
		if !ok {
			g = &Group{Key: key, Kind: KindMovie, Title: title, Year: year}
			groups[key] = g
			order = append(order, key)
		}
		g.Files = append(g.Files, File{Path: f.path, SizeBytes: f.size, Quality: string(tier), ModTime: f.mod})
	}
	for _, k := range order {
		res.Groups = append(res.Groups, *groups[k])
	}
	return res, nil
}

func movieIdentity(rel []string) (string, int, quality.Tier) {
	fr := parser.Parse(fileBase(rel[len(rel)-1]))
	title, year, tier := fr.Title, fr.Year, quality.Classify(fr)
	if len(rel) >= 2 {
		dr := parser.Parse(rel[len(rel)-2])
		if (year == 0 && dr.Year != 0 && dr.Title != "") || (title == "" && dr.Title != "") {
			title, year = dr.Title, dr.Year
		}
		if tier == quality.TierUnknown {
			tier = quality.Classify(dr)
		}
	}
	title, year = fixFutureYear(title, year)
	return title, year, tier
}

// ScanTV finds every episode file under root. Files whose season/episode
// can't be read from their name are reported as skipped rather than guessed
// at.
func ScanTV(root string) (Result, error) {
	files, err := walkVideos(root, maxLibraryFiles)
	if err != nil {
		return Result{}, err
	}
	var res Result
	groups := map[string]*Group{}
	var order []string
	seen := map[string]int{} // group key + season/episode -> index into that group's Files, to dedupe
	for _, f := range files {
		title, year, season, episodes, tier, ok := tvIdentity(f.rel)
		var (
			absolute []int
			daily    parser.Release
		)
		if ok {
			absolute = absoluteInName(fileBase(f.rel[len(f.rel)-1]), len(episodes))
		} else if title, year, absolute, tier, ok = absoluteIdentity(f.rel); !ok {
			if title, year, daily, tier, ok = dailyIdentity(f.rel); !ok {
				res.Skipped = append(res.Skipped, f.path+" - couldn't read a season/episode number from the name")
				continue
			}
		}
		key := groupKey(title, year)
		g, exists := groups[key]
		if !exists {
			g = &Group{Key: key, Kind: KindTV, Title: title, Year: year}
			groups[key] = g
			order = append(order, key)
		}
		file := File{Path: f.path, SizeBytes: f.size, Season: season, Episodes: episodes, Absolute: absolute, AirDate: daily.AirDate, DailyPart: daily.DailyPart, Quality: string(tier), ModTime: f.mod}
		var dupKey string
		switch {
		case len(episodes) > 0:
			dupKey = fmt.Sprintf("%s#%d#%d", key, season, episodes[0])
		case daily.AirDate != "":
			dupKey = fmt.Sprintf("%s#d%s#%d", key, daily.AirDate, daily.DailyPart)
		default:
			dupKey = fmt.Sprintf("%s#%d#a%d", key, season, absolute[0])
		}
		if idx, dup := seen[dupKey]; dup {
			// Two files claim the same episode (say a 720p and a 1080p
			// copy): keep the bigger one and report the other.
			if g.Files[idx].SizeBytes >= file.SizeBytes {
				res.Skipped = append(res.Skipped, f.path+" - duplicate copy of the same episode")
				continue
			}
			res.Skipped = append(res.Skipped, g.Files[idx].Path+" - duplicate copy of the same episode")
			g.Files[idx] = file
			continue
		}
		seen[dupKey] = len(g.Files)
		g.Files = append(g.Files, file)
	}
	for _, k := range order {
		res.Groups = append(res.Groups, *groups[k])
	}
	return res, nil
}

func tvIdentity(rel []string) (title string, year, season int, episodes []int, tier quality.Tier, ok bool) {
	fr := parser.Parse(fileBase(rel[len(rel)-1]))
	// Season 0 is the specials; only a missing episode number means "not an episode".
	if len(fr.Episodes) == 0 {
		return "", 0, 0, nil, "", false
	}
	title, year, tier, ok = showIdentity(rel, fr)
	if !ok {
		return "", 0, 0, nil, "", false
	}
	return title, year, fr.Season, fr.Episodes, tier, true
}

// absoluteIdentity is tvIdentity for a file named only by its number counted
// from the start of the show ("[Group] One Piece - 1100 [1080p]"), the way
// anime is usually named.
func absoluteIdentity(rel []string) (title string, year int, absolute []int, tier quality.Tier, ok bool) {
	fr := parser.Parse(fileBase(rel[len(rel)-1]))
	if len(fr.Absolute) == 0 || len(fr.Episodes) > 0 || fr.Season > 0 || fr.Special {
		return "", 0, nil, "", false
	}
	title, year, tier, ok = showIdentity(rel, fr)
	if !ok {
		return "", 0, nil, "", false
	}
	return title, year, fr.Absolute, tier, true
}

// dailyIdentity is tvIdentity for a daily show's file named by the day it
// aired ("The Daily Show - 2024-01-15 - Guest", "Show.2024.03.05.720p.WEB").
func dailyIdentity(rel []string) (title string, year int, fr parser.Release, tier quality.Tier, ok bool) {
	fr = parser.Parse(fileBase(rel[len(rel)-1]))
	if fr.AirDate == "" || len(fr.Episodes) > 0 || fr.Season > 0 || fr.Special {
		return "", 0, parser.Release{}, "", false
	}
	// The year of the air date is not the show's year.
	if len(fr.AirDate) >= 4 && strconv.Itoa(fr.Year) == fr.AirDate[:4] {
		fr.Year = 0
	}
	title, year, tier, ok = showIdentity(rel, fr)
	if !ok {
		return "", 0, parser.Release{}, "", false
	}
	return title, year, fr, tier, true
}

// absoluteInNameRe finds the number a name carries after its season and
// episode, the way Sonarr names anime: "S23E10 - 1100 - Title". Three or four
// digits, set off by a dash or a dot and ended by a separator, so that
// "S01E05.1080p" and "S01E05.x264" are not read as numbers.
var absoluteInNameRe = regexp.MustCompile(`(?i)\bS\d{1,4}[ ._-]?E\d{1,3}(?:[ ._-]?E\d{1,3})*(?:-E?\d{1,3})?\s*[-.]\s*\(?(\d{3,4})(?:v\d)?(?:\s*-\s*(\d{3,4})(?:v\d)?)?\)?(?:[\s\-.\[(]|$)`)

// AbsoluteNumbers is the episode numbers, counted from the start of the show,
// that a file name carries: "[Group] One Piece - 1100" or "One Piece - S23E10 -
// 1100 - Title". Nil when it has none, or when it is not sure.
func AbsoluteNumbers(name string) []int {
	base := fileBase(name)
	fr := parser.Parse(base)
	switch {
	case len(fr.Episodes) > 0:
		return absoluteInName(base, len(fr.Episodes))
	case fr.Season == 0 && !fr.Special:
		return fr.Absolute
	}
	return nil
}

// absoluteInName reads the absolute number out of a file name that has a
// season and episodes. It answers nothing unless there is exactly one number
// per episode, so it never attaches a number it is not sure of.
func absoluteInName(base string, episodes int) []int {
	m := absoluteInNameRe.FindStringSubmatch(base)
	if m == nil {
		return nil
	}
	first, _ := strconv.Atoi(m[1])
	last := first
	if m[2] != "" {
		last, _ = strconv.Atoi(m[2])
	}
	if year := parser.Parse(base).Year; year != 0 && first == year {
		return nil
	}
	if last < first || last-first+1 != episodes {
		return nil
	}
	out := make([]int, 0, episodes)
	for n := first; n <= last; n++ {
		out = append(out, n)
	}
	return out
}

// showIdentity is the show a file belongs to: named by the folder directly
// under the scan root, or by the file itself.
func showIdentity(rel []string, fr parser.Release) (title string, year int, tier quality.Tier, ok bool) {
	title, year = fr.Title, fr.Year
	tier = tierFromNames(fileBase(rel[len(rel)-1]), nearestFolders(rel))
	// The folder directly under the scan root is the series folder, unless
	// it is itself a "Season 01" folder (the scan was pointed at one show),
	// in which case only the file name carries the show's name.
	if len(rel) >= 2 {
		top := parser.Parse(rel[0])
		if !seasonFolder.MatchString(rel[0]) && top.Title != "" {
			title, year = top.Title, top.Year
		}
	}
	if title == "" {
		return "", 0, "", false
	}
	title, year = fixFutureYear(title, year)
	return title, year, tier, true
}
