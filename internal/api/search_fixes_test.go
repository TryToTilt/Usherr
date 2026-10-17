package api_test

import (
	"testing"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/queue"
)

// "Search now" after adding a show looks only for the episodes that are
// monitored: choosing "future episodes" or "none" must not fetch the back catalogue.
func TestSearchSeriesNowKeepsTheMonitoredBoxes(t *testing.T) {
	env := newTVAutoEnv(t, []string{s1e1WEB, s1e2WEB, sPack, otherE01}, wholeSeasonMissing)
	if err := env.server.MovieRepo.SetAllEpisodesMonitored(env.seriesID, false); err != nil {
		t.Fatal(err)
	}
	env.server.TestSearchSeriesNow(env.seriesID)
	requireTitles(t, env.grabbedTitles(t))

	// Only the second episode is monitored: that one is searched for, the others are not.
	eps, err := env.server.MovieRepo.ListEpisodes(env.seriesID)
	if err != nil {
		t.Fatal(err)
	}
	if err := env.server.MovieRepo.SetEpisodeMonitored(eps[1].ID, true); err != nil {
		t.Fatal(err)
	}
	env.server.TestSearchSeriesNow(env.seriesID)
	requireTitles(t, env.grabbedTitles(t), s1e2WEB)
	env.finish(t)
}

// A hunt that can only afford a few searches starts with the shows searched for
// longest ago, so the same show does not take them all every time.
func TestHuntOrderPutsTheLeastRecentlySearchedShowFirst(t *testing.T) {
	env := newTVAutoEnv(t, nil, wholeSeasonMissing)
	other, err := env.server.MovieRepo.AddSeries(library.Series{TMDBID: 2, Title: "Second Show", Year: 2021, Monitored: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	third, err := env.server.MovieRepo.AddSeries(library.Series{TMDBID: 3, Title: "Third Show", Year: 2022, Monitored: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ids := []int64{env.seriesID, other.ID, third.ID}
	if got := env.server.TestSearchOrder(ids); got[0] != ids[0] || got[1] != ids[1] || got[2] != ids[2] {
		t.Fatalf("with no searches yet the order should stay as it is, got %v", got)
	}
	// The first show was searched for just now, the second a while ago.
	log := func(id int64, at string) {
		t.Helper()
		if err := env.server.QueueRepo.LogItemEvent(queue.ItemEvent{SeriesID: id, Kind: "searched", Message: "Searched"}); err != nil {
			t.Fatal(err)
		}
		if err := env.server.TestSetSearchTime(id, at); err != nil {
			t.Fatal(err)
		}
	}
	log(env.seriesID, "2026-10-16 10:00:00")
	log(other.ID, "2026-10-14 10:00:00")
	got := env.server.TestSearchOrder(ids)
	if got[0] != third.ID || got[1] != other.ID || got[2] != env.seriesID {
		t.Fatalf("order = %v, want the never-searched show, then the older search, then the newest (%d, %d, %d)", got, third.ID, other.ID, env.seriesID)
	}
}
