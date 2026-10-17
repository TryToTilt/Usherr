package library_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/library/librarytest"
	"github.com/rdborg/mediarium/internal/store"
)

// The counts behind the Dashboard and Wanted, and the file list behind the TV
// list, are read from the indexes made for them: on a big library, reading
// the episodes themselves took seconds.
func TestWantedQueriesUseTheirIndexes(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := librarytest.Fill(db, librarytest.Small); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ name, query, want string }{
		{"wanted shows", library.WantedShowsQuery(true), "COVERING INDEX episodes_wanted"},
		{"wanted episode files", library.WantedEpisodeFilesQuery(true), "COVERING INDEX episodes_wanted"},
		{"wanted episodes", library.WantedEpisodesQuery(library.StatusMissing, 0), "INDEX episodes_wanted"},
		{"files by show", library.EpisodeFilesQuery, "COVERING INDEX episodes_with_file"},
	} {
		plan, err := librarytest.Plan(db, c.query, sampleArgs(c.query)...)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if !strings.Contains(plan, c.want) {
			t.Errorf("%s: plan\n%s\nwant it to use %s", c.name, plan, c.want)
		}
		// The wanted queries start from the shows, so with none monitored
		// nothing else is read.
		for _, line := range strings.Split(plan, "\n") {
			if line == "SCAN e" || strings.HasPrefix(line, "SCAN e ") {
				t.Errorf("%s goes through every episode:\n%s", c.name, plan)
			}
		}
	}
}

// sampleArgs gives a query as many arguments as it has placeholders.
func sampleArgs(q string) []any {
	args := make([]any, strings.Count(q, "?"))
	for i := range args {
		args[i] = "2026-10-09"
	}
	return args
}

// TestScaleTimings builds a big library and times the questions the Dashboard,
// Wanted and the TV list ask. It only runs with MEDIARIUM_SCALE set (a number
// above 1 is the episodes per show; 780 makes 390,000 episodes).
func TestScaleTimings(t *testing.T) {
	librarytest.NeedScale(t)
	ctx := context.Background()
	for _, monitored := range []bool{true, false} {
		db, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
		if err != nil {
			t.Fatal(err)
		}
		sz := librarytest.BigSize()
		sz.Monitored = monitored
		start := time.Now()
		if err := librarytest.Fill(db, sz); err != nil {
			t.Fatal(err)
		}
		t.Logf("monitored=%v: filled %d movies, %d shows, %d episodes in %s", monitored, sz.Movies, sz.Shows, sz.Shows*sz.EpisodesPerShow, time.Since(start).Round(time.Millisecond))
		repo := library.NewRepo(db)
		today := time.Now().UTC().Format("2006-01-02")
		timed := func(name string, f func() int) {
			start := time.Now()
			n := f()
			t.Logf("monitored=%v %-22s %7d in %s", monitored, name, n, time.Since(start).Round(100*time.Microsecond))
		}
		timed("SeriesCounts", func() int { c, _ := repo.SeriesCounts(ctx); return c.Episodes })
		timed("MissingMovies", func() int { n, f, _ := repo.MissingMovies(ctx); return n + len(f) })
		timed("WantedShows", func() int {
			shows, _ := repo.WantedShows(ctx, today, true)
			n := 0
			for _, s := range shows {
				n += s.Episodes
			}
			return n
		})
		timed("WantedEpisodeFiles", func() int { f, _ := repo.WantedEpisodeFiles(ctx, today, true); return len(f) })
		timed("ListWantedEpisodes", func() int { f, _ := repo.ListWantedEpisodes(ctx, library.StatusMissing, today, 0); return len(f) })
		timed("ListWantedEpisodes(1)", func() int { f, _ := repo.ListWantedEpisodes(ctx, library.StatusMissing, today, 1); return len(f) })
		timed("ListSeries", func() int { f, _ := repo.ListSeries(); return len(f) })
		timed("EpisodeFilesBySeries", func() int { f, _ := repo.EpisodeFilesBySeries(); return len(f) })
		// "Look for missing episodes" on every show, while another writer
		// keeps saving small changes: how long did that writer wait at most?
		batch, err := librarytest.ImportedShows(db)
		if err != nil {
			t.Fatal(err)
		}
		stop := make(chan struct{})
		waited := make(chan time.Duration)
		go func() {
			var longest time.Duration
			for {
				select {
				case <-stop:
					waited <- longest
					return
				default:
				}
				start := time.Now()
				if _, err := db.Exec(`UPDATE movies SET title = title WHERE id = 1`); err != nil {
					t.Error(err)
				}
				longest = max(longest, time.Since(start))
				time.Sleep(5 * time.Millisecond)
			}
		}()
		timed("LookForMissing", func() int {
			res, err := repo.StartWatchingImport(batch, false, true)
			if err != nil {
				t.Fatal(err)
			}
			return res.Shows
		})
		close(stop)
		t.Logf("monitored=%v another write waited at most %s meanwhile", monitored, (<-waited).Round(time.Millisecond))
		db.Close()
	}
}
