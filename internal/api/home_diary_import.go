package api

// Home fork: bringing a Diary in from elsewhere.
//   - Letterboxd: the export (Settings → Import & Export → Export your
//     data), the whole zip or one of its CSV files. The diary becomes Diary
//     entries with their stars, rewatches and reviews; films only rated or
//     marked watched come in without a watch date (they count for taste,
//     not for Wrapped); liked films become thumbs up. Letterboxd has no
//     TMDB ids, so each film is found on TMDB by its name and year; the
//     ones that can't be are listed.
//   - Trakt: a public profile's watch history (films, and episodes as one
//     entry per show per day) and its ratings, by user name.
// An import runs in the background; the Diary shows how far it got.

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/internal/trakt"
)

const maxImportUpload = 25 << 20

type diaryImport struct {
	Running   bool     `json:"running"`
	Source    string   `json:"source,omitempty"`
	Done      int      `json:"done"`
	Total     int      `json:"total"`
	Added     int      `json:"added"`
	Unmatched []string `json:"unmatched,omitempty"` // up to 30 names TMDB couldn't place
	Missed    int      `json:"missed,omitempty"`    // how many in all
	Error     string   `json:"error,omitempty"`
	Finished  string   `json:"finished,omitempty"`
}

var diaryImports = struct {
	sync.Mutex
	m map[int64]*diaryImport
}{m: map[int64]*diaryImport{}}

func diaryImportOf(uid int64) diaryImport {
	diaryImports.Lock()
	defer diaryImports.Unlock()
	if st, ok := diaryImports.m[uid]; ok {
		return *st
	}
	return diaryImport{}
}

// startDiaryImport sets an import going for a person, unless one already is.
func startDiaryImport(uid int64, source string, run func(ctx context.Context, st *diaryImport)) bool {
	diaryImports.Lock()
	if st, ok := diaryImports.m[uid]; ok && st.Running {
		diaryImports.Unlock()
		return false
	}
	st := &diaryImport{Running: true, Source: source}
	diaryImports.m[uid] = st
	diaryImports.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
		defer cancel()
		run(ctx, st)
		diaryImports.Lock()
		st.Running, st.Finished = false, time.Now().UTC().Format(time.RFC3339)
		diaryImports.Unlock()
		tasteChanged(uid)
	}()
	return true
}

// importProgress updates an import's state under the lock.
func importProgress(st *diaryImport, f func(st *diaryImport)) {
	diaryImports.Lock()
	f(st)
	diaryImports.Unlock()
}

// GET /api/home/diary/import: how the latest import is getting on.
func (s *Server) handleDiaryImportState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, diaryImportOf(userID(r)))
}

// ---- Letterboxd ----

type lbFilm struct {
	Name    string
	Year    int
	Date    string // the day it was logged, rated or liked
	Watched string // diary only: the day it was watched
	Rating  float64
	Rewatch bool
	Review  string
	URI     string
}

type lbExport struct {
	diary, reviews, ratings, watched, likes []lbFilm
}

// readLetterboxd reads the export zip, or one CSV from it (which one, by its
// columns).
func readLetterboxd(name string, data []byte) (*lbExport, error) {
	out := &lbExport{}
	if strings.HasSuffix(strings.ToLower(name), ".zip") || bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, errors.New("That zip couldn't be opened.")
		}
		for _, f := range zr.File {
			base := strings.ToLower(path.Base(f.Name))
			dir := strings.ToLower(path.Base(path.Dir(f.Name)))
			var into *[]lbFilm
			switch {
			case base == "diary.csv" && dir != "deleted":
				into = &out.diary
			case base == "reviews.csv" && dir != "deleted":
				into = &out.reviews
			case base == "ratings.csv":
				into = &out.ratings
			case base == "watched.csv":
				into = &out.watched
			case base == "films.csv" && dir == "likes":
				into = &out.likes
			default:
				continue
			}
			rc, err := f.Open()
			if err != nil {
				continue
			}
			b, _ := io.ReadAll(io.LimitReader(rc, maxImportUpload))
			rc.Close()
			films, _, err := readLetterboxdCSV(b)
			if err == nil {
				*into = append(*into, films...)
			}
		}
	} else {
		films, cols, err := readLetterboxdCSV(data)
		if err != nil {
			return nil, err
		}
		switch {
		case cols["review"]:
			out.reviews = films
		case cols["watched date"]:
			out.diary = films
		case cols["rating"]:
			out.ratings = films
		default:
			out.watched = films
		}
	}
	if len(out.diary)+len(out.reviews)+len(out.ratings)+len(out.watched)+len(out.likes) == 0 {
		return nil, errors.New("No films were found in that file. Choose the zip Letterboxd exports, or its diary.csv.")
	}
	return out, nil
}

func readLetterboxdCSV(data []byte) ([]lbFilm, map[string]bool, error) {
	r := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))))
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil || len(rows) == 0 {
		return nil, nil, errors.New("That file isn't a CSV from Letterboxd.")
	}
	at := map[string]int{}
	cols := map[string]bool{}
	for i, h := range rows[0] {
		h = strings.ToLower(strings.TrimSpace(h))
		at[h], cols[h] = i, true
	}
	if !cols["name"] || !cols["year"] {
		return nil, nil, errors.New("That file isn't a CSV from Letterboxd.")
	}
	get := func(row []string, col string) string {
		if i, ok := at[col]; ok && i < len(row) {
			return strings.TrimSpace(row[i])
		}
		return ""
	}
	var out []lbFilm
	for _, row := range rows[1:] {
		f := lbFilm{Name: get(row, "name"), Date: get(row, "date"), Watched: get(row, "watched date"), Review: get(row, "review"), URI: get(row, "letterboxd uri")}
		f.Year, _ = strconv.Atoi(get(row, "year"))
		f.Rating, _ = strconv.ParseFloat(get(row, "rating"), 64)
		f.Rewatch = strings.EqualFold(get(row, "rewatch"), "yes")
		if f.Name != "" {
			out = append(out, f)
		}
	}
	return out, cols, nil
}

// POST /api/home/diary/import/letterboxd (a file, as "file"): bring a
// Letterboxd export in.
func (s *Server) handleDiaryImportLetterboxd(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxImportUpload)
	file, hdr, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Choose the file Letterboxd exported (25 MB at most).")
		return
	}
	data, err := io.ReadAll(file)
	file.Close()
	if err != nil {
		writeError(w, http.StatusBadRequest, "That file couldn't be read.")
		return
	}
	exp, err := readLetterboxd(hdr.Filename, data)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.TMDB().HasAPIKey() {
		writeError(w, http.StatusBadRequest, "Mediarium needs its TMDB key to find the films.")
		return
	}
	uid := userID(r)
	if !startDiaryImport(uid, "letterboxd", func(ctx context.Context, st *diaryImport) { s.importLetterboxd(ctx, uid, exp, st) }) {
		writeError(w, http.StatusConflict, "An import is already running.")
		return
	}
	writeJSON(w, http.StatusAccepted, diaryImportOf(uid))
}

type lbKey struct {
	name string
	year int
}

func (s *Server) importLetterboxd(ctx context.Context, uid int64, exp *lbExport, st *diaryImport) {
	// Every film once, found on TMDB.
	keys := map[lbKey]bool{}
	for _, l := range [][]lbFilm{exp.diary, exp.reviews, exp.ratings, exp.watched, exp.likes} {
		for _, f := range l {
			keys[lbKey{f.Name, f.Year}] = true
		}
	}
	importProgress(st, func(st *diaryImport) { st.Total = len(keys) })
	found := s.findFilms(ctx, keys, st)

	add := func(e diaryEntry) {
		if err := s.diaryPut(ctx, uid, e); err == nil {
			importProgress(st, func(st *diaryImport) { st.Added++ })
		}
	}
	stars := func(r float64) int { return int(math.Round(r * 2)) }
	logged := map[int]bool{}
	reviews := map[string]string{} // by film and day, or by Letterboxd address
	for _, f := range exp.reviews {
		if f.Review != "" {
			reviews[f.URI] = f.Review
			reviews[fmt.Sprintf("%s|%d|%s", f.Name, f.Year, firstDay(f.Watched, f.Date))] = f.Review
		}
	}
	// The diary (and reviews, which are diary entries with words).
	for _, l := range [][]lbFilm{exp.diary, exp.reviews} {
		for _, f := range l {
			id := found[lbKey{f.Name, f.Year}]
			day := firstDay(f.Watched, f.Date)
			if id == 0 || !validDay(day) {
				continue
			}
			note := reviews[f.URI]
			if note == "" {
				note = reviews[fmt.Sprintf("%s|%d|%s", f.Name, f.Year, day)]
			}
			logged[id] = true
			add(diaryEntry{Kind: "movie", TMDBID: id, Title: f.Name, Year: f.Year, WatchedOn: day, Dated: true, Stars: stars(f.Rating), Rewatch: f.Rewatch, Note: firstN(note, 2000), Source: "letterboxd"})
		}
	}
	// Rated or watched without a diary entry: no watch date.
	for _, l := range [][]lbFilm{exp.ratings, exp.watched} {
		for _, f := range l {
			id := found[lbKey{f.Name, f.Year}]
			if id == 0 || logged[id] || !validDay(f.Date) {
				continue
			}
			logged[id] = true
			add(diaryEntry{Kind: "movie", TMDBID: id, Title: f.Name, Year: f.Year, WatchedOn: f.Date, Dated: false, Stars: stars(f.Rating), Source: "letterboxd"})
		}
	}
	// Liked films: a thumbs up, unless there's a vote already.
	for _, f := range exp.likes {
		if id := found[lbKey{f.Name, f.Year}]; id > 0 {
			_, _ = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO home_votes (user_id, kind, ref, vote, title, year) VALUES (?, 'movie', ?, 1, ?, ?)`,
				uid, strconv.Itoa(id), f.Name, f.Year)
		}
	}
	s.fillRuntimes(ctx, uid)
	s.markRewatches(ctx, uid)
}

func firstDay(days ...string) string {
	for _, d := range days {
		if d != "" {
			return d
		}
	}
	return ""
}

// findFilms finds each film on TMDB by its name and year: the result with
// the same name and year, else the same year, else a year either side.
// Ones it can't place are noted on the import.
func (s *Server) findFilms(ctx context.Context, keys map[lbKey]bool, st *diaryImport) map[lbKey]int {
	out := map[lbKey]int{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for k := range keys {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			id := 0
			if res, err := s.TMDB().SearchMovies(ctx, k.name); err == nil {
				id = pickFilm(res, k.name, k.year)
			}
			mu.Lock()
			if id > 0 {
				out[k] = id
			}
			mu.Unlock()
			importProgress(st, func(st *diaryImport) {
				st.Done++
				if id == 0 {
					st.Missed++
					if len(st.Unmatched) < 30 {
						name := k.name
						if k.year > 0 {
							name += fmt.Sprintf(" (%d)", k.year)
						}
						st.Unmatched = append(st.Unmatched, name)
					}
				}
			})
		}()
	}
	wg.Wait()
	return out
}

func pickFilm(res []metadata.Movie, name string, year int) int {
	norm := func(s string) string {
		var b strings.Builder
		for _, r := range strings.ToLower(s) {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				b.WriteRune(r)
			}
		}
		return b.String()
	}
	best, bestScore := 0, 0
	for _, m := range res {
		score := 0
		switch d := m.Year() - year; {
		case year == 0:
			score = 1
		case d == 0:
			score = 3
		case d == 1 || d == -1:
			score = 1
		default:
			continue
		}
		if norm(m.Title) == norm(name) || norm(m.OriginalTitle) == norm(name) {
			score += 2
		}
		if score > bestScore {
			best, bestScore = m.TMDBID, score
		}
	}
	return best
}

// fillRuntimes looks up how long the dated films without a running time
// are, for Wrapped's hours.
func (s *Server) fillRuntimes(ctx context.Context, uid int64) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT tmdb_id FROM home_diary WHERE user_id = ? AND kind = 'movie' AND runtime = 0 AND dated = 1`, uid)
	if err != nil {
		return
	}
	var ids []int
	for rows.Next() {
		var id int
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for _, id := range ids {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if d, err := s.TMDB().GetMovieDetail(ctx, id); err == nil && d.Runtime > 0 {
				_, _ = s.db.ExecContext(ctx, `UPDATE home_diary SET runtime = ? WHERE user_id = ? AND kind = 'movie' AND tmdb_id = ? AND runtime = 0`, d.Runtime, uid, id)
			}
		}()
	}
	wg.Wait()
}

// ---- Trakt ----

// POST /api/home/diary/import/trakt {"user": "name"}: a public Trakt
// profile's history and ratings.
func (s *Server) handleDiaryImportTrakt(w http.ResponseWriter, r *http.Request) {
	var in struct {
		User string `json:"user"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	user := strings.TrimSpace(in.User)
	if i := strings.Index(user, "trakt.tv/users/"); i >= 0 { // a profile's address
		user = strings.SplitN(user[i+len("trakt.tv/users/"):], "/", 2)[0]
	}
	if user == "" || len(user) > 80 || strings.ContainsAny(user, " ?#") {
		writeError(w, http.StatusBadRequest, "Type your Trakt user name.")
		return
	}
	if !s.Trakt().HasClientID() {
		writeError(w, http.StatusBadRequest, "Trakt isn't set up: an administrator can add its key under Settings → Info, lists and subtitles → Import lists.")
		return
	}
	uid := userID(r)
	if !startDiaryImport(uid, "trakt", func(ctx context.Context, st *diaryImport) { s.importTrakt(ctx, uid, user, st) }) {
		writeError(w, http.StatusConflict, "An import is already running.")
		return
	}
	writeJSON(w, http.StatusAccepted, diaryImportOf(uid))
}

func (s *Server) importTrakt(ctx context.Context, uid int64, user string, st *diaryImport) {
	fail := func(err error) {
		msg := "Trakt couldn't be read: " + err.Error()
		switch {
		case errors.Is(err, trakt.ErrPrivate):
			msg = "That Trakt profile is private. Make it public in Trakt's settings, import, then make it private again if you like."
		case errors.Is(err, trakt.ErrNotFound):
			msg = "Trakt has no one by that name."
		}
		importProgress(st, func(st *diaryImport) { st.Error = msg })
		slog.Info("diary: trakt import", "err", err)
	}
	history, err := s.Trakt().UserHistory(ctx, user)
	if err != nil {
		fail(err)
		return
	}
	ratings, err := s.Trakt().UserRatings(ctx, user)
	if err != nil {
		fail(err)
		return
	}
	importProgress(st, func(st *diaryImport) { st.Total = len(history) + len(ratings) })
	day := func(t time.Time) string { return t.In(time.Local).Format("2006-01-02") }
	add := func(e diaryEntry) {
		if err := s.diaryPut(ctx, uid, e); err == nil {
			importProgress(st, func(st *diaryImport) { st.Added++ })
		}
	}
	type showDay struct {
		id  int
		day string
	}
	shows := map[showDay][]trakt.Watched{}
	for _, h := range history {
		importProgress(st, func(st *diaryImport) { st.Done++ })
		if h.Kind == "movie" {
			add(diaryEntry{Kind: "movie", TMDBID: h.TMDBID, Title: h.Title, Year: h.Year, WatchedOn: day(h.WatchedAt), Dated: true, Source: "trakt"})
			continue
		}
		k := showDay{h.TMDBID, day(h.WatchedAt)}
		shows[k] = append(shows[k], h)
	}
	epLength := map[int]int{}
	for k, list := range shows {
		sort.Slice(list, func(i, j int) bool {
			return list[i].Season < list[j].Season || list[i].Season == list[j].Season && list[i].Episode < list[j].Episode
		})
		label := fmt.Sprintf("S%dE%d", list[0].Season, list[0].Episode)
		if last := list[len(list)-1]; len(list) > 1 {
			label += fmt.Sprintf("–S%dE%d", last.Season, last.Episode)
		}
		if _, ok := epLength[k.id]; !ok {
			epLength[k.id] = 0
			if d, err := s.TMDB().GetShow(ctx, k.id); err == nil && len(d.EpisodeRunTime) > 0 {
				epLength[k.id] = d.EpisodeRunTime[0]
			}
		}
		add(diaryEntry{Kind: "tv", TMDBID: k.id, Title: list[0].Title, Year: list[0].Year, WatchedOn: k.day, Dated: true, Episodes: len(list), Episode: label, Runtime: epLength[k.id] * len(list), Source: "trakt"})
	}
	// Ratings: on the latest entry for the title, else an entry without a
	// watch date.
	for _, rt := range ratings {
		importProgress(st, func(st *diaryImport) { st.Done++ })
		res, err := s.db.ExecContext(ctx, `UPDATE home_diary SET stars = ? WHERE id = (SELECT id FROM home_diary WHERE user_id = ? AND kind = ? AND tmdb_id = ? AND hidden = 0
			ORDER BY watched_on DESC LIMIT 1) AND stars = 0`, rt.Rating, uid, rt.Kind, rt.TMDBID)
		if n, _ := rowsAffected(res, err); n > 0 {
			continue
		}
		var has int
		_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM home_diary WHERE user_id = ? AND kind = ? AND tmdb_id = ?`, uid, rt.Kind, rt.TMDBID).Scan(&has)
		if has == 0 {
			add(diaryEntry{Kind: rt.Kind, TMDBID: rt.TMDBID, Title: rt.Title, Year: rt.Year, WatchedOn: day(rt.RatedAt), Dated: false, Stars: rt.Rating, Source: "trakt"})
		}
	}
	s.fillRuntimes(ctx, uid)
	s.markRewatches(ctx, uid)
}
