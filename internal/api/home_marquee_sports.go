package api

// Usherr: Sports on the Marquee. The games of the day in the leagues the
// screen follows: a live scoreboard scene, a "Tonight's games" scene, a
// score ticker and a kick-off countdown band. Scores come from the sports
// backend and, with a paid TheSportsDB key, live from TheSportsDB (the
// clock, updated every minute). A screen that doesn't show scores never
// gets them: they are taken out here, not hidden on the screen.

import (
	"context"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type marqueeSports struct {
	Games  []marqueeGame `json:"games"`
	Scores bool          `json:"scores"` // scores are shown on this screen
	Live   bool          `json:"live"`   // live scores (Sportarr's feed or a paid TheSportsDB key)
}

type marqueeGame struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	League      string    `json:"league,omitempty"`
	LeagueBadge string    `json:"leagueBadge,omitempty"`
	Home        string    `json:"home,omitempty"`
	Away        string    `json:"away,omitempty"`
	HomeBadge   string    `json:"homeBadge,omitempty"`
	AwayBadge   string    `json:"awayBadge,omitempty"`
	When        time.Time `json:"when"`
	Channel     string    `json:"channel,omitempty"`
	State       string    `json:"state"`               // "upcoming", "live" or "final"
	Clock       string    `json:"clock,omitempty"`     // live: "2H 85'", "Q3", "Lap 41"...
	HomeScore   *int      `json:"homeScore,omitempty"` // only when the screen shows scores
	AwayScore   *int      `json:"awayScore,omitempty"`
	Followed    bool      `json:"followed,omitempty"` // a team the household follows plays
	Venue       string    `json:"venue,omitempty"`
	Round       string    `json:"round,omitempty"`
	Image       string    `json:"image,omitempty"` // the game's picture
}

// marqueeSportsOn says whether a screen's settings use sports at all.
func marqueeSportsOn(set marqueeSettings) bool {
	if set.SportsTicker || set.SportsCountdown {
		return true
	}
	for _, sc := range set.Scenes {
		if sc.On && (sc.Kind == "scores" || sc.Kind == "sports-tonight") {
			return true
		}
	}
	return false
}

// The day's games, kept a minute: every screen asks every minute.
var marqueeGamesCache struct {
	sync.Mutex
	at    time.Time
	mode  string
	games []sportEvent
	teams []sportTeam
}

func (s *Server) marqueeDayGames(ctx context.Context) ([]sportEvent, []sportTeam, bool) {
	b, err := s.sports()
	if err != nil {
		return nil, nil, false
	}
	mode := s.sportsMode()
	marqueeGamesCache.Lock()
	defer marqueeGamesCache.Unlock()
	if marqueeGamesCache.mode == mode && time.Since(marqueeGamesCache.at) < time.Minute {
		return marqueeGamesCache.games, marqueeGamesCache.teams, true
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	now := time.Now()
	games, err := b.between(ctx, now.Add(-14*time.Hour), now.Add(36*time.Hour))
	if err != nil {
		return marqueeGamesCache.games, marqueeGamesCache.teams, len(marqueeGamesCache.games) > 0
	}
	teams, _ := b.followedTeams(ctx)
	marqueeGamesCache.at, marqueeGamesCache.mode, marqueeGamesCache.games, marqueeGamesCache.teams = time.Now(), mode, games, teams
	return games, teams, true
}

// marqueeSportsFor is a screen's sports: its leagues (or followed teams
// only), live first, then what's coming, then what's finished.
func (s *Server) marqueeSportsFor(ctx context.Context, set marqueeSettings) *marqueeSports {
	if !marqueeSportsOn(set) {
		return nil
	}
	games, teams, ok := s.marqueeDayGames(ctx)
	if !ok {
		return nil
	}
	leagues := map[string]bool{}
	for _, l := range set.SportsLeagues {
		leagues[strings.ToLower(l)] = true
	}
	followed := func(name string) bool {
		n := normWords(name)
		for _, t := range teams {
			if n != "" && normWords(t.Name) == n {
				return true
			}
		}
		return false
	}
	live := s.liveScores(ctx, games, set.SportsLeagues)
	out := &marqueeSports{Games: []marqueeGame{}, Scores: set.SportsScores, Live: live != nil}
	now := time.Now()
	for _, e := range games {
		if len(leagues) > 0 && !leagues[strings.ToLower(e.League)] {
			continue
		}
		g := gameCard(e, now, live, set.SportsScores)
		g.Followed = followed(g.Home) || followed(g.Away)
		if set.SportsTeamsOnly && !g.Followed {
			continue
		}
		if g.State != "final" {
			s.withCrests(&g) // the ones that may be shown big first
		}
		out.Games = append(out.Games, g)
	}
	rank := map[string]int{"live": 0, "upcoming": 1, "final": 2}
	sort.SliceStable(out.Games, func(i, j int) bool {
		a, b := out.Games[i], out.Games[j]
		if rank[a.State] != rank[b.State] {
			return rank[a.State] < rank[b.State]
		}
		if a.Followed != b.Followed {
			return a.Followed
		}
		if a.State == "final" {
			return a.When.After(b.When)
		}
		return a.When.Before(b.When)
	})
	if len(out.Games) > 60 {
		out.Games = out.Games[:60]
	}
	return out
}

// gameCard is a game as the Marquee shows it: its live score when there is
// one, and no score at all when the screen doesn't show them.
func gameCard(e sportEvent, now time.Time, live map[string]liveScore, scores bool) marqueeGame {
	if e.Home == "" && e.Away == "" {
		e.Home, e.Away = teamsOf(e)
	}
	g := marqueeGame{ID: e.ID, Title: e.Title, League: e.League, LeagueBadge: e.LeagueBadge, Home: e.Home, Away: e.Away, When: e.When,
		Channel: e.Broadcast, HomeScore: e.HomeScore, AwayScore: e.AwayScore, Venue: e.Venue, Round: e.Round, Image: e.Image}
	g.State = gameState(e, now)
	if l, ok := live[liveKey(e.Home, e.Away)]; ok && l.state != "upcoming" {
		g.State, g.Clock, g.HomeScore, g.AwayScore = l.state, l.clock, l.home, l.away
		g.HomeBadge, g.AwayBadge = l.homeBadge, l.awayBadge
	}
	if n, err := strconv.Atoi(g.Round); err == nil && (n <= 0 || n > 99) {
		g.Round = "" // Sportarr numbers pre-season and special games 500 and up: not a round
	}
	if !scores {
		g.HomeScore, g.AwayScore = nil, nil
		if g.State == "live" && strings.TrimSpace(g.Clock) != "" {
			g.Clock = "" // the minute can give a result away too ("FT", "AET")
		}
	}
	return g
}

// teamsOf reads a team game's two sides from its title ("Green Bay Packers
// vs Dallas Cowboys") when the backend doesn't give them. Fights and races
// keep their title: "UFC 330 Smith vs Jones" isn't two teams.
func teamsOf(e sportEvent) (string, string) {
	sport := strings.ToLower(e.Sport)
	for _, w := range []string{"fight", "combat", "mma", "boxing", "wrestl", "motor", "racing", "golf", "tennis", "cycling", "athletic"} {
		if strings.Contains(sport, w) {
			return "", ""
		}
	}
	parts := strings.Split(e.Title, " vs ")
	if len(parts) != 2 {
		return "", ""
	}
	h, a := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	if h == "" || a == "" || strings.ContainsAny(h+a, "0123456789") {
		return "", ""
	}
	return h, a
}

// Teams' crests from TheSportsDB (a paid key), looked up once each in the
// background: a screen never waits for them, it gets them the next minute.
var teamCrestLookups = true // tests turn the background lookups off

var teamCrests struct {
	sync.Mutex
	m       map[string]string // normalised name -> crest ("" = none found)
	pending map[string]bool
}

func (s *Server) teamCrest(name string) string {
	key := normWords(name)
	if key == "" {
		return ""
	}
	teamCrests.Lock()
	defer teamCrests.Unlock()
	if teamCrests.m == nil {
		teamCrests.m, teamCrests.pending = map[string]string{}, map[string]bool{}
	}
	if c, ok := teamCrests.m[key]; ok {
		return c
	}
	if teamCrests.pending[key] || !teamCrestLookups || !s.tsdb().paid() {
		return ""
	}
	teamCrests.pending[key] = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		crest, done := "", false
		if found, err := s.tsdb().searchTeams(ctx, name); err == nil {
			done = true
			for _, t := range found {
				if normWords(t.Name) == key {
					crest = t.Badge
					break
				}
			}
			if crest == "" && len(found) == 1 {
				crest = found[0].Badge
			}
		}
		teamCrests.Lock()
		delete(teamCrests.pending, key)
		if done {
			teamCrests.m[key] = crest
		}
		teamCrests.Unlock()
	}()
	return ""
}

// withCrests fills in the teams' crests a game lacks.
func (s *Server) withCrests(g *marqueeGame) {
	if g.Home == "" || g.Away == "" {
		return
	}
	if g.HomeBadge == "" {
		g.HomeBadge = s.teamCrest(g.Home)
	}
	if g.AwayBadge == "" {
		g.AwayBadge = s.teamCrest(g.Away)
	}
}

// ---- the game being watched ----

// Games of the last ten days, for telling which one is being watched (a
// game is often watched the day after); kept ten minutes.
var marqueeRecentGames struct {
	sync.Mutex
	at    time.Time
	mode  string
	games []sportEvent
}

func (s *Server) recentGames(ctx context.Context) []sportEvent {
	b, err := s.sports()
	if err != nil {
		return nil
	}
	mode := s.sportsMode()
	marqueeRecentGames.Lock()
	defer marqueeRecentGames.Unlock()
	if marqueeRecentGames.mode == mode && time.Since(marqueeRecentGames.at) < 10*time.Minute {
		return marqueeRecentGames.games
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	now := time.Now()
	if games, err := b.between(ctx, now.AddDate(0, 0, -10), now.Add(24*time.Hour)); err == nil {
		marqueeRecentGames.at, marqueeRecentGames.mode, marqueeRecentGames.games = time.Now(), mode, games
	}
	return marqueeRecentGames.games
}

// identifies says whether a title (a file's or a media server's) is this
// game: the league named, and both teams, or (with no teams) the matcher's
// date or title words. A league and a date alone is a whole day's games.
func (g ownGame) identifies(title string) bool {
	if g.home != "" && g.away != "" {
		t := " " + normWords(title) + " "
		league := false
		for _, a := range g.aliases {
			league = league || strings.Contains(t, " "+a+" ")
		}
		h, a := lastWord(g.home), lastWord(g.away)
		return league && h != "" && a != "" && strings.Contains(t, " "+h+" ") && strings.Contains(t, " "+a+" ") && !sideProgram(title, g.title)
	}
	return g.matches(title) && !sideProgram(title, g.title)
}

var nameDate = regexp.MustCompile(`(?:^|\D)(20\d\d)[ ._-](\d\d)[ ._-](\d\d)(?:\D|$)`)

// Games around a day, by day, kept an hour (a replay is watched for hours).
var gamesByDay struct {
	sync.Mutex
	m map[string]gamesOfDay
}

type gamesOfDay struct {
	games []sportEvent
	at    time.Time
}

// gameOnTheDay finds the game a name points to by the date in it
// ("NBA.Playoffs.2026.05.19.Oklahoma.City.Thunder.vs.San.Antonio.Spurs"):
// that day's games (a day either way) and the league and teams.
func (s *Server) gameOnTheDay(ctx context.Context, name string) *sportEvent {
	m := nameDate.FindStringSubmatch(name)
	if m == nil {
		return nil
	}
	day, err := time.Parse("2006-01-02", m[1]+"-"+m[2]+"-"+m[3])
	if err != nil {
		return nil
	}
	b, err := s.sports()
	if err != nil {
		return nil
	}
	key := s.sportsMode() + "|" + day.Format("2006-01-02")
	gamesByDay.Lock()
	if gamesByDay.m == nil {
		gamesByDay.m = map[string]gamesOfDay{}
	}
	d, ok := gamesByDay.m[key]
	gamesByDay.Unlock()
	if !ok || time.Since(d.at) > time.Hour {
		ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		games, err := b.between(ctx, day.Add(-24*time.Hour), day.Add(48*time.Hour))
		if err != nil {
			return nil
		}
		d = gamesOfDay{games: games, at: time.Now()}
		gamesByDay.Lock()
		gamesByDay.m[key] = d
		for k, v := range gamesByDay.m {
			if time.Since(v.at) > 2*time.Hour {
				delete(gamesByDay.m, k)
			}
		}
		gamesByDay.Unlock()
	}
	for i := range d.games {
		e := d.games[i]
		home, away := e.Home, e.Away
		if home == "" && away == "" {
			home, away = teamsOf(e)
		}
		g := ownGame{title: e.Title, home: home, away: away, league: e.League, aliases: leagueAliases(e.League, ""), at: e.When}
		if g.identifies(name) {
			return &e
		}
	}
	return nil
}

// marqueeGamesPlaying puts the game on what's being watched: the game a
// stream from Sports says it is, or the one a media server's title names.
// What's watched after the game is never given its final score.
func (s *Server) marqueeGamesPlaying(ctx context.Context, set marqueeSettings, list []marqueePlaying) {
	if _, err := s.sports(); err != nil || len(list) == 0 {
		return
	}
	var games []sportEvent
	for i := range list {
		p := &list[i]
		if p.Kind == "movie" || p.TMDBID > 0 || (p.GameID == "" && p.SeriesTMDB > 0) {
			continue
		}
		if games == nil {
			games = s.recentGames(ctx)
		}
		var found *sportEvent
		for j := range games {
			e := &games[j]
			if p.GameID != "" {
				if e.ID == p.GameID {
					found = e
					break
				}
				continue
			}
			home, away := e.Home, e.Away
			if home == "" && away == "" {
				home, away = teamsOf(*e)
			}
			g := ownGame{title: e.Title, home: home, away: away, league: e.League, aliases: leagueAliases(e.League, ""), at: e.When}
			if g.identifies(p.Series + " " + p.Title + " " + p.Release) {
				found = e
				break
			}
		}
		if found == nil && p.GameID == "" { // an older game: the day its name says
			found = s.gameOnTheDay(ctx, p.Series+" "+p.Title+" "+p.Release)
		}
		if found == nil && p.GameID != "" { // an older game: asked for by its id
			type byID interface {
				game(ctx context.Context, id string) (sportEvent, error)
			}
			if b, err := s.sports(); err == nil {
				if g, ok := b.(byID); ok {
					if e, err := g.game(ctx, p.GameID); err == nil {
						found = &e
					}
				}
			}
		}
		if found == nil {
			continue
		}
		live := s.liveScores(ctx, []sportEvent{*found}, nil)
		g := gameCard(*found, time.Now(), live, set.SportsScores && gameState(*found, time.Now()) == "live")
		s.withCrests(&g)
		p.Game = &g
	}
}

// gameState reads a game's state from the backend: finished, under way (from
// its start for three hours, while not finished) or still to come.
func gameState(e sportEvent, now time.Time) string {
	st := strings.ToLower(e.Status)
	switch {
	case strings.Contains(st, "finish") || strings.Contains(st, "final") || strings.Contains(st, "complete") || st == "ft" || st == "aet" || st == "ap":
		return "final"
	case strings.Contains(st, "progress") || st == "live" || st == "1h" || st == "2h" || st == "ht" || (len(st) == 2 && st[0] == 'q'):
		return "live" // the backend says so (Sportarr: "in_progress")
	case e.When.IsZero() || e.When.After(now):
		return "upcoming"
	case now.Sub(e.When) < 3*time.Hour:
		return "live"
	}
	return "final"
}

// ---- live scores (a paid TheSportsDB key) ----

type liveScore struct {
	state, clock         string
	home, away           *int
	homeBadge, awayBadge string
}

func liveKey(home, away string) string { return normWords(home) + "|" + normWords(away) }

var liveCache struct {
	sync.Mutex
	at     map[string]time.Time // league -> when asked
	scores map[string]map[string]liveScore
	ids    map[string]string // league name -> TheSportsDB's idLeague ("" = not found)
}

// liveScores are the scores of the games under way now, by team names:
// from Sportarr's live feed (by sport) when Sports runs on it, and from
// TheSportsDB with a paid key (by league; it adds the clock and the crests).
// Each source is asked at most once a minute. nil when there's neither.
func (s *Server) liveScores(ctx context.Context, games []sportEvent, only []string) map[string]liveScore {
	want := map[string]bool{}
	for _, l := range only {
		want[strings.ToLower(l)] = true
	}
	now := time.Now()
	leagues, sports := map[string]bool{}, map[string]bool{}
	for _, e := range games {
		if gameState(e, now) == "live" && (len(want) == 0 || want[strings.ToLower(e.League)]) {
			if e.League != "" {
				leagues[e.League] = true
			}
			if e.Sport != "" {
				sports[e.Sport] = true
			}
		}
	}
	type feed interface {
		liveFeed(ctx context.Context, sport string) ([]tsdbRow, error)
	}
	b, _ := s.sports()
	fromSportarr, hasFeed := b.(feed)
	c := s.tsdb()
	if !hasFeed && !c.paid() {
		return nil
	}
	out := map[string]liveScore{}
	liveCache.Lock()
	defer liveCache.Unlock()
	if liveCache.at == nil {
		liveCache.at, liveCache.scores, liveCache.ids = map[string]time.Time{}, map[string]map[string]liveScore{}, map[string]string{}
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	ask := func(key string, get func() ([]tsdbRow, error)) {
		if time.Since(liveCache.at[key]) >= time.Minute {
			if rows, err := get(); err == nil {
				m := map[string]liveScore{}
				for _, r := range rows {
					m[liveKey(r.str("strHomeTeam"), r.str("strAwayTeam"))] = liveScoreOf(r)
				}
				liveCache.scores[key] = m
			}
			liveCache.at[key] = time.Now()
		}
		for k, v := range liveCache.scores[key] {
			if old, ok := out[k]; ok { // the clock and crests fill in what the other source lacks
				if v.clock == "" {
					v.clock = old.clock
				}
				if v.homeBadge == "" {
					v.homeBadge, v.awayBadge = old.homeBadge, old.awayBadge
				}
			}
			out[k] = v
		}
	}
	if hasFeed {
		for sport := range sports {
			ask("sportarr|"+sport, func() ([]tsdbRow, error) { return fromSportarr.liveFeed(ctx, sport) })
		}
	}
	if c.paid() {
		for league := range leagues {
			id, known := liveCache.ids[league]
			if !known {
				if found, err := c.searchLeagues(ctx, league); err == nil {
					for _, l := range found {
						if strings.EqualFold(l.Name, league) {
							id = l.ExternalID
							break
						}
					}
					if id == "" && len(found) == 1 {
						id = found[0].ExternalID
					}
					liveCache.ids[league] = id
				}
			}
			if id != "" {
				ask("tsdb|"+league, func() ([]tsdbRow, error) { return c.rows(ctx, "livescore/"+tsdbSlug(id), "") })
			}
		}
	}
	return out
}

func liveScoreOf(r tsdbRow) liveScore {
	l := liveScore{state: "live", home: r.num("intHomeScore"), away: r.num("intAwayScore"), homeBadge: r.str("strHomeTeamBadge"), awayBadge: r.str("strAwayTeamBadge")}
	status, progress := r.str("strStatus"), r.str("strProgress")
	st := strings.ToLower(status)
	switch {
	case st == "ft" || st == "aet" || st == "pen" || st == "ap" || strings.Contains(st, "finish") || strings.Contains(st, "final") || strings.Contains(st, "complete"):
		l.state = "final"
	case st == "" || st == "ns" || strings.Contains(st, "sched") || strings.Contains(st, "not started") || strings.Contains(st, "postpon") || strings.Contains(st, "cancel"):
		l.state = "upcoming" // nothing to say yet
		return l
	}
	switch {
	case l.state == "final":
		if len(status) <= 4 {
			l.clock = status // "FT", "AET"
		}
	case progress != "":
		if _, err := time.ParseDuration(progress + "m"); err == nil {
			progress += "'"
		}
		l.clock = strings.TrimSpace(status + " " + progress)
	case len(status) <= 4 && !strings.EqualFold(status, "live"):
		l.clock = status // "Q3", "2H", "HT"
	}
	return l
}
