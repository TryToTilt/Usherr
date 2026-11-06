package musicbrainz

import "testing"

func rel(id, status, country, date string, tracks ...int) Release {
	r := Release{ID: id, Status: status, Country: country, Date: date}
	for i, n := range tracks {
		r.Media = append(r.Media, Medium{Position: i + 1, TrackCount: n})
	}
	return r
}

func TestEditionForPicksTheNearestTrackCount(t *testing.T) {
	releases := []Release{
		rel("std-gb", "Official", "GB", "1997-05-21", 12),
		rel("std-xw", "Official", "XW", "1997-05-21", 12),
		rel("deluxe", "Official", "XW", "2009-03-24", 12, 15),
		rel("jp", "Official", "JP", "1997-05-28", 13),
		rel("boot", "Bootleg", "XW", "1996", 13),
		rel("none", "Official", "XW", "1997"),
	}
	for _, c := range []struct {
		files int
		want  string
	}{
		{12, "std-xw"}, // same date and count: worldwide wins
		{13, "jp"},     // the Japanese bonus track, Official before the bootleg
		{27, "deluxe"},
		{30, "deluxe"},
		{1, "std-xw"},
	} {
		got, ok := EditionFor(releases, c.files)
		if !ok || got.ID != c.want {
			t.Errorf("%d files: got %q, want %q", c.files, got.ID, c.want)
		}
	}
	if _, ok := EditionFor([]Release{rel("none", "Official", "XW", "1997")}, 10); ok {
		t.Error("a release with no track count was picked")
	}
}

func TestEditionForPrefersAPreciseDate(t *testing.T) {
	got, _ := EditionFor([]Release{rel("year", "Official", "US", "2001", 10), rel("day", "Official", "US", "2001-05-02", 10)}, 10)
	if got.ID != "day" {
		t.Fatalf("got %q", got.ID)
	}
}
