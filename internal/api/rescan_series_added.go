package api

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rdborg/mediarium/internal/libimport"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/mediaservers"
	"github.com/rdborg/mediarium/internal/quality"
)

// The most file names a rescan lists as not matched.
const maxNotMatchedListed = 10

// attachHandAddedEpisodes is the second half of Rescan files on a show: it
// reads the show's own folder and gives the episodes the video files that were
// put there by hand. The names are read the way a library import reads them
// (season and episode, the number counted from the first episode for an
// anime, the air date for a daily show). Nothing is moved, renamed or
// deleted, no download is queued and an episode's monitored state stays as it
// is. A file an episode of this show already has, a file that belongs to
// another title, a file that a link leads out of the library folders with and
// an episode that still has its own file are all left alone. So is a file
// whose name carries another show's title (the folder may be shared with
// other shows); it is listed as not matched.
func (s *Server) attachHandAddedEpisodes(sr library.Series, rep *rescanReport) {
	eps, err := s.MovieRepo.ListEpisodes(sr.ID)
	if err != nil {
		return
	}
	known := map[string]bool{}
	var haveFiles []string
	byNumber := map[[2]int]library.Episode{}
	for _, e := range eps {
		byNumber[[2]int{e.Season, e.Episode}] = e
		if e.FilePath != "" {
			known[filepath.Clean(e.FilePath)] = true
			haveFiles = append(haveFiles, e.FilePath)
		}
	}
	folder := s.seriesFolderFor(sr, haveFiles)
	if folder == "" {
		return
	}
	// Only a folder inside the library, and never a library folder itself
	// (a show with no folder of its own would make that the whole library).
	root := rootContaining(s.videoRoots(), folder)
	if root == "" || !libraryAvailable(root) || filepath.Clean(root) == filepath.Clean(folder) || !s.insideLibrary(folder) {
		return
	}
	// Files the show or another title already has, and files named for another
	// show, take no part in removing duplicates, so a hand-added file for two
	// episodes is not dropped as a copy of a bigger file that is somebody
	// else's.
	var taken map[string]bool
	ours := s.showFileOwner(sr, folder, haveFiles)
	files, skipped, err := libimport.ScanSeriesFolderSkipping(folder, func(all []string) func(libimport.File) bool {
		taken = s.filesOfOtherTitles(all, sr.ID, 0)
		return func(f libimport.File) bool {
			clean := filepath.Clean(f.Path)
			return known[clean] || taken[clean] || !ours(f)
		}
	})
	if err != nil {
		slog.Info("rescan: couldn't read a show's folder", "series", sr.Title, "folder", folder, "err", err)
		return
	}
	if taken == nil {
		taken = map[string]bool{}
	}
	place := library.ImportPlacer(eps, sr.SeriesType == library.SeriesAnime, s.importMaps(sr), importFilesOf(files))

	var notMatched []string
	for _, line := range skipped {
		// A duplicate copy of an episode is not a file Mediarium failed to
		// read.
		if strings.HasSuffix(line, "duplicate copy of the same episode") {
			continue
		}
		path := filepath.Clean(skippedPath(line))
		if known[path] || taken[path] || !s.insideLibrary(path) {
			continue
		}
		notMatched = append(notMatched, filepath.Base(path))
	}

	// The bigger file first, so that two files for one episode give it the
	// same one every time.
	sort.SliceStable(files, func(i, j int) bool {
		if files[i].SizeBytes != files[j].SizeBytes {
			return files[i].SizeBytes > files[j].SizeBytes
		}
		return files[i].Path < files[j].Path
	})
	var attached []string
	for _, f := range files {
		clean := filepath.Clean(f.Path)
		if known[clean] || taken[clean] || !s.insideLibrary(f.Path) {
			continue
		}
		// A shared folder: a file named for another show is not this show's.
		if !ours(f) {
			notMatched = append(notMatched, filepath.Base(f.Path))
			continue
		}
		season, numbers, ok := place(library.ImportFile{Path: f.Path, Quality: f.Quality, Season: f.Season, Episodes: f.Episodes, Absolute: f.Absolute, AirDate: f.AirDate, DailyPart: f.DailyPart})
		if !ok {
			notMatched = append(notMatched, filepath.Base(f.Path))
			continue
		}
		tier := f.Quality
		if tier == string(quality.TierUnknown) {
			tier = ""
		}
		used := false
		for _, n := range numbers {
			key := [2]int{season, n}
			ep, listed := byNumber[key]
			// An episode that is downloading, or has a file of its own, keeps
			// it; the extra file is a spare copy and is not touched.
			if !listed || ep.Status == library.StatusDownloading || (ep.Status == library.StatusDownloaded && ep.FilePath != "") {
				continue
			}
			if err := s.MovieRepo.SetEpisodeStatus(ep.ID, library.StatusDownloaded, tier, f.Path); err != nil {
				slog.Warn("rescan: couldn't save a hand-added episode file", "series", sr.Title, "file", filepath.Base(f.Path), "err", err)
				continue
			}
			ep.Status, ep.FilePath = library.StatusDownloaded, f.Path
			byNumber[key] = ep
			rep.AddedEpisodes++
			used = true
		}
		if used {
			known[clean] = true
			attached = append(attached, f.Path)
		}
	}
	if len(notMatched) > 0 {
		sort.Strings(notMatched)
		rep.NotMatchedTotal = len(notMatched)
		rep.NotMatched = notMatched[:min(len(notMatched), maxNotMatchedListed)]
	}
	if rep.AddedEpisodes > 0 {
		_ = s.QueueRepo.LogSeriesActivity(sr.ID, "imported", fmt.Sprintf("%s: you put video files in its folder yourself, and %d of them were added to episodes.", seriesLabel(sr), rep.AddedEpisodes))
		s.mediaServersImported(mediaservers.MediaTV, attached...)
	}
}

// skippedPath is the path at the start of a line of libimport's skipped list
// ("path - reason").
func skippedPath(line string) string {
	for _, reason := range []string{" - couldn't read a season/episode number from the name", " - duplicate copy of the same episode"} {
		if p, ok := strings.CutSuffix(line, reason); ok {
			return p
		}
	}
	return line
}
