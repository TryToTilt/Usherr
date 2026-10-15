package api

import (
	"path/filepath"
	"strconv"
	"testing"

	"github.com/rdborg/mediarium/internal/comics"
	"github.com/rdborg/mediarium/internal/store"
)

// A standard volume the VizBig edition doesn't have is found in the
// standard edition of the same series.
func TestIssueInOtherEdition(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s := &Server{db: db}
	repo := s.comicRepo()
	big, err := repo.UpsertSeries(comics.Series{CVID: 49356, Name: "Vagabond", Publisher: "Viz", IssueCount: 12})
	if err != nil {
		t.Fatal(err)
	}
	std, _ := repo.UpsertSeries(comics.Series{CVID: 11111, Name: "Vagabond", Publisher: "Viz", IssueCount: 37})
	other, _ := repo.UpsertSeries(comics.Series{CVID: 22222, Name: "Vagabond Bones", Publisher: "Viz", IssueCount: 30})
	for n := 1; n <= 12; n++ {
		_, _ = repo.UpsertIssue(comics.Issue{SeriesID: big.ID, CVID: int64(1000 + n), Number: strconv.Itoa(n), Wanted: true})
	}
	for n := 1; n <= 37; n++ {
		_, _ = repo.UpsertIssue(comics.Issue{SeriesID: std.ID, CVID: int64(2000 + n), Number: strconv.Itoa(n), Wanted: true})
		_, _ = repo.UpsertIssue(comics.Issue{SeriesID: other.ID, CVID: int64(3000 + n), Number: strconv.Itoa(n), Wanted: true})
	}
	sr, is, ok := s.comicIssueInOtherEdition(big, "22")
	if !ok || sr.ID != std.ID || is.Number != "22" {
		t.Fatalf("got %v %+v %+v; want #22 of the standard edition", ok, sr, is)
	}
	if _, _, ok := s.comicIssueInOtherEdition(big, "40"); ok {
		t.Fatal("no edition has #40")
	}
}
