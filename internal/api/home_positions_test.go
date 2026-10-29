package api

import "testing"

func TestPositionFinished(t *testing.T) {
	cases := []struct {
		item     string
		pos, dur float64
		want     bool
	}{
		{"movie:1", 5, 7200, true},        // barely begun
		{"movie:1", 1800, 7200, false},    // part-way
		{"movie:1", 6900, 7200, true},     // into the credits
		{"episode:2", 1290, 1320, true},   // the last half minute
		{"title:tv:1:1:2", 600, 0, false}, // no length known
		{"comic:3", 1, 24, true},          // the first page
		{"comic:3", 12, 24, false},
		{"comic:3", 24, 24, true}, // read to the end
	}
	for _, c := range cases {
		if got := positionFinished(c.item, c.pos, c.dur); got != c.want {
			t.Errorf("positionFinished(%q, %v, %v) = %v, want %v", c.item, c.pos, c.dur, got, c.want)
		}
	}
	for _, ok := range []string{"movie:12", "title:tv:603:1:2", "sport:2052711", "comic:9"} {
		if !positionItemRe.MatchString(ok) {
			t.Errorf("%q should be a place", ok)
		}
	}
	for _, bad := range []string{"channel:1", "movie:", "music:3", "movie:1 OR 1=1"} {
		if positionItemRe.MatchString(bad) {
			t.Errorf("%q shouldn't be a place", bad)
		}
	}
}
