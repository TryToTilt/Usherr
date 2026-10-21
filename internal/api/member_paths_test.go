package api_test

import (
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/queue"
)

// Members never see where the server keeps its files: not in the downloads
// list, not in the second-copies answer.
func TestMembersDoNotSeeServerPaths(t *testing.T) {
	server, base, admin, member, _ := familyServer(t)
	server.TestSetTMDBBaseURL("k", newTitleTMDB(t).URL)
	copies := t.TempDir()
	putJSONStatus(t, admin, base+"/api/settings", map[string]any{"movieCopiesPath": copies}, http.StatusOK)

	m, err := server.MovieRepo.Add(library.Movie{TMDBID: 603, Title: "Film", Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	id, err := server.QueueRepo.Enqueue(queue.Item{MovieID: m.ID, ReleaseTitle: "Film.2020.1080p", NZBURL: "http://x"})
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(copies, "Film (2020)", "Film (2020).mkv")
	if err := server.QueueRepo.SetConflict(id, filepath.Join(copies, "src.mkv"), dest); err != nil {
		t.Fatal(err)
	}
	failed, err := server.QueueRepo.Enqueue(queue.Item{MovieID: m.ID, ReleaseTitle: "Film.2021.1080p", NZBURL: "http://y"})
	if err != nil {
		t.Fatal(err)
	}
	lost := filepath.ToSlash(filepath.Join(copies, "lost"))
	if err := server.QueueRepo.SetStatus(failed, queue.StatusFailed, "The library folder "+lost+" can't be found, so nothing was put into it."); err != nil {
		t.Fatal(err)
	}

	needle := filepath.ToSlash(copies)
	for _, url := range []string{base + "/api/queue", fmt.Sprintf("%s/api/movies/%d/copies", base, m.ID)} {
		resp, err := member.Get(url)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		text := strings.ReplaceAll(strings.ReplaceAll(string(body), `\\`, "/"), `\`, "/")
		if strings.Contains(text, needle) {
			t.Errorf("%s shows a member the server path %s: %s", url, needle, body)
		}
	}
	// The administrator still gets the full folder.
	adminCopies := getJSON[struct{ Folder string }](t, admin, fmt.Sprintf("%s/api/movies/%d/copies", base, m.ID))
	if adminCopies.Folder == "" {
		t.Errorf("the administrator should see the copies folder")
	}
}
