package api

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/rdborg/mediarium/internal/libimport"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/internal/parser"
)

// seasonFolderRE matches the name of a folder that only holds a season of
// whatever title it sits in ("Season 02", "S2", "Specials").
var seasonFolderRE = regexp.MustCompile(`(?i)^(?:(?:season|series|staffel|saison|temporada|stagione|seizoen|sezon)[ ._-]*\d+(?:[ ._-].*)?|s\d{1,2}|specials?)$`)

// showFileOwner returns a test that says whether a video file found in a
// show's folder is the show's own, for the rescan of hand-added files. A
// folder is often shared (files of shows that are not in the library, or of
// other titles), so a file is only given to this show when the title in its
// name is the show's: its title, its other names, the title the folder is
// named for, or the title of a file the show already has (the same title match
// the library import uses). A name without a title ("S01E02.mkv") takes the
// title of the folder it sits in under the show's folder, and with no such
// folder it is the show's own.
func (s *Server) showFileOwner(sr library.Series, folder string, haveFiles []string) func(libimport.File) bool {
	keys := map[string]bool{}
	add := func(title string) {
		for _, k := range metadata.TitleKeys(title) {
			keys[k] = true
		}
		if base, code := parser.SplitCountryTag(title); code != "" {
			for _, k := range metadata.TitleKeys(base) {
				keys[k] = true
			}
		}
	}
	add(sr.Title)
	for _, a := range sr.Aliases {
		add(a)
	}
	add(parsedTitle(filepath.Base(folder)))
	for _, f := range haveFiles {
		add(parsedTitle(filepath.Base(f)))
	}
	var once sync.Once
	otherNames := func() {
		once.Do(func() {
			// The show's other titles are only looked up once a name turns out
			// not to be the main one. Saved names come at once, a fetch gets a
			// couple of seconds.
			ctx, cancel := context.WithTimeout(context.Background(), tvWantsNamesTimeout)
			defer cancel()
			for _, a := range s.seriesWithAltTitles(ctx, sr).AltTitles {
				add(a)
			}
		})
	}
	isOurs := func(title string) bool {
		if decided, match := countryTagMatch(title, sr); decided {
			return match
		}
		fits := func() bool {
			for _, k := range metadata.TitleKeys(title) {
				if keys[k] {
					return true
				}
			}
			return false
		}
		if fits() {
			return true
		}
		otherNames()
		return fits()
	}
	return func(f libimport.File) bool {
		title := f.Title
		if title == "" {
			title = titleOfFolders(folder, f.Path)
		}
		if title == "" {
			return true
		}
		return isOurs(title)
	}
}

// titleOfFolders is the title of the nearest folder between the show's folder
// and the file that is not a season folder ("" when there is none).
func titleOfFolders(showFolder, file string) string {
	rel, err := filepath.Rel(showFolder, filepath.Dir(file))
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return ""
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] == "" || seasonFolderRE.MatchString(parts[i]) {
			continue
		}
		return parsedTitle(parts[i])
	}
	return ""
}

// parsedTitle is the title a name carries, or the name itself when none can
// be read from it.
func parsedTitle(name string) string {
	if t := parser.Parse(name).Title; t != "" {
		return t
	}
	return strings.TrimSpace(strings.TrimSuffix(name, filepath.Ext(name)))
}
