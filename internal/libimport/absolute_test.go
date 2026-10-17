package libimport_test

import (
	"reflect"
	"testing"

	"github.com/rdborg/mediarium/internal/libimport"
)

func TestScanTVReadsTheNumberCountedFromTheFirstEpisode(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		wantSeason   int
		wantEpisodes []int
		wantAbsolute []int
	}{
		{"Sonarr's anime naming", "One Piece (1999)/Season 23/One Piece - S23E10 - 1100 - Title.mkv", 23, []int{10}, []int{1100}},
		{"the same with the quality after it", "One Piece (1999)/Season 22/One Piece - S22E05 - 1015 - Title [WEBDL-1080p].mkv", 22, []int{5}, []int{1015}},
		{"no title after the number", "One Piece (1999)/Season 22/One Piece - S22E05 - 1015.mkv", 22, []int{5}, []int{1015}},
		{"a file named only by its number", "One Piece (1999)/[Group] One Piece - 1100 [1080p].mkv", 0, nil, []int{1100}},
		{"two episodes in one file", "One Piece (1999)/Season 22/One Piece - S22E05-E06 - 1015-1016 - Title.mkv", 22, []int{5, 6}, []int{1015, 1016}},
		{"a season and episode alone carry no absolute number", "Show (2015)/Season 01/Show - S01E05 - Title.mkv", 1, []int{5}, nil},
		{"a resolution is not a number", "Show (2015)/Season 01/Show.S01E05.1080p.WEB-DL.mkv", 1, []int{5}, nil},
		{"a year is not a number", "Show (2015)/Season 01/Show.S01E05.2015.WEB-DL.mkv", 1, []int{5}, nil},
		{"two numbers for one episode are not trusted", "One Piece (1999)/Season 22/One Piece - S22E05 - 1015-1016 - Title.mkv", 22, []int{5}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			touch(t, root, tc.path, 10)
			res, err := libimport.ScanTV(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Groups) != 1 || len(res.Groups[0].Files) != 1 || len(res.Skipped) != 0 {
				t.Fatalf("groups %+v skipped %v", res.Groups, res.Skipped)
			}
			f := res.Groups[0].Files[0]
			if f.Season != tc.wantSeason || !reflect.DeepEqual(f.Episodes, tc.wantEpisodes) || !reflect.DeepEqual(f.Absolute, tc.wantAbsolute) {
				t.Fatalf("season %d episodes %v absolute %v, want %d %v %v", f.Season, f.Episodes, f.Absolute, tc.wantSeason, tc.wantEpisodes, tc.wantAbsolute)
			}
		})
	}
}

func TestAbsoluteNumbers(t *testing.T) {
	tests := []struct {
		name string
		want []int
	}{
		{"One Piece - S23E10 - 1100 - Title.mkv", []int{1100}},
		{"[Group] One Piece - 1100 [1080p].mkv", []int{1100}},
		{"Show.S01E05.1080p.mkv", nil},
		{"Show.S00E05.mkv", nil},
		{"Movie (2010).mkv", nil},
	}
	for _, tc := range tests {
		if got := libimport.AbsoluteNumbers(tc.name); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("AbsoluteNumbers(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}
