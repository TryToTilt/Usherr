package library

import (
	"strings"
	"testing"
)

// Each note is one whole sentence (or two), with the count the only part
// filled in, so the screen can find its translation.
func TestLeftOutNote(t *testing.T) {
	cases := []struct {
		unknown                   int
		byAbsolute, byNumber, ani bool
		want                      string
	}{
		{0, true, true, true, ""},
		{1, false, false, false, "Left out 1 episode that the movie database doesn't list for this show."},
		{26, false, true, false, "Left out 26 episodes that the movie database doesn't list for this show."},
		{1, true, false, false, "Left out 1 episode that the movie database doesn't list for this show. Its name carries"},
		{55, true, false, false, "Left out 55 episodes that the movie database doesn't list for this show. Their names carry"},
		{1, true, false, true, "Left out 1 episode that the movie database doesn't list for this show. Its number doesn't match"},
		{3, false, true, true, "Left out 3 episodes that the movie database doesn't list for this show. Their numbers don't match"},
	}
	for _, tc := range cases {
		got := leftOutNote(tc.unknown, tc.byAbsolute, tc.byNumber, tc.ani)
		if (tc.want == "") != (got == "") || !strings.HasPrefix(got, tc.want) {
			t.Errorf("leftOutNote(%d, %v, %v, %v) = %q, want it to start %q", tc.unknown, tc.byAbsolute, tc.byNumber, tc.ani, got, tc.want)
		}
	}
}
