package api_test

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// When a show's folder also holds files named for other shows, a rescan only
// gives the show the files whose title is its own. The others are listed as
// not matched and left where they are.
func TestRescanShowIgnoresOtherShowsInASharedFolder(t *testing.T) {
	tests := []struct {
		name      string
		files     map[string]string // path under the show's folder -> content
		wantAdded int
		wantS1E2  string // the file S01E02 should get ("" = none)
		wantS2E1  string
		wantNot   []string
	}{
		{
			name:      "a show that is not in the library",
			files:     map[string]string{"Fixture Show - S01E02.mkv": "a", "Other Show - S02E01.mkv": "bb"},
			wantAdded: 1,
			wantS1E2:  "Fixture Show - S01E02.mkv",
			wantNot:   []string{"Other Show - S02E01.mkv"},
		},
		{
			name:      "only another show's files: nothing is placed",
			files:     map[string]string{"Other.Show.S01E02.720p.mkv": "a", "Other.Show.S02E01.720p.mkv": "bb"},
			wantAdded: 0,
			wantNot:   []string{"Other.Show.S01E02.720p.mkv", "Other.Show.S02E01.720p.mkv"},
		},
		{
			name:      "another show's file in a folder of its own is also left alone",
			files:     map[string]string{"Other Show/Season 01/Other Show - S01E02.mkv": "a", "Fixture.Show.S02E01.mkv": "b"},
			wantAdded: 1,
			wantS2E1:  "Fixture.Show.S02E01.mkv",
			wantNot:   []string{"Other Show - S01E02.mkv"},
		},
		{
			name:      "a name with no title, straight in the show's folder or in a season folder, is still placed",
			files:     map[string]string{"S01E02.mkv": "a", "Season 02/S02E01.mkv": "bb", "Other Show - S01E02.mkv": "ccc"},
			wantAdded: 2,
			wantS1E2:  "S01E02.mkv",
			wantS2E1:  "Season 02/S02E01.mkv",
			wantNot:   []string{"Other Show - S01E02.mkv"},
		},
		{
			name:      "a folder with only this show's files works as before",
			files:     map[string]string{"Season 01/Fixture Show - S01E02.mkv": "a", "Season 02/Fixture.Show.S02E01.mkv": "bb"},
			wantAdded: 2,
			wantS1E2:  "Season 01/Fixture Show - S01E02.mkv",
			wantS2E1:  "Season 02/Fixture.Show.S02E01.mkv",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := newMetaLibrary(t)
			showID := int64(getJSON[[]map[string]any](t, l.client, l.base+"/api/series")[0]["id"].(float64))
			// S01E02 and S02E01 have no file; S01E01 keeps its own.
			if err := l.server.TestExec(`UPDATE episodes SET status = 'missing', file_path = NULL WHERE series_id = ? AND ((season = 1 AND episode = 2) OR (season = 2 AND episode = 1))`, showID); err != nil {
				t.Fatal(err)
			}
			if err := l.server.TestExec(`UPDATE episodes SET file_path = ? WHERE series_id = ? AND season = 1 AND episode = 1`, l.ep1, showID); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(l.ep2); err != nil {
				t.Fatal(err)
			}
			show := filepath.Join(l.tv, "Fixture Show")
			for rel, content := range tt.files {
				putFile(t, filepath.Join(show, rel), content)
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
			for _, w := range []struct {
				season, episode int
				rel             string
			}{{1, 2, tt.wantS1E2}, {2, 1, tt.wantS2E1}} {
				ep, err := l.server.MovieRepo.GetEpisode(showID, w.season, w.episode)
				if err != nil {
					t.Fatal(err)
				}
				want := ""
				if w.rel != "" {
					want = filepath.Join(show, w.rel)
				}
				if ep.FilePath != want {
					t.Fatalf("S%02dE%02d = %q, want %q", w.season, w.episode, ep.FilePath, want)
				}
			}
		})
	}
}
