package library_test

import (
	"testing"

	"github.com/rdborg/mediarium/internal/library"
)

func TestApplyMonitorMode(t *testing.T) {
	const today = "2026-10-18"
	eps := []library.Episode{
		{Season: 0, Episode: 1, Title: "Special", AirDate: "2020-01-01"}, // 1: a special, starts unmonitored
		{Season: 1, Episode: 1, AirDate: "2024-01-01"},                   // 2: aired
		{Season: 1, Episode: 2, AirDate: "2024-01-08"},                   // 3: aired and has a file
		{Season: 2, Episode: 1, AirDate: today},                          // 4: airs today
		{Season: 2, Episode: 2, AirDate: "2027-01-01"},                   // 5: not yet
		{Season: 2, Episode: 3},                                          // 6: no air date
	}
	// before lists the episodes (numbered as above) that start unmonitored;
	// want is the monitored flag of episodes 1 to 6 afterwards.
	cases := []struct {
		name   string
		before []int
		mode   string
		want   [6]bool
		want2  library.MonitorChange
	}{
		{"future stops the aired ones, keeps today, unaired and undated", nil, "future", [6]bool{false, false, false, true, true, true}, library.MonitorChange{Stopped: 2}},
		{"future also switches unaired ones back on", []int{4, 5, 6}, "future", [6]bool{false, false, false, true, true, true}, library.MonitorChange{Stopped: 2, Started: 3}},
		{"none stops everything", []int{2}, "none", [6]bool{false, false, false, false, false, false}, library.MonitorChange{Stopped: 4}},
		{"all monitors every real episode and leaves specials", []int{2, 3, 4, 5, 6}, "all", [6]bool{false, true, true, true, true, true}, library.MonitorChange{Started: 5}},
		{"all with nothing to change", nil, "all", [6]bool{false, true, true, true, true, true}, library.MonitorChange{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo := newRepo(t)
			s, err := repo.AddSeries(library.Series{TMDBID: 1, Title: "Show", Year: 2020, Monitored: true}, eps)
			if err != nil {
				t.Fatal(err)
			}
			list, _ := repo.ListEpisodes(s.ID)
			if err := repo.SetEpisodeStatus(list[2].ID, library.StatusDownloaded, "WEBDL-1080p", ""); err != nil {
				t.Fatal(err)
			}
			for _, n := range c.before {
				if err := repo.SetEpisodeMonitored(list[n-1].ID, false); err != nil {
					t.Fatal(err)
				}
			}
			dry, err := repo.ApplyMonitorMode(s.ID, c.mode, today, true)
			if err != nil || dry != c.want2 {
				t.Fatalf("dry run: got %+v, %v; want %+v", dry, err, c.want2)
			}
			untouched, _ := repo.ListEpisodes(s.ID)
			for i, e := range untouched {
				if wasOff := i == 0 || contains(c.before, i+1); e.Monitored == wasOff {
					t.Fatalf("a dry run changed episode %d", i+1)
				}
			}
			n, err := repo.ApplyMonitorMode(s.ID, c.mode, today, false)
			if err != nil || n != c.want2 {
				t.Fatalf("apply: got %+v, %v; want %+v", n, err, c.want2)
			}
			after, _ := repo.ListEpisodes(s.ID)
			for i, e := range after {
				if e.Monitored != c.want[i] {
					t.Errorf("episode %d: monitored=%v, want %v", i+1, e.Monitored, c.want[i])
				}
			}
			if after[2].Status != library.StatusDownloaded {
				t.Errorf("the episode with a file changed status: %s", after[2].Status)
			}
		})
	}
	if _, err := newRepo(t).ApplyMonitorMode(1, "sometimes", today, false); err == nil {
		t.Error("an unknown mode should be an error")
	}
}

func contains(l []int, n int) bool {
	for _, v := range l {
		if v == n {
			return true
		}
	}
	return false
}
