package api

// Usherr: the Chapters tab's routes, and reading a chapter. Members read; an
// administrator chooses what's followed. Nothing here asks MangaDex while its switch
// in Settings > Manga is off.

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/books"
	"github.com/rdborg/mediarium/internal/manga"
	"github.com/rdborg/mediarium/internal/queue"
)

const mangaChapterGetMax = 20 // what one press of a button may ask for

func (s *Server) registerMangaChapterRoutes(admin, member routeGroup, on func(http.HandlerFunc) http.HandlerFunc) {
	member.HandleFunc("GET /api/manga/{id}/chapters", on(s.handleMangaChapters))
	member.HandleFunc("GET /api/manga/chapters/{id}/pages", on(s.handleMangaChapterPages))
	member.HandleFunc("GET /api/manga/chapters/{id}/pages/{n}", on(s.handleMangaChapterPage))
	member.HandleFunc("GET /api/manga/chapters/{id}/pages/{n}/panels", on(s.handleMangaChapterPagePanels))
	member.HandleFunc("GET /api/manga/chapters/{id}/file", on(s.handleMangaChapterFile))
	member.HandleFunc("PUT /api/manga/reading/chapter-mark", on(s.handleMangaChapterMarkRead))
	admin.HandleFunc("PUT /api/manga/{id}/mangadex", on(s.handlePutMangaDex))
	admin.HandleFunc("POST /api/manga/{id}/chapters/check", on(s.handleCheckMangaChapters))
	admin.HandleFunc("POST /api/manga/{id}/chapters/get", on(s.handleGetMangaChapters))
	admin.HandleFunc("POST /api/manga/{id}/chapters/tidy", on(s.handleTidyMangaChapters))
	admin.HandleFunc("PUT /api/manga/chapters/wanted", on(s.handleSetMangaChaptersWanted))
}

// mangaChapterSummary is the series page's short word on chapters, without the list.
type mangaChapterSummary struct {
	Allowed     bool               `json:"allowed"` // MangaDex is switched on, and this series may use it
	Follow      string             `json:"follow"`
	Link        string             `json:"link"`
	Total       int                `json:"total"`
	Have        int                `json:"have"`
	Wanted      int                `json:"wanted"`
	Stats       manga.DexStats     `json:"stats"`
	Empties     int                `json:"empties,omitempty"`
	Trouble     string             `json:"trouble,omitempty"`
	CheckedAt   string             `json:"checkedAt,omitempty"`
	NextAt      string             `json:"nextAt,omitempty"`
	Tonight     bool               `json:"tonight,omitempty"`   // a big catch-up waiting for 2 to 9
	FirstHave   int64              `json:"firstHave,omitempty"` // the first chapter here that no volume holds
	FirstNumber string             `json:"firstNumber,omitempty"`
	Away        *mangaDexAwayState `json:"away,omitempty"`
}

func (s *Server) mangaChapterSummary(sr manga.Series) mangaChapterSummary {
	repo := s.mangaRepo()
	d := repo.DexStateOf(sr.ID)
	out := mangaChapterSummary{Allowed: s.mangaSourcesFor(sr).MangaDex, Follow: d.Follow, Link: d.Link, Stats: d.Stats, Empties: d.Empties, Trouble: d.Trouble, CheckedAt: d.CheckedAt, NextAt: d.NextAt}
	if list, err := repo.Chapters(sr.ID); err == nil {
		out.Total = len(list)
		for _, c := range list {
			switch {
			case c.Status == manga.StatusDownloaded:
				out.Have++
				if out.FirstHave == 0 && c.InVolumeID == 0 {
					out.FirstHave, out.FirstNumber = c.ID, c.Number
				}
			case c.Wanted && c.InVolumeID == 0 && c.ExternalURL == "" && !c.Unavailable:
				out.Wanted++
			}
		}
	}
	if out.Allowed {
		out.Away = s.mangaDexAway()
	}
	now := time.Now()
	if _, total, err := repo.ChaptersToFetch(sr.ID, now, 1); err == nil && total > mangaDexDayBatch && !mangaDexWindow(now) && d.Follow != manga.FollowOff {
		out.Tonight = true
	}
	return out
}

// GET /api/manga/{id}/chapters: the Chapters tab.
func (s *Server) handleMangaChapters(w http.ResponseWriter, r *http.Request) {
	sr, ok := s.mangaFromPath(w, r)
	if !ok {
		return
	}
	repo := s.mangaRepo()
	list, err := repo.Chapters(sr.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The chapters couldn't be read.")
		return
	}
	admin := isAdminRequest(r)
	if !admin {
		for i := range list {
			list[i].FilePath = ""
		}
	}
	_ = repo.FillChapterReading(requestUserID(r), sr.ID, list)
	groups, _ := repo.ChapterGroups(sr.ID)
	d := repo.DexStateOf(sr.ID)
	out := map[string]any{"chapters": list, "groups": groups, "summary": s.mangaChapterSummary(sr), "preferGroup": d.PreferGroup, "dexId": d.DexID}
	if admin {
		out["candidates"] = d.Candidates
	}
	writeJSON(w, http.StatusOK, out)
}

// PUT /api/manga/{id}/mangadex {"follow", "dexId", "preferGroup"}: only what's sent
// changes. dexId is a person's word on which entry this is; "" sets it free to look again.
func (s *Server) handlePutMangaDex(w http.ResponseWriter, r *http.Request) {
	sr, ok := s.mangaFromPath(w, r)
	if !ok {
		return
	}
	var req struct {
		Follow      *string `json:"follow"`
		DexID       *string `json:"dexId"`
		PreferGroup *string `json:"preferGroup"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	repo := s.mangaRepo()
	d := repo.DexStateOf(sr.ID)
	d.SeriesID = sr.ID
	look := false
	if req.DexID != nil {
		id := strings.TrimSpace(*req.DexID)
		if link := manga.DexIDFromLink(id); link != "" {
			id = link
		}
		switch {
		case id == "":
			d.DexID, d.Link, d.LinkHow = "", "", ""
		case !manga.IsDexID(id):
			writeError(w, http.StatusBadRequest, "That isn't a MangaDex address.")
			return
		default:
			d.DexID, d.Link, d.LinkHow, d.Candidates = strings.ToLower(id), manga.DexLinked, "person", nil
		}
		d.NextAt, d.Empties = "", 0
		look = true
	}
	if req.PreferGroup != nil {
		d.PreferGroup = *req.PreferGroup
		if err := repo.RechooseCopies(sr.ID, d.PreferGroup); err != nil {
			writeError(w, http.StatusInternalServerError, "The choice couldn't be saved.")
			return
		}
	}
	if err := repo.SaveDexState(d); err != nil {
		writeError(w, http.StatusInternalServerError, "The choice couldn't be saved.")
		return
	}
	if req.Follow != nil {
		f := *req.Follow
		if f != manga.FollowOff && f != manga.FollowNew && f != manga.FollowAll {
			writeError(w, http.StatusBadRequest, "That isn't a choice of chapters.")
			return
		}
		if f == manga.FollowOff {
			d.Follow = f
			if err := repo.SaveDexState(d); err != nil {
				writeError(w, http.StatusInternalServerError, "The choice couldn't be saved.")
				return
			}
			look = false
		} else {
			if !s.mangaSourcesFor(sr).MangaDex {
				writeError(w, http.StatusConflict, "Chapters come from MangaDex, which is off for this series. Turn it on in Settings > Manga.")
				return
			}
			if problem := s.mangaFolderProblem(); problem != "" {
				writeError(w, http.StatusConflict, problem)
				return
			}
			s.followMangaChapters(sr, f)
			look = false
		}
	}
	if look && s.mangaSourcesFor(sr).MangaDex {
		s.lookAtMangaDexSoon(sr)
	}
	writeJSON(w, http.StatusOK, s.mangaChapterSummary(sr))
}

// POST /api/manga/{id}/chapters/check: look now, unless that was a moment ago or
// MangaDex asked to be left alone.
func (s *Server) handleCheckMangaChapters(w http.ResponseWriter, r *http.Request) {
	sr, ok := s.mangaFromPath(w, r)
	if !ok {
		return
	}
	if !s.mangaSourcesFor(sr).MangaDex {
		writeError(w, http.StatusConflict, "Chapters come from MangaDex, which is off for this series. Turn it on in Settings > Manga.")
		return
	}
	repo := s.mangaRepo()
	d := repo.DexStateOf(sr.ID)
	if away := s.mangaDexAway(); away != nil {
		writeError(w, http.StatusConflict, "MangaDex asked Usherr to wait, or isn't answering, so it isn't being asked right now. It tries again by itself.")
		return
	}
	if t, err := time.Parse(time.RFC3339, d.CheckedAt); err == nil && time.Since(t) < mangaDexManualEvery {
		writeJSON(w, http.StatusOK, map[string]string{"message": "Usherr looked a moment ago."})
		return
	}
	s.lookAtMangaDexSoon(sr)
	writeJSON(w, http.StatusAccepted, map[string]string{"message": "Looking on MangaDex now."})
}

// POST /api/manga/{id}/chapters/get {"ids":[...]} or {"next":10}: download these now,
// a handful at most. A bigger catch-up waits for the night.
func (s *Server) handleGetMangaChapters(w http.ResponseWriter, r *http.Request) {
	sr, ok := s.mangaFromPath(w, r)
	if !ok {
		return
	}
	if !s.mangaSourcesFor(sr).MangaDex {
		writeError(w, http.StatusConflict, "Chapters come from MangaDex, which is off for this series. Turn it on in Settings > Manga.")
		return
	}
	var req struct {
		IDs  []int64 `json:"ids"`
		Next int     `json:"next"`
	}
	if err := decodeJSON(r, &req); err != nil || len(req.IDs) > mangaChapterGetMax || req.Next > mangaChapterGetMax || req.Next < 0 {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Choose up to %d chapters.", mangaChapterGetMax))
		return
	}
	repo := s.mangaRepo()
	if len(req.IDs) > 0 {
		owned := make([]int64, 0, len(req.IDs))
		for _, id := range req.IDs {
			if c, err := repo.GetChapter(id); err == nil && c.SeriesID == sr.ID {
				owned = append(owned, id)
			}
		}
		if _, err := repo.SetChaptersWanted(owned, true); err != nil {
			writeError(w, http.StatusInternalServerError, "The chapters couldn't be changed.")
			return
		}
	}
	limit := req.Next
	if limit == 0 {
		limit = max(len(req.IDs), 1)
	}
	list, _, err := repo.ChaptersToFetch(sr.ID, time.Now(), mangaChapterGetMax)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The chapters couldn't be read.")
		return
	}
	if len(req.IDs) > 0 {
		chosen := map[int64]bool{}
		for _, id := range req.IDs {
			chosen[id] = true
		}
		kept := list[:0]
		for _, c := range list {
			if chosen[c.ID] {
				kept = append(kept, c)
			}
		}
		list = kept
	}
	if len(list) > limit {
		list = list[:limit]
	}
	if len(list) == 0 {
		writeError(w, http.StatusConflict, "None of those can be downloaded right now.")
		return
	}
	id, err := s.grabMangaDexBatch(sr, list, queue.PriorityManual)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"queueId": id, "chapters": len(list)})
}

// PUT /api/manga/chapters/wanted {"ids":[...], "wanted":true}
func (s *Server) handleSetMangaChaptersWanted(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs    []int64 `json:"ids"`
		Wanted bool    `json:"wanted"`
	}
	if err := decodeJSON(r, &req); err != nil || len(req.IDs) == 0 || len(req.IDs) > 5000 {
		writeError(w, http.StatusBadRequest, "Choose some chapters.")
		return
	}
	n, err := s.mangaRepo().SetChaptersWanted(req.IDs, req.Wanted)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The chapters couldn't be changed.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"changed": n})
}

// POST /api/manga/{id}/chapters/tidy {"undo":false}: set aside the chapters a volume
// here holds, or put them back.
func (s *Server) handleTidyMangaChapters(w http.ResponseWriter, r *http.Request) {
	sr, ok := s.mangaFromPath(w, r)
	if !ok {
		return
	}
	var req struct {
		Undo bool `json:"undo"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	n, err := s.tidyMangaChapters(sr, req.Undo)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"moved": n})
}

// ---- reading a chapter ----

func (s *Server) mangaChapterFromPath(w http.ResponseWriter, r *http.Request) (manga.Chapter, manga.Series, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a chapter id.")
		return manga.Chapter{}, manga.Series{}, false
	}
	c, err := s.mangaRepo().GetChapter(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "That chapter isn't in your library.")
		return manga.Chapter{}, manga.Series{}, false
	}
	sr, err := s.mangaRepo().GetSeries(c.SeriesID)
	if err != nil || (sr.IsAdult && !s.canSeeAdultManga(r)) {
		writeError(w, http.StatusNotFound, "That chapter isn't in your library.")
		return manga.Chapter{}, manga.Series{}, false
	}
	return c, sr, true
}

func (s *Server) mangaChapterPages(c manga.Chapter) ([]string, error) {
	st, err := os.Stat(c.FilePath)
	if err != nil {
		return nil, err
	}
	return s.pagesInCache(fmt.Sprintf("manga-chapter-%d-%d", c.ID, st.Size()), c.FilePath)
}

// GET /api/manga/chapters/{id}/pages: as for a volume.
func (s *Server) handleMangaChapterPages(w http.ResponseWriter, r *http.Request) {
	c, sr, ok := s.mangaChapterFromPath(w, r)
	if !ok {
		return
	}
	if c.FilePath == "" {
		writeError(w, http.StatusNotFound, "This chapter has no file yet.")
		return
	}
	repo := s.mangaRepo()
	var fileManga string
	if info, ok := books.ReadComicInfo(c.FilePath); ok {
		fileManga = info.Manga
	}
	how := manga.ResolveReader(sr.Format, fileManga, repo.ReaderPrefsFor(requestUserID(r), sr.ID))
	out := map[string]any{
		"title": fmt.Sprintf("%s, Chapter %s", sr.Title, c.Number), "seriesId": sr.ID, "number": c.Number, "format": sr.Format,
		"direction": how.Direction, "layout": how.Layout, "shift": how.Shift == 1, "chapter": true, "group": c.Group,
	}
	if strings.EqualFold(filepath.Ext(c.FilePath), ".pdf") {
		out["pdf"] = true
	} else {
		pages, err := s.mangaChapterPages(c)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, "This file's pages can't be read: "+err.Error())
			return
		}
		out["pages"], out["sizes"] = len(pages), pageSizes(pages)
	}
	s.fillMangaNext(out, r, sr.ID, true, c.ID)
	writeJSON(w, http.StatusOK, out)
}

// GET /api/manga/chapters/{id}/pages/{n}: one page (1-based); ?w= a copy that wide.
func (s *Server) handleMangaChapterPage(w http.ResponseWriter, r *http.Request) {
	c, _, ok := s.mangaChapterFromPath(w, r)
	if !ok {
		return
	}
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || n < 1 || c.FilePath == "" {
		writeError(w, http.StatusNotFound, "No such page.")
		return
	}
	pages, err := s.mangaChapterPages(c)
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

// GET /api/manga/chapters/{id}/file
func (s *Server) handleMangaChapterFile(w http.ResponseWriter, r *http.Request) {
	c, _, ok := s.mangaChapterFromPath(w, r)
	if !ok {
		return
	}
	if c.FilePath == "" {
		writeError(w, http.StatusNotFound, "This chapter has no file yet.")
		return
	}
	f, err := os.Open(c.FilePath)
	if err != nil {
		writeError(w, http.StatusNotFound, "The file isn't there any more.")
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusNotFound, "The file isn't there any more.")
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(c.FilePath)))
	http.ServeContent(w, r, filepath.Base(c.FilePath), st.ModTime(), f)
}

// PUT /api/manga/reading/chapter-mark {"chapterId", "upTo", "read"}
func (s *Server) handleMangaChapterMarkRead(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ChapterID int64 `json:"chapterId"`
		UpTo      bool  `json:"upTo"`
		Read      bool  `json:"read"`
	}
	if err := decodeJSON(r, &req); err != nil || req.ChapterID <= 0 {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	repo := s.mangaRepo()
	c, err := repo.GetChapter(req.ChapterID)
	if err != nil {
		writeError(w, http.StatusNotFound, "That chapter isn't in your library.")
		return
	}
	if sr, err := repo.GetSeries(c.SeriesID); err != nil || (sr.IsAdult && !s.canSeeAdultManga(r)) {
		writeError(w, http.StatusNotFound, "That chapter isn't in your library.")
		return
	}
	ids := []int64{c.ID}
	if req.UpTo {
		list, err := repo.Chapters(c.SeriesID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "The chapters couldn't be read.")
			return
		}
		ids = ids[:0]
		for _, o := range list {
			ids = append(ids, o.ID)
			if o.ID == c.ID {
				break
			}
		}
	}
	if err := repo.MarkChaptersRead(requestUserID(r), ids, req.Read, time.Now()); err != nil {
		writeError(w, http.StatusInternalServerError, "That couldn't be saved.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"changed": len(ids)})
}
