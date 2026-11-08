package api

// Usherr: from a finished download to the Manga library. Volumes are filed as
// <root>/<Series>/<Series> v01.cbz, which Komga and Kavita read as they are. A
// file is checked before filing, and a volume you have isn't filed twice: the
// same file is dropped, and another copy waits for you to choose.

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/blocklist"
	"github.com/rdborg/mediarium/internal/books"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/manga"
	"github.com/rdborg/mediarium/internal/notify"
	"github.com/rdborg/mediarium/internal/organizer"
	"github.com/rdborg/mediarium/internal/plainerror"
	"github.com/rdborg/mediarium/internal/queue"
)

// mangaItem is the notification item for a series.
func mangaItem(sr manga.Series, what, release, reason string) notify.Item {
	title := sr.Title
	if what != "" {
		title += " " + what
	}
	return notify.Item{Media: "manga", Title: title, Year: sr.StartYear, Release: release, Reason: reason, PosterURL: sr.CoverURL, LinkPath: fmt.Sprintf("/manga/%d", sr.ID)}
}

// prepareMangaPipeline is preparePipeline's case for a manga download.
func (s *Server) prepareMangaPipeline(item queue.Item, protocol indexers.Protocol) (run func(), ok bool, err error) {
	repo := s.mangaRepo()
	var d manga.Download
	for i := 0; i < 5; i++ {
		d, err = repo.DownloadFor(item.ID)
		if !errors.Is(err, manga.ErrNotFound) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if errors.Is(err, manga.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, true, errors.New("That volume isn't in your library any more.")
	}
	sr, err := repo.GetSeries(d.SeriesID)
	if err != nil {
		return nil, true, errors.New("That series isn't in your library any more.")
	}
	if d.Source == mangaSourceDex {
		return func() {
			if err := s.runMangaDexBatch(item.ID, sr); err != nil {
				slog.Info("queue: mangadex batch ended", "queueId", item.ID, "err", err)
			}
		}, true, nil
	}
	return func() {
		if err := s.runMangaPipeline(item.ID, sr, d, item.ReleaseTitle, item.NZBURL, protocol); err != nil {
			slog.Info("queue: manga download ended", "queueId", item.ID, "err", err)
		}
	}, true, nil
}

// mangaDest is where a volume's file goes.
func (s *Server) mangaDest(sr manga.Series, number, file string) (string, error) {
	root := s.mangaRoot()
	if root == "" {
		return "", errors.New("the manga folder isn't set. Choose one in Settings > Library > Folders")
	}
	mode, repl := s.illegalCharSettings()
	name := organizer.Sanitize(sr.Title, mode, repl)
	folder := sr.Folder
	if folder == "" {
		folder = filepath.Join(root, name)
	}
	return filepath.Join(folder, organizer.Sanitize(manga.FileName(sr.Title, number), mode, repl)+strings.ToLower(filepath.Ext(file))), nil
}

// mangaFiles lists the files of a download that could be volumes.
func mangaFiles(dir string) []string {
	var out []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && manga.IsPictureFile(d.Name()) {
			out = append(out, p)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// openWrappedManga deals with ZIPs left in a finished download. Published
// volumes are often posted as a RAR holding a ZIP, and only the RAR has been
// opened by now. A ZIP of pictures is a CBZ in all but name; one holding a PDF,
// an EPUB or a comic is opened beside itself. Anything else is left alone, and
// mangaHolds says what's there.
func openWrappedManga(dir string) {
	var zips []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.EqualFold(filepath.Ext(d.Name()), ".zip") {
			zips = append(zips, p)
		}
		return nil
	})
	for _, p := range zips {
		images, comicFiles, _, err := archiveHolds(p)
		if err != nil {
			slog.Info("manga: read a ZIP in a download", "file", filepath.Base(p), "err", err)
			continue
		}
		if images > 0 && comicFiles == 0 {
			dst := freeName(strings.TrimSuffix(p, filepath.Ext(p)) + ".cbz")
			if err := os.Rename(p, dst); err != nil {
				slog.Info("manga: name a ZIP of pages", "file", filepath.Base(p), "err", err)
			}
			continue
		}
		if err := organizer.NewExtractor().Extract(p, filepath.Dir(p)); err != nil {
			slog.Info("manga: open a ZIP in a download", "file", filepath.Base(p), "err", err)
		}
	}
}

// openNestedRars unpacks the RAR sets in a download's folders below the top:
// the top holds the download's own archives, which were unpacked already.
// It says whether any set was opened.
func openNestedRars(dir string) bool {
	primaries, _ := organizer.FindPrimaryArchivesDeep(dir)
	opened := false
	for _, p := range primaries {
		if filepath.Dir(p) == filepath.Clean(dir) || !strings.EqualFold(filepath.Ext(p), ".rar") {
			continue
		}
		if err := organizer.NewExtractor().Extract(p, filepath.Dir(p)); err != nil {
			slog.Info("manga: open a RAR set in a download", "file", filepath.Base(p), "err", err)
			continue
		}
		opened = true
	}
	return opened
}

// mangaHolds says in words what a download with nothing to file does hold.
func mangaHolds(dir string) string {
	kinds := map[string]bool{}
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		switch ext := strings.ToLower(filepath.Ext(d.Name())); ext {
		case ".nfo", ".sfv", ".diz", ".txt", ".nzb", ".torrent", ".jpg", ".jpeg", ".png", ".url", "":
		default:
			kinds[strings.ToUpper(strings.TrimPrefix(ext, "."))] = true
		}
		return nil
	})
	if len(kinds) == 0 {
		return "This download has no pages in it: no CBZ, CBR, PDF or EPUB."
	}
	list := make([]string, 0, len(kinds))
	for k := range kinds {
		list = append(list, k)
	}
	sort.Strings(list)
	return "This download holds only " + strings.Join(list, ", ") + " files, which Usherr can't show as pages. It can show CBZ, CBR, PDF and EPUB."
}

// mangaFiled is what happened to a download's files.
type mangaFiled struct {
	filed, same, copies, unnamed, broken int
	doubles                              int // more copies of a volume inside this download, worse than the one filed
	novels                               int // EPUBs that are mostly words
	numbers                              []string
	copyVolumes                          []int64 // volumes this download has another copy of
	chapters                             int     // chapters filed, from a run of them
	runs                                 string  // a single file holding a run of chapters
	lastBroken                           error
}

// fileMangaFile files one file as a volume. A volume that has a file already
// keeps it, unless replace is set.
func (s *Server) fileMangaFile(sr *manga.Series, number, file, class string, replace bool, out *mangaFiled) error {
	repo := s.mangaRepo()
	number = manga.NormalNumber(number)
	if _, err := comicPageCount(file); err != nil {
		out.broken++
		out.lastBroken = err
		return nil
	}
	info, err := os.Stat(file)
	if err != nil {
		return err
	}
	size := info.Size()
	vol, _, err := repo.EnsureVolume(sr.ID, number, true)
	if err != nil {
		return err
	}
	dest, err := s.mangaDest(*sr, number, file)
	if err != nil {
		return err
	}
	existing := vol.FilePath
	if vol.Status != manga.StatusDownloaded {
		existing = ""
	}
	if there, same := copyAt(existing, size); there && (same || size == vol.ReleaseSize) {
		out.same++
		return nil
	} else if there && !replace {
		out.copies++
		out.copyVolumes = append(out.copyVolumes, vol.ID)
		return nil
	} else if there {
		keep := s.mangaReplacedPath(existing)
		if err := os.MkdirAll(filepath.Dir(keep), 0o755); err != nil {
			return err
		}
		if err := os.Rename(existing, freeName(keep)); err != nil {
			return fmt.Errorf("couldn't set your earlier file aside: %w", err)
		}
	}
	if there, same := copyAt(dest, size); there && same && !replace {
		out.same++ // the same file, left by an earlier download
		return repo.SetVolumeFile(vol.ID, dest, size, class)
	} else if there && !replace {
		out.copies++ // a file of ours that wasn't recorded: keep it, and record it
		if info, err := os.Stat(dest); err == nil {
			return repo.SetVolumeFile(vol.ID, dest, info.Size(), string(manga.ClassOwn))
		}
		return nil
	}
	res, err := organizer.Import(file, freeName(dest), organizer.ConflictSkip, nil)
	if err != nil {
		return fmt.Errorf("couldn't move the volume into your library: %w", err)
	}
	if res.Skipped {
		return fmt.Errorf("a file is already at %s", dest)
	}
	released := size
	if strings.EqualFold(filepath.Ext(res.DestPath), ".cbz") {
		if err := manga.WriteComicInfo(res.DestPath, manga.InfoFor(*sr, number, class)); err != nil {
			slog.Warn("manga: couldn't add ComicInfo", "file", res.DestPath, "err", err)
		}
		if st, err := os.Stat(res.DestPath); err == nil {
			size = st.Size()
		}
	}
	if sr.Folder == "" {
		sr.Folder = filepath.Dir(res.DestPath)
		_ = repo.SetFolder(sr.ID, sr.Folder)
	}
	if err := repo.SetVolumeFile(vol.ID, res.DestPath, size, class); err != nil {
		return err
	}
	_ = repo.SetReleaseSize(vol.ID, released)
	out.filed++
	out.numbers = append(out.numbers, number)
	return nil
}

// mangaReplacedPath is where a file goes when another takes its place: the
// manga folder's .replaced folder, which Komga and Kavita skip.
func (s *Server) mangaReplacedPath(file string) string {
	root := s.mangaRoot()
	rel, err := filepath.Rel(root, file)
	if err != nil || strings.HasPrefix(rel, "..") {
		rel = filepath.Base(file)
	}
	return filepath.Join(root, ".replaced", rel)
}

// fileMangaDownload files what a download holds: one volume, or each that a pack holds.
func (s *Server) fileMangaDownload(sr *manga.Series, d manga.Download, files []string, release, linked string) (mangaFiled, error) {
	names := s.mangaRepo().Names(*sr)
	single := len(files) == 1
	var out mangaFiled
	// A pack can hold a volume twice. Only the better copy is filed, so the first
	// one found can't shut out a fuller one.
	type copyOf struct {
		file string
		size int64
	}
	best := map[string]copyOf{}
	var order []string
	for _, f := range files {
		// A run of chapters has its chapters filed as chapters, one file each.
		if ch := manga.ParseFor(filepath.Base(f), names); ch.Kind == manga.KindChapter && d.VolumeID == 0 {
			if ch.ChapterTo != "" {
				out.runs = ch.Chapter + " to " + ch.ChapterTo
				continue
			}
			if ch.Chapter != "" && s.mangaRepo().DexStateOf(sr.ID).Follow != manga.FollowOff {
				if err := s.fileMangaChapterFile(sr, ch.Chapter, f, &out); err != nil {
					return out, err
				}
				continue
			}
		}
		number := mangaNumberOf(f, release, linked, names, single)
		if number == "" {
			out.unnamed++
			continue
		}
		number = manga.NormalNumber(number)
		// An EPUB may be the novel, whatever the release calls itself.
		if strings.EqualFold(filepath.Ext(f), ".epub") {
			if _, pictures, err := books.EPUBPictures(f); err != nil || !pictures {
				out.novels++
				continue
			}
		}
		// "Take it" on a copy from a pack swaps that one volume, and only that.
		if d.Replace && d.VolumeID > 0 && linked != "" && number != manga.NormalNumber(linked) {
			continue
		}
		var size int64
		if st, err := os.Stat(f); err == nil {
			size = st.Size()
		}
		had, ok := best[number]
		switch {
		case !ok:
			order = append(order, number)
			best[number] = copyOf{f, size}
		case manga.FormatRank(f) > manga.FormatRank(had.file),
			manga.FormatRank(f) == manga.FormatRank(had.file) && manga.BetterCopy(filepath.Base(f), size, filepath.Base(had.file), had.size, sr.LicensedEN):
			out.doubles++
			best[number] = copyOf{f, size}
		default:
			out.doubles++
		}
	}
	for _, number := range order {
		if err := s.fileMangaFile(sr, number, best[number].file, d.Class, d.Replace, &out); err != nil {
			return out, err
		}
	}
	return out, nil
}

// mangaNumberOf reads which volume a file is: its own name, else (for a single
// file) the release's name, else the volume the download was for.
func mangaNumberOf(file, release, linked string, names []string, single bool) string {
	read := func(s string) string {
		n := manga.ParseFor(s, names)
		if !n.Known {
			n = manga.Parse(s)
		}
		if n.Kind == manga.KindVolume || n.Kind == manga.KindBare {
			return n.Number
		}
		return ""
	}
	if single {
		// A release's name is a person's or a group's word; a file inside may be
		// called "file_0" or a chapter's page, so the release goes first.
		if num := read(release); num != "" {
			return num
		}
	}
	if num := read(filepath.Base(file)); num != "" {
		return num
	}
	if single {
		return linked
	}
	return ""
}

func (s *Server) runMangaPipeline(queueID int64, sr manga.Series, d manga.Download, releaseTitle, downloadURL string, protocol indexers.Protocol) error {
	ctx, run := s.pipelines.begin(queueID)
	defer s.pipelines.end(queueID, run)
	if s.pausedBeforeStart(queueID) {
		return errPaused
	}
	repo := s.mangaRepo()
	label := sr.Title
	linkedNumber := ""
	if d.VolumeID > 0 {
		if v, err := repo.GetVolume(d.VolumeID); err == nil {
			linkedNumber = v.Number
			label = fmt.Sprintf("%s, Volume %s", sr.Title, v.Number)
		}
	}
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
		_ = s.QueueRepo.LogActivity(queueID, "failed", fmt.Sprintf("%s: %s", label, plain))
		s.notifyItem("failed", mangaItem(sr, "", releaseTitle, plain))
		if isBadRelease(stepErr) {
			_ = s.Blocklist.Add(blocklist.Entry{ReleaseTitle: releaseTitle, Protocol: string(protocol), Reason: stepErr.Error()})
		}
		return stepErr
	}
	if err := s.QueueRepo.SetStatus(queueID, queue.StatusDownloading, ""); err != nil {
		return fail(err)
	}
	dir, err := s.prepareDownload(ctx, queueID, downloadURL, protocol)
	if err != nil {
		return fail(err)
	}
	if ctx.Err() != nil {
		return fail(ctx.Err())
	}
	nameHiddenComics(dir, releaseTitle)
	// A format is no fault of the release, so nothing here blocklists it. It's
	// remembered, so that a search asks a person rather than fetching it again.
	wrongKind := func(why string) error {
		_ = repo.NoteRelease(sr.ID, releaseTitle, why)
		return fail(errors.New(why))
	}
	openWrappedManga(dir)
	// Usherr: publishers' releases often come as ZIPs that each hold one piece
	// of a RAR set (vol.rar in a.zip, vol.r00 in b.zip...). Opening the ZIPs
	// leaves that set in the release's folder: unpack it, then look again.
	for round := 0; round < 2 && len(mangaFiles(dir)) == 0; round++ {
		if !openNestedRars(dir) {
			break
		}
		openWrappedManga(dir)
	}
	files := dropSkipped(dir, mangaFiles(dir), readSkipped(dir)) // volumes the torrent left out
	if len(files) == 0 {
		return wrongKind(mangaHolds(dir))
	}
	out, err := s.fileMangaDownload(&sr, d, files, releaseTitle, linkedNumber)
	if err != nil {
		return fail(err)
	}
	if out.filed == 0 && out.same == 0 && out.copies == 0 && out.chapters == 0 {
		if out.runs != "" {
			return wrongKind("This download is one file holding chapters " + out.runs + ". Usherr keeps chapters one file each, so it can't file this. A volume or your own copy of those chapters will fit.")
		}
		if out.novels > 0 && out.broken == 0 {
			return wrongKind("This download's EPUB is mostly words, so it looks like the novel and not a volume of pictures.")
		}
		if out.broken > 0 && out.unnamed == 0 {
			return fail(badRelease(fmt.Errorf("the file can't be read as a volume: %v", out.lastBroken)))
		}
		// Only the names are in the way: the pack may be fine, so it isn't
		// blocklisted, and what was downloaded is kept for a Retry.
		return fail(errors.New("the file names in this download don't say which volumes they are"))
	}
	if err := s.QueueRepo.SetStatus(queueID, queue.StatusCompleted, ""); err != nil {
		return fail(err)
	}
	// The same file was dropped already; a different one is for a person to judge.
	for _, id := range out.copyVolumes {
		note := "Another copy of a volume you have. Yours is kept unless you take this one."
		if d.VolumeID != id {
			note = "Another copy of a volume you have, from a pack. Yours is kept unless you take this one, which downloads the pack again and swaps only this volume."
		}
		_ = repo.AddReview(manga.Review{SeriesID: sr.ID, VolumeID: id, Kind: "copy", Title: releaseTitle, URL: downloadURL, Protocol: string(protocol), Class: d.Class, Notes: []string{note}})
	}
	s.cleanupWorkDir(dir, protocol)
	_ = s.QueueRepo.LogActivity(queueID, "imported", mangaFiledNote(label, out))
	if out.filed > 0 {
		s.tidyAfterVolume(sr)
		what := ""
		if len(out.numbers) == 1 {
			what = "Volume " + out.numbers[0]
		} else {
			what = fmt.Sprintf("%d volumes", len(out.numbers))
		}
		s.notifyItem("imported", mangaItem(sr, what, releaseTitle, ""))
	}
	slog.Info("manga: imported", "series", sr.Title, "filed", out.filed, "same", out.same, "copies", out.copies)
	return nil
}

// mangaFiledNote says in Activity what a download came to.
func mangaFiledNote(label string, f mangaFiled) string {
	var parts []string
	if f.filed > 0 {
		parts = append(parts, fmt.Sprintf("%d filed", f.filed))
	}
	if f.chapters > 0 {
		parts = append(parts, fmt.Sprintf("%d chapters filed", f.chapters))
	}
	if f.same > 0 {
		parts = append(parts, fmt.Sprintf("%d already in your library, so dropped", f.same))
	}
	if f.copies > 0 {
		parts = append(parts, fmt.Sprintf("%d another copy of a volume you have, not filed", f.copies))
	}
	if f.doubles > 0 {
		parts = append(parts, fmt.Sprintf("%d more copies of a volume in this download, of which the better one was kept", f.doubles))
	}
	if f.novels > 0 {
		parts = append(parts, fmt.Sprintf("%d EPUB that's mostly words, so not filed", f.novels))
	}
	if f.unnamed > 0 {
		parts = append(parts, fmt.Sprintf("%d with a name that doesn't say which volume", f.unnamed))
	}
	if f.broken > 0 {
		parts = append(parts, fmt.Sprintf("%d that can't be read", f.broken))
	}
	return label + ": " + strings.Join(parts, ", ")
}

// retryMangaDownload is Retry in Activity for a manga download: the same release
// again, from what was already downloaded when it's still there. It reports
// false when the queue item isn't a manga download.
func (s *Server) retryMangaDownload(w http.ResponseWriter, item queue.Item) bool {
	repo := s.mangaRepo()
	d, err := repo.DownloadFor(item.ID)
	if errors.Is(err, manga.ErrNotFound) {
		return false
	}
	if err != nil {
		writeError(w, http.StatusNotFound, "That volume isn't in your library any more.")
		return true
	}
	sr, err := repo.GetSeries(d.SeriesID)
	if err != nil {
		writeError(w, http.StatusNotFound, "That series isn't in your library any more.")
		return true
	}
	if d.Source == mangaSourceDex {
		s.retryMangaDexBatch(w, item, sr)
		return true
	}
	keptDir, reuse := s.keptDownload(item)
	release := s.dispatch.Hold() // the new item must not start before the earlier files are in its folder
	newID, err := s.grabManga(sr, d.VolumeID, item.ReleaseTitle, item.NZBURL, item.SizeBytes, indexers.Protocol(item.Protocol), d.Class, queue.PriorityManual, d.Replace)
	if err != nil {
		release()
		writeError(w, http.StatusConflict, err.Error())
		return true
	}
	if reuse {
		s.moveKeptDownload(keptDir, newID)
	}
	release()
	message := "Retrying, downloading again: " + item.ReleaseTitle
	if reuse {
		message = "Retrying from the files already downloaded: " + item.ReleaseTitle
	}
	s.queueItemEvent(item, "retried", queue.LevelInfo, message)
	_ = s.QueueRepo.Delete(item.ID)
	writeJSON(w, http.StatusAccepted, map[string]int64{"queueId": newID})
	return true
}
