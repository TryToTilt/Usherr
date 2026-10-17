package api

import (
	"context"
	"fmt"
	"time"

	"github.com/rdborg/mediarium/internal/library"
)

// The rules for "wanted, and missing" live here so the Wanted page and the
// Dashboard number can never disagree: a title only counts when it is
// monitored, has no file, and (for an episode) has already aired.

// movieWantedMissing reports whether a movie belongs in Wanted > Missing.
func movieWantedMissing(m library.Movie) bool {
	return m.Monitored && m.Status == library.StatusMissing && !fileExists(m.FilePath)
}

// An episode belongs in Wanted > Missing when its show and it are monitored,
// it is marked missing, it has aired by today and no file of it is on disk.
// The library package's ListWantedEpisodes and WantedShows apply the first
// three in SQL; the file check stays in Go.

// wantedMissingCounts counts what Wanted > Missing would list for the main
// titles (second copies left out).
//
// The database counts the titles marked missing, without reading them, and
// the only thing left to check is the odd one that still names a file: if
// that file is on disk, it is not wanted.
func (s *Server) wantedMissingCounts(ctx context.Context) (movies, episodes int, err error) {
	noFile, movieFiles, err := s.MovieRepo.MissingMovies(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("count missing movies: %w", err)
	}
	movies = noFile
	for _, path := range movieFiles {
		if !fileExists(path) {
			movies++
		}
	}
	today := time.Now().UTC().Format("2006-01-02")
	shows, err := s.MovieRepo.WantedShows(ctx, today, true)
	if err != nil {
		return 0, 0, fmt.Errorf("count missing episodes: %w", err)
	}
	withFile := 0
	for _, sh := range shows {
		episodes += sh.Episodes
		withFile += sh.WithFile
	}
	if withFile == 0 {
		return movies, episodes, nil
	}
	files, err := s.MovieRepo.WantedEpisodeFiles(ctx, today, true)
	if err != nil {
		return 0, 0, fmt.Errorf("list missing episodes: %w", err)
	}
	for _, f := range files {
		if fileExists(f.Path) {
			episodes--
		}
	}
	return movies, episodes, nil
}
