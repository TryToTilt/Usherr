package api

import (
	"fmt"
	"testing"

	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/quality"
)

// parseOnceResults is a feed of releases for one show and a few movies: the
// same release from two indexers, season packs, other shows, odd names.
func parseOnceResults(episodes int) []indexers.Result {
	var res []indexers.Result
	add := func(title string, size int64) {
		res = append(res, indexers.Result{Title: title, IndexerName: "a", DownloadURL: "u/" + title, SizeBytes: size})
	}
	for e := 1; e <= episodes; e++ {
		add(fmt.Sprintf("Show.Name.S01E%02d.1080p.WEB-DL.DDP5.1.x264-GRPA", e), 2<<30)
		add(fmt.Sprintf("Show.Name.S01E%02d.720p.HDTV.x264-GRPB", e), 1<<30)
		add(fmt.Sprintf("Show.Name.S01E%02d.2160p.WEB-DL.x265-GRPC", e), 6<<30)
		add(fmt.Sprintf("Other.Show.S01E%02d.1080p.WEB-DL.x264-GRPA", e), 2<<30)
	}
	add("Show.Name.S01.1080p.BluRay.x264-PACK", 40<<30)
	add("Show.Name.S02E05.2012.Olympics.Special.720p.HDTV.x264-GRP", 1<<30)
	add("Blade.Runner.2049.2017.1080p.BluRay.x264-GRP", 10<<30)
	add("Blade.Runner.2049.1080p.BluRay.x264-GRP", 10<<30)
	add("Movie.Name.2024.1080p.WEB-DL.x264-GRP", 4<<30)
	add("Movie.Name.2019.1080p.WEB-DL.x264-GRP", 4<<30)
	return res
}

func titleOf(r *indexers.Result) string {
	if r == nil {
		return ""
	}
	return r.Title
}

// TestRunParsesEachReleaseOnce shows that picking for many episodes and
// movies out of one feed parses each name once, not once per check, episode
// and profile, and that the pick is the same either way.
func TestRunParsesEachReleaseOnce(t *testing.T) {
	const episodes = 20
	raw := parseOnceResults(episodes)
	series := library.Series{Title: "Show Name", Year: 2020}
	profile := quality.Presets()[quality.Preset1080p]
	movies := []library.Movie{
		{Title: "Blade Runner 2049", Year: 2017},
		{Title: "Movie Name", Year: 2024},
		{Title: "Movie Name", Year: 2019},
	}

	run := func(results []indexers.Result) (picks []string, calls int64) {
		before := parser.ParseCalls()
		for ep := 1; ep <= episodes; ep++ {
			picks = append(picks, titleOf(pickTVResult(results, series, 1, ep, profile, nil)))
			cur := &upgradeBase{Tier: quality.TierHDTV720p}
			picks = append(picks, titleOf(pickTVResult(results, series, 1, ep, profile, cur)))
		}
		picks = append(picks, titleOf(pickTVResult(results, series, 1, 0, profile, nil)))
		for _, m := range movies {
			picks = append(picks, titleOf(pickBestResult(matchingResults(results, m), profile, m.Year)))
			up := pickUpgradeResult(matchingResults(results, m), profile, quality.TierHDTV720p, "", m.Year)
			picks = append(picks, titleOf(up))
		}
		return picks, parser.ParseCalls() - before
	}

	rawPicks, rawCalls := run(raw) // unmerged: every check parses on its own
	merged := indexers.MergeResults([]indexers.Outcome{{IndexerName: "a", Results: raw}})
	mergeBefore := parser.ParseCalls()
	_ = indexers.MergeResults([]indexers.Outcome{{IndexerName: "a", Results: raw}})
	mergeCalls := parser.ParseCalls() - mergeBefore
	mergedPicks, mergedCalls := run(merged)

	if len(rawPicks) != len(mergedPicks) {
		t.Fatalf("%d picks without the shared parse, %d with it", len(rawPicks), len(mergedPicks))
	}
	nonEmpty := 0
	for i := range rawPicks {
		if rawPicks[i] != mergedPicks[i] {
			t.Errorf("pick %d: %q without the shared parse, %q with it", i, rawPicks[i], mergedPicks[i])
		}
		if rawPicks[i] != "" {
			nonEmpty++
		}
	}
	if nonEmpty < episodes {
		t.Fatalf("only %d picks were made: the fixture does not exercise the picking", nonEmpty)
	}

	distinct := int64(len(raw))
	if mergeCalls < distinct || mergeCalls > distinct+5 {
		t.Errorf("merging %d releases parsed %d names", distinct, mergeCalls)
	}
	// Anything past the merge would be a re-parse (a little slack for other
	// tests running in the background).
	if mergedCalls > distinct/4 {
		t.Errorf("picking after the merge parsed %d names, want about none", mergedCalls)
	}
	if rawCalls < 20*distinct {
		t.Errorf("fixture too small to show anything: %d parses without the shared parse", rawCalls)
	}
	t.Logf("%d releases x %d episodes: %d parses before, %d after (%d at merge)", distinct, episodes, rawCalls, mergedCalls+mergeCalls, mergeCalls)
}

// TestRelVariantsAgree checks the parsed-input variants decide like the title ones.
func TestRelVariantsAgree(t *testing.T) {
	profiles := []quality.Profile{
		quality.Presets()[quality.Preset1080p],
		quality.Presets()[quality.Preset720p],
		func() quality.Profile {
			p := quality.Presets()[quality.Preset1080p]
			p.NoSeasonPacks, p.SkipBadReleases = true, true
			p.MustNotContain = []string{"hdtv"}
			return p
		}(),
	}
	for _, res := range parseOnceResults(3) {
		rel := parser.Parse(res.Title)
		for _, p := range profiles {
			ok1, why1 := p.TitleAllowed(res.Title)
			ok2, why2 := p.TitleAllowedRel(res.Title, rel)
			if ok1 != ok2 || why1 != why2 {
				t.Errorf("%q: TitleAllowed %v %q, TitleAllowedRel %v %q", res.Title, ok1, why1, ok2, why2)
			}
			if p.AcceptsTitle(res.Title) != p.AcceptsTitleRel(res.Title, rel) {
				t.Errorf("%q: AcceptsTitle and AcceptsTitleRel differ", res.Title)
			}
			if p.IsUpgradeOver(quality.TierHDTV720p, "", res.Title) != p.IsUpgradeOverRel(quality.TierHDTV720p, "", res.Title, rel) {
				t.Errorf("%q: IsUpgradeOver and IsUpgradeOverRel differ", res.Title)
			}
		}
		for _, series := range []library.Series{{Title: "Show Name", Year: 2020}, {Title: "Other Show"}} {
			for _, season := range []int{0, 1} {
				if matchesShowFor(res.Title, series, season) != matchesShowForRel(rel, series, season) {
					t.Errorf("%q: matchesShowFor and matchesShowForRel differ", res.Title)
				}
			}
		}
	}
}

func TestMatchesShowYearAfterEpisode(t *testing.T) {
	series := library.Series{Title: "Show Name", Year: 2010}
	cases := []struct {
		release string
		want    bool
	}{
		{"Show.Name.2010.S01E01.1080p.WEB-DL", true},
		{"Show.Name.2019.S01E01.1080p.WEB-DL", false}, // another show of that name
		{"Show.Name.S09E01.2019.1080p.WEB-DL", true},  // the air year
		{"Show.Name.S02E05.2012.Olympics.Special.720p", true},
		{"Show.Name.S02.2019.1080p.WEB-DL", false}, // a pack names the show's year
	}
	for _, tc := range cases {
		if got := matchesShow(tc.release, series); got != tc.want {
			t.Errorf("matchesShow(%q) = %v, want %v", tc.release, got, tc.want)
		}
	}
}

func TestMatchesMovieYearLikeTitle(t *testing.T) {
	cases := []struct {
		release string
		movie   library.Movie
		want    bool
	}{
		{"Blade.Runner.2049.2017.1080p.BluRay.x264-GRP", library.Movie{Title: "Blade Runner 2049", Year: 2017}, true},
		{"Blade.Runner.2049.1080p.BluRay.x264-GRP", library.Movie{Title: "Blade Runner 2049", Year: 2017}, true},
		{"Blade.Runner.2049.1080p.BluRay.x264-GRP", library.Movie{Title: "Blade Runner", Year: 1982}, false},
		{"1917.1080p.BluRay.x264-GRP", library.Movie{Title: "1917", Year: 2019}, true},
		{"2012.1080p.BluRay.x264-GRP", library.Movie{Title: "2012", Year: 2009}, true},
		{"2012 (2009) 1080p", library.Movie{Title: "2012", Year: 2009}, true},
		{"2012 (2009) 1080p", library.Movie{Title: "2012", Year: 2010}, false},
		{"Wonder.Woman.1984.2020.1080p.WEB-DL", library.Movie{Title: "Wonder Woman 1984", Year: 2020}, true},
		{"Cars.2.2011.1080p.BluRay", library.Movie{Title: "Cars 2", Year: 2011}, true},
		{"Cars.2.2011.1080p.BluRay", library.Movie{Title: "Cars", Year: 2006}, false},
	}
	for _, tc := range cases {
		if got := matchesMovie(tc.release, tc.movie); got != tc.want {
			t.Errorf("matchesMovie(%q, %q %d) = %v, want %v", tc.release, tc.movie.Title, tc.movie.Year, got, tc.want)
		}
	}
}
