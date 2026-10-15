package api

// Home fork: the Diary. What each person watched and when: read from what
// they finish in Jellyfin (or Emby), logged by hand, or imported from
// Letterboxd or Trakt (home_diary_import.go). One entry per title per day;
// a day's episodes of a show are one entry ("S2E3–S2E5"). Stars and a note
// are optional; stars count towards the person's taste. Wrapped
// (home_wrapped.go) is a year of it.

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/metadata"
)

const (
	diarySyncEvery  = 30 * time.Minute // the background job
	diaryFreshFor   = 10 * time.Minute // opening the Diary reads the media server again after this
	diaryPageImages = 60               // missing posters filled in per look
)

type diaryEntry struct {
	ID        int64  `json:"id"`
	Kind      string `json:"kind"`
	TMDBID    int    `json:"tmdbId"`
	Title     string `json:"title"`
	Year      int    `json:"year,omitempty"`
	Image     string `json:"image,omitempty"`
	WatchedOn string `json:"watchedOn"`
	Dated     bool   `json:"dated"`
	Episodes  int    `json:"episodes,omitempty"`
	Episode   string `json:"episode,omitempty"`
	Runtime   int    `json:"runtime,omitempty"`
	Stars     int    `json:"stars,omitempty"`
	Note      string `json:"note,omitempty"`
	Rewatch   bool   `json:"rewatch,omitempty"`
	Source    string `json:"source"`
}

// diaryPut adds an entry, or fills in one already there for that title and
// day: what it already has (stars, a note, a poster) is kept.
func (s *Server) diaryPut(ctx context.Context, uid int64, e diaryEntry) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO home_diary
		(user_id, kind, tmdb_id, title, year, image, watched_on, dated, episodes, episode, runtime, stars, note, rewatch, source)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (user_id, kind, tmdb_id, watched_on) DO UPDATE SET
			title = CASE WHEN home_diary.title = '' THEN excluded.title ELSE home_diary.title END,
			year = CASE WHEN home_diary.year = 0 THEN excluded.year ELSE home_diary.year END,
			image = CASE WHEN home_diary.image = '' THEN excluded.image ELSE home_diary.image END,
			episode = CASE WHEN excluded.episodes >= home_diary.episodes THEN excluded.episode ELSE home_diary.episode END,
			episodes = MAX(home_diary.episodes, excluded.episodes),
			runtime = MAX(home_diary.runtime, excluded.runtime),
			stars = CASE WHEN home_diary.stars = 0 THEN excluded.stars ELSE home_diary.stars END,
			note = CASE WHEN home_diary.note = '' THEN excluded.note ELSE home_diary.note END,
			dated = MAX(home_diary.dated, excluded.dated),
			rewatch = MAX(home_diary.rewatch, excluded.rewatch),
			hidden = CASE WHEN excluded.source = 'jellyfin' THEN home_diary.hidden ELSE 0 END`,
		uid, e.Kind, e.TMDBID, e.Title, e.Year, e.Image, e.WatchedOn, b2i(e.Dated), e.Episodes, e.Episode, e.Runtime, e.Stars, e.Note, b2i(e.Rewatch), e.Source)
	return err
}

// markRewatches flags each film watched again after an earlier, dated time.
func (s *Server) markRewatches(ctx context.Context, uid int64) {
	_, _ = s.db.ExecContext(ctx, `UPDATE home_diary SET rewatch = 1 WHERE user_id = ? AND kind = 'movie' AND dated = 1 AND rewatch = 0
		AND EXISTS (SELECT 1 FROM home_diary d WHERE d.user_id = home_diary.user_id AND d.kind = 'movie'
			AND d.tmdb_id = home_diary.tmdb_id AND d.dated = 1 AND d.watched_on < home_diary.watched_on)`, uid)
}

// ---- from the media server ----

// personOfUser is the media server user an account watches as: the one
// named like it; an administrator falls back to the server's administrator.
func personOfUser(people []jfPerson, u *auth.User) string {
	person := personMatch(people, u)
	if person == "" && u != nil && u.IsAdmin {
		person = personFor(people, u)
	}
	return person
}

type jfPlayed struct {
	ID             string            `json:"Id"`
	Name           string            `json:"Name"`
	ProductionYear int               `json:"ProductionYear"`
	SeriesID       string            `json:"SeriesId"`
	SeriesName     string            `json:"SeriesName"`
	Season         int               `json:"ParentIndexNumber"`
	Episode        int               `json:"IndexNumber"`
	RunTimeTicks   int64             `json:"RunTimeTicks"`
	ProviderIDs    map[string]string `json:"ProviderIds"`
	UserData       struct {
		LastPlayedDate string `json:"LastPlayedDate"`
	} `json:"UserData"`
}

func (it jfPlayed) day() string {
	t, err := time.Parse(time.RFC3339Nano, it.UserData.LastPlayedDate)
	if err != nil {
		return ""
	}
	return t.In(time.Local).Format("2006-01-02")
}

var diarySyncMu sync.Map // user id -> *sync.Mutex: one sync at a time per person

// syncDiary adds what the person finished in the media server since the
// last look (the whole history the first time).
func (s *Server) syncDiary(ctx context.Context, u *auth.User) error {
	mu, _ := diarySyncMu.LoadOrStore(u.ID, &sync.Mutex{})
	mu.(*sync.Mutex).Lock()
	defer mu.(*sync.Mutex).Unlock()
	srv, ok := s.jellyfinServer()
	if !ok {
		return nil
	}
	people, err := s.jfPeople(ctx)
	if err != nil {
		return err
	}
	person := personOfUser(people, u)
	if person == "" {
		return nil
	}
	base := "/Users/" + url.PathEscape(person) + "/Items?Recursive=true&Filters=IsPlayed&SortBy=DatePlayed&SortOrder=Descending&EnableImages=false&Fields=ProviderIds"
	// After the first look, only what changed since the last one (an hour's
	// margin for clocks).
	var last string
	_ = s.db.QueryRowContext(ctx, `SELECT synced_at FROM home_diary_sync WHERE user_id = ?`, u.ID).Scan(&last)
	if t, err := time.Parse(time.RFC3339, last); err == nil {
		base += "&MinDateLastSavedForUser=" + url.QueryEscape(t.Add(-time.Hour).UTC().Format(time.RFC3339))
	}
	var movies, eps struct {
		Items []jfPlayed `json:"Items"`
	}
	if err := jfJSON(ctx, srv, http.MethodGet, base+"&IncludeItemTypes=Movie&Limit=5000", nil, &movies); err != nil {
		return err
	}
	if err := jfJSON(ctx, srv, http.MethodGet, base+"&IncludeItemTypes=Episode&Limit=20000", nil, &eps); err != nil {
		return err
	}
	for _, it := range movies.Items {
		id := jfItem{ProviderIDs: it.ProviderIDs}.tmdbID()
		day := it.day()
		if id <= 0 || day == "" {
			continue
		}
		if err := s.diaryPut(ctx, u.ID, diaryEntry{Kind: "movie", TMDBID: id, Title: it.Name, Year: it.ProductionYear, WatchedOn: day, Dated: true, Runtime: int(it.RunTimeTicks / 600_000_000), Source: "jellyfin"}); err != nil {
			return err
		}
	}
	// Episodes: one entry per show per day.
	type showDay struct {
		series, day string
	}
	days := map[showDay][]jfPlayed{}
	var series []string
	seenSeries := map[string]bool{}
	for _, it := range eps.Items {
		day := it.day()
		if it.SeriesID == "" || day == "" {
			continue
		}
		days[showDay{it.SeriesID, day}] = append(days[showDay{it.SeriesID, day}], it)
		if !seenSeries[it.SeriesID] {
			seenSeries[it.SeriesID] = true
			series = append(series, it.SeriesID)
		}
	}
	tmdbOf := map[string]int{}
	yearOf := map[string]int{}
	for i := 0; i < len(series); i += 100 {
		var shows struct {
			Items []jfItem `json:"Items"`
		}
		batch := series[i:min(i+100, len(series))]
		if err := jfJSON(ctx, srv, http.MethodGet, "/Users/"+url.PathEscape(person)+"/Items?EnableImages=false&Fields=ProviderIds&Ids="+strings.Join(batch, ","), nil, &shows); err != nil {
			return err
		}
		for _, it := range shows.Items {
			tmdbOf[it.ID] = it.tmdbID()
			yearOf[it.ID] = it.ProductionYear
		}
	}
	for k, list := range days {
		id := tmdbOf[k.series]
		if id <= 0 {
			continue
		}
		sort.Slice(list, func(i, j int) bool {
			return list[i].Season < list[j].Season || list[i].Season == list[j].Season && list[i].Episode < list[j].Episode
		})
		var mins int64
		for _, e := range list {
			mins += e.RunTimeTicks
		}
		label := fmt.Sprintf("S%dE%d", list[0].Season, list[0].Episode)
		if last := list[len(list)-1]; len(list) > 1 {
			label += fmt.Sprintf("–S%dE%d", last.Season, last.Episode)
		}
		e := diaryEntry{Kind: "tv", TMDBID: id, Title: list[0].SeriesName, Year: yearOf[k.series], WatchedOn: k.day, Dated: true, Episodes: len(list), Episode: label, Runtime: int(mins / 600_000_000), Source: "jellyfin"}
		if err := s.diaryPut(ctx, u.ID, e); err != nil {
			return err
		}
	}
	s.markRewatches(ctx, u.ID)
	_, err = s.db.ExecContext(ctx, `INSERT INTO home_diary_sync (user_id, synced_at) VALUES (?, ?)
		ON CONFLICT (user_id) DO UPDATE SET synced_at = excluded.synced_at`, u.ID, time.Now().UTC().Format(time.RFC3339))
	tasteChanged(u.ID)
	return err
}

// diaryJob keeps everyone's Diary up to date with the media server.
func (s *Server) diaryJob(ctx context.Context) {
	if _, ok := s.jellyfinServer(); !ok {
		return
	}
	list, err := s.Auth.ListAccounts()
	if err != nil {
		return
	}
	for i := range list {
		if list[i].Disabled {
			continue
		}
		if err := s.syncDiary(ctx, &list[i].User); err != nil {
			slog.Info("diary: read the media server", "user", list[i].Username, "err", err)
		}
	}
}

// ---- reading and changing it ----

func (s *Server) diaryEntries(ctx context.Context, uid int64, where string, args ...any) ([]diaryEntry, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, kind, tmdb_id, title, year, image, watched_on, dated, episodes, episode, runtime, stars, note, rewatch, source
		FROM home_diary WHERE user_id = ? AND hidden = 0`+where+` ORDER BY watched_on DESC, id DESC`, append([]any{uid}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []diaryEntry{}
	for rows.Next() {
		var e diaryEntry
		var dated, rewatch int
		if err := rows.Scan(&e.ID, &e.Kind, &e.TMDBID, &e.Title, &e.Year, &e.Image, &e.WatchedOn, &dated, &e.Episodes, &e.Episode, &e.Runtime, &e.Stars, &e.Note, &rewatch, &e.Source); err != nil {
			return nil, err
		}
		e.Dated, e.Rewatch = dated == 1, rewatch == 1
		out = append(out, e)
	}
	return out, rows.Err()
}

// fillPosters finds the posters entries don't have yet (a few at a time),
// and keeps them.
func (s *Server) fillPosters(ctx context.Context, list []diaryEntry) {
	var todo []int
	for i, e := range list {
		if e.Image == "" && len(todo) < diaryPageImages {
			todo = append(todo, i)
		}
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, tasteParallel)
	for _, i := range todo {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			e := &list[i]
			title, year, poster := s.tmdbBasics(ctx, e.Kind, e.TMDBID)
			if poster == "" {
				return
			}
			e.Image = poster
			if e.Title == "" {
				e.Title, e.Year = title, year
			}
			_, _ = s.db.ExecContext(ctx, `UPDATE home_diary SET image = ?, title = CASE WHEN title = '' THEN ? ELSE title END,
				year = CASE WHEN year = 0 THEN ? ELSE year END WHERE kind = ? AND tmdb_id = ? AND image = ''`, poster, title, year, e.Kind, e.TMDBID)
		}()
	}
	wg.Wait()
}

// tmdbBasics is a film's or show's name, year and poster, in the page's
// language.
func (s *Server) tmdbBasics(ctx context.Context, kind string, id int) (string, int, string) {
	if kind == "tv" {
		d, err := s.displayTMDB().GetShow(ctx, id)
		if err != nil {
			return "", 0, ""
		}
		return d.Name, d.Year(), metadata.PosterURL(d.PosterPath)
	}
	m, err := s.displayTMDB().GetMovie(ctx, id)
	if err != nil {
		return "", 0, ""
	}
	return m.Title, m.Year(), metadata.PosterURL(m.PosterPath)
}

// GET /api/home/diary?year=&kind=: your Diary, newest first, and the years
// it covers. Reads the media server first when it hasn't for a while.
func (s *Server) handleDiary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := auth.UserFromContext(ctx)
	if u == nil {
		writeError(w, http.StatusUnauthorized, "Sign in first.")
		return
	}
	var last string
	_ = s.db.QueryRowContext(ctx, `SELECT synced_at FROM home_diary_sync WHERE user_id = ?`, u.ID).Scan(&last)
	if t, err := time.Parse(time.RFC3339, last); err != nil || time.Since(t) > diaryFreshFor {
		sctx, cancel := context.WithTimeout(ctx, 25*time.Second)
		if err := s.syncDiary(sctx, u); err != nil {
			slog.Info("diary: read the media server", "user", u.Username, "err", err)
		}
		cancel()
	}
	q := r.URL.Query()
	where, args := "", []any{}
	if y := q.Get("year"); len(y) == 4 {
		where += ` AND watched_on LIKE ?`
		args = append(args, y+"-%")
	}
	if k := q.Get("kind"); k == "movie" || k == "tv" {
		where += ` AND kind = ?`
		args = append(args, k)
	}
	list, err := s.diaryEntries(ctx, u.ID, where, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The Diary couldn't be read.")
		return
	}
	s.fillPosters(ctx, list)
	years := []string{}
	if rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT substr(watched_on, 1, 4) FROM home_diary WHERE user_id = ? ORDER BY 1 DESC`, u.ID); err == nil {
		for rows.Next() {
			var y string
			if rows.Scan(&y) == nil {
				years = append(years, y)
			}
		}
		rows.Close()
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": list, "years": years, "media": s.hasMediaServerPerson(ctx, u)})
}

func (s *Server) hasMediaServerPerson(ctx context.Context, u *auth.User) bool {
	if _, ok := s.jellyfinServer(); !ok {
		return false
	}
	people, err := s.jfPeople(ctx)
	return err == nil && personOfUser(people, u) != ""
}

// POST /api/home/diary {kind, tmdbId, watchedOn, stars, note, rewatch}: log
// something by hand.
func (s *Server) handleDiaryLog(w http.ResponseWriter, r *http.Request) {
	var in diaryEntry
	if err := decodeJSON(r, &in); err != nil || (in.Kind != "movie" && in.Kind != "tv") || in.TMDBID <= 0 || !validDay(in.WatchedOn) || in.Stars < 0 || in.Stars > 10 {
		writeError(w, http.StatusBadRequest, "That entry couldn't be read.")
		return
	}
	ctx := r.Context()
	in.Title, in.Year, in.Image = s.tmdbBasics(ctx, in.Kind, in.TMDBID)
	if in.Title == "" {
		writeError(w, http.StatusBadRequest, "TMDB doesn't know that title.")
		return
	}
	if in.Kind == "movie" {
		if d, err := s.displayTMDB().GetMovieDetail(ctx, in.TMDBID); err == nil {
			in.Runtime = d.Runtime
		}
	}
	in.Note = firstN(strings.TrimSpace(in.Note), 2000)
	in.Dated, in.Source = true, "manual"
	if err := s.diaryPut(ctx, userID(r), in); err != nil {
		writeError(w, http.StatusInternalServerError, "That couldn't be saved.")
		return
	}
	s.markRewatches(ctx, userID(r))
	tasteChanged(userID(r))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func validDay(d string) bool {
	t, err := time.Parse("2006-01-02", d)
	return err == nil && t.Year() > 1900 && !t.After(time.Now().AddDate(0, 0, 1))
}

// PUT /api/home/diary/{id} {stars, note, rewatch}: rate, write a note.
func (s *Server) handleDiaryEdit(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var in struct {
		Stars   int    `json:"stars"`
		Note    string `json:"note"`
		Rewatch bool   `json:"rewatch"`
	}
	if err != nil || decodeJSON(r, &in) != nil || in.Stars < 0 || in.Stars > 10 {
		writeError(w, http.StatusBadRequest, "That change couldn't be read.")
		return
	}
	res, err := s.db.ExecContext(r.Context(), `UPDATE home_diary SET stars = ?, note = ?, rewatch = ? WHERE id = ? AND user_id = ? AND hidden = 0`,
		in.Stars, firstN(strings.TrimSpace(in.Note), 2000), b2i(in.Rewatch), id, userID(r))
	if n, _ := rowsAffected(res, err); n == 0 {
		writeError(w, http.StatusNotFound, "That entry isn't in your Diary.")
		return
	}
	tasteChanged(userID(r))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// DELETE /api/home/diary/{id}: take an entry out. One from the media server
// is only hidden, or the next look would bring it back.
func (s *Server) handleDiaryDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That entry couldn't be read.")
		return
	}
	res, err := s.db.ExecContext(r.Context(), `UPDATE home_diary SET hidden = 1, stars = 0, note = '' WHERE id = ? AND user_id = ? AND source = 'jellyfin'`, id, userID(r))
	if n, _ := rowsAffected(res, err); n == 0 {
		res, err = s.db.ExecContext(r.Context(), `DELETE FROM home_diary WHERE id = ? AND user_id = ?`, id, userID(r))
	}
	if n, _ := rowsAffected(res, err); n == 0 {
		writeError(w, http.StatusNotFound, "That entry isn't in your Diary.")
		return
	}
	tasteChanged(userID(r))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func rowsAffected(res sql.Result, err error) (int64, error) {
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// diaryStars is the stars a person gave each title (the latest), for their
// taste.
func (s *Server) diaryStars(ctx context.Context, uid int64, kind string) map[int]diaryEntry {
	out := map[int]diaryEntry{}
	list, err := s.diaryEntries(ctx, uid, ` AND kind = ? AND stars > 0`, kind)
	if err != nil {
		return out
	}
	for _, e := range list {
		if _, ok := out[e.TMDBID]; !ok {
			out[e.TMDBID] = e
		}
	}
	return out
}
