package api

// Usherr: the panels of a manga page, for the reader's panel-by-panel view. The
// same detector as the Comics reader's (home_comic_panels.go): each page is worked
// out once, kept in manga_panels or manga_chapter_panels, and the next few are worked
// out ahead. A PDF's pages are drawn in the browser, so it has no panels here.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"

	"github.com/rdborg/mediarium/internal/manga"
)

// panelShelf is where a book of pages keeps its panels, and how to find its pages.
type panelShelf struct {
	table, idColumn string
	id              int64
	file            string
	pages           func() ([]string, error)
}

func (s *Server) volumePanelShelf(v manga.Volume) panelShelf {
	return panelShelf{"manga_panels", "volume_id", v.ID, v.FilePath, func() ([]string, error) { return s.mangaPages(v) }}
}

func (s *Server) chapterPanelShelf(c manga.Chapter) panelShelf {
	return panelShelf{"manga_chapter_panels", "chapter_id", c.ID, c.FilePath, func() ([]string, error) { return s.mangaChapterPages(c) }}
}

// mangaPagePanels returns page n's panels in the order wanted, working them out
// when they aren't kept yet.
func (s *Server) mangaPagePanels(ctx context.Context, sh panelShelf, n int, rtl bool) (pagePanels, error) {
	var res pagePanels
	info, err := os.Stat(sh.file)
	if err != nil {
		return res, err
	}
	order := 0
	if rtl {
		order = 1
	}
	kept := func() bool {
		var stored string
		err := s.db.QueryRowContext(ctx, `SELECT method, panels FROM `+sh.table+` WHERE `+sh.idColumn+` = ? AND page = ? AND file_size = ? AND rtl = ?`, sh.id, n, info.Size(), order).Scan(&res.Method, &stored)
		return err == nil && json.Unmarshal([]byte(stored), &res.Panels) == nil
	}
	if kept() {
		return res, nil
	}
	pages, err := sh.pages()
	if err != nil {
		return res, err
	}
	if n < 1 || n > len(pages) {
		return res, errors.New("no such page")
	}
	panelWorkMu.Lock()
	defer panelWorkMu.Unlock()
	if kept() { // another request may have worked it out meanwhile
		return res, nil
	}
	res = detectPagePanels(s.panelDetector(ctx), pages[n-1], rtl)
	b, _ := json.Marshal(res.Panels)
	_, _ = s.db.ExecContext(ctx, `INSERT OR REPLACE INTO `+sh.table+` (`+sh.idColumn+`, page, file_size, method, panels, rtl) VALUES (?, ?, ?, ?, ?, ?)`, sh.id, n, info.Size(), res.Method, string(b), order)
	return res, nil
}

// answerMangaPanels answers for page n of a volume or chapter, with the next pages
// worked out in the background.
func (s *Server) answerMangaPanels(w http.ResponseWriter, r *http.Request, sh panelShelf) {
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || n < 1 || sh.file == "" {
		writeError(w, http.StatusNotFound, "No such page.")
		return
	}
	rtl := r.URL.Query().Get("rtl") == "1"
	res, err := s.mangaPagePanels(r.Context(), sh, n, rtl)
	if err != nil {
		writeError(w, http.StatusNotFound, "No such page.")
		return
	}
	s.background(func() {
		for k := n + 1; k <= n+3; k++ {
			if _, err := s.mangaPagePanels(context.Background(), sh, k, rtl); err != nil {
				return
			}
		}
	})
	w.Header().Set("Cache-Control", "private, max-age=3600")
	writeJSON(w, http.StatusOK, res)
}

// GET /api/manga/volumes/{id}/pages/{n}/panels?rtl=1: page n's panels in reading
// order, as fractions of the page.
func (s *Server) handleMangaPagePanels(w http.ResponseWriter, r *http.Request) {
	if v, _, ok := s.mangaVolumeFromPath(w, r); ok {
		s.answerMangaPanels(w, r, s.volumePanelShelf(v))
	}
}

// GET /api/manga/chapters/{id}/pages/{n}/panels?rtl=1: the same for a chapter.
func (s *Server) handleMangaChapterPagePanels(w http.ResponseWriter, r *http.Request) {
	if c, _, ok := s.mangaChapterFromPath(w, r); ok {
		s.answerMangaPanels(w, r, s.chapterPanelShelf(c))
	}
}
