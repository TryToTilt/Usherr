package api

import (
	"testing"

	"github.com/rdborg/mediarium/internal/indexers"
)

func TestLowerIndexerNumberWinsATie(t *testing.T) {
	u := indexers.ProtocolUsenet
	tor := indexers.ProtocolTorrent
	cases := []struct {
		name            string
		best, candidate indexers.Result
		want            bool
	}{
		{"a lower number beats the default", indexers.Result{Protocol: u, Priority: 25}, indexers.Result{Protocol: u, Priority: 10}, true},
		{"a higher number never replaces the default", indexers.Result{Protocol: tor, Priority: 25}, indexers.Result{Protocol: u, Priority: 40}, false},
		{"1 beats 2", indexers.Result{Protocol: u, Priority: 2}, indexers.Result{Protocol: u, Priority: 1}, true},
		{"50 loses to 49", indexers.Result{Protocol: u, Priority: 49}, indexers.Result{Protocol: u, Priority: 50}, false},
		{"priority goes before Usenet over torrents", indexers.Result{Protocol: u, Priority: 25}, indexers.Result{Protocol: tor, Priority: 10}, true},
		{"same priority: Usenet wins over a torrent", indexers.Result{Protocol: tor, Priority: 25}, indexers.Result{Protocol: u, Priority: 25}, true},
		{"not set counts as 25", indexers.Result{Protocol: tor}, indexers.Result{Protocol: u, Priority: 25}, true},
		{"out of range counts as 25", indexers.Result{Protocol: tor, Priority: 25}, indexers.Result{Protocol: u, Priority: 99}, true},
	}
	for _, tc := range cases {
		if got := preferUsenet(&tc.best, &tc.candidate); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
