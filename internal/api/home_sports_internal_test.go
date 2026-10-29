package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/mediaservers"
)

// A link by Prowlarr's container name, which Usherr can't reach, goes to the
// Prowlarr Usherr's own indexers use; reachable links stay as they are.
func TestSportsReleaseLinksReachProwlarr(t *testing.T) {
	s, _ := optionsServer(t)
	if _, err := s.IndexerRepo.Create(indexers.Instance{Name: "DrunkenSlug (via Prowlarr)", BaseURL: "http://192.168.1.20:9697/3/api", Protocol: indexers.ProtocolUsenet, Kind: indexers.KindNewznab, Enabled: true}, nil); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	got := s.reachableLink(ctx, "http://prowlarr.invalid:9696/3/download?apikey=k&link=abc&file=Game")
	if got != "http://192.168.1.20:9697/3/download?apikey=k&link=abc&file=Game" {
		t.Fatalf("rewritten: %s", got)
	}
	for _, keep := range []string{
		"http://prowlarr.invalid:9696/7/download?apikey=k", // no indexer of Usherr's goes there
		"http://127.0.0.1:9696/3/download?apikey=k",        // reachable
		"https://indexer.invalid/getnzb/abc.nzb",           // not Prowlarr's shape
	} {
		if got := s.reachableLink(ctx, keep); got != keep {
			t.Fatalf("%s became %s", keep, got)
		}
	}
}

// Sportarr's releases for streaming: Usenet only, tied to the game (close to
// the best match score), not blocklisted; its quality-profile rejections
// don't matter.
func TestSportarrReleasesForStreaming(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/event/306/search" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"results":[
			{"title":"NFL.2026.09.28.Eagles.Vs.Bears.1080p","downloadUrl":"http://p/3/download?a","protocol":"Usenet","size":5,"indexer":"DS","matchScore":100,"rejections":["Quality not wanted"]},
			{"title":"NFL.2026.Week.4.Highlights","downloadUrl":"http://p/3/download?d","protocol":"Usenet","matchScore":40,"rejections":[]},
			{"title":"NFL.2026.09.28.Eagles.Vs.Bears.720p","downloadUrl":"magnet:?xt=1","protocol":"Torrent","rejections":[]},
			{"title":"NFL.2026.09.21.Other.Game","downloadUrl":"http://p/3/download?b","protocol":"Usenet","rejections":["Release doesn't match event (score: 20)"]},
			{"title":"NFL.2026.09.28.Eagles.Vs.Bears.2160p","downloadUrl":"http://p/3/download?c","protocol":"Usenet","matchScore":100,"isBlocklisted":true,"rejections":[]}
		]}`))
	}))
	defer fake.Close()
	b := &sportarrBackend{base: fake.URL, key: "k"}
	list, err := b.releases(context.Background(), "306")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Title != "NFL.2026.09.28.Eagles.Vs.Bears.1080p" || list[0].Indexer != "DS" || list[0].Size != 5 {
		t.Fatalf("releases: %+v", list)
	}
}

// A game is told apart from the others by the league and its date or teams;
// a race isn't its qualifying.
func TestOwnSportsReleaseMatching(t *testing.T) {
	at := func(s string) time.Time { v, _ := time.Parse(time.RFC3339, s); return v }
	nfl := ownGame{title: "Philadelphia Eagles vs Chicago Bears", home: "Philadelphia Eagles", away: "Chicago Bears", league: "NFL", aliases: leagueAliases("NFL", ""), at: at("2026-09-29T00:15:00Z")}
	f1 := ownGame{title: "Azerbaijan Grand Prix - Race", league: "Formula 1", aliases: leagueAliases("Formula 1", "F1"), at: at("2026-09-26T11:00:00Z")}
	ufc := ownGame{title: "UFC Fight Night 288 Silva vs Delgado", league: "UFC", aliases: leagueAliases("UFC", ""), at: at("2026-09-12T23:00:00Z")}
	for _, c := range []struct {
		g     ownGame
		title string
		want  bool
	}{
		{nfl, "NFL.2026.09.28.Philadelphia.Eagles.Vs.Chicago.Bears.720p.WEB.H264-NiGHTNiNJAS", true}, // the evening before, US time
		{nfl, "nfl.2026.10.12.eagles.vs.bears.1080p.web.h264", true},                                 // the teams
		{nfl, "NFL.2026.09.28.Packers.Vs.Lions.720p.WEB", true},                                      // same day: the date is enough
		{nfl, "NFL.2026.10.05.Packers.Vs.Lions.720p.WEB", false},
		{nfl, "NBA.2026.09.28.Bulls.Vs.Celtics", false}, // another league
		{f1, "Formula1.2026.Round15.Azerbaijan.Race.F1LIVE.F1TV.WEB-DL.1080p.H264.English-MWR", true},
		{f1, "Formula1.2026.Round15.Azerbaijan.Qualifying.SKY.F1TV.WEB-DL.1080p", false},
		{f1, "F1.2026.Azerbaijan.Grand.Prix.Race.1080p", true},
		{ufc, "UFC.Fight.Night.288.Silva.vs.Delgado.1080p.WEB-DL.H264.Fight-BB", true},
		{ufc, "UFC.Fight.Night.288.Silva.vs.Delgado.Prelims.1080p.WEB-DL", false}, // the prelims aren't the card
		{ufc, "UFC.Fight.Night.287.Hooker.vs.Parnasse.1080p.WEB-DL", false},
	} {
		if got := c.g.matches(c.title); got != c.want {
			t.Errorf("%q for %q: %v", c.title, c.g.title, got)
		}
	}
	if a := leagueAliases("Formula 1", "F1, Formula One"); a[0] != "f1" || len(a) != 4 {
		t.Fatalf("aliases: %v", a)
	}
}

// Usherr's own engine on a paid key: a league found and added, its current
// season fetched with the key in the header, its games listed by season and
// between dates; a team followed.
func TestOwnSportsOnTheSportsDB(t *testing.T) {
	var keys []string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		keys = append(keys, r.Header.Get("X-API-KEY"))
		switch r.URL.Path {
		case "/v2/json/search/league/nfl":
			_, _ = w.Write([]byte(`{"search":[{"idLeague":"4391","strLeague":"NFL","strSport":"American Football","strBadge":"https://r2.thesportsdb.com/images/b.png"}]}`))
		case "/v2/json/lookup/league/4391":
			_, _ = w.Write([]byte(`{"lookup":[{"idLeague":"4391","strLeague":"NFL","strLeagueAlternate":"National Football League","strSport":"American Football","strCurrentSeason":"2026"}]}`))
		case "/v2/json/list/seasons/4391":
			_, _ = w.Write([]byte(`{"list":[{"strSeason":"2025"},{"strSeason":"2026"}]}`))
		case "/v2/json/schedule/league/4391/2026":
			_, _ = w.Write([]byte(`{"schedule":[{"idEvent":"1","strEvent":"Philadelphia Eagles vs Chicago Bears","strHomeTeam":"Philadelphia Eagles","strAwayTeam":"Chicago Bears","strTimestamp":"2026-09-29T00:15:00","intRound":4,"intHomeScore":"24","intAwayScore":null},
				{"idEvent":"2","strEvent":"Packers vs Lions","dateEvent":"2026-10-05","strTime":"17:00:00"}]}`))
		case "/v2/json/search/team/chiefs":
			_, _ = w.Write([]byte(`{"search":[{"idTeam":"134931","strTeam":"Kansas City Chiefs","strSport":"American Football","strBadge":"b"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer fake.Close()
	old := tsdbBase
	tsdbBase = fake.URL
	defer func() { tsdbBase = old }()
	tsdbPace.last = time.Time{}

	s, db := optionsServer(t)
	s.db = db
	if err := s.Settings.Set(keySportsTSDBKey, "paid-key", false); err != nil {
		t.Fatal(err)
	}
	o := &ownSports{s: s}
	ctx := context.Background()
	found, err := o.searchLeagues(ctx, "NFL")
	if err != nil || len(found) != 1 || found[0].ExternalID != "4391" {
		t.Fatalf("search: %v %+v", err, found)
	}
	if err := o.addLeague(ctx, found[0], false); err != nil {
		t.Fatal(err)
	}
	if err := o.addLeague(ctx, found[0], false); err == nil {
		t.Fatal("added twice")
	}
	leagues, err := o.leagues(ctx)
	if err != nil || len(leagues) != 1 {
		t.Fatalf("leagues: %v %+v", err, leagues)
	}
	id := leagues[0].ID
	seasons, err := o.leagueSeasons(ctx, id)
	if err != nil || len(seasons) != 2 || seasons[0] != "2026" {
		t.Fatalf("seasons: %v %v", err, seasons)
	}
	games, err := o.leagueEvents(ctx, id, "2026")
	if err != nil || len(games) != 2 {
		t.Fatalf("games: %v %+v", err, games)
	}
	g := games[0]
	if g.Title != "Philadelphia Eagles vs Chicago Bears" || g.Round != "4" || g.HomeScore == nil || *g.HomeScore != 24 || g.AwayScore != nil || g.League != "NFL" ||
		!g.When.Equal(time.Date(2026, 9, 29, 0, 15, 0, 0, time.UTC)) {
		t.Fatalf("game: %+v", g)
	}
	if !games[1].When.Equal(time.Date(2026, 10, 5, 17, 0, 0, 0, time.UTC)) {
		t.Fatalf("date and time: %v", games[1].When)
	}
	between, err := o.between(ctx, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC))
	if err != nil || len(between) != 1 || between[0].Title != "Packers vs Lions" {
		t.Fatalf("between: %v %+v", err, between)
	}
	teams, err := o.searchTeams(ctx, "Chiefs")
	if err != nil || len(teams) != 1 {
		t.Fatalf("teams: %v %+v", err, teams)
	}
	if err := o.followTeam(ctx, teams[0]); err != nil {
		t.Fatal(err)
	}
	if f, _ := o.followedTeams(ctx); len(f) != 1 || f[0].Name != "Kansas City Chiefs" {
		t.Fatalf("followed: %+v", f)
	}
	for _, k := range keys {
		if k != "paid-key" {
			t.Fatalf("a call without the key: %q", k)
		}
	}
	if o.canDownload() || o.getEvent(ctx, g.ID) == nil {
		t.Fatal("the own engine doesn't download yet")
	}
}

// Releases in the household's language go first, other languages last.
func TestSportsReleasesInYourLanguageFirst(t *testing.T) {
	en := []string{"English"}
	for _, c := range []struct {
		r    sportRelease
		want int
	}{
		{sportRelease{Title: "Formula1.2026.Round11.Hungary.Race.F1TV.WEB-DL.1080p.H264.English-MWR"}, 0},
		{sportRelease{Title: "Formula1.S2026E94.Singapore.Race.1080p.WEB-DL", Language: "English"}, 1}, // Sportarr's English means "can't tell"
		{sportRelease{Title: "Formula1.S2026E94.Singapore.Race.1080p.WEB-DL"}, 1},
		{sportRelease{Title: "Formula1.S2026E94.Singapore.Race.1080p.WEB-DL", Language: "Unknown"}, 1},
		{sportRelease{Title: "Formula1.2026.Round11.Hungary.Race.F1TV.WEB-DL.2160p.HLG.H265.Multi-MWR"}, 2},
		{sportRelease{Title: "Formula1.S2026E94.Singapore.Race.REPACK.1080p.ANTP.WEB-DL", Language: "Spanish"}, 3},
		{sportRelease{Title: "Formel.1.2026.GP.Italien.Rennen.German.AAC.720p.WEBRiP"}, 3},
	} {
		if got := sportLanguageRank(c.r, en); got != c.want {
			t.Errorf("%s (%s): %d, want %d", c.r.Title, c.r.Language, got, c.want)
		}
	}
}

// The shows around a game aren't the game.
func TestSportsSidePrograms(t *testing.T) {
	game := "Singapore Grand Prix - Qualifying"
	for title, side := range map[string]bool{
		"Formula1.2026.Round17.Singapore.Qualifying.F1LIVE.F1TV.WEB-DL.1080p.H264.English-MWR":       false,
		"Formula1.2026.Singapore.Grand.Prix.Qualifying.1080p.WEB.H.264-BILLIE":                       false,
		"Formula1.2026.Round17.Singapore.Pre-Qualifying.Show.F1TV.WEB-DL.1080p.H264.English-MWR":     true,
		"Formula1.2026.Round17.Singapore.Post-Qualifying.Show.F1TV.WEB-DL.1080p.H264.English-MWR":    true,
		"Formula1.2026.Round17.Singapore.Weekend.Warm-Up.F1TV.WEB-DL.1080p.H264.English-MWR":         true,
		"Formula1.2026.Round17.Singapore.Jolyon.Palmers.Analysis.F1TV.WEB-DL.1080p.H264.English-MWR": true,
		"Formula1.2026.Singapore.Grand.Prix.Teds.Qualifying.Notebook.1080p.AHDTV.x264-DARKSPORT":     true,
	} {
		if got := sideProgram(title, game); got != side {
			t.Errorf("%s: %v", title, got)
		}
	}
	if sideProgram("UFC.Fight.Night.288.Prelims.1080p", "UFC Fight Night 288") {
		t.Fatal("prelims are part of a card")
	}
}

// Two people streaming the same game at once start one search.
func TestSportsOneSearchAtATime(t *testing.T) {
	s, _ := optionsServer(t)
	b := &countingBackend{gate: make(chan struct{})}
	done := make(chan []sportRelease, 2)
	for range 2 {
		go func() {
			list, _ := s.sportReleases(context.Background(), b, "7")
			done <- list
		}()
	}
	time.Sleep(50 * time.Millisecond)
	close(b.gate)
	for range 2 {
		if l := <-done; len(l) != 1 {
			t.Fatalf("answer: %+v", l)
		}
	}
	if b.calls.Load() != 1 {
		t.Fatalf("searches: %d", b.calls.Load())
	}
}

type countingBackend struct {
	ownSports
	gate  chan struct{}
	calls atomic.Int32
}

func (c *countingBackend) name() string { return "counting" }

func (c *countingBackend) releases(ctx context.Context, id string) ([]sportRelease, error) {
	c.calls.Add(1)
	<-c.gate
	return []sportRelease{{Title: "Game"}}, nil
}

// The Marquee's sports: a screen without scores never gets them; with a
// paid key a game under way has its live score and clock; followed teams
// first, a screen's leagues and "followed teams only" are kept.
func TestMarqueeSports(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/json/search/league/nfl":
			_, _ = w.Write([]byte(`{"search":[{"idLeague":"4391","strLeague":"NFL"}]}`))
		case "/v2/json/livescore/4391":
			_, _ = w.Write([]byte(`{"livescore":[{"strHomeTeam":"Kansas City Chiefs","strAwayTeam":"Las Vegas Raiders","intHomeScore":"21","intAwayScore":"14","strStatus":"Q3","strProgress":"08:12","strHomeTeamBadge":"hb"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer fake.Close()
	old := tsdbBase
	tsdbBase = fake.URL
	defer func() { tsdbBase = old }()
	tsdbPace.last = time.Time{}
	teamCrestLookups = false
	defer func() { teamCrestLookups = true }()
	marqueeGamesCache.Lock()
	marqueeGamesCache.at = time.Time{}
	marqueeGamesCache.Unlock()

	s, db := optionsServer(t)
	s.db = db
	_ = s.Settings.Set(keySportsMode, "usherr", false)
	_ = s.Settings.Set(keySportsTSDBKey, "paid-key", false)
	now := time.Now()
	mustExec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustExec(`INSERT INTO sports_leagues (id, external_id, name, added_ms) VALUES (1, '4391', 'NFL', 0), (2, '4387', 'NBA', 0)`)
	mustExec(`INSERT INTO sports_events (league_id, external_id, title, home, away, at_ms, home_score, away_score, status) VALUES
		(1, 'a', 'Kansas City Chiefs vs Las Vegas Raiders', 'Kansas City Chiefs', 'Las Vegas Raiders', ?, NULL, NULL, ''),
		(1, 'b', 'Green Bay Packers vs Detroit Lions', 'Green Bay Packers', 'Detroit Lions', ?, NULL, NULL, ''),
		(1, 'c', 'Dallas Cowboys vs New York Giants', 'Dallas Cowboys', 'New York Giants', ?, 30, 10, 'Match Finished'),
		(2, 'd', 'Bulls vs Celtics', 'Chicago Bulls', 'Boston Celtics', ?, NULL, NULL, '')`,
		now.Add(-time.Hour).UnixMilli(), now.Add(3*time.Hour).UnixMilli(), now.Add(-10*time.Hour).UnixMilli(), now.Add(5*time.Hour).UnixMilli())
	mustExec(`INSERT INTO sports_teams (external_id, name, added_ms) VALUES ('t1', 'Detroit Lions', 0)`)

	set := defaultMarquee()
	set.SportsTicker = true
	ctx := context.Background()
	got := s.marqueeSportsFor(ctx, set)
	if got == nil || !got.Live || got.Scores || len(got.Games) != 4 {
		t.Fatalf("sports: %+v", got)
	}
	// Live first, then upcoming (the followed team's game before the other), then finished.
	order := []string{got.Games[0].Home, got.Games[1].Home, got.Games[2].Home, got.Games[3].Home}
	if order[0] != "Kansas City Chiefs" || order[1] != "Green Bay Packers" || order[2] != "Chicago Bulls" || order[3] != "Dallas Cowboys" {
		t.Fatalf("order: %v", order)
	}
	for _, g := range got.Games {
		if g.HomeScore != nil || g.AwayScore != nil || g.Clock != "" {
			t.Fatalf("a score without scores on: %+v", g)
		}
	}
	if got.Games[0].State != "live" || got.Games[0].HomeBadge != "hb" || !got.Games[1].Followed || got.Games[3].State != "final" {
		t.Fatalf("states: %+v", got.Games)
	}

	set.SportsScores = true
	got = s.marqueeSportsFor(ctx, set)
	live := got.Games[0]
	if live.HomeScore == nil || *live.HomeScore != 21 || *live.AwayScore != 14 || live.Clock != "Q3 08:12" {
		t.Fatalf("live: %+v", live)
	}
	if f := got.Games[3]; f.HomeScore == nil || *f.HomeScore != 30 {
		t.Fatalf("final: %+v", f)
	}

	set.SportsLeagues = []string{"nba"}
	if got = s.marqueeSportsFor(ctx, set); len(got.Games) != 1 || got.Games[0].League != "NBA" {
		t.Fatalf("leagues: %+v", got.Games)
	}
	set.SportsLeagues, set.SportsTeamsOnly = nil, true
	if got = s.marqueeSportsFor(ctx, set); len(got.Games) != 1 || got.Games[0].Away != "Detroit Lions" {
		t.Fatalf("followed only: %+v", got.Games)
	}
	set.SportsTicker, set.SportsTeamsOnly = false, false
	for i := range set.Scenes {
		if set.Scenes[i].Kind == "scores" || set.Scenes[i].Kind == "sports-tonight" {
			set.Scenes[i].On = false
		}
	}
	if s.marqueeSportsFor(ctx, set) != nil {
		t.Fatal("sports for a screen that shows none")
	}
}

// With Sports on Sportarr and no key, live scores come from Sportarr's feed
// (scores and status, no clock).
func TestMarqueeSportsFromSportarrFeed(t *testing.T) {
	soon := time.Now().Add(-40 * time.Minute).UTC().Format(time.RFC3339)
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/calendar":
			_, _ = w.Write([]byte(`[{"id":1,"title":"Bulls vs Celtics","sport":"Basketball","leagueId":4,"leagueName":"NBA","homeTeamName":"Chicago Bulls","awayTeamName":"Boston Celtics","eventDate":"` + soon + `","status":"in_progress"}]`))
		case "/api/followed-teams":
			_, _ = w.Write([]byte(`[]`))
		case "/api/events/livescore":
			if r.URL.Query().Get("sport") != "Basketball" {
				t.Errorf("sport: %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`[{"idEvent":"ev-1","strHomeTeam":"Chicago Bulls","strAwayTeam":"Boston Celtics","intHomeScore":"55","intAwayScore":"60","strStatus":"Live"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer fake.Close()
	s, db := optionsServer(t)
	s.db = db
	_ = s.Settings.Set(keySportsMode, "sportarr", false)
	_ = s.Settings.Set(keySportsSportarrURL, fake.URL, false)
	_ = s.Settings.Set(keySportsSportarrKey, "k", false)
	marqueeGamesCache.Lock()
	marqueeGamesCache.at = time.Time{}
	marqueeGamesCache.Unlock()
	liveCache.Lock()
	liveCache.at = nil
	liveCache.Unlock()
	set := defaultMarquee()
	set.SportsScores = true
	got := s.marqueeSportsFor(context.Background(), set)
	if got == nil || !got.Live || len(got.Games) != 1 {
		t.Fatalf("sports: %+v", got)
	}
	g := got.Games[0]
	if g.State != "live" || g.HomeScore == nil || *g.HomeScore != 55 || *g.AwayScore != 60 || g.Clock != "" {
		t.Fatalf("game: %+v", g)
	}
}

// The game being watched gets its card: one streamed from Sports by its id,
// one played in Jellyfin by its name. A game watched after it was played
// never shows its final score; another game the same day isn't taken for it.
func TestMarqueeGameBeingWatched(t *testing.T) {
	s, db := optionsServer(t)
	s.db = db
	_ = s.Settings.Set(keySportsMode, "usherr", false)
	marqueeRecentGames.Lock()
	marqueeRecentGames.at = time.Time{}
	marqueeRecentGames.Unlock()
	day := time.Date(2026, 9, 29, 0, 15, 0, 0, time.UTC)
	if time.Since(day) > 9*24*time.Hour {
		day = time.Now().AddDate(0, 0, -2) // the last ten days are looked at
	}
	if _, err := db.Exec(`INSERT INTO sports_leagues (id, external_id, name, badge, added_ms) VALUES (1, '4391', 'NFL', 'lb', 0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO sports_events (id, league_id, external_id, title, home, away, at_ms, venue, home_score, away_score, status) VALUES
		(7, 1, 'a', 'Philadelphia Eagles vs Chicago Bears', 'Philadelphia Eagles', 'Chicago Bears', ?, 'Lincoln Financial Field', 24, 20, 'Match Finished'),
		(8, 1, 'b', 'Green Bay Packers vs Detroit Lions', 'Green Bay Packers', 'Detroit Lions', ?, '', 17, 14, 'Match Finished')`, day.UnixMilli(), day.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	stamp := day.Add(-12 * time.Hour).Format("2006.01.02")
	list := []marqueePlaying{
		{Playing: mediaservers.Playing{Kind: "other", Title: "Eagles vs Bears"}, GameID: "8"},
		{Playing: mediaservers.Playing{Kind: "episode", Series: "NFL", Title: "NFL." + stamp + ".Philadelphia.Eagles.Vs.Chicago.Bears.720p.WEB.H264-NiGHTNiNJAS"}},
		{Playing: mediaservers.Playing{Kind: "episode", Series: "NFL", Title: "NFL." + stamp + ".Dallas.Cowboys.Vs.New.York.Giants.720p"}},
		{Playing: mediaservers.Playing{Kind: "movie", Title: "Dune", TMDBID: 438631}},
	}
	set := defaultMarquee()
	set.SportsScores = true
	s.marqueeGamesPlaying(context.Background(), set, list)
	if g := list[0].Game; g == nil || g.Home != "Green Bay Packers" || g.HomeScore != nil || g.State != "final" {
		t.Fatalf("streamed: %+v", g)
	}
	if g := list[1].Game; g == nil || g.Away != "Chicago Bears" || g.Venue != "Lincoln Financial Field" || g.LeagueBadge != "lb" || g.HomeScore != nil {
		t.Fatalf("from Jellyfin: %+v", g)
	}
	if list[2].Game != nil || list[3].Game != nil {
		t.Fatalf("taken for a game: %+v %+v", list[2].Game, list[3].Game)
	}
}

// A team game's sides come from its title when the backend leaves them out;
// a fight card or a race keeps its title.
func TestSportsTeamsFromTheTitle(t *testing.T) {
	for _, c := range []struct {
		e          sportEvent
		home, away string
	}{
		{sportEvent{Title: "Green Bay Packers vs Dallas Cowboys", Sport: "Football"}, "Green Bay Packers", "Dallas Cowboys"},
		{sportEvent{Title: "UFC Fight Night 288 Silva vs Delgado", Sport: "Fighting"}, "", ""},
		{sportEvent{Title: "Smith vs Jones", Sport: "Combat"}, "", ""},
		{sportEvent{Title: "Azerbaijan Grand Prix - Race", Sport: "Motorsport"}, "", ""},
		{sportEvent{Title: "UFC 330 Smith vs Jones", Sport: "Mixed"}, "", ""}, // a number: a card
	} {
		if h, a := teamsOf(c.e); h != c.home || a != c.away {
			t.Errorf("%q: %q / %q", c.e.Title, h, a)
		}
	}
}

// An older game is found by the date in its name.
func TestMarqueeOlderGameByItsDate(t *testing.T) {
	s, db := optionsServer(t)
	s.db = db
	_ = s.Settings.Set(keySportsMode, "usherr", false)
	marqueeRecentGames.Lock()
	marqueeRecentGames.at = time.Time{}
	marqueeRecentGames.Unlock()
	may := time.Date(2026, 5, 20, 0, 30, 0, 0, time.UTC)
	if _, err := db.Exec(`INSERT INTO sports_leagues (id, external_id, name, added_ms) VALUES (1, '4387', 'NBA', 0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO sports_events (id, league_id, external_id, title, sport, at_ms, status) VALUES
		(5, 1, 'x', 'San Antonio Spurs vs Oklahoma City Thunder', 'Basketball', ?, 'Match Finished'),
		(6, 1, 'y', 'Boston Celtics vs New York Knicks', 'Basketball', ?, 'Match Finished')`, may.UnixMilli(), may.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	list := []marqueePlaying{{Playing: mediaservers.Playing{Kind: "other", Title: "San Antonio Spurs vs Oklahoma City Thunder"},
		Release: "NBA.Playoffs.2026.05.19.Oklahoma.City.Thunder.vs.San.Antonio.Spurs.1080p.WEB.h264-EPOWORKS"}}
	s.marqueeGamesPlaying(context.Background(), defaultMarquee(), list)
	if g := list[0].Game; g == nil || g.ID != "5" || g.Home != "San Antonio Spurs" || g.Away != "Oklahoma City Thunder" {
		t.Fatalf("game: %+v", g)
	}
}

// An NZB is fetched from the indexer once; a release found broken isn't
// fetched again that day.
func TestNZBFetchedOnce(t *testing.T) {
	fetches := 0
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetches++
		_, _ = w.Write([]byte(`<?xml version="1.0"?><nzb xmlns="http://www.newzbin.com/DTD/2003/nzb"><file subject="a"><segments><segment number="1" bytes="1">x@y</segment></segments></file></nzb>`))
	}))
	defer fake.Close()
	s, _ := optionsServer(t)
	s.cfg.ConfigDir = t.TempDir()
	link := fake.URL + "/3/download?apikey=k&link=abc"
	for range 3 {
		if b, err := s.fetchRelease(context.Background(), link); err != nil || len(b) == 0 {
			t.Fatalf("fetch: %v", err)
		}
	}
	// The same NZB reached by another host name is the same file.
	if _, err := s.fetchRelease(context.Background(), strings.Replace(link, "127.0.0.1", "localhost", 1)); err != nil {
		t.Fatal(err)
	}
	if fetches != 1 {
		t.Fatalf("fetched %d times", fetches)
	}
	if s.nzbBad(link) {
		t.Fatal("bad before marked")
	}
	s.markNZBBad(link)
	if !s.nzbBad(link) {
		t.Fatal("not remembered as bad")
	}
	if _, err := s.ustreamOpen(context.Background(), nil, indexers.Result{DownloadURL: link}); err == nil || fetches != 1 {
		t.Fatalf("a broken release was tried again: %v (fetches %d)", err, fetches)
	}
}
