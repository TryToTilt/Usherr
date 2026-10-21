package api_test

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
)

// Rescan files on a show also reads the show's folder: a video file put there
// by hand goes on the episode its name says, as the library import reads it.
// Nothing is moved, queued or taken from another episode.
func TestRescanShowAddsEpisodesPutInTheFolderByHand(t *testing.T) {
	tests := []struct {
		name       string
		seriesType string
		files      map[string]string // path under the show's folder -> content
		links      map[string]string // path under the show's folder -> a file outside the library it links to
		wantAdded  int
		wantFiles  map[[2]int]string // episode -> the path under the show's folder it should have
		wantNot    []string
	}{
		{
			name:      "season and episode numbers, and one file nothing matches",
			files:     map[string]string{"Season 01/Fixture Show - S01E02.mkv": "a", "Season 02/Fixture Show - S02E01.1080p.mkv": "bb", "Random Clip.mkv": "c"},
			wantAdded: 2,
			wantFiles: map[[2]int]string{{1, 2}: "Season 01/Fixture Show - S01E02.mkv", {2, 1}: "Season 02/Fixture Show - S02E01.1080p.mkv"},
			wantNot:   []string{"Random Clip.mkv"},
		},
		{
			name:       "an anime named by its number from the first episode",
			seriesType: library.SeriesAnime,
			files:      map[string]string{"[Group] Fixture Show - 03 [1080p].mkv": "a"},
			wantAdded:  1,
			wantFiles:  map[[2]int]string{{2, 1}: "[Group] Fixture Show - 03 [1080p].mkv"},
		},
		{
			name:       "a daily show named by the day it aired",
			seriesType: library.SeriesDaily,
			files:      map[string]string{"Fixture.Show.2011.04.24.720p.WEB.mkv": "a", "Fixture.Show.2011.05.01.720p.WEB.mkv": "b"},
			wantAdded:  1,
			wantFiles:  map[[2]int]string{{1, 2}: "Fixture.Show.2011.04.24.720p.WEB.mkv"},
			wantNot:    []string{"Fixture.Show.2011.05.01.720p.WEB.mkv"},
		},
		{
			name:      "an episode that has its own file keeps it",
			files:     map[string]string{"Season 01/Fixture Show - S01E01.1080p.mkv": "a much bigger file"},
			wantAdded: 0,
			wantFiles: map[[2]int]string{{1, 1}: "Season 01/Fixture Show - S01E01-E02.mkv"},
		},
		{
			name:      "a link that leads out of the library is not followed",
			links:     map[string]string{"Season 01/Fixture Show - S01E02.mkv": "outside.mkv"},
			wantAdded: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := newMetaLibrary(t)
			showID := int64(getJSON[[]map[string]any](t, l.client, l.base+"/api/series")[0]["id"].(float64))
			if tt.seriesType != "" {
				if err := l.server.TestExec(`UPDATE series SET series_type = ? WHERE id = ?`, tt.seriesType, showID); err != nil {
					t.Fatal(err)
				}
			}
			// S01E02 and S02E01 have no file; S01E02 is not wanted, S02E01 is.
			if err := l.server.TestExec(`UPDATE episodes SET status = 'missing', file_path = NULL, monitored = 0 WHERE series_id = ? AND season = 1 AND episode = 2`, showID); err != nil {
				t.Fatal(err)
			}
			if err := l.server.TestExec(`UPDATE episodes SET status = 'missing', file_path = NULL, monitored = 1 WHERE series_id = ? AND season = 2 AND episode = 1`, showID); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(l.ep2); err != nil { // the fixture's own file for S02E01
				t.Fatal(err)
			}
			if tt.name == "an episode that has its own file keeps it" {
				if err := l.server.TestExec(`UPDATE episodes SET status = 'downloaded', file_path = ? WHERE series_id = ? AND season = 1 AND episode = 1`, l.ep1, showID); err != nil {
					t.Fatal(err)
				}
			}
			show := filepath.Join(l.tv, "Fixture Show")
			for rel, content := range tt.files {
				putFile(t, filepath.Join(show, rel), content)
			}
			for rel, target := range tt.links {
				outside := filepath.Join(t.TempDir(), target)
				putFile(t, outside, "elsewhere")
				link := filepath.Join(show, rel)
				if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, link); err != nil {
					t.Skipf("can't make a symlink here: %v", err)
				}
			}

			got := postJSON[map[string]any](t, l.client, fmt.Sprintf("%s/api/series/%d/rescan", l.base, showID), nil, http.StatusOK)
			added, _ := got["addedEpisodes"].(float64)
			if int(added) != tt.wantAdded {
				t.Fatalf("addedEpisodes = %v, want %d (%v)", got["addedEpisodes"], tt.wantAdded, got)
			}
			var notMatched []string
			nm, _ := got["notMatched"].([]any)
			for _, n := range nm {
				notMatched = append(notMatched, n.(string))
			}
			if !reflect.DeepEqual(notMatched, tt.wantNot) {
				t.Fatalf("notMatched = %v, want %v", notMatched, tt.wantNot)
			}
			for key, rel := range tt.wantFiles {
				ep, err := l.server.MovieRepo.GetEpisode(showID, key[0], key[1])
				if err != nil {
					t.Fatal(err)
				}
				if want := filepath.Join(show, rel); ep.FilePath != want || ep.Status != library.StatusDownloaded {
					t.Fatalf("S%02dE%02d = %q (%s), want %q", key[0], key[1], ep.FilePath, ep.Status, want)
				}
			}
			// What the person chose to watch for stays, nothing is downloaded and
			// nothing was moved or renamed.
			if ep, _ := l.server.MovieRepo.GetEpisode(showID, 1, 2); ep.Monitored {
				t.Fatal("the episode that was not wanted is wanted now")
			}
			if ep, _ := l.server.MovieRepo.GetEpisode(showID, 2, 1); !ep.Monitored {
				t.Fatal("the episode that was wanted is not wanted now")
			}
			if q := getJSON[[]map[string]any](t, l.client, l.base+"/api/queue"); len(q) != 0 {
				t.Fatalf("nothing should be queued: %v", q)
			}
			for rel := range tt.files {
				if !exists(filepath.Join(show, rel)) {
					t.Fatalf("%s was moved or deleted", rel)
				}
			}
		})
	}
}

// A file that an episode of another show has is not taken, and a second press
// of the button finds nothing new.
func TestRescanShowLeavesOtherTitlesFilesAlone(t *testing.T) {
	l := newMetaLibrary(t)
	showID := int64(getJSON[[]map[string]any](t, l.client, l.base+"/api/series")[0]["id"].(float64))
	if err := l.server.TestExec(`UPDATE episodes SET status = 'missing', file_path = NULL WHERE series_id = ? AND season = 2`, showID); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(l.ep2); err != nil { // the fixture's own file for S02E01
		t.Fatal(err)
	}
	file := filepath.Join(l.tv, "Fixture Show", "Fixture Show - S02E01.mkv")
	putFile(t, file, "video")
	if err := l.server.TestExec(`UPDATE movies SET file_path = ? WHERE id = ?`, file, int64(l.movieID)); err != nil {
		t.Fatal(err)
	}
	got := postJSON[map[string]any](t, l.client, fmt.Sprintf("%s/api/series/%d/rescan", l.base, showID), nil, http.StatusOK)
	if got["addedEpisodes"] != nil {
		t.Fatalf("a file that belongs to a movie was taken: %v", got)
	}
	if err := l.server.TestExec(`UPDATE movies SET file_path = NULL WHERE id = ?`, int64(l.movieID)); err != nil {
		t.Fatal(err)
	}
	if got = postJSON[map[string]any](t, l.client, fmt.Sprintf("%s/api/series/%d/rescan", l.base, showID), nil, http.StatusOK); got["addedEpisodes"] != float64(1) {
		t.Fatalf("the file should be added once it is free: %v", got)
	}
	if got = postJSON[map[string]any](t, l.client, fmt.Sprintf("%s/api/series/%d/rescan", l.base, showID), nil, http.StatusOK); got["addedEpisodes"] != nil {
		t.Fatalf("the second rescan should find nothing new: %v", got)
	}
}
