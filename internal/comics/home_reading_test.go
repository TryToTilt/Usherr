package comics

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestResolveReader(t *testing.T) {
	viz := Series{Publisher: "Viz"}
	west := Series{Publisher: "Image"}
	cases := []struct {
		name  string
		s     Series
		file  string
		own   ReaderPrefs
		def   ReaderPrefs
		wantD string
		wantL string
	}{
		{"nothing known", west, "", ReaderPrefs{}, ReaderPrefs{}, "ltr", ""},
		{"the file says right to left", west, "YesAndRightToLeft", ReaderPrefs{}, ReaderPrefs{Direction: "ltr"}, "rtl", ""},
		{"Manga Yes alone isn't right to left", west, "Yes", ReaderPrefs{}, ReaderPrefs{}, "ltr", ""},
		{"a series set to manga", Series{Type: TypeManga}, "", ReaderPrefs{}, ReaderPrefs{}, "rtl", ""},
		{"a manga publisher", viz, "", ReaderPrefs{}, ReaderPrefs{}, "rtl", ""},
		{"a manga publisher, but the file says it isn't", viz, "No", ReaderPrefs{}, ReaderPrefs{}, "ltr", ""},
		{"a manga publisher, but set to comic", Series{Publisher: "Viz", Type: TypeComic}, "", ReaderPrefs{}, ReaderPrefs{}, "ltr", ""},
		{"their default", west, "", ReaderPrefs{}, ReaderPrefs{Direction: "rtl", Layout: "double"}, "rtl", "double"},
		{"their choice for the series beats the file", west, "YesAndRightToLeft", ReaderPrefs{Direction: "ltr", Layout: "slide"}, ReaderPrefs{Layout: "double"}, "ltr", "slide"},
		{"a webtoon scrolls", Series{Type: TypeWebtoon}, "", ReaderPrefs{}, ReaderPrefs{Layout: "double"}, "ltr", "webtoon"},
		{"their choice for a webtoon", Series{Type: TypeWebtoon}, "", ReaderPrefs{Layout: "slide"}, ReaderPrefs{}, "ltr", "slide"},
	}
	for _, c := range cases {
		got := ResolveReader(c.s, c.file, c.own, c.def)
		if got.Direction != c.wantD || got.Layout != c.wantL {
			t.Errorf("%s: got %+v, want %s %s", c.name, got, c.wantD, c.wantL)
		}
	}
}

func TestSeriesTypeDecidesManga(t *testing.T) {
	if !IsManga(Series{Publisher: "Viz"}) || IsManga(Series{Publisher: "Viz", Type: TypeComic}) || !IsManga(Series{Publisher: "Image", Type: TypeManga}) {
		t.Fatal("the type set on a series should decide, then the publisher")
	}
	s := Series{Name: "Saga", Type: TypeCollected}
	i := Issue{Number: "6"}
	if v := Evaluate(Release{Title: "Saga Vol. 6 TPB (2016)"}, s, i); v.Reject != "" {
		t.Errorf("collected series, the volume with this number: %+v", v)
	}
	if v := Evaluate(Release{Title: "Saga Vol. 5 TPB (2015)"}, s, i); v.Reject == "" {
		t.Errorf("collected series, another volume should be rejected: %+v", v)
	}
}

func TestNextIssues(t *testing.T) {
	list := []Issue{{ID: 1, Status: StatusDownloaded}, {ID: 2}, {ID: 3, Status: StatusDownloaded}, {ID: 4}}
	if n, h := NextIssues(list, 1); n == nil || n.ID != 2 || h == nil || h.ID != 3 {
		t.Errorf("after #1: %+v %+v", n, h)
	}
	if n, h := NextIssues(list, 2); n == nil || n.ID != 3 || h != n {
		t.Errorf("after #2: %+v %+v", n, h)
	}
	if n, h := NextIssues(list, 3); n == nil || n.ID != 4 || h != nil {
		t.Errorf("after #3: %+v %+v", n, h)
	}
	if n, h := NextIssues(list, 4); n != nil || h != nil {
		t.Errorf("after the last: %+v %+v", n, h)
	}
}

func TestPlanRepoint(t *testing.T) {
	here := []Issue{
		{ID: 1, Number: "1", Status: StatusDownloaded},
		{ID: 2, Number: "02", Status: StatusMissing},
		{ID: 3, Number: "3", Status: StatusDownloaded},
		{ID: 4, Number: "4", Status: StatusMissing},
		{ID: 5, Number: "5", Status: StatusDownloading},
		{ID: 6, Number: "1", Status: StatusMissing}, // the same number twice
		{ID: 7, Number: "1", Status: StatusDownloaded},
	}
	p := PlanRepoint(here, []Issue{{CVID: 10, Number: "1"}, {CVID: 20, Number: "2"}, {CVID: 90, Number: "9"}})
	if len(p.Matched) != 2 || p.Matched[0].Here.ID != 1 || p.Matched[1].Here.ID != 2 || p.Matched[1].CV.CVID != 20 {
		t.Errorf("matched: %+v", p.Matched)
	}
	if len(p.New) != 1 || p.New[0].CVID != 90 {
		t.Errorf("new: %+v", p.New)
	}
	ids := func(l []Issue) (out []int64) {
		for _, i := range l {
			out = append(out, i.ID)
		}
		return out
	}
	if got := ids(p.Kept); len(got) != 3 || got[0] != 3 || got[1] != 5 || got[2] != 7 {
		t.Errorf("kept: %v", got)
	}
	if got := ids(p.Dropped); len(got) != 2 || got[0] != 4 || got[1] != 6 {
		t.Errorf("dropped: %v", got)
	}
}

func readingUser(t *testing.T, r *Repo, name string) int64 {
	t.Helper()
	res, err := r.db.Exec(`INSERT INTO users (username, password_hash) VALUES (?, 'x')`, name)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func TestReadingViews(t *testing.T) {
	r := testRepo(t)
	ctx := context.Background()
	now := time.Now()
	sam, kim := readingUser(t, r, "sam"), readingUser(t, r, "kim")
	sr, _ := r.UpsertSeries(Series{CVID: 1, Name: "Saga"})
	other, _ := r.UpsertSeries(Series{CVID: 2, Name: "Groo"})
	var ids []int64
	for n, s := range []Series{sr, sr, sr, sr} {
		_, _ = r.UpsertIssue(Issue{SeriesID: s.ID, CVID: int64(100 + n), Number: string(rune('1' + n))})
	}
	_, _ = r.UpsertIssue(Issue{SeriesID: other.ID, CVID: 200, Number: "1"})
	list, _ := r.Issues(sr.ID)
	for _, i := range list {
		ids = append(ids, i.ID)
		_ = r.SetState(i.ID, StatusDownloaded, "/x", 1)
	}
	gl, _ := r.Issues(other.ID)
	_ = r.SetState(gl[0].ID, StatusDownloaded, "/y", 1)

	// Part-way through Groo #1; finished Saga #1 and #2.
	if _, err := r.db.Exec(`INSERT INTO home_positions (user_id, item, position, duration, updated_ms) VALUES (?, ?, 5, 20, ?)`, sam, fmt.Sprintf("comic:%d", gl[0].ID), now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if err := r.MarkRead(sam, ids[:2], true, now); err != nil {
		t.Fatal(err)
	}
	if err := r.SetWantRead(sam, sr.ID, 0, true, now); err != nil {
		t.Fatal(err)
	}
	if err := r.SetWantRead(sam, sr.ID, ids[3], true, now); err != nil {
		t.Fatal(err)
	}
	v, err := r.Reading(ctx, sam)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Continue) != 2 || v.Continue[0].IssueID != gl[0].ID || v.Continue[0].Page != 5 || v.Continue[1].IssueID != ids[2] || !v.Continue[1].Next {
		t.Errorf("continue: %+v", v.Continue)
	}
	if len(v.Read) != 2 || len(v.Want) != 2 {
		t.Errorf("read %+v, want %+v", v.Read, v.Want)
	}
	// It's per person.
	if w, _ := r.Reading(ctx, kim); len(w.Continue)+len(w.Read)+len(w.Want) != 0 {
		t.Errorf("someone else's reading showed: %+v", w)
	}
	// Reading an issue takes it off Want to Read; with everything read the series goes too.
	if err := r.MarkRead(sam, ids[2:], true, now); err != nil {
		t.Fatal(err)
	}
	if v, _ = r.Reading(ctx, sam); len(v.Want) != 0 {
		t.Errorf("want after reading it all: %+v", v.Want)
	}
	// Up to here, leaving out what comes after and what isn't out.
	is, _ := r.GetIssue(ids[1])
	got, err := r.ScopeIssues(is, ScopeUpTo, false, now)
	if err != nil || len(got) != 2 || got[1] != ids[1] {
		t.Errorf("up to here: %v %v", got, err)
	}
	if err := r.MarkRead(sam, got, false, now); err != nil {
		t.Fatal(err)
	}
	s2 := sr
	if err := r.FillReading(sam, &s2, list); err != nil {
		t.Fatal(err)
	}
	if list[0].Read || list[1].Read || !list[2].Read || !list[3].Read {
		t.Errorf("after marking the first two unread: %+v", list)
	}
	// An id that's gone is skipped, not an error (the browser's old flags).
	if err := r.MarkRead(sam, []int64{999999}, true, now); err != nil {
		t.Errorf("a gone issue: %v", err)
	}
	// Removing a series clears what hung on it.
	if err := r.SetReaderPrefs(sam, sr.ID, ReaderPrefs{Direction: "rtl"}); err != nil {
		t.Fatal(err)
	}
	if err := r.SetReaderPrefs(sam, sr.ID, ReaderPrefs{Layout: "webtoon"}); err != nil {
		t.Fatal(err)
	}
	if p := r.ReaderPrefsFor(sam, sr.ID); p.Direction != "rtl" || p.Layout != "webtoon" {
		t.Errorf("prefs keep what isn't sent: %+v", p)
	}
	if err := r.DeleteSeries(sr.ID); err != nil {
		t.Fatal(err)
	}
	if p := r.ReaderPrefsFor(sam, sr.ID); p.Direction != "" {
		t.Errorf("prefs of a removed series: %+v", p)
	}
	if v, _ = r.Reading(ctx, sam); len(v.Read) != 0 {
		t.Errorf("read of a removed series: %+v", v.Read)
	}
}

func TestUnreleasedStaysUnreadWhenSeriesMarked(t *testing.T) {
	r := testRepo(t)
	sr, _ := r.UpsertSeries(Series{CVID: 1, Name: "Saga"})
	_, _ = r.UpsertIssue(Issue{SeriesID: sr.ID, CVID: 10, Number: "1", StoreDate: "2020-01-01"})
	_, _ = r.UpsertIssue(Issue{SeriesID: sr.ID, CVID: 11, Number: "2", StoreDate: "2999-01-01"})
	list, _ := r.Issues(sr.ID)
	got, err := r.ScopeIssues(list[0], ScopeSeries, true, time.Now())
	if err != nil || len(got) != 1 || got[0] != list[0].ID {
		t.Errorf("read: %v %v", got, err)
	}
	if got, _ = r.ScopeIssues(list[0], ScopeSeries, false, time.Now()); len(got) != 2 {
		t.Errorf("unread takes them all: %v", got)
	}
}
