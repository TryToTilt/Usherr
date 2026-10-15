package api

import (
	"testing"

	"github.com/rdborg/mediarium/internal/queue"
)

func TestEpisodeSearch(t *testing.T) {
	ev := []queue.Event{
		{Kind: "searched", Message: "Searched for S02E13: none acceptable", At: "5"},
		{Kind: "grabbed", Message: "Added something", At: "4"},
		{Kind: "searched", Message: "Searched for season 1: 300 releases", At: "3"},
		{Kind: "searched", Message: "Searched for S01E01: 12 releases", At: "2"},
		{Kind: "searched", Message: "Searched for La Brea", At: "1"},
	}
	cases := []struct {
		s, e int
		want string
	}{{2, 13, "5"}, {1, 1, "3"}, {1, 5, "3"}, {3, 1, "1"}}
	for _, c := range cases {
		if _, at := episodeSearch(ev, c.s, c.e); at != c.want {
			t.Errorf("S%dE%d: got event %s, want %s", c.s, c.e, at, c.want)
		}
	}
}
