package api

import (
	"reflect"
	"testing"

	"github.com/rdborg/mediarium/internal/manga"
	"github.com/rdborg/mediarium/internal/torrentclient"
)

// A pack leaves out the volumes the library has or doesn't want; files that
// don't say which volume they are, and volumes it doesn't list, stay in.
func TestMangaPackSkipsWhatYouHave(t *testing.T) {
	files := []torrentclient.FileState{
		{Path: "Berserk Vol. 1-41 (ENG)/Berserk v01.cbz"},
		{Path: "Berserk Vol. 1-41 (ENG)/Berserk v02.cbz"},
		{Path: "Berserk Vol. 1-41 (ENG)/Berserk v03.zip"},
		{Path: "Berserk Vol. 1-41 (ENG)/Berserk v41.cbz"},
		{Path: "Berserk Vol. 1-41 (ENG)/Berserk v42.cbz"},
		{Path: "Berserk Vol. 1-41 (ENG)/cover.jpg"},
		{Path: "Berserk Vol. 1-41 (ENG)/Extras.cbz"},
	}
	vols := []manga.Volume{
		{Number: "1", Status: manga.StatusDownloaded, Wanted: true},
		{Number: "2", Status: manga.StatusMissing, Wanted: false},
		{Number: "3", Status: manga.StatusDownloaded, Wanted: true},
		{Number: "41", Status: manga.StatusMissing, Wanted: true},
	}
	got := mangaSkipsFor(files, []string{"Berserk"}, vols, "")
	want := []string{
		"Berserk Vol. 1-41 (ENG)/Berserk v01.cbz",
		"Berserk Vol. 1-41 (ENG)/Berserk v02.cbz",
		"Berserk Vol. 1-41 (ENG)/Berserk v03.zip",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("skips = %q, want %q", got, want)
	}

	// A pack taken to swap one volume keeps only that one.
	got = mangaSkipsFor(files, []string{"Berserk"}, vols, "1")
	want = []string{
		"Berserk Vol. 1-41 (ENG)/Berserk v02.cbz",
		"Berserk Vol. 1-41 (ENG)/Berserk v03.zip",
		"Berserk Vol. 1-41 (ENG)/Berserk v41.cbz",
		"Berserk Vol. 1-41 (ENG)/Berserk v42.cbz",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("swap skips = %q, want %q", got, want)
	}
}

func TestMangaPackLabel(t *testing.T) {
	if got := mangaPackLabel(manga.Parse("Berserk Vol. 1-41 (ENG)")); got != "Volumes 1–41" {
		t.Fatalf("label = %q", got)
	}
	if got := mangaPackLabel(manga.Parse("Berserk v32 (2009) (Digital)")); got != "" {
		t.Fatalf("single volume label = %q", got)
	}
}
