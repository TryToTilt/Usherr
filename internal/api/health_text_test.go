package api

import "testing"

func TestRecentFailuresTitle(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{1, "1 download failed in the last 24 hours"},
		{3, "3 downloads failed in the last 24 hours"},
	}
	for _, c := range cases {
		if got := recentFailuresTitle(c.n); got != c.want {
			t.Errorf("recentFailuresTitle(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}
