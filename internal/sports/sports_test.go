package sports

import (
	"reflect"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/quality"
)

func day(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

// Real-world sports release names, as the indexers list them.
func TestParseSportsReleaseNames(t *testing.T) {
	for _, c := range []struct {
		title   string
		league  string
		date    string
		year    int
		round   int
		number  int
		teams   [2]string
		part    string
		session string
		side    bool
		tier    quality.Tier
	}{
		{title: "UFC.300.Pereira.vs.Hill.PPV.1080p.HDTV.x264-VERUM", league: "ufc", year: 0, number: 300, teams: [2]string{"pereira", "hill"}, part: PartMainCard, tier: quality.TierHDTV1080p},
		{title: "UFC.300.Prelims.720p.WEB.h264-VERUM", league: "ufc", number: 300, part: PartPrelims, tier: quality.TierWebDL720p},
		{title: "UFC.300.Early.Prelims.1080p.WEB-DL.H264.Fight-BB", league: "ufc", number: 300, part: PartEarlyPrelims, tier: quality.TierWebDL1080p},
		{title: "UFC.Fight.Night.250.Main.Card.1080p.WEB.h264-VERUM", league: "ufc", number: 250, part: PartMainCard, tier: quality.TierWebDL1080p},
		{title: "UFC.on.ESPN.60.Prelims.720p.WEB.h264-VERUM", league: "ufc", number: 60, part: PartPrelims, tier: quality.TierWebDL720p},
		{title: "Formula1.2026.Round15.Azerbaijan.Race.F1LIVE.F1TV.WEB-DL.1080p.H264.English-MWR", league: "f1", year: 2026, round: 15, session: SessionRace, tier: quality.TierWebDL1080p},
		{title: "Formula1.2026.Round15.Azerbaijan.Qualifying.SkyF1HD.1080p50", league: "f1", year: 2026, round: 15, session: SessionQualifying, tier: quality.TierWebDL1080p},
		{title: "F1.2026.R15.Azerbaijan.Grand.Prix.Sprint.Qualifying.1080p.WEB", league: "f1", year: 2026, round: 15, session: SessionSprintQualifying, tier: quality.TierWebDL1080p},
		{title: "Formula1.2026.Round15.Azerbaijan.Teds.Notebook.1080p.WEB", league: "f1", year: 2026, round: 15, side: true, tier: quality.TierWebDL1080p},
		{title: "NFL.2026.09.28.Philadelphia.Eagles.Vs.Chicago.Bears.720p.WEB.H264-NiGHTNiNJAS", league: "nfl", date: "2026-09-28", year: 2026, teams: [2]string{"philadelphia eagles", "chicago bears"}, tier: quality.TierWebDL720p},
		{title: "NFL.2026.Week.4.Eagles.vs.Bears.1080p.WEB.h264-SPORTS", league: "nfl", year: 2026, round: 4, teams: [2]string{"eagles", "bears"}, tier: quality.TierWebDL1080p},
		{title: "NFL.2026.09.28.Week.4.Eagles.vs.Bears.Pregame.Show.720p", league: "nfl", date: "2026-09-28", year: 2026, round: 4, teams: [2]string{"eagles", "bears pregame show"}, side: true, tier: quality.TierWebDL720p},
		{title: "NBA.2026.10.21.Lakers.vs.Celtics.720p.WEB.h264-SPORTS", league: "nba", date: "2026-10-21", year: 2026, teams: [2]string{"lakers", "celtics"}, tier: quality.TierWebDL720p},
		{title: "NHL.2026.10.15.Toronto.Maple.Leafs.@.Montreal.Canadiens.1080p.HDTV", league: "nhl", date: "2026-10-15", year: 2026, teams: [2]string{"toronto maple leafs", "montreal canadiens"}, tier: quality.TierHDTV1080p},
		{title: "MLB.2026.10.24.World.Series.Game.1.Dodgers.vs.Yankees.1080p.WEB", league: "mlb", date: "2026-10-24", year: 2026, teams: [2]string{"dodgers", "yankees"}, tier: quality.TierWebDL1080p},
		{title: "EPL.2025.11.02.Arsenal.vs.Chelsea.720p.HDTV.x264-FAIRPLAY", league: "epl", date: "2025-11-02", year: 2025, teams: [2]string{"arsenal", "chelsea"}, tier: quality.TierHDTV720p},
		{title: "English.Premier.League.2025.26.Matchweek.10.Arsenal.vs.Chelsea.1080p.WEB", league: "epl", year: 2025, round: 10, teams: [2]string{"arsenal", "chelsea"}, tier: quality.TierWebDL1080p},
		{title: "UEFA.Champions.League.2025.10.21.Real.Madrid.vs.Juventus.1080p.WEB", league: "ucl", date: "2025-10-21", year: 2025, teams: [2]string{"real madrid", "juventus"}, tier: quality.TierWebDL1080p},
		{title: "Bundesliga.21.10.2025.Bayern.Munich.vs.Borussia.Dortmund.1080p.WEB", league: "bundesliga", date: "2025-10-21", year: 2025, teams: [2]string{"bayern munich", "borussia dortmund"}, tier: quality.TierWebDL1080p},
		{title: "WWE.Monday.Night.Raw.2026.10.19.1080p.WEB.h264-HEEL", league: "wwe", date: "2026-10-19", year: 2026, tier: quality.TierWebDL1080p},
		{title: "WWE.SmackDown.2026.10.24.720p.HDTV.x264-NWCHD", league: "wwe", date: "2026-10-24", year: 2026, tier: quality.TierHDTV720p},
		{title: "WWE.Survivor.Series.WarGames.2025.PPV.1080p.WEB.h264-HEEL", league: "wwe", year: 2025, part: PartMainCard, tier: quality.TierWebDL1080p},
		{title: "MotoGP.2026.Round18.Australia.Race.1080p.WEB-DL", league: "motogp", year: 2026, round: 18, session: SessionRace, tier: quality.TierWebDL1080p},
		{title: "MotoGP.2026.Round18.Australia.Sprint.1080p.WEB", league: "motogp", year: 2026, round: 18, session: SessionSprint, tier: quality.TierWebDL1080p},
		{title: "NASCAR.Cup.Series.2026.Round30.Martinsville.Race.720p.HDTV", league: "nascar", year: 2026, round: 30, session: SessionRace, tier: quality.TierHDTV720p},
		{title: "Formula1.2026.Round15.Azerbaijan.FP1.1080p.F1TV.WEB-DL", league: "f1", year: 2026, round: 15, session: SessionPractice, tier: quality.TierWebDL1080p},
		{title: "Premier League 12th Oct 2025 Arsenal v Chelsea 720p", league: "epl", date: "2025-10-12", year: 2025, teams: [2]string{"arsenal", "chelsea"}, tier: quality.TierWebDL720p},
		{title: "UFC.300.Pereira.vs.Hill", league: "ufc", number: 300, teams: [2]string{"pereira", "hill"}, tier: quality.TierUnknown},
	} {
		r := Parse(c.title)
		got := ""
		if !r.Date.IsZero() {
			got = r.Date.Format("2006-01-02")
		}
		if r.League != c.league || got != c.date || r.Year != c.year || r.Round != c.round || r.Number != c.number || r.Teams != c.teams ||
			r.Part != c.part || r.Session != c.session || r.Side != c.side || r.Tier != c.tier {
			t.Errorf("%s:\n got league=%q date=%q year=%d round=%d number=%d teams=%q part=%q session=%q side=%v tier=%s\nwant league=%q date=%q year=%d round=%d number=%d teams=%q part=%q session=%q side=%v tier=%s",
				c.title, r.League, got, r.Year, r.Round, r.Number, r.Teams, r.Part, r.Session, r.Side, r.Tier,
				c.league, c.date, c.year, c.round, c.number, c.teams, c.part, c.session, c.side, c.tier)
		}
	}
}

var (
	nfl    = League{Name: "NFL", Sport: "American Football"}
	epl    = League{Name: "English Premier League", Alternate: "Premier League, EPL", Sport: "Soccer"}
	f1     = League{Name: "Formula 1", Alternate: "F1", Sport: "Motorsport"}
	ufc    = League{Name: "UFC", Sport: "Fighting"}
	wwe    = League{Name: "WWE", Sport: "Fighting"}
	nhl    = League{Name: "NHL", Sport: "Ice Hockey"}
	motogp = League{Name: "MotoGP", Sport: "Motorsport"}
	rugby  = League{Name: "Super Rugby", Sport: "Rugby"}
)

func TestMatchSportsReleases(t *testing.T) {
	eagles := Event{League: nfl, Title: "Philadelphia Eagles vs Chicago Bears", Home: "Philadelphia Eagles", Away: "Chicago Bears", Round: "4", Season: "2026", At: at("2026-09-29T00:15:00Z")}
	arsenal := Event{League: epl, Title: "Arsenal vs Chelsea", Home: "Arsenal", Away: "Chelsea", Round: "10", Season: "2025-2026", At: at("2025-11-02T16:30:00Z")}
	baku := Event{League: f1, Title: "Azerbaijan Grand Prix", Round: "15", Season: "2026", At: at("2026-09-20T11:00:00Z")}
	bakuQuali := Event{League: f1, Title: "Azerbaijan Grand Prix Qualifying", Round: "15", Season: "2026", At: at("2026-09-19T12:00:00Z")}
	ufc300 := Event{League: ufc, Title: "UFC 300 Pereira vs Hill", Season: "2026", At: at("2026-04-14T02:00:00Z")}
	fn288 := Event{League: ufc, Title: "UFC Fight Night 288 Silva vs Delgado", Season: "2026", At: at("2026-09-12T23:00:00Z")}
	raw := Event{League: wwe, Title: "WWE Monday Night Raw", Season: "2026", At: at("2026-10-20T00:00:00Z")}
	leafs := Event{League: nhl, Title: "Toronto Maple Leafs vs Montreal Canadiens", Home: "Toronto Maple Leafs", Away: "Montreal Canadiens", Season: "2026-2027", At: at("2026-10-15T23:00:00Z")}
	australia := Event{League: motogp, Title: "Australian Motorcycle Grand Prix", Round: "18", Season: "2026", At: at("2026-10-18T05:00:00Z")}
	crusaders := Event{League: rugby, Title: "Crusaders vs Blues", Home: "Crusaders", Away: "Blues", Season: "2026", At: at("2026-03-07T06:35:00Z")}

	for _, c := range []struct {
		e     Event
		part  string
		title string
		want  bool
	}{
		{eagles, "", "NFL.2026.09.28.Philadelphia.Eagles.Vs.Chicago.Bears.720p.WEB.H264-NiGHTNiNJAS", true}, // the US evening before
		{eagles, "", "NFL.2026.Week.4.Eagles.vs.Bears.1080p.WEB.h264-SPORTS", true},                         // the week
		{eagles, "", "NFL.2026.09.28.Packers.Vs.Lions.720p.WEB", false},                                     // another game the same day
		{eagles, "", "NFL.2026.10.12.Eagles.vs.Bears.1080p.WEB", false},                                     // the same teams, another day
		{eagles, "", "NFL.2026.Week.11.Eagles.vs.Bears.1080p.WEB", false},                                   // another week
		{eagles, "", "NBA.2026.09.28.Bulls.Vs.Celtics", false},                                              // another league
		{eagles, "", "NFL.2026.09.28.Eagles.vs.Bears.Highlights.720p", false},                               // highlights
		{eagles, "", "NFL.Eagles.vs.Bears.1080p.WEB", false},                                                // no when at all
		{arsenal, "", "EPL.2025.11.02.Arsenal.vs.Chelsea.720p.HDTV.x264-FAIRPLAY", true},
		{arsenal, "", "English.Premier.League.2025.26.Matchweek.10.Arsenal.vs.Chelsea.1080p.WEB", true},
		{arsenal, "", "Premier League 2nd Nov 2025 Arsenal v Chelsea 720p", true},
		{arsenal, "", "EPL.2025.11.02.Liverpool.vs.Everton.720p", false},
		{arsenal, "", "Serie.A.2025.11.02.Inter.vs.Milan.720p", false},
		{baku, "", "Formula1.2026.Round15.Azerbaijan.Race.F1LIVE.F1TV.WEB-DL.1080p.H264.English-MWR", true},
		{baku, "", "F1.2026.Azerbaijan.Grand.Prix.1080p.WEB", true},
		{baku, "", "Formula1.2026.Round15.Azerbaijan.Qualifying.SkyF1HD.1080p50", false},
		{baku, "", "Formula1.2026.Round15.Azerbaijan.Teds.Notebook.1080p.WEB", false},
		{baku, "", "Formula1.2026.Round16.Singapore.Race.1080p.WEB", false},
		{baku, "", "Formula1.2025.Round17.Azerbaijan.Race.1080p.WEB", false}, // last year's
		{bakuQuali, "", "Formula1.2026.Round15.Azerbaijan.Qualifying.SkyF1HD.1080p50", true},
		{bakuQuali, "", "F1.2026.R15.Azerbaijan.Grand.Prix.Sprint.Qualifying.1080p.WEB", false},
		{ufc300, "", "UFC.300.Pereira.vs.Hill.PPV.1080p.HDTV.x264-VERUM", true},
		{ufc300, PartMainCard, "UFC.300.1080p.WEB.h264-VERUM", true},
		{ufc300, "", "UFC.300.Prelims.720p.WEB.h264-VERUM", false},
		{ufc300, PartPrelims, "UFC.300.Prelims.720p.WEB.h264-VERUM", true},
		{ufc300, PartEarlyPrelims, "UFC.300.Early.Prelims.1080p.WEB-DL.H264.Fight-BB", true},
		{ufc300, PartPrelims, "UFC.300.Early.Prelims.1080p.WEB-DL.H264.Fight-BB", false},
		{ufc300, "", "UFC.301.Pantoja.vs.Erceg.1080p.WEB", false},
		{ufc300, "", "UFC.300.Embedded.Episode.3.720p", false},
		{fn288, "", "UFC.Fight.Night.288.Silva.vs.Delgado.1080p.WEB-DL.H264.Fight-BB", true},
		{fn288, "", "UFC.Fight.Night.287.Hooker.vs.Parnasse.1080p.WEB-DL", false},
		{raw, "", "WWE.Monday.Night.Raw.2026.10.19.1080p.WEB.h264-HEEL", true},
		{raw, "", "WWE.Monday.Night.Raw.2026.10.12.1080p.WEB.h264-HEEL", false},
		{raw, "", "WWE.SmackDown.2026.10.24.720p.HDTV.x264-NWCHD", false},
		{leafs, "", "NHL.2026.10.15.Toronto.Maple.Leafs.@.Montreal.Canadiens.1080p.HDTV", true},
		{leafs, "", "NHL.2026.10.15.Maple.Leafs.vs.Habs.720p", true}, // one team and the date
		{leafs, "", "NHL.2026.10.15.Oilers.vs.Flames.720p", false},
		{australia, "", "MotoGP.2026.Round18.Australia.Race.1080p.WEB-DL", true},
		{australia, "", "MotoGP.2026.Round18.Australia.Sprint.1080p.WEB", false},
		{crusaders, "", "Super.Rugby.2026.03.07.Crusaders.vs.Blues.720p", true},
		{crusaders, "", "Super.Rugby.2026.03.07.Chiefs.vs.Hurricanes.720p", false},
	} {
		v := Match(Parse(c.title), c.e, c.part)
		if v.Match != c.want {
			t.Errorf("%q for %q (%s): match=%v (%s, score %d)", c.title, c.e.Title, c.part, v.Match, v.Reject, v.Score)
		}
	}
}

// A release that names the date and both teams beats one that names the
// teams and the year only.
func TestSportsMatchScores(t *testing.T) {
	e := Event{League: nfl, Title: "Philadelphia Eagles vs Chicago Bears", Home: "Philadelphia Eagles", Away: "Chicago Bears", Round: "4", At: at("2026-09-29T00:15:00Z")}
	full := Match(Parse("NFL.2026.09.28.Philadelphia.Eagles.Vs.Chicago.Bears.720p.WEB"), e, "")
	week := Match(Parse("NFL.2026.Week.4.Eagles.vs.Bears.720p.WEB"), e, "")
	year := Match(Parse("NFL.2026.Eagles.vs.Bears.720p.WEB"), e, "")
	if !(full.Score > week.Score && week.Score > year.Score && year.Match) {
		t.Fatalf("scores: date %d, week %d, year %d (%v)", full.Score, week.Score, year.Score, year)
	}
}

func TestSportsQueries(t *testing.T) {
	for _, c := range []struct {
		e    Event
		part string
		want []string
	}{
		{Event{League: nfl, Title: "Philadelphia Eagles vs Chicago Bears", Home: "Philadelphia Eagles", Away: "Chicago Bears", At: at("2026-09-29T00:15:00Z")}, "",
			[]string{"nfl 2026 09 28", "nfl eagles bears"}},
		{Event{League: epl, Title: "Manchester United vs Arsenal", Home: "Manchester United", Away: "Arsenal", At: at("2025-11-02T16:30:00Z")}, "",
			[]string{"epl 2025 11 02", "epl manchester arsenal", "premier league 2025 11 02"}},
		{Event{League: f1, Title: "Azerbaijan Grand Prix", At: at("2026-09-20T11:00:00Z")}, "",
			[]string{"formula1 2026 azerbaijan", "f1 2026 azerbaijan"}},
		{Event{League: f1, Title: "Azerbaijan Grand Prix Qualifying", At: at("2026-09-19T12:00:00Z")}, "",
			[]string{"formula1 2026 azerbaijan qualifying", "f1 2026 azerbaijan qualifying"}},
		{Event{League: ufc, Title: "UFC 300 Pereira vs Hill", At: at("2026-04-14T02:00:00Z")}, PartPrelims,
			[]string{"ufc 300 prelims", "ufc pereira hill prelims", "ufc 2026 04 13 prelims"}},
		{Event{League: ufc, Title: "UFC Fight Night 288 Silva vs Delgado", At: at("2026-09-12T23:00:00Z")}, "",
			[]string{"ufc fight night 288", "ufc silva delgado", "ufc 2026 09 12"}},
		{Event{League: wwe, Title: "WWE Monday Night Raw", At: at("2026-10-20T00:00:00Z")}, "",
			[]string{"wwe 2026 10 19", "wwe monday night raw"}},
		{Event{League: motogp, Title: "Australian Motorcycle Grand Prix", At: at("2026-10-18T05:00:00Z")}, "",
			[]string{"motogp 2026 australia"}},
		{Event{League: f1, Title: "Saudi Arabian Grand Prix", At: at("2026-04-19T17:00:00Z")}, "",
			[]string{"formula1 2026 saudi arabia", "f1 2026 saudi arabia"}},
		{Event{League: rugby, Title: "Crusaders vs Blues", Home: "Crusaders", Away: "Blues", At: at("2026-03-07T06:35:00Z")}, "",
			[]string{"super rugby 2026 03 07", "super rugby crusaders blues"}},
	} {
		if got := Queries(c.e, c.part); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s (%s): %q, want %q", c.e.Title, c.part, got, c.want)
		}
	}
}

func TestSportsFileNames(t *testing.T) {
	league, season := Folder("UFC", SeasonNumber("2026", time.Time{}))
	if league != "UFC" || season != "Season 2026" {
		t.Fatalf("folder: %s / %s", league, season)
	}
	if n := SeasonNumber("2025-2026", time.Time{}); n != 2025 {
		t.Fatalf("season: %d", n)
	}
	got := FileName(FileInfo{League: "UFC", Season: 2026, Episode: 7, Title: "UFC 300 Pereira vs Hill", Part: PartPrelims, Quality: QualityLabel(Parse("UFC.300.Prelims.720p.WEB.h264")), Ext: ".MKV"})
	if got != "UFC - S2026E07 - UFC 300 Pereira vs Hill - Prelims - 720p WEB - pt2.mkv" {
		t.Fatalf("fight card part: %s", got)
	}
	got = FileName(FileInfo{League: "NFL", Season: 2026, Episode: 112, Title: "Philadelphia Eagles vs Chicago Bears", Quality: "1080p WEB", Ext: ".mkv"})
	if got != "NFL - S2026E112 - Philadelphia Eagles vs Chicago Bears - 1080p WEB.mkv" {
		t.Fatalf("game: %s", got)
	}
}

func TestLeagueKnowledge(t *testing.T) {
	if k := f1.Kind(); k != KindMotor {
		t.Fatalf("f1 kind %s", k)
	}
	if k := (League{Name: "Some Cup", Sport: "Soccer"}).Kind(); k != KindTeam {
		t.Fatalf("soccer kind %s", k)
	}
	if !nfl.American() || epl.American() {
		t.Fatal("american leagues")
	}
	if a := f1.Aliases(); a[0] != "f1" {
		t.Fatalf("aliases %v", a)
	}
	if got := (League{Name: "Super Rugby"}).SearchNames(); !reflect.DeepEqual(got, []string{"super rugby"}) {
		t.Fatalf("search names %v", got)
	}
}
