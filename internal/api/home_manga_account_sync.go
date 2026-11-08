package api

// Usherr: syncing each connected MangaDex account. Three kinds of sync:
//
//   - Read marks made here go up within a couple of minutes: a quick local check
//     every two minutes, and one call when something changed, after it's been
//     still for a little while (so reading through a volume is one call, not ten).
//   - A light sync every half hour: the follow feed, reading history, reading
//     statuses, follows when there are few, and the read marks of series being
//     read. A few calls.
//   - The whole sync once a night between 2 and 9 (the house rule for heavy work):
//     every follow, every linked series' read marks, adding followed series to the
//     library. "Sync now" does a day-sized version of it.
//
// Everything waits while MangaDex is away, and a sign-in MangaDex turns down stops
// that person's syncing until they save it again.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/manga"
)

const (
	mdJobEvery     = 2 * time.Minute
	mdLightEvery   = 30 * time.Minute
	mdFullEvery    = 20 * time.Hour
	mdListsEvery   = 6 * time.Hour
	mdStillFor     = 45 * time.Second // read marks unchanged this long go up
	mdActiveFor    = 14 * 24 * time.Hour
	mdAggFresh     = 24 * time.Hour // a series' chapter list, asked again for new ids after this
	mdFeedSize     = 100
	mdStretchCheck = 24 * time.Hour // a series the feed watches is looked at by itself this seldom
)

var mdNoStill bool // tests set this

// mdSyncKind is how much a sync does.
type mdSyncKind struct {
	name        string
	followPages int  // pages of 100 follows read; the merge only runs when all are read
	readsAll    bool // every linked series' read marks, not just those being read
	aggregates  int  // series chapter lists that may be asked for
	adds        int  // followed series that may be added to the library
	lists       bool // read the lists whatever their age
}

var (
	mdSyncLight  = mdSyncKind{name: "light", followPages: 1, aggregates: 5, adds: 3}
	mdSyncManual = mdSyncKind{name: "manual", followPages: 5, readsAll: true, aggregates: 10, adds: 10, lists: true}
	mdSyncFull   = mdSyncKind{name: "full", followPages: 100, readsAll: true, aggregates: 100, adds: 50, lists: true}
)

// mangaDexAccountJob runs every two minutes: read marks up, and the light or the
// whole sync when due.
func (s *Server) mangaDexAccountJob(ctx context.Context) {
	if !s.mangaEnabled() || s.mangaMod().dex.Paused() != nil {
		return
	}
	accounts, err := s.mangaRepo().DexAccounts()
	if err != nil {
		return
	}
	now := time.Now()
	for _, a := range accounts {
		if ctx.Err() != nil || s.mangaMod().dex.Paused() != nil {
			return
		}
		lock := s.mdAcc().lock(a.UserID)
		if !lock.TryLock() {
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		var kind *mdSyncKind
		switch {
		case mangaDexWindow(now) && olderThan(a.FullAt, mdFullEvery, now):
			kind = &mdSyncFull
		case olderThan(a.LightAt, mdLightEvery, now):
			kind = &mdSyncLight
		}
		if kind != nil {
			s.mdSync(cctx, a.UserID, *kind)
		} else {
			s.mdPushReads(cctx, a.UserID, now)
		}
		cancel()
		lock.Unlock()
	}
}

func olderThan(at string, d time.Duration, now time.Time) bool {
	t, err := time.Parse(time.RFC3339, at)
	return err != nil || now.Sub(t) >= d
}

// mdSyncSoon runs a sync for one person now, waiting for their lock.
func (s *Server) mdSyncSoon(user int64, kind mdSyncKind) {
	if s.mangaMod().dex.Paused() != nil {
		return
	}
	lock := s.mdAcc().lock(user)
	lock.Lock()
	defer lock.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	s.mdSync(ctx, user, kind)
}

// mdSync is one sync of a person's account. Call with their lock held.
func (s *Server) mdSync(ctx context.Context, user int64, kind mdSyncKind) {
	repo := s.mangaRepo()
	a, err := repo.DexAccountOf(user)
	if err != nil || a.State == manga.DexAccountRefused {
		return
	}
	now := time.Now()
	stats := a.Stats
	stats.ReadPushed, stats.ReadPulled, stats.Added, stats.Note, stats.Unfinished = 0, 0, 0, "", false
	budget := &mdBudget{aggregates: kind.aggregates, adds: kind.adds}
	steps := []func() error{
		func() error { return s.mdSyncFollows(ctx, user, kind, budget, &stats) },
		func() error { return s.mdSyncStatuses(ctx, user) },
		func() error { return s.mdSyncFeed(ctx, user) },
		func() error { return s.mdSyncHistory(ctx, user) },
		func() error { return s.mdSyncReads(ctx, user, kind, budget, &stats, now) },
	}
	if kind.lists || olderThan(a.ListsAt, mdListsEvery, now) {
		steps = append(steps, func() error {
			if err := s.mdSyncLists(ctx, user); err != nil {
				return err
			}
			return repo.NoteDexSync(user, "lists", now, nil)
		})
	}
	for _, step := range steps {
		if err := step(); err != nil {
			if ctx.Err() == nil && !errors.Is(err, manga.ErrDexSignIn) {
				slog.Info("manga: MangaDex account sync stopped", "user", user, "err", err)
			}
			stats.Note = "stopped"
			_ = repo.NoteDexSync(user, "light", now, &stats)
			return
		}
	}
	_ = repo.NoteDexSync(user, "light", now, &stats)
	if kind.name != "light" {
		_ = repo.NoteDexSync(user, "full", now, &stats)
	}
}

type mdBudget struct{ aggregates, adds int }

// ---- follows ----

// mdSyncFollows merges the follows: new follows on MangaDex come into the library
// (or are linked to a series there), new follows here go up, and an unfollow on
// either side is made on the other. Only with the whole list in hand.
func (s *Server) mdSyncFollows(ctx context.Context, user int64, kind mdSyncKind, budget *mdBudget, stats *manga.DexSyncStats) error {
	repo := s.mangaRepo()
	var remote []manga.DexFollowTitle
	total := 0
	err := s.mdRun(user, func(sess *manga.DexSession) error {
		var err error
		remote, total, err = sess.Follows(ctx, kind.followPages*100)
		return err
	})
	if err != nil {
		return err
	}
	stats.Follows = total
	if len(remote) < total {
		stats.Unfinished = true // the rest waits for the whole sync tonight
		return nil
	}
	_ = repo.SaveDexTitles(remote, time.Now())
	there := map[string]bool{}
	byID := map[string]manga.DexFollowTitle{}
	for _, t := range remote {
		id := strings.ToLower(t.ID)
		there[id], byID[id] = true, t
	}
	links, err := repo.DexLinks()
	if err != nil {
		return err
	}
	followed, err := repo.FollowedSeries(user)
	if err != nil {
		return err
	}
	here := map[string]bool{}
	seriesOf := map[string]int64{}
	for _, id := range followed {
		if dex := links[id]; dex != "" {
			here[dex], seriesOf[dex] = true, id
		}
	}
	synced, err := repo.FollowSynced(user)
	if err != nil {
		return err
	}
	scope := map[string]bool{}
	for _, set := range []map[string]bool{here, there, synced} {
		for k := range set {
			scope[k] = true
		}
	}
	merged := manga.MergeSets(here, there, synced, scope)
	keepOutside := map[string]string{}
	canAdd, adult := s.mdAllows(user, auth.PermManga), s.mdAdultFor(user)
	waiting := 0
	for _, dex := range manga.SortedKeys(merged) {
		switch {
		case !there[dex]: // followed here: follow there
			if err := s.mdRun(user, func(sess *manga.DexSession) error { return sess.SetFollow(ctx, dex, true) }); err != nil {
				return err
			}
		case !here[dex]: // followed there: here too
			id, why, err := s.mdBringIn(ctx, byID[dex], canAdd, adult, budget)
			if err != nil {
				return err
			}
			if id > 0 {
				_ = repo.SetFollowing(user, id, true)
				stats.Added++
				continue
			}
			if why == mdWhyLater {
				waiting++
			}
			keepOutside[dex] = why
		}
	}
	for dex := range scope {
		if merged[dex] {
			continue
		}
		if there[dex] { // unfollowed here
			if err := s.mdRun(user, func(sess *manga.DexSession) error { return sess.SetFollow(ctx, dex, false) }); err != nil {
				return err
			}
		}
		if here[dex] { // unfollowed there
			_ = repo.SetFollowing(user, seriesOf[dex], false)
		}
	}
	stats.Outside, stats.Waiting = len(keepOutside), waiting
	if err := repo.SetDexOutside(user, keepOutside); err != nil {
		return err
	}
	return repo.SetFollowSynced(user, merged)
}

// Why a followed series isn't in the library.
const (
	mdWhyAdult  = "adult"  // MangaDex rates it 18+, and this person may not see that here
	mdWhyCant   = "can't"  // this person may not add series
	mdWhyLater  = "later"  // waits for the next sync
	mdWhyNovel  = "novel"  // AniList says it's a novel
	mdWhyFailed = "failed" // AniList or the library couldn't take it
)

// mdBringIn finds a followed series in the library, linking it to MangaDex, or adds
// it. Added series want no volumes; they follow new chapters only when MangaDex
// chapters are switched on, and otherwise nothing downloads (the page says why).
func (s *Server) mdBringIn(ctx context.Context, t manga.DexFollowTitle, canAdd, adult bool, budget *mdBudget) (int64, string, error) {
	repo := s.mangaRepo()
	dex := strings.ToLower(t.ID)
	if sr, err := repo.SeriesByDex(dex); err == nil {
		return sr.ID, "", nil
	}
	if t.AniListID > 0 {
		if sr, err := repo.SeriesByAniList(t.AniListID); err == nil {
			if err := repo.LinkToDex(sr.ID, dex); err != nil {
				return 0, "", err
			}
			return sr.ID, "", nil
		}
	}
	switch {
	case manga.DexAdult(t.Rating) && !adult:
		return 0, mdWhyAdult, nil
	case !canAdd:
		return 0, mdWhyCant, nil
	case budget.adds <= 0:
		return 0, mdWhyLater, nil
	}
	budget.adds--
	info := manga.Series{Title: t.Title, Status: t.Status, StartYear: t.Year, LicensedEN: -1}
	var aliases []manga.Alias
	for _, alt := range t.AltTitles {
		aliases = append(aliases, manga.Alias{Alias: alt, Kind: "synonym"})
	}
	if t.AniListID > 0 {
		actx, cancel := context.WithTimeout(ctx, time.Minute)
		f, err := s.mangaMod().anilist.Get(actx, t.AniListID)
		cancel()
		switch {
		case err == nil && f.IsNovel:
			return 0, mdWhyNovel, nil
		case err == nil:
			info, aliases = f.AsSeries(), f.Aliases
			info.CheckedAt = time.Now().UTC().Format(time.RFC3339)
		case ctx.Err() != nil:
			return 0, "", ctx.Err()
		default:
			// AniList is away: MangaDex's own details do for now; the nightly refresh fills in the rest.
		}
	}
	if strings.TrimSpace(info.Title) == "" {
		return 0, mdWhyFailed, nil
	}
	info.IsAdult = info.IsAdult || manga.DexAdult(t.Rating)
	if info.IsAdult && !adult {
		return 0, mdWhyAdult, nil
	}
	info.MonitorNew, info.WantAll = false, false // nothing is searched for by itself: the person reads chapters
	sr, err := repo.AddSeries(info, aliases)
	if err != nil {
		if t.AniListID > 0 { // added a moment ago by someone else
			if have, e := repo.SeriesByAniList(t.AniListID); e == nil {
				_ = repo.LinkToDex(have.ID, dex)
				return have.ID, "", nil
			}
		}
		return 0, mdWhyFailed, nil
	}
	for n := 1; n <= sr.VolumeCount; n++ {
		_, _, _ = repo.EnsureVolume(sr.ID, strconv.Itoa(n), false)
	}
	if err := repo.LinkToDex(sr.ID, dex); err != nil {
		return 0, "", err
	}
	if s.mangaSourcesFor(sr).MangaDex {
		s.followMangaChapters(sr, manga.FollowNew)
	}
	slog.Info("manga: added from a MangaDex follow", "series", sr.Title)
	return sr.ID, "", nil
}

// mdLinkThenSync matches a series followed here to MangaDex, then follows it there.
func (s *Server) mdLinkThenSync(user int64, sr manga.Series) {
	st := s.mangaMod()
	if st.dex.Paused() != nil {
		return
	}
	d := s.mangaRepo().DexStateOf(sr.ID)
	d.SeriesID = sr.ID
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	st.dexJob.Lock()
	err := s.linkMangaDex(ctx, sr, &d)
	st.dexJob.Unlock()
	if err != nil {
		s.noteDexTrouble(err)
		return
	}
	if d.Link != manga.DexLinked {
		if d.Link != "" {
			_ = s.mangaRepo().SaveDexState(d) // unsure or none: the page offers the choice, as for chapters
		}
		return
	}
	if err := s.mangaRepo().SaveDexState(d); err != nil {
		return
	}
	lock := s.mdAcc().lock(user)
	lock.Lock()
	defer lock.Unlock()
	if s.mangaRepo().Following(user, sr.ID) {
		if err := s.mdRun(user, func(sess *manga.DexSession) error { return sess.SetFollow(ctx, d.DexID, true) }); err == nil {
			_ = s.mangaRepo().SetFollowSyncedOne(user, strings.ToLower(d.DexID), true)
		}
	}
}

// ---- reading status ----

func (s *Server) mdSyncStatuses(ctx context.Context, user int64) error {
	repo := s.mangaRepo()
	var there map[string]string
	if err := s.mdRun(user, func(sess *manga.DexSession) error {
		var err error
		there, err = sess.ReadingStatuses(ctx)
		return err
	}); err != nil {
		return err
	}
	links, err := repo.DexLinks()
	if err != nil {
		return err
	}
	for id, dex := range links {
		here, synced := repo.ReadingStatus(user, id)
		merged := manga.MergeStatus(here, there[dex], synced)
		if merged != there[dex] {
			if err := s.mdRun(user, func(sess *manga.DexSession) error { return sess.SetReadingStatus(ctx, dex, merged) }); err != nil {
				return err
			}
		}
		if merged != here || merged != synced {
			_ = repo.SetReadingStatusSynced(user, id, merged)
		}
	}
	return nil
}

// ---- lists, feed, history ----

func (s *Server) mdSyncLists(ctx context.Context, user int64) error {
	var lists []manga.DexAccountList
	if err := s.mdRun(user, func(sess *manga.DexSession) error {
		var err error
		lists, err = sess.Lists(ctx)
		return err
	}); err != nil {
		return err
	}
	if err := s.mangaRepo().SaveDexLists(user, lists); err != nil {
		return err
	}
	var ids []string
	for _, l := range lists {
		ids = append(ids, l.Manga...)
	}
	return s.mdLearnTitles(ctx, ids)
}

// mdLearnTitles keeps the titles, ratings and covers of series not seen before.
func (s *Server) mdLearnTitles(ctx context.Context, ids []string) error {
	repo := s.mangaRepo()
	seen := map[string]bool{}
	var unknown []string
	have, err := repo.DexTitlesOf(ids)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if _, ok := have[id]; !ok && !seen[id] && manga.IsDexID(id) {
			seen[id] = true
			unknown = append(unknown, id)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	found, err := s.mangaMod().dex.FollowTitlesByID(ctx, unknown)
	if err != nil {
		s.noteDexTrouble(err)
		return err
	}
	return repo.SaveDexTitles(found, time.Now())
}

// mdSyncFeed reads the follow feed: one call for every followed series' new
// chapters, rather than asking each one. A series in the library whose chapters
// are followed is looked at soon when the feed has something new for it.
func (s *Server) mdSyncFeed(ctx context.Context, user int64) error {
	ratings := []string{"safe", "suggestive"}
	if s.mdAdultFor(user) {
		ratings = append(ratings, "erotica", "pornographic")
	}
	var feed []manga.DexFeedChapter
	if err := s.mdRun(user, func(sess *manga.DexSession) error {
		var err error
		feed, err = sess.FollowFeed(ctx, s.mangaDexLanguage(), ratings, mdFeedSize)
		return err
	}); err != nil {
		return err
	}
	repo := s.mangaRepo()
	if err := repo.SaveDexFeed(user, feed); err != nil {
		return err
	}
	var titles []manga.DexFollowTitle
	var ids []string
	for _, c := range feed {
		ids = append(ids, c.DexID)
		if c.Series != "" {
			titles = append(titles, manga.DexFollowTitle{DexSeries: manga.DexSeries{ID: c.DexID, Title: c.Series, Rating: c.Rating}})
		}
	}
	have, _ := repo.DexTitlesOf(ids)
	var fresh []manga.DexFollowTitle
	for _, t := range titles {
		if _, ok := have[t.ID]; !ok {
			fresh = append(fresh, t)
		}
	}
	_ = repo.SaveDexTitles(fresh, time.Now()) // covers come from the next lookup
	var noCover []string
	for _, t := range fresh {
		noCover = append(noCover, t.ID)
	}
	if len(noCover) > 0 {
		if found, err := s.mangaMod().dex.FollowTitlesByID(ctx, noCover); err == nil {
			_ = repo.SaveDexTitles(found, time.Now())
		}
	}
	s.mdBumpFromFeed(feed)
	return nil
}

// mdBumpFromFeed moves a series' next chapter look to now when the feed shows a
// chapter newer than its last look.
func (s *Server) mdBumpFromFeed(feed []manga.DexFeedChapter) {
	repo := s.mangaRepo()
	done := map[string]bool{}
	for _, c := range feed {
		if done[c.DexID] {
			continue
		}
		done[c.DexID] = true
		sr, err := repo.SeriesByDex(c.DexID)
		if err != nil {
			continue
		}
		d := repo.DexStateOf(sr.ID)
		if d.Follow == manga.FollowOff {
			continue
		}
		at, err1 := time.Parse(time.RFC3339, c.ReadableAt)
		checked, err2 := time.Parse(time.RFC3339, d.CheckedAt)
		if err1 == nil && (err2 != nil || at.After(checked)) && d.NextAt != "" {
			d.NextAt = ""
			_ = repo.SaveDexState(d)
		}
	}
}

// mangaDexCheckEvery is how long until a followed series' chapters are looked at
// again: much longer when a connected account follows it on MangaDex, as the
// follow feed then says when something new is there.
func (s *Server) mdCheckEvery(dexID string, every time.Duration) time.Duration {
	if dexID != "" && s.mangaRepo().FollowedOnDex(dexID) {
		return max(every, mdStretchCheck)
	}
	return every
}

func (s *Server) mdSyncHistory(ctx context.Context, user int64) error {
	var list []manga.DexRead
	if err := s.mdRun(user, func(sess *manga.DexSession) error {
		var err error
		list, err = sess.History(ctx)
		return err
	}); err != nil {
		if errors.Is(err, manga.ErrNotFound) { // no history yet
			return nil
		}
		return err
	}
	repo := s.mangaRepo()
	if err := repo.SaveDexHistory(user, list); err != nil {
		return err
	}
	var ids []string
	for _, h := range list {
		ids = append(ids, h.ChapterID)
	}
	known := repo.KnownChapterIDs(ids)
	var unknown []string
	for _, id := range ids {
		if !known[id] {
			unknown = append(unknown, id)
		}
	}
	if len(unknown) > 0 {
		refs, err := s.mangaMod().dex.ChapterRefs(ctx, unknown)
		if err != nil {
			s.noteDexTrouble(err)
			return err
		}
		if err := repo.SaveChapterRefs(refs); err != nil {
			return err
		}
	}
	items, _ := repo.DexHistory(user)
	var series []string
	for _, h := range items {
		if h.DexID != "" {
			series = append(series, h.DexID)
		}
	}
	return s.mdLearnTitles(ctx, series)
}

// ---- read marks ----

// mdPrint is a series' read marks here, in short, to notice a change.
type mdPrint struct {
	key     string
	since   time.Time
	pending bool
}

// mdReadPrints are the read marks of every linked series here, in short.
func (s *Server) mdReadPrints(user int64) (map[int64]string, error) {
	out := map[int64]string{}
	add := func(q string) error {
		rows, err := s.db.Query(q, user)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id, n, sum int64
			if err := rows.Scan(&id, &n, &sum); err != nil {
				return err
			}
			out[id] += fmt.Sprintf("%d:%d;", n, sum)
		}
		return rows.Err()
	}
	if err := add(`SELECT c.series_id, COUNT(*), SUM(m.read_ms % 1000003) FROM manga_chapter_read m JOIN manga_chapters c ON c.id = m.chapter_id WHERE m.user_id = ? GROUP BY c.series_id`); err != nil {
		return nil, err
	}
	if err := add(`SELECT v.series_id, COUNT(*), SUM(m.read_ms % 1000003) FROM manga_read m JOIN manga_volumes v ON v.id = m.volume_id WHERE m.user_id = ? GROUP BY v.series_id`); err != nil {
		return nil, err
	}
	return out, nil
}

// mdPushReads sends read marks made here, once they've been still for a little
// while. The local check costs nothing at MangaDex; a call is made only for series
// that changed.
func (s *Server) mdPushReads(ctx context.Context, user int64, now time.Time) {
	prints, err := s.mdReadPrints(user)
	if err != nil {
		return
	}
	links, err := s.mangaRepo().DexLinks()
	if err != nil {
		return
	}
	acc := s.mdAcc()
	acc.mu.Lock()
	seen := acc.prints[user]
	first := seen == nil
	if first {
		seen = map[int64]mdPrint{}
		acc.prints[user] = seen
	}
	var due []int64
	for id := range links {
		key := prints[id]
		p, ok := seen[id]
		switch {
		case !ok:
			seen[id] = mdPrint{key: key, since: now, pending: !first}
		case p.key != key:
			seen[id] = mdPrint{key: key, since: now, pending: true}
		case p.pending && (mdNoStill || now.Sub(p.since) >= mdStillFor):
			due = append(due, id)
		}
	}
	acc.mu.Unlock()
	if len(due) == 0 {
		return
	}
	stats := manga.DexSyncStats{}
	_ = s.mdSyncSeriesReads(ctx, user, due, links, &mdBudget{aggregates: 3}, &stats, now)
}

// mdSettlePrints notes these series' read marks as they are now, after a sync: a
// change the sync itself made here isn't a reason to send anything.
func (s *Server) mdSettlePrints(user int64, ids []int64, now time.Time) {
	prints, err := s.mdReadPrints(user)
	if err != nil {
		return
	}
	acc := s.mdAcc()
	acc.mu.Lock()
	defer acc.mu.Unlock()
	seen := acc.prints[user]
	if seen == nil {
		return // the first look takes them as they are anyway
	}
	for _, id := range ids {
		seen[id] = mdPrint{key: prints[id], since: now}
	}
}

// mdSyncReads merges the read marks of the series being read (or, in a whole
// sync, of every linked series).
func (s *Server) mdSyncReads(ctx context.Context, user int64, kind mdSyncKind, budget *mdBudget, stats *manga.DexSyncStats, now time.Time) error {
	repo := s.mangaRepo()
	links, err := repo.DexLinks()
	if err != nil {
		return err
	}
	var ids []int64
	if kind.readsAll {
		for id := range links {
			ids = append(ids, id)
		}
	} else {
		active := map[int64]bool{}
		followed, _ := repo.FollowedSeries(user)
		for _, id := range followed {
			active[id] = true
		}
		since := now.Add(-mdActiveFor).UnixMilli()
		recent, _ := repo.RecentlyReadSeries(user, since)
		for _, id := range recent {
			active[id] = true
		}
		for id := range active {
			if links[id] != "" {
				ids = append(ids, id)
			}
		}
		if len(ids) > 100 {
			ids = ids[:100] // one call; the rest at night
		}
	}
	return s.mdSyncSeriesReads(ctx, user, ids, links, budget, stats, now)
}

// mdSyncOpened brings in one series' read marks and status when its page opens.
func (s *Server) mdSyncOpened(ctx context.Context, user int64, sr manga.Series, dex string) (bool, error) {
	stats := manga.DexSyncStats{}
	err := s.mdSyncSeriesReads(ctx, user, []int64{sr.ID}, map[int64]string{sr.ID: dex}, &mdBudget{aggregates: 1}, &stats, time.Now())
	return stats.ReadPulled > 0, err
}

// mdSyncSeriesReads merges the read marks of these series: one call for MangaDex's
// marks, a chapter list where ids are new, one call a series with changes to send.
func (s *Server) mdSyncSeriesReads(ctx context.Context, user int64, ids []int64, links map[int64]string, budget *mdBudget, stats *manga.DexSyncStats, now time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	repo := s.mangaRepo()
	var dexIDs []string
	for _, id := range ids {
		dexIDs = append(dexIDs, links[id])
	}
	var marks map[string][]string
	if err := s.mdRun(user, func(sess *manga.DexSession) error {
		var err error
		marks, err = sess.ReadMarks(ctx, dexIDs)
		return err
	}); err != nil {
		return err
	}
	lang := s.mangaDexLanguage()
	for _, id := range ids {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		dex := links[id]
		there := make([]string, 0, len(marks[dex]))
		for _, c := range marks[dex] {
			there = append(there, strings.ToLower(c))
		}
		m, err := repo.ChapterMapOf(id, dex)
		if err != nil {
			return err
		}
		// New ids, or volumes read here with no chapters to name: the chapter list.
		need := false
		for _, c := range there {
			if _, ok := m.Number[c]; !ok {
				need = true
				break
			}
		}
		if !need && len(m.Volume) == 0 {
			if lr, err := repo.LocalReadingOf(user, id, m); err == nil && len(lr.Read) == 0 {
				need = repo.HasReadVolumes(user, id)
			}
		}
		if need && budget.aggregates > 0 && now.Sub(repo.AggregatedAt(dex, lang)) >= mdAggFresh {
			budget.aggregates--
			refs, err := s.mangaMod().dex.Aggregate(ctx, dex, lang)
			if err != nil && !errors.Is(err, manga.ErrNotFound) {
				s.noteDexTrouble(err)
				return err
			}
			_ = repo.SaveChapterRefs(refs)
			_ = repo.NoteAggregated(dex, lang, now)
			if m, err = repo.ChapterMapOf(id, dex); err != nil {
				return err
			}
		}
		lr, err := repo.LocalReadingOf(user, id, m)
		if err != nil {
			return err
		}
		synced, err := repo.ReadSynced(user, dex)
		if err != nil {
			return err
		}
		plan := manga.PlanReads(lr, there, synced, m)
		if len(plan.Read) > 0 || len(plan.Unread) > 0 {
			if err := s.mdRun(user, func(sess *manga.DexSession) error { return sess.SetReadMarks(ctx, dex, plan.Read, plan.Unread) }); err != nil {
				return err
			}
			stats.ReadPushed += len(plan.Read) + len(plan.Unread)
		}
		changed, err := repo.ApplyDexReading(user, id, m, lr, plan.Scope, plan.Merged, now)
		if err != nil {
			return err
		}
		stats.ReadPulled += changed
		if err := repo.SetReadSynced(user, dex, plan.Merged); err != nil {
			return err
		}
		s.mdSettlePrints(user, []int64{id}, now)
	}
	return nil
}
