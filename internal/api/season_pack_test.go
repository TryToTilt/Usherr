package api_test

import (
	"context"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/blocklist"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/settings"
)

const (
	packBlu = "Fixture.Show.S01.1080p.BluRay.x264-GRP"
	packWeb = "Fixture.Show.S01.1080p.WEB-DL.x264-GRP"
	pack720 = "Fixture.Show.S01.720p.WEB-DL.x264-GRP"
)

func days(n int) string { return time.Now().AddDate(0, 0, n).Format("2006-01-02") }

// A season of three episodes that finished long ago.
func endedSeason(status [3]library.Status, q [3]string) []episodeSpec {
	return []episodeSpec{
		{1, 1, "2020-01-01", status[0], q[0]},
		{1, 2, "2020-01-08", status[1], q[1]},
		{1, 3, "2020-01-15", status[2], q[2]},
	}
}

var (
	oneMissingWeb = endedSeason([3]library.Status{library.StatusMissing, library.StatusDownloaded, library.StatusDownloaded}, [3]string{"", "WEBDL-1080p", "WEBDL-1080p"})
	oneMissingBlu = endedSeason([3]library.Status{library.StatusMissing, library.StatusDownloaded, library.StatusDownloaded}, [3]string{"", "Bluray-1080p", "Bluray-1080p"})
	allWeb720     = endedSeason([3]library.Status{library.StatusDownloaded, library.StatusDownloaded, library.StatusDownloaded}, [3]string{"WEBDL-720p", "WEBDL-720p", "WEBDL-720p"})
	allWeb1080    = endedSeason([3]library.Status{library.StatusDownloaded, library.StatusDownloaded, library.StatusDownloaded}, [3]string{"WEBDL-1080p", "WEBDL-1080p", "WEBDL-1080p"})
	allBlu        = endedSeason([3]library.Status{library.StatusDownloaded, library.StatusDownloaded, library.StatusDownloaded}, [3]string{"Bluray-1080p", "Bluray-1080p", "Bluray-1080p"})
)

func TestHuntTVSeasonPacksOnceASeasonHasEnded(t *testing.T) {
	stillAiring := []episodeSpec{
		{1, 1, "2020-01-01", library.StatusMissing, ""},
		{1, 2, "2020-01-08", library.StatusDownloaded, "WEBDL-1080p"},
		{1, 3, days(-1), library.StatusDownloaded, "WEBDL-1080p"}, // aired yesterday: the pack is not out yet
	}
	cases := []struct {
		name     string
		global   bool
		show     string // the show's own choice
		specs    []episodeSpec
		titles   []string
		upgrades bool
		prep     func(t *testing.T, env *tvAutoEnv)
		want     []string
	}{
		{name: "setting off does nothing", specs: oneMissingWeb, titles: []string{packBlu, s1e1WEB}, want: []string{s1e1WEB}},
		{name: "season not ended yet: episodes one by one", global: true, specs: stillAiring, titles: []string{packBlu, s1e1WEB}, want: []string{s1e1WEB}},
		{name: "ended, an episode missing, better pack", global: true, specs: oneMissingWeb, titles: []string{packBlu, s1e1WEB}, want: []string{packBlu}},
		{name: "ended, an episode missing, same quality pack", global: true, specs: oneMissingWeb, titles: []string{packWeb, s1e1WEB}, want: []string{packWeb}},
		{name: "ended, pack worse than the files: not taken, the episode is", global: true, specs: oneMissingBlu, titles: []string{packWeb, s1e1WEB}, want: []string{s1e1WEB}},
		{name: "ended, no pack to be found: the episode is taken", global: true, specs: oneMissingWeb, titles: []string{s1e1WEB}, want: []string{s1e1WEB}},
		{name: "show says on, setting off", show: library.SeasonPackOn, specs: oneMissingWeb, titles: []string{packBlu, s1e1WEB}, want: []string{packBlu}},
		{name: "show says off, setting on", global: true, show: library.SeasonPackOff, specs: oneMissingWeb, titles: []string{packBlu, s1e1WEB}, want: []string{s1e1WEB}},
		{name: "upgrade by pack when the files are below the cutoff", global: true, upgrades: true, specs: allWeb720, titles: []string{packBlu, s1e1Blu}, want: []string{packBlu}},
		{name: "a pack that is no better than the files is no upgrade", global: true, upgrades: true, specs: allWeb720, titles: []string{pack720, s1e1WEB}, want: []string{s1e1WEB}},
		{name: "files at the cutoff are left alone", global: true, upgrades: true, specs: allBlu, titles: []string{packBlu, s1e1Blu}, want: nil},
		{name: "upgrades off: complete season left alone", global: true, specs: allWeb1080, titles: []string{packBlu}, want: nil},
		{
			name: "a pack is already coming for the season", global: true, specs: oneMissingWeb, titles: []string{packBlu, s1e1WEB},
			prep: func(t *testing.T, env *tvAutoEnv) { setEpisodeStatus(t, env, 1, 1, library.StatusDownloading) },
			want: nil,
		},
		{
			name: "the season is covered by a pack already", global: true, specs: oneMissingWeb, titles: []string{packBlu, s1e1WEB},
			prep: func(t *testing.T, env *tvAutoEnv) {
				for _, n := range []int{2, 3} {
					setEpisodeRelease(t, env, 1, n, packWeb)
				}
			},
			want: []string{s1e1WEB},
		},
		{
			name: "the pack is on the blocklist", global: true, specs: oneMissingWeb, titles: []string{packBlu, s1e1WEB},
			prep: func(t *testing.T, env *tvAutoEnv) {
				if err := env.server.Blocklist.Add(blocklist.Entry{ReleaseTitle: packBlu, Reason: "bad"}); err != nil {
					t.Fatal(err)
				}
			},
			want: []string{s1e1WEB},
		},
		{
			name: "searched for a day ago: not again yet", global: true, specs: oneMissingWeb, titles: []string{packBlu, s1e1WEB},
			prep: func(t *testing.T, env *tvAutoEnv) { notePackSearch(t, env, 1, -24*time.Hour) },
			want: []string{s1e1WEB},
		},
		{
			name: "searched for eight days ago: time to look again", global: true, specs: oneMissingWeb, titles: []string{packBlu, s1e1WEB},
			prep: func(t *testing.T, env *tvAutoEnv) { notePackSearch(t, env, 1, -8*24*time.Hour) },
			want: []string{packBlu},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newTVAutoEnv(t, tc.titles, tc.specs)
			if tc.global {
				if err := env.server.Settings.Set(settings.KeySeasonPacks, "1", false); err != nil {
					t.Fatal(err)
				}
			}
			if tc.show != "" {
				if err := env.server.MovieRepo.SetSeriesSeasonPack(env.seriesID, tc.show); err != nil {
					t.Fatal(err)
				}
			}
			if tc.upgrades {
				enableUpgrades(t, env.server)
			}
			if tc.prep != nil {
				tc.prep(t, env)
			}
			env.server.TestHunt(context.Background())
			requireTitles(t, env.grabbedTitles(t), tc.want...)
			env.finish(t)
		})
	}
}

// A search for a pack that finds none is remembered, and the next run does not
// ask again; RSS sync never looks for packs.
func TestHuntTVRemembersASeasonPackSearchThatFoundNothing(t *testing.T) {
	env := newTVAutoEnv(t, []string{s1e1WEB}, oneMissingWeb)
	if err := env.server.Settings.Set(settings.KeySeasonPacks, "1", false); err != nil {
		t.Fatal(err)
	}
	env.server.TestRSSSync(context.Background())
	if got, _ := env.server.MovieRepo.SeasonPackSearches(env.seriesID); len(got) != 0 {
		t.Fatalf("RSS sync noted a pack search: %v", got)
	}
	env.finish(t)

	env = newTVAutoEnv(t, []string{s1e1WEB}, oneMissingWeb)
	if err := env.server.Settings.Set(settings.KeySeasonPacks, "1", false); err != nil {
		t.Fatal(err)
	}
	env.server.TestHunt(context.Background())
	first, err := env.server.MovieRepo.SeasonPackSearches(env.seriesID)
	if err != nil || first[1].At == "" {
		t.Fatalf("the search for a season 1 pack was not remembered: %v %v", first, err)
	}
	env.server.TestHunt(context.Background())
	second, _ := env.server.MovieRepo.SeasonPackSearches(env.seriesID)
	if second[1].At != first[1].At {
		t.Fatalf("searched again straight away: %s then %s", first[1].At, second[1].At)
	}
	env.finish(t)
}

func setEpisodeStatus(t *testing.T, env *tvAutoEnv, season, episode int, status library.Status) {
	t.Helper()
	ep, err := env.server.MovieRepo.GetEpisode(env.seriesID, season, episode)
	if err != nil {
		t.Fatal(err)
	}
	if err := env.server.MovieRepo.SetEpisodeStatus(ep.ID, status, "", ""); err != nil {
		t.Fatal(err)
	}
}

func setEpisodeRelease(t *testing.T, env *tvAutoEnv, season, episode int, release string) {
	t.Helper()
	ep, err := env.server.MovieRepo.GetEpisode(env.seriesID, season, episode)
	if err != nil {
		t.Fatal(err)
	}
	if err := env.server.MovieRepo.SetEpisodeReleaseTitle(ep.ID, release); err != nil {
		t.Fatal(err)
	}
}

func notePackSearch(t *testing.T, env *tvAutoEnv, season int, ago time.Duration) {
	t.Helper()
	at := time.Now().Add(ago).UTC().Format(time.RFC3339)
	if err := env.server.MovieRepo.NoteSeasonPackSearch(env.seriesID, season, at, 3, false); err != nil {
		t.Fatal(err)
	}
}
