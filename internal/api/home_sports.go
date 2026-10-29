package api

// Usherr: Sports. Leagues, teams and games, for everyone, on top of a sports
// backend: the household's Sportarr ("stacked", home_sports_sportarr.go), or
// Usherr's own sports engine on TheSportsDB (home_sports_own.go). Everyone can browse; adding a league, following
// a team and getting a game need the "manage" permission (requests from
// everyone come with the request portal).

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
)

const (
	keySportsMode        = "sports.mode"         // "" (off), "sportarr" or "usherr" (Usherr's own engine)
	keySportsSportarrURL = "sports.sportarr_url" // Sportarr's address, e.g. http://192.168.1.20:1867
	keySportsSportarrKey = "sports.sportarr_key" // encrypted; Sportarr's API key
)

var (
	errSportsOff     = errors.New("Sports isn't set up (Settings > Extras > Sports).")
	errSportNotFound = errors.New("That isn't in Sports.")
)

type sportLeague struct {
	ID          string `json:"id,omitempty"` // the backend's id, once in the library
	ExternalID  string `json:"externalId"`
	Name        string `json:"name"`
	Sport       string `json:"sport"`
	Country     string `json:"country,omitempty"`
	Description string `json:"description,omitempty"`
	Badge       string `json:"badge,omitempty"`
	Poster      string `json:"poster,omitempty"`
	Banner      string `json:"banner,omitempty"`
	InLibrary   bool   `json:"inLibrary"`
	Monitored   bool   `json:"monitored"`
	Events      int    `json:"events,omitempty"`
	Files       int    `json:"files,omitempty"`
	Missing     int    `json:"missing,omitempty"` // watched for, not downloaded yet
}

type sportTeam struct {
	ID         string `json:"id,omitempty"`
	ExternalID string `json:"externalId"`
	Name       string `json:"name"`
	Sport      string `json:"sport"`
	Country    string `json:"country,omitempty"`
	Badge      string `json:"badge,omitempty"`
	Followed   bool   `json:"followed"`
}

type sportEvent struct {
	ID          string    `json:"id"`
	ExternalID  string    `json:"externalId,omitempty"`
	Title       string    `json:"title"`
	Sport       string    `json:"sport"`
	LeagueID    string    `json:"leagueId,omitempty"`
	League      string    `json:"league,omitempty"`
	LeagueBadge string    `json:"leagueBadge,omitempty"`
	Home        string    `json:"home,omitempty"`
	Away        string    `json:"away,omitempty"`
	Season      string    `json:"season,omitempty"`
	Round       string    `json:"round,omitempty"`
	When        time.Time `json:"when"`
	Venue       string    `json:"venue,omitempty"`
	Broadcast   string    `json:"broadcast,omitempty"`
	Image       string    `json:"image,omitempty"`
	Monitored   bool      `json:"monitored"`
	HasFile     bool      `json:"hasFile"`
	Quality     string    `json:"quality,omitempty"`
	Status      string    `json:"status,omitempty"`
	HomeScore   *int      `json:"homeScore,omitempty"`
	AwayScore   *int      `json:"awayScore,omitempty"`
}

// sportsBackend is what Sports needs from whatever keeps the leagues.
type sportsBackend interface {
	name() string
	check(ctx context.Context) error
	searchLeagues(ctx context.Context, q string) ([]sportLeague, error)
	searchTeams(ctx context.Context, q string) ([]sportTeam, error)
	leagues(ctx context.Context) ([]sportLeague, error)
	followedTeams(ctx context.Context) ([]sportTeam, error)
	leagueSeasons(ctx context.Context, leagueID string) ([]string, error) // newest first
	leagueEvents(ctx context.Context, leagueID, season string) ([]sportEvent, error)
	between(ctx context.Context, from, to time.Time) ([]sportEvent, error)
	addLeague(ctx context.Context, l sportLeague, monitor bool) error
	followTeam(ctx context.Context, t sportTeam) error
	unfollowTeam(ctx context.Context, id string) error
	getEvent(ctx context.Context, id string) error
	releases(ctx context.Context, eventID string) ([]sportRelease, error) // for streaming, best first
	canDownload() bool                                                    // whether getEvent downloads
}

func (s *Server) sportsMode() string {
	v, _ := s.Settings.Get(keySportsMode)
	return strings.TrimSpace(v)
}

// sports is the backend set up in Settings, or errSportsOff.
func (s *Server) sports() (sportsBackend, error) {
	switch s.sportsMode() {
	case "sportarr":
		base, _ := s.Settings.Get(keySportsSportarrURL)
		key, _ := s.Settings.Get(keySportsSportarrKey)
		if strings.TrimSpace(base) == "" || strings.TrimSpace(key) == "" {
			return nil, errors.New("Enter Sportarr's address and API key (Settings > Extras > Sports).")
		}
		return &sportarrBackend{base: strings.TrimRight(strings.TrimSpace(base), "/"), key: strings.TrimSpace(key)}, nil
	case "usherr":
		return &ownSports{s: s}, nil
	}
	return nil, errSportsOff
}

func (s *Server) registerSportsRoutes(member, play, manage, admin routeGroup) {
	play.HandleFunc("POST /api/home/sports/events/{id}/stream", s.handleSportsStream)
	play.HandleFunc("POST /api/home/sports/events/{id}/streamed", s.handleSportsStreamed)
	member.HandleFunc("GET /api/home/sports", s.handleSportsHome)
	member.HandleFunc("GET /api/home/sports/on", s.handleSportsOn)
	member.HandleFunc("GET /api/home/sports/search", s.handleSportsSearch)
	member.HandleFunc("GET /api/home/sports/leagues/{id}", s.handleSportsLeague)
	member.HandleFunc("GET /api/home/sports/image", s.handleSportsImage)
	manage.HandleFunc("POST /api/home/sports/leagues", s.handleSportsAddLeague)
	manage.HandleFunc("POST /api/home/sports/teams", s.handleSportsFollowTeam)
	manage.HandleFunc("DELETE /api/home/sports/teams/{id}", s.handleSportsUnfollowTeam)
	manage.HandleFunc("POST /api/home/sports/events/{id}/get", s.handleSportsGetEvent)
	admin.HandleFunc("GET /api/home/sports/settings", s.handleGetSportsSettings)
	admin.HandleFunc("PUT /api/home/sports/settings", s.handlePutSportsSettings)
	admin.HandleFunc("POST /api/home/sports/test", s.handleTestSports)
}

func sportsFail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errSportsOff):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, errSportNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	default:
		writeError(w, http.StatusBadGateway, err.Error())
	}
}

// GET /api/home/sports: the Sports home. Whether it's on, the leagues in the
// library, the teams followed, and the games of the coming two weeks (and
// the last two days, for catching up).
func (s *Server) handleSportsHome(w http.ResponseWriter, r *http.Request) {
	out := struct {
		On        bool          `json:"on"`
		Backend   string        `json:"backend,omitempty"`
		Problem   string        `json:"problem,omitempty"`
		CanManage bool          `json:"canManage"`
		CanStream bool          `json:"canStream"` // Usherr's streaming engine is ready, and the person may play
		CanGet    bool          `json:"canGet"`    // games can be downloaded (the backend does), and the person may
		Leagues   []sportLeague `json:"leagues"`
		Teams     []sportTeam   `json:"teams"`
		Games     []sportEvent  `json:"games"`
	}{Leagues: []sportLeague{}, Teams: []sportTeam{}, Games: []sportEvent{}}
	b, err := s.sports()
	if err != nil {
		if !errors.Is(err, errSportsOff) {
			out.Problem = err.Error()
		}
		writeJSON(w, http.StatusOK, out)
		return
	}
	out.On, out.Backend, out.CanManage, out.CanStream = true, b.name(), s.userAllows(r, auth.PermManage), s.userAllows(r, auth.PermPlay) && s.ownProblem() == ""
	out.CanGet = out.CanManage && b.canDownload()
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if out.Leagues, err = b.leagues(ctx); err != nil {
		out.Problem, out.Leagues = err.Error(), []sportLeague{}
		writeJSON(w, http.StatusOK, out)
		return
	}
	sort.Slice(out.Leagues, func(i, j int) bool {
		return strings.ToLower(out.Leagues[i].Name) < strings.ToLower(out.Leagues[j].Name)
	})
	if teams, err := b.followedTeams(ctx); err == nil {
		out.Teams = teams
	}
	now := time.Now()
	if games, err := b.between(ctx, now.Add(-48*time.Hour), now.Add(14*24*time.Hour)); err == nil {
		sort.SliceStable(games, func(i, j int) bool { return games[i].When.Before(games[j].When) })
		if len(games) > 200 {
			games = games[:200]
		}
		out.Games = games
	}
	writeJSON(w, http.StatusOK, out)
}

// countSports adds the sports library to the Dashboard's counts (Usherr).
func (s *Server) countSports(ctx context.Context, c *streamedCounts) {
	b, err := s.sports()
	if err != nil {
		return
	}
	c.sportsOn = true
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	leagues, err := b.leagues(ctx)
	if err != nil {
		return
	}
	c.sportLeagues = len(leagues)
	for _, l := range leagues {
		c.sportGames += l.Files
		c.sportWanted += l.Missing
	}
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sports_streamed`).Scan(&c.sportStreams)
}

// GET /api/home/sports/on: whether Sports is set up, for the menu (quick:
// it doesn't ask the backend).
func (s *Server) handleSportsOn(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"on": s.sportsMode() != ""})
}

// GET /api/home/sports/search?q=: leagues and teams by name, marked when
// they're already in the library or followed.
func (s *Server) handleSportsSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) < 2 || len(q) > 100 {
		writeError(w, http.StatusBadRequest, "Type at least two letters.")
		return
	}
	b, err := s.sports()
	if err != nil {
		sportsFail(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	leagues, err := b.searchLeagues(ctx, q)
	if err != nil {
		sportsFail(w, err)
		return
	}
	teams, err := b.searchTeams(ctx, q)
	if err != nil {
		teams = nil // the leagues are still worth showing
	}
	if have, err := b.leagues(ctx); err == nil {
		ids := map[string]string{}
		for _, l := range have {
			ids[l.ExternalID] = l.ID
		}
		for i := range leagues {
			if id, ok := ids[leagues[i].ExternalID]; ok {
				leagues[i].ID, leagues[i].InLibrary = id, true
			}
		}
	}
	if have, err := b.followedTeams(ctx); err == nil {
		ids := map[string]string{}
		for _, t := range have {
			ids[t.ExternalID] = t.ID
		}
		for i := range teams {
			if id, ok := ids[teams[i].ExternalID]; ok {
				teams[i].ID, teams[i].Followed = id, true
			}
		}
	}
	if leagues == nil {
		leagues = []sportLeague{}
	}
	if teams == nil {
		teams = []sportTeam{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"leagues": leagues, "teams": teams})
}

// GET /api/home/sports/leagues/{id}?season=: one league of the library, its
// seasons (newest first) and the games of one season (the newest unless
// asked).
func (s *Server) handleSportsLeague(w http.ResponseWriter, r *http.Request) {
	b, err := s.sports()
	if err != nil {
		sportsFail(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	id := r.PathValue("id")
	have, err := b.leagues(ctx)
	if err != nil {
		sportsFail(w, err)
		return
	}
	var league *sportLeague
	for i := range have {
		if have[i].ID == id {
			league = &have[i]
		}
	}
	if league == nil {
		sportsFail(w, errSportNotFound)
		return
	}
	seasons, err := b.leagueSeasons(ctx, id)
	if err != nil {
		sportsFail(w, err)
		return
	}
	season := strings.TrimSpace(r.URL.Query().Get("season"))
	if season == "" && len(seasons) > 0 {
		season = seasons[0]
	}
	games := []sportEvent{}
	if season != "" {
		if games, err = b.leagueEvents(ctx, id, season); err != nil {
			sportsFail(w, err)
			return
		}
	}
	sort.SliceStable(games, func(i, j int) bool { return games[i].When.Before(games[j].When) })
	if seasons == nil {
		seasons = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"league": league, "seasons": seasons, "season": season, "games": games, "canManage": s.userAllows(r, auth.PermManage),
		"canStream": s.userAllows(r, auth.PermPlay) && s.ownProblem() == "", "canGet": s.userAllows(r, auth.PermManage) && b.canDownload()})
}

// POST /api/home/sports/leagues: put a found league in the library.
func (s *Server) handleSportsAddLeague(w http.ResponseWriter, r *http.Request) {
	var req struct {
		sportLeague
		Monitor bool `json:"monitor"`
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.ExternalID) == "" || strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	b, err := s.sports()
	if err != nil {
		sportsFail(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	if err := b.addLeague(ctx, req.sportLeague, req.Monitor); err != nil {
		sportsFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": req.Name + " is in Sports. Its games arrive in a minute."})
}

// POST /api/home/sports/teams: follow a found team.
func (s *Server) handleSportsFollowTeam(w http.ResponseWriter, r *http.Request) {
	var t sportTeam
	if err := decodeJSON(r, &t); err != nil || strings.TrimSpace(t.ExternalID) == "" || strings.TrimSpace(t.Name) == "" || strings.TrimSpace(t.Sport) == "" {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	b, err := s.sports()
	if err != nil {
		sportsFail(w, err)
		return
	}
	if err := b.followTeam(r.Context(), t); err != nil {
		sportsFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Following " + t.Name + "."})
}

// DELETE /api/home/sports/teams/{id}: stop following a team. Its games and
// files stay.
func (s *Server) handleSportsUnfollowTeam(w http.ResponseWriter, r *http.Request) {
	b, err := s.sports()
	if err != nil {
		sportsFail(w, err)
		return
	}
	if err := b.unfollowTeam(r.Context(), r.PathValue("id")); err != nil {
		sportsFail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/home/sports/events/{id}/get: watch for one game and search for
// it now.
func (s *Server) handleSportsGetEvent(w http.ResponseWriter, r *http.Request) {
	b, err := s.sports()
	if err != nil {
		sportsFail(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := b.getEvent(ctx, r.PathValue("id")); err != nil {
		sportsFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": b.name() + " is looking for it."})
}

// sportsImageHosts are where sports artwork comes from.
var sportsImageHosts = []string{"https://sportarr.net/static/images/", "https://www.thesportsdb.com/images/", "https://r2.thesportsdb.com/images/"}

var sportsImageClient = &http.Client{Timeout: 20 * time.Second}

// GET /api/home/sports/image?u=: a league's badge, a team's crest or a
// game's picture, through Usherr (the page only loads images from Usherr).
func (s *Server) handleSportsImage(w http.ResponseWriter, r *http.Request) {
	u := r.URL.Query().Get("u")
	ok := false
	for _, h := range sportsImageHosts {
		ok = ok || strings.HasPrefix(u, h)
	}
	if !ok || len(u) > 1024 || strings.Contains(u, "..") {
		writeError(w, http.StatusNotFound, "Unknown image.")
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, u, nil)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Bad image address.")
		return
	}
	req.Header.Set("User-Agent", "Usherr/"+s.version)
	resp, err := sportsImageClient.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "The image couldn't be fetched.")
		return
	}
	defer resp.Body.Close()
	ct := resp.Header.Get("Content-Type")
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(ct, "image/") || strings.Contains(ct, "svg") {
		writeError(w, http.StatusBadGateway, "The image couldn't be fetched.")
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "private, max-age=604800")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = io.Copy(w, io.LimitReader(resp.Body, 10<<20))
}

type sportsSettings struct {
	Mode           string `json:"mode"`
	SportarrURL    string `json:"sportarrUrl"`
	SportarrKeySet bool   `json:"sportarrKeySet"`
	TSDBKeySet     bool   `json:"tsdbKeySet"` // a paid TheSportsDB key (Usherr's own engine)
	Ready          bool   `json:"ready"`
	Problem        string `json:"problem,omitempty"`
}

func (s *Server) currentSportsSettings(ctx context.Context) sportsSettings {
	var out sportsSettings
	out.Mode = s.sportsMode()
	out.SportarrURL, _ = s.Settings.Get(keySportsSportarrURL)
	k, _ := s.Settings.Get(keySportsSportarrKey)
	out.SportarrKeySet = strings.TrimSpace(k) != ""
	out.TSDBKeySet = s.tsdb().paid()
	if out.Mode == "" {
		return out
	}
	b, err := s.sports()
	if err == nil {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		err = b.check(ctx)
	}
	out.Ready = err == nil
	if err != nil {
		out.Problem = err.Error()
	}
	return out
}

// GET /api/home/sports/settings
func (s *Server) handleGetSportsSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.currentSportsSettings(r.Context()))
}

// PUT /api/home/sports/settings: only what is sent changes.
func (s *Server) handlePutSportsSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Mode              *string `json:"mode"`
		SportarrURL       *string `json:"sportarrUrl"`
		SportarrKey       *string `json:"sportarrKey"`
		RemoveSportarrKey bool    `json:"removeSportarrKey"`
		TSDBKey           *string `json:"tsdbKey"`
		RemoveTSDBKey     bool    `json:"removeTsdbKey"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	if req.Mode != nil {
		m := strings.TrimSpace(*req.Mode)
		if m != "" && m != "sportarr" && m != "usherr" {
			writeError(w, http.StatusBadRequest, "Sports runs on Sportarr or on Usherr's own engine, or is off.")
			return
		}
		_ = s.Settings.Set(keySportsMode, m, false)
	}
	if req.SportarrURL != nil {
		a, err := cleanBaseURL(*req.SportarrURL)
		if err != nil {
			writeError(w, http.StatusBadRequest, "Sportarr's address "+err.Error())
			return
		}
		_ = s.Settings.Set(keySportsSportarrURL, a, false)
	}
	if req.RemoveSportarrKey {
		_ = s.Settings.Set(keySportsSportarrKey, "", false)
	} else if req.SportarrKey != nil && strings.TrimSpace(*req.SportarrKey) != "" {
		_ = s.Settings.Set(keySportsSportarrKey, strings.TrimSpace(*req.SportarrKey), true)
	}
	// The Dashboard counts again on its next visit (its Sports card).
	streamedCountsCache.Lock()
	streamedCountsCache.at = time.Time{}
	streamedCountsCache.Unlock()
	if req.RemoveTSDBKey {
		_ = s.Settings.Set(keySportsTSDBKey, "", false)
	} else if req.TSDBKey != nil && strings.TrimSpace(*req.TSDBKey) != "" {
		_ = s.Settings.Set(keySportsTSDBKey, strings.TrimSpace(*req.TSDBKey), true)
	}
	writeJSON(w, http.StatusOK, s.currentSportsSettings(r.Context()))
}

// POST /api/home/sports/test
func (s *Server) handleTestSports(w http.ResponseWriter, r *http.Request) {
	b, err := s.sports()
	if err != nil {
		writeError(w, http.StatusPreconditionFailed, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if err := b.check(ctx); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	msg := b.name() + " is connected."
	if c := s.tsdb(); c.paid() {
		// The key is used for live scores whatever Sports runs on.
		if _, err := c.searchLeagues(ctx, "NFL"); err != nil {
			writeError(w, http.StatusBadGateway, msg+" But TheSportsDB: "+err.Error())
			return
		}
		msg += " Your TheSportsDB key works."
	}
	if _, own := b.(*ownSports); own {
		msg = "TheSportsDB answers with the free key: search and part of each season."
		if s.tsdb().paid() {
			msg = "TheSportsDB answers with your key: full schedules."
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": msg})
}
