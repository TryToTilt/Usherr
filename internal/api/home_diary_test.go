package api

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/internal/store"
)

func diaryServer(t *testing.T) *Server {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`INSERT INTO users (username, password_hash) VALUES ('alex', 'x')`); err != nil {
		t.Fatal(err)
	}
	return &Server{db: db, tmdb: metadata.NewWithBaseURL("", "http://127.0.0.1:1")}
}

func TestDiaryKeepsWhatItHas(t *testing.T) {
	s := diaryServer(t)
	ctx := t.Context()
	put := func(e diaryEntry) {
		t.Helper()
		if err := s.diaryPut(ctx, 1, e); err != nil {
			t.Fatal(err)
		}
	}
	// From the media server, then rated from Letterboxd the same day, then a
	// later import that disagrees: the first stars stay.
	put(diaryEntry{Kind: "movie", TMDBID: 603, Title: "The Matrix", WatchedOn: "2026-03-01", Dated: true, Runtime: 136, Source: "jellyfin"})
	put(diaryEntry{Kind: "movie", TMDBID: 603, Title: "Matrix", WatchedOn: "2026-03-01", Dated: true, Stars: 9, Note: "Still great", Source: "letterboxd"})
	put(diaryEntry{Kind: "movie", TMDBID: 603, WatchedOn: "2026-03-01", Dated: true, Stars: 4, Source: "trakt"})
	put(diaryEntry{Kind: "movie", TMDBID: 603, Title: "The Matrix", WatchedOn: "2026-09-20", Dated: true, Source: "jellyfin"})
	s.markRewatches(ctx, 1)
	list, err := s.diaryEntries(ctx, 1, "")
	if err != nil || len(list) != 2 {
		t.Fatalf("entries: %v %v", list, err)
	}
	later, first := list[0], list[1]
	if first.Title != "The Matrix" || first.Stars != 9 || first.Note != "Still great" || first.Runtime != 136 || first.Source != "jellyfin" || first.Rewatch {
		t.Fatalf("the first watch: %+v", first)
	}
	if !later.Rewatch {
		t.Fatal("watching it again later isn't a rewatch")
	}

	// Taking out an entry from the media server hides it; the next look
	// doesn't bring it back, logging it by hand does.
	w := httptest.NewRecorder()
	req := withUser(httptest.NewRequest(http.MethodDelete, "/", nil), 1)
	req.SetPathValue("id", strconv.FormatInt(later.ID, 10))
	s.handleDiaryDelete(w, req)
	put(diaryEntry{Kind: "movie", TMDBID: 603, WatchedOn: "2026-09-20", Dated: true, Source: "jellyfin"})
	if list, _ := s.diaryEntries(ctx, 1, ""); len(list) != 1 {
		t.Fatalf("a hidden entry came back: %d entries", len(list))
	}
	put(diaryEntry{Kind: "movie", TMDBID: 603, WatchedOn: "2026-09-20", Dated: true, Source: "manual"})
	if list, _ := s.diaryEntries(ctx, 1, ""); len(list) != 2 {
		t.Fatal("logging it by hand didn't bring it back")
	}
	// Another person's entry can't be changed.
	w = httptest.NewRecorder()
	req = withUser(httptest.NewRequest(http.MethodDelete, "/", nil), 2)
	req.SetPathValue("id", strconv.FormatInt(first.ID, 10))
	s.handleDiaryDelete(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("someone else's entry: %d", w.Code)
	}
}

const lbDiary = "Date,Name,Year,Letterboxd URI,Rating,Rewatch,Tags,Watched Date\n2026-02-03,Heat,1995,https://boxd.it/a,4.5,,,2026-02-01\n2026-02-10,Heat,1995,https://boxd.it/b,5,Yes,,2026-02-09\n"

func TestReadLetterboxd(t *testing.T) {
	exp, err := readLetterboxd("diary.csv", []byte("\xef\xbb\xbf"+lbDiary))
	if err != nil || len(exp.diary) != 2 {
		t.Fatalf("diary.csv: %v %+v", err, exp)
	}
	if f := exp.diary[1]; f.Name != "Heat" || f.Year != 1995 || f.Rating != 5 || !f.Rewatch || f.Watched != "2026-02-09" {
		t.Fatalf("a diary row: %+v", f)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range map[string]string{
		"diary.csv":         lbDiary,
		"ratings.csv":       "Date,Name,Year,Letterboxd URI,Rating\n2025-01-01,Alien,1979,https://boxd.it/c,4\n",
		"likes/films.csv":   "Date,Name,Year,Letterboxd URI\n2025-01-01,Alien,1979,https://boxd.it/c\n",
		"deleted/diary.csv": lbDiary,
	} {
		f, _ := zw.Create(name)
		_, _ = f.Write([]byte(body))
	}
	_ = zw.Close()
	exp, err = readLetterboxd("letterboxd-export.zip", buf.Bytes())
	if err != nil || len(exp.diary) != 2 || len(exp.ratings) != 1 || len(exp.likes) != 1 {
		t.Fatalf("the zip: %v %d diary, %d ratings, %d likes", err, len(exp.diary), len(exp.ratings), len(exp.likes))
	}
	if _, err := readLetterboxd("notes.csv", []byte("a,b\n1,2\n")); err == nil {
		t.Fatal("a CSV that isn't Letterboxd's was read")
	}
}

func TestPickFilm(t *testing.T) {
	m := func(id int, title, date string) metadata.Movie {
		return metadata.Movie{TMDBID: id, Title: title, ReleaseDate: date}
	}
	res := []metadata.Movie{m(1, "Heat", "1986-01-01"), m(2, "Heat", "1995-12-15"), m(3, "Heatwave", "1995-05-01")}
	if got := pickFilm(res, "Heat", 1995); got != 2 {
		t.Fatalf("Heat (1995): %d", got)
	}
	if got := pickFilm(res, "Heat", 2010); got != 0 {
		t.Fatalf("no film from that year: %d", got)
	}
}

func TestWrapped(t *testing.T) {
	s := diaryServer(t)
	ctx := t.Context()
	img := "https://image.tmdb.org/x.jpg"
	for _, e := range []diaryEntry{
		{Kind: "movie", TMDBID: 1, Title: "Old", WatchedOn: "2025-12-31", Dated: true, Image: img, Runtime: 100},
		{Kind: "movie", TMDBID: 1, Title: "Old", WatchedOn: "2026-01-05", Dated: true, Image: img, Runtime: 100, Stars: 8},
		{Kind: "movie", TMDBID: 2, Title: "New", WatchedOn: "2026-01-06", Dated: true, Image: img, Stars: 10},
		{Kind: "tv", TMDBID: 9, Title: "Show", WatchedOn: "2026-01-07", Dated: true, Image: img, Episodes: 3, Runtime: 90},
		{Kind: "movie", TMDBID: 3, Title: "Rated only", WatchedOn: "2026-05-01", Dated: false, Image: img, Stars: 2},
	} {
		if err := s.diaryPut(ctx, 1, e); err != nil {
			t.Fatal(err)
		}
	}
	all, _ := s.diaryEntries(ctx, 1, ` AND dated = 1`)
	w := s.wrapped(ctx, all, 2026)
	if w.Films != 2 || w.NewFilms != 1 || w.Rewatches != 1 || w.Episodes != 3 || w.Shows != 1 {
		t.Fatalf("counts: %+v", w)
	}
	if w.Minutes != 100+typicalFilm+90 || !w.About || w.Months[0] != w.Minutes {
		t.Fatalf("minutes: %d about %v months %v", w.Minutes, w.About, w.Months)
	}
	if w.Streak != 3 || w.Days != 3 || w.First.Title != "Old" || w.Last.Title != "New" {
		t.Fatalf("days: streak %d, %d days, first %v, last %v", w.Streak, w.Days, w.First, w.Last)
	}
	if len(w.TopRated) != 2 || w.TopRated[0].Title != "New" || len(w.TopShows) != 1 || w.TopShows[0].Episodes != 3 {
		t.Fatalf("tops: %+v %+v", w.TopRated, w.TopShows)
	}
	if len(w.Years) != 2 || w.Years[0] != 2026 {
		t.Fatalf("years: %v", w.Years)
	}
	if e := s.wrapped(ctx, all, 2019); !e.Empty {
		t.Fatal("a year with nothing isn't empty")
	}
}
