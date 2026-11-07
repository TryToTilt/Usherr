package books_test

import (
	"database/sql"
	"math"
	"path/filepath"
	"testing"

	"github.com/rdborg/mediarium/internal/books"
	"github.com/rdborg/mediarium/internal/store"
)

func TestAudioTimelineRoundTrip(t *testing.T) {
	tl := books.AudioTimeline{100, 50.5, 200}
	if !tl.Complete() || tl.Total() != 350.5 {
		t.Fatalf("complete %v total %v", tl.Complete(), tl.Total())
	}
	for _, c := range []struct {
		track   int
		seconds float64
		at      float64
	}{{0, 0, 0}, {0, 99.5, 99.5}, {1, 0, 100}, {1, 50, 150}, {2, 10, 160.5}, {2, 200, 350.5}} {
		if got := tl.Absolute(c.track, c.seconds); got != c.at {
			t.Errorf("absolute %d:%v = %v, want %v", c.track, c.seconds, got, c.at)
		}
		track, seconds := tl.Locate(c.at)
		if track != c.track || math.Abs(seconds-c.seconds) > 1e-9 {
			t.Errorf("locate %v = %d:%v, want %d:%v", c.at, track, seconds, c.track, c.seconds)
		}
	}
	// A place a moment past its file's end stays at that file's end.
	if got := tl.Absolute(0, 100.4); got != 100 {
		t.Errorf("past the end: %v", got)
	}
	if track, seconds := tl.Locate(1000); track != 2 || seconds != 200 {
		t.Errorf("past the book: %d:%v", track, seconds)
	}
	if (books.AudioTimeline{100, 0}).Complete() || (books.AudioTimeline{}).Complete() {
		t.Error("a missing length is not complete")
	}
}

func TestConvertAudioPosition(t *testing.T) {
	tl := books.AudioTimeline{3000, 1000}
	for _, c := range []struct {
		position string
		percent  float64
		ok       bool
	}{
		{"0:1500", 37.5, true},
		{"1:500", 87.5, true},
		{"1:93.5", 77.3, true},
		{"0:3000.4", 75, true}, // rounding past a file's end doesn't move on
		{"2:10", 0, false},     // that file is gone
		{"", 0, false},
		{"epubcfi(/6/2)", 0, false},
	} {
		got, ok := books.ConvertAudioPosition(c.position, tl)
		if ok != c.ok || got != c.percent {
			t.Errorf("%q: %v %v, want %v %v", c.position, got, ok, c.percent, c.ok)
		}
	}
	if _, ok := books.ConvertAudioPosition("0:10", books.AudioTimeline{3000, 0}); ok {
		t.Error("a length is missing: the percent can't be told")
	}
}

func lengthsRepo(t *testing.T) (*books.Repo, *sql.DB, int64) {
	t.Helper()
	conn, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	repo := books.NewRepo(conn)
	b, err := repo.Add(books.Book{OLKey: "OL1W", Title: "A Long Book", WantAudiobook: true})
	if err != nil {
		t.Fatal(err)
	}
	return repo, conn, b.ID
}

// Places saved before the lengths were known carry over: the same spot in the
// same file, saved at the same time; only the percent becomes true.
func TestRefreshAudioPercentsCarriesPlacesOver(t *testing.T) {
	repo, conn, id := lengthsRepo(t)
	for _, name := range []string{"a", "b", "c", "d"} {
		if _, err := conn.Exec(`INSERT INTO users (username, password_hash) VALUES (?, 'x')`, name); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []struct {
		user     int64
		position string
		percent  float64
		finished bool
	}{
		{1, "1:500", 95, false},  // the estimate from sizes
		{2, "5:10", 40, false},   // a file that is no longer there: left alone
		{3, "1:1000", 100, true}, // finished stays finished
		{4, "nonsense", 12, false},
	} {
		if err := repo.SetProgress(p.user, books.Progress{BookID: id, Format: books.Audiobook, Position: p.position, Percent: p.percent, Finished: p.finished}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := conn.Exec(`UPDATE book_progress SET updated_at = '2020-01-02T03:04:05Z'`); err != nil {
		t.Fatal(err)
	}
	n, err := repo.RefreshAudioPercents(id, books.AudioTimeline{3000, 1000})
	if err != nil || n != 1 {
		t.Fatalf("refreshed %d, %v", n, err)
	}
	want := map[int64]struct {
		position string
		percent  float64
	}{1: {"1:500", 87.5}, 2: {"5:10", 40}, 3: {"1:1000", 100}, 4: {"nonsense", 12}}
	for user, w := range want {
		p, ok, err := repo.GetProgress(user, id, books.Audiobook)
		if err != nil || !ok {
			t.Fatal(ok, err)
		}
		if p.Position != w.position || p.Percent != w.percent || p.UpdatedAt != "2020-01-02T03:04:05Z" {
			t.Errorf("user %d: %+v, want %+v", user, p, w)
		}
	}
	if n, err := repo.RefreshAudioPercents(id, books.AudioTimeline{3000, 0}); n != 0 || err != nil {
		t.Errorf("incomplete timeline changed %d rows, %v", n, err)
	}
}

func TestAudioLengthsStore(t *testing.T) {
	repo, _, id := lengthsRepo(t)
	if err := repo.SaveAudioLength(id, books.AudioLength{Rel: "a.mp3", Size: 10, ModNs: 20, Seconds: 61.5}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveAudioLength(id, books.AudioLength{Rel: "b.mp3", Size: 1, ModNs: 2, Tries: books.MaxLengthTries}); err != nil {
		t.Fatal(err)
	}
	got, err := repo.AudioLengths(id)
	if err != nil || len(got) != 2 {
		t.Fatal(got, err)
	}
	if a := got["a.mp3"]; !a.Fresh(10, 20) || a.Fresh(11, 20) || !a.Settled() || a.Seconds != 61.5 {
		t.Errorf("a: %+v", a)
	}
	if b := got["b.mp3"]; !b.Settled() {
		t.Errorf("b has been tried enough: %+v", b)
	}
	if err := repo.ForgetAudioLength(id, "b.mp3"); err != nil {
		t.Fatal(err)
	}
	if err := repo.FillAudioRuntime(id, 600); err != nil {
		t.Fatal(err)
	}
	if err := repo.FillAudioRuntime(id, 6000); err != nil {
		t.Fatal(err)
	}
	if b, _ := repo.Get(id); b.RuntimeMin != 10 {
		t.Errorf("runtime only filled when empty: %d", b.RuntimeMin)
	}
	if err := repo.Delete(id); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.AudioLengths(id); len(got) != 0 {
		t.Errorf("lengths go with the book: %v", got)
	}
}
