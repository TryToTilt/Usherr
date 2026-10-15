package api

// Home fork: a download holding several comics (a pack of a run, or of a
// story arc across series) fills every issue it holds that the library has
// and doesn't have yet, matched by the file names. Files the library doesn't
// know stay out.

import (
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"

	"github.com/rdborg/mediarium/internal/comics"
	"github.com/rdborg/mediarium/internal/organizer"
)

// comicFilesIn lists the comic files in a download.
func comicFilesIn(dir string) []string {
	var out []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && comics.IsComicFile(d.Name()) {
			out = append(out, p)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// importComicPack files every comic in a pack that matches an issue in the
// library that isn't downloaded yet. It returns how many it filed, and
// whether the wanted issue was among them.
// already counts the pack's issues the library had before (home fork: a pack
// grabbed twice isn't a failure).
func (s *Server) importComicPack(want comics.Issue, files []string) (n int, gotWanted bool, already int) {
	repo := s.comicRepo()
	all, err := repo.ListSeries()
	if err != nil {
		return 0, false, 0
	}
	byKey := map[string][]comics.Series{}
	for _, sr := range all {
		byKey[comics.Key(sr.Name)] = append(byKey[comics.Key(sr.Name)], sr)
	}
	issues := map[int64][]comics.Issue{}
	for _, f := range files {
		name := comics.ParseName(filepath.Base(f))
		if name.Number == "" || name.Collection {
			continue
		}
		cands := byKey[comics.Key(name.Series)]
		had := false // the library already has this file's issue
		var hit *comics.Issue
		var hitSeries comics.Series
		for _, sr := range cands {
			list, ok := issues[sr.ID]
			if !ok {
				list, _ = repo.Issues(sr.ID)
				issues[sr.ID] = list
			}
			for k := range list {
				is := &list[k]
				if !comics.Matches(name, sr, *is) {
					continue
				}
				if is.Status == comics.StatusDownloaded {
					had = true
					continue
				}
				if hit == nil || (is.ID == want.ID) {
					hit, hitSeries = is, sr
				}
			}
		}
		if hit == nil {
			if had {
				already++
			}
			continue
		}
		dest, err := s.comicDest(hitSeries, *hit, f)
		if err != nil {
			continue
		}
		res, err := organizer.Import(f, freeName(dest), organizer.ConflictSkip, nil)
		if err != nil || res.Skipped {
			continue
		}
		size := int64(0)
		if st, err := os.Stat(res.DestPath); err == nil {
			size = st.Size()
		}
		if hitSeries.Folder == "" {
			_ = repo.SetSeriesFolder(hitSeries.ID, filepath.Dir(res.DestPath))
		}
		if err := repo.SetState(hit.ID, comics.StatusDownloaded, res.DestPath, size); err == nil {
			n++
			hit.Status = comics.StatusDownloaded
			if hit.ID == want.ID {
				gotWanted = true
			}
			slog.Info("comics: filed from a pack", "series", hitSeries.Name, "issue", hit.Number)
		}
	}
	return n, gotWanted, already
}
