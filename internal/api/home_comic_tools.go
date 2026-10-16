package api

// Home fork: comics library tools (after Pullbox's library utilities):
//   - check: open every comic file and report the missing, damaged or empty;
//   - convert: CBR to CBZ (the reader and most apps prefer ZIP). The new file
//     is checked before it's used, and the original is moved to the comics
//     folder's .replaced folder, never deleted;
//   - rename: files not named the way new downloads are get that name (and
//     folder). Nothing is ever overwritten.
// One tool runs at a time, in the background; the page shows its progress.

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nwaples/rardecode/v2"

	"github.com/rdborg/mediarium/internal/comics"
)

func (s *Server) registerComicToolRoutes(admin routeGroup, on func(http.HandlerFunc) http.HandlerFunc) {
	admin.HandleFunc("GET /api/comics/tools", on(s.handleComicTools))
	admin.HandleFunc("POST /api/comics/tools/run/{tool}", on(s.handleRunComicTool))
}

type comicToolProblem struct {
	IssueID int64  `json:"issueId"`
	Series  string `json:"series"`
	Number  string `json:"number"`
	File    string `json:"file"`
	Problem string `json:"problem"`
}

type comicToolState struct {
	Tool     string             `json:"tool,omitempty"`
	Running  bool               `json:"running"`
	Done     int                `json:"done"`
	Total    int                `json:"total"`
	Changed  int                `json:"changed"`
	Problems []comicToolProblem `json:"problems"`
	Started  string             `json:"started,omitempty"`
	Finished string             `json:"finished,omitempty"`
}

var (
	comicToolMu sync.Mutex
	comicTool   = comicToolState{Problems: []comicToolProblem{}}
)

// comicFile is a downloaded issue and its file.
type comicFile struct {
	issue  comics.Issue
	series comics.Series
}

func (s *Server) comicFiles(ctx context.Context) ([]comicFile, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM comic_issues WHERE status = 'downloaded' AND file_path != '' ORDER BY series_id, sort_key`)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	repo := s.comicRepo()
	series := map[int64]comics.Series{}
	var out []comicFile
	for _, id := range ids {
		is, err := repo.GetIssue(id)
		if err != nil {
			continue
		}
		sr, ok := series[is.SeriesID]
		if !ok {
			if sr, err = repo.GetSeries(is.SeriesID); err != nil {
				continue
			}
			series[is.SeriesID] = sr
		}
		out = append(out, comicFile{issue: is, series: sr})
	}
	return out, nil
}

// GET /api/comics/tools: the last run, and what the tools would change now.
func (s *Server) handleComicTools(w http.ResponseWriter, r *http.Request) {
	files, err := s.comicFiles(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	cbr, misnamed := 0, 0
	for _, f := range files {
		if strings.EqualFold(filepath.Ext(f.issue.FilePath), ".cbr") {
			cbr++
		}
		if want, err := s.comicDest(f.series, f.issue, f.issue.FilePath); err == nil && filepath.Clean(want) != filepath.Clean(f.issue.FilePath) {
			misnamed++
		}
	}
	comicToolMu.Lock()
	st := comicTool
	st.Problems = append([]comicToolProblem{}, comicTool.Problems...) // never null: the page counts it
	comicToolMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"files": len(files), "cbr": cbr, "misnamed": misnamed, "last": st})
}

// POST /api/comics/tools/run/{check|convert|rename}
func (s *Server) handleRunComicTool(w http.ResponseWriter, r *http.Request) {
	tool := r.PathValue("tool")
	if tool != "check" && tool != "convert" && tool != "rename" {
		writeError(w, http.StatusNotFound, "There's no such tool.")
		return
	}
	comicToolMu.Lock()
	if comicTool.Running {
		comicToolMu.Unlock()
		writeError(w, http.StatusConflict, "Another tool is running. Wait for it to finish.")
		return
	}
	comicTool = comicToolState{Tool: tool, Running: true, Problems: []comicToolProblem{}, Started: time.Now().UTC().Format(time.RFC3339)}
	comicToolMu.Unlock()
	s.background(func() { s.runComicTool(tool) })
	writeJSON(w, http.StatusAccepted, map[string]string{"message": "Started."})
}

func (s *Server) runComicTool(tool string) {
	ctx := context.Background()
	defer func() {
		comicToolMu.Lock()
		comicTool.Running = false
		comicTool.Finished = time.Now().UTC().Format(time.RFC3339)
		comicToolMu.Unlock()
	}()
	files, err := s.comicFiles(ctx)
	if err != nil {
		slog.Info("comics: tools", "tool", tool, "err", err)
		return
	}
	comicToolMu.Lock()
	comicTool.Total = len(files)
	comicToolMu.Unlock()
	problem := func(f comicFile, msg string) {
		comicToolMu.Lock()
		if len(comicTool.Problems) < 500 {
			comicTool.Problems = append(comicTool.Problems, comicToolProblem{IssueID: f.issue.ID, Series: f.series.Name, Number: f.issue.Number,
				File: filepath.Base(f.issue.FilePath), Problem: msg})
		}
		comicToolMu.Unlock()
	}
	for _, f := range files {
		changed := false
		switch tool {
		case "check":
			if _, err := comicPageCount(f.issue.FilePath); err != nil {
				problem(f, err.Error())
			}
		case "convert":
			if strings.EqualFold(filepath.Ext(f.issue.FilePath), ".cbr") {
				if err := s.convertComicToCBZ(f); err != nil {
					problem(f, "Not converted: "+err.Error())
				} else {
					changed = true
				}
			}
		case "rename":
			moved, err := s.renameComicFile(f)
			if err != nil {
				problem(f, "Not renamed: "+err.Error())
			}
			changed = moved
		}
		comicToolMu.Lock()
		comicTool.Done++
		if changed {
			comicTool.Changed++
		}
		comicToolMu.Unlock()
	}
}

// comicPageCount opens a comic and counts its pages. A PDF or CB7 is only
// checked for being there and not empty.
func comicPageCount(file string) (int, error) {
	st, err := os.Stat(file)
	if err != nil {
		return 0, errors.New("the file is missing")
	}
	if st.Size() == 0 {
		return 0, errors.New("the file is empty")
	}
	head := make([]byte, 8)
	f, err := os.Open(file)
	if err != nil {
		return 0, fmt.Errorf("can't open it: %w", err)
	}
	n, _ := io.ReadFull(f, head)
	f.Close()
	head = head[:n]
	pages := 0
	switch {
	case bytes.HasPrefix(head, []byte("PK")):
		zr, err := zip.OpenReader(file)
		if err != nil {
			return 0, fmt.Errorf("damaged ZIP: %w", err)
		}
		defer zr.Close()
		for _, zf := range zr.File {
			if zf.FileInfo().IsDir() || !isPageImage(zf.Name) {
				continue
			}
			rc, err := zf.Open()
			if err != nil {
				return pages, fmt.Errorf("page %s won't open: %w", zf.Name, err)
			}
			_, err = io.Copy(io.Discard, rc) // reading it all checks every page's checksum
			rc.Close()
			if err != nil {
				return pages, fmt.Errorf("page %s is damaged", zf.Name)
			}
			pages++
		}
	case bytes.HasPrefix(head, []byte("Rar!")):
		rc, err := rardecode.OpenReader(file, rardecode.BufferSize(4<<20))
		if err != nil {
			return 0, fmt.Errorf("damaged RAR: %w", err)
		}
		defer rc.Close()
		for {
			h, err := rc.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return pages, fmt.Errorf("damaged RAR: %w", err)
			}
			if h.IsDir || !isPageImage(h.Name) {
				continue
			}
			if _, err := io.Copy(io.Discard, rc); err != nil {
				return pages, fmt.Errorf("page %s is damaged", h.Name)
			}
			pages++
		}
	case bytes.HasPrefix(head, []byte("%PDF")), bytes.HasPrefix(head, []byte("7z\xbc\xaf")):
		return 1, nil // can't count these here; they're there and not empty
	default:
		return 0, errors.New("not a comic archive (the content isn't ZIP, RAR, 7z or PDF)")
	}
	if pages == 0 {
		return 0, errors.New("no pages inside")
	}
	return pages, nil
}

// replacedPath is where a file that was converted goes: the comics folder's
// .replaced folder, keeping its place in the library.
func (s *Server) replacedPath(file string) string {
	root := s.comicsRoot()
	rel, err := filepath.Rel(root, file)
	if err != nil || strings.HasPrefix(rel, "..") {
		rel = filepath.Base(file)
	}
	return filepath.Join(root, ".replaced", rel) // hidden: Komga and Kavita skip it
}

// convertComicToCBZ writes a CBR's contents into a CBZ beside it, checks the
// CBZ has every page, then moves the CBR to .replaced and uses the CBZ.
func (s *Server) convertComicToCBZ(f comicFile) error {
	src := f.issue.FilePath
	head := make([]byte, 4)
	if fh, err := os.Open(src); err == nil {
		_, _ = io.ReadFull(fh, head)
		fh.Close()
	}
	if bytes.HasPrefix(head, []byte("PK")) {
		// A ZIP that was only called .cbr: just the name changes.
		return s.moveComic(f, strings.TrimSuffix(src, filepath.Ext(src))+".cbz", false)
	}
	before, err := comicPageCount(src)
	if err != nil {
		return err
	}
	dst := strings.TrimSuffix(src, filepath.Ext(src)) + ".cbz"
	if _, err := os.Stat(dst); err == nil {
		return errors.New("a .cbz of the same name is already there")
	}
	part := dst + ".part"
	if err := rarToCBZ(src, part, before); err != nil {
		return err
	}
	if err := os.Rename(part, dst); err != nil {
		_ = os.Remove(part)
		return err
	}
	keep := s.replacedPath(src)
	if err := os.MkdirAll(filepath.Dir(keep), 0o755); err != nil {
		return err
	}
	if err := os.Rename(src, keep); err != nil {
		if err := copyThenRemove(src, keep); err != nil {
			return fmt.Errorf("the CBZ is ready but the CBR couldn't be moved to .replaced: %w", err)
		}
	}
	st, _ := os.Stat(dst)
	var size int64
	if st != nil {
		size = st.Size()
	}
	return s.comicRepo().SetState(f.issue.ID, comics.StatusDownloaded, dst, size)
}

// rarToCBZ writes a RAR comic's files into a ZIP at part and checks it has
// all the pages (want); on any trouble the half-written ZIP is removed.
func rarToCBZ(src, part string, want int) error {
	out, err := os.Create(part)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(out)
	rc, err := rardecode.OpenReader(src, rardecode.BufferSize(4<<20))
	if err == nil {
		for {
			h, herr := rc.Next()
			if errors.Is(herr, io.EOF) {
				break
			}
			if herr != nil {
				err = herr
				break
			}
			if h.IsDir {
				continue
			}
			name := strings.TrimLeft(filepath.ToSlash(h.Name), "/")
			if strings.Contains(name, "..") {
				continue
			}
			method := zip.Store // images are compressed already
			if !isPageImage(name) {
				method = zip.Deflate
			}
			w, werr := zw.CreateHeader(&zip.FileHeader{Name: name, Method: method, Modified: h.ModificationTime})
			if werr != nil {
				err = werr
				break
			}
			if _, cerr := io.Copy(w, rc); cerr != nil {
				err = cerr
				break
			}
		}
		rc.Close()
	}
	if cerr := zw.Close(); err == nil {
		err = cerr
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		if after, cerr := comicPageCount(part); cerr != nil || after != want {
			err = fmt.Errorf("the new CBZ has %d pages, the CBR %d", after, want)
		}
	}
	if err != nil {
		_ = os.Remove(part) // our own half-written file
		return err
	}
	return nil
}

// renameComicFile gives a file the name and folder new downloads get. It
// reports whether it moved the file.
func (s *Server) renameComicFile(f comicFile) (bool, error) {
	want, err := s.comicDest(f.series, f.issue, f.issue.FilePath)
	if err != nil {
		return false, err
	}
	if filepath.Clean(want) == filepath.Clean(f.issue.FilePath) {
		return false, nil
	}
	if err := s.moveComic(f, want, true); err != nil {
		return false, err
	}
	return true, nil
}

// moveComic moves an issue's file to dst (never over another file) and
// records the new place.
func (s *Server) moveComic(f comicFile, dst string, tidy bool) error {
	src := f.issue.FilePath
	if _, err := os.Stat(src); err != nil {
		return errors.New("the file is missing")
	}
	if !strings.EqualFold(filepath.Clean(src), filepath.Clean(dst)) {
		if _, err := os.Stat(dst); err == nil {
			return errors.New("another file already has that name")
		}
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err != nil {
		if err := copyThenRemove(src, dst); err != nil {
			return err
		}
	}
	if tidy {
		if root := s.comicsRoot(); root != "" {
			removeEmptyParents(root, src) // only folders left empty
		}
	}
	st, _ := os.Stat(dst)
	var size int64
	if st != nil {
		size = st.Size()
	}
	return s.comicRepo().SetState(f.issue.ID, comics.StatusDownloaded, dst, size)
}

// copyThenRemove moves a file across drives: copy, check the size, remove.
func copyThenRemove(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	part := dst + ".part"
	out, err := os.Create(part)
	if err != nil {
		return err
	}
	n, err := io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	st, serr := os.Stat(src)
	if err == nil && (serr != nil || st.Size() != n) {
		err = errors.New("the copy is incomplete")
	}
	if err != nil {
		_ = os.Remove(part)
		return err
	}
	if err := os.Rename(part, dst); err != nil {
		_ = os.Remove(part)
		return err
	}
	in.Close()
	return os.Remove(src)
}
