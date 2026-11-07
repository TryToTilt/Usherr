package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/comics"
	"github.com/rdborg/mediarium/internal/manga"
)

// A comic series with files, a place, a mark and a read issue; temp folders only.
type moveFixture struct {
	s      *Server
	repo   *manga.Repo
	root   string
	comics string
	sr     comics.Series
	issues []comics.Issue
	user   int64
}

func moveSetup(t *testing.T) moveFixture {
	t.Helper()
	s, repo, root := mangaTestServer(t)
	dir := filepath.Join(t.TempDir(), "comics")
	if err := s.Settings.Set("comics.path", dir, false); err != nil {
		t.Fatal(err)
	}
	cr := s.comicRepo()
	folder := filepath.Join(dir, "Old Ronin")
	sr, err := cr.UpsertSeries(comics.Series{CVID: 77, Name: "Old Ronin", Publisher: "Viz", Folder: folder, MonitorNew: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := cr.SetSeriesType(sr.ID, comics.TypeManga); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 3; i++ {
		if _, err := cr.UpsertIssue(comics.Issue{SeriesID: sr.ID, CVID: int64(100 + i), Number: strconv.Itoa(i), SortKey: float64(i), Wanted: i == 3}); err != nil {
			t.Fatal(err)
		}
	}
	issues, _ := cr.Issues(sr.ID)
	for _, is := range issues[:2] {
		p := filepath.Join(folder, fmt.Sprintf("Old Ronin Issue %s.cbz", is.Number))
		writeCBZ(t, p, 2+len(is.Number))
		st, _ := os.Stat(p)
		if err := cr.SetState(is.ID, comics.StatusDownloaded, p, st.Size()); err != nil {
			t.Fatal(err)
		}
	}
	issues, _ = cr.Issues(sr.ID)
	user := mangaUser(t, s, "reader")
	item := fmt.Sprintf("comic:%d", issues[1].ID)
	if _, err := s.db.Exec(`INSERT INTO home_positions (user_id, item, position, duration, updated_ms) VALUES (?, ?, 2, 3, 1)`, user, item); err != nil {
		t.Fatal(err)
	}
	if err := cr.MarkRead(user, []int64{issues[0].ID}, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO home_reading_marks (user_id, item, kind, position, label, excerpt, note, color, percent) VALUES (?, ?, 'bookmark', 'page:2', '', '', '', '', 50)`, user, item); err != nil {
		t.Fatal(err)
	}
	if err := cr.SetReaderPrefs(user, sr.ID, comics.ReaderPrefs{Direction: "ltr", Layout: "double"}); err != nil {
		t.Fatal(err)
	}
	return moveFixture{s: s, repo: repo, root: root, comics: dir, sr: sr, issues: issues, user: user}
}

func (f moveFixture) preview(t *testing.T, body string) manga.MovePlan {
	t.Helper()
	rec := httptest.NewRecorder()
	f.s.handleMangaMovePreview(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	var p manga.MovePlan
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func (f moveFixture) apply(t *testing.T, id int64) manga.Move {
	t.Helper()
	m, err := f.repo.GetMove(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.s.applyMangaMove(t.Context(), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestMangaMovePreviewChangesNothing(t *testing.T) {
	f := moveSetup(t)
	// Duplicates beside them: the very same bytes, and a different copy.
	folder := filepath.Dir(f.issues[0].FilePath)
	same := filepath.Join(folder, "Old Ronin v01 again.cbz")
	b, _ := os.ReadFile(f.issues[0].FilePath)
	if err := os.WriteFile(same, b, 0o644); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(folder, "Old Ronin v02 other.cbz")
	writeCBZ(t, other, 9)
	extra := filepath.Join(folder, "Old Ronin v05.cbz")
	writeCBZ(t, extra, 4)
	unread := filepath.Join(folder, "cover art.cbz")
	writeCBZ(t, unread, 1)

	plan := f.preview(t, fmt.Sprintf(`{"comicSeriesId":%d}`, f.sr.ID))
	why := map[string]string{}
	to := map[string]string{}
	for _, file := range plan.Files {
		why[filepath.Base(file.From)] = file.Action + ":" + file.Why
		to[filepath.Base(file.From)] = filepath.Base(file.To)
	}
	if why["Old Ronin Issue 1.cbz"] != "move:" || to["Old Ronin Issue 1.cbz"] != "Old Ronin v01.cbz" {
		t.Fatalf("%v %v", why, to)
	}
	if why["Old Ronin v01 again.cbz"] != "stay:identical" || why["Old Ronin v02 other.cbz"] != "stay:copy" || why["Old Ronin v05.cbz"] != "move:" || why["cover art.cbz"] != "stay:unread" {
		t.Fatalf("%v", why)
	}
	if plan.Volumes != 4 || len(plan.Issues) != 1 {
		t.Fatalf("%+v", plan)
	}
	// Nothing moved, and nothing new in the library.
	for _, p := range []string{f.issues[0].FilePath, same, other, extra, unread} {
		if _, err := os.Stat(p); err != nil {
			t.Fatal(err)
		}
	}
	if list, _ := f.repo.ListSeries(); len(list) != 0 {
		t.Fatalf("%+v", list)
	}
	if m, _ := f.repo.Moves(); len(m) != 0 {
		t.Fatalf("a preview isn't a move: %+v", m)
	}
}

func TestMangaMoveAndUndo(t *testing.T) {
	f := moveSetup(t)
	cr := f.s.comicRepo()
	plan := f.preview(t, fmt.Sprintf(`{"comicSeriesId":%d}`, f.sr.ID))
	m := f.apply(t, plan.ID)
	if m.Status != manga.MoveApplied || m.MangaSeriesID == 0 {
		t.Fatalf("%+v", m)
	}
	sr, err := f.repo.GetSeries(m.MangaSeriesID)
	if err != nil || sr.Title != "Old Ronin" {
		t.Fatalf("%v %+v", err, sr)
	}
	volumes, _ := f.repo.Volumes(sr.ID)
	if len(volumes) != 3 || volumes[0].Status != manga.StatusDownloaded || volumes[2].Status == manga.StatusDownloaded || !volumes[2].Wanted {
		t.Fatalf("%+v", volumes)
	}
	want := filepath.Join(f.root, "Old Ronin", "Old Ronin v01.cbz")
	if volumes[0].FilePath != want {
		t.Fatalf("%q", volumes[0].FilePath)
	}
	if _, err := os.Stat(f.issues[0].FilePath); !os.IsNotExist(err) {
		t.Fatal("the original is still there")
	}
	// Hidden from Comics, and nothing is looked for there.
	if list, _ := cr.ListSeries(); len(list) != 0 {
		t.Fatalf("%+v", list)
	}
	if rec := httptest.NewRecorder(); true {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.SetPathValue("id", strconv.FormatInt(f.sr.ID, 10))
		f.s.handleGetComicSeries(rec, r)
		if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "moved to Manga") {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
	}
	if miss, _ := cr.WantedMissing(10); len(miss) != 0 {
		t.Fatalf("%+v", miss)
	}
	// Your place, bookmark, what you've read and how you read all came along.
	if err := f.repo.FillReading(f.user, sr.ID, volumes); err != nil {
		t.Fatal(err)
	}
	if !volumes[0].Read || volumes[1].Page != 2 || volumes[1].Pages != 3 {
		t.Fatalf("%+v", volumes)
	}
	var marks int
	_ = f.s.db.QueryRow(`SELECT COUNT(*) FROM home_reading_marks WHERE item = ?`, manga.PositionItem(volumes[1].ID)).Scan(&marks)
	if marks != 1 {
		t.Fatal("the bookmark didn't come along")
	}
	if p := f.repo.ReaderPrefsFor(f.user, sr.ID); p.Direction != "ltr" || p.Layout != "double" {
		t.Fatalf("%+v", p)
	}

	// Undo: all back as it was.
	f.s.undoMangaMove(&m)
	if m.Status != manga.MoveUndone || len(m.Journal.Left) != 0 {
		t.Fatalf("%+v", m)
	}
	if _, err := os.Stat(f.issues[0].FilePath); err != nil {
		t.Fatal("the file didn't go back")
	}
	if _, err := os.Stat(want); !os.IsNotExist(err) {
		t.Fatal("the new one is still there")
	}
	if list, _ := f.repo.ListSeries(); len(list) != 0 {
		t.Fatalf("the series the move made should go: %+v", list)
	}
	if list, _ := cr.ListSeries(); len(list) != 1 || !list[0].MonitorNew {
		t.Fatalf("%+v", list)
	}
	issues, _ := cr.Issues(f.sr.ID)
	if issues[0].FilePath != f.issues[0].FilePath || issues[0].Status != comics.StatusDownloaded || !issues[2].Wanted {
		t.Fatalf("%+v", issues)
	}
	var pos float64
	if err := f.s.db.QueryRow(`SELECT position FROM home_positions WHERE item = ?`, fmt.Sprintf("comic:%d", issues[1].ID)).Scan(&pos); err != nil || pos != 2 {
		t.Fatalf("%v %v", err, pos)
	}
	_ = f.s.db.QueryRow(`SELECT COUNT(*) FROM home_reading_marks WHERE item = ?`, fmt.Sprintf("comic:%d", issues[1].ID)).Scan(&marks)
	if marks != 1 {
		t.Fatal("the bookmark didn't go back")
	}
}

func TestMangaMoveNeverOverwrites(t *testing.T) {
	f := moveSetup(t)
	plan := f.preview(t, fmt.Sprintf(`{"comicSeriesId":%d}`, f.sr.ID))
	// A file takes the new name after the preview.
	there := filepath.Join(f.root, "Old Ronin", "Old Ronin v02.cbz")
	writeCBZ(t, there, 7)
	before, _ := os.ReadFile(there)
	m := f.apply(t, plan.ID)
	after, _ := os.ReadFile(there)
	if string(before) != string(after) {
		t.Fatal("a file was overwritten")
	}
	if _, err := os.Stat(f.issues[1].FilePath); err != nil {
		t.Fatal("the original went missing")
	}
	// A file that stayed keeps its series in Comics, and says why.
	if list, _ := f.s.comicRepo().ListSeries(); len(list) != 1 {
		t.Fatalf("%+v", list)
	}
	if m.Stayed != 0 && m.Plan.Files[1].Why != manga.StayExists {
		t.Fatalf("%+v", m.Plan.Files)
	}
	saved, _ := f.repo.GetMove(m.ID)
	if saved.Plan.Files[1].Why != manga.StayExists {
		t.Fatalf("%+v", saved.Plan.Files)
	}
	// An undo with a changed file leaves it, and lists it.
	moved := m.Journal.Steps[0].To
	if err := os.WriteFile(moved, []byte("changed since"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.s.undoMangaMove(&m)
	if len(m.Journal.Left) != 1 || m.Journal.Left[0] != moved {
		t.Fatalf("%+v", m.Journal)
	}
	if b, _ := os.ReadFile(moved); string(b) != "changed since" {
		t.Fatal("the changed file was touched")
	}
}

func TestMangaMoveEmptyWithATick(t *testing.T) {
	f := moveSetup(t)
	f.s.mangaMod().anilist.Gap = 0
	// AniList is a fake that has nothing, so the name is kept.
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"Page":{"media":[]}}}`))
	}))
	defer fake.Close()
	f.s.mangaMod().anilist = manga.NewAniList("test", fake.URL)
	f.s.mangaMod().anilist.Gap = 0
	cr := f.s.comicRepo()
	empty, err := cr.UpsertSeries(comics.Series{CVID: 88, Name: "Empty One", Publisher: "Kodansha"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cr.UpsertIssue(comics.Issue{SeriesID: empty.ID, CVID: 201, Number: "1", SortKey: 1, Wanted: true}); err != nil {
		t.Fatal(err)
	}
	post := func(ids ...int64) map[string]any {
		b, _ := json.Marshal(map[string]any{"ids": ids})
		rec := httptest.NewRecorder()
		f.s.handleMangaMoveEmpty(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(b))))
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return out
	}
	// The one with files isn't moved with a tick.
	if out := post(empty.ID, f.sr.ID); out["moved"] != 1.0 {
		t.Fatalf("%+v", out)
	}
	list, _ := f.repo.ListSeries()
	if len(list) != 1 || list[0].Title != "Empty One" || list[0].Wanted != 1 {
		t.Fatalf("%+v", list)
	}
	if left, _ := cr.ListSeries(); len(left) != 1 || left[0].ID != f.sr.ID {
		t.Fatalf("%+v", left)
	}
}

func TestMangaMoveCandidates(t *testing.T) {
	f := moveSetup(t)
	cr := f.s.comicRepo()
	if _, err := cr.UpsertSeries(comics.Series{CVID: 91, Name: "Monthly Something", Publisher: "Akita Shoten"}); err != nil {
		t.Fatal(err)
	}
	if _, err := cr.UpsertSeries(comics.Series{CVID: 92, Name: "A Cape", Publisher: "DC Comics"}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	f.s.handleMangaMoveState(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	var out struct {
		Candidates []mangaMoveCandidate `json:"candidates"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Candidates) != 2 {
		t.Fatalf("%+v", out.Candidates)
	}
	if !out.Candidates[0].Magazine || out.Candidates[0].Name != "Monthly Something" || out.Candidates[1].Magazine || out.Candidates[1].Files != 2 {
		t.Fatalf("%+v", out.Candidates)
	}
}

func TestMoveNoOverwrite(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "x", "b")
	if err := os.WriteFile(a, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := manga.MoveNoOverwrite(a, b); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(a); !os.IsNotExist(err) {
		t.Fatal("the original is still there")
	}
	if err := os.WriteFile(a, []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := manga.MoveNoOverwrite(a, b); err != manga.ErrFileThere {
		t.Fatalf("%v", err)
	}
	if got, _ := os.ReadFile(b); string(got) != "one" {
		t.Fatal("overwritten")
	}
}
