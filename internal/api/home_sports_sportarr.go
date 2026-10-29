package api

// Usherr: Sports, stacked on the household's Sportarr (github.com/Sportarr/
// Sportarr, GPL-3.0). Usherr talks to it through its own API, signed in with
// its API key, like it talks to Jellyfin: Sportarr keeps the leagues, finds
// and downloads the games; Usherr shows them and lets people ask for them.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var sportarrClient = &http.Client{Timeout: 3 * time.Minute} // a game's release search can take a minute or more; the callers set shorter limits

type sportarrBackend struct {
	base, key string
}

func (b *sportarrBackend) name() string { return "Sportarr" }

func (b *sportarrBackend) canDownload() bool { return true }

// call sends one request to Sportarr and reads its JSON answer into out.
func (b *sportarrBackend) call(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, b.base+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("X-Api-Key", b.key)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := sportarrClient.Do(req)
	if err != nil {
		return fmt.Errorf("Sportarr didn't answer at %s. Check its address.", b.base)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return errors.New("Sportarr didn't accept the API key. Copy it from Sportarr's Settings > General.")
	case resp.StatusCode >= 300:
		var e struct {
			Error  string `json:"error"`
			Detail string `json:"detail"`
		}
		if json.Unmarshal(raw, &e) == nil && (e.Error != "" || e.Detail != "") {
			return errors.New("Sportarr: " + strings.TrimSpace(e.Error+" "+e.Detail))
		}
		return fmt.Errorf("Sportarr answered %d.", resp.StatusCode)
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("Sportarr's answer couldn't be read (%s %s).", method, strings.SplitN(path, "?", 2)[0])
	}
	return nil
}

func (b *sportarrBackend) check(ctx context.Context) error {
	var st struct {
		AppName string `json:"appName"`
		Version string `json:"version"`
	}
	if err := b.call(ctx, http.MethodGet, "/api/system/status", nil, &st); err != nil {
		return err
	}
	if !strings.EqualFold(st.AppName, "Sportarr") {
		return errors.New("Something other than Sportarr answered at " + b.base + ".")
	}
	return nil
}

// The search answers carry TheSportsDB's field names.
type sportarrFoundLeague struct {
	ID          string `json:"idLeague"`
	Name        string `json:"strLeague"`
	Sport       string `json:"strSport"`
	Country     string `json:"strCountry"`
	Description string `json:"strDescriptionEN"`
	Badge       string `json:"strBadge"`
	Logo        string `json:"strLogo"`
	Poster      string `json:"strPoster"`
	Banner      string `json:"strBanner"`
}

func (b *sportarrBackend) searchLeagues(ctx context.Context, q string) ([]sportLeague, error) {
	var found []sportarrFoundLeague
	if err := b.call(ctx, http.MethodGet, "/api/leagues/search/"+url.PathEscape(q), nil, &found); err != nil {
		return nil, err
	}
	out := make([]sportLeague, 0, len(found))
	for _, l := range found {
		out = append(out, sportLeague{ExternalID: l.ID, Name: l.Name, Sport: l.Sport, Country: l.Country, Description: l.Description,
			Badge: firstNonEmpty(l.Badge, l.Logo), Poster: l.Poster, Banner: l.Banner})
	}
	return out, nil
}

func (b *sportarrBackend) searchTeams(ctx context.Context, q string) ([]sportTeam, error) {
	var found []struct {
		ID      string `json:"idTeam"`
		Name    string `json:"strTeam"`
		Sport   string `json:"strSport"`
		Country string `json:"strCountry"`
		League  string `json:"league"`
		Badge   string `json:"strTeamBadge"`
	}
	if err := b.call(ctx, http.MethodGet, "/api/teams/search/"+url.PathEscape(q), nil, &found); err != nil {
		return nil, err
	}
	out := make([]sportTeam, 0, len(found))
	for _, t := range found {
		out = append(out, sportTeam{ExternalID: t.ID, Name: t.Name, Sport: t.Sport, Country: t.Country, Badge: t.Badge})
	}
	return out, nil
}

type sportarrLeague struct {
	ID          int    `json:"id"`
	ExternalID  string `json:"externalId"`
	Name        string `json:"name"`
	Sport       string `json:"sport"`
	Country     string `json:"country"`
	Description string `json:"description"`
	Monitored   bool   `json:"monitored"`
	Logo        string `json:"logoUrl"`
	Poster      string `json:"posterUrl"`
	Banner      string `json:"bannerUrl"`
	Events      int    `json:"eventCount"`
	Files       int    `json:"fileCount"`
	Missing     int    `json:"missingCount"`
}

func (b *sportarrBackend) leagues(ctx context.Context) ([]sportLeague, error) {
	var list []sportarrLeague
	if err := b.call(ctx, http.MethodGet, "/api/leagues", nil, &list); err != nil {
		return nil, err
	}
	out := make([]sportLeague, 0, len(list))
	for _, l := range list {
		out = append(out, sportLeague{ID: strconv.Itoa(l.ID), ExternalID: l.ExternalID, Name: l.Name, Sport: l.Sport, Country: l.Country,
			Description: l.Description, Badge: l.Logo, Poster: l.Poster, Banner: l.Banner, InLibrary: true, Monitored: l.Monitored, Events: l.Events, Files: l.Files, Missing: l.Missing})
	}
	return out, nil
}

func (b *sportarrBackend) followedTeams(ctx context.Context) ([]sportTeam, error) {
	var list []struct {
		ID         int    `json:"id"`
		ExternalID string `json:"externalId"`
		Name       string `json:"name"`
		Sport      string `json:"sport"`
		Badge      string `json:"badgeUrl"`
	}
	if err := b.call(ctx, http.MethodGet, "/api/followed-teams", nil, &list); err != nil {
		return nil, err
	}
	out := make([]sportTeam, 0, len(list))
	for _, t := range list {
		out = append(out, sportTeam{ID: strconv.Itoa(t.ID), ExternalID: t.ExternalID, Name: t.Name, Sport: t.Sport, Badge: t.Badge, Followed: true})
	}
	return out, nil
}

type sportarrEvent struct {
	ID         int        `json:"id"`
	ExternalID string     `json:"externalId"`
	Title      string     `json:"title"`
	Sport      string     `json:"sport"`
	LeagueID   *int       `json:"leagueId"`
	League     string     `json:"leagueName"`
	LeagueLogo string     `json:"leagueLogoUrl"`
	Home       string     `json:"homeTeamName"`
	Away       string     `json:"awayTeamName"`
	Season     string     `json:"season"`
	Round      string     `json:"round"`
	When       *time.Time `json:"eventDate"`
	Venue      string     `json:"venue"`
	Broadcast  string     `json:"broadcast"`
	Monitored  bool       `json:"monitored"`
	HasFile    bool       `json:"hasFile"`
	Quality    string     `json:"quality"`
	Thumb      string     `json:"thumbUrl"`
	Images     []string   `json:"images"`
	Status     string     `json:"status"`
	HomeScore  flexInt    `json:"homeScore"`
	AwayScore  flexInt    `json:"awayScore"`
}

// flexInt reads a score Sportarr sends as a number or as a string.
type flexInt struct{ n *int }

func (f *flexInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
		f.n = &n
	}
	return nil
}

func (e sportarrEvent) event() sportEvent {
	out := sportEvent{ID: strconv.Itoa(e.ID), ExternalID: e.ExternalID, Title: e.Title, Sport: e.Sport, League: e.League, LeagueBadge: e.LeagueLogo,
		Home: e.Home, Away: e.Away, Season: e.Season, Round: e.Round, Venue: e.Venue, Broadcast: e.Broadcast, Monitored: e.Monitored,
		HasFile: e.HasFile, Quality: e.Quality, Status: e.Status, HomeScore: e.HomeScore.n, AwayScore: e.AwayScore.n, Image: e.Thumb}
	if e.LeagueID != nil {
		out.LeagueID = strconv.Itoa(*e.LeagueID)
	}
	if e.When != nil {
		out.When = e.When.UTC()
	}
	if out.Image == "" && len(e.Images) > 0 {
		out.Image = e.Images[0]
	}
	return out
}

func sportarrEvents(list []sportarrEvent) []sportEvent {
	out := make([]sportEvent, 0, len(list))
	for _, e := range list {
		out = append(out, e.event())
	}
	return out
}

// leagueSeasons lists a league's seasons, newest first.
func (b *sportarrBackend) leagueSeasons(ctx context.Context, leagueID string) ([]string, error) {
	id, err := strconv.Atoi(leagueID)
	if err != nil {
		return nil, errSportNotFound
	}
	var out struct {
		Seasons []struct {
			Season string `json:"season"`
		} `json:"seasons"`
	}
	if err := b.call(ctx, http.MethodGet, "/api/leagues/"+strconv.Itoa(id)+"/seasons", nil, &out); err != nil {
		return nil, err
	}
	list := make([]string, 0, len(out.Seasons))
	for _, s := range out.Seasons {
		list = append(list, s.Season)
	}
	return list, nil
}

// leagueEvents is one season of a league (all of a league's seasons can be
// tens of thousands of games).
func (b *sportarrBackend) leagueEvents(ctx context.Context, leagueID, season string) ([]sportEvent, error) {
	id, err := strconv.Atoi(leagueID)
	if err != nil {
		return nil, errSportNotFound
	}
	var list []sportarrEvent
	if err := b.call(ctx, http.MethodGet, "/api/leagues/"+strconv.Itoa(id)+"/events?season="+url.QueryEscape(season), nil, &list); err != nil {
		return nil, err
	}
	return sportarrEvents(list), nil
}

func (b *sportarrBackend) between(ctx context.Context, from, to time.Time) ([]sportEvent, error) {
	q := url.Values{"start": {from.UTC().Format(time.RFC3339)}, "end": {to.UTC().Format(time.RFC3339)}, "unmonitored": {"true"}}
	var list []sportarrEvent
	if err := b.call(ctx, http.MethodGet, "/api/calendar?"+q.Encode(), nil, &list); err != nil {
		return nil, err
	}
	return sportarrEvents(list), nil
}

// addLeague puts a league in Sportarr with its default quality profile and
// first root folder. monitor watches the league's future games: in a team
// sport Sportarr watches only the games of the teams picked, so all of them
// are.
func (b *sportarrBackend) addLeague(ctx context.Context, l sportLeague, monitor bool) error {
	var profiles []struct {
		ID        int  `json:"id"`
		IsDefault bool `json:"isDefault"`
	}
	if err := b.call(ctx, http.MethodGet, "/api/qualityprofile", nil, &profiles); err != nil {
		return err
	}
	var roots []struct {
		ID int `json:"id"`
	}
	if err := b.call(ctx, http.MethodGet, "/api/rootfolder", nil, &roots); err != nil {
		return err
	}
	if len(profiles) == 0 || len(roots) == 0 {
		return errors.New("Sportarr needs a quality profile and a root folder first (Sportarr's Settings > Media Management).")
	}
	profile := profiles[0].ID
	for _, p := range profiles {
		if p.IsDefault {
			profile = p.ID
			break
		}
	}
	body := map[string]any{
		"externalId": l.ExternalID, "name": l.Name, "sport": l.Sport, "country": l.Country, "description": l.Description,
		"monitored": monitor, "monitorType": "Future", "qualityProfileId": profile, "rootFolderId": roots[0].ID,
		"searchForMissingEvents": false, "searchForCutoffUnmetEvents": false, "tags": []int{},
	}
	if monitor {
		var teams []struct {
			ID string `json:"idTeam"`
		}
		if err := b.call(ctx, http.MethodGet, "/api/leagues/external/"+url.PathEscape(l.ExternalID)+"/teams", nil, &teams); err != nil {
			return err
		}
		ids := make([]string, 0, len(teams))
		for _, t := range teams {
			if t.ID != "" {
				ids = append(ids, t.ID)
			}
		}
		if len(ids) > 0 {
			body["monitoredTeamIds"] = ids
		}
	}
	return b.call(ctx, http.MethodPost, "/api/leagues", body, nil)
}

func (b *sportarrBackend) followTeam(ctx context.Context, t sportTeam) error {
	body := map[string]any{"externalId": t.ExternalID, "name": t.Name, "sport": t.Sport, "badgeUrl": t.Badge}
	return b.call(ctx, http.MethodPost, "/api/followed-teams", body, nil)
}

func (b *sportarrBackend) unfollowTeam(ctx context.Context, id string) error {
	n, err := strconv.Atoi(id)
	if err != nil {
		return errSportNotFound
	}
	return b.call(ctx, http.MethodDelete, "/api/followed-teams/"+strconv.Itoa(n), nil, nil)
}

// getEvent monitors one game and asks Sportarr to search for it now.
func (b *sportarrBackend) getEvent(ctx context.Context, id string) error {
	n, err := strconv.Atoi(id)
	if err != nil {
		return errSportNotFound
	}
	p := strconv.Itoa(n)
	if err := b.call(ctx, http.MethodPut, "/api/events/"+p, map[string]any{"monitored": true}, nil); err != nil {
		return err
	}
	return b.call(ctx, http.MethodPost, "/api/event/"+p+"/automatic-search", map[string]any{}, nil)
}

// releases asks Sportarr to search the indexers for a game, and answers the
// Usenet releases it matched to it (its sport-by-sport matching), best
// first. Releases it found but couldn't tie to the game are left out.
func (b *sportarrBackend) releases(ctx context.Context, eventID string) ([]sportRelease, error) {
	n, err := strconv.Atoi(eventID)
	if err != nil {
		return nil, errSportNotFound
	}
	var out struct {
		Results []struct {
			Title       string   `json:"title"`
			DownloadURL string   `json:"downloadUrl"`
			Indexer     string   `json:"indexer"`
			Protocol    string   `json:"protocol"`
			Size        int64    `json:"size"`
			Quality     string   `json:"quality"`
			Part        string   `json:"part"`
			Language    string   `json:"language"`
			MatchScore  int      `json:"matchScore"`
			Blocklisted bool     `json:"isBlocklisted"`
			Rejections  []string `json:"rejections"`
		} `json:"results"`
	}
	if err := b.call(ctx, http.MethodPost, "/api/event/"+strconv.Itoa(n)+"/search", map[string]any{}, &out); err != nil {
		return nil, err
	}
	// Sportarr counts a release as the game from a low score (15 of 100), so
	// a race's practice sessions and its shows come along: only those close
	// to the best match are kept.
	top := 0
	for _, r := range out.Results {
		if strings.EqualFold(r.Protocol, "usenet") && sportarrMatched(r.Rejections) && r.MatchScore > top {
			top = r.MatchScore
		}
	}
	floor := max(50, top-15)
	list := make([]sportRelease, 0, len(out.Results))
	for _, r := range out.Results {
		if !strings.EqualFold(r.Protocol, "usenet") || r.Blocklisted || r.DownloadURL == "" || !sportarrMatched(r.Rejections) || r.MatchScore < floor {
			continue
		}
		list = append(list, sportRelease{Title: r.Title, URL: r.DownloadURL, Indexer: r.Indexer, Size: r.Size, Quality: r.Quality, Part: r.Part, Language: r.Language})
	}
	return list, nil
}

// sportarrMatched: Sportarr marks a release that isn't the game with a
// rejection saying so; other rejections (its quality profile, size limits)
// don't matter for streaming.
func sportarrMatched(rejections []string) bool {
	for _, r := range rejections {
		if strings.Contains(r, "doesn't match event") {
			return false
		}
	}
	return true
}

// gameTitle is a game's title in Sportarr ("" when it can't be read).
func (b *sportarrBackend) gameTitle(ctx context.Context, id string) string {
	n, err := strconv.Atoi(id)
	if err != nil {
		return ""
	}
	var e struct {
		Title string `json:"title"`
	}
	if b.call(ctx, http.MethodGet, "/api/events/"+strconv.Itoa(n), nil, &e) != nil {
		return ""
	}
	return e.Title
}

// liveFeed is Sportarr's live and recent scores for a sport (from its
// metadata service): teams, scores and status, no clock.
func (b *sportarrBackend) liveFeed(ctx context.Context, sport string) ([]tsdbRow, error) {
	var rows []tsdbRow
	if err := b.call(ctx, http.MethodGet, "/api/events/livescore?sport="+url.QueryEscape(sport), nil, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// game is one game by its id.
func (b *sportarrBackend) game(ctx context.Context, id string) (sportEvent, error) {
	n, err := strconv.Atoi(id)
	if err != nil {
		return sportEvent{}, errSportNotFound
	}
	var e sportarrEvent
	if err := b.call(ctx, http.MethodGet, "/api/events/"+strconv.Itoa(n), nil, &e); err != nil {
		return sportEvent{}, err
	}
	return e.event(), nil
}
