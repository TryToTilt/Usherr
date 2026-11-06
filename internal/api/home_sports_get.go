package api

// Usherr: getting games on Usherr's own sports engine, without Sportarr.
// Each league (and each followed team) says whether its new games are got,
// which events (a fight card's parts, a race weekend's sessions) and in
// which quality profile. Usherr searches the indexers it already has for a
// game once it has been played, picks the release that is that game (the
// sports release parser, internal/sports) and that the profile takes, and
// sends it down the usual download line; the import files it in the Sports
// folder the way Plex and Jellyfin read it (home_sports_import.go).
//
// The indexers' daily limits are precious: a game is searched a few hours
// after it ends, then less and less often, for two weeks at most; each
// search stops at the first query that finds the game; and the automatic
// search adds no more than the usual ten downloads an hour.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/blocklist"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/plainerror"
	"github.com/rdborg/mediarium/internal/quality"
	"github.com/rdborg/mediarium/internal/queue"
	"github.com/rdborg/mediarium/internal/sports"
)

const (
	keySportsPath       = "sports.path"        // the Sports folder; "" = "Sports" next to the TV folder
	keySportsProfile    = "sports.profile_id"  // the quality profile games use by default; 0 = the shows' default
	keySportsAutoSearch = "sports.auto_search" // "0" stops the automatic searches ("" = on)
	keySportsSearchDays = "sports.search_days" // how many days after a game the automatic search keeps looking

	defaultSportsSearchDays = 14
	// sportsAfterEnd is how long after its start a game is first searched:
	// it has to end, and be posted.
	sportsAfterEnd = 3 * time.Hour
	// maxSportsSearchesPerHunt keeps one automatic run small.
	maxSportsSearchesPerHunt = 10
)

// sportsDownloadCategories are Newznab's TV and TV > Sport.
var sportsDownloadCategories = []int{5000, 5060}

// sportsRoot is the Sports folder: the setting, else "Sports" next to the
// TV folder.
func (s *Server) sportsRoot() string {
	if v, _ := s.Settings.Get(keySportsPath); strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return s.defaultSportsRoot()
}

func (s *Server) defaultSportsRoot() string {
	tv := strings.TrimSpace(s.tvRoot())
	if tv == "" {
		return ""
	}
	parent := filepath.Dir(filepath.Clean(tv))
	if parent == filepath.Dir(parent) {
		// The TV folder is a mount of its own (/tv): next to it is the
		// container's own disk, not the media. A folder must be chosen.
		return ""
	}
	return filepath.Join(parent, "Sports")
}

func (s *Server) sportsAutoSearch() bool {
	v, _ := s.Settings.Get(keySportsAutoSearch)
	return v != "0"
}

func (s *Server) sportsSearchDays() int {
	v, _ := s.Settings.Get(keySportsSearchDays)
	if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n >= 1 && n <= 60 {
		return n
	}
	return defaultSportsSearchDays
}

func (s *Server) sportsDefaultProfileID() int64 {
	v, _ := s.Settings.Get(keySportsProfile)
	id, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	return id
}

// sportsDownloadsOn says Usherr gets games itself: its own engine, with a
// Sports folder.
func (s *Server) sportsDownloadsOn() bool {
	return s.sportsMode() == "usherr" && s.sportsRoot() != ""
}

// ---- a game, as getting it needs it ----

type sportGame struct {
	id, leagueID   int64
	ev             sports.Event
	externalID     string
	season         string
	venue, image   string
	monitored      bool
	searchedMs     int64
	leagueProfile  int64
	leagueParts    string
	leagueBadge    string
	leagueSport    string
	leagueMonitor  bool
	leagueTeamOnly bool
}

func (g sportGame) label() string {
	if g.ev.League.Name != "" && !strings.Contains(strings.ToLower(g.ev.Title), strings.ToLower(g.ev.League.Name)) {
		return g.ev.League.Name + " · " + g.ev.Title
	}
	return g.ev.Title
}

const sportGameCols = `e.id, e.league_id, e.external_id, e.title, e.home, e.away, e.season, e.round, e.at_ms, e.venue, e.image, e.monitored, e.searched_ms,
	l.name, l.alternate, l.sport, l.badge, l.profile_id, l.parts, l.monitored, l.teams_only`

func scanSportGame(scan func(...any) error) (sportGame, error) {
	var g sportGame
	var at int64
	err := scan(&g.id, &g.leagueID, &g.externalID, &g.ev.Title, &g.ev.Home, &g.ev.Away, &g.season, &g.ev.Round, &at, &g.venue, &g.image, &g.monitored, &g.searchedMs,
		&g.ev.League.Name, &g.ev.League.Alternate, &g.ev.League.Sport, &g.leagueBadge, &g.leagueProfile, &g.leagueParts, &g.leagueMonitor, &g.leagueTeamOnly)
	if err != nil {
		return g, err
	}
	g.ev.Season, g.leagueSport = g.season, g.ev.League.Sport
	if at > 0 {
		g.ev.At = time.UnixMilli(at).UTC()
	}
	return g, nil
}

func (s *Server) sportGame(ctx context.Context, id int64) (sportGame, error) {
	g, err := scanSportGame(s.db.QueryRowContext(ctx, `SELECT `+sportGameCols+` FROM sports_events e JOIN sports_leagues l ON l.id = e.league_id WHERE e.id = ?`, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return g, errSportNotFound
	}
	return g, err
}

// choices reads a league's comma list of event choices.
func choices(list string) map[string]bool {
	out := map[string]bool{}
	for _, c := range strings.Split(list, ",") {
		if c = strings.TrimSpace(strings.ToLower(c)); c != "" {
			out[c] = true
		}
	}
	return out
}

// wantedParts are the parts of a game that are got: a fight card's chosen
// parts (all when none is chosen), or the game itself ("").
func (g sportGame) wantedParts() []string {
	if g.ev.Kind() != sports.KindFight {
		return []string{""}
	}
	ch := choices(g.leagueParts)
	var out []string
	for _, p := range sports.FightParts {
		if len(ch) == 0 || ch[sports.PartChoice(p)] {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		out = []string{sports.PartMainCard}
	}
	return out
}

// sessionWanted says a race weekend's event is one of the league's chosen
// sessions (any other event is).
func sessionWanted(kind sports.Kind, title, list string) bool {
	if kind != sports.KindMotor {
		return true
	}
	ch := choices(list)
	return len(ch) == 0 || ch[sports.SessionChoice(sports.SessionOf(title))]
}

// ---- monitoring rules ----

type followedTeam struct {
	name      string
	monitored bool
	profileID int64
}

func (s *Server) followedSportTeams(ctx context.Context) []followedTeam {
	rows, err := s.db.QueryContext(ctx, `SELECT name, monitored, profile_id FROM sports_teams`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []followedTeam
	for rows.Next() {
		var t followedTeam
		if rows.Scan(&t.name, &t.monitored, &t.profileID) == nil {
			out = append(out, t)
		}
	}
	return out
}

// plays says a team is one of a game's two sides.
func plays(team, home, away string) bool {
	t := sports.Words(team)
	return t != "" && (sports.Words(home) == t || sports.Words(away) == t)
}

// applySportsRules marks a league's recent and coming games wanted or not,
// by the league's and the followed teams' choices. Games a person chose
// (Get, or not) stay as they are, and so do games played more than a day
// and a half ago.
func (s *Server) applySportsRules(ctx context.Context, leagueID int64) {
	var monitored, teamsOnly bool
	var parts, name, alternate, sport string
	if err := s.db.QueryRowContext(ctx, `SELECT monitored, teams_only, parts, name, alternate, sport FROM sports_leagues WHERE id = ?`, leagueID).
		Scan(&monitored, &teamsOnly, &parts, &name, &alternate, &sport); err != nil {
		return
	}
	kind := (sports.League{Name: name, Alternate: alternate, Sport: sport}).Kind()
	teams := s.followedSportTeams(ctx)
	rows, err := s.db.QueryContext(ctx, `SELECT id, title, home, away, monitored FROM sports_events WHERE league_id = ? AND by_hand = 0 AND (at_ms = 0 OR at_ms >= ?)`,
		leagueID, time.Now().Add(-36*time.Hour).UnixMilli())
	if err != nil {
		return
	}
	type change struct {
		id   int64
		want bool
	}
	var changes []change
	for rows.Next() {
		var id int64
		var title, home, away string
		var was bool
		if rows.Scan(&id, &title, &home, &away, &was) != nil {
			continue
		}
		want := false
		if monitored && sessionWanted(kind, title, parts) {
			want = true
			if teamsOnly {
				want = false
				for _, t := range teams {
					if plays(t.name, home, away) {
						want = true
					}
				}
			}
		}
		for _, t := range teams {
			if t.monitored && plays(t.name, home, away) {
				want = true
			}
		}
		if want != was {
			changes = append(changes, change{id, want})
		}
	}
	rows.Close()
	for _, c := range changes {
		_, _ = s.db.ExecContext(ctx, `UPDATE sports_events SET monitored = ? WHERE id = ?`, c.want, c.id)
	}
}

// applyAllSportsRules applies the rules to every league (a team's choice
// touches them all).
func (s *Server) applyAllSportsRules(ctx context.Context) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM sports_leagues`)
	if err != nil {
		return
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		s.applySportsRules(ctx, id)
	}
}

// ---- the quality profile ----

// sportProfile is the profile that rules a game: its league's, else a
// followed team's, else Sports' default, else the shows' default.
func (s *Server) sportProfile(ctx context.Context, ps profileSet, g sportGame) quality.Profile {
	id := g.leagueProfile
	if id == 0 {
		for _, t := range s.followedSportTeams(ctx) {
			if t.profileID > 0 && plays(t.name, g.ev.Home, g.ev.Away) {
				id = t.profileID
				break
			}
		}
	}
	if id == 0 {
		id = s.sportsDefaultProfileID()
	}
	return ps.resolveTV(id)
}

// ---- finding a game's releases ----

// sportCandidate is a release found for a game. It holds the real download
// address, so it never goes to the page as it is.
type sportCandidate struct {
	indexers.Result
	Verdict  sports.Verdict
	Tier     quality.Tier
	Accepted bool     // the profile takes it
	Reasons  []string // why it isn't taken (not the game, or not the profile's)
	Blocked  bool
	score    int
	rank     int
}

func (c sportCandidate) usable() bool { return c.Verdict.Match && c.Accepted && !c.Blocked }

// minSportsSize: smaller is a clip or a sample, not a whole game.
const minSportsSize = 100 << 20

// sportCandidates searches the indexers for a game (one part of it) and
// judges what they answer, best first. own replaces the game's own searches
// with a person's words. With stopEarly, the searches stop at the first that
// finds something usable (the automatic search).
func (s *Server) sportCandidates(ctx context.Context, g sportGame, part, own string, stopEarly bool) ([]sportCandidate, error) {
	list, err := s.searchableIndexers()
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, errors.New("there are no indexers to search. Add one in Settings > Indexers & Search")
	}
	ps, err := s.loadProfiles()
	if err != nil {
		return nil, err
	}
	profile := s.sportProfile(ctx, ps, g)
	blocked := s.blockedKeys()
	queries := sports.Queries(g.ev, part)
	if own = strings.TrimSpace(own); own != "" {
		queries = []string{own}
	}
	seen := map[string]bool{}
	var out []sportCandidate
	for _, q := range queries {
		if ctx.Err() != nil {
			break
		}
		for _, res := range indexers.MergeResults(indexers.SearchAll(ctx, list, q, sportsDownloadCategories)) {
			if seen[res.DownloadURL+"|"+res.Title] {
				continue
			}
			seen[res.DownloadURL+"|"+res.Title] = true
			out = append(out, judgeSportRelease(res, g, part, profile, blocked))
		}
		if stopEarly && hasUsable(out) {
			break
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return betterSportCandidate(out[i], out[j]) })
	return out, nil
}

func hasUsable(list []sportCandidate) bool {
	for _, c := range list {
		if c.usable() {
			return true
		}
	}
	return false
}

// judgeSportRelease says whether a release is the game and the profile
// takes it.
func judgeSportRelease(res indexers.Result, g sportGame, part string, profile quality.Profile, blocked map[string]bool) sportCandidate {
	rel := sports.Parse(res.Title)
	c := sportCandidate{Result: res, Tier: rel.Tier, Verdict: sports.Match(rel, g.ev, part)}
	c.Blocked = blocked[blocklist.Key(res.Title)]
	if !c.Verdict.Match {
		c.Reasons = append(c.Reasons, "Not this game: "+c.Verdict.Reject+".")
	}
	if res.SizeBytes > 0 && res.SizeBytes < minSportsSize {
		c.Verdict.Match = false
		c.Reasons = append(c.Reasons, "Too small to be the whole game (a clip or a sample).")
	}
	pr := parser.Parse(sports.Clean(res.Title))
	pr.Resolution, pr.Source = rel.Resolution, rel.Source
	c.Accepted = true
	if ok, why := profile.TitleAllowedRel(res.Title, pr); !ok {
		c.Accepted = false
		c.Reasons = append(c.Reasons, "The quality profile: "+why+".")
	} else if !profile.Accepts(pr) {
		c.Accepted = false
		c.Reasons = append(c.Reasons, fmt.Sprintf("The %q profile doesn't take %s.", profile.Name, rel.Tier))
	}
	if c.Accepted && res.SizeBytes > 0 && !profile.SizeAllowed(res.SizeBytes) {
		c.Accepted = false
		c.Reasons = append(c.Reasons, fmt.Sprintf("Bigger than the %q profile allows.", profile.Name))
	}
	if c.Blocked {
		c.Reasons = append(c.Reasons, "On the blocklist (it failed before).")
	}
	c.rank = profile.TierRank(rel.Tier)
	c.score = profile.Score(res.Title)
	return c
}

func betterSportCandidate(a, b sportCandidate) bool {
	if a.usable() != b.usable() {
		return a.usable()
	}
	if a.Verdict.Match != b.Verdict.Match {
		return a.Verdict.Match
	}
	if a.rank != b.rank {
		return a.rank > b.rank
	}
	if a.score != b.score {
		return a.score > b.score
	}
	if a.Verdict.Score != b.Verdict.Score {
		return a.Verdict.Score > b.Verdict.Score
	}
	if a.Priority != b.Priority {
		return indexerRank(a.Priority) < indexerRank(b.Priority)
	}
	if a.Protocol != b.Protocol {
		return a.Protocol == indexers.ProtocolUsenet
	}
	return a.SizeBytes > b.SizeBytes
}

// ---- searching and grabbing ----

// sportPartBusy says a download for a game's part is waiting or running.
func (s *Server) sportPartBusy(ctx context.Context, eventID int64, part string) bool {
	var n int
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sports_downloads d JOIN download_queue q ON q.id = d.queue_id
		WHERE d.event_id = ? AND d.part = ? AND q.status IN ('queued','downloading','importing','paused')`, eventID, part).Scan(&n)
	return n > 0
}

// sportPartFile is the file a game's part has ("" for none).
func (s *Server) sportPartFile(ctx context.Context, eventID int64, part string) (path, tier string) {
	_ = s.db.QueryRowContext(ctx, `SELECT path, quality FROM sports_files WHERE event_id = ? AND part = ?`, eventID, part).Scan(&path, &tier)
	return path, tier
}

// missingParts are the wanted parts of a game with no file and no download.
func (s *Server) missingParts(ctx context.Context, g sportGame) []string {
	var out []string
	for _, p := range g.wantedParts() {
		if f, _ := s.sportPartFile(ctx, g.id, p); f != "" && fileExists(f) {
			continue
		}
		if s.sportPartBusy(ctx, g.id, p) {
			continue
		}
		out = append(out, p)
	}
	return out
}

// searchSportGame looks for a game's missing parts and grabs the best
// release of each. It reports how many it grabbed and what it found.
func (s *Server) searchSportGame(ctx context.Context, id int64, prio queue.Priority) (int, string) {
	g, err := s.sportGame(ctx, id)
	if err != nil {
		return 0, plainerror.Message(err)
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE sports_events SET searched_ms = ? WHERE id = ?`, time.Now().UnixMilli(), id)
	if prio == queue.PriorityManual {
		ctx = indexers.AsManual(ctx)
	}
	parts := s.missingParts(ctx, g)
	if len(parts) == 0 {
		return 0, "This game has its files, or they're downloading."
	}
	sources := s.sourcesFor("")
	grabbed, nothing := 0, []string{}
	for _, part := range parts {
		list, err := s.sportCandidates(ctx, g, part, "", true)
		if err != nil {
			return grabbed, plainerror.Message(err)
		}
		done := false
		for _, c := range list {
			if !c.usable() || len(filterSources([]indexers.Result{c.Result}, sources)) == 0 {
				continue
			}
			if _, err := s.grabSportGame(g, part, c.Title, c.DownloadURL, c.SizeBytes, c.Protocol, prio); err == nil {
				grabbed++
				done = true
			}
			break
		}
		if !done {
			nothing = append(nothing, firstNonEmpty(strings.ToLower(part), "it"))
		}
	}
	switch {
	case grabbed > 0 && len(nothing) == 0:
		return grabbed, "Found it. It's downloading now."
	case grabbed > 0:
		return grabbed, "Found part of it, downloading now. Nothing yet for the " + strings.Join(nothing, ", ") + "."
	}
	if prio == queue.PriorityManual {
		_ = s.QueueRepo.LogActivity(0, "searched", "No release found for "+g.label())
	}
	return 0, "Nothing suitable was found on your indexers right now."
}

// grabSportGame puts a release of a game (one part of it) in the download
// line.
func (s *Server) grabSportGame(g sportGame, part, title, url string, size int64, protocol indexers.Protocol, prio queue.Priority) (int64, error) {
	if protocol == indexers.ProtocolTorrent && !s.torrentsEnabled() {
		return 0, errTorrentsDisabled
	}
	if s.sportsRoot() == "" {
		return 0, errors.New("Choose the Sports folder in Settings > Extras > Sports first.")
	}
	ctx := context.Background()
	s.grabMu.Lock()
	var id int64
	var err error
	if s.sportPartBusy(ctx, g.id, part) {
		err = errors.New("A download for this game is already running or waiting.")
	}
	if err == nil {
		released := ""
		if !g.ev.At.IsZero() {
			released = g.ev.At.Format("2006-01-02")
		}
		id, err = s.QueueRepo.Enqueue(queue.Item{ReleaseTitle: title, NZBURL: url, SizeBytes: size, Protocol: queue.Protocol(protocol), Priority: prio, Released: released})
	}
	if err == nil {
		if _, err = s.db.Exec(`INSERT INTO sports_downloads (queue_id, event_id, part) VALUES (?, ?, ?)`, id, g.id, part); err != nil {
			_ = s.QueueRepo.SetStatus(id, queue.StatusFailed, "couldn't link the download to its game")
		}
	}
	s.grabMu.Unlock()
	if err != nil {
		return 0, err
	}
	what := g.label()
	if part != "" {
		what += " (" + part + ")"
	}
	_ = s.QueueRepo.LogActivity(0, "grabbed", s.grabbedMessage(title, what))
	s.dispatch.Kick()
	return id, nil
}

// ---- the automatic search ----

var sportsHunting sync.Map // *Server -> *sync.Mutex

// sportsSearchGap is how long the automatic search waits before looking
// for a game again: often just after it, then less and less.
func sportsSearchGap(sinceStart time.Duration) time.Duration {
	switch {
	case sinceStart < 12*time.Hour:
		return 3 * time.Hour
	case sinceStart < 48*time.Hour:
		return 8 * time.Hour
	}
	return 24 * time.Hour
}

// dueSportGames are the wanted games the automatic search looks for now:
// played, within the days it keeps looking, missing a part, and not
// searched too recently.
func (s *Server) dueSportGames(ctx context.Context, now time.Time) []sportGame {
	rows, err := s.db.QueryContext(ctx, `SELECT `+sportGameCols+` FROM sports_events e JOIN sports_leagues l ON l.id = e.league_id
		WHERE e.monitored = 1 AND e.at_ms > 0 AND e.at_ms <= ? AND e.at_ms >= ? ORDER BY e.at_ms DESC LIMIT 300`,
		now.Add(-sportsAfterEnd).UnixMilli(), now.AddDate(0, 0, -s.sportsSearchDays()).UnixMilli())
	if err != nil {
		return nil
	}
	var all []sportGame
	for rows.Next() {
		if g, err := scanSportGame(rows.Scan); err == nil {
			all = append(all, g)
		}
	}
	rows.Close()
	var out []sportGame
	for _, g := range all {
		if g.searchedMs > 0 && now.Sub(time.UnixMilli(g.searchedMs)) < sportsSearchGap(now.Sub(g.ev.At)) {
			continue
		}
		if len(s.missingParts(ctx, g)) == 0 {
			continue
		}
		out = append(out, g)
	}
	return out
}

// huntSports is Sports' part of the automatic search.
func (s *Server) huntSports(ctx context.Context) {
	if !s.sportsDownloadsOn() || !s.sportsAutoSearch() {
		return
	}
	mu, _ := sportsHunting.LoadOrStore(s, &sync.Mutex{})
	if !mu.(*sync.Mutex).TryLock() {
		return
	}
	defer mu.(*sync.Mutex).Unlock()
	due := s.dueSportGames(ctx, time.Now())
	for i, g := range due {
		if ctx.Err() != nil || i >= maxSportsSearchesPerHunt {
			return
		}
		if !s.autoGrabRoom() {
			holdBack("sports")
			return
		}
		sctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		n, _ := s.searchSportGame(sctx, g.id, queue.PriorityAutomatic)
		cancel()
		if n > 0 {
			slog.Info("sports: grabbed", "game", g.label(), "downloads", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(1500 * time.Millisecond):
		}
	}
}

// ---- the page's requests ----

func (s *Server) registerSportsGetRoutes(member, manage routeGroup) {
	member.HandleFunc("GET /api/home/sports/wanted", s.handleSportsWanted)
	manage.HandleFunc("PUT /api/home/sports/leagues/{id}/get", s.handleSportsLeagueGet)
	manage.HandleFunc("PUT /api/home/sports/teams/{id}/get", s.handleSportsTeamGet)
	manage.HandleFunc("PUT /api/home/sports/events/{id}/wanted", s.handleSportsEventWanted)
	manage.HandleFunc("POST /api/home/sports/events/{id}/search", s.handleSportsSearchNow)
	manage.HandleFunc("GET /api/home/sports/events/{id}/releases", s.handleSportsReleases)
	manage.HandleFunc("POST /api/home/sports/events/{id}/grab", s.handleSportsGrab)
}

// ownSportsOnly answers that a request needs Usherr's own engine.
func (s *Server) ownSportsOnly(w http.ResponseWriter) bool {
	if s.sportsMode() != "usherr" {
		writeError(w, http.StatusPreconditionFailed, "Getting games this way is part of Usherr's own sports engine (Settings > Extras > Sports).")
		return false
	}
	return true
}

// validParts keeps the choices Usherr knows.
func validParts(list []string) string {
	known := map[string]bool{"early": true, "prelims": true, "main": true, "race": true, "qualifying": true, "sprint": true, "practice": true}
	var out []string
	for _, p := range list {
		p = strings.ToLower(strings.TrimSpace(p))
		if known[p] && !strings.Contains(","+strings.Join(out, ",")+",", ","+p+",") {
			out = append(out, p)
		}
	}
	return strings.Join(out, ",")
}

// profileExists says a profile id is 0 (the default) or a stored profile.
func (s *Server) profileExists(id int64) bool {
	if id == 0 {
		return true
	}
	_, err := s.QualityRepo.Get(id)
	return err == nil
}

// PUT /api/home/sports/leagues/{id}/get {monitored, parts, teamsOnly, profileId}:
// what a league gets. Its coming games follow at once.
func (s *Server) handleSportsLeagueGet(w http.ResponseWriter, r *http.Request) {
	if !s.ownSportsOnly(w) {
		return
	}
	id, ok := pathID(r)
	var req struct {
		Monitored *bool    `json:"monitored"`
		Parts     []string `json:"parts"`
		TeamsOnly *bool    `json:"teamsOnly"`
		ProfileID *int64   `json:"profileId"`
	}
	if err := decodeJSON(r, &req); err != nil || !ok {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	if req.ProfileID != nil && !s.profileExists(*req.ProfileID) {
		writeError(w, http.StatusBadRequest, "That quality profile doesn't exist.")
		return
	}
	if (req.Monitored != nil && *req.Monitored) && s.sportsRoot() == "" {
		writeError(w, http.StatusPreconditionFailed, "Choose the Sports folder in Settings > Extras > Sports first.")
		return
	}
	ctx := r.Context()
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sports_leagues WHERE id = ?`, id).Scan(&n); err != nil || n == 0 {
		sportsFail(w, errSportNotFound)
		return
	}
	if req.Monitored != nil {
		_, _ = s.db.ExecContext(ctx, `UPDATE sports_leagues SET monitored = ? WHERE id = ?`, *req.Monitored, id)
	}
	if req.Parts != nil {
		_, _ = s.db.ExecContext(ctx, `UPDATE sports_leagues SET parts = ? WHERE id = ?`, validParts(req.Parts), id)
	}
	if req.TeamsOnly != nil {
		_, _ = s.db.ExecContext(ctx, `UPDATE sports_leagues SET teams_only = ? WHERE id = ?`, *req.TeamsOnly, id)
	}
	if req.ProfileID != nil {
		_, _ = s.db.ExecContext(ctx, `UPDATE sports_leagues SET profile_id = ? WHERE id = ?`, *req.ProfileID, id)
	}
	s.applySportsRules(ctx, id)
	writeJSON(w, http.StatusOK, map[string]string{"message": "Saved."})
}

// PUT /api/home/sports/teams/{id}/get {monitored, profileId}: whether a
// followed team's games are got (in the leagues in Sports).
func (s *Server) handleSportsTeamGet(w http.ResponseWriter, r *http.Request) {
	if !s.ownSportsOnly(w) {
		return
	}
	id, ok := pathID(r)
	var req struct {
		Monitored *bool  `json:"monitored"`
		ProfileID *int64 `json:"profileId"`
	}
	if err := decodeJSON(r, &req); err != nil || !ok {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	if req.ProfileID != nil && !s.profileExists(*req.ProfileID) {
		writeError(w, http.StatusBadRequest, "That quality profile doesn't exist.")
		return
	}
	ctx := r.Context()
	if req.Monitored != nil {
		if res, err := s.db.ExecContext(ctx, `UPDATE sports_teams SET monitored = ? WHERE id = ?`, *req.Monitored, id); err != nil {
			sportsFail(w, err)
			return
		} else if n, _ := res.RowsAffected(); n == 0 {
			sportsFail(w, errSportNotFound)
			return
		}
	}
	if req.ProfileID != nil {
		_, _ = s.db.ExecContext(ctx, `UPDATE sports_teams SET profile_id = ? WHERE id = ?`, *req.ProfileID, id)
	}
	s.applyAllSportsRules(ctx)
	writeJSON(w, http.StatusOK, map[string]string{"message": "Saved."})
}

// PUT /api/home/sports/events/{id}/wanted {wanted}: get one game, or not
// (the league's rules leave it as chosen).
func (s *Server) handleSportsEventWanted(w http.ResponseWriter, r *http.Request) {
	if !s.ownSportsOnly(w) {
		return
	}
	id, ok := pathID(r)
	var req struct {
		Wanted bool `json:"wanted"`
	}
	if err := decodeJSON(r, &req); err != nil || !ok {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	res, err := s.db.ExecContext(r.Context(), `UPDATE sports_events SET monitored = ?, by_hand = 1 WHERE id = ?`, req.Wanted, id)
	if err != nil {
		sportsFail(w, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		sportsFail(w, errSportNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/home/sports/events/{id}/search: look for a game now and get
// the best release.
func (s *Server) handleSportsSearchNow(w http.ResponseWriter, r *http.Request) {
	if !s.ownSportsOnly(w) {
		return
	}
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	if s.sportsRoot() == "" {
		writeError(w, http.StatusPreconditionFailed, "Choose the Sports folder in Settings > Extras > Sports first.")
		return
	}
	// Searching for it means wanting it.
	_, _ = s.db.ExecContext(r.Context(), `UPDATE sports_events SET monitored = 1, by_hand = 1 WHERE id = ?`, id)
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 2*time.Minute)
	defer cancel()
	n, msg := s.searchSportGame(ctx, id, queue.PriorityManual)
	writeJSON(w, http.StatusOK, map[string]any{"grabbed": n, "message": msg})
}

// GET /api/home/sports/events/{id}/releases?part=&q=: every release the
// indexers answer for a game, with whether it is the game and whether the
// profile takes it, best first (Pick a release).
func (s *Server) handleSportsReleases(w http.ResponseWriter, r *http.Request) {
	if !s.ownSportsOnly(w) {
		return
	}
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	g, err := s.sportGame(r.Context(), id)
	if err != nil {
		sportsFail(w, err)
		return
	}
	part := r.URL.Query().Get("part")
	if part != "" && sports.PartNumber(part) == 0 {
		writeError(w, http.StatusBadRequest, "That part isn't one Usherr knows.")
		return
	}
	if part == "" && g.ev.Kind() == sports.KindFight {
		part = sports.PartMainCard
	}
	q := r.URL.Query().Get("q")
	if len(q) > 200 {
		writeError(w, http.StatusBadRequest, "That search is too long.")
		return
	}
	ctx, cancel := context.WithTimeout(indexers.AsManual(r.Context()), 2*time.Minute)
	defer cancel()
	list, err := s.sportCandidates(ctx, g, part, q, false)
	if err != nil {
		writeError(w, http.StatusBadGateway, plainerror.Message(err))
		return
	}
	type payload struct {
		searchResultPayload
		Matches  bool   `json:"matches"`
		Accepted bool   `json:"accepted"`
		Score    int    `json:"score"`
		Quality  string `json:"quality"`
		Part     string `json:"part,omitempty"`
	}
	out := make([]payload, 0, len(list))
	for _, c := range list {
		p := payload{searchResultPayload: s.toSearchResultPayload(c.Result), Matches: c.Verdict.Match, Accepted: c.Accepted, Score: c.Verdict.Score, Quality: string(c.Tier), Part: part}
		p.Blocklisted = c.Blocked
		p.Rejections = c.Reasons
		out = append(out, p)
	}
	queries := sports.Queries(g.ev, part)
	if strings.TrimSpace(q) != "" {
		queries = []string{strings.TrimSpace(q)}
	}
	writeJSON(w, http.StatusOK, map[string]any{"releases": out, "searched": queries, "parts": g.wantedParts()})
}

// POST /api/home/sports/events/{id}/grab {title, downloadUrl, sizeBytes,
// protocol, part}: download a release picked by hand.
func (s *Server) handleSportsGrab(w http.ResponseWriter, r *http.Request) {
	if !s.ownSportsOnly(w) {
		return
	}
	id, ok := pathID(r)
	var req struct {
		Title       string `json:"title"`
		DownloadURL string `json:"downloadUrl"`
		SizeBytes   int64  `json:"sizeBytes"`
		Protocol    string `json:"protocol"`
		Part        string `json:"part"`
	}
	if err := decodeJSON(r, &req); err != nil || !ok || req.Title == "" || req.DownloadURL == "" {
		writeError(w, http.StatusBadRequest, "Choose a release to download.")
		return
	}
	if req.Part != "" && sports.PartNumber(req.Part) == 0 {
		writeError(w, http.StatusBadRequest, "That part isn't one Usherr knows.")
		return
	}
	if !s.checkGrabURL(w, r, &req.DownloadURL) {
		return
	}
	g, err := s.sportGame(r.Context(), id)
	if err != nil {
		sportsFail(w, err)
		return
	}
	if req.Part == "" && g.ev.Kind() == sports.KindFight {
		req.Part = sports.PartMainCard
	}
	protocol := indexers.Protocol(req.Protocol)
	if protocol == "" {
		protocol = indexers.ProtocolUsenet
	}
	qid, err := s.grabSportGame(g, req.Part, req.Title, req.DownloadURL, req.SizeBytes, protocol, queue.PriorityManual)
	if err != nil {
		writeGrabError(w, err, http.StatusConflict)
		return
	}
	_, _ = s.db.ExecContext(r.Context(), `UPDATE sports_events SET monitored = 1, by_hand = 1 WHERE id = ?`, id)
	writeJSON(w, http.StatusOK, map[string]any{"queueId": qid})
}

// sportWanted is a played game that is wanted and lacks a file.
type sportWanted struct {
	sportEvent
	Missing   []string `json:"missing"`            // the parts with no file ("" = the game)
	Searching bool     `json:"searching"`          // the automatic search still looks for it
	Searched  string   `json:"searched,omitempty"` // when it was last looked for
}

// GET /api/home/sports/wanted: the played games that are wanted and have
// no file yet, newest first.
func (s *Server) handleSportsWanted(w http.ResponseWriter, r *http.Request) {
	out := struct {
		On     bool          `json:"on"`
		CanGet bool          `json:"canGet"`
		Games  []sportWanted `json:"games"`
	}{Games: []sportWanted{}}
	if s.sportsMode() != "usherr" {
		writeJSON(w, http.StatusOK, out)
		return
	}
	out.On, out.CanGet = s.sportsRoot() != "", s.userAllows(r, auth.PermManage) && s.sportsRoot() != ""
	ctx := r.Context()
	now := time.Now()
	rows, err := s.db.QueryContext(ctx, `SELECT `+sportGameCols+` FROM sports_events e JOIN sports_leagues l ON l.id = e.league_id
		WHERE e.monitored = 1 AND e.at_ms > 0 AND e.at_ms <= ? ORDER BY e.at_ms DESC LIMIT 300`, now.Add(-sportsAfterEnd).UnixMilli())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var games []sportGame
	for rows.Next() {
		if g, err := scanSportGame(rows.Scan); err == nil {
			games = append(games, g)
		}
	}
	rows.Close()
	window := now.AddDate(0, 0, -s.sportsSearchDays())
	o := &ownSports{s: s}
	for _, g := range games {
		missing := s.missingParts(ctx, g)
		if len(missing) == 0 {
			continue
		}
		ev, err := o.game(ctx, strconv.FormatInt(g.id, 10))
		if err != nil {
			continue
		}
		wnt := sportWanted{sportEvent: ev, Missing: missing, Searching: s.sportsAutoSearch() && g.ev.At.After(window)}
		if g.searchedMs > 0 {
			wnt.Searched = time.UnixMilli(g.searchedMs).UTC().Format(time.RFC3339)
		}
		out.Games = append(out.Games, wnt)
	}
	writeJSON(w, http.StatusOK, out)
}

// sportCalendarEntries are the wanted games on the calendar (Usherr's own
// engine): the last 30 days and the coming 120, like episodes.
func (s *Server) sportCalendarEntries() []calendarEntryPayload {
	if s.sportsMode() != "usherr" {
		return nil
	}
	now := time.Now()
	rows, err := s.db.Query(`SELECT `+sportGameCols+` FROM sports_events e JOIN sports_leagues l ON l.id = e.league_id
		WHERE e.monitored = 1 AND e.at_ms >= ? AND e.at_ms <= ? ORDER BY e.at_ms LIMIT 1000`,
		now.Add(-calendarEpisodeLookback).UnixMilli(), now.Add(calendarEpisodeLookahead).UnixMilli())
	if err != nil {
		return nil
	}
	var games []sportGame
	for rows.Next() {
		if g, err := scanSportGame(rows.Scan); err == nil {
			games = append(games, g)
		}
	}
	rows.Close()
	ctx := context.Background()
	out := make([]calendarEntryPayload, 0, len(games))
	for _, g := range games {
		status := "missing"
		if missing := s.missingParts(ctx, g); len(missing) == 0 {
			status = "downloaded"
			for _, p := range g.wantedParts() {
				if s.sportPartBusy(ctx, g.id, p) {
					status = "downloading"
				}
			}
		}
		img := ""
		if g.leagueBadge != "" {
			img = "/api/home/sports/image?u=" + url.QueryEscape(g.leagueBadge)
		}
		out = append(out, calendarEntryPayload{Kind: "sport", ID: g.id, SportLeagueID: g.leagueID, Title: g.ev.Title, Subtitle: g.ev.League.Name,
			ReleaseDate: g.ev.LocalDay().Format("2006-01-02"), AirAt: g.ev.At.UTC().Format(time.RFC3339), Status: status, PosterURL: img})
	}
	return out
}
