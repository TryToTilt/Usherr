package api

// Usherr: chapters from MangaDex, which you choose in Settings > Manga and which do
// nothing until then. This file: linking a series to its entry, the gentle look for
// new chapters, and waiting when MangaDex is away. The download itself is in
// home_manga_dex_fetch.go, the page's routes in home_manga_chapters.go. No account
// is used. See docs/manga.md for the rules this keeps to.

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"regexp"
	"strconv"
	"time"

	"github.com/rdborg/mediarium/internal/manga"
	"github.com/rdborg/mediarium/internal/queue"
)

const (
	mangaSourceDex = "mangadex"

	keyMangaDexPause    = "manga.mangadex_pause"    // why and until when MangaDex isn't asked: survives a restart
	keyMangaDexLanguage = "manga.mangadex_language" // the chapters' language; English when unset
	keyMangaDexSaver    = "manga.mangadex_saver"    // "1" takes the smaller, lower-quality pages
	keyMangaTidy        = "manga.tidy_chapters"     // "1" sets chapters aside once their volume arrives

	mangaDexJobEvery      = 15 * time.Minute
	mangaDexChecksPerRun  = 5                  // series looked at per run
	mangaDexCheckGap      = 5 * time.Second    // between series
	mangaDexCheckEvery    = 4 * time.Hour      // a followed series that's still going
	mangaDexFinishedEvery = 24 * time.Hour     // a finished one
	mangaDexDayBatch      = 10                 // chapters at a time by day: a few new ones
	mangaDexNightBatch    = 40                 // chapters at a time between 2 and 9: catching up
	mangaDexMaxBatches    = 2                  // batches waiting or running at once
	mangaDexManualEvery   = time.Minute        // the least time between checks by hand
	mangaDexLinkRetry     = 7 * 24 * time.Hour // a series with no entry there
)

var mangaDexNoGap bool // tests set this

func (s *Server) dexGap(d time.Duration) time.Duration {
	if mangaDexNoGap {
		return 0
	}
	return d
}

// ---- when MangaDex is away ----

// mangaDexPause is what's remembered about MangaDex being away.
type mangaDexPause struct {
	Kind  string    `json:"kind"`
	Until time.Time `json:"until"`
	Fails int       `json:"fails"`
}

func (s *Server) readDexPause() mangaDexPause {
	var p mangaDexPause
	if v, _ := s.Settings.Get(keyMangaDexPause); v != "" {
		_ = json.Unmarshal([]byte(v), &p)
	}
	return p
}

// restoreDexPause carries a wait over a restart: a ban's clock starts again with any
// request, so a restart mustn't ask.
func (s *Server) restoreDexPause(c *manga.MangaDex) {
	if p := s.readDexPause(); p.Until.After(time.Now()) {
		c.Pause(p.Kind, p.Until)
	}
}

// dexBackoff is how long to leave MangaDex after so many failed looks in a row.
func dexBackoff(fails int) time.Duration {
	wait := 15 * time.Minute << min(max(fails-1, 0), 5)
	return min(wait, 6*time.Hour)
}

// noteDexTrouble keeps MangaDex's word, or for an outage a wait that grows.
func (s *Server) noteDexTrouble(err error) (*manga.DexError, bool) {
	var de *manga.DexError
	if !errors.As(err, &de) {
		return nil, false
	}
	p := s.readDexPause()
	p.Fails++
	p.Kind = de.Kind
	p.Until = de.Until
	if de.Kind == manga.DexDown {
		if until := time.Now().Add(dexBackoff(p.Fails)); until.After(p.Until) {
			p.Until = until
		}
	}
	s.mangaMod().dex.Pause(p.Kind, p.Until)
	if b, err := json.Marshal(p); err == nil {
		_ = s.Settings.Set(keyMangaDexPause, string(b), false)
	}
	slog.Info("manga: MangaDex is away", "kind", p.Kind, "until", p.Until)
	return &manga.DexError{Kind: p.Kind, Until: p.Until}, true
}

func (s *Server) clearDexTrouble() {
	if p := s.readDexPause(); p.Fails > 0 || !p.Until.IsZero() {
		_ = s.Settings.Set(keyMangaDexPause, "", false)
	}
}

// ---- linking ----

// linkMangaDex finds a series' entry: by a cross-link from AniList or back from
// MangaDex, then by title, leaving anything unsure for a person.
func (s *Server) linkMangaDex(ctx context.Context, sr manga.Series, d *manga.DexState) error {
	st := s.mangaMod()
	if sr.AniListID > 0 {
		f, err := st.anilist.Get(ctx, sr.AniListID)
		if err == nil && f.MangaDexID != "" {
			if found, err := st.dex.Get(ctx, f.MangaDexID); err == nil {
				d.DexID, d.Link, d.LinkHow, d.Candidates = found.ID, manga.DexLinked, "anilist", nil
				return nil
			} else if !errors.Is(err, manga.ErrNotFound) {
				return err
			}
		}
	}
	names := manga.SearchableNames(st.repo.Names(sr), 2)
	var unsure []manga.DexSeries
	seen := map[string]bool{}
	for _, name := range names {
		found, err := st.dex.Search(ctx, manga.SearchName(name), sr.IsAdult)
		if err != nil {
			return err
		}
		best, how, maybe := manga.DexMatch(sr, st.repo.Names(sr), found)
		if best != nil {
			d.DexID, d.Link, d.LinkHow, d.Candidates = best.ID, manga.DexLinked, how, nil
			return nil
		}
		for _, m := range maybe {
			if !seen[m.ID] {
				seen[m.ID] = true
				unsure = append(unsure, m)
			}
		}
	}
	d.DexID, d.LinkHow, d.Candidates = "", "", nil
	if len(unsure) == 0 {
		d.Link = manga.DexNone
		return nil
	}
	d.Link = manga.DexUnsure
	for i, m := range unsure {
		if i == 6 {
			break
		}
		d.Candidates = append(d.Candidates, manga.DexCandidate{ID: m.ID, Title: m.Title, Year: m.Year})
	}
	return nil
}

// ---- looking for chapters ----

// mangaDexLanguage is the language to ask for.
func (s *Server) mangaDexLanguage() string {
	if v, _ := s.Settings.Get(keyMangaDexLanguage); v != "" {
		return v
	}
	return "en"
}

var mangaDexLanguageRe = regexp.MustCompile(`^[a-z]{2}(-[a-z]{2})?$`)

// checkMangaDexSeries links a series if it isn't yet, and reads its chapters. The
// error says when the whole service is away; a series' own bad news is kept in its
// state, for the page.
func (s *Server) checkMangaDexSeries(ctx context.Context, sr manga.Series) error {
	repo := s.mangaRepo()
	st := s.mangaMod()
	d := repo.DexStateOf(sr.ID)
	now := time.Now()
	d.CheckedAt = now.UTC().Format(time.RFC3339)
	save := func() error { return repo.SaveDexState(d) }
	fail := func(err error) error {
		if de, ok := s.noteDexTrouble(err); ok {
			d.Trouble = de.Kind
			d.NextAt = de.Until.UTC().Format(time.RFC3339)
			_ = save()
			return de
		}
		d.Trouble = manga.DexDown
		d.Empties++
		d.NextAt = now.Add(manga.BackoffAfter(d.Empties)).UTC().Format(time.RFC3339)
		_ = save()
		return nil
	}
	if d.Link != manga.DexLinked {
		if err := s.linkMangaDex(ctx, sr, &d); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fail(err)
		}
		if d.Link != manga.DexLinked {
			d.Trouble = ""
			d.NextAt = now.Add(mangaDexLinkRetry).UTC().Format(time.RFC3339)
			s.clearDexTrouble()
			return save()
		}
	}
	feed, err := st.dex.Feed(ctx, d.DexID, s.mangaDexLanguage(), sr.IsAdult)
	if errors.Is(err, manga.ErrNotFound) { // the entry itself is gone
		d.Link, d.DexID = manga.DexNone, ""
		d.Stats = manga.DexStats{}
		d.NextAt = now.Add(mangaDexLinkRetry).UTC().Format(time.RFC3339)
		return save()
	}
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fail(err)
	}
	// Only the first look of a "new chapters only" series takes nothing: what's
	// there then is the starting line.
	first := d.Stats.Listed == 0 && d.Empties == 0 && d.Follow == manga.FollowNew
	res, err := repo.SaveFeed(sr.ID, feed, d.PreferGroup, s.mangaDexLanguage(), d.Follow == manga.FollowAll || (d.Follow == manga.FollowNew && !first))
	if err != nil {
		return err
	}
	stats := res.Stats
	if entry, err := st.dex.Get(ctx, d.DexID); err == nil {
		stats.Languages, stats.LicensedLink = entry.Languages, entry.LicensedLink
	} else if ctx.Err() != nil {
		return ctx.Err()
	} else {
		stats.Languages, stats.LicensedLink = d.Stats.Languages, d.Stats.LicensedLink
	}
	d.Stats = stats
	d.Trouble = ""
	s.clearDexTrouble()
	s.claimMangaChapterFiles(sr)
	if stats.Readable == 0 {
		d.Empties++
		d.NextAt = now.Add(manga.BackoffAfter(d.Empties)).UTC().Format(time.RFC3339)
		return save()
	}
	d.Empties = 0
	every := mangaDexCheckEvery
	if sr.Status == "finished" || sr.Status == "cancelled" {
		every = mangaDexFinishedEvery
	}
	// Spread out by series, so they don't all come due together.
	d.NextAt = now.Add(every + time.Duration(sr.ID%48)*5*time.Minute).UTC().Format(time.RFC3339)
	if err := save(); err != nil {
		return err
	}
	if res.New > 0 {
		slog.Info("manga: new chapters", "series", sr.Title, "new", res.New)
	}
	return nil
}

// mangaDexJob is the gentle look: a few series a run, spaced out, with nothing asked
// while MangaDex is away. Then whatever's wanted goes in the download line.
func (s *Server) mangaDexJob(ctx context.Context) {
	if !s.mangaEnabled() || !s.currentMangaSources().MangaDex || s.mangaFolderProblem() != "" {
		return
	}
	st := s.mangaMod()
	if !st.dexJob.TryLock() {
		return
	}
	defer st.dexJob.Unlock()
	if st.dex.Paused() != nil {
		return
	}
	now := time.Now()
	due, err := st.repo.DexDue(now, mangaDexChecksPerRun)
	if err != nil {
		return
	}
	for i, d := range due {
		if i > 0 && !sleepOrDone(ctx, s.dexGap(mangaDexCheckGap)) {
			return
		}
		sr, err := st.repo.GetSeries(d.SeriesID)
		if err != nil || !s.mangaSourcesFor(sr).MangaDex {
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		err = s.checkMangaDexSeries(cctx, sr)
		cancel()
		var de *manga.DexError
		if errors.As(err, &de) {
			return // all of it is away: not one more this run
		}
	}
	s.startMangaDexBatches(time.Now())
}

// mangaDexWindow is the house rule for heavy work: between 2 and 9 in the morning.
func mangaDexWindow(now time.Time) bool {
	return now.Hour() >= mangaScanFromHour && now.Hour() < mangaScanToHour
}

// startMangaDexBatches puts what's wanted in the download line. By day, a series
// with a few chapters waiting gets them; a big catch-up waits for the night.
func (s *Server) startMangaDexBatches(now time.Time) {
	repo := s.mangaRepo()
	list, err := repo.ListSeries()
	if err != nil {
		return
	}
	limit := mangaDexDayBatch
	night := mangaDexWindow(now)
	if night {
		limit = mangaDexNightBatch
	}
	for _, sr := range list {
		active, err := repo.ActiveDexBatches()
		if err != nil || active >= mangaDexMaxBatches {
			return
		}
		if repo.DexStateOf(sr.ID).Follow == manga.FollowOff || !s.mangaSourcesFor(sr).MangaDex {
			continue
		}
		if busy, err := repo.ActiveDexBatch(sr.ID); err != nil || busy {
			continue
		}
		chapters, total, err := repo.ChaptersToFetch(sr.ID, now, limit)
		if err != nil || len(chapters) == 0 {
			continue
		}
		if !night && total > mangaDexDayBatch {
			continue // waits for tonight, and the page says so
		}
		if _, err := s.grabMangaDexBatch(sr, chapters, queue.PriorityAutomatic); err != nil {
			slog.Info("manga: mangadex batch", "series", sr.Title, "err", err)
		}
	}
}

// ---- following ----

// followMangaChapters switches a series' chapters on and looks right away.
func (s *Server) followMangaChapters(sr manga.Series, follow string) {
	repo := s.mangaRepo()
	d := repo.DexStateOf(sr.ID)
	d.SeriesID = sr.ID
	was := d.Follow
	d.Follow = follow
	d.NextAt = ""
	if err := repo.SaveDexState(d); err != nil {
		slog.Warn("manga: follow chapters", "series", sr.Title, "err", err)
		return
	}
	if follow == manga.FollowAll && was != manga.FollowAll {
		_ = repo.WantAllChapters(sr.ID)
	}
	s.lookAtMangaDexSoon(sr)
}

// lookAtMangaDexSoon checks one series in the background, for a person's button.
func (s *Server) lookAtMangaDexSoon(sr manga.Series) {
	s.background(func() {
		st := s.mangaMod()
		if st.dex.Paused() != nil {
			return
		}
		st.dexJob.Lock()
		defer st.dexJob.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		if err := s.checkMangaDexSeries(ctx, sr); err != nil {
			return
		}
		s.startMangaDexBatches(time.Now())
	})
}

// mangaChapterSettingsChange is what Settings > Manga may change for chapters.
type mangaChapterSettingsChange struct {
	Language *string `json:"chapterLanguage"`
	Saver    *bool   `json:"chapterSaver"`
	Tidy     *bool   `json:"tidyChapters"`
}

func (s *Server) saveMangaChapterSettings(c mangaChapterSettingsChange) string {
	if c.Language != nil {
		if !mangaDexLanguageRe.MatchString(*c.Language) {
			return "That isn't a language."
		}
		_ = s.Settings.Set(keyMangaDexLanguage, *c.Language, false)
	}
	flag := func(key string, v *bool) {
		if v == nil {
			return
		}
		_ = s.Settings.Set(key, strconv.Itoa(map[bool]int{true: 1}[*v]), false)
	}
	flag(keyMangaDexSaver, c.Saver)
	flag(keyMangaTidy, c.Tidy)
	return ""
}

func (s *Server) mangaChapterSettings() map[string]any {
	return map[string]any{"language": s.mangaDexLanguage(), "saver": s.mangaDexSaver(), "tidy": s.mangaTidyOn(), "away": s.mangaDexAway()}
}

func (s *Server) mangaDexSaver() bool {
	v, _ := s.Settings.Get(keyMangaDexSaver)
	return v == "1"
}

func (s *Server) mangaTidyOn() bool {
	v, _ := s.Settings.Get(keyMangaTidy)
	return v == "1"
}

// mangaDexAwayState is what the page says when MangaDex can't be asked.
type mangaDexAwayState struct {
	Kind  string `json:"kind"`
	Until string `json:"until"`
}

func (s *Server) mangaDexAway() *mangaDexAwayState {
	p := s.mangaMod().dex.Paused()
	if p == nil {
		return nil
	}
	return &mangaDexAwayState{Kind: p.Kind, Until: p.Until.UTC().Format(time.RFC3339)}
}
