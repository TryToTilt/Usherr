package api

// Usherr: browsing MangaDex from Manga > Discover, the one place to find manga. The
// AniList rows stay as they are (home_manga_find.go); this adds MangaDex's rows when
// its source is on in Settings > Manga, a search of MangaDex, a random title, a
// title's page, covers, and the "New official volumes" row from your own downloads.
// Reading a chapter online is in home_manga_browse_read.go.
//
// Every answer is kept for a few minutes, for everyone, so a room full of people
// browsing asks MangaDex no more than one person would. Everything goes through
// the same client and pace as chapter downloads (internal/manga/mangadex.go).
//
// Content ratings: safe and suggestive only, unless the NSFW section is set up, the
// account may see it, and the page asked for adult titles with its own switch.

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/manga"
)

const (
	dexBrowseTTL   = 5 * time.Minute
	dexTitleTTL    = 10 * time.Minute
	dexTagsTTL     = 24 * time.Hour
	dexCoverTTL    = 6 * time.Hour
	dexCacheMax    = 400      // answers kept at once
	dexCoverMax    = 48 << 20 // bytes of covers kept at once
	dexRowSize     = 18
	dexOfficialAge = 60 * 24 * time.Hour
)

// ---- a short memory of answers ----

type dexCached struct {
	until time.Time
	value any
}

type dexCoverCached struct {
	until time.Time
	ct    string
	data  []byte
}

type dexBrowseState struct {
	mu         sync.Mutex
	fills      map[string]*sync.Mutex // one fill of each answer at a time; answers may fill others
	items      map[string]dexCached
	covers     map[string]dexCoverCached
	coverBytes int
	known      sync.Map // covers handed out to pages: only those are fetched
}

var dexBrowseStates sync.Map

func (s *Server) dexBrowse() *dexBrowseState {
	v, _ := dexBrowseStates.LoadOrStore(s, &dexBrowseState{items: map[string]dexCached{}, covers: map[string]dexCoverCached{}, fills: map[string]*sync.Mutex{}})
	return v.(*dexBrowseState)
}

func (b *dexBrowseState) get(key string) (any, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	c, ok := b.items[key]
	if !ok || time.Now().After(c.until) {
		return nil, false
	}
	return c.value, true
}

func (b *dexBrowseState) put(key string, v any, ttl time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	if len(b.items) >= dexCacheMax {
		for k, c := range b.items {
			if now.After(c.until) {
				delete(b.items, k)
			}
		}
		for k := range b.items { // still full: any one goes
			if len(b.items) < dexCacheMax {
				break
			}
			delete(b.items, k)
		}
	}
	b.items[key] = dexCached{until: now.Add(ttl), value: v}
}

// dexCachedAnswer is the kept answer for key, or a new one from fill.
func dexCachedAnswer[T any](s *Server, key string, ttl time.Duration, fill func() (T, error)) (T, error) {
	b := s.dexBrowse()
	if v, ok := b.get(key); ok {
		return v.(T), nil
	}
	b.mu.Lock()
	lock, ok := b.fills[key]
	if !ok {
		if len(b.fills) > dexCacheMax {
			b.fills = map[string]*sync.Mutex{}
		}
		lock = &sync.Mutex{}
		b.fills[key] = lock
	}
	b.mu.Unlock()
	lock.Lock()
	defer lock.Unlock()
	if v, ok := b.get(key); ok { // filled while this one waited
		return v.(T), nil
	}
	v, err := fill()
	if err != nil {
		var zero T
		return zero, err
	}
	b.put(key, v, ttl)
	return v, nil
}

// ---- who may see what ----

// dexOn says whether MangaDex may be browsed: the Manga module is on (the routes
// check that) and so is the MangaDex source.
func (s *Server) dexOn() bool { return s.currentMangaSources().MangaDex }

func (s *Server) needDex(w http.ResponseWriter) bool {
	if !s.dexOn() {
		writeError(w, http.StatusNotFound, "MangaDex is switched off. Turn it on in Settings > Manga.")
		return false
	}
	return true
}

// dexAdultAllowed says whether this account may ever see adult titles here.
func (s *Server) dexAdultAllowed(r *http.Request) bool {
	return s.nsfwEnabled() && s.canSeeAdultManga(r)
}

// dexRatings are the ratings to ask for: adult ones only on the page's own switch.
func (s *Server) dexRatings(r *http.Request) []string {
	if r.URL.Query().Get("adult") == "1" && s.dexAdultAllowed(r) {
		return manga.AllRatings
	}
	return manga.SafeRatings
}

func dexRatingAllowed(rating string, ratings []string) bool {
	if rating == "" {
		rating = "safe"
	}
	for _, r := range ratings {
		if r == rating {
			return true
		}
	}
	return false
}

// dexProblem says in plain words why MangaDex couldn't be asked, and keeps a
// wait it asked for, for everything else that asks it.
func (s *Server) dexProblem(err error) string {
	if de, ok := s.noteDexTrouble(err); ok {
		switch de.Kind {
		case manga.DexBusy:
			return "MangaDex asked Usherr to slow down. Try again in a minute."
		case manga.DexRefused:
			return "MangaDex is refusing requests from this server for now. Usherr waits, and tries again later by itself."
		}
	}
	if errors.Is(err, manga.ErrNotFound) {
		return "MangaDex doesn't have that any more."
	}
	return "MangaDex couldn't be reached. Try again in a few minutes."
}

// ---- tiles ----

// dexTile is a title dressed for the page.
type dexTile struct {
	manga.DexTitle
	CoverURL  string `json:"coverUrl,omitempty"`
	InLibrary int64  `json:"inLibrary,omitempty"`
	CanRead   bool   `json:"canRead"`
}

// dexLibrary is which MangaDex and AniList ids are series in the library.
type dexLibrary struct {
	byAniList map[int64]int64
	byDex     map[string]int64
}

func (s *Server) dexLibraryNow() dexLibrary {
	lib := dexLibrary{byDex: map[string]int64{}}
	lib.byAniList, _ = s.mangaRepo().LibraryIDs()
	if rows, err := s.db.Query(`SELECT dex_id, series_id FROM manga_dex_series WHERE dex_id != '' AND link = ?`, manga.DexLinked); err == nil {
		for rows.Next() {
			var id string
			var sid int64
			if rows.Scan(&id, &sid) == nil {
				lib.byDex[strings.ToLower(id)] = sid
			}
		}
		rows.Close()
	}
	return lib
}

func (l dexLibrary) of(t manga.DexTitle) int64 {
	if id := l.byDex[strings.ToLower(t.ID)]; id > 0 {
		return id
	}
	if t.AniListID > 0 {
		return l.byAniList[t.AniListID]
	}
	return 0
}

// dexCoverURL is a cover's address through Usherr, remembered as handed out.
func (s *Server) dexCoverURL(mangaID, file string) string {
	if file == "" {
		return ""
	}
	s.dexBrowse().known.Store(mangaID+"/"+file, struct{}{})
	return "/api/manga/dex/covers/" + mangaID + "/" + url.PathEscape(file)
}

func (s *Server) dexTiles(list []manga.DexTitle, lib dexLibrary, ratings []string) []dexTile {
	lang := s.mangaDexLanguage()
	out := make([]dexTile, 0, len(list))
	for _, t := range list {
		if !dexRatingAllowed(t.Rating, ratings) {
			continue
		}
		tile := dexTile{DexTitle: t, CoverURL: s.dexCoverURL(t.ID, t.CoverFile), InLibrary: lib.of(t)}
		for _, l := range t.Languages {
			if l == lang {
				tile.CanRead = true
			}
		}
		out = append(out, tile)
	}
	return out
}

func ratingsKey(r []string) string { return strings.Join(r, ",") }

// ---- the hub ----

type dexUpdateTile struct {
	manga.DexUpdate
	Manga *dexTile `json:"manga,omitempty"`
}

type dexListRow struct {
	Name   string    `json:"name"`
	Titles []dexTile `json:"titles"`
}

// dexHub is MangaDex's rows, as kept for everyone (titles undressed).
type dexHub struct {
	Popular     []manga.DexTitle
	Latest      []manga.DexUpdate
	Seasonal    dexHubList
	Recommended dexHubList
	Self        dexHubList
	Recent      []manga.DexTitle
	Problem     string
}

type dexHubList struct {
	Name   string
	Titles []manga.DexTitle
}

// fillDexHub asks MangaDex for each row, the way its own home page does. A row
// that fails is left out; MangaDex asking to wait stops the rest.
func (s *Server) fillDexHub(ctx context.Context, ratings []string) (dexHub, error) {
	c := s.mangaMod().dex
	lang := s.mangaDexLanguage()
	var hub dexHub
	stop := func(err error) bool {
		var de *manga.DexError
		if errors.As(err, &de) || ctx.Err() != nil {
			hub.Problem = s.dexProblem(err)
			return true
		}
		return false
	}
	// Popular new titles: the most followed of those added in the last month.
	popular, _, err := c.Titles(ctx, manga.DexQuery{Order: "followedCount", HasChapters: true, CreatedSince: time.Now().AddDate(0, -1, 0), Ratings: ratings, Limit: 10})
	if err != nil && stop(err) {
		return hub, nil
	}
	hub.Popular = popular
	// Latest updates: the newest chapters, then their series in one call.
	latest, err := c.Latest(ctx, lang, ratings, 32)
	if err != nil && stop(err) {
		return hub, nil
	}
	if len(latest) > 0 {
		ids := []string{}
		seen := map[string]bool{}
		chapterIDs := []string{}
		for _, u := range latest {
			chapterIDs = append(chapterIDs, u.ChapterID)
			if !seen[u.MangaID] {
				seen[u.MangaID] = true
				ids = append(ids, u.MangaID)
			}
		}
		titles, err := c.TitlesByID(ctx, ids, ratings)
		if err != nil && stop(err) {
			return hub, nil
		}
		by := map[string]manga.DexTitle{}
		for _, t := range titles {
			by[t.ID] = t
		}
		comments, err := c.ChapterComments(ctx, chapterIDs)
		if err != nil && stop(err) {
			return hub, nil
		}
		for _, u := range latest {
			t, ok := by[u.MangaID]
			if !ok {
				continue // a rating not asked for, or gone
			}
			u.Manga = &t
			u.Comments = comments[u.ChapterID]
			hub.Latest = append(hub.Latest, u)
		}
	}
	// Seasonal, recommended and self-published: the staff's public lists, found
	// from their Staff Picks list.
	staff := manga.DexStaffPicks
	if v, _ := s.Settings.Get(keyMangaDexStaffList); manga.IsDexID(v) {
		staff = v
	}
	if picks, err := c.List(ctx, staff); err == nil {
		seasonal, recommended, self := manga.DexList{}, picks, manga.DexList{}
		if picks.Owner != "" {
			if lists, err := c.UserLists(ctx, picks.Owner); err == nil {
				var rec manga.DexList
				seasonal, rec, self = manga.PickLists(lists)
				if rec.ID != "" {
					recommended = rec
				}
			} else if stop(err) {
				return hub, nil
			}
		}
		for _, row := range []struct {
			list manga.DexList
			to   *dexHubList
		}{{seasonal, &hub.Seasonal}, {recommended, &hub.Recommended}, {self, &hub.Self}} {
			if len(row.list.Titles) == 0 {
				continue
			}
			ids := row.list.Titles
			if len(ids) > dexRowSize*2 {
				ids = ids[:dexRowSize*2]
			}
			titles, err := c.TitlesByID(ctx, ids, ratings)
			if err != nil {
				if stop(err) {
					return hub, nil
				}
				continue
			}
			row.to.Name, row.to.Titles = row.list.Name, titles
		}
	} else if stop(err) {
		return hub, nil
	}
	// Recently added.
	recent, _, err := c.Titles(ctx, manga.DexQuery{Order: "createdAt", HasChapters: true, Ratings: ratings, Limit: dexRowSize})
	if err != nil && stop(err) {
		return hub, nil
	}
	hub.Recent = recent
	return hub, nil
}

// mangaOfficialTile is a recent official volume that came through your indexers.
type mangaOfficialTile struct {
	SeriesID int64  `json:"seriesId"`
	VolumeID int64  `json:"volumeId"`
	Title    string `json:"title"`
	Number   string `json:"number"`
	CoverURL string `json:"coverUrl,omitempty"`
	At       string `json:"at"`
	Have     bool   `json:"have"`
}

// mangaOfficialRow is the newest official English volumes your indexers gave,
// from what was already downloaded: nothing is searched for it.
func (s *Server) mangaOfficialRow(adult bool) []mangaOfficialTile {
	out := []mangaOfficialTile{}
	since := time.Now().Add(-dexOfficialAge).UTC().Format("2006-01-02T15:04:05")
	rows, err := s.db.Query(`SELECT s.id, v.id, s.title, v.number, s.cover_url, MAX(q.added_at), v.status, s.is_adult
		FROM manga_downloads d JOIN download_queue q ON q.id = d.queue_id
		JOIN manga_volumes v ON v.id = d.volume_id JOIN manga_series s ON s.id = d.series_id
		WHERE d.class = ? AND q.added_at >= ?
		GROUP BY v.id ORDER BY MAX(q.added_at) DESC LIMIT 40`, string(manga.ClassOfficial), since)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var t mangaOfficialTile
		var status string
		var isAdult bool
		if rows.Scan(&t.SeriesID, &t.VolumeID, &t.Title, &t.Number, &t.CoverURL, &t.At, &status, &isAdult) != nil {
			continue
		}
		if isAdult && !adult {
			continue
		}
		t.Have = status == manga.StatusDownloaded
		t.CoverURL = s.mangaImage(t.CoverURL)
		out = append(out, t)
		if len(out) == dexRowSize {
			break
		}
	}
	return out
}

// GET /api/manga/dex/hub?adult=1: MangaDex's rows (when its source is on) and the
// new official volumes.
func (s *Server) handleMangaDexHub(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"dexOn": s.dexOn(), "adultAllowed": s.dexAdultAllowed(r), "official": s.mangaOfficialRow(s.canSeeAdultManga(r)), "language": s.mangaDexLanguage()}
	if !s.dexOn() {
		writeJSON(w, http.StatusOK, out)
		return
	}
	ratings := s.dexRatings(r)
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	hub, err := dexCachedAnswer(s, "hub:"+ratingsKey(ratings)+":"+s.mangaDexLanguage(), dexBrowseTTL, func() (dexHub, error) {
		h, err := s.fillDexHub(ctx, ratings)
		if err == nil && h.Problem != "" && len(h.Popular)+len(h.Latest)+len(h.Recent) == 0 {
			return h, errors.New(h.Problem) // nothing to keep: ask again next time
		}
		return h, err
	})
	if err != nil {
		hub.Problem = err.Error()
	}
	lib := s.dexLibraryNow()
	list := func(l dexHubList) *dexListRow {
		if len(l.Titles) == 0 {
			return nil
		}
		tiles := s.dexTiles(l.Titles, lib, ratings)
		if len(tiles) > dexRowSize {
			tiles = tiles[:dexRowSize]
		}
		return &dexListRow{Name: l.Name, Titles: tiles}
	}
	latest := make([]dexUpdateTile, 0, len(hub.Latest))
	for _, u := range hub.Latest {
		tiles := s.dexTiles([]manga.DexTitle{*u.Manga}, lib, ratings)
		if len(tiles) == 0 {
			continue
		}
		u2 := dexUpdateTile{DexUpdate: u, Manga: &tiles[0]}
		u2.DexUpdate.Manga = nil
		latest = append(latest, u2)
	}
	out["mangadex"] = map[string]any{
		"popular": s.dexTiles(hub.Popular, lib, ratings), "latest": latest, "recent": s.dexTiles(hub.Recent, lib, ratings),
		"seasonal": list(hub.Seasonal), "recommended": list(hub.Recommended), "selfPublished": list(hub.Self),
		"problem": hub.Problem,
	}
	writeJSON(w, http.StatusOK, out)
}

// ---- search, tags, random ----

// GET /api/manga/dex/search?title=&tag=&notTag=&status=&demo=&lang=&year=&order=&asc=1&readable=1&adult=1&offset=
func (s *Server) handleMangaDexSearch(w http.ResponseWriter, r *http.Request) {
	if !s.needDex(w) {
		return
	}
	q := r.URL.Query()
	dq := manga.DexQuery{
		Title: q.Get("title"), IncludedTags: q["tag"], ExcludedTags: q["notTag"], Status: q["status"], Demographic: q["demo"],
		Original: q["lang"], Order: q.Get("order"), Ascending: q.Get("asc") == "1", Ratings: s.dexRatings(r), Limit: 24,
	}
	if len([]rune(dq.Title)) > 200 {
		dq.Title = string([]rune(dq.Title)[:200])
	}
	if y, err := strconv.Atoi(q.Get("year")); err == nil && y > 1900 && y < 2200 {
		dq.Year = y
	}
	if o, err := strconv.Atoi(q.Get("offset")); err == nil && o > 0 {
		dq.Offset = o
	}
	if q.Get("readable") == "1" {
		dq.Translated = s.mangaDexLanguage()
	}
	if dq.Order == "" && strings.TrimSpace(dq.Title) != "" {
		dq.Order = "relevance"
	}
	values := dq.Values()
	type answer struct {
		Titles []manga.DexTitle
		Total  int
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	got, err := dexCachedAnswer(s, "search:"+values.Encode(), dexBrowseTTL, func() (answer, error) {
		t, total, err := s.mangaMod().dex.Titles(ctx, dq)
		return answer{t, total}, err
	})
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"results": []any{}, "problem": s.dexProblem(err)})
		return
	}
	offset, _ := strconv.Atoi(values.Get("offset"))
	tiles := s.dexTiles(got.Titles, s.dexLibraryNow(), dq.Ratings)
	writeJSON(w, http.StatusOK, map[string]any{"results": tiles, "total": got.Total, "more": offset+len(got.Titles) < got.Total && offset+len(got.Titles) < 10000})
}

// GET /api/manga/dex/tags: MangaDex's tags, by group, for the search panel.
func (s *Server) handleMangaDexTags(w http.ResponseWriter, r *http.Request) {
	if !s.needDex(w) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	tags, err := dexCachedAnswer(s, "tags", dexTagsTTL, func() ([]manga.DexTag, error) { return s.mangaMod().dex.Tags(ctx) })
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"tags": []any{}, "problem": s.dexProblem(err)})
		return
	}
	sorted := append([]manga.DexTag(nil), tags...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Group != sorted[j].Group {
			return sorted[i].Group < sorted[j].Group
		}
		return sorted[i].Name < sorted[j].Name
	})
	writeJSON(w, http.StatusOK, map[string]any{"tags": sorted})
}

// GET /api/manga/dex/random?adult=1: one title, picked by MangaDex. Never kept.
func (s *Server) handleMangaDexRandom(w http.ResponseWriter, r *http.Request) {
	if !s.needDex(w) {
		return
	}
	ratings := s.dexRatings(r)
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	t, err := s.mangaMod().dex.Random(ctx, ratings)
	if err != nil {
		writeError(w, http.StatusBadGateway, s.dexProblem(err))
		return
	}
	tiles := s.dexTiles([]manga.DexTitle{t}, s.dexLibraryNow(), ratings)
	if len(tiles) == 0 {
		writeError(w, http.StatusBadGateway, "MangaDex gave a title this account can't see. Try again.")
		return
	}
	writeJSON(w, http.StatusOK, tiles[0])
}

// ---- one title ----

// dexChapterRow is one chapter in a title's list: one upload a number, the one
// read online, with its group credited.
type dexChapterRow struct {
	ID          string `json:"id"`
	Number      string `json:"number"`
	Volume      string `json:"volume,omitempty"`
	Title       string `json:"title,omitempty"`
	Group       string `json:"group,omitempty"`
	Pages       int    `json:"pages,omitempty"`
	PublishedAt string `json:"publishedAt,omitempty"`
	ExternalURL string `json:"externalUrl,omitempty"`
	Copies      int    `json:"copies,omitempty"`
}

// dexReadable are a title's chapters one a number, in reading order. The group
// given wins where it did a chapter; then the group that did the most.
func dexReadable(feed []manga.DexChapter, preferGroup string) []dexChapterRow {
	type slot struct {
		copies []manga.ChapterCopy
		first  manga.DexChapter
	}
	byNumber := map[string]*slot{}
	order := []string{}
	counts := map[string]int{}
	for _, c := range feed {
		key := c.Number
		if key == "" {
			key = manga.OneshotNumber + ":" + c.ID // each one-shot is its own
		}
		cp := manga.CopyOf(c)
		if cp.Readable() {
			counts[cp.GroupKey]++
		}
		sl := byNumber[key]
		if sl == nil {
			sl = &slot{first: c}
			byNumber[key] = sl
			order = append(order, key)
		}
		sl.copies = append(sl.copies, cp)
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := byNumber[order[i]].first, byNumber[order[j]].first
		return manga.ChapterSort(a.Number) < manga.ChapterSort(b.Number)
	})
	out := make([]dexChapterRow, 0, len(order))
	for _, key := range order {
		sl := byNumber[key]
		best := manga.ChooseCopy(sl.copies, preferGroup, counts)
		number := sl.first.Number
		if number == "" {
			number = manga.OneshotNumber
		}
		row := dexChapterRow{ID: best.DexID, Number: number, Volume: best.Volume, Title: best.Title, Group: best.GroupName, Pages: best.Pages, PublishedAt: best.PublishedAt, ExternalURL: best.ExternalURL, Copies: len(sl.copies)}
		if !best.Readable() && best.ExternalURL == "" {
			continue // taken down: nothing to read or follow
		}
		out = append(out, row)
	}
	return out
}

// dexFeedFor is a title's chapters in the reading language, kept a few minutes.
func (s *Server) dexFeedFor(ctx context.Context, mangaID string, ratings []string) ([]manga.DexChapter, error) {
	adult := len(ratings) > 2
	return dexCachedAnswer(s, "feed:"+mangaID+":"+s.mangaDexLanguage()+":"+strconv.FormatBool(adult), dexBrowseTTL, func() ([]manga.DexChapter, error) {
		return s.mangaMod().dex.Feed(ctx, mangaID, s.mangaDexLanguage(), adult)
	})
}

// dexTitleFor is a title, kept a few minutes.
func (s *Server) dexTitleFor(ctx context.Context, id string) (manga.DexTitle, error) {
	return dexCachedAnswer(s, "title:"+id, dexTitleTTL, func() (manga.DexTitle, error) {
		t, err := s.mangaMod().dex.Title(ctx, id)
		if err != nil {
			return t, err
		}
		if st, err := s.mangaMod().dex.TitleStats(ctx, []string{id}); err == nil {
			t.Follows, t.Score = st[id].Follows, st[id].Score
		}
		return t, nil
	})
}

// GET /api/manga/dex/titles/{id}?adult=1: a title and its chapters, for the preview.
func (s *Server) handleMangaDexTitle(w http.ResponseWriter, r *http.Request) {
	if !s.needDex(w) {
		return
	}
	id := strings.ToLower(r.PathValue("id"))
	if !manga.IsDexID(id) {
		writeError(w, http.StatusNotFound, "That isn't a MangaDex title.")
		return
	}
	ratings := s.dexRatings(r)
	if s.dexAdultAllowed(r) {
		ratings = manga.AllRatings // a title opened by its id: its own rating decides below
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	t, err := s.dexTitleFor(ctx, id)
	if err != nil {
		writeError(w, http.StatusBadGateway, s.dexProblem(err))
		return
	}
	tiles := s.dexTiles([]manga.DexTitle{t}, s.dexLibraryNow(), ratings)
	if len(tiles) == 0 {
		writeError(w, http.StatusNotFound, "That title is for accounts allowed 18+ content.")
		return
	}
	out := map[string]any{"title": tiles[0], "language": s.mangaDexLanguage()}
	feed, err := s.dexFeedFor(ctx, id, ratings)
	if err != nil {
		out["problem"] = s.dexProblem(err)
		out["chapters"] = []any{}
	} else {
		out["chapters"] = dexReadable(feed, "")
	}
	writeJSON(w, http.StatusOK, out)
}

// ---- covers ----

// GET /api/manga/dex/covers/{id}/{file}?s=256|512: a cover handed out to a page,
// fetched without any login and kept a few hours for everyone.
func (s *Server) handleMangaDexCover(w http.ResponseWriter, r *http.Request) {
	id, file := strings.ToLower(r.PathValue("id")), r.PathValue("file")
	b := s.dexBrowse()
	if _, ok := b.known.Load(id + "/" + file); !ok || !manga.IsDexID(id) || !manga.ValidCoverFile(file) {
		writeError(w, http.StatusNotFound, "Unknown image.")
		return
	}
	size := 256
	if r.URL.Query().Get("s") == "512" {
		size = 512
	}
	key := id + "/" + file + "/" + strconv.Itoa(size)
	b.mu.Lock()
	c, ok := b.covers[key]
	b.mu.Unlock()
	if !ok || time.Now().After(c.until) {
		var buf bytes.Buffer
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		ct, err := s.mangaMod().dex.Cover(ctx, id, file, size, &buf)
		cancel()
		if err != nil {
			writeError(w, http.StatusBadGateway, "The image couldn't be fetched.")
			return
		}
		c = dexCoverCached{until: time.Now().Add(dexCoverTTL), ct: ct, data: buf.Bytes()}
		b.mu.Lock()
		if old, had := b.covers[key]; had {
			b.coverBytes -= len(old.data)
		}
		for k, o := range b.covers { // room for it
			if b.coverBytes+len(c.data) <= dexCoverMax {
				break
			}
			b.coverBytes -= len(o.data)
			delete(b.covers, k)
		}
		b.covers[key] = c
		b.coverBytes += len(c.data)
		b.mu.Unlock()
	}
	w.Header().Set("Content-Type", c.ct)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(c.data)
}
