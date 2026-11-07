package api

// Home fork: keeping a comic issue's state in step with its file on disk.
//   - unclaimComicIssue: a download that didn't file an issue marks it missing
//     again, unless something else (another copy of the same pack, running at
//     the same moment) filed it meanwhile.
//   - claimComicFiles: Refresh on a series picks up comic files already in
//     its folder for issues still marked missing.

import (
	"io/fs"
	"os"
	"path/filepath"

	"github.com/rdborg/mediarium/internal/comics"
)

func (s *Server) unclaimComicIssue(id int64) {
	repo := s.comicRepo()
	if is, err := repo.GetIssue(id); err == nil && is.Status == comics.StatusDownloaded && is.FilePath != "" {
		if _, err := os.Stat(is.FilePath); err == nil {
			return // filed, and the file is there
		}
	}
	_ = repo.SetState(id, comics.StatusMissing, "", 0)
}

// copyAt reports what's at a path, for not filing a second copy: whether a
// file is there, and whether it's as big as the one about to be filed.
func copyAt(path string, size int64) (there, same bool) {
	if path == "" {
		return false, false
	}
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return false, false
	}
	return true, st.Size() == size
}

// comicAlreadyFiled says why a finished download shouldn't be filed as this
// issue, or "" when it should: the issue has its file already, by another copy
// of the same release, or an earlier download left the same file at dest.
func (s *Server) comicAlreadyFiled(issueID int64, dest string, size int64) string {
	repo := s.comicRepo()
	if is, err := repo.GetIssue(issueID); err == nil && is.Status == comics.StatusDownloaded {
		if there, same := copyAt(is.FilePath, size); there {
			if same {
				return "the same file is already in your library, so this copy was dropped"
			}
			return "you already have this issue, in a file of another size. It was kept, and this copy wasn't filed"
		}
	}
	if there, same := copyAt(dest, size); there && same {
		if repo.SetState(issueID, comics.StatusDownloaded, dest, size) == nil {
			return "the same file is already in your library, so this copy was dropped"
		}
	}
	return ""
}

// claimComicFiles registers files in the series' folder as the missing
// issues they name; it returns how many.
func (s *Server) claimComicFiles(sr comics.Series) int {
	if sr.Folder == "" {
		return 0
	}
	repo := s.comicRepo()
	issues, err := repo.Issues(sr.ID)
	if err != nil {
		return 0
	}
	taken := map[string]bool{}
	for _, is := range issues {
		if is.FilePath != "" {
			taken[filepath.Clean(is.FilePath)] = true
		}
	}
	n := 0
	// Only this series' own folder: two editions of a series (Vagabond in
	// VizBig and standard volumes) share the folder above it.
	root := sr.Folder
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !comics.IsComicFile(d.Name()) || taken[filepath.Clean(p)] {
			return nil
		}
		name := comics.ParseName(d.Name())
		if name.Number == "" || name.Collection {
			return nil
		}
		for k := range issues {
			is := &issues[k]
			if is.Status == comics.StatusDownloaded || comics.NormalNumber(is.Number) != comics.NormalNumber(name.Number) {
				continue
			}
			size := int64(0)
			if info, err := d.Info(); err == nil {
				size = info.Size()
			}
			if repo.SetState(is.ID, comics.StatusDownloaded, p, size) == nil {
				is.Status = comics.StatusDownloaded
				taken[filepath.Clean(p)] = true
				n++
			}
			break
		}
		return nil
	})
	return n
}
