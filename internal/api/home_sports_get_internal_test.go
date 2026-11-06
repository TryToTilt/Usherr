package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/sports"
)

func sportsRulesServer(t *testing.T) *Server {
	t.Helper()
	s, db := optionsServer(t)
	s.db = db
	return s
}

func addSportLeague(t *testing.T, s *Server, ext, name, sport string) int64 {
	t.Helper()
	res, err := s.db.Exec(`INSERT INTO sports_leagues (external_id, name, sport, added_ms) VALUES (?, ?, ?, 1)`, ext, name, sport)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func addSportEvent(t *testing.T, s *Server, league int64, ext, title, home, away string, at time.Time) int64 {
	t.Helper()
	res, err := s.db.Exec(`INSERT INTO sports_events (league_id, external_id, title, home, away, season, at_ms) VALUES (?, ?, ?, ?, ?, '2026', ?)`,
		league, ext, title, home, away, at.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func wantedIDs(t *testing.T, s *Server) map[int64]bool {
	t.Helper()
	rows, err := s.db.Query(`SELECT id FROM sports_events WHERE monitored = 1`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		_ = rows.Scan(&id)
		out[id] = true
	}
	return out
}

// The Sports folder: as set, else "Sports" next to the TV folder, but never
// at the top of the disk (a TV folder mounted at /tv).
func TestSportsFolderDefault(t *testing.T) {
	s := sportsRulesServer(t)
	for tv, want := range map[string]string{"": "", "/tv": "", "/data/media/tv": "/data/media/Sports"} {
		s.cfg.TVDir = tv
		if got := filepath.ToSlash(s.sportsRoot()); got != want {
			t.Errorf("TV at %q: %q, want %q", tv, got, want)
		}
	}
	_ = s.Settings.Set(keySportsPath, "/data/sports", false)
	if got := s.sportsRoot(); got != "/data/sports" {
		t.Fatalf("set: %q", got)
	}
}

// Which games are wanted: a league's new games (all, or only the followed
// teams'), a followed team's games, a race weekend's chosen sessions; a
// game chosen by hand stays as chosen, and old games aren't touched.
func TestSportsMonitoringRules(t *testing.T) {
	s := sportsRulesServer(t)
	ctx := context.Background()
	soon := time.Now().Add(48 * time.Hour)
	nfl := addSportLeague(t, s, "4391", "NFL", "American Football")
	other := addSportEvent(t, s, nfl, "1", "Packers vs Lions", "Green Bay Packers", "Detroit Lions", soon)
	chiefs := addSportEvent(t, s, nfl, "2", "Chiefs vs Raiders", "Kansas City Chiefs", "Las Vegas Raiders", soon)
	old := addSportEvent(t, s, nfl, "3", "Chiefs vs Broncos", "Kansas City Chiefs", "Denver Broncos", time.Now().AddDate(0, 0, -20))
	byHand := addSportEvent(t, s, nfl, "4", "Bears vs Vikings", "Chicago Bears", "Minnesota Vikings", soon)
	if _, err := s.db.Exec(`UPDATE sports_events SET by_hand = 1 WHERE id = ?`, byHand); err != nil {
		t.Fatal(err)
	}

	// A followed team that gets its games.
	if _, err := s.db.Exec(`INSERT INTO sports_teams (external_id, name, sport, monitored, added_ms) VALUES ('134931', 'Kansas City Chiefs', 'American Football', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	s.applyAllSportsRules(ctx)
	if got := wantedIDs(t, s); !reflect.DeepEqual(got, map[int64]bool{chiefs: true}) {
		t.Fatalf("a team's games: %v", got)
	}
	// The league, only the followed teams' games: the same.
	_, _ = s.db.Exec(`UPDATE sports_teams SET monitored = 0`)
	_, _ = s.db.Exec(`UPDATE sports_leagues SET monitored = 1, teams_only = 1 WHERE id = ?`, nfl)
	s.applySportsRules(ctx, nfl)
	if got := wantedIDs(t, s); !reflect.DeepEqual(got, map[int64]bool{chiefs: true}) {
		t.Fatalf("followed teams only: %v", got)
	}
	// The whole league: every new game but the one chosen by hand and the old one.
	_, _ = s.db.Exec(`UPDATE sports_leagues SET teams_only = 0 WHERE id = ?`, nfl)
	s.applySportsRules(ctx, nfl)
	if got := wantedIDs(t, s); !reflect.DeepEqual(got, map[int64]bool{chiefs: true, other: true}) || got[old] || got[byHand] {
		t.Fatalf("the whole league: %v", got)
	}

	// A race weekend: only the races.
	f1 := addSportLeague(t, s, "4370", "Formula 1", "Motorsport")
	race := addSportEvent(t, s, f1, "10", "Azerbaijan Grand Prix", "", "", soon)
	quali := addSportEvent(t, s, f1, "11", "Azerbaijan Grand Prix Qualifying", "", "", soon)
	_, _ = s.db.Exec(`UPDATE sports_leagues SET monitored = 1, parts = 'race' WHERE id = ?`, f1)
	s.applySportsRules(ctx, f1)
	if got := wantedIDs(t, s); !got[race] || got[quali] {
		t.Fatalf("race weekend: %v", got)
	}

	// A fight card's chosen parts.
	g := sportGame{ev: sports.Event{League: sports.League{Name: "UFC", Sport: "Fighting"}}, leagueParts: "main,prelims"}
	if got := g.wantedParts(); !reflect.DeepEqual(got, []string{sports.PartPrelims, sports.PartMainCard}) {
		t.Fatalf("fight card parts: %v", got)
	}
	g.leagueParts = ""
	if got := g.wantedParts(); len(got) != 3 {
		t.Fatalf("every part: %v", got)
	}
	if got := (sportGame{ev: sports.Event{League: sports.League{Name: "NFL"}}}).wantedParts(); !reflect.DeepEqual(got, []string{""}) {
		t.Fatalf("a game: %v", got)
	}
}

// The automatic search looks for a game a few hours after it, then less and
// less often, and stops after the days it keeps looking.
func TestSportsSearchSchedule(t *testing.T) {
	s := sportsRulesServer(t)
	ctx := context.Background()
	now := time.Now()
	nfl := addSportLeague(t, s, "4391", "NFL", "American Football")
	justPlayed := addSportEvent(t, s, nfl, "1", "Packers vs Lions", "Green Bay Packers", "Detroit Lions", now.Add(-4*time.Hour))
	playing := addSportEvent(t, s, nfl, "2", "Chiefs vs Raiders", "Kansas City Chiefs", "Las Vegas Raiders", now.Add(-time.Hour))
	longAgo := addSportEvent(t, s, nfl, "3", "Bears vs Vikings", "Chicago Bears", "Minnesota Vikings", now.AddDate(0, 0, -20))
	lastWeek := addSportEvent(t, s, nfl, "4", "Jets vs Bills", "New York Jets", "Buffalo Bills", now.AddDate(0, 0, -7))
	_, _ = s.db.Exec(`UPDATE sports_events SET monitored = 1`)

	due := func() map[int64]bool {
		out := map[int64]bool{}
		for _, g := range s.dueSportGames(ctx, now) {
			out[g.id] = true
		}
		return out
	}
	if got := due(); !reflect.DeepEqual(got, map[int64]bool{justPlayed: true, lastWeek: true}) || got[playing] || got[longAgo] {
		t.Fatalf("due: %v", got)
	}
	// Searched an hour ago: not again yet; a week-old game, searched 20 hours ago, neither.
	_, _ = s.db.Exec(`UPDATE sports_events SET searched_ms = ? WHERE id = ?`, now.Add(-time.Hour).UnixMilli(), justPlayed)
	_, _ = s.db.Exec(`UPDATE sports_events SET searched_ms = ? WHERE id = ?`, now.Add(-20*time.Hour).UnixMilli(), lastWeek)
	if got := due(); len(got) != 0 {
		t.Fatalf("searched recently: %v", got)
	}
	// Three and a half hours later the fresh game is looked for again.
	_, _ = s.db.Exec(`UPDATE sports_events SET searched_ms = ? WHERE id = ?`, now.Add(-210*time.Minute).UnixMilli(), justPlayed)
	if got := due(); !got[justPlayed] {
		t.Fatalf("again: %v", got)
	}
	// A game with its file isn't.
	_, _ = s.db.Exec(`INSERT INTO sports_files (event_id, part, path, imported_ms) VALUES (?, '', ?, 1)`, justPlayed, t.TempDir())
	if got := due(); got[justPlayed] {
		t.Fatalf("has its file: %v", got)
	}
}

// "Bring my followed leagues from Sportarr": the leagues and teams of a
// (fake) Sportarr come into Usherr's own engine, with whether a league gets
// its new games; Sportarr is only read, and nothing else is brought.
func TestSportsImportFromSportarr(t *testing.T) {
	var methods []string
	sportarr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		if r.Header.Get("X-Api-Key") != "sa-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/leagues":
			_, _ = w.Write([]byte(`[{"id":1,"externalId":"4391","name":"NFL","sport":"American Football","monitored":true},
				{"id":2,"externalId":"4443","name":"UFC","sport":"Fighting","monitored":false},
				{"id":3,"externalId":"","name":"Local League","sport":"Soccer","monitored":true}]`))
		case "/api/followed-teams":
			_, _ = w.Write([]byte(`[{"id":9,"externalId":"134931","name":"Kansas City Chiefs","sport":"American Football"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer sportarr.Close()
	var schedules atomic.Int32 // the background fetch of each new league's season
	tsdb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/schedule/") {
			schedules.Add(1)
		}
		switch r.URL.Path {
		case "/v2/json/lookup/league/4391":
			_, _ = w.Write([]byte(`{"lookup":[{"idLeague":"4391","strLeague":"NFL","strSport":"American Football","strCurrentSeason":"2026"}]}`))
		case "/v2/json/lookup/league/4443":
			_, _ = w.Write([]byte(`{"lookup":[{"idLeague":"4443","strLeague":"UFC","strSport":"Fighting","strCurrentSeason":"2026"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer tsdb.Close()
	old := tsdbBase
	tsdbBase = tsdb.URL
	defer func() { tsdbBase = old }()
	tsdbPace.last = time.Time{}

	s := sportsRulesServer(t)
	for k, v := range map[string]string{keySportsMode: "usherr", keySportsSportarrURL: sportarr.URL, keySportsSportarrKey: "sa-key", keySportsTSDBKey: "paid-key"} {
		if err := s.Settings.Set(k, v, false); err != nil {
			t.Fatal(err)
		}
	}
	rec := httptest.NewRecorder()
	s.handleSportsImportSportarr(rec, httptest.NewRequest(http.MethodPost, "/api/home/sports/import-sportarr", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"leagues":2`) || !strings.Contains(rec.Body.String(), `"teams":1`) ||
		!strings.Contains(rec.Body.String(), "Local League") {
		t.Fatalf("import: %d %s", rec.Code, rec.Body.String())
	}
	var nflGets, ufcGets bool
	_ = s.db.QueryRow(`SELECT monitored FROM sports_leagues WHERE external_id = '4391'`).Scan(&nflGets)
	_ = s.db.QueryRow(`SELECT monitored FROM sports_leagues WHERE external_id = '4443'`).Scan(&ufcGets)
	if !nflGets || ufcGets {
		t.Fatalf("monitored: NFL %v, UFC %v", nflGets, ufcGets)
	}
	// Again: nothing twice.
	rec = httptest.NewRecorder()
	s.handleSportsImportSportarr(rec, httptest.NewRequest(http.MethodPost, "/api/home/sports/import-sportarr", nil))
	if !strings.Contains(rec.Body.String(), `"leagues":0`) || !strings.Contains(rec.Body.String(), `"already":2`) {
		t.Fatalf("again: %s", rec.Body.String())
	}
	// The new leagues' seasons are fetched in the background: wait for them
	// before the fake TheSportsDB goes.
	for deadline := time.Now().Add(20 * time.Second); schedules.Load() < 2 && time.Now().Before(deadline); {
		time.Sleep(50 * time.Millisecond)
	}
	for _, m := range methods {
		if m != http.MethodGet {
			t.Fatalf("Sportarr was sent a %s", m)
		}
	}
}
