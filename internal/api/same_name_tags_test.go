package api

import (
	"testing"

	"github.com/rdborg/mediarium/internal/library"
)

func TestSameNameTags(t *testing.T) {
	who := library.Series{ID: 50, TMDBID: 5, Title: "Doctor Who", Year: 1963, OriginCountry: []string{"GB"}}
	who2 := library.Series{ID: 51, TMDBID: 6, Title: "Doctor Who", Year: 2005, OriginCountry: []string{"GB"}}
	lonely := library.Series{ID: 60, TMDBID: 7, Title: "Alone", Year: 2010, OriginCountry: []string{"US"}}
	sameBoth := library.Series{ID: 70, TMDBID: 8, Title: "Twin", Year: 2000}
	sameBoth2 := library.Series{ID: 71, TMDBID: 9, Title: "Twin", Year: 2000}
	copyUS := shamelessUS
	copyUS.ID, copyUS.CopyOf = 80, shamelessUS.ID

	for _, tc := range []struct {
		name string
		in   []library.Series
		want map[int64]string
	}{
		{"countries differ", []library.Series{shamelessUS, shamelessUK, lonely}, map[int64]string{2: "US", 1: "UK"}},
		{"same country, years differ", []library.Series{who, who2}, map[int64]string{50: "1963", 51: "2005"}},
		{"country not read for one, years differ", []library.Series{officeNoCountry, officeUK}, map[int64]string{5: "2005", 4: "2001"}},
		{"nothing tells them apart", []library.Series{sameBoth, sameBoth2}, map[int64]string{}},
		{"a copy is not another show", []library.Series{shamelessUS, copyUS}, map[int64]string{}},
		{"all different names", []library.Series{lonely, who}, map[int64]string{}},
	} {
		got := sameNameTags(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
			continue
		}
		for id, tag := range tc.want {
			if got[id] != tag {
				t.Errorf("%s: show %d tag %q, want %q", tc.name, id, got[id], tag)
			}
		}
	}
}
