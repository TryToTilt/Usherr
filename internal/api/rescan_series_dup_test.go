package api_test

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
)

// A smaller file for two episodes put in the folder by hand is not thrown out
// as a duplicate of a bigger file that an episode already has.
func TestRescanShowKeepsMultiEpisodeFileNextToAttachedFile(t *testing.T) {
	l := newMetaLibrary(t)
	showID := int64(getJSON[[]map[string]any](t, l.client, l.base+"/api/series")[0]["id"].(float64))
	show := filepath.Join(l.tv, "Fixture Show")
	big := filepath.Join(show, "Season 01", "Fixture Show - S01E01.1080p.mkv")
	if err := os.Remove(l.ep1); err != nil {
		t.Fatal(err)
	}
	putFile(t, big, "a much bigger file than the double one")
	if err := l.server.TestExec(`UPDATE episodes SET status = 'downloaded', file_path = ? WHERE series_id = ? AND season = 1 AND episode = 1`, big, showID); err != nil {
		t.Fatal(err)
	}
	if err := l.server.TestExec(`UPDATE episodes SET status = 'missing', file_path = NULL WHERE series_id = ? AND season = 1 AND episode = 2`, showID); err != nil {
		t.Fatal(err)
	}
	double := filepath.Join(show, "Season 01", "Fixture Show - S01E01E02.mkv")
	putFile(t, double, "x")

	got := postJSON[map[string]any](t, l.client, fmt.Sprintf("%s/api/series/%d/rescan", l.base, showID), nil, http.StatusOK)
	if got["addedEpisodes"] != float64(1) {
		t.Fatalf("the double file should give the missing episode its file: %v", got)
	}
	e1, _ := l.server.MovieRepo.GetEpisode(showID, 1, 1)
	e2, _ := l.server.MovieRepo.GetEpisode(showID, 1, 2)
	if e1.FilePath != big {
		t.Fatalf("S01E01 lost its own file: %q", e1.FilePath)
	}
	if e2.FilePath != double || e2.Status != library.StatusDownloaded {
		t.Fatalf("S01E02 = %q (%s), want %q", e2.FilePath, e2.Status, double)
	}
}
