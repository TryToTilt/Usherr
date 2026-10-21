package libimport

import (
	"fmt"
	"path/filepath"

	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/quality"
)

// ScanMovieFolder lists the video files in one movie's own folder, when the
// title is already known (another app said which movie lives there). Unlike
// ScanMovies nothing is dropped for having an unreadable title: every video
// file counts, with its quality read from the file name and, failing that,
// from the folder name.
func ScanMovieFolder(dir string) ([]File, error) {
	files, err := walkVideos(dir, maxTitleFiles)
	if err != nil {
		return nil, err
	}
	folderTier := quality.Classify(parser.Parse(filepath.Base(dir)))
	out := make([]File, 0, len(files))
	for _, f := range files {
		tier := quality.Classify(parser.Parse(fileBase(f.rel[len(f.rel)-1])))
		if tier == quality.TierUnknown && len(f.rel) >= 2 {
			tier = quality.Classify(parser.Parse(f.rel[len(f.rel)-2]))
		}
		if tier == quality.TierUnknown {
			tier = folderTier
		}
		out = append(out, File{Path: f.path, SizeBytes: f.size, Quality: string(tier)})
	}
	return out, nil
}

// ScanSeriesFolder lists the episode files in one show's own folder, when
// the show is already known. Only the season and episode numbers are read
// from each file name; a file without them ("Pilot.mkv") is reported as
// skipped. Two files for the same episode keep the bigger one, as in ScanTV.
func ScanSeriesFolder(dir string) ([]File, []string, error) {
	return ScanSeriesFolderSkipping(dir, nil)
}

// ScanSeriesFolderSkipping is ScanSeriesFolder for a caller that already knows
// some of the files are not up for grabs (the show's episodes already have
// them, they belong to another title, or they are named for another show).
// prepare gets the path of every video file in the folder and returns the test
// for a file; it may be nil, and so may the test it returns. A file the test
// says to skip is still listed, but it takes no part in removing duplicates: a
// smaller multi-episode file is not thrown out as a copy of a bigger file that
// is already somebody else's. Two files that are both up for grabs for the
// same episode still keep the bigger one.
func ScanSeriesFolderSkipping(dir string, prepare func(paths []string) func(File) bool) ([]File, []string, error) {
	files, err := walkVideos(dir, maxTitleFiles)
	if err != nil {
		return nil, nil, err
	}
	var skip func(File) bool
	if prepare != nil {
		paths := make([]string, len(files))
		for i, f := range files {
			paths[i] = f.path
		}
		skip = prepare(paths)
	}
	var (
		out     []File
		skipped []string
		seen    = map[string]int{}
	)
	// add lists a file, or notes it as a duplicate copy of the episode.
	add := func(key string, file File) {
		if skip != nil && skip(file) {
			out = append(out, file)
			return
		}
		if idx, dup := seen[key]; dup {
			if out[idx].SizeBytes >= file.SizeBytes {
				skipped = append(skipped, file.Path+" - duplicate copy of the same episode")
				return
			}
			skipped = append(skipped, out[idx].Path+" - duplicate copy of the same episode")
			out[idx] = file
			return
		}
		seen[key] = len(out)
		out = append(out, file)
	}
	for _, f := range files {
		fr := parser.Parse(fileBase(f.rel[len(f.rel)-1]))
		if len(fr.Episodes) == 0 { // season 0 is the specials
			// An anime file named only by its number from the first episode.
			if len(fr.Absolute) > 0 && fr.Season == 0 && !fr.Special {
				folders := append(nearestFolders(f.rel), filepath.Base(dir))
				tier := tierFromNames(fileBase(f.rel[len(f.rel)-1]), folders)
				add(fmt.Sprintf("a#%d", fr.Absolute[0]), File{Path: f.path, Title: fr.Title, SizeBytes: f.size, Absolute: fr.Absolute, Quality: string(tier)})
				continue
			}
			// A daily show's file named by the day it aired.
			if fr.AirDate != "" && fr.Season == 0 && !fr.Special {
				folders := append(nearestFolders(f.rel), filepath.Base(dir))
				tier := tierFromNames(fileBase(f.rel[len(f.rel)-1]), folders)
				add(fmt.Sprintf("d#%s#%d", fr.AirDate, fr.DailyPart), File{Path: f.path, Title: fr.Title, SizeBytes: f.size, AirDate: fr.AirDate, DailyPart: fr.DailyPart, Quality: string(tier)})
				continue
			}
			skipped = append(skipped, f.path+" - couldn't read a season/episode number from the name")
			continue
		}
		// Quality comes from the file name, then the folders above it, then
		// the show's own folder.
		folders := append(nearestFolders(f.rel), filepath.Base(dir))
		tier := tierFromNames(fileBase(f.rel[len(f.rel)-1]), folders)
		add(fmt.Sprintf("%d#%d", fr.Season, fr.Episodes[0]), File{Path: f.path, Title: fr.Title, SizeBytes: f.size, Season: fr.Season, Episodes: fr.Episodes, Absolute: absoluteInName(fileBase(f.rel[len(f.rel)-1]), len(fr.Episodes)), Quality: string(tier)})
	}
	return out, skipped, nil
}
