package api

// Usherr: looking for manga. A series is first looked for once by its name,
// which costs one search and answers most of what's worth knowing: whether your
// indexers carry it at all, any volumes they have, and any packs. Only then,
// and only if the name turned something up, are single volumes searched, a few
// at a time. A search that finds nothing waits longer each time. All of it goes
// through the usual indexers, with their daily limits, and the usual download line.

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/blocklist"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/manga"
	"github.com/rdborg/mediarium/internal/plainerror"
	"github.com/rdborg/mediarium/internal/queue"
)

// Newznab: Books, Books > EBook and Books > Comics. Real indexers file published
// manga under all three, and with no category at all a title's anime and films
// fill the results. No indexer here has a manga category of its own.
var mangaCategories = []int{7000, 7020, 7030}

const (
	maxMangaSearchesPerHunt = 10 // what one automatic run asks about (each a series or a volume)
	maxMangaGrabsAtOnce     = 10 // what one look may start, however many volumes it finds
	mangaSearchGap          = 1500 * time.Millisecond
)

// mangaRelease is a search result with what's been made of it.
type mangaRelease struct {
	indexers.Result
	Verdict manga.Verdict
	Blocked bool
}

func (r mangaRelease) usable() bool { return r.Verdict.Reject == "" && !r.Blocked }

func (s *Server) mangaTarget(sr manga.Series) manga.Target {
	return manga.Target{Names: s.mangaRepo().Names(sr), StartYear: sr.StartYear, Licensed: sr.LicensedEN, Sources: s.mangaSourcesFor(sr), Notes: s.mangaRepo().ReleaseNotes(sr.ID), FollowsChapters: s.mangaRepo().DexStateOf(sr.ID).Follow != manga.FollowOff}
}

// askIndexers searches each set of words in turn until stop says enough, and
// judges everything that comes back.
func (s *Server) askIndexers(ctx context.Context, target manga.Target, wanted string, queries []string, sum *manga.SearchSummary, stop func([]mangaRelease) bool) []mangaRelease {
	list, nyaa, err := s.mangaInstances(target.Sources.Nyaa)
	if err != nil || len(list)+len(nyaa) == 0 {
		sum.NoIndex = true
		return nil
	}
	blocked := s.blockedKeys()
	allowed := s.sourcesFor("")
	seen := map[string]bool{}
	var out []mangaRelease
	for _, q := range queries {
		if ctx.Err() != nil {
			break
		}
		sum.Query = q
		outcomes := indexers.SearchAll(ctx, append(list[:len(list):len(list)], nyaa...), q, mangaCategories)
		asked, failed, skipped := 0, 0, 0
		for _, o := range outcomes {
			switch {
			case o.Skipped:
				skipped++
			case o.Err != nil:
				failed++
				asked++
			default:
				asked++
			}
		}
		sum.Asked, sum.Failed, sum.Skipped = max(sum.Asked, asked), max(sum.Failed, failed), max(sum.Skipped, skipped)
		for _, res := range indexers.MergeResults(outcomes) {
			if seen[res.DownloadURL] {
				continue
			}
			seen[res.DownloadURL] = true
			res.Title = html.UnescapeString(res.Title)
			v := manga.Evaluate(manga.Release{Title: res.Title, SizeBytes: res.SizeBytes, Categories: res.Categories, Indexer: res.IndexerName}, target, wanted)
			// Nyaa's switch is the choice to take torrents from it, whatever the
			// usual choice between Usenet and torrents is.
			fromNyaa := target.Sources.Nyaa && strings.Contains(strings.ToLower(res.IndexerName), "nyaa")
			if v.Reject == "" && !fromNyaa && len(filterSources([]indexers.Result{res}, allowed)) == 0 {
				v = manga.Verdict{Reject: "A torrent, and torrents are off", Why: manga.RejectTorrent}
			}
			if v.Reject == "" && res.Protocol == indexers.ProtocolTorrent && res.Seeders < 1 {
				v = manga.Verdict{Reject: "Nobody is sharing this torrent right now", Why: manga.RejectDead}
			}
			r := mangaRelease{Result: res, Verdict: v, Blocked: blocked[blocklist.Key(res.Title)]}
			if r.Blocked && v.Reject == "" {
				sum.Found++
				sum.Rejected = noteReject(sum.Rejected, manga.RejectBlocked)
			} else {
				sum.Note(v)
			}
			out = append(out, r)
		}
		if stop != nil && stop(out) {
			break
		}
	}
	sortMangaReleases(out)
	return out
}

func noteReject(m map[string]int, key string) map[string]int {
	if m == nil {
		m = map[string]int{}
	}
	m[key]++
	return m
}

// sortMangaReleases puts the best first: accepted, surer, official over fan,
// the preferred indexer, the fuller file.
func sortMangaReleases(out []mangaRelease) {
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.usable() != b.usable() {
			return a.usable()
		}
		if x, y := mangaConfidenceRank(a.Verdict.Confidence), mangaConfidenceRank(b.Verdict.Confidence); x != y {
			return x < y
		}
		if a.Verdict.Score != b.Verdict.Score {
			return a.Verdict.Score > b.Verdict.Score
		}
		if a.Priority != b.Priority {
			return indexerRank(a.Priority) < indexerRank(b.Priority)
		}
		return a.SizeBytes > b.SizeBytes
	})
}

func mangaConfidenceRank(c string) int {
	switch c {
	case manga.ConfidenceHigh:
		return 0
	case manga.ConfidenceMedium:
		return 1
	}
	return 2
}

// mangaOutcome is what the search log calls a search's result.
func mangaOutcome(sum manga.SearchSummary, grabbed, doubtful int) string {
	switch {
	case grabbed > 0:
		return "grabbed"
	case doubtful > 0:
		return "review"
	case sum.Asked == 0 || sum.Failed == sum.Asked:
		return "unasked"
	}
	return "nothing"
}

// lookForManga searches for a series once by its name, under other titles only
// if that found nothing of it. Confident volumes it wants are grabbed, the
// doubtful ones and packs go on the review list. It reports how many it grabbed
// and whether any release was of this series at all.
func (s *Server) lookForManga(ctx context.Context, seriesID int64, prio queue.Priority, maxGrabs int) (grabbed int, named bool) {
	repo := s.mangaRepo()
	sr, err := repo.GetSeries(seriesID)
	if err != nil {
		return 0, false
	}
	// Nothing is searched for or fetched while nothing could be filed.
	if problem := s.mangaFolderProblem(); problem != "" {
		slog.Warn("manga: not searching", "series", sr.Title, "why", problem)
		return 0, false
	}
	target := s.mangaTarget(sr)
	var sum manga.SearchSummary
	if !target.Sources.Any() {
		return 0, false
	}
	var releases []mangaRelease
	for _, name := range manga.SearchableNames(target.Names, 2) {
		// Only until the series has been seen: one search when the title works.
		found := s.askIndexers(ctx, target, "", []string{manga.SearchName(name)}, &sum, nil)
		releases = append(releases, found...)
		if sum.Named() || sum.NoIndex {
			break
		}
		time.Sleep(s.mangaGap())
	}
	sortMangaReleases(releases)
	if ctx.Err() != nil {
		return 0, false
	}
	_ = repo.ClearReviews(sr.ID, 0)
	grabbed, doubtful := s.takeMangaReleases(sr, releases, prio, maxGrabs)
	sum.Taken, sum.Doubtful = grabbed, doubtful
	_ = repo.LogSearch(sr.ID, 0, mangaOutcome(sum, grabbed, doubtful), sum, time.Now())
	return grabbed, sum.Named()
}

// mangaGap is the pause between searches; tests set it to nothing.
func (s *Server) mangaGap() time.Duration {
	if mangaNoGap {
		return 0
	}
	return mangaSearchGap
}

var mangaNoGap bool

// takeMangaReleases decides what to do with what a look by name found.
func (s *Server) takeMangaReleases(sr manga.Series, releases []mangaRelease, prio queue.Priority, maxGrabs int) (grabbed, doubtful int) {
	repo := s.mangaRepo()
	volumes, err := repo.Volumes(sr.ID)
	if err != nil {
		return 0, 0
	}
	byNumber := map[string]manga.Volume{}
	highest := -1.0
	for _, v := range volumes {
		byNumber[v.Number] = v
		highest = max(highest, manga.SortKey(v.Number))
	}
	hadRows := len(volumes) > 0
	wantedIn := func(lo, hi float64) int {
		n := 0
		for _, v := range byNumber {
			if v.Wanted && v.Status != manga.StatusDownloaded && manga.SortKey(v.Number) >= lo && manga.SortKey(v.Number) <= hi {
				n++
			}
		}
		return n
	}
	handled := map[string]bool{}
	perVolume := map[string]int{}
	packs := 0
	for _, r := range releases {
		if !r.usable() {
			continue
		}
		v := r.Verdict
		switch v.Kind {
		case manga.KindChapter: // a run of chapters, for a series that follows them
			if packs < 5 {
				packs++
				doubtful++
				s.addMangaReview(sr.ID, 0, "pack", r)
			}
		case manga.KindPack, manga.KindRange, manga.KindWhole:
			lo, hi := v.Lo, v.Hi
			if v.Kind == manga.KindWhole {
				lo, hi = 0, 1e9
			}
			if wantedIn(lo, hi) < 2 || packs >= 5 {
				continue
			}
			packs++
			doubtful++
			s.addMangaReview(sr.ID, 0, "pack", r)
		case manga.KindVolume, manga.KindBare:
			vol, ok := byNumber[v.Number]
			if !ok {
				if !v.Confident() {
					continue
				}
				// A confident release of a volume not on the list adds it.
				wanted := sr.WantAll || (sr.MonitorNew && hadRows && manga.SortKey(v.Number) > highest)
				nv, isNew, err := repo.EnsureVolume(sr.ID, v.Number, wanted)
				if err != nil {
					continue
				}
				if isNew {
					s.noteNewMangaVolumes(sr, manga.NewVolumesAfter([]manga.Volume{nv}, highest, hadRows))
				}
				vol = nv
				byNumber[v.Number] = nv
			}
			if !vol.Wanted || vol.Status == manga.StatusDownloaded || handled[v.Number] {
				continue
			}
			if busy, _ := repo.ActiveForVolume(vol.ID); busy {
				handled[v.Number] = true
				continue
			}
			if v.Confident() {
				handled[v.Number] = true
				if grabbed >= maxGrabs || (prio == queue.PriorityAutomatic && !s.autoGrabRoom()) {
					continue // left for a later run; it isn't a doubt
				}
				if _, err := s.grabManga(sr, vol.ID, r.Title, r.DownloadURL, r.SizeBytes, r.Protocol, string(v.Class), prio, false); err == nil {
					grabbed++
				} else {
					slog.Info("manga: grab", "series", sr.Title, "volume", vol.Number, "err", err)
				}
				continue
			}
			if perVolume[v.Number] == 0 {
				_ = repo.ClearReviews(sr.ID, vol.ID)
			}
			if perVolume[v.Number] < 5 {
				perVolume[v.Number]++
				doubtful++
				s.addMangaReview(sr.ID, vol.ID, "single", r)
			}
		}
	}
	return grabbed, doubtful
}

func (s *Server) addMangaReview(seriesID, volumeID int64, kind string, r mangaRelease) {
	if err := s.mangaRepo().AddReview(manga.Review{SeriesID: seriesID, VolumeID: volumeID, Kind: kind, Title: r.Title, URL: r.DownloadURL, SizeBytes: r.SizeBytes,
		Protocol: string(r.Protocol), Indexer: r.IndexerName, Score: r.Verdict.Score, Class: string(r.Verdict.Class), Notes: r.Verdict.Notes}); err != nil {
		slog.Info("manga: review list", "series", seriesID, "err", err)
	}
}

// searchMangaVolume looks for one volume: its search words, most likely first,
// stopping at the first that finds it. It reports whether it grabbed one.
func (s *Server) searchMangaVolume(ctx context.Context, sr manga.Series, v manga.Volume, prio queue.Priority, manual bool) (bool, manga.SearchSummary) {
	repo := s.mangaRepo()
	var sum manga.SearchSummary
	sum.Number = v.Number
	if v.Status == manga.StatusDownloaded {
		return false, sum
	}
	if busy, _ := repo.ActiveForVolume(v.ID); busy {
		return false, sum
	}
	target := s.mangaTarget(sr)
	if !target.Sources.Any() {
		return false, sum
	}
	if manual {
		ctx = indexers.AsManual(ctx)
	}
	releases := s.askIndexers(ctx, target, v.Number, manga.VolumeQueries(target.Names, v.Number, manual), &sum, func(found []mangaRelease) bool {
		for _, r := range found {
			if r.usable() {
				return true
			}
		}
		return false
	})
	if ctx.Err() != nil {
		return false, sum
	}
	_ = repo.ClearReviews(sr.ID, v.ID)
	doubtful := 0
	for _, r := range releases {
		if !r.usable() {
			continue
		}
		if !r.Verdict.Confident() {
			if doubtful < 5 {
				kind := "single"
				if r.Verdict.Kind != manga.KindVolume && r.Verdict.Kind != manga.KindBare {
					kind = "pack"
				}
				doubtful++
				s.addMangaReview(sr.ID, v.ID, kind, r)
			}
			continue
		}
		if _, err := s.grabManga(sr, v.ID, r.Title, r.DownloadURL, r.SizeBytes, r.Protocol, string(r.Verdict.Class), prio, false); err != nil {
			slog.Info("manga: grab", "series", sr.Title, "volume", v.Number, "err", err)
			break
		}
		sum.Taken = 1
		_ = repo.ClearReviews(sr.ID, v.ID)
		_ = repo.LogSearch(sr.ID, v.ID, "grabbed", sum, time.Now())
		return true, sum
	}
	sum.Doubtful = doubtful
	_ = repo.LogSearch(sr.ID, v.ID, mangaOutcome(sum, 0, doubtful), sum, time.Now())
	return false, sum
}

// grabManga puts a release in the download line for a volume, or for the whole
// series with volumeID 0.
func (s *Server) grabManga(sr manga.Series, volumeID int64, title, url string, size int64, protocol indexers.Protocol, class string, prio queue.Priority, replace bool) (int64, error) {
	if protocol == indexers.ProtocolTorrent && !s.torrentsEnabled() {
		return 0, errTorrentsDisabled
	}
	if s.mangaRoot() == "" {
		return 0, errors.New("Choose the manga folder in Settings > Library > Folders first.")
	}
	if problem := s.mangaFolderProblem(); problem != "" {
		return 0, errors.New(problem)
	}
	repo := s.mangaRepo()
	s.grabMu.Lock()
	var err error
	busy := false
	if volumeID > 0 {
		busy, err = repo.ActiveForVolume(volumeID)
	}
	if err == nil && !busy {
		busy, err = repo.ActiveRelease(sr.ID, title)
	}
	if err == nil && busy {
		err = errors.New("A download for this is already running or waiting.")
	}
	var id int64
	if err == nil {
		id, err = s.QueueRepo.Enqueue(queue.Item{ReleaseTitle: title, NZBURL: url, SizeBytes: size, Protocol: queue.Protocol(protocol), Priority: prio})
	}
	if err == nil {
		if err = repo.LinkDownload(manga.Download{QueueID: id, SeriesID: sr.ID, VolumeID: volumeID, Class: class, Replace: replace}); err != nil {
			_ = s.QueueRepo.SetStatus(id, queue.StatusFailed, "couldn't link the download to its volume")
		}
	}
	s.grabMu.Unlock()
	if err != nil {
		return 0, err
	}
	_ = s.QueueRepo.LogActivity(id, "grabbed", fmt.Sprintf("Downloading %s: %s", sr.Title, title))
	s.dispatch.Kick()
	return id, nil
}

// ---- the automatic search ----

// huntManga is Manga's part of the automatic search: a daily refresh from
// AniList, then at most ten searches a run. A series is looked for by name when
// it's due; single volumes only if the name turned up anything of the series.
// Nothing is asked of an indexer that's paused or out of searches for the day.
func (s *Server) huntManga(ctx context.Context) {
	if !s.mangaEnabled() || s.mangaRoot() == "" {
		return
	}
	st := s.mangaMod()
	if !st.hunt.TryLock() {
		return
	}
	defer st.hunt.Unlock()
	repo := s.mangaRepo()
	list, err := repo.ListSeries()
	if err != nil {
		return
	}
	// A series' details are refreshed at night, by mangaRefreshJob (home_manga_find.go).
	usual, nyaa, err := s.mangaInstances(s.currentMangaSources().Nyaa)
	if err != nil || !anyIndexerAvailable(append(usual, nyaa...)) {
		return
	}
	list, err = repo.ListSeries()
	if err != nil {
		return
	}
	budget := maxMangaSearchesPerHunt
	now := time.Now()
	type due struct {
		sr  manga.Series
		log []manga.SearchEntry
		at  time.Time
	}
	var todo []due
	for _, sr := range list {
		follows := sr.Wanted > 0 || (sr.MonitorNew && sr.Status != "finished" && sr.Status != "cancelled")
		if !follows || !s.mangaSourcesFor(sr).Any() {
			continue
		}
		log, _ := repo.SearchLog(sr.ID)
		todo = append(todo, due{sr, log, manga.DueAt(log, 0)})
	}
	sort.SliceStable(todo, func(i, j int) bool { return todo[i].at.Before(todo[j].at) }) // the longest waiting first
	for _, d := range todo {
		if budget <= 0 || ctx.Err() != nil {
			return
		}
		if !s.autoGrabRoom() {
			holdBack("hunt")
			return
		}
		named := false
		if !d.at.After(now) {
			budget--
			lctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			_, named = s.lookForManga(lctx, d.sr.ID, queue.PriorityAutomatic, maxMangaGrabsAtOnce)
			cancel()
			if !sleepOrDone(ctx, s.mangaGap()) {
				return
			}
			d.log, _ = repo.SearchLog(d.sr.ID)
		} else {
			for _, e := range d.log {
				if e.VolumeID == 0 {
					named = e.Summary.Named()
				}
			}
		}
		if !named {
			continue // nothing of it on the indexers: no volume is worth a search
		}
		volumes, err := repo.Volumes(d.sr.ID)
		if err != nil {
			continue
		}
		for _, v := range volumes {
			if budget <= 0 || ctx.Err() != nil {
				return
			}
			if !v.Wanted || v.State != manga.StatusMissing || manga.DueAt(d.log, v.ID).After(now) {
				continue
			}
			if !s.autoGrabRoom() {
				holdBack("hunt")
				return
			}
			budget--
			vctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			s.searchMangaVolume(vctx, d.sr, v, queue.PriorityAutomatic, false)
			cancel()
			if !sleepOrDone(ctx, s.mangaGap()) {
				return
			}
		}
	}
}

func sleepOrDone(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// ---- searching by hand ----

// POST /api/manga/{id}/search: look now, by name and then the wanted volumes.
func (s *Server) handleSearchManga(w http.ResponseWriter, r *http.Request) {
	sr, ok := s.mangaFromPath(w, r)
	if !ok {
		return
	}
	if !s.mangaSourcesFor(sr).Any() {
		writeError(w, http.StatusConflict, "No source is switched on for this series, so there's nowhere to look.")
		return
	}
	_ = s.mangaRepo().ResetBackoff(sr.ID)
	s.background(func() {
		ctx, cancel := context.WithTimeout(indexers.AsManual(context.Background()), 5*time.Minute)
		defer cancel()
		grabbed, named := s.lookForManga(ctx, sr.ID, queue.PriorityManual, maxMangaGrabsAtOnce)
		if !named {
			return
		}
		volumes, err := s.mangaRepo().Volumes(sr.ID)
		if err != nil {
			return
		}
		tried := 0
		for _, v := range volumes {
			if tried >= maxMangaSearchesPerHunt || grabbed >= maxMangaGrabsAtOnce {
				return
			}
			if !v.Wanted || v.State != manga.StatusMissing {
				continue
			}
			tried++
			if !sleepOrDone(ctx, s.mangaGap()) {
				return
			}
			if ok, _ := s.searchMangaVolume(ctx, sr, v, queue.PriorityManual, true); ok {
				grabbed++
			}
		}
	})
	writeJSON(w, http.StatusAccepted, map[string]string{"message": "Looking for " + sr.Title + " now."})
}

// POST /api/manga/volumes/{id}/search
func (s *Server) handleSearchMangaVolume(w http.ResponseWriter, r *http.Request) {
	v, sr, ok := s.mangaVolumeFromPath(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	grabbed, sum := s.searchMangaVolume(ctx, sr, v, queue.PriorityManual, true)
	writeJSON(w, http.StatusOK, map[string]any{"grabbed": grabbed, "summary": sum})
}

// GET /api/manga/volumes/{id}/releases: what the indexers have, with why each
// would or wouldn't be taken. ?q= uses a person's own words.
func (s *Server) handleMangaVolumeReleases(w http.ResponseWriter, r *http.Request) {
	v, sr, ok := s.mangaVolumeFromPath(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(indexers.AsManual(r.Context()), 2*time.Minute)
	defer cancel()
	target := s.mangaTarget(sr)
	queries := manga.VolumeQueries(target.Names, v.Number, true)
	if own := strings.TrimSpace(r.URL.Query().Get("q")); own != "" {
		queries = []string{own}
	}
	var sum manga.SearchSummary
	releases := s.askIndexers(ctx, target, v.Number, queries, &sum, func(found []mangaRelease) bool { return len(found) > 0 })
	type payload struct {
		searchResultPayload
		Verdict manga.Verdict `json:"verdict"`
	}
	out := make([]payload, 0, len(releases))
	for _, rel := range releases {
		p := payload{searchResultPayload: s.toSearchResultPayload(rel.Result), Verdict: rel.Verdict}
		p.Blocklisted = rel.Blocked
		out = append(out, p)
	}
	writeJSON(w, http.StatusOK, map[string]any{"releases": out, "summary": sum})
}

// POST /api/manga/volumes/{id}/grab {"title","downloadUrl","sizeBytes","protocol"}
func (s *Server) handleGrabMangaVolume(w http.ResponseWriter, r *http.Request) {
	v, sr, ok := s.mangaVolumeFromPath(w, r)
	if !ok {
		return
	}
	var req struct {
		Title       string `json:"title"`
		DownloadURL string `json:"downloadUrl"`
		SizeBytes   int64  `json:"sizeBytes"`
		Protocol    string `json:"protocol"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Title == "" || req.DownloadURL == "" {
		writeError(w, http.StatusBadRequest, "Choose a release to download.")
		return
	}
	if !s.checkGrabURL(w, r, &req.DownloadURL) {
		return
	}
	protocol := indexers.Protocol(req.Protocol)
	if protocol == "" {
		protocol = indexers.ProtocolUsenet
	}
	class := manga.Classify(req.Title, manga.ParseFor(req.Title, s.mangaRepo().Names(sr)).Kind, sr.LicensedEN)
	id, err := s.grabManga(sr, v.ID, req.Title, req.DownloadURL, req.SizeBytes, protocol, string(class), queue.PriorityManual, false)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"queueId": id})
}

// ---- the review list ----

// GET /api/manga/reviews: everything waiting for a person.
func (s *Server) handleMangaReviews(w http.ResponseWriter, r *http.Request) {
	list, err := s.mangaRepo().Reviews(0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, plainerror.Message(err))
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) mangaReviewFromPath(w http.ResponseWriter, r *http.Request) (manga.Review, bool) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	v, err := s.mangaRepo().GetReview(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "That's no longer on the list.")
		return v, false
	}
	return v, true
}

// POST /api/manga/reviews/{id}/accept: download it. For another copy of a volume
// you have, it replaces your file, which moves to .replaced.
func (s *Server) handleAcceptMangaReview(w http.ResponseWriter, r *http.Request) {
	v, ok := s.mangaReviewFromPath(w, r)
	if !ok {
		return
	}
	repo := s.mangaRepo()
	sr, err := repo.GetSeries(v.SeriesID)
	if err != nil {
		writeError(w, http.StatusNotFound, "That series is gone.")
		return
	}
	qid, err := s.grabManga(sr, v.VolumeID, v.Title, v.URL, v.SizeBytes, indexers.Protocol(v.Protocol), v.Class, queue.PriorityManual, v.Kind == "copy")
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if v.VolumeID > 0 {
		_ = repo.ClearReviews(sr.ID, v.VolumeID)
	}
	_ = repo.DeleteReview(v.ID)
	writeJSON(w, http.StatusOK, map[string]any{"queueId": qid, "message": "Downloading it now."})
}

// POST /api/manga/reviews/{id}/reject: not this. It's blocklisted, so no search
// offers it again. Saying no to another copy of a volume only keeps your file.
func (s *Server) handleRejectMangaReview(w http.ResponseWriter, r *http.Request) {
	v, ok := s.mangaReviewFromPath(w, r)
	if !ok {
		return
	}
	if v.Kind != "copy" {
		if err := s.Blocklist.Add(blocklist.Entry{ReleaseTitle: v.Title, Protocol: v.Protocol, Reason: "Turned down on the manga list"}); err != nil {
			writeError(w, http.StatusInternalServerError, plainerror.Message(err))
			return
		}
	}
	_ = s.mangaRepo().DeleteReview(v.ID)
	writeJSON(w, http.StatusOK, map[string]string{"message": "Done. It won't be offered again."})
}
