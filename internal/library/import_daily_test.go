package library

import (
	"reflect"
	"testing"
)

func TestPlaceByAirDate(t *testing.T) {
	episodes := []Episode{
		{Season: 29, Episode: 1, AirDate: "2024-01-15"},
		{Season: 29, Episode: 2, AirDate: "2024-01-16"},
		{Season: 29, Episode: 4, AirDate: "2024-01-17"},
		{Season: 29, Episode: 3, AirDate: "2024-01-17"},
		{Season: 0, Episode: 1, AirDate: "2024-01-18"}, // a special is never a daily episode
	}
	files := []ImportFile{
		{Path: "a", AirDate: "2024-01-16"},
		{Path: "b", AirDate: "2024-01-17", DailyPart: 2},
		{Path: "c", AirDate: "2024-01-17"}, // two episodes that day, and no part: not guessed
		{Path: "d", AirDate: "2023-12-31"}, // not listed
		{Path: "e", AirDate: "2024-01-18"},
		{Path: "f", Season: 1, Episodes: []int{3}},
	}
	got := PlaceByAirDate(files, episodes)
	want := []ImportFile{
		{Path: "a", AirDate: "2024-01-16", Season: 29, Episodes: []int{2}},
		{Path: "b", AirDate: "2024-01-17", DailyPart: 2, Season: 29, Episodes: []int{4}},
		{Path: "c", AirDate: "2024-01-17", Season: -1, Episodes: []int{0}},
		{Path: "d", AirDate: "2023-12-31", Season: -1, Episodes: []int{0}},
		{Path: "e", AirDate: "2024-01-18", Season: -1, Episodes: []int{0}},
		{Path: "f", Season: 1, Episodes: []int{3}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
	if files[0].Season != 0 {
		t.Fatal("the files passed in are not changed")
	}

	// The files left without an episode are counted as left out, never placed.
	listed := map[[2]int]bool{}
	for _, e := range episodes {
		listed[[2]int{e.Season, e.Episode}] = true
	}
	has := func(s, e int) bool { return listed[[2]int{s, e}] }
	for _, f := range got[2:5] {
		if _, ok, _ := resolveImportFile(f, has, AbsoluteOrder(episodes), true, nil); ok {
			t.Errorf("%s should stay unplaced", f.Path)
		}
	}

	// Change folder uses the placer, which reads the date too.
	place := ImportPlacer(episodes, false, nil)
	if s, eps, ok := place(ImportFile{AirDate: "2024-01-15"}); !ok || s != 29 || !reflect.DeepEqual(eps, []int{1}) {
		t.Fatalf("placer: %d %v %v", s, eps, ok)
	}
	if _, _, ok := place(ImportFile{AirDate: "2023-12-31"}); ok {
		t.Fatal("a day the show doesn't list is not placed")
	}
}
