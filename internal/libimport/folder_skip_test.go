package libimport_test

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/libimport"
)

// A file the caller says is not up for grabs (already attached, or someone
// else's) takes no part in removing duplicates, so a smaller multi-episode
// file is not dropped as a copy of a bigger file that is taken.
func TestScanSeriesFolderSkippingKeepsDuplicatesAmongNewFilesOnly(t *testing.T) {
	tests := []struct {
		name     string
		files    map[string]int64 // name -> size
		taken    []string         // names the caller already has
		want     []string         // names listed, sorted
		wantDups int              // lines about duplicate copies
	}{
		{
			name:     "no skipping: the smaller file is a duplicate",
			files:    map[string]int64{"Show.S01E02.mkv": 50, "Show.S01E02E03.mkv": 10},
			want:     []string{"Show.S01E02.mkv"},
			wantDups: 1,
		},
		{
			name:  "the bigger file is already attached: the double episode is kept",
			files: map[string]int64{"Show.S01E02.mkv": 50, "Show.S01E02E03.mkv": 10},
			taken: []string{"Show.S01E02.mkv"},
			want:  []string{"Show.S01E02.mkv", "Show.S01E02E03.mkv"},
		},
		{
			name:     "two new files for one episode keep the better one",
			files:    map[string]int64{"Show.S01E02.720p.mkv": 10, "Show.S01E02.1080p.mkv": 50, "Show.S01E01.mkv": 5},
			taken:    []string{"Show.S01E01.mkv"},
			want:     []string{"Show.S01E01.mkv", "Show.S01E02.1080p.mkv"},
			wantDups: 1,
		},
		{
			name:  "daily and anime files too",
			files: map[string]int64{"Show.2024.01.15.mkv": 50, "Show.2024.01.15.720p.mkv": 10, "[G] Show - 05.mkv": 50, "[G] Show - 05 [480p].mkv": 10},
			taken: []string{"Show.2024.01.15.mkv", "[G] Show - 05.mkv"},
			want:  []string{"Show.2024.01.15.720p.mkv", "Show.2024.01.15.mkv", "[G] Show - 05 [480p].mkv", "[G] Show - 05.mkv"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			for name, size := range tt.files {
				touch(t, root, name, int(size))
			}
			var gotPaths []string
			files, skipped, err := libimport.ScanSeriesFolderSkipping(root, func(paths []string) func(libimport.File) bool {
				gotPaths = paths
				return func(f libimport.File) bool {
					for _, n := range tt.taken {
						if filepath.Base(f.Path) == n {
							return true
						}
					}
					return false
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(gotPaths) != len(tt.files) {
				t.Fatalf("prepare got %d paths, want %d", len(gotPaths), len(tt.files))
			}
			var got []string
			for _, f := range files {
				got = append(got, filepath.Base(f.Path))
			}
			sort.Strings(got)
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Fatalf("listed %v, want %v (skipped %v)", got, tt.want, skipped)
			}
			dups := 0
			for _, s := range skipped {
				if strings.HasSuffix(s, "duplicate copy of the same episode") {
					dups++
				}
			}
			if dups != tt.wantDups {
				t.Fatalf("duplicate lines = %d, want %d (%v)", dups, tt.wantDups, skipped)
			}
		})
	}
}

// The title read from each name is passed on.
func TestScanSeriesFolderReadsTitles(t *testing.T) {
	root := t.TempDir()
	touch(t, root, "Other.Show.S01E01.mkv", 10)
	touch(t, root, "S01E02.mkv", 10)
	files, _, err := libimport.ScanSeriesFolder(root)
	if err != nil {
		t.Fatal(err)
	}
	titles := map[string]string{}
	for _, f := range files {
		titles[filepath.Base(f.Path)] = f.Title
	}
	if titles["Other.Show.S01E01.mkv"] != "Other Show" || titles["S01E02.mkv"] != "" {
		t.Fatalf("titles = %v", titles)
	}
}
