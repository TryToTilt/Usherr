package api

// Home fork: reading comics in the browser. A CBZ is a zip of images and a
// CBR a RAR of them (many ".cbr" files are really zips, so the first bytes
// decide). On first open an issue's pages are unpacked into a small cache in
// the downloads volume (fast disk); the last 20 issues read are kept. PDFs
// are handed to the browser's own viewer.

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nwaples/rardecode/v2"
)

const comicCacheKeep = 20

var comicCacheMu sync.Mutex

func (s *Server) issuePagesDir() string {
	return filepath.Join(s.downloadsRoot(), ".comic-pages")
}

func isPageImage(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif", ".avif":
		base := strings.ToLower(filepath.Base(name))
		return !strings.HasPrefix(base, ".") && !strings.HasPrefix(name, "__MACOSX")
	}
	return false
}

// pageNameLess orders page names the way people number them: "2" before "10".
func pageNameLess(a, b string) bool {
	a, b = strings.ToLower(a), strings.ToLower(b)
	for a != "" && b != "" {
		ad, bd := a[0] >= '0' && a[0] <= '9', b[0] >= '0' && b[0] <= '9'
		if ad && bd {
			i, j := 0, 0
			for i < len(a) && a[i] >= '0' && a[i] <= '9' {
				i++
			}
			for j < len(b) && b[j] >= '0' && b[j] <= '9' {
				j++
			}
			na, _ := strconv.Atoi(a[:i])
			nb, _ := strconv.Atoi(b[:j])
			if na != nb {
				return na < nb
			}
			a, b = a[i:], b[j:]
			continue
		}
		if a[0] != b[0] {
			return a[0] < b[0]
		}
		a, b = a[1:], b[1:]
	}
	return len(a) < len(b)
}

// comicPages unpacks an issue's pages (once) and returns their files in
// reading order.
func (s *Server) comicPages(issueID int64, file string) ([]string, error) {
	comicCacheMu.Lock()
	defer comicCacheMu.Unlock()
	dir := filepath.Join(s.issuePagesDir(), strconv.FormatInt(issueID, 10))
	done := filepath.Join(dir, ".done")
	if _, err := os.Stat(done); err != nil {
		_ = os.RemoveAll(dir)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
		if err := unpackComic(file, dir); err != nil {
			_ = os.RemoveAll(dir)
			return nil, err
		}
		_ = os.WriteFile(done, []byte(file), 0o644)
		s.trimComicCache()
	}
	_ = os.Chtimes(done, time.Now(), time.Now())
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var pages []string
	for _, e := range entries {
		if !e.IsDir() && isPageImage(e.Name()) {
			pages = append(pages, filepath.Join(dir, e.Name()))
		}
	}
	sort.Slice(pages, func(i, j int) bool { return pageNameLess(filepath.Base(pages[i]), filepath.Base(pages[j])) })
	if len(pages) == 0 {
		return nil, errors.New("this file has no pages to show")
	}
	return pages, nil
}

// trimComicCache keeps the most recently read issues.
func (s *Server) trimComicCache() {
	entries, err := os.ReadDir(s.issuePagesDir())
	if err != nil || len(entries) <= comicCacheKeep {
		return
	}
	type item struct {
		path string
		t    time.Time
	}
	var items []item
	for _, e := range entries {
		p := filepath.Join(s.issuePagesDir(), e.Name())
		st, err := os.Stat(filepath.Join(p, ".done"))
		if err != nil {
			items = append(items, item{p, time.Time{}})
			continue
		}
		items = append(items, item{p, st.ModTime()})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].t.After(items[j].t) })
	for _, it := range items[comicCacheKeep:] {
		_ = os.RemoveAll(it.path)
	}
}

// unpackComic writes a comic's images into dir, flattened and numbered so
// pages from subfolders keep their order and can't escape dir.
func unpackComic(file, dir string) error {
	head := make([]byte, 8)
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	n, _ := io.ReadFull(f, head)
	f.Close()
	head = head[:n]
	type page struct {
		name string
		data []byte
	}
	var pages []page
	const maxPage = 64 << 20
	switch {
	case bytes.HasPrefix(head, []byte("PK")):
		zr, err := zip.OpenReader(file)
		if err != nil {
			return fmt.Errorf("open the comic: %w", err)
		}
		defer zr.Close()
		for _, zf := range zr.File {
			if zf.FileInfo().IsDir() || !isPageImage(zf.Name) {
				continue
			}
			rc, err := zf.Open()
			if err != nil {
				return err
			}
			b, err := io.ReadAll(io.LimitReader(rc, maxPage))
			rc.Close()
			if err != nil {
				return err
			}
			pages = append(pages, page{zf.Name, b})
		}
	case bytes.HasPrefix(head, []byte("Rar!")):
		rc, err := rardecode.OpenReader(file, rardecode.BufferSize(4<<20))
		if err != nil {
			return fmt.Errorf("open the comic: %w", err)
		}
		defer rc.Close()
		for {
			h, err := rc.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return fmt.Errorf("read the comic: %w", err)
			}
			if h.IsDir || !isPageImage(h.Name) {
				continue
			}
			b, err := io.ReadAll(io.LimitReader(rc, maxPage))
			if err != nil {
				return err
			}
			pages = append(pages, page{h.Name, b})
		}
	default:
		return errors.New("this comic's format can't be shown in the browser (open the file instead)")
	}
	sort.Slice(pages, func(i, j int) bool { return pageNameLess(pages[i].name, pages[j].name) })
	for i, p := range pages {
		out := filepath.Join(dir, fmt.Sprintf("%04d%s", i+1, strings.ToLower(filepath.Ext(p.name))))
		if err := os.WriteFile(out, p.data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// GET /api/comics/issues/{id}/pages: how many pages there are to read.
func (s *Server) handleComicPages(w http.ResponseWriter, r *http.Request) {
	is, sr, ok := s.comicIssueFromPath(w, r)
	if !ok {
		return
	}
	if is.FilePath == "" {
		writeError(w, http.StatusNotFound, "This issue has no file yet.")
		return
	}
	if strings.EqualFold(filepath.Ext(is.FilePath), ".pdf") {
		writeJSON(w, http.StatusOK, map[string]any{"pdf": true, "title": sr.Name + " #" + is.Number})
		return
	}
	pages, err := s.comicPages(is.ID, is.FilePath)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"pages": len(pages), "title": sr.Name + " #" + is.Number})
}

// GET /api/comics/issues/{id}/pages/{n}: one page (1-based).
func (s *Server) handleComicIssuePage(w http.ResponseWriter, r *http.Request) {
	is, _, ok := s.comicIssueFromPath(w, r)
	if !ok {
		return
	}
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || n < 1 || is.FilePath == "" {
		writeError(w, http.StatusNotFound, "No such page.")
		return
	}
	pages, err := s.comicPages(is.ID, is.FilePath)
	if err != nil || n > len(pages) {
		writeError(w, http.StatusNotFound, "No such page.")
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// home fork: ?w= sends a copy sized for the screen (phones)
	if wd, err := strconv.Atoi(r.URL.Query().Get("w")); err == nil && wd >= 600 && wd <= 2400 {
		if sized := sizedPage(pages[n-1], wd/100*100); sized != "" {
			http.ServeFile(w, r, sized)
			return
		}
	}
	http.ServeFile(w, r, pages[n-1])
}
