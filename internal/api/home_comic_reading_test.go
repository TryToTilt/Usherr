package api

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/comics"
	"github.com/rdborg/mediarium/internal/config"
	"github.com/rdborg/mediarium/internal/settings"
)

func comicUser(t *testing.T, s *Server, name string) int64 {
	t.Helper()
	res, err := s.db.Exec(`INSERT INTO users (username, password_hash) VALUES (?, 'x')`, name)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func comicReq(method, path, body string, user int64) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	return r.WithContext(auth.WithUser(r.Context(), &auth.User{ID: user}))
}

// fakeComicVine answers for one volume, counting the requests; nothing reaches
// the real ComicVine.
func fakeComicVine(t *testing.T, s *Server, volume int64, name string, issues []string) *int {
	t.Helper()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if strings.HasPrefix(r.URL.Path, fmt.Sprintf("/volume/4050-%d", volume)) {
			fmt.Fprintf(w, `{"status_code":1,"results":{"id":%d,"name":%q,"start_year":"2012","count_of_issues":%d,"publisher":{"name":"Image"}}}`, volume, name, len(issues))
			return
		}
		list := make([]string, 0, len(issues))
		for k, n := range issues {
			list = append(list, fmt.Sprintf(`{"id":%d,"issue_number":%q,"name":"Chapter %s"}`, volume*100+int64(k), n, n))
		}
		fmt.Fprintf(w, `{"status_code":1,"number_of_total_results":%d,"results":[%s]}`, len(issues), strings.Join(list, ","))
	}))
	t.Cleanup(srv.Close)
	cv := s.comicVine()
	cv.Base = srv.URL
	cv.Key = func() string { return "k" }
	cv.Gap = 0
	return &calls
}

func TestComicFixMatch(t *testing.T) {
	s, repo := comicTestServer(t)
	user := comicUser(t, s, "sam")
	folder := filepath.Join(t.TempDir(), "Wrong Name", "Volume 01 (1990)")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	sr, _ := repo.UpsertSeries(comics.Series{CVID: 1, Name: "Wrong Name", StartYear: 1990, Folder: folder, MonitorNew: true})
	for _, n := range []string{"1", "2", "3", "4"} {
		_, _ = repo.UpsertIssue(comics.Issue{SeriesID: sr.ID, CVID: 10 + int64(n[0]-'0'), Number: n})
	}
	before, _ := repo.Issues(sr.ID)
	one, two, three, four := before[0], before[1], before[2], before[3]
	files := map[int64]string{one.ID: filepath.Join(folder, "Wrong Name 001.cbz"), three.ID: filepath.Join(folder, "Wrong Name 003.cbz")}
	for id, f := range files {
		if err := os.WriteFile(f, []byte("comic"), 0o644); err != nil {
			t.Fatal(err)
		}
		_ = repo.SetState(id, comics.StatusDownloaded, f, 5)
	}
	_ = repo.MarkRead(user, []int64{one.ID}, true, time.Now())
	if _, err := s.db.Exec(`INSERT INTO home_positions (user_id, item, position, duration, updated_ms) VALUES (?, ?, 4, 20, 1)`, user, fmt.Sprintf("comic:%d", three.ID)); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetSeriesType(sr.ID, comics.TypeManga); err != nil {
		t.Fatal(err)
	}
	// #1 and #2 are in the new volume, and #5 is new. #3 has a file but no match; #4 has neither.
	fakeComicVine(t, s, 7, "Right Name", []string{"1", "2", "5"})

	post := func(path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := comicReq(http.MethodPost, path, body, user)
		r.SetPathValue("id", fmt.Sprint(sr.ID))
		if strings.HasSuffix(path, "/preview") {
			s.handleComicMatchPreview(w, r)
		} else {
			s.handleComicMatch(w, r)
		}
		return w
	}

	// The preview tells and changes nothing.
	w := post("/api/comics/1/match/preview", `{"cvId":7}`)
	if w.Code != http.StatusOK {
		t.Fatalf("preview: %d %s", w.Code, w.Body)
	}
	var pv struct {
		Series  comics.Series `json:"series"`
		Files   int           `json:"files"`
		NoMatch []string      `json:"noMatch"`
		Added   int           `json:"added"`
		Removed int           `json:"removed"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &pv); err != nil {
		t.Fatal(err)
	}
	if pv.Series.Name != "Right Name" || pv.Files != 1 || len(pv.NoMatch) != 1 || pv.NoMatch[0] != "3" || pv.Added != 1 || pv.Removed != 1 {
		t.Errorf("preview: %+v", pv)
	}
	if got, _ := repo.GetSeries(sr.ID); got.CVID != 1 {
		t.Fatal("the preview changed the series")
	}

	// Choosing it.
	if w := post("/api/comics/1/match", `{"cvId":7}`); w.Code != http.StatusOK {
		t.Fatalf("match: %d %s", w.Code, w.Body)
	}
	got, _ := repo.GetSeries(sr.ID)
	if got.CVID != 7 || got.Name != "Right Name" || got.StartYear != 2012 {
		t.Errorf("series: %+v", got)
	}
	if got.Folder != folder || got.Type != comics.TypeManga {
		t.Errorf("the folder and type should stay: %+v", got)
	}
	for id, f := range files {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("a file moved: %v", err)
		}
		is, err := repo.GetIssue(id)
		if err != nil || is.FilePath != f || is.Status != comics.StatusDownloaded {
			t.Errorf("issue %d lost its file: %+v %v", id, is, err)
		}
	}
	if is, _ := repo.GetIssue(one.ID); is.CVID != 700 || is.Title != "Chapter 1" {
		t.Errorf("#1 should take the new volume's issue: %+v", is)
	}
	if is, _ := repo.GetIssue(two.ID); is.CVID != 701 {
		t.Errorf("#2 should take the new volume's issue: %+v", is)
	}
	if is, _ := repo.GetIssue(three.ID); is.CVID >= 0 {
		t.Errorf("#3 has no match and should lose its old id: %+v", is)
	}
	if _, err := repo.GetIssue(four.ID); err == nil {
		t.Error("#4 had no file and no match, and should have gone")
	}
	after, _ := repo.Issues(sr.ID)
	if len(after) != 4 || after[3].Number != "5" || after[3].Wanted {
		t.Errorf("issues: %+v", after)
	}
	// Read state and the place stay with the issue.
	s2 := got
	if err := repo.FillReading(user, &s2, after); err != nil {
		t.Fatal(err)
	}
	if !after[0].Read || after[2].Page != 4 {
		t.Errorf("reading lost: %+v", after)
	}

	// Pointing at what it already is, or at another series here, is refused and costs no call.
	if w := post("/api/comics/1/match", `{"cvId":7}`); w.Code != http.StatusBadRequest {
		t.Errorf("same volume: %d", w.Code)
	}
	_, _ = repo.UpsertSeries(comics.Series{CVID: 8, Name: "Another"})
	if w := post("/api/comics/1/match", `{"cvId":8}`); w.Code != http.StatusConflict {
		t.Errorf("a volume already here: %d", w.Code)
	}
}

func TestComicReadToTheEndIsRead(t *testing.T) {
	s, repo := comicTestServer(t)
	user := comicUser(t, s, "sam")
	sr, _ := repo.UpsertSeries(comics.Series{CVID: 1, Name: "Saga"})
	_, _ = repo.UpsertIssue(comics.Issue{SeriesID: sr.ID, CVID: 10, Number: "1"})
	list, _ := repo.Issues(sr.ID)
	id := list[0].ID
	item := fmt.Sprintf("comic:%d", id)
	put := func(page, total int) {
		t.Helper()
		w := httptest.NewRecorder()
		s.handlePositionPut(w, comicReq(http.MethodPut, "/api/home/position", fmt.Sprintf(`{"item":%q,"position":%d,"duration":%d}`, item, page, total), user))
		if w.Code != http.StatusNoContent {
			t.Fatalf("put: %d %s", w.Code, w.Body)
		}
	}
	reading := func() comics.ReadingViews {
		t.Helper()
		w := httptest.NewRecorder()
		s.handleComicReading(w, comicReq(http.MethodGet, "/api/comics/reading", "", user))
		var v comics.ReadingViews
		if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	put(5, 20)
	if v := reading(); len(v.Continue) != 0 || len(v.Read) != 0 {
		t.Errorf("no file yet, so nothing to continue: %+v", v)
	}
	_ = repo.SetState(id, comics.StatusDownloaded, "/x.cbz", 1)
	if v := reading(); len(v.Continue) != 1 || v.Continue[0].Page != 5 || len(v.Read) != 0 {
		t.Errorf("part-way: %+v", v)
	}
	put(20, 20)
	if v := reading(); len(v.Continue) != 0 || len(v.Read) != 1 {
		t.Errorf("read to the end: %+v", v)
	}
	// The first page again (reading it again) doesn't make it unread.
	put(1, 20)
	if v := reading(); len(v.Read) != 1 {
		t.Errorf("reading it again: %+v", v)
	}
	// Marked unread.
	w := httptest.NewRecorder()
	s.handleComicMarkRead(w, comicReq(http.MethodPut, "/api/comics/reading/mark", fmt.Sprintf(`{"issueId":%d,"scope":"series","read":false}`, id), user))
	if w.Code != http.StatusOK {
		t.Fatalf("mark: %d %s", w.Code, w.Body)
	}
	if v := reading(); len(v.Read) != 0 {
		t.Errorf("marked unread: %+v", v)
	}
	w = httptest.NewRecorder()
	s.handleComicMarkRead(w, comicReq(http.MethodPut, "/api/comics/reading/mark", fmt.Sprintf(`{"issueId":%d,"scope":"nonsense","read":true}`, id), user))
	if w.Code != http.StatusBadRequest {
		t.Errorf("a scope that isn't one: %d", w.Code)
	}
}

// A file's ComicInfo decides the direction, then the person's choice takes
// over; the answer says what comes next.
func TestComicPagesAnswerHowToRead(t *testing.T) {
	s, repo := comicTestServer(t)
	s.Settings = settings.New(s.db, nil)
	s.cfg = config.Config{DownloadsDir: t.TempDir()}
	user := comicUser(t, s, "sam")
	file := filepath.Join(t.TempDir(), "one.cbz")
	f, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range map[string]string{"001.jpg": "x", "ComicInfo.xml": `<ComicInfo><Manga>YesAndRightToLeft</Manga></ComicInfo>`} {
		e, _ := zw.Create(name)
		_, _ = e.Write([]byte(body))
	}
	_ = zw.Close()
	_ = f.Close()
	sr, _ := repo.UpsertSeries(comics.Series{CVID: 1, Name: "Vagabond"})
	for k, n := range []string{"1", "2", "3"} {
		_, _ = repo.UpsertIssue(comics.Issue{SeriesID: sr.ID, CVID: 10 + int64(k), Number: n})
	}
	list, _ := repo.Issues(sr.ID)
	_ = repo.SetState(list[0].ID, comics.StatusDownloaded, file, 1)
	_ = repo.SetState(list[2].ID, comics.StatusDownloaded, file, 1)

	pages := func() map[string]any {
		t.Helper()
		w := httptest.NewRecorder()
		r := comicReq(http.MethodGet, "/x", "", user)
		r.SetPathValue("id", fmt.Sprint(list[0].ID))
		s.handleComicPages(w, r)
		var out map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != http.StatusOK {
			t.Fatalf("pages: %d %s", w.Code, w.Body)
		}
		return out
	}
	got := pages()
	if got["direction"] != "rtl" || got["pages"] != float64(1) || got["seriesId"] != float64(sr.ID) {
		t.Errorf("answer: %+v", got)
	}
	if n, _ := got["next"].(map[string]any); n["number"] != "2" || n["have"] != false {
		t.Errorf("next: %+v", got["next"])
	}
	if n, _ := got["nextHave"].(map[string]any); n["number"] != "3" {
		t.Errorf("next one here: %+v", got["nextHave"])
	}

	w := httptest.NewRecorder()
	s.handleComicReaderPrefs(w, comicReq(http.MethodPut, "/x", fmt.Sprintf(`{"seriesId":%d,"direction":"ltr","layout":"webtoon"}`, sr.ID), user))
	if w.Code != http.StatusNoContent {
		t.Fatalf("prefs: %d %s", w.Code, w.Body)
	}
	if got := pages(); got["direction"] != "ltr" || got["layout"] != "webtoon" {
		t.Errorf("their choice: %+v", got)
	}
	w = httptest.NewRecorder()
	s.handleComicReaderPrefs(w, comicReq(http.MethodPut, "/x", `{"layout":"sideways"}`, user))
	if w.Code != http.StatusBadRequest {
		t.Errorf("a layout that isn't one: %d", w.Code)
	}
}

func TestPanelOrderFits(t *testing.T) {
	viz, west := comics.Series{Publisher: "Viz"}, comics.Series{Publisher: "Image"}
	cases := []struct {
		was  int
		rtl  bool
		sr   comics.Series
		want bool
	}{
		{1, true, west, true},
		{1, false, viz, false},
		{0, false, viz, true},
		{0, true, west, false},
		{-1, true, viz, true}, // kept before the order was noted: the publisher decided
		{-1, false, viz, false},
		{-1, false, west, true},
		{-1, true, west, false},
	}
	for _, c := range cases {
		if got := panelOrderFits(c.was, c.rtl, c.sr); got != c.want {
			t.Errorf("%+v: got %v", c, got)
		}
	}
}
