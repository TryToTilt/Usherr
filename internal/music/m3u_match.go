package music

import (
	"fmt"
	"path"
	"strings"
)

// Matching playlist entries to the library's tracks: first by where the file
// is, then by the end of its path (a playlist made on another computer, or
// before the library moved), then by artist, title and length.

// LibraryTrack is a track with a file, as the playlist matcher sees it.
type LibraryTrack struct {
	ID       int64
	Path     string
	Title    string
	Artist   string
	LengthMs int
}

// LibraryTracks lists every track that has a file, with its artist's name.
func (r *Repo) LibraryTracks() ([]LibraryTrack, error) {
	rows, err := r.db.Query(`SELECT t.id, t.file_path, t.title, ar.name, t.length_ms
		FROM tracks t JOIN albums al ON al.id = t.album_id JOIN artists ar ON ar.id = al.artist_id
		WHERE t.file_path <> '' ORDER BY t.id`)
	if err != nil {
		return nil, fmt.Errorf("list library tracks: %w", err)
	}
	defer rows.Close()
	var out []LibraryTrack
	for rows.Next() {
		var t LibraryTrack
		if err := rows.Scan(&t.ID, &t.Path, &t.Title, &t.Artist, &t.LengthMs); err != nil {
			return nil, fmt.Errorf("list library tracks: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// How an entry was matched.
const (
	MatchPath = "path" // the file the playlist names
	MatchTail = "name" // the end of the path: the folder and file name, or the file name alone
	MatchTags = "tags" // artist, title and length
)

// maxTail is how many path parts the end-of-path match looks at, most first.
const maxTail = 4

// durationSlack is how far the playlist's length may be from the track's.
const durationSlack = 3

// M3UMatcher finds library tracks for playlist entries.
type M3UMatcher struct {
	tracks  []LibraryTrack
	byPath  map[string]int     // lower-cased path with forward slashes -> index
	byTail  []map[string][]int // [n] tail of n parts -> indexes
	byName  map[string][]int   // normalized artist+title -> indexes
	byTitle map[string][]int   // normalized title -> indexes
}

// NewM3UMatcher indexes the tracks.
func NewM3UMatcher(tracks []LibraryTrack) *M3UMatcher {
	m := &M3UMatcher{tracks: tracks, byPath: map[string]int{}, byTail: make([]map[string][]int, maxTail+1),
		byName: map[string][]int{}, byTitle: map[string][]int{}}
	for n := 1; n <= maxTail; n++ {
		m.byTail[n] = map[string][]int{}
	}
	for i, t := range tracks {
		m.byPath[strings.ToLower(path.Clean(slashed(t.Path)))] = i
		for n := 1; n <= maxTail; n++ {
			if k := PathTail(t.Path, n); k != "" {
				m.byTail[n][k] = append(m.byTail[n][k], i)
			}
		}
		title := NormalizeName(t.Title)
		m.byName[NormalizeName(t.Artist)+"\x00"+title] = append(m.byName[NormalizeName(t.Artist)+"\x00"+title], i)
		m.byTitle[title] = append(m.byTitle[title], i)
	}
	return m
}

// Match finds the track for an entry of a playlist kept in playlistDir (with
// forward slashes). It returns 0 when there is none.
func (m *M3UMatcher) Match(playlistDir string, e M3UEntry) (int64, string) {
	resolved := ResolveM3UPath(playlistDir, e.Path)
	if i, ok := m.byPath[strings.ToLower(resolved)]; ok {
		return m.tracks[i].ID, MatchPath
	}
	dirKey := strings.ToLower(strings.TrimSuffix(path.Clean(slashed(playlistDir)), "/") + "/")
	for n := maxTail; n >= 1; n-- {
		k := PathTail(resolved, n)
		if k == "" {
			continue
		}
		hits := m.byTail[n][k]
		if len(hits) == 1 {
			return m.tracks[hits[0]].ID, MatchTail
		}
		if len(hits) > 1 {
			// Several files end the same way: the one under the playlist's folder.
			var under []int
			for _, i := range hits {
				if strings.HasPrefix(strings.ToLower(slashed(m.tracks[i].Path)), dirKey) {
					under = append(under, i)
				}
			}
			if len(under) == 1 {
				return m.tracks[under[0]].ID, MatchTail
			}
		}
	}
	if e.Title == "" {
		return 0, ""
	}
	title := NormalizeName(e.Title)
	if title == "" {
		return 0, ""
	}
	if e.Artist != "" {
		if i, ok := m.closest(m.byName[NormalizeName(e.Artist)+"\x00"+title], e.Duration, true); ok {
			return m.tracks[i].ID, MatchTags
		}
		return 0, ""
	}
	// A title alone is only trusted with a length that fits one track.
	if e.Duration >= 0 {
		if i, ok := m.closest(m.byTitle[title], e.Duration, false); ok {
			return m.tracks[i].ID, MatchTags
		}
	}
	return 0, ""
}

// closest picks among candidates the one whose length is nearest the
// playlist's (within durationSlack). With no length given, the first one
// is taken when anyIfUnknown is set.
func (m *M3UMatcher) closest(cands []int, durationSec int, anyIfUnknown bool) (int, bool) {
	if len(cands) == 0 {
		return 0, false
	}
	if durationSec < 0 {
		return cands[0], anyIfUnknown
	}
	best, bestDiff, ties, known := -1, durationSlack+1, 0, false
	for _, i := range cands {
		l := m.tracks[i].LengthMs
		if l <= 0 {
			continue
		}
		known = true
		d := (l+500)/1000 - durationSec
		if d < 0 {
			d = -d
		}
		switch {
		case d < bestDiff:
			best, bestDiff, ties = i, d, 1
		case d == bestDiff:
			ties++
		}
	}
	if !known {
		// No lengths known in the library: an artist and title is still a match.
		return cands[0], anyIfUnknown
	}
	if best < 0 {
		return 0, false
	}
	if !anyIfUnknown && ties > 1 {
		return 0, false
	}
	return best, true
}
