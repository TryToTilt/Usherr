package api

// Home fork: importing loose comic files (a folder of downloads) into the
// Comics library. Each file's name gives a series, an issue number and a
// year; the series is found on ComicVine (same name, the year when there is
// one, enough issues), added without following new issues, and the file is
// COPIED into the library layout. The originals are never touched.

import (
	"context"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/comics"
	"github.com/rdborg/mediarium/internal/organizer"
	"github.com/rdborg/mediarium/internal/plainerror"
)

type comicFileResult struct {
	File    string `json:"file"`
	Series  string `json:"series,omitempty"`
	Issue   string `json:"issue,omitempty"`
	Status  string `json:"status"` // imported, already, unmatched, failed
	Message string `json:"message,omitempty"`
	Dest    string `json:"dest,omitempty"`
}

// pickComicSeries chooses the ComicVine series a file belongs to: the same
// name; then the start year closest to the file's year (not after it); and
// enough issues for the number.
func pickComicSeries(n comics.Name, found []comics.Series) (comics.Series, bool) {
	num, _ := strconv.ParseFloat(comics.NormalNumber(n.Number), 64)
	var best comics.Series
	bestScore := -1
	for _, s := range found {
		if comics.Key(s.Name) != comics.Key(n.Series) {
			continue
		}
		if num > 0 && s.IssueCount > 0 && float64(s.IssueCount) < num {
			continue
		}
		score := 1000
		if n.Year > 0 && s.StartYear > 0 {
			d := n.Year - s.StartYear
			if d < 0 {
				continue // a series can't start after its own issue
			}
			score -= d * 10
		}
		score += min(s.IssueCount, 50) // prefer the main run over a one-off
		if score > bestScore {
			best, bestScore = s, score
		}
	}
	return best, bestScore >= 0
}

// POST /api/comics/import/folder {"path": "/data/comics-incoming", "dryRun": true}
func (s *Server) handleImportComicFolder(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path   string `json:"path"`
		DryRun bool   `json:"dryRun"`
	}
	if err := decodeJSON(r, &req); err != nil || !filepath.IsAbs(req.Path) {
		writeError(w, http.StatusBadRequest, "Give the folder to import, as a full path inside the container.")
		return
	}
	if !s.comicKeySet() || s.comicsRoot() == "" {
		writeError(w, http.StatusPreconditionFailed, "Set the comics folder and the ComicVine key first.")
		return
	}
	root := filepath.Clean(req.Path)
	if strings.HasPrefix(root+string(filepath.Separator), filepath.Clean(s.comicsRoot())+string(filepath.Separator)) {
		writeError(w, http.StatusBadRequest, "That folder is inside the comics library already.")
		return
	}
	var files []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && comics.IsComicFile(d.Name()) {
			files = append(files, p)
		}
		return nil
	})
	sort.Strings(files)
	ctx := r.Context()
	lookups := map[string][]comics.Series{} // per series name
	added := map[int64]comics.Series{}      // ComicVine id -> library series
	var out []comicFileResult
	for _, f := range files {
		res := comicFileResult{File: strings.TrimPrefix(f, root+string(filepath.Separator))}
		n := comics.ParseName(filepath.Base(f))
		if n.Number == "" && !n.Collection {
			n.Number = "1" // a one-shot or graphic novel
		}
		if n.Series == "" || n.Number == "" {
			res.Status, res.Message = "unmatched", "The name doesn't say which series and issue this is."
			out = append(out, res)
			continue
		}
		key := comics.Key(n.Series)
		found, ok := lookups[key]
		if !ok {
			cctx, cancel := context.WithTimeout(ctx, time.Minute)
			list, err := s.comicVine().SearchSeries(cctx, n.Series, 30)
			cancel()
			if err != nil {
				res.Status, res.Message = "failed", plainerror.Message(err)
				out = append(out, res)
				continue
			}
			lookups[key], found = list, list
		}
		cv, ok := pickComicSeries(n, found)
		if !ok {
			res.Status, res.Message = "unmatched", "ComicVine has no series called "+n.Series+" that fits."
			out = append(out, res)
			continue
		}
		res.Series = cv.Name
		if cv.StartYear > 0 {
			res.Series += " (" + strconv.Itoa(cv.StartYear) + ")"
		}
		res.Issue = comics.NormalNumber(n.Number)
		if req.DryRun {
			res.Status = "would import"
			out = append(out, res)
			continue
		}
		sr, ok := added[cv.CVID]
		if !ok {
			if have, err := s.comicRepo().SeriesByCV(cv.CVID); err == nil {
				sr = have
			} else {
				cctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
				var err error
				sr, _, err = s.addComicSeries(cctx, cv.CVID, "none", "")
				cancel()
				if err != nil {
					res.Status, res.Message = "failed", plainerror.Message(err)
					out = append(out, res)
					continue
				}
			}
			added[cv.CVID] = sr
		}
		issues, _ := s.comicRepo().Issues(sr.ID)
		var is comics.Issue
		for _, x := range issues {
			if comics.NormalNumber(x.Number) == res.Issue {
				is = x
				break
			}
		}
		if is.ID == 0 {
			res.Status, res.Message = "unmatched", "ComicVine lists no issue #"+res.Issue+" in "+res.Series+"."
			out = append(out, res)
			continue
		}
		if is.Status == comics.StatusDownloaded && is.FilePath != "" && fileExists(is.FilePath) {
			res.Status, res.Dest = "already", is.FilePath
			out = append(out, res)
			continue
		}
		dest, err := s.comicDest(sr, is, f)
		if err != nil {
			res.Status, res.Message = "failed", err.Error()
			out = append(out, res)
			continue
		}
		dest = freeName(dest)
		imp, err := organizer.Import(f, dest, organizer.ConflictSkip, nil) // copies; the original stays
		if err != nil || imp.Skipped {
			res.Status, res.Message = "failed", "couldn't copy the file into the library"
			out = append(out, res)
			continue
		}
		if sr.Folder == "" {
			_ = s.comicRepo().SetSeriesFolder(sr.ID, filepath.Dir(imp.DestPath))
			sr.Folder = filepath.Dir(imp.DestPath)
			added[cv.CVID] = sr
		}
		size := int64(0)
		if st, err := comicFileSize(imp.DestPath); err == nil {
			size = st
		}
		_ = s.comicRepo().SetState(is.ID, comics.StatusDownloaded, imp.DestPath, size)
		res.Status, res.Dest = "imported", imp.DestPath
		out = append(out, res)
	}
	writeJSON(w, http.StatusOK, out)
}

func comicFileSize(p string) (int64, error) {
	st, err := os.Stat(p)
	if err != nil {
		return 0, err
	}
	return st.Size(), nil
}
