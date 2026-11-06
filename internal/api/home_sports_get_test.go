package api_test

import (
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/api"
	"github.com/rdborg/mediarium/internal/config"
	"github.com/rdborg/mediarium/internal/crypto"
)

// fakeSportsIndexer answers every search with a few releases around one NFL
// game: the game itself in two qualities, another game the same day, and
// the highlights. It remembers what it was asked.
func fakeSportsIndexer(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var asked []string
	mux := http.NewServeMux()
	mux.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
		if q := r.URL.Query().Get("q"); q != "" {
			mu.Lock()
			asked = append(asked, q)
			mu.Unlock()
		}
		item := func(title, file string, size int64) string {
			return fmt.Sprintf(`<item><title>%s</title><guid>%s</guid><enclosure url="http://%s/nzb/%s.nzb" length="%d" type="application/x-nzb" />
<newznab:attr name="size" value="%d"/><newznab:attr name="category" value="5060"/></item>`, title, file, r.Host, file, size, size)
		}
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprintf(w, `<?xml version="1.0"?>
<rss version="2.0" xmlns:newznab="http://www.newznab.com/DTD/2010/feeds/attributes/"><channel>%s%s%s%s</channel></rss>`,
			item("NFL.2026.09.28.Philadelphia.Eagles.vs.Chicago.Bears.720p.HDTV.x264-FIXTURE", "game720", 2<<30),
			item("NFL.2026.09.28.Philadelphia.Eagles.vs.Chicago.Bears.1080p.WEB.h264-FIXTURE", "game1080", 4<<30),
			item("NFL.2026.09.28.Green.Bay.Packers.vs.Detroit.Lions.1080p.WEB.h264-FIXTURE", "other", 4<<30),
			item("NFL.2026.09.28.Eagles.vs.Bears.Highlights.1080p.WEB.h264-FIXTURE", "highlights", 500<<20))
	})
	mux.HandleFunc("/nzb/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprint(w, `<?xml version="1.0"?>
<nzb xmlns="http://www.newzbin.com/DTD/2003/nzb">
<file subject="[1/1] &quot;fixture-movie.mkv&quot; yEnc (1/1)">
<groups><group>alt.binaries.test</group></groups>
<segments><segment bytes="1000" number="1">fixture-segment@example</segment></segments>
</file>
</nzb>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), asked...)
	}
}

// A game Usherr's own sports engine gets, start to finish, with a fake
// indexer and a fake Usenet server: wanted game -> Pick a release (judged)
// -> Search now (the right release, the best quality) -> download -> filed
// in the Sports folder the way Plex and Jellyfin read it, with its .nfo
// files -> no longer wanted.
func TestSportsGameDownloadedEndToEnd(t *testing.T) {
	content := []byte("Fixture game bytes standing in for a real video file. " + strings.Repeat("kick-off-", 200))
	nntp := newFakeNNTPServer(t, content)
	idx, asked := fakeSportsIndexer(t)

	dir := t.TempDir()
	cfg := config.Config{
		ConfigDir:           filepath.Join(dir, "config"),
		DownloadsDir:        filepath.Join(dir, "downloads"),
		DownloadsIncomplete: filepath.Join(dir, "downloads", "incomplete"),
		DownloadsComplete:   filepath.Join(dir, "downloads", "complete"),
		MoviesDir:           filepath.Join(dir, "movies"),
	}
	sportsDir := filepath.Join(dir, "sports")
	for _, d := range []string{cfg.ConfigDir, cfg.DownloadsIncomplete, cfg.MoviesDir, sportsDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	db, err := api.OpenTestDB(filepath.Join(cfg.ConfigDir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	box, err := crypto.LoadOrCreateKey(filepath.Join(cfg.ConfigDir, "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	server, err := api.New(db, cfg, box, "", "test")
	if err != nil {
		t.Fatal(err)
	}
	httpSrv := httptest.NewServer(server.Routes())
	defer httpSrv.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/onboarding/admin", map[string]string{
		"username": "coach", "password": "correct-horse-battery-staple", "firstName": "Pat", "lastName": "Coach",
	}, http.StatusCreated)
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/indexers", map[string]any{
		"name": "Fixture Indexer", "definitionId": "fixture", "baseUrl": idx.URL, "apiKey": "fixture-key",
	}, http.StatusCreated)
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/usenet-servers", map[string]any{
		"name": "Fixture Usenet", "host": nntp.addr, "port": nntp.port, "useSsl": false, "connections": 2,
	}, http.StatusCreated)

	// Sports on Usherr's own engine, with its folder (set directly: the
	// settings page would ask TheSportsDB whether it answers).
	for k, v := range map[string]string{"sports.mode": "usherr", "sports.path": sportsDir} {
		if err := server.Settings.Set(k, v, false); err != nil {
			t.Fatal(err)
		}
	}
	// A league that gets its games, and one of its games, played.
	res, err := db.Exec(`INSERT INTO sports_leagues (external_id, name, sport, description, monitored, added_ms) VALUES ('4391', 'NFL', 'American Football', 'Football in the US.', 1, 1)`)
	if err != nil {
		t.Fatal(err)
	}
	league, _ := res.LastInsertId()
	res, err = db.Exec(`INSERT INTO sports_events (league_id, external_id, title, sport, home, away, season, round, at_ms, venue, monitored)
		VALUES (?, '2338110', 'Philadelphia Eagles vs Chicago Bears', 'American Football', 'Philadelphia Eagles', 'Chicago Bears', '2026', '4', ?, 'Lincoln Financial Field', 1)`,
		league, time.Date(2026, 9, 29, 0, 15, 0, 0, time.UTC).UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	game, _ := res.LastInsertId()

	// It's wanted.
	wanted := getJSON[map[string]any](t, client, httpSrv.URL+"/api/home/sports/wanted")
	if games, _ := wanted["games"].([]any); len(games) != 1 {
		t.Fatalf("wanted: %+v", wanted)
	}

	// Pick a release: every release, judged; the game's best first.
	picked := getJSON[map[string]any](t, client, fmt.Sprintf("%s/api/home/sports/events/%d/releases", httpSrv.URL, game))
	list, _ := picked["releases"].([]any)
	if len(list) != 4 {
		t.Fatalf("releases: %+v", picked)
	}
	first := list[0].(map[string]any)
	if first["title"] != "NFL.2026.09.28.Philadelphia.Eagles.vs.Chicago.Bears.1080p.WEB.h264-FIXTURE" || first["matches"] != true || first["accepted"] != true {
		t.Fatalf("best release: %+v", first)
	}
	for _, x := range list[2:] {
		rel := x.(map[string]any)
		if rel["matches"] == true {
			t.Fatalf("%v matched the game: %+v", rel["title"], rel)
		}
		if why, _ := rel["rejections"].([]any); len(why) == 0 {
			t.Fatalf("%v: no reason given", rel["title"])
		}
	}
	if q := asked(); len(q) == 0 || q[0] != "nfl 2026 09 28" {
		t.Fatalf("searched for %q", q)
	}

	// Search now: the best release of the game goes down the line.
	got := postJSON[map[string]any](t, client, fmt.Sprintf("%s/api/home/sports/events/%d/search", httpSrv.URL, game), map[string]any{}, http.StatusOK)
	if got["grabbed"] != float64(1) {
		t.Fatalf("search now: %+v", got)
	}
	deadline := time.Now().Add(20 * time.Second)
	var item map[string]any
	for time.Now().Before(deadline) {
		q := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/queue")
		if len(q) == 1 {
			item = q[0]
			if st := item["status"]; st == "completed" || st == "failed" {
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if item == nil || item["status"] != "completed" {
		t.Fatalf("download: %+v", item)
	}
	if item["title"] != "Philadelphia Eagles vs Chicago Bears" || !strings.Contains(fmt.Sprint(item["subtitle"]), "NFL") {
		t.Fatalf("the queue names it %q / %q", item["title"], item["subtitle"])
	}

	// Filed the way Plex and Jellyfin read a TV library.
	season := filepath.Join(sportsDir, "NFL", "Season 2026")
	file := filepath.Join(season, "NFL - S2026E01 - Philadelphia Eagles vs Chicago Bears - 1080p WEB.mkv")
	data, err := os.ReadFile(file)
	if err != nil {
		entries, _ := os.ReadDir(season)
		t.Fatalf("filed: %v (the season folder has %v)", err, entries)
	}
	if string(data) != string(content) {
		t.Fatalf("the file isn't the download (%d bytes)", len(data))
	}
	nfo, err := os.ReadFile(strings.TrimSuffix(file, ".mkv") + ".nfo")
	if err != nil || !strings.Contains(string(nfo), "<title>Philadelphia Eagles vs Chicago Bears</title>") || !strings.Contains(string(nfo), "<aired>2026-09-28</aired>") {
		t.Fatalf("game nfo: %v %s", err, nfo)
	}
	if show, err := os.ReadFile(filepath.Join(sportsDir, "NFL", "tvshow.nfo")); err != nil || !strings.Contains(string(show), "<title>NFL</title>") {
		t.Fatalf("league nfo: %v %s", err, show)
	}
	var tier string
	if err := db.QueryRow(`SELECT quality FROM sports_files WHERE event_id = ?`, game).Scan(&tier); err != nil || tier != "WEBDL-1080p" {
		t.Fatalf("file row: %v %q", err, tier)
	}

	// No longer wanted; the league counts it.
	wanted = getJSON[map[string]any](t, client, httpSrv.URL+"/api/home/sports/wanted")
	if games, _ := wanted["games"].([]any); len(games) != 0 {
		t.Fatalf("still wanted: %+v", wanted)
	}
	home := getJSON[map[string]any](t, client, httpSrv.URL+"/api/home/sports")
	leagues, _ := home["leagues"].([]any)
	if len(leagues) != 1 || home["canGet"] != true {
		t.Fatalf("sports home: %+v", home)
	}
	if l := leagues[0].(map[string]any); l["files"] != float64(1) || l["missing"] != nil {
		t.Fatalf("league counts: %+v", l)
	}
}
