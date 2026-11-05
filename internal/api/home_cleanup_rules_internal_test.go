package api

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/library"
)

func TestCleanupConditions(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	old := now.AddDate(0, 0, -90)
	watched := cleanupUnit{Kind: "movie", Known: true, AnyPlay: true, Watched: true, LastPlayed: now.AddDate(0, 0, -40), Fresh: old, Arrived: old,
		Size: 12 * gib, Year: 1999, Tags: []string{"Kids"}, Qualities: []string{"Bluray-2160p"}, AddedBy: 3, Requesters: []int64{4}}
	never := cleanupUnit{Kind: "movie", Known: true, Fresh: old, Arrived: old, Size: 2 * gib, Year: 2020, Qualities: []string{"WEBDL-1080p"}}
	cases := []struct {
		name string
		c    ruleCondition
		u    cleanupUnit
		want bool
	}{
		{"watched long enough ago", ruleCondition{Type: condWatchedDays, Number: 30}, watched, true},
		{"watched too recently", ruleCondition{Type: condWatchedDays, Number: 60}, watched, false},
		{"never watched is not watched", ruleCondition{Type: condWatchedDays, Number: 30}, never, false},
		{"downloaded again since the play", ruleCondition{Type: condWatchedDays, Number: 30}, func() cleanupUnit { u := watched; u.Fresh = now; return u }(), false},
		{"never watched, arrived long ago", ruleCondition{Type: condUnwatchedDays, Number: 60}, never, true},
		{"never watched but no server has it", ruleCondition{Type: condUnwatchedDays, Number: 60}, func() cleanupUnit { u := never; u.Known = false; return u }(), false},
		{"watched is not never watched", ruleCondition{Type: condUnwatchedDays, Number: 60}, watched, false},
		{"idle: watched long ago", ruleCondition{Type: condIdleDays, Number: 30}, watched, true},
		{"idle: never watched", ruleCondition{Type: condIdleDays, Number: 30}, never, true},
		{"idle: too recent", ruleCondition{Type: condIdleDays, Number: 50}, watched, false},
		{"watched completely", ruleCondition{Type: condWatchedAll}, watched, true},
		{"not watched completely", ruleCondition{Type: condWatchedAll}, never, false},
		{"big file", ruleCondition{Type: condSizeOver, Number: 10}, watched, true},
		{"small file", ruleCondition{Type: condSizeOver, Number: 10}, never, false},
		{"old film", ruleCondition{Type: condYearBefore, Number: 2000}, watched, true},
		{"new film", ruleCondition{Type: condYearBefore, Number: 2000}, never, false},
		{"has tag, any case", ruleCondition{Type: condHasTag, Text: "kids"}, watched, true},
		{"lacks tag", ruleCondition{Type: condLacksTag, Text: "kids"}, never, true},
		{"lacks tag but has it", ruleCondition{Type: condLacksTag, Text: "Kids"}, watched, false},
		{"requested by", ruleCondition{Type: condRequestedBy, Number: 4}, watched, true},
		{"added by", ruleCondition{Type: condRequestedBy, Number: 3}, watched, true},
		{"someone else", ruleCondition{Type: condRequestedBy, Number: 9}, watched, false},
		{"not requested", ruleCondition{Type: condNotRequested}, never, true},
		{"requested", ruleCondition{Type: condNotRequested}, watched, false},
		{"quality", ruleCondition{Type: condQuality, Text: "2160p"}, watched, true},
		{"other quality", ruleCondition{Type: condQuality, Text: "2160p"}, never, false},
		{"movies never 'ended'", ruleCondition{Type: condShowEnded}, watched, false},
		{"ended show", ruleCondition{Type: condShowEnded}, cleanupUnit{Kind: "show", ShowStatus: "Ended"}, true},
		{"cancelled show", ruleCondition{Type: condShowEnded}, cleanupUnit{Kind: "season", ShowStatus: "Canceled"}, true},
		{"running show", ruleCondition{Type: condShowEnded}, cleanupUnit{Kind: "show", ShowStatus: "Returning Series"}, false},
		{"unknown condition", ruleCondition{Type: "rating"}, watched, false},
	}
	for _, tc := range cases {
		if got, _ := evalCondition(tc.c, tc.u, now); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestCleanupAllOrAny(t *testing.T) {
	now := time.Now()
	u := cleanupUnit{Kind: "movie", Known: true, AnyPlay: true, Watched: true, LastPlayed: now.AddDate(0, 0, -40), Fresh: now.AddDate(0, 0, -90), Size: 2 * gib}
	conds := []ruleCondition{{Type: condWatchedDays, Number: 30}, {Type: condSizeOver, Number: 10}}
	if ok, why := ruleSetMatches(cleanupRuleSet{Match: "any", Conditions: conds}, u, now); !ok || why != "watched 40 days ago" {
		t.Errorf("any: %v %q", ok, why)
	}
	if ok, _ := ruleSetMatches(cleanupRuleSet{Match: "all", Conditions: conds}, u, now); ok {
		t.Error("all: matched with only one condition true")
	}
	u.Size = 20 * gib
	if ok, why := ruleSetMatches(cleanupRuleSet{Match: "all", Conditions: conds}, u, now); !ok || why != "watched 40 days ago, 20.0 GB" {
		t.Errorf("all: %v %q", ok, why)
	}
	if ok, _ := ruleSetMatches(cleanupRuleSet{Match: "any"}, u, now); ok {
		t.Error("a rule set with no conditions matched")
	}
	if ok, _ := ruleSetMatches(cleanupRuleSet{Match: "all"}, u, now); ok {
		t.Error("a rule set with no conditions matched (all)")
	}
}

// Streamed titles, kept titles and titles with a keep tag are left out before
// any condition is looked at.
func TestCleanupNeverTouchesStreamedOrKept(t *testing.T) {
	dir := t.TempDir()
	write := func(rel string) string {
		p := filepath.Join(dir, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		old := time.Now().AddDate(0, 0, -100)
		_ = os.Chtimes(p, old, old)
		return p
	}
	plain := write("Movies/A (2000)/A (2000).mkv")
	strm := write("Movies/B (2000)/B (2000).strm")
	streamFolder := write("Movies-Stream/C (2000)/C (2000).mkv")
	streamedDir := write("Streamed/D (2000)/D (2000).mkv")
	tagged := write("Movies/E (2000)/E (2000).mkv")
	kept := write("Movies/F (2000)/F (2000).mkv")
	link := filepath.Join(dir, "Movies", "G (2000)", "G (2000).mkv")
	_ = os.MkdirAll(filepath.Dir(link), 0o755)
	linked := runtime.GOOS != "windows" && os.Symlink(plain, link) == nil

	for _, p := range []string{strm, streamFolder, streamedDir} {
		if !streamedFile(p) {
			t.Errorf("%s should count as streamed", p)
		}
	}
	if streamedFile(plain) || streamedFile(tagged) {
		t.Error("a plain file counted as streamed")
	}
	if linked && !streamedFile(link) {
		t.Error("a link counted as a plain file")
	}

	snap := &cleanupSnapshot{movieHeld: map[int64]bool{}, movieWatch: map[int64]library.Watched{}, movieTags: map[int64][]string{5: {"Keep"}},
		added: map[int64]time.Time{}, downloaded: map[int64]time.Time{}, requesters: map[string][]int64{}}
	for i, p := range []string{plain, strm, streamFolder, streamedDir, tagged, kept} {
		id := int64(i + 1)
		snap.movies = append(snap.movies, library.Movie{ID: id, TMDBID: 100 + i, Title: string(rune('A' + i)), Year: 2000, Status: library.StatusDownloaded, FilePath: p})
		snap.movieHeld[id] = true
		snap.added[id] = time.Now().AddDate(0, 0, -100)
	}
	if linked {
		snap.movies = append(snap.movies, library.Movie{ID: 7, TMDBID: 107, Title: "G", Year: 2000, Status: library.StatusDownloaded, FilePath: link})
		snap.movieHeld[7] = true
		snap.added[7] = time.Now().AddDate(0, 0, -100)
	}
	rs := cleanupRuleSet{ID: 1, Name: "Unwatched", Enabled: true, Library: "movies", Match: "any", Conditions: []ruleCondition{{Type: condUnwatchedDays, Number: 30}}, KeepTags: []string{"keep"}}
	rs.fill()
	matches, streamed, keptN, err := matchUnits([]cleanupRuleSet{rs}, snap, map[string]bool{"movie:6": true}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || matches[0].Unit != "movie:1" {
		t.Fatalf("matches %+v, want only movie:1", matches)
	}
	wantStreamed := 3
	if linked {
		wantStreamed = 4
	}
	if streamed != wantStreamed || keptN != 2 {
		t.Errorf("streamed %d (want %d), kept %d (want 2)", streamed, wantStreamed, keptN)
	}

	// Even if a streamed title got onto the list somehow, acting on it is refused.
	s := &Server{}
	if err := s.actOnMatch(cleanupMatch{Kind: "movie", Action: actDeleteUnmonitor, unit: cleanupUnit{Kind: "movie", Files: []string{strm}}}); err == nil {
		t.Error("acted on a .strm file")
	}
}

func TestCleanupGraceAndCap(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	rs := cleanupRuleSet{ID: 1, GraceDays: 14}
	m := func(unit string) cleanupMatch { return cleanupMatch{Unit: unit, Title: unit, rs: rs} }

	// First match: on Leaving soon in 14 days, nothing due.
	p := arrangePlan(cleanupPlan{Limit: 50}, []cleanupMatch{m("movie:1")}, nil, now)
	if len(p.NewLeaving) != 1 || len(p.Due) != 0 || p.NewLeaving[0].ActMs != now.AddDate(0, 0, 14).UnixMilli() {
		t.Fatalf("first match: %+v", p)
	}
	listed := []leavingEntry{{Unit: "movie:1", Act: p.NewLeaving[0].ActMs}, {Unit: "movie:2", Act: now.AddDate(0, 0, -1).UnixMilli()}}

	// A week on: still waiting.
	p = arrangePlan(cleanupPlan{Limit: 50}, []cleanupMatch{m("movie:1")}, listed, now.AddDate(0, 0, 7))
	if len(p.Waiting) != 1 || len(p.Due) != 0 || len(p.NewLeaving) != 0 {
		t.Fatalf("a week on: %+v", p)
	}
	// movie:2's date came but it no longer matches: it comes off, untouched.
	if len(p.Dropped) != 1 || p.Dropped[0].Unit != "movie:2" {
		t.Fatalf("dropped %+v", p.Dropped)
	}
	// After the grace period, and still matching: due.
	p = arrangePlan(cleanupPlan{Limit: 50}, []cleanupMatch{m("movie:1")}, listed, now.AddDate(0, 0, 15))
	if len(p.Due) != 1 || p.Due[0].Unit != "movie:1" {
		t.Fatalf("after grace: %+v", p)
	}

	// The cap: the oldest dates go first, the rest wait for the next run.
	var many []cleanupMatch
	var onList []leavingEntry
	for i := 0; i < 5; i++ {
		u := "movie:" + string(rune('a'+i))
		many = append(many, m(u))
		onList = append(onList, leavingEntry{Unit: u, Act: now.AddDate(0, 0, -10+i).UnixMilli()})
	}
	p = arrangePlan(cleanupPlan{Limit: 2}, many, onList, now)
	if len(p.Due) != 2 || p.More != 3 || p.Due[0].Unit != "movie:a" || p.Due[1].Unit != "movie:b" {
		t.Fatalf("cap: due %+v, more %d", p.Due, p.More)
	}
}

func TestCleanupShowScopes(t *testing.T) {
	now := time.Now()
	old := now.AddDate(0, 0, -90)
	dir := t.TempDir()
	file := func(name string) string {
		p := filepath.Join(dir, name)
		_ = os.WriteFile(p, []byte("x"), 0o644)
		_ = os.Chtimes(p, old, old)
		return p
	}
	f1, f2, f3 := file("s1e1.mkv"), file("s1e2.mkv"), file("s2e1.mkv")
	snap := &cleanupSnapshot{
		shows: []library.Series{{ID: 9, TMDBID: 99, Title: "Show"}},
		episodes: map[int64][]library.Episode{9: {
			{ID: 1, SeriesID: 9, Season: 1, Episode: 1, Status: library.StatusDownloaded, FilePath: f1},
			{ID: 2, SeriesID: 9, Season: 1, Episode: 2, Status: library.StatusDownloaded, FilePath: f2},
			{ID: 3, SeriesID: 9, Season: 2, Episode: 1, Status: library.StatusDownloaded, FilePath: f3},
		}},
		seriesTags: map[int64][]string{}, showHeld: map[int64]bool{9: true}, showPlayed: map[int64]bool{9: true}, requesters: map[string][]int64{},
		epWatch: map[[3]int64]library.Watched{
			{9, 1, 1}: {LastPlayed: now.AddDate(0, 0, -40)},
			{9, 1, 2}: {LastPlayed: now.AddDate(0, 0, -35)},
		},
	}
	watched := []ruleCondition{{Type: condWatchedDays, Number: 30}}
	run := func(scope string) []string {
		rs := cleanupRuleSet{ID: 1, Library: "tv", ShowScope: scope, Conditions: watched}
		rs.fill()
		got, _, _, _ := matchUnits([]cleanupRuleSet{rs}, snap, nil, now)
		var out []string
		for _, m := range got {
			out = append(out, m.Unit)
		}
		return out
	}
	if got := run(scopeEpisodes); len(got) != 2 {
		t.Errorf("episodes: %v", got)
	}
	if got := run(scopeSeason); len(got) != 1 || got[0] != "season:9:1" {
		t.Errorf("season: %v (season 2 isn't watched)", got)
	}
	if got := run(scopeShow); len(got) != 0 {
		t.Errorf("show: %v (not all of it is watched)", got)
	}
}
