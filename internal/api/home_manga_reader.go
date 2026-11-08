package api

// Usherr: reading manga. The pages are unpacked into the same small cache as the
// Comics reader's (home_comics_reader.go), under a name of their own that changes
// when the file does. What someone read and how they like to read are theirs; see
// internal/manga/reading.go.

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "golang.org/x/image/webp" // pages may be WebP

	"github.com/rdborg/mediarium/internal/books"
	"github.com/rdborg/mediarium/internal/manga"
)

const mangaContinueMax = 12

func (s *Server) registerMangaReaderRoutes(member routeGroup, on func(http.HandlerFunc) http.HandlerFunc) {
	member.HandleFunc("GET /api/manga/continue", on(s.handleMangaContinue))
	member.HandleFunc("GET /api/manga/volumes/{id}/pages", on(s.handleMangaPages))
	member.HandleFunc("GET /api/manga/volumes/{id}/pages/{n}", on(s.handleMangaPage))
	member.HandleFunc("GET /api/manga/volumes/{id}/pages/{n}/panels", on(s.handleMangaPagePanels))
	member.HandleFunc("GET /api/manga/volumes/{id}/marks", on(s.handleMangaMarks))
	member.HandleFunc("POST /api/manga/volumes/{id}/marks", on(s.handleAddMangaMark))
	member.HandleFunc("PUT /api/manga/{id}/reader", on(s.handleMangaReaderPrefs))
	member.HandleFunc("PUT /api/manga/reading/mark", on(s.handleMangaMarkRead))
}

// mangaCacheName is a volume's folder in the page cache: another file for the
// volume is another name, so old pages can't be shown for it.
func mangaCacheName(v manga.Volume) (string, error) {
	st, err := os.Stat(v.FilePath)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("manga-%d-%d", v.ID, st.Size()), nil
}

// mangaPages unpacks a volume's pages (once) and lists their files in reading order.
func (s *Server) mangaPages(v manga.Volume) ([]string, error) {
	name, err := mangaCacheName(v)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(filepath.Ext(v.FilePath), ".epub") {
		return s.pagesInCacheBy(name, v.FilePath, unpackMangaEPUB)
	}
	return s.pagesInCache(name, v.FilePath)
}

// unpackMangaEPUB writes an EPUB's pictures into dir, numbered in the book's own
// reading order, which isn't always the order of their names.
func unpackMangaEPUB(file, dir string) error {
	images, pictures, err := books.EPUBPictures(file)
	if err != nil {
		return fmt.Errorf("open the EPUB: %w", err)
	}
	if !pictures {
		return errors.New("this EPUB is mostly words, so it looks like the novel and not a volume of pictures")
	}
	zr, err := zip.OpenReader(file)
	if err != nil {
		return fmt.Errorf("open the EPUB: %w", err)
	}
	defer zr.Close()
	for i, name := range images {
		rc, err := zr.Open(name)
		if err != nil {
			return err
		}
		out, err := os.Create(filepath.Join(dir, fmt.Sprintf("%04d%s", i+1, strings.ToLower(filepath.Ext(name)))))
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, io.LimitReader(rc, 64<<20))
		rc.Close()
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// mangaFileValue is the Manga value of a volume's ComicInfo.xml ("" when it has
// none), from the file, or from what was kept when a RAR was unpacked.
func (s *Server) mangaFileValue(v manga.Volume) string {
	if info, ok := books.ReadComicInfo(v.FilePath); ok {
		return info.Manga
	}
	name, err := mangaCacheName(v)
	if err != nil {
		return ""
	}
	f, err := os.Open(filepath.Join(s.issuePagesDir(), name, comicInfoName))
	if err != nil {
		return ""
	}
	defer f.Close()
	info, _ := books.ParseComicInfo(f)
	return info.Manga
}

// pageSizes are the width and height of each page (0, 0 where a page can't be
// read that way), kept beside the pages so a big volume is only read once. The
// reader lays pages out with them: a wide page is a two-page spread on its own.
func pageSizes(pages []string) [][2]int {
	if len(pages) == 0 {
		return nil
	}
	keep := filepath.Join(filepath.Dir(pages[0]), ".sizes.json")
	var out [][2]int
	if b, err := os.ReadFile(keep); err == nil && json.Unmarshal(b, &out) == nil && len(out) == len(pages) {
		return out
	}
	out = make([][2]int, len(pages))
	for i, p := range pages {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		c, _, err := image.DecodeConfig(f)
		f.Close()
		if err == nil {
			out[i] = [2]int{c.Width, c.Height}
		}
	}
	if b, err := json.Marshal(out); err == nil {
		_ = os.WriteFile(keep, b, 0o644)
	}
	return out
}

// GET /api/manga/volumes/{id}/pages: how many pages there are, how this person
// reads them, and what comes after.
func (s *Server) handleMangaPages(w http.ResponseWriter, r *http.Request) {
	v, sr, ok := s.mangaVolumeFromPath(w, r)
	if !ok {
		return
	}
	if v.FilePath == "" {
		writeError(w, http.StatusNotFound, "This volume has no file yet.")
		return
	}
	repo := s.mangaRepo()
	how := manga.ResolveReader(sr.Format, s.mangaFileValue(v), repo.ReaderPrefsFor(requestUserID(r), sr.ID))
	out := map[string]any{
		"title": fmt.Sprintf("%s, Volume %s", sr.Title, v.Number), "seriesId": sr.ID, "number": v.Number, "format": sr.Format,
		"direction": how.Direction, "layout": how.Layout, "shift": how.Shift == 1,
	}
	if strings.EqualFold(filepath.Ext(v.FilePath), ".pdf") {
		out["pdf"] = true // the browser draws its pages, from the file
	} else {
		pages, err := s.mangaPages(v)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, "This file's pages can't be read: "+err.Error())
			return
		}
		out["pages"], out["sizes"] = len(pages), pageSizes(pages)
	}
	s.fillMangaNext(out, r, sr.ID, false, v.ID)
	writeJSON(w, http.StatusOK, out)
}

// mangaNextVolume is what comes after a volume, for the reader's last page.
type mangaNextVolume struct {
	ID      int64  `json:"id"`
	Number  string `json:"number"`
	Have    bool   `json:"have"`
	Wanted  bool   `json:"wanted,omitempty"`
	Chapter bool   `json:"chapter,omitempty"` // a chapter, not a volume
}

// fillMangaNext says what comes after a volume or chapter, in the series' reading
// order, which runs across both.
func (s *Server) fillMangaNext(out map[string]any, r *http.Request, seriesID int64, chapter bool, id int64) {
	order, err := s.mangaRepo().ReadingOrderFor(requestUserID(r), seriesID)
	if err != nil {
		return
	}
	next, have := manga.NextInOrder(order, chapter, id)
	if next != nil {
		out["next"] = mangaNextVolume{ID: next.ID, Number: next.Number, Have: next.Have, Wanted: next.Wanted, Chapter: next.Chapter}
	}
	if have != nil && have != next {
		out["nextHave"] = mangaNextVolume{ID: have.ID, Number: have.Number, Have: true, Chapter: have.Chapter}
	}
}

// GET /api/manga/volumes/{id}/pages/{n}: one page (1-based); ?w= a copy that wide.
func (s *Server) handleMangaPage(w http.ResponseWriter, r *http.Request) {
	v, _, ok := s.mangaVolumeFromPath(w, r)
	if !ok {
		return
	}
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || n < 1 || v.FilePath == "" {
		writeError(w, http.StatusNotFound, "No such page.")
		return
	}
	pages, err := s.mangaPages(v)
	if err != nil || n > len(pages) {
		writeError(w, http.StatusNotFound, "No such page.")
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if wd, err := strconv.Atoi(r.URL.Query().Get("w")); err == nil && wd >= 600 && wd <= 2400 {
		if sized := sizedPage(pages[n-1], wd/100*100); sized != "" {
			http.ServeFile(w, r, sized)
			return
		}
	}
	http.ServeFile(w, r, pages[n-1])
}

// GET /api/manga/volumes/{id}/marks
func (s *Server) handleMangaMarks(w http.ResponseWriter, r *http.Request) {
	if v, _, ok := s.mangaVolumeFromPath(w, r); ok {
		s.listMarks(w, r, manga.PositionItem(v.ID))
	}
}

// POST /api/manga/volumes/{id}/marks
func (s *Server) handleAddMangaMark(w http.ResponseWriter, r *http.Request) {
	if v, _, ok := s.mangaVolumeFromPath(w, r); ok {
		s.addMark(w, r, manga.PositionItem(v.ID), false)
	}
}

// PUT /api/manga/{id}/reader {"direction", "layout"}: how this person reads this series.
func (s *Server) handleMangaReaderPrefs(w http.ResponseWriter, r *http.Request) {
	sr, ok := s.mangaFromPath(w, r)
	if !ok {
		return
	}
	var p manga.ReaderPrefs
	if err := decodeJSON(r, &p); err != nil || !manga.ValidDirection(p.Direction) || !manga.ValidLayout(p.Layout) {
		writeError(w, http.StatusBadRequest, "That isn't a way to read.")
		return
	}
	if err := s.mangaRepo().SetReaderPrefs(requestUserID(r), sr.ID, p); err != nil {
		writeError(w, http.StatusInternalServerError, "The choice couldn't be saved.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// PUT /api/manga/reading/mark {"volumeId", "upTo", "read"}: this volume, and with
// upTo every one before it, read or unread.
func (s *Server) handleMangaMarkRead(w http.ResponseWriter, r *http.Request) {
	var req struct {
		VolumeID int64 `json:"volumeId"`
		UpTo     bool  `json:"upTo"`
		Read     bool  `json:"read"`
	}
	if err := decodeJSON(r, &req); err != nil || req.VolumeID <= 0 {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	repo := s.mangaRepo()
	v, err := repo.GetVolume(req.VolumeID)
	if err != nil {
		writeError(w, http.StatusNotFound, "That volume isn't in your library.")
		return
	}
	if sr, err := repo.GetSeries(v.SeriesID); err != nil || (sr.IsAdult && !s.canSeeAdultManga(r)) {
		writeError(w, http.StatusNotFound, "That volume isn't in your library.")
		return
	}
	ids := []int64{v.ID}
	if req.UpTo {
		list, err := repo.Volumes(v.SeriesID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "The volumes couldn't be read.")
			return
		}
		ids = ids[:0]
		for _, o := range list {
			ids = append(ids, o.ID)
			if o.ID == v.ID {
				break
			}
		}
	}
	if err := repo.MarkRead(requestUserID(r), ids, req.Read, time.Now()); err != nil {
		writeError(w, http.StatusInternalServerError, "That couldn't be saved.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"changed": len(ids)})
}

// GET /api/manga/continue: this person's Continue row.
func (s *Server) handleMangaContinue(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.mangaContinueFor(r, 0))
}

// mangaContinueFor is the Continue row (or a series' line of it), dressed for the
// page, leaving out what this account may not see.
func (s *Server) mangaContinueFor(r *http.Request, seriesID int64) []manga.ContinueItem {
	list, err := s.mangaRepo().Continue(requestUserID(r), seriesID, mangaContinueMax)
	out := []manga.ContinueItem{}
	if err != nil {
		return out
	}
	adult := s.canSeeAdultManga(r)
	for _, c := range list {
		if c.IsAdult && !adult {
			continue
		}
		c.CoverURL = s.mangaImage(c.CoverURL)
		out = append(out, c)
	}
	return out
}

// mangaPlaceSaved is told each place the reader sends: reaching the last page
// finishes the volume, read here or later from a device that was offline.
func (s *Server) mangaPlaceSaved(user int64, item string, position, duration float64) {
	if s.dexOnlinePlaceSaved(user, item, position, duration) { // a MangaDex chapter read online
		return
	}
	if id, ok := manga.ChapterOfItem(item); ok {
		if duration > 0 && position >= duration {
			_ = s.mangaRepo().MarkChaptersRead(user, []int64{id}, true, time.Now())
		}
		return
	}
	id, ok := manga.VolumeOfItem(item)
	if !ok || duration <= 0 || position < duration {
		return
	}
	_ = s.mangaRepo().MarkRead(user, []int64{id}, true, time.Now())
}
