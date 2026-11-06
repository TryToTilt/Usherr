package sports

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The library's layout, the way Plex and Jellyfin read a TV library (and
// Sportarr files sports):
//
//	<Sports folder>/<League>/Season <Year>/<League> - S<Year>E<NN> - <Event>[ - <Part>] - <Quality>[ - ptN].<ext>
//
// The league is the show, the season's first year the season, the event's
// place in the season the episode. A fight card's parts share the episode
// and end in pt1 (early prelims), pt2 (prelims), pt3 (main card), so Plex
// stacks them and plays them in order.

var firstYear = regexp.MustCompile(`(?:19|20)\d{2}`)

// SeasonNumber is the season as a number: "2026" and "2025-2026" give their
// first year; with no year, the event's.
func SeasonNumber(season string, at time.Time) int {
	if m := firstYear.FindString(season); m != "" {
		n, _ := strconv.Atoi(m)
		return n
	}
	if !at.IsZero() {
		return at.Year()
	}
	return 0
}

// PartNumber is a fight card's part's place in the card (1-3), 0 for none.
func PartNumber(part string) int {
	for i, p := range FightParts {
		if p == part {
			return i + 1
		}
	}
	return 0
}

// FileInfo is what an imported file's name is made of.
type FileInfo struct {
	League  string
	Season  int
	Episode int
	Title   string
	Part    string // a fight card's part, or ""
	Quality string // "1080p WEB" or ""
	Ext     string // ".mkv"
}

// Folder is the league's folder and the season's, relative to the Sports
// folder (not yet cleaned of characters the disk refuses).
func Folder(league string, season int) (string, string) {
	return strings.TrimSpace(league), fmt.Sprintf("Season %d", season)
}

// FileName is the file's name (not yet cleaned of characters the disk
// refuses).
func FileName(f FileInfo) string {
	parts := []string{strings.TrimSpace(f.League), fmt.Sprintf("S%dE%02d", f.Season, f.Episode), strings.TrimSpace(f.Title)}
	if f.Part != "" {
		parts = append(parts, f.Part)
	}
	if q := strings.TrimSpace(f.Quality); q != "" {
		parts = append(parts, q)
	}
	if n := PartNumber(f.Part); n > 0 {
		parts = append(parts, fmt.Sprintf("pt%d", n))
	}
	return strings.Join(parts, " - ") + strings.ToLower(f.Ext)
}

// QualityLabel is a release's quality for a file name: "1080p WEB",
// "720p HDTV", or "" when the name says neither.
func QualityLabel(r Release) string {
	src := r.Source
	switch strings.ToUpper(src) {
	case "WEB-DL", "WEBRIP", "WEB":
		src = "WEB"
	}
	return strings.TrimSpace(r.Resolution + " " + src)
}
