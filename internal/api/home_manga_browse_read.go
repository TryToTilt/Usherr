package api

// Usherr: reading a MangaDex chapter online, in Usherr's own manga reader, without
// downloading it.
//
// How the pages travel: Usherr fetches each page from the MangaDex@Home address it
// is given (GET /at-home/server/{chapter}) and hands it to the browser from its own
// address. The browser never talks to MangaDex, so:
//   - every page from an MD@Home node is reported to api.mangadex.network/report,
//     good or bad, by the same client the chapter downloads use (mangadex.go);
//   - no login or cookie ever goes to an image server;
//   - on a failed page Usherr reports it, asks /at-home/server again for a new
//     address and tries once more, then falls back to the smaller data-saver copy;
//   - the at-home address is used for 10 minutes at most (MangaDex promises 15);
//   - pages are kept on disk for 30 minutes at most, for turning back and for the
//     panel view, then deleted; browsers are told to keep them 15 minutes.
//
// Your place is kept in home_positions as "manga:d:<chapter id>", like any other
// chapter, and manga_dex_reads keeps what the Continue row shows. Finishing a
// chapter that's also in your library marks that one read too.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/manga"
	"github.com/rdborg/mediarium/internal/queue"
)

const (
	keyMangaDexStaffList = "manga.mangadex_staff_list" // another staff list to find the home rows from (an id)

	dexAtHomeTTL     = 10 * time.Minute
	dexOnlinePageTTL = 30 * time.Minute
	dexOnlineSweep   = 5 * time.Minute
)

func (s *Server) registerMangaBrowseRoutes(admin, member routeGroup, on func(http.HandlerFunc) http.HandlerFunc) {
	member.HandleFunc("GET /api/manga/dex/hub", on(s.handleMangaDexHub))
	member.HandleFunc("GET /api/manga/dex/search", on(s.handleMangaDexSearch))
	member.HandleFunc("GET /api/manga/dex/tags", on(s.handleMangaDexTags))
	member.HandleFunc("GET /api/manga/dex/random", on(s.handleMangaDexRandom))
	member.HandleFunc("GET /api/manga/dex/titles/{id}", on(s.handleMangaDexTitle))
	member.HandleFunc("GET /api/manga/dex/covers/{id}/{file}", on(s.handleMangaDexCover))
	member.HandleFunc("GET /api/manga/dex/continue", on(s.handleMangaDexContinue))
	member.HandleFunc("GET /api/manga/dex/chapters/{id}/pages", on(s.handleMangaDexPages))
	member.HandleFunc("GET /api/manga/dex/chapters/{id}/pages/{n}", on(s.handleMangaDexPage))
	member.HandleFunc("GET /api/manga/dex/chapters/{id}/pages/{n}/panels", on(s.handleMangaDexPanels))
	admin.HandleFunc("POST /api/manga/dex/chapters/{id}/download", on(s.handleMangaDexDownload))
}

// ---- the at-home address and the pages ----

type dexHomeEntry struct {
	at    manga.AtHome
	until time.Time
}

type dexOnlineState struct {
	mu        sync.Mutex
	homes     map[string]dexHomeEntry
	locks     map[string]*sync.Mutex
	lastSweep time.Time
}

var dexOnlineStates sync.Map

func (s *Server) dexOnline() *dexOnlineState {
	v, _ := dexOnlineStates.LoadOrStore(s, &dexOnlineState{homes: map[string]dexHomeEntry{}, locks: map[string]*sync.Mutex{}})
	return v.(*dexOnlineState)
}

// dexAtHomeFor is where a chapter's pages are; fresh asks again, after a failure.
func (s *Server) dexAtHomeFor(ctx context.Context, chapterID string, fresh bool) (manga.AtHome, error) {
	st := s.dexOnline()
	st.mu.Lock()
	e, ok := st.homes[chapterID]
	st.mu.Unlock()
	if ok && !fresh && time.Now().Before(e.until) {
		return e.at, nil
	}
	at, err := s.mangaMod().dex.AtHome(ctx, chapterID)
	if err != nil {
		return at, err
	}
	st.mu.Lock()
	now := time.Now()
	for k, old := range st.homes {
		if now.After(old.until) {
			delete(st.homes, k)
		}
	}
	st.homes[chapterID] = dexHomeEntry{at: at, until: now.Add(dexAtHomeTTL)}
	st.mu.Unlock()
	return at, nil
}

func (s *Server) dexOnlineRoot() string { return filepath.Join(s.issuePagesDir(), "mangadex-online") }

func (s *Server) dexChapterLock(id string) *sync.Mutex {
	st := s.dexOnline()
	st.mu.Lock()
	defer st.mu.Unlock()
	l, ok := st.locks[id]
	if !ok {
		if len(st.locks) > 200 {
			st.locks = map[string]*sync.Mutex{}
		}
		l = &sync.Mutex{}
		st.locks[id] = l
	}
	return l
}

// sweepDexOnline deletes pages kept longer than the short while allowed.
func (s *Server) sweepDexOnline(now time.Time) {
	st := s.dexOnline()
	st.mu.Lock()
	if now.Sub(st.lastSweep) < dexOnlineSweep {
		st.mu.Unlock()
		return
	}
	st.lastSweep = now
	st.mu.Unlock()
	entries, err := os.ReadDir(s.dexOnlineRoot())
	if err != nil {
		return
	}
	for _, e := range entries {
		info, err := e.Info()
		if err == nil && now.Sub(info.ModTime()) > dexOnlinePageTTL {
			_ = os.RemoveAll(filepath.Join(s.dexOnlineRoot(), e.Name()))
		}
	}
}

// dexOnlinePage is page n of a chapter on disk, fetched now if it isn't there.
func (s *Server) dexOnlinePage(ctx context.Context, chapterID string, n int) (string, error) {
	s.sweepDexOnline(time.Now())
	lock := s.dexChapterLock(chapterID)
	lock.Lock()
	defer lock.Unlock()
	dir := filepath.Join(s.dexOnlineRoot(), chapterID)
	if done, err := filepath.Glob(filepath.Join(dir, fmt.Sprintf("%04d.*", n))); err == nil && len(done) > 0 {
		if info, err := os.Stat(done[0]); err == nil && time.Since(info.ModTime()) < dexOnlinePageTTL {
			return done[0], nil
		}
		_ = os.Remove(done[0])
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	client := s.mangaMod().dex
	saver := s.mangaDexSaver()
	at, err := s.dexAtHomeFor(ctx, chapterID, false)
	if err != nil {
		return "", err
	}
	try := func(at manga.AtHome, saver bool) (string, error) {
		files := at.Files(saver)
		if n < 1 || n > len(files) {
			return "", manga.ErrNotFound
		}
		return fetchMangaDexPage(ctx, client, at, saver, files[n-1], dir, n)
	}
	path, err := try(at, saver)
	if errors.Is(err, manga.ErrDexPage) { // reported; now a new address, as the rules say
		if at, err = s.dexAtHomeFor(ctx, chapterID, true); err != nil {
			return "", err
		}
		path, err = try(at, saver)
		if errors.Is(err, manga.ErrDexPage) && !saver {
			path, err = try(at, true) // the smaller copy, from the same new address
		}
	}
	return path, err
}

// ---- the chapter ----

// dexOnlineChapter is what's known of a chapter being read.
type dexOnlineChapter struct {
	Info  manga.DexChapterInfo
	Title manga.DexTitle
}

// dexChapterFor reads a chapter and its series (kept a few minutes) and checks
// this account may read it. It writes the answer itself when it can't.
func (s *Server) dexChapterFor(w http.ResponseWriter, r *http.Request) (dexOnlineChapter, bool) {
	if !s.needDex(w) {
		return dexOnlineChapter{}, false
	}
	id := strings.ToLower(r.PathValue("id"))
	if !manga.IsDexID(id) {
		writeError(w, http.StatusNotFound, "That isn't a MangaDex chapter.")
		return dexOnlineChapter{}, false
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	ch, err := dexCachedAnswer(s, "chapter:"+id, dexTitleTTL, func() (dexOnlineChapter, error) {
		info, err := s.mangaMod().dex.Chapter(ctx, id)
		if err != nil {
			return dexOnlineChapter{}, err
		}
		t, err := s.dexTitleFor(ctx, info.MangaID)
		return dexOnlineChapter{Info: info, Title: t}, err
	})
	switch {
	case err != nil:
		writeError(w, http.StatusBadGateway, s.dexProblem(err))
		return ch, false
	case !dexRatingAllowed(ch.Title.Rating, manga.SafeRatings) && !s.dexAdultAllowed(r):
		writeError(w, http.StatusNotFound, "That chapter is for accounts allowed 18+ content.")
		return ch, false
	case ch.Info.ExternalURL != "":
		writeError(w, http.StatusConflict, "This chapter is read on another site, so it can't be read here.")
		return ch, false
	case ch.Info.Unavailable:
		writeError(w, http.StatusNotFound, "This chapter was taken down from MangaDex.")
		return ch, false
	}
	return ch, true
}

func dexChapterLabel(number string) string {
	if number == "" || strings.HasPrefix(number, manga.OneshotNumber) {
		return "Oneshot"
	}
	return "Chapter " + number
}

// dexReaderWay is how a title reads when nobody has chosen: Korean and Chinese
// comics are one long strip, Japanese ones right to left, others left to right.
func dexReaderWay(t manga.DexTitle) (direction, layout string) {
	for _, tag := range t.Tags {
		if strings.EqualFold(tag, "Long Strip") || strings.EqualFold(tag, "Web Comic") && t.OriginalLanguage != "ja" {
			return "ltr", "webtoon"
		}
	}
	switch {
	case strings.HasPrefix(t.OriginalLanguage, "ko"), strings.HasPrefix(t.OriginalLanguage, "zh"):
		return "ltr", "webtoon"
	case t.OriginalLanguage == "ja":
		return "rtl", ""
	}
	return "ltr", ""
}

type dexNextChapter struct {
	ID     string `json:"id"`
	Number string `json:"number"`
}

// GET /api/manga/dex/chapters/{id}/pages: what the reader needs, like a downloaded
// chapter's, with the chapter list for the picker and the groups credited.
func (s *Server) handleMangaDexPages(w http.ResponseWriter, r *http.Request) {
	ch, ok := s.dexChapterFor(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	at, err := s.dexAtHomeFor(ctx, ch.Info.ID, false)
	if err != nil {
		writeError(w, http.StatusBadGateway, s.dexProblem(err))
		return
	}
	pages := len(at.Files(s.mangaDexSaver()))
	if pages == 0 {
		writeError(w, http.StatusNotFound, "This chapter has no pages on MangaDex.")
		return
	}
	user := requestUserID(r)
	t := ch.Title
	direction, layout := dexReaderWay(t)
	seriesID := s.dexLibraryNow().of(t)
	if seriesID > 0 {
		if sr, err := s.mangaRepo().GetSeries(seriesID); err == nil {
			how := manga.ResolveReader(sr.Format, "", s.mangaRepo().ReaderPrefsFor(user, sr.ID))
			direction = how.Direction
			if how.Layout != "" {
				layout = how.Layout
			}
		}
	}
	groups := make([]string, 0, len(ch.Info.Groups))
	for _, g := range ch.Info.Groups {
		groups = append(groups, g.Name)
	}
	number := ch.Info.Number
	if number == "" {
		number = manga.OneshotNumber
	}
	out := map[string]any{
		"title": t.Title + ", " + dexChapterLabel(ch.Info.Number), "seriesId": seriesID, "number": number, "chapter": true,
		"direction": direction, "layout": layout, "pages": pages, "group": strings.Join(groups, " & "),
	}
	dex := map[string]any{"mangaId": t.ID, "chapterId": ch.Info.ID, "series": t.Title, "chapterTitle": ch.Info.Title, "volume": ch.Info.Volume, "groups": groups, "canDownload": isAdminRequest(r) && seriesID > 0}
	ratings := manga.SafeRatings
	if s.dexAdultAllowed(r) {
		ratings = manga.AllRatings
	}
	if feed, err := s.dexFeedFor(ctx, t.ID, ratings); err == nil {
		list := dexReadable(feed, manga.CopyOf(ch.Info.DexChapter).GroupKey)
		readable := make([]dexChapterRow, 0, len(list))
		for _, c := range list {
			if c.ExternalURL == "" {
				readable = append(readable, c)
			}
		}
		at := -1
		for i, c := range readable {
			if c.ID == ch.Info.ID || (c.Number == number && at < 0) {
				at = i
			}
		}
		if at >= 0 {
			if at+1 < len(readable) {
				dex["next"] = dexNextChapter{ID: readable[at+1].ID, Number: readable[at+1].Number}
			}
			if at > 0 {
				dex["prev"] = dexNextChapter{ID: readable[at-1].ID, Number: readable[at-1].Number}
			}
		}
		dex["chapters"] = readable
	}
	out["dex"] = dex
	adult := !dexRatingAllowed(t.Rating, manga.SafeRatings)
	_, _ = s.db.Exec(`INSERT INTO manga_dex_reads (user_id, chapter_id, manga_id, title, number, cover_file, adult, opened_ms) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (user_id, chapter_id) DO UPDATE SET title = excluded.title, number = excluded.number, cover_file = excluded.cover_file, adult = excluded.adult, opened_ms = excluded.opened_ms`,
		user, ch.Info.ID, t.ID, t.Title, number, t.CoverFile, adult, time.Now().UnixMilli())
	writeJSON(w, http.StatusOK, out)
}

func dexPageNumber(w http.ResponseWriter, r *http.Request) (int, bool) {
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || n < 1 || n > 2000 {
		writeError(w, http.StatusNotFound, "No such page.")
		return 0, false
	}
	return n, true
}

// GET /api/manga/dex/chapters/{id}/pages/{n}: one page, through Usherr.
func (s *Server) handleMangaDexPage(w http.ResponseWriter, r *http.Request) {
	ch, ok := s.dexChapterFor(w, r)
	if !ok {
		return
	}
	n, ok := dexPageNumber(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	path, err := s.dexOnlinePage(ctx, ch.Info.ID, n)
	switch {
	case errors.Is(err, manga.ErrNotFound):
		writeError(w, http.StatusNotFound, "No such page.")
		return
	case errors.Is(err, manga.ErrDexPage):
		writeError(w, http.StatusBadGateway, "MangaDex's page servers didn't give this page. Try again in a moment.")
		return
	case err != nil:
		writeError(w, http.StatusBadGateway, s.dexProblem(err))
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=900")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFile(w, r, path)
}

// GET /api/manga/dex/chapters/{id}/pages/{n}/panels?rtl=1: the panel view's panels,
// worked out from the page while it's kept, and remembered as long.
func (s *Server) handleMangaDexPanels(w http.ResponseWriter, r *http.Request) {
	ch, ok := s.dexChapterFor(w, r)
	if !ok {
		return
	}
	n, ok := dexPageNumber(w, r)
	if !ok {
		return
	}
	rtl := r.URL.Query().Get("rtl") == "1"
	key := fmt.Sprintf("panels:%s:%d:%v", ch.Info.ID, n, rtl)
	if v, ok := s.dexBrowse().get(key); ok {
		writeJSON(w, http.StatusOK, v)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	path, err := s.dexOnlinePage(ctx, ch.Info.ID, n)
	if err != nil {
		writeError(w, http.StatusNotFound, "No such page.")
		return
	}
	panelWorkMu.Lock()
	res := detectPagePanels(s.panelDetector(ctx), path, rtl)
	panelWorkMu.Unlock()
	s.dexBrowse().put(key, res, dexOnlinePageTTL)
	writeJSON(w, http.StatusOK, res)
}

// ---- your place ----

// mangaDexOnlineItem is a chapter's key in home_positions.
func mangaDexOnlineItem(chapterID string) string { return "manga:d:" + chapterID }

// dexOnlinePlaceSaved is told each place saved for a chapter read online: reaching
// its last page marks the same chapter in your library read, when it's there.
func (s *Server) dexOnlinePlaceSaved(user int64, item string, position, duration float64) bool {
	id, ok := strings.CutPrefix(item, "manga:d:")
	if !ok {
		return false
	}
	if !manga.IsDexID(id) || duration <= 0 || position < duration {
		return true
	}
	rows, err := s.db.Query(`SELECT id FROM manga_chapters WHERE dex_id = ? UNION SELECT chapter_id FROM manga_chapter_copies WHERE dex_id = ?`, id, id)
	if err != nil {
		return true
	}
	var ids []int64
	for rows.Next() {
		var c int64
		if rows.Scan(&c) == nil {
			ids = append(ids, c)
		}
	}
	rows.Close()
	if len(ids) > 0 {
		_ = s.mangaRepo().MarkChaptersRead(user, ids, true, time.Now())
	}
	return true
}

// mangaDexContinue is a chapter read online and not finished.
type mangaDexContinue struct {
	ChapterID string `json:"dexChapterId"`
	MangaID   string `json:"dexMangaId"`
	Series    string `json:"series"`
	Number    string `json:"number"`
	CoverURL  string `json:"coverUrl,omitempty"`
	Page      int    `json:"page"`
	Pages     int    `json:"pages"`
}

// GET /api/manga/dex/continue: the chapters you're part way through online, one a
// series, newest first.
func (s *Server) handleMangaDexContinue(w http.ResponseWriter, r *http.Request) {
	out := []mangaDexContinue{}
	if !s.dexOn() {
		writeJSON(w, http.StatusOK, out)
		return
	}
	rows, err := s.db.Query(`SELECT r.chapter_id, r.manga_id, r.title, r.number, r.cover_file, r.adult, p.position, p.duration
		FROM manga_dex_reads r JOIN home_positions p ON p.user_id = r.user_id AND p.item = 'manga:d:' || r.chapter_id
		WHERE r.user_id = ? ORDER BY p.updated_ms DESC LIMIT 100`, requestUserID(r))
	if err != nil {
		writeJSON(w, http.StatusOK, out)
		return
	}
	defer rows.Close()
	adult := s.dexAdultAllowed(r)
	seen := map[string]bool{}
	for rows.Next() {
		var c mangaDexContinue
		var cover string
		var isAdult bool
		var pos, dur float64
		if rows.Scan(&c.ChapterID, &c.MangaID, &c.Series, &c.Number, &cover, &isAdult, &pos, &dur) != nil {
			continue
		}
		if seen[c.MangaID] || (isAdult && !adult) {
			continue
		}
		seen[c.MangaID] = true
		if dur > 0 && pos >= dur {
			continue // finished
		}
		c.Page, c.Pages = int(pos), int(dur)
		c.CoverURL = s.dexCoverURL(c.MangaID, cover)
		out = append(out, c)
		if len(out) == mangaContinueMax {
			break
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// ---- downloading it ----

// dexLibraryChapter is the library's row for a MangaDex chapter, by any upload of it.
func (s *Server) dexLibraryChapter(seriesID int64, dexID, number string) (manga.Chapter, bool) {
	var id int64
	err := s.db.QueryRow(`SELECT c.id FROM manga_chapters c WHERE c.series_id = ? AND (c.dex_id = ? OR EXISTS (SELECT 1 FROM manga_chapter_copies k WHERE k.chapter_id = c.id AND k.dex_id = ?) OR c.number = ?)
		ORDER BY c.dex_id = ? DESC LIMIT 1`, seriesID, dexID, dexID, number, dexID).Scan(&id)
	if err != nil {
		return manga.Chapter{}, false
	}
	c, err := s.mangaRepo().GetChapter(id)
	return c, err == nil
}

// POST /api/manga/dex/chapters/{id}/download: hands a chapter read online to the
// usual chapter download, for a series in the library.
func (s *Server) handleMangaDexDownload(w http.ResponseWriter, r *http.Request) {
	ch, ok := s.dexChapterFor(w, r)
	if !ok {
		return
	}
	seriesID := s.dexLibraryNow().of(ch.Title)
	if seriesID == 0 {
		writeError(w, http.StatusConflict, "Add this series to your library first, then its chapters can be downloaded.")
		return
	}
	repo := s.mangaRepo()
	sr, err := repo.GetSeries(seriesID)
	if err != nil {
		writeError(w, http.StatusNotFound, "That series isn't in your library.")
		return
	}
	if !s.mangaSourcesFor(sr).MangaDex {
		writeError(w, http.StatusConflict, "Chapters come from MangaDex, which is off for this series. Turn it on in Settings > Manga.")
		return
	}
	if problem := s.mangaFolderProblem(); problem != "" {
		writeError(w, http.StatusConflict, problem)
		return
	}
	number := ch.Info.Number
	if number == "" {
		number = manga.OneshotNumber
	}
	c, found := s.dexLibraryChapter(sr.ID, ch.Info.ID, number)
	if !found {
		// The library hasn't read this series' chapters yet: link it, if it isn't, and look now.
		d := repo.DexStateOf(sr.ID)
		if d.Link != manga.DexLinked {
			d.SeriesID, d.DexID, d.Link, d.LinkHow, d.Candidates = sr.ID, ch.Title.ID, manga.DexLinked, "person", nil
			_ = repo.SaveDexState(d)
		}
		st := s.mangaMod()
		st.dexJob.Lock()
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
		err := s.checkMangaDexSeries(ctx, sr)
		cancel()
		st.dexJob.Unlock()
		if err != nil {
			writeError(w, http.StatusBadGateway, s.dexProblem(err))
			return
		}
		if c, found = s.dexLibraryChapter(sr.ID, ch.Info.ID, number); !found {
			writeError(w, http.StatusConflict, "Usherr couldn't find this chapter in the series' list yet. Try again in a few minutes.")
			return
		}
	}
	if c.Status == manga.StatusDownloaded {
		writeJSON(w, http.StatusOK, map[string]any{"message": "That chapter is already downloaded.", "chapterId": c.ID})
		return
	}
	if _, err := repo.SetChaptersWanted([]int64{c.ID}, true); err != nil {
		writeError(w, http.StatusInternalServerError, "The chapter couldn't be changed.")
		return
	}
	qid, err := s.grabMangaDexBatch(sr, []manga.Chapter{c}, queue.PriorityManual)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"queueId": qid, "chapterId": c.ID, "message": "Downloading it now. It goes in your library with the series' other chapters."})
}
