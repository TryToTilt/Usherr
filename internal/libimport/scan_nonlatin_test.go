package libimport_test

import (
	"testing"

	"github.com/rdborg/mediarium/internal/libimport"
)

func TestScanMoviesNonLatinTitlesStaySeparate(t *testing.T) {
	root := t.TempDir()
	touch(t, root, "千と千尋の神隠し (2001)/千と千尋の神隠し (2001).mkv", 10)
	touch(t, root, "もののけ姫 (2001)/もののけ姫 (2001).mkv", 10)
	touch(t, root, "Брат (2001)/Брат (2001).mkv", 10)
	touch(t, root, "霸王别姬 (2001)/霸王别姬 (2001).mkv", 10)
	touch(t, root, "Αθήνα (2001)/Αθήνα (2001).mkv", 10)
	res, err := libimport.ScanMovies(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Groups) != 5 || len(res.Skipped) != 0 {
		t.Fatalf("want 5 groups, got %d: %+v skipped %v", len(res.Groups), res.Groups, res.Skipped)
	}
}

func TestScanTVNonLatinShowsOfOneYearStaySeparate(t *testing.T) {
	root := t.TempDir()
	touch(t, root, "進撃の巨人 (2013)/Season 01/進撃の巨人.S01E01.mkv", 10)
	touch(t, root, "鬼滅の刃 (2013)/Season 01/鬼滅の刃.S01E01.mkv", 10)
	res, err := libimport.ScanTV(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Groups) != 2 || len(res.Skipped) != 0 {
		t.Fatalf("want 2 groups and nothing skipped, got %+v skipped %v", res.Groups, res.Skipped)
	}
}
