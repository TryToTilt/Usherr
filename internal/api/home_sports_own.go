package api

// Usherr: Sports on Usherr's own engine, without Sportarr. Leagues, games
// and teams come from TheSportsDB (home_sports_tsdb.go) and are kept in
// Usherr's database; a job fetches the current season again every few hours.
// Games stream through Usherr's streaming engine: the indexers are searched
// for the league and the game's date or teams, and a release is taken only
// when it names the league and the game. Downloading games is Sportarr's
// part for now (the stacked way).

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/indexers"
)

type ownSports struct{ s *Server }

func (o *ownSports) name() string { return "Usherr" }

func (o *ownSports) check(ctx context.Context) error {
	c := o.s.tsdb()
	found, err := c.searchLeagues(ctx, "NFL")
	if err != nil {
		return err
	}
	if len(found) == 0 {
		return errors.New("TheSportsDB answered, but found nothing for a test search.")
	}
	return nil
}

func (o *ownSports) searchLeagues(ctx context.Context, q string) ([]sportLeague, error) {
	return o.s.tsdb().searchLeagues(ctx, q)
}

func (o *ownSports) searchTeams(ctx context.Context, q string) ([]sportTeam, error) {
	return o.s.tsdb().searchTeams(ctx, q)
}

func (o *ownSports) leagues(ctx context.Context) ([]sportLeague, error) {
	now := time.Now().UnixMilli()
	rows, err := o.s.db.QueryContext(ctx, `SELECT l.id, l.external_id, l.name, l.sport, l.country, l.description, l.badge, l.poster, l.banner, l.monitored,
		(SELECT COUNT(*) FROM sports_events e WHERE e.league_id = l.id),
		(SELECT COUNT(*) FROM sports_events e WHERE e.league_id = l.id AND e.monitored = 1 AND e.at_ms < ?)
		FROM sports_leagues l ORDER BY l.name`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []sportLeague
	for rows.Next() {
		var l sportLeague
		var id int64
		if err := rows.Scan(&id, &l.ExternalID, &l.Name, &l.Sport, &l.Country, &l.Description, &l.Badge, &l.Poster, &l.Banner, &l.Monitored, &l.Events, &l.Missing); err != nil {
			return nil, err
		}
		l.ID, l.InLibrary = strconv.FormatInt(id, 10), true
		out = append(out, l)
	}
	return out, rows.Err()
}

func (o *ownSports) followedTeams(ctx context.Context) ([]sportTeam, error) {
	rows, err := o.s.db.QueryContext(ctx, `SELECT id, external_id, name, sport, country, badge FROM sports_teams ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []sportTeam
	for rows.Next() {
		var t sportTeam
		var id int64
		if err := rows.Scan(&id, &t.ExternalID, &t.Name, &t.Sport, &t.Country, &t.Badge); err != nil {
			return nil, err
		}
		t.ID, t.Followed = strconv.FormatInt(id, 10), true
		out = append(out, t)
	}
	return out, rows.Err()
}

type ownLeagueRow struct {
	id                        int64
	externalID, name, current string
	seasons                   []string
	synced                    map[string]int64
}

func (o *ownSports) leagueRow(ctx context.Context, id string) (ownLeagueRow, error) {
	var r ownLeagueRow
	var seasons, synced string
	err := o.s.db.QueryRowContext(ctx, `SELECT id, external_id, name, current, seasons_json, synced_json FROM sports_leagues WHERE id = ?`, id).
		Scan(&r.id, &r.externalID, &r.name, &r.current, &seasons, &synced)
	if errors.Is(err, sql.ErrNoRows) {
		return r, errSportNotFound
	}
	if err != nil {
		return r, err
	}
	_ = json.Unmarshal([]byte(seasons), &r.seasons)
	_ = json.Unmarshal([]byte(synced), &r.synced)
	if r.synced == nil {
		r.synced = map[string]int64{}
	}
	return r, nil
}

func (o *ownSports) leagueSeasons(ctx context.Context, id string) ([]string, error) {
	l, err := o.leagueRow(ctx, id)
	if err != nil {
		return nil, err
	}
	if len(l.seasons) == 0 {
		if list, err := o.s.tsdb().seasons(ctx, l.externalID); err == nil && len(list) > 0 {
			l.seasons = list
			raw, _ := json.Marshal(list)
			_, _ = o.s.db.ExecContext(ctx, `UPDATE sports_leagues SET seasons_json = ? WHERE id = ?`, string(raw), l.id)
		}
	}
	if len(l.seasons) == 0 && l.current != "" {
		return []string{l.current}, nil
	}
	return l.seasons, nil
}

// syncSeason fetches a season's games and keeps them (scores and times
// change; whether a game is wanted stays).
func (o *ownSports) syncSeason(ctx context.Context, l ownLeagueRow, season string) error {
	games, err := o.s.tsdb().schedule(ctx, l.externalID, season)
	if err != nil {
		return err
	}
	tx, err := o.s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, g := range games {
		var at int64
		if !g.At.IsZero() {
			at = g.At.UnixMilli()
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO sports_events (league_id, external_id, title, sport, home, away, season, round, at_ms, venue, image, home_score, away_score, status)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(external_id) DO UPDATE SET title = excluded.title, home = excluded.home, away = excluded.away, round = excluded.round,
				at_ms = excluded.at_ms, venue = excluded.venue, image = CASE WHEN excluded.image != '' THEN excluded.image ELSE sports_events.image END,
				home_score = excluded.home_score, away_score = excluded.away_score, status = excluded.status`,
			l.id, g.ExternalID, g.Title, g.Sport, g.Home, g.Away, g.Season, g.Round, at, g.Venue, g.Image, g.HomeScore, g.AwayScore, g.Status); err != nil {
			return err
		}
	}
	l.synced[season] = time.Now().UnixMilli()
	raw, _ := json.Marshal(l.synced)
	if _, err := tx.ExecContext(ctx, `UPDATE sports_leagues SET synced_json = ? WHERE id = ?`, string(raw), l.id); err != nil {
		return err
	}
	return tx.Commit()
}

const ownEventCols = `e.id, e.external_id, e.title, e.sport, e.league_id, l.name, l.badge, e.home, e.away, e.season, e.round, e.at_ms, e.venue, e.image,
	e.monitored, e.status, e.home_score, e.away_score`

func scanOwnEvents(rows *sql.Rows) ([]sportEvent, error) {
	defer rows.Close()
	var out []sportEvent
	for rows.Next() {
		var e sportEvent
		var id, league, at int64
		var hs, as sql.NullInt64
		if err := rows.Scan(&id, &e.ExternalID, &e.Title, &e.Sport, &league, &e.League, &e.LeagueBadge, &e.Home, &e.Away, &e.Season, &e.Round, &at,
			&e.Venue, &e.Image, &e.Monitored, &e.Status, &hs, &as); err != nil {
			return nil, err
		}
		e.ID, e.LeagueID = strconv.FormatInt(id, 10), strconv.FormatInt(league, 10)
		if at > 0 {
			e.When = time.UnixMilli(at).UTC()
		}
		if hs.Valid {
			n := int(hs.Int64)
			e.HomeScore = &n
		}
		if as.Valid {
			n := int(as.Int64)
			e.AwayScore = &n
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (o *ownSports) leagueEvents(ctx context.Context, leagueID, season string) ([]sportEvent, error) {
	l, err := o.leagueRow(ctx, leagueID)
	if err != nil {
		return nil, err
	}
	if _, ok := l.synced[season]; !ok && season != "" {
		if err := o.syncSeason(ctx, l, season); err != nil {
			return nil, err
		}
	}
	rows, err := o.s.db.QueryContext(ctx, `SELECT `+ownEventCols+` FROM sports_events e JOIN sports_leagues l ON l.id = e.league_id
		WHERE e.league_id = ? AND e.season = ? ORDER BY e.at_ms`, l.id, season)
	if err != nil {
		return nil, err
	}
	return scanOwnEvents(rows)
}

func (o *ownSports) between(ctx context.Context, from, to time.Time) ([]sportEvent, error) {
	rows, err := o.s.db.QueryContext(ctx, `SELECT `+ownEventCols+` FROM sports_events e JOIN sports_leagues l ON l.id = e.league_id
		WHERE e.at_ms >= ? AND e.at_ms <= ? ORDER BY e.at_ms LIMIT 500`, from.UnixMilli(), to.UnixMilli())
	if err != nil {
		return nil, err
	}
	return scanOwnEvents(rows)
}

func (o *ownSports) addLeague(ctx context.Context, l sportLeague, monitor bool) error {
	c := o.s.tsdb()
	full, alternate, current, err := c.lookupLeague(ctx, l.ExternalID)
	if err != nil && !errors.Is(err, errSportNotFound) {
		return err
	}
	if full.ExternalID == "" {
		full = l
	}
	res, err := o.s.db.ExecContext(ctx, `INSERT INTO sports_leagues (external_id, name, alternate, sport, country, description, badge, poster, banner, current, monitored, added_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(external_id) DO NOTHING`,
		full.ExternalID, firstNonEmpty(full.Name, l.Name), alternate, firstNonEmpty(full.Sport, l.Sport), full.Country, full.Description,
		firstNonEmpty(full.Badge, l.Badge), full.Poster, full.Banner, current, monitor, time.Now().UnixMilli())
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New(l.Name + " is already in Sports.")
	}
	id, _ := res.LastInsertId()
	// The current season's games in the background: a paid key answers in a
	// second, the free one slowly.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		seasons, _ := o.leagueSeasons(ctx, strconv.FormatInt(id, 10))
		row, err := o.leagueRow(ctx, strconv.FormatInt(id, 10))
		if err != nil {
			return
		}
		season := firstNonEmpty(current, firstOf(seasons))
		if season != "" {
			_ = o.syncSeason(ctx, row, season)
		}
	}()
	return nil
}

func firstOf(list []string) string {
	if len(list) > 0 {
		return list[0]
	}
	return ""
}

func (o *ownSports) followTeam(ctx context.Context, t sportTeam) error {
	_, err := o.s.db.ExecContext(ctx, `INSERT INTO sports_teams (external_id, name, sport, country, badge, added_ms) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(external_id) DO NOTHING`, t.ExternalID, t.Name, t.Sport, t.Country, t.Badge, time.Now().UnixMilli())
	return err
}

func (o *ownSports) unfollowTeam(ctx context.Context, id string) error {
	_, err := o.s.db.ExecContext(ctx, `DELETE FROM sports_teams WHERE id = ?`, id)
	return err
}

func (o *ownSports) canDownload() bool { return false }

// game is one game by its id.
func (o *ownSports) game(ctx context.Context, id string) (sportEvent, error) {
	rows, err := o.s.db.QueryContext(ctx, `SELECT `+ownEventCols+` FROM sports_events e JOIN sports_leagues l ON l.id = e.league_id WHERE e.id = ?`, id)
	if err != nil {
		return sportEvent{}, err
	}
	list, err := scanOwnEvents(rows)
	if err != nil || len(list) == 0 {
		return sportEvent{}, errSportNotFound
	}
	return list[0], nil
}

func (o *ownSports) gameTitle(ctx context.Context, id string) string {
	var t string
	_ = o.s.db.QueryRowContext(ctx, `SELECT title FROM sports_events WHERE id = ?`, id).Scan(&t)
	return t
}

func (o *ownSports) getEvent(ctx context.Context, id string) error {
	return errors.New("Downloading games needs Sportarr (Settings > Extras > Sports). Stream works on its own.")
}

// syncCurrent fetches each league's current season again (the job).
func (o *ownSports) syncCurrent(ctx context.Context) {
	rows, err := o.s.db.QueryContext(ctx, `SELECT id FROM sports_leagues`)
	if err != nil {
		return
	}
	var ids []string
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, strconv.FormatInt(id, 10))
		}
	}
	rows.Close()
	for _, id := range ids {
		l, err := o.leagueRow(ctx, id)
		if err != nil {
			continue
		}
		season := firstNonEmpty(l.current, firstOf(l.seasons))
		if season != "" {
			_ = o.syncSeason(ctx, l, season)
		}
		if ctx.Err() != nil {
			return
		}
	}
}

// sportsSyncJob keeps the own engine's schedules and scores current.
func (s *Server) sportsSyncJob(ctx context.Context) {
	if s.sportsMode() != "usherr" {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()
	(&ownSports{s: s}).syncCurrent(ctx)
}

// ---- finding a game's releases on the indexers ----

var sportsCategories = []int{5000, 5060}

// releases searches Usherr's own indexers for a game: the league with the
// game's date, then with its teams (or its title, for a fight card or a
// race), and keeps what names the league and the game.
func (o *ownSports) releases(ctx context.Context, eventID string) ([]sportRelease, error) {
	var g ownGame
	var at int64
	var alternate string
	err := o.s.db.QueryRowContext(ctx, `SELECT e.title, e.home, e.away, e.at_ms, l.name, l.alternate FROM sports_events e JOIN sports_leagues l ON l.id = e.league_id WHERE e.id = ?`, eventID).
		Scan(&g.title, &g.home, &g.away, &at, &g.league, &alternate)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errSportNotFound
	}
	if err != nil {
		return nil, err
	}
	g.at = time.UnixMilli(at).UTC()
	g.aliases = leagueAliases(g.league, alternate)
	instances, err := o.s.usenetIndexers()
	if err != nil {
		return nil, err
	}
	var queries []string
	short := g.aliases[0]
	if !g.at.IsZero() {
		queries = append(queries, short+" "+g.at.Format("2006 01 02"))
	}
	if g.home != "" && g.away != "" {
		queries = append(queries, short+" "+lastWord(g.home)+" "+lastWord(g.away))
	} else {
		queries = append(queries, normWords(g.title))
	}
	seen := map[string]bool{}
	var out []sportRelease
	for _, q := range queries {
		for _, res := range indexers.SearchAll(ctx, instances, q, sportsCategories) {
			for _, r := range res.Results {
				if r.Torrent || r.Protocol == indexers.ProtocolTorrent || isMagnetURI(r.DownloadURL) || seen[r.Title] {
					continue
				}
				if r.SizeBytes > 0 && r.SizeBytes < 100<<20 {
					continue // a clip or a sample
				}
				if !g.matches(r.Title) || sideProgram(r.Title, g.title) {
					continue
				}
				seen[r.Title] = true
				out = append(out, sportRelease{Title: r.Title, URL: r.DownloadURL, Indexer: r.IndexerName, Size: r.SizeBytes})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return heightOf(out[i].Title) > heightOf(out[j].Title) })
	return out, nil
}

// ownGame is what a release title is checked against.
type ownGame struct {
	title, home, away, league string
	aliases                   []string // the league's names as release titles write them, shortest first
	at                        time.Time
}

// leagueAliases are a league's names in release titles: "Formula 1" is
// also "Formula1" and "F1"; the alternate names TheSportsDB has count too.
func leagueAliases(name, alternate string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(v string) {
		v = normWords(v)
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	add(name)
	add(strings.ReplaceAll(normWords(name), " ", ""))
	for _, a := range strings.Split(alternate, ",") {
		add(a)
	}
	if n := normWords(name); strings.HasPrefix(n, "formula ") {
		add("f" + strings.TrimPrefix(n, "formula "))
	}
	sort.SliceStable(out, func(i, j int) bool { return len(out[i]) < len(out[j]) })
	return out
}

func lastWord(s string) string {
	f := strings.Fields(normWords(s))
	if len(f) == 0 {
		return ""
	}
	return f[len(f)-1]
}

// sessionWords tell a race from its qualifying, a card from its prelims.
var sessionWords = []string{"qualifying", "sprint", "practice", "fp1", "fp2", "fp3", "prelims", "race"}

// matches says whether a release title is this game: the league named, and
// the game's date (a day either way, for time zones) or both its teams (or,
// with no teams, the distinctive words of its title).
func (g ownGame) matches(title string) bool {
	t := " " + normWords(title) + " "
	league := false
	for _, a := range g.aliases {
		if strings.Contains(t, " "+a+" ") {
			league = true
			break
		}
	}
	if !league {
		return false
	}
	// A session or part named in the game must be in the release too; one
	// the game doesn't name must not be (a race isn't its qualifying).
	gt := " " + normWords(g.title) + " "
	for _, w := range sessionWords {
		if strings.Contains(gt, " "+w+" ") != strings.Contains(t, " "+w+" ") && w != "race" {
			return false
		}
	}
	if !g.at.IsZero() {
		for _, d := range []time.Time{g.at, g.at.AddDate(0, 0, -1), g.at.AddDate(0, 0, 1)} {
			if strings.Contains(t, " "+d.Format("2006 01 02")+" ") || strings.Contains(t, " "+d.Format("02 01 2006")+" ") {
				return true
			}
		}
	}
	if g.home != "" && g.away != "" {
		h, a := lastWord(g.home), lastWord(g.away)
		return h != "" && a != "" && strings.Contains(t, " "+h+" ") && strings.Contains(t, " "+a+" ")
	}
	// No teams: a number in the title (UFC 330, Fight Night 288) must be
	// there, and the words of the title that aren't the league's or common.
	hits, want := 0, 0
	for _, w := range strings.Fields(normWords(g.title)) {
		if _, err := strconv.Atoi(w); err == nil && !strings.Contains(t, " "+w+" ") {
			return false
		}
		if len(w) < 3 || w == "vs" || w == "the" || w == "grand" || w == "prix" || strings.Contains(" "+strings.Join(g.aliases, " ")+" ", " "+w+" ") {
			continue
		}
		want++
		if strings.Contains(t, " "+w+" ") {
			hits++
		}
	}
	return want > 0 && hits >= min(2, want)
}
