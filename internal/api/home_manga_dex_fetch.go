package api

// Usherr: downloading chapters from MangaDex. One line in Activity per batch, not
// one per chapter. A batch takes no place in the download line once it's started, so
// it can't keep a film waiting, and it never touches Usenet connections. It has a
// slot of its own: one batch at a time, one page at a time, at MangaDex's pace. Pages
// wait in the batch's work folder, so a stop or a restart carries on where it was.
// Chapters go to Series/Chapters/, each with who translated it in the file.

import (
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif"  // pages may be GIF
	_ "image/jpeg" // pages may be JPEG
	_ "image/png"  // pages may be PNG
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/manga"
	"github.com/rdborg/mediarium/internal/organizer"
	"github.com/rdborg/mediarium/internal/plainerror"
	"github.com/rdborg/mediarium/internal/queue"
)

const (
	mangaDexPageTries = 3 // new at-home addresses asked for, per chapter, before it waits
)

var mangaDexProtocol = indexers.Protocol(mangaSourceDex)

// grabMangaDexBatch puts chapters in the download line as one item. Earlier failed
// batches of the series are settled by it: the chapters are all still wanted.
func (s *Server) grabMangaDexBatch(sr manga.Series, chapters []manga.Chapter, prio queue.Priority) (int64, error) {
	if problem := s.mangaFolderProblem(); problem != "" {
		return 0, errors.New(problem)
	}
	repo := s.mangaRepo()
	s.grabMu.Lock()
	defer s.grabMu.Unlock()
	if busy, err := repo.ActiveDexBatch(sr.ID); err != nil {
		return 0, err
	} else if busy {
		return 0, errors.New("A download of this series' chapters is already running or waiting.")
	}
	s.discardFailedDexBatches(sr.ID)
	ids := make([]int64, 0, len(chapters))
	for _, c := range chapters {
		ids = append(ids, c.ID)
	}
	title := fmt.Sprintf("%s, %d chapters", sr.Title, len(chapters))
	if len(chapters) == 1 {
		title = fmt.Sprintf("%s, Chapter %s", sr.Title, chapters[0].Number)
	}
	id, err := s.QueueRepo.Enqueue(queue.Item{ReleaseTitle: title, NZBURL: fmt.Sprintf("mangadex:series:%d:%d", sr.ID, time.Now().UnixNano()), Protocol: queue.Protocol(mangaSourceDex), Priority: prio})
	if err != nil {
		return 0, err
	}
	if err := repo.LinkDownload(manga.Download{QueueID: id, SeriesID: sr.ID, Class: string(manga.ClassFan), Source: mangaSourceDex}); err != nil {
		_ = s.QueueRepo.SetStatus(id, queue.StatusFailed, "couldn't link the download to its series")
		return 0, err
	}
	if err := repo.AssignBatch(id, ids); err != nil {
		return 0, err
	}
	_ = s.QueueRepo.LogActivity(id, "grabbed", "Downloading from MangaDex: "+title)
	s.dispatch.Kick()
	return id, nil
}

// discardFailedDexBatches clears a series' failed batches from Activity.
func (s *Server) discardFailedDexBatches(seriesID int64) {
	items, err := s.QueueRepo.List()
	if err != nil {
		return
	}
	for _, it := range items {
		if it.Status != queue.StatusFailed || it.Protocol != queue.Protocol(mangaSourceDex) {
			continue
		}
		if d, err := s.mangaRepo().DownloadFor(it.ID); err == nil && d.SeriesID == seriesID {
			s.removeWorkDir(s.workDirFor(it.ID))
			_ = s.QueueRepo.Delete(it.ID)
		}
	}
}

// retryMangaDexBatch is Retry in Activity: the same chapters, carrying on from the
// pages already here.
func (s *Server) retryMangaDexBatch(w http.ResponseWriter, item queue.Item, sr manga.Series) {
	repo := s.mangaRepo()
	release := s.dispatch.Hold() // the new item must not start before the earlier pages are in its folder
	defer release()
	id, err := s.QueueRepo.Enqueue(queue.Item{ReleaseTitle: item.ReleaseTitle, NZBURL: fmt.Sprintf("mangadex:series:%d:%d", sr.ID, time.Now().UnixNano()), Protocol: queue.Protocol(mangaSourceDex), Priority: queue.PriorityManual})
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err := repo.LinkDownload(manga.Download{QueueID: id, SeriesID: sr.ID, Class: string(manga.ClassFan), Source: mangaSourceDex}); err != nil {
		writeError(w, http.StatusConflict, "The download couldn't be linked to its series.")
		return
	}
	_ = repo.MoveBatch(item.ID, id)
	if list, err := repo.BatchChapters(id); err == nil { // a stop had set them to not wanted
		ids := make([]int64, 0, len(list))
		for _, c := range list {
			ids = append(ids, c.ID)
		}
		_, _ = repo.SetChaptersWanted(ids, true)
	}
	if dir, ok := s.existingWorkDir(item.ID); ok {
		s.moveKeptDownload(dir, id)
	}
	s.queueItemEvent(item, "retried", queue.LevelInfo, "Retrying from MangaDex: "+item.ReleaseTitle)
	_ = s.QueueRepo.Delete(item.ID)
	writeJSON(w, http.StatusAccepted, map[string]int64{"queueId": id})
}

// mangaDexStopped is told when a person stops a batch. They said no, so those
// chapters stop being wanted, or the next look would start them straight away.
func (s *Server) mangaDexStopped(item queue.Item) {
	if item.Protocol != queue.Protocol(mangaSourceDex) {
		return
	}
	repo := s.mangaRepo()
	list, err := repo.BatchChapters(item.ID)
	if err != nil {
		return
	}
	ids := make([]int64, 0, len(list))
	for _, c := range list {
		ids = append(ids, c.ID)
	}
	_, _ = repo.SetChaptersWanted(ids, false)
}

// runMangaDexBatch fetches a batch's chapters, one after the other.
func (s *Server) runMangaDexBatch(queueID int64, sr manga.Series) error {
	ctx, run := s.pipelines.begin(queueID)
	defer s.pipelines.end(queueID, run)
	if s.pausedBeforeStart(queueID) {
		return errPaused
	}
	st := s.mangaMod()
	repo := st.repo
	fail := func(stepErr error) error {
		if run.cancelled.Load() {
			_ = s.QueueRepo.SetStatus(queueID, queue.StatusFailed, errRemovedFromLibrary.Error())
			return errRemovedFromLibrary
		}
		if handled, err := s.settleInterrupted(queueID, run); handled {
			return err
		}
		plain := plainerror.Message(stepErr)
		_ = s.QueueRepo.SetStatus(queueID, queue.StatusFailed, plain)
		_ = s.QueueRepo.LogActivity(queueID, "failed", sr.Title+": "+plain)
		s.notifyItem("failed", mangaItem(sr, "", "MangaDex", plain))
		return stepErr
	}
	if err := s.QueueRepo.SetStatus(queueID, queue.StatusDownloading, ""); err != nil {
		return fail(err)
	}
	setQueueStage(queueID, "Waiting for the last chapters to finish")
	select {
	case st.dexSlot <- struct{}{}:
		defer func() { <-st.dexSlot }()
	case <-ctx.Done():
		return fail(ctx.Err())
	}
	setQueueStage(queueID, "Downloading")
	list, err := repo.BatchChapters(queueID)
	if err != nil {
		return fail(err)
	}
	s.claimMangaChapterFiles(sr)
	dir := s.workDirFor(queueID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fail(err)
	}
	var todo []manga.Chapter
	pagesAll, pagesDone := int64(0), int64(0)
	for _, c := range list {
		// Fresh from the table: it may have arrived, or stopped being wanted, by now.
		if c.Status == manga.StatusDownloaded || c.ExternalURL != "" || c.Unavailable || !c.Wanted || c.InVolumeID > 0 {
			continue
		}
		todo = append(todo, c)
		pagesAll += int64(max(c.Pages, 1))
	}
	progress := newProgressSaver(func(pct float64) error { return s.QueueRepo.SetProgress(queueID, pct) })
	defer progress.Flush()
	filed, troubled := 0, 0
	var lastProblem string
	for _, c := range todo {
		if ctx.Err() != nil {
			return fail(ctx.Err())
		}
		base := pagesDone
		path, err := s.fetchMangaDexChapter(ctx, sr, c, dir, func(page int64) {
			progress.Report(base+page, pagesAll)
		})
		pagesDone = base + int64(max(c.Pages, 1))
		var de *manga.DexError
		switch {
		case err == nil:
			sr, _ = repo.GetSeries(sr.ID) // the first chapter may have set its folder
			filed++
			_ = path
		case ctx.Err() != nil:
			return fail(ctx.Err())
		case errors.As(err, &de):
			// All of MangaDex is away. What's here stays, and nothing more is asked.
			noted, _ := s.noteDexTrouble(err)
			if filed > 0 {
				_ = s.QueueRepo.LogActivity(queueID, "imported", fmt.Sprintf("%s: %d chapters filed before MangaDex stopped answering", sr.Title, filed))
			}
			return fail(fmt.Errorf("%s. Usherr tries again after %s, by itself", noted.Error(), noted.Until.Local().Format("15:04")))
		case errors.Is(err, manga.ErrNotFound):
			_ = repo.MarkChapterGone(c.ID) // it's been taken down since the last look
			troubled++
			lastProblem = "taken down on MangaDex since the last look"
		default:
			plain := plainerror.Message(err)
			_ = repo.ChapterTrouble(c.ID, plain, time.Now())
			troubled++
			lastProblem = plain
			slog.Info("manga: chapter", "series", sr.Title, "chapter", c.Number, "err", err)
		}
	}
	progress.Flush()
	s.clearDexTrouble()
	if filed == 0 && troubled > 0 {
		return fail(fmt.Errorf("no chapter could be fetched: %s", lastProblem))
	}
	if err := s.QueueRepo.SetStatus(queueID, queue.StatusCompleted, ""); err != nil {
		return fail(err)
	}
	s.removeWorkDir(dir)
	note := fmt.Sprintf("%s: %d chapters filed, from MangaDex", sr.Title, filed)
	if troubled > 0 {
		note += fmt.Sprintf(". %d couldn't be fetched, and will be tried again later", troubled)
	}
	_ = s.QueueRepo.LogActivity(queueID, "imported", note)
	if filed > 0 {
		what := fmt.Sprintf("%d chapters", filed)
		if filed == 1 {
			what = "1 chapter"
		}
		s.notifyItem("imported", mangaItem(sr, what, "MangaDex", ""))
	}
	return nil
}

// fetchMangaDexChapter gets a chapter's pages and files it. A failed page is
// reported by the client; then a new address is asked for, a few times at most.
// Pages already here from an earlier try aren't fetched again.
func (s *Server) fetchMangaDexChapter(ctx context.Context, sr manga.Series, c manga.Chapter, dir string, onPage func(int64)) (string, error) {
	dest, err := s.mangaChapterDest(&sr, c.Number, c.Group)
	if err != nil {
		return "", err
	}
	client := s.mangaMod().dex
	saver := s.mangaDexSaver()
	at, err := client.AtHome(ctx, c.DexID)
	if err != nil {
		return "", err
	}
	files := at.Files(saver)
	if len(files) == 0 {
		return "", manga.ErrNotFound
	}
	pageDir := filepath.Join(dir, fmt.Sprintf("chapter-%d", c.ID))
	if err := os.MkdirAll(pageDir, 0o755); err != nil {
		return "", err
	}
	pages := make([]string, len(files))
	tries := 0
	for i := range files {
		done, err := filepath.Glob(filepath.Join(pageDir, fmt.Sprintf("%04d.*", i+1)))
		if err == nil && len(done) > 0 {
			pages[i] = done[0]
			onPage(int64(i + 1))
			continue
		}
		for {
			path, err := fetchMangaDexPage(ctx, client, at, saver, files[i], pageDir, i+1)
			if err == nil {
				pages[i] = path
				onPage(int64(i + 1))
				break
			}
			if !errors.Is(err, manga.ErrDexPage) {
				return "", err
			}
			if tries++; tries >= mangaDexPageTries {
				return "", errors.New("MangaDex's page servers didn't give this chapter's pages")
			}
			fresh, err := client.AtHome(ctx, c.DexID)
			if err != nil {
				return "", err
			}
			at = fresh
			if names := at.Files(saver); len(names) == len(files) {
				files = names
			}
		}
	}
	cbz := filepath.Join(dir, fmt.Sprintf("chapter-%d.cbz", c.ID))
	if err := manga.WriteChapterCBZ(cbz, pages, manga.InfoForChapter(sr, c)); err != nil {
		return "", fmt.Errorf("couldn't pack the chapter: %w", err)
	}
	path, err := s.fileMangaChapter(&sr, c, cbz, dest)
	if err != nil {
		return "", err
	}
	_ = os.RemoveAll(pageDir)
	return path, nil
}

// fetchMangaDexPage fetches one page, and keeps it only if it's really a picture,
// and whole. Its final name is the proof that it is, for a later try.
func fetchMangaDexPage(ctx context.Context, client *manga.MangaDex, at manga.AtHome, saver bool, file, dir string, n int) (string, error) {
	tmp, err := os.CreateTemp(dir, ".page-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if _, err := client.Page(ctx, at, saver, file, tmp); err != nil {
		tmp.Close()
		return "", err
	}
	if _, err := tmp.Seek(0, 0); err != nil {
		tmp.Close()
		return "", err
	}
	_, format, err := image.DecodeConfig(tmp)
	tmp.Close()
	if err != nil {
		return "", manga.ErrDexPage
	}
	ext := "." + format
	if format == "jpeg" {
		ext = ".jpg"
	}
	path := filepath.Join(dir, fmt.Sprintf("%04d%s", n, ext))
	if err := os.Rename(tmp.Name(), path); err != nil {
		return "", err
	}
	return path, nil
}

// mangaChapterFolder is a series' Chapters folder.
func (s *Server) mangaChapterFolder(sr *manga.Series) (string, error) {
	root := s.mangaRoot()
	if root == "" {
		return "", errors.New("the manga folder isn't set. Choose one in Settings > Library > Folders")
	}
	mode, repl := s.illegalCharSettings()
	folder := sr.Folder
	if folder == "" {
		folder = filepath.Join(root, organizer.Sanitize(sr.Title, mode, repl))
	}
	return filepath.Join(folder, "Chapters"), nil
}

// mangaChapterDest is where a chapter's file goes: Series/Chapters/Series c0123 [Group].cbz.
func (s *Server) mangaChapterDest(sr *manga.Series, number, group string) (string, error) {
	folder, err := s.mangaChapterFolder(sr)
	if err != nil {
		return "", err
	}
	mode, repl := s.illegalCharSettings()
	return filepath.Join(folder, organizer.Sanitize(manga.ChapterFileName(sr.Title, number, group), mode, repl)+".cbz"), nil
}

// fileMangaChapter moves a finished chapter into the library and records it. A file
// already at its place is the same chapter: it's recorded, and nothing is filed twice.
func (s *Server) fileMangaChapter(sr *manga.Series, c manga.Chapter, file, dest string) (string, error) {
	repo := s.mangaRepo()
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", fmt.Errorf("couldn't make the Chapters folder: %w", err)
	}
	if st, err := os.Stat(dest); err == nil {
		_ = os.Remove(file)
		return dest, repo.SetChapterFile(c.ID, dest, st.Size())
	}
	res, err := organizer.Import(file, dest, organizer.ConflictSkip, nil)
	if err != nil {
		return "", fmt.Errorf("couldn't move the chapter into your library: %w", err)
	}
	if res.Skipped {
		return "", fmt.Errorf("a file is already at %s", dest)
	}
	if sr.Folder == "" {
		sr.Folder = filepath.Dir(filepath.Dir(res.DestPath))
		_ = repo.SetFolder(sr.ID, sr.Folder)
	}
	var size int64
	if st, err := os.Stat(res.DestPath); err == nil {
		size = st.Size()
	}
	return res.DestPath, repo.SetChapterFile(c.ID, res.DestPath, size)
}

// fileMangaChapterFile files one chapter out of a pack from an indexer.
func (s *Server) fileMangaChapterFile(sr *manga.Series, number, file string, out *mangaFiled) error {
	if _, err := comicPageCount(file); err != nil {
		out.broken++
		out.lastBroken = err
		return nil
	}
	repo := s.mangaRepo()
	c, err := repo.EnsureChapter(sr.ID, number, "file")
	if err != nil {
		return err
	}
	if c.Status == manga.StatusDownloaded {
		out.same++
		return nil
	}
	dest, err := s.mangaChapterDest(sr, number, c.Group)
	if err != nil {
		return err
	}
	dest = strings.TrimSuffix(dest, ".cbz") + strings.ToLower(filepath.Ext(file))
	path, err := s.fileMangaChapter(sr, c, file, dest)
	if err != nil {
		return err
	}
	if strings.EqualFold(filepath.Ext(path), ".cbz") {
		if err := manga.WriteComicInfo(path, manga.InfoFor(*sr, number, string(manga.ClassFan))); err != nil {
			slog.Warn("manga: couldn't add ComicInfo", "file", path, "err", err)
		}
	}
	out.chapters++
	return nil
}

var mangaChapterFileRe = regexp.MustCompile(`(?i)\.(cbz|cbr|zip|pdf)$`)

// claimMangaChapterFiles records chapter files already in a series' Chapters folder,
// so nothing that's here is fetched again: after a series is removed and added back,
// say, or a chapter someone put there.
func (s *Server) claimMangaChapterFiles(sr manga.Series) {
	folder, err := s.mangaChapterFolder(&sr)
	if err != nil {
		return
	}
	entries, err := os.ReadDir(folder)
	if err != nil {
		return
	}
	repo := s.mangaRepo()
	chapters, err := repo.Chapters(sr.ID)
	if err != nil {
		return
	}
	byNumber := map[string]manga.Chapter{}
	for _, c := range chapters {
		byNumber[c.Number] = c
	}
	names := repo.Names(sr)
	for _, e := range entries {
		if e.IsDir() || !mangaChapterFileRe.MatchString(e.Name()) {
			continue
		}
		n := manga.ParseFor(e.Name(), names)
		if n.Kind != manga.KindChapter || n.Chapter == "" || n.ChapterTo != "" {
			continue
		}
		c, ok := byNumber[manga.NormalNumber(n.Chapter)]
		if !ok || c.Status == manga.StatusDownloaded {
			continue
		}
		path := filepath.Join(folder, e.Name())
		if st, err := os.Stat(path); err == nil {
			_ = repo.SetChapterFile(c.ID, path, st.Size())
		}
	}
}

// checkMangaChapterFiles forgets chapter files that have gone, when the folder itself
// is there, and tidies chapters if that's asked for.
func (s *Server) checkMangaChapterFiles() {
	repo := s.mangaRepo()
	files, err := repo.ChapterFiles()
	if err != nil {
		return
	}
	for _, f := range files {
		if _, err := os.Stat(f.Path); errors.Is(err, os.ErrNotExist) {
			_ = repo.ClearChapterFile(f.ID)
		}
	}
	if !s.mangaTidyOn() {
		return
	}
	if list, err := repo.ListSeries(); err == nil {
		for _, sr := range list {
			s.tidyAfterVolume(sr)
		}
	}
}

// tidyAfterVolume sets chapters aside when that's the setting.
func (s *Server) tidyAfterVolume(sr manga.Series) {
	if s.mangaTidyOn() {
		if _, err := s.tidyMangaChapters(sr, false); err != nil {
			slog.Info("manga: tidy chapters", "series", sr.Title, "err", err)
		}
	}
}

// tidyMangaChapters moves the chapters a volume here now holds into .replaced, as
// the tools do with any file that makes way, and never deletes them. Undo puts them
// back. Your place and read marks aren't touched by either.
func (s *Server) tidyMangaChapters(sr manga.Series, undo bool) (int, error) {
	repo := s.mangaRepo()
	chapters, err := repo.Chapters(sr.ID)
	if err != nil {
		return 0, err
	}
	folder, err := s.mangaChapterFolder(&sr)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, c := range chapters {
		if c.Status != manga.StatusDownloaded || c.FilePath == "" {
			continue
		}
		var to string
		switch {
		case undo && c.SetAside:
			to = freeName(filepath.Join(folder, filepath.Base(c.FilePath)))
		case !undo && !c.SetAside && c.InVolumeID > 0:
			to = freeName(s.mangaReplacedPath(c.FilePath))
		default:
			continue
		}
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return n, err
		}
		if err := os.Rename(c.FilePath, to); err != nil {
			return n, fmt.Errorf("couldn't move %s: %w", filepath.Base(c.FilePath), err)
		}
		if err := repo.SetChapterAside(c.ID, to, !undo); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
