package api_test

import (
	"context"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/settings"
)

// A season that has found no pack six times in a row is not searched for
// again by the weekly hunt; the episodes are still taken one by one.
func TestHuntTVGivesUpOnASeasonPackAfterSixEmptySearches(t *testing.T) {
	env := newTVAutoEnv(t, []string{packBlu, s1e1WEB}, oneMissingWeb)
	if err := env.server.Settings.Set(settings.KeySeasonPacks, "1", false); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		notePackSearch(t, env, 1, -8*24*time.Hour)
	}
	got, _ := env.server.MovieRepo.SeasonPackSearches(env.seriesID)
	if got[1].Tries != 6 || got[1].Episodes != 3 {
		t.Fatalf("remembered %+v, want six tries on a season of three episodes", got[1])
	}
	env.server.TestHunt(context.Background())
	requireTitles(t, env.grabbedTitles(t), s1e1WEB)
	after, _ := env.server.MovieRepo.SeasonPackSearches(env.seriesID)
	if after[1] != got[1] {
		t.Fatalf("the season was searched for a pack again: %+v then %+v", got[1], after[1])
	}
	env.finish(t)
}

// Changing the show's pack choice lets a season that was given up on be
// searched for a pack again.
func TestHuntTVSearchesAgainForPacksAfterTheChoiceChanged(t *testing.T) {
	env := newTVAutoEnv(t, []string{packBlu, s1e1WEB}, oneMissingWeb)
	if err := env.server.Settings.Set(settings.KeySeasonPacks, "1", false); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		notePackSearch(t, env, 1, -8*24*time.Hour)
	}
	if err := env.server.MovieRepo.SetSeriesSeasonPack(env.seriesID, library.SeasonPackOn); err != nil {
		t.Fatal(err)
	}
	env.server.TestHunt(context.Background())
	requireTitles(t, env.grabbedTitles(t), packBlu)
	env.finish(t)
}

// The count of empty searches goes up by one a search, starts again when the
// season has another number of episodes, and is cleared when a pack was taken.
func TestNoteSeasonPackSearchCountsEmptySearches(t *testing.T) {
	env := newTVAutoEnv(t, []string{s1e1WEB}, oneMissingWeb)
	repo := env.server.MovieRepo
	at := func(daysAgo int) string { return time.Now().AddDate(0, 0, -daysAgo).UTC().Format(time.RFC3339) }
	note := func(daysAgo, episodes int, found bool) library.PackSearch {
		t.Helper()
		if err := repo.NoteSeasonPackSearch(env.seriesID, 1, at(daysAgo), episodes, found); err != nil {
			t.Fatal(err)
		}
		got, err := repo.SeasonPackSearches(env.seriesID)
		if err != nil {
			t.Fatal(err)
		}
		return got[1]
	}
	for i, want := range []int{1, 2, 3} {
		if got := note(30-i*7, 3, false); got.Tries != want {
			t.Fatalf("search %d: tries = %d, want %d", i+1, got.Tries, want)
		}
	}
	if got := note(2, 4, false); got.Tries != 1 || got.Episodes != 4 {
		t.Fatalf("a new episode should start the count again: %+v", got)
	}
	note(1, 4, false)
	if got := note(0, 4, true); got.Tries != 0 {
		t.Fatalf("a pack was taken, the count should be cleared: %+v", got)
	}
	note(200, 4, false)
	note(100, 4, false) // more than 60 days after the one before: starts again
	if got := note(0, 4, false); got.Tries != 1 {
		t.Fatalf("a search 100 days after the last should start the count again: %+v", got)
	}
	env.finish(t)
}
