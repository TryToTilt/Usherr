package api_test

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
)

// The folder a title's files are in is shown to administrators only.
func TestFolderPathIsForAdministratorsOnly(t *testing.T) {
	e := newLocateEnv(t)
	m := e.addMovie(t, 603, "The Matrix")
	file := filepath.Join(e.movies, "The Matrix (1999)", "The Matrix (1999).mkv")
	writeBody(t, file, "video")
	if err := e.server.MovieRepo.SetStatus(m.ID, library.StatusDownloaded, "", file); err != nil {
		t.Fatal(err)
	}
	sr, eps := e.addShow(t, 99, "Show", false, [2]int{1, 1})
	epFile := filepath.Join(e.tv, "Show (2020)", "Season 1", "Show.S01E01.mkv")
	writeBody(t, epFile, "video")
	if err := e.server.MovieRepo.SetEpisodeStatus(eps[0].ID, library.StatusDownloaded, "", epFile); err != nil {
		t.Fatal(err)
	}

	for _, url := range []string{
		fmt.Sprintf("%s/api/movies/%d", e.base, m.ID),
		fmt.Sprintf("%s/api/series/%d", e.base, sr.ID),
	} {
		if got := getJSON[folderPayload](t, e.admin, url); got.FolderPath == "" {
			t.Errorf("%s: an administrator should see the folder", url)
		}
		if got := getJSON[folderPayload](t, e.member, url); got.FolderPath != "" {
			t.Errorf("%s: a member was told the folder %q", url, got.FolderPath)
		}
	}
}
