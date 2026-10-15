package api

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rdborg/mediarium/internal/comics"
	"github.com/rdborg/mediarium/internal/store"
)

func comicTestServer(t *testing.T) (*Server, *comics.Repo) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s := &Server{db: db}
	return s, s.comicRepo()
}

// Two copies of a pack at once: the one that didn't file #1 must not mark it
// missing again; Refresh picks up a file that is there but marked missing.
func TestComicIssueStaysFiled(t *testing.T) {
	s, repo := comicTestServer(t)
	folder := filepath.Join(t.TempDir(), "Absolute Carnage Miles Morales", "Volume 01 (2019)")
	_ = os.MkdirAll(folder, 0o755)
	sr, _ := repo.UpsertSeries(comics.Series{CVID: 120946, Name: "Absolute Carnage: Miles Morales", Publisher: "Marvel", IssueCount: 3, Folder: folder})
	for n := 1; n <= 3; n++ {
		_, _ = repo.UpsertIssue(comics.Issue{SeriesID: sr.ID, CVID: int64(700 + n), Number: string(rune('0' + n)), Wanted: true})
	}
	list, _ := repo.Issues(sr.ID)
	one := list[0]
	file := filepath.Join(folder, "Absolute Carnage Miles Morales (2019) Volume 01 Issue 001.cbr")
	_ = os.WriteFile(file, []byte("comic"), 0o644)

	_ = repo.SetState(one.ID, comics.StatusDownloaded, file, 5)
	s.unclaimComicIssue(one.ID) // the other copy finishing
	if is, _ := repo.GetIssue(one.ID); is.Status != comics.StatusDownloaded {
		t.Fatal("a filed issue was marked missing by the other copy")
	}

	_ = repo.SetState(one.ID, comics.StatusMissing, "", 0) // as the old code left it
	if n := s.claimComicFiles(sr); n != 1 {
		t.Fatalf("Refresh picked up %d files, want 1", n)
	}
	if is, _ := repo.GetIssue(one.ID); is.Status != comics.StatusDownloaded || is.FilePath != file {
		t.Fatalf("#1 is %s at %q", is.Status, is.FilePath)
	}
	if n := s.claimComicFiles(sr); n != 0 {
		t.Fatal("a file was claimed twice")
	}
}
