package libimport_test

import (
	"testing"

	"github.com/rdborg/mediarium/internal/libimport"
)

// Daily shows are named by the day an episode aired, not by season and
// episode. Their files are kept with that date, to be placed on the show's
// episode of that day when the import runs.
func TestScanTVReadsDailyShowsByAirDate(t *testing.T) {
	root := t.TempDir()
	touch(t, root, "The Daily Show (1996)/Season 2024/The Daily Show - 2024-01-15 - Guest.mkv", 10)
	touch(t, root, "The Daily Show (1996)/Season 2024/The Daily Show - 2024-01-16 - Other Guest.mkv", 10)
	touch(t, root, "The Tonight Show Starring Jimmy Fallon (2014)/Season 2024/The.Tonight.Show.Starring.Jimmy.Fallon.2024.03.05.720p.WEB.mkv", 10)
	touch(t, root, "The Tonight Show Starring Jimmy Fallon (2014)/Season 2024/The.Tonight.Show.Starring.Jimmy.Fallon.2024.03.05.1080p.WEB.mkv", 20)
	res, err := libimport.ScanTV(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Groups) != 2 {
		t.Fatalf("groups: %+v (skipped %v)", res.Groups, res.Skipped)
	}
	byTitle := map[string]libimport.Group{}
	for _, g := range res.Groups {
		byTitle[g.Title] = g
	}
	daily := byTitle["The Daily Show"]
	if daily.Year != 1996 || len(daily.Files) != 2 || daily.Files[0].AirDate != "2024-01-15" || daily.Files[1].AirDate != "2024-01-16" {
		t.Fatalf("daily show: %+v", daily)
	}
	if f := daily.Files[0]; f.Season != 0 || len(f.Episodes) != 0 {
		t.Fatalf("a date-named file has no season or episode yet: %+v", f)
	}
	// Two copies of the same day keep the bigger one, as for episodes.
	fallon := byTitle["The Tonight Show Starring Jimmy Fallon"]
	if fallon.Year != 2014 || len(fallon.Files) != 1 || fallon.Files[0].AirDate != "2024-03-05" || fallon.Files[0].Quality != "WEBDL-1080p" {
		t.Fatalf("tonight show: %+v", fallon)
	}
	if len(res.Skipped) != 1 {
		t.Fatalf("skipped: %v", res.Skipped)
	}
}

func TestScanSeriesFolderReadsAirDates(t *testing.T) {
	root := t.TempDir()
	touch(t, root, "Season 2024/The Daily Show - 2024-01-15 - Guest.mkv", 10)
	files, skipped, err := libimport.ScanSeriesFolder(root)
	if err != nil || len(skipped) != 0 || len(files) != 1 || files[0].AirDate != "2024-01-15" {
		t.Fatalf("files %+v skipped %v err %v", files, skipped, err)
	}
}
