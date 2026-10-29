package api

// Usherr: TheSportsDB (thesportsdb.com), the sports data behind Usherr's own
// sports engine. A paid key uses its v2 API (the key in the X-API-KEY
// header, full schedules, 100 calls a minute); without one, the free v1 API
// with its public test key "123": search and a part of each season, at a
// slow pace.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const keySportsTSDBKey = "sports.tsdb_key" // encrypted; a paid TheSportsDB key. Unset = the free test key

var tsdbBase = "https://www.thesportsdb.com/api" // a test points it elsewhere

var errTSDBKey = errors.New("TheSportsDB didn't accept the key. Copy it again from your profile on thesportsdb.com.")

type tsdbClient struct {
	key  string // "" = the free v1 key
	http *http.Client
}

// tsdbPace keeps calls apart: TheSportsDB blocks bursts (the free key after
// about a dozen quick calls).
var tsdbPace struct {
	sync.Mutex
	last time.Time
}

func (s *Server) tsdb() *tsdbClient {
	k, _ := s.Settings.Get(keySportsTSDBKey)
	return &tsdbClient{key: strings.TrimSpace(k), http: &http.Client{Timeout: 30 * time.Second}}
}

func (c *tsdbClient) paid() bool { return c.key != "" && c.key != "123" }

// get calls v2 (paid) or v1 (free) and reads the JSON answer into out.
func (c *tsdbClient) get(ctx context.Context, v2, v1 string, out any) error {
	gap := 2500 * time.Millisecond
	u := tsdbBase + "/v1/json/123/" + v1
	if c.paid() {
		gap, u = 650*time.Millisecond, tsdbBase+"/v2/json/"+v2
	} else if v1 == "" {
		return errors.New("This needs a TheSportsDB key (Settings > Extras > Sports).")
	}
	tsdbPace.Lock()
	if wait := gap - time.Since(tsdbPace.last); wait > 0 {
		select {
		case <-ctx.Done():
			tsdbPace.Unlock()
			return ctx.Err()
		case <-time.After(wait):
		}
	}
	tsdbPace.last = time.Now()
	tsdbPace.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	if c.paid() {
		req.Header.Set("X-API-KEY", c.key)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return errors.New("TheSportsDB didn't answer. Try again in a minute.")
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return errTSDBKey
	case resp.StatusCode == http.StatusTooManyRequests:
		return errors.New("TheSportsDB asked to slow down. Try again in a minute.")
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("TheSportsDB answered %d.", resp.StatusCode)
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return errors.New("TheSportsDB's answer couldn't be read.")
	}
	return nil
}

// tsdbRow is one item of an answer; fields come as strings, numbers or null.
type tsdbRow map[string]any

func (r tsdbRow) str(keys ...string) string {
	for _, k := range keys {
		switch v := r[k].(type) {
		case string:
			if v = strings.TrimSpace(v); v != "" {
				return v
			}
		case float64:
			return strconv.FormatFloat(v, 'f', -1, 64)
		}
	}
	return ""
}

func (r tsdbRow) num(key string) *int {
	if n, err := strconv.Atoi(r.str(key)); err == nil {
		return &n
	}
	return nil
}

// tsdbList reads whichever list an answer holds ("search", "lookup",
// "schedule", "list" in v2; "leagues", "teams", "events", "seasons" in v1).
func tsdbList(m map[string][]tsdbRow) []tsdbRow {
	for _, k := range []string{"search", "lookup", "schedule", "list", "livescore", "leagues", "teams", "events", "seasons", "countries"} {
		if v, ok := m[k]; ok {
			return v
		}
	}
	return nil
}

func (c *tsdbClient) rows(ctx context.Context, v2, v1 string) ([]tsdbRow, error) {
	var m map[string][]tsdbRow
	if err := c.get(ctx, v2, v1, &m); err != nil {
		return nil, err
	}
	return tsdbList(m), nil
}

// slug is how v2 takes a name in its path: english_premier_league.
func tsdbSlug(q string) string {
	return url.PathEscape(strings.ReplaceAll(strings.ToLower(strings.TrimSpace(q)), " ", "_"))
}

func tsdbLeague(r tsdbRow) sportLeague {
	return sportLeague{ExternalID: r.str("idLeague"), Name: r.str("strLeague"), Sport: r.str("strSport"), Country: r.str("strCountry"),
		Description: r.str("strDescriptionEN"), Badge: r.str("strBadge", "strLogo"), Poster: r.str("strPoster"), Banner: r.str("strBanner")}
}

func (c *tsdbClient) searchLeagues(ctx context.Context, q string) ([]sportLeague, error) {
	if c.paid() {
		rows, err := c.rows(ctx, "search/league/"+tsdbSlug(q), "")
		if err != nil {
			return nil, err
		}
		out := make([]sportLeague, 0, len(rows))
		for _, r := range rows {
			if l := tsdbLeague(r); l.ExternalID != "" {
				out = append(out, l)
			}
		}
		return out, nil
	}
	// v1 has no league search: every league, matched here.
	rows, err := c.rows(ctx, "", "all_leagues.php")
	if err != nil {
		return nil, err
	}
	needle := normWords(q)
	var out []sportLeague
	for _, r := range rows {
		if strings.Contains(normWords(r.str("strLeague")+" "+r.str("strLeagueAlternate")), needle) {
			out = append(out, tsdbLeague(r))
		}
	}
	return out, nil
}

func (c *tsdbClient) searchTeams(ctx context.Context, q string) ([]sportTeam, error) {
	rows, err := c.rows(ctx, "search/team/"+tsdbSlug(q), "searchteams.php?t="+url.QueryEscape(q))
	if err != nil {
		return nil, err
	}
	out := make([]sportTeam, 0, len(rows))
	for _, r := range rows {
		if id := r.str("idTeam"); id != "" {
			out = append(out, sportTeam{ExternalID: id, Name: r.str("strTeam"), Sport: r.str("strSport"), Country: r.str("strCountry"), Badge: r.str("strBadge", "strTeamBadge")})
		}
	}
	return out, nil
}

// lookupLeague is a league's details, with its other names and its current
// season.
func (c *tsdbClient) lookupLeague(ctx context.Context, id string) (sportLeague, string, string, error) {
	rows, err := c.rows(ctx, "lookup/league/"+url.PathEscape(id), "lookupleague.php?id="+url.QueryEscape(id))
	if err != nil {
		return sportLeague{}, "", "", err
	}
	if len(rows) == 0 {
		return sportLeague{}, "", "", errSportNotFound
	}
	return tsdbLeague(rows[0]), rows[0].str("strLeagueAlternate"), rows[0].str("strCurrentSeason"), nil
}

// seasons lists a league's seasons, newest first.
func (c *tsdbClient) seasons(ctx context.Context, id string) ([]string, error) {
	rows, err := c.rows(ctx, "list/seasons/"+url.PathEscape(id), "search_all_seasons.php?id="+url.QueryEscape(id))
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		if s := r.str("strSeason"); s != "" {
			out = append(out, s)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(out))) // newest first ("2026-2027" sorts like "2026")
	return out, nil
}

// tsdbGame is a game as TheSportsDB has it.
type tsdbGame struct {
	ExternalID, Title, Sport, Home, Away, Season, Round, Venue, Image, Status string
	At                                                                        time.Time
	HomeScore, AwayScore                                                      *int
}

func tsdbGameOf(r tsdbRow) tsdbGame {
	g := tsdbGame{ExternalID: r.str("idEvent"), Title: r.str("strEvent"), Sport: r.str("strSport"), Home: r.str("strHomeTeam"), Away: r.str("strAwayTeam"),
		Season: r.str("strSeason"), Round: r.str("intRound"), Venue: r.str("strVenue"), Image: r.str("strThumb", "strPoster"), Status: r.str("strStatus"),
		HomeScore: r.num("intHomeScore"), AwayScore: r.num("intAwayScore")}
	g.At = tsdbTime(r)
	return g
}

// tsdbTime reads a game's start: strTimestamp (UTC), else its date and time.
func tsdbTime(r tsdbRow) time.Time {
	if ts := r.str("strTimestamp"); ts != "" {
		for _, f := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05"} {
			if t, err := time.Parse(f, strings.TrimSuffix(ts, "+00:00")); err == nil {
				return t.UTC()
			}
		}
	}
	d := r.str("dateEvent")
	if d == "" {
		return time.Time{}
	}
	if tm := r.str("strTime"); tm != "" && tm != "00:00:00" {
		if t, err := time.Parse("2006-01-02 15:04:05", d+" "+strings.TrimSuffix(tm, "+00:00")); err == nil {
			return t.UTC()
		}
	}
	t, _ := time.Parse("2006-01-02", d)
	return t.UTC()
}

// schedule is a league's games in a season.
func (c *tsdbClient) schedule(ctx context.Context, id, season string) ([]tsdbGame, error) {
	rows, err := c.rows(ctx, "schedule/league/"+url.PathEscape(id)+"/"+url.PathEscape(season), "eventsseason.php?id="+url.QueryEscape(id)+"&s="+url.QueryEscape(season))
	if err != nil {
		return nil, err
	}
	out := make([]tsdbGame, 0, len(rows))
	for _, r := range rows {
		if g := tsdbGameOf(r); g.ExternalID != "" {
			if g.Season == "" {
				g.Season = season
			}
			out = append(out, g)
		}
	}
	return out, nil
}
