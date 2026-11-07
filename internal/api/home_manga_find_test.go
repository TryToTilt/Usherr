package api

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/manga"
)

// noPanelModels keeps a test from fetching the AI panel models: the gutter method answers.
func noPanelModels(t *testing.T) {
	t.Helper()
	panelAIMu.Lock()
	was := panelAIFailed
	panelAIFailed = time.Now()
	panelAIMu.Unlock()
	t.Cleanup(func() {
		panelAIMu.Lock()
		panelAIFailed = was
		panelAIMu.Unlock()
	})
}

// writePanelCBZ makes a volume of pages, each drawn with the given panels.
func writePanelCBZ(t *testing.T, path string, pages [][]image.Rectangle, sizes [][2]int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for i, boxes := range pages {
		img := image.NewGray(image.Rect(0, 0, sizes[i][0], sizes[i][1]))
		draw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
		for _, b := range boxes {
			draw.Draw(img, b, image.NewUniform(color.Gray{Y: 50}), image.Point{}, draw.Src)
		}
		w, err := zw.Create(fmt.Sprintf("%03d.png", i+1))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(w, img); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

func panelsOf(t *testing.T, s *Server, user, volume int64, page int, rtl string) pagePanels {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/manga/volumes/%d/pages/%d/panels?rtl=%s", volume, page, rtl), nil)
	r.SetPathValue("id", strconv.FormatInt(volume, 10))
	r.SetPathValue("n", strconv.Itoa(page))
	rec := httptest.NewRecorder()
	s.handleMangaPagePanels(rec, withUser(r, user))
	if rec.Code != http.StatusOK {
		t.Fatalf("page %d: %d %s", page, rec.Code, rec.Body.String())
	}
	var out pagePanels
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestMangaPanelsReadRightToLeftAndFallBack(t *testing.T) {
	noPanelModels(t)
	s, repo, root := mangaTestServer(t)
	user := mangaUser(t, s, "reader")
	sr := addSeries(t, repo, "Spy × Family", 1, 1)
	v := fileVolume(t, repo, root, sr, "1", 1)
	two := []image.Rectangle{image.Rect(420, 30, 770, 570), image.Rect(30, 30, 380, 570), image.Rect(30, 620, 770, 1170)}
	spread := []image.Rectangle{image.Rect(1050, 50, 1950, 650), image.Rect(50, 50, 950, 650), image.Rect(1050, 750, 1950, 1350), image.Rect(50, 750, 950, 1350)}
	writePanelCBZ(t, v.FilePath, [][]image.Rectangle{two, nil, spread}, [][2]int{{800, 1200}, {800, 1200}, {2000, 1400}})
	st, _ := os.Stat(v.FilePath)
	if err := repo.SetVolumeFile(v.ID, v.FilePath, st.Size(), "own"); err != nil {
		t.Fatal(err)
	}

	got := panelsOf(t, s, user, v.ID, 1, "1")
	if len(got.Panels) != 3 || got.Panels[0].X < 0.4 || got.Panels[1].X > 0.4 || got.Panels[2].Y < 0.4 {
		t.Fatalf("right to left: %+v", got)
	}
	// The other way is another order, kept apart.
	got = panelsOf(t, s, user, v.ID, 1, "0")
	if got.Panels[0].X > 0.4 {
		t.Errorf("left to right: %+v", got)
	}
	// A page with no panels to tell apart has none, and the reader shows it whole.
	if got = panelsOf(t, s, user, v.ID, 2, "1"); len(got.Panels) != 0 {
		t.Errorf("blank page: %+v", got)
	}
	// A two-page spread is read a page at a time.
	got = panelsOf(t, s, user, v.ID, 3, "1")
	if len(got.Panels) != 4 || got.Panels[0].X < 0.5 || got.Panels[1].X < 0.5 || got.Panels[2].X > 0.5 {
		t.Errorf("spread: %+v", got)
	}
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM manga_panels WHERE volume_id = ?`, v.ID).Scan(&n)
	if n == 0 {
		t.Error("nothing was kept")
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.SetPathValue("id", strconv.FormatInt(v.ID, 10))
	r.SetPathValue("n", "99")
	rec := httptest.NewRecorder()
	s.handleMangaPagePanels(rec, withUser(r, user))
	if rec.Code != http.StatusNotFound {
		t.Errorf("no such page: %d", rec.Code)
	}
}

func TestMangaPanelsStripReadsTopToBottom(t *testing.T) {
	noPanelModels(t)
	s, repo, root := mangaTestServer(t)
	user := mangaUser(t, s, "reader")
	sr := addSeries(t, repo, "Tower", 1, 1)
	v := fileVolume(t, repo, root, sr, "1", 1)
	writePanelCBZ(t, v.FilePath, [][]image.Rectangle{{image.Rect(0, 100, 600, 800), image.Rect(0, 950, 600, 1700), image.Rect(0, 1900, 600, 2900)}}, [][2]int{{600, 3000}})
	st, _ := os.Stat(v.FilePath)
	_ = repo.SetVolumeFile(v.ID, v.FilePath, st.Size(), "own")
	got := panelsOf(t, s, user, v.ID, 1, "0")
	if len(got.Panels) != 3 || got.Panels[0].Y > got.Panels[1].Y || got.Panels[1].Y > got.Panels[2].Y {
		t.Errorf("%+v", got)
	}
}

func TestMangaReaderRemembersPanelsAndShift(t *testing.T) {
	s, repo, root := mangaTestServer(t)
	user := mangaUser(t, s, "reader")
	sr := addSeries(t, repo, "Kept", 1, 1)
	v := fileVolume(t, repo, root, sr, "1", 2)
	put := func(body string) int {
		r := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(body))
		r.SetPathValue("id", strconv.FormatInt(sr.ID, 10))
		rec := httptest.NewRecorder()
		s.handleMangaReaderPrefs(rec, withUser(r, user))
		return rec.Code
	}
	if c := put(`{"layout":"panels"}`); c != http.StatusNoContent {
		t.Fatal(c)
	}
	if c := put(`{"shift":1}`); c != http.StatusNoContent {
		t.Fatal(c)
	}
	if c := put(`{"layout":"sideways"}`); c != http.StatusBadRequest {
		t.Errorf("a bad layout: %d", c)
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.SetPathValue("id", strconv.FormatInt(v.ID, 10))
	rec := httptest.NewRecorder()
	s.handleMangaPages(rec, withUser(r, user))
	var pages struct {
		Layout string `json:"layout"`
		Shift  bool   `json:"shift"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &pages)
	if pages.Layout != "panels" || !pages.Shift {
		t.Errorf("%s", rec.Body.String())
	}
	if c := put(`{"shift":-1}`); c != http.StatusNoContent {
		t.Fatal(c)
	}
	if p := repo.ReaderPrefsFor(user, sr.ID); p.Shift != -1 || p.Layout != "panels" {
		t.Errorf("%+v", p)
	}
}

const fakeTile = `"title":{"romaji":"R","english":"T%d","native":""},"synonyms":[],"status":"RELEASING","startDate":{"year":2024},"volumes":null,"countryOfOrigin":"JP","description":"d","coverImage":{"large":"https://s4.anilist.co/c%d.jpg"},"siteUrl":"","genres":["Action"],"averageScore":80`

func fakeTileJSON(id int, adult bool, format string) string {
	return fmt.Sprintf(`{"id":%d,"type":"MANGA","format":%q,"isAdult":%v,`+fakeTile+`}`, id, format, adult, id, id)
}

func TestMangaDiscoverAboutAndList(t *testing.T) {
	var asked int
	ani := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked++
		var body struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch {
		case strings.Contains(body.Query, "MediaListCollection"):
			if body.Variables["name"] == "nobody" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = fmt.Fprintf(w, `{"data":{"MediaListCollection":{"lists":[{"entries":[{"status":"CURRENT","media":%s}]}]}}}`, fakeTileJSON(11, false, "MANGA"))
		case strings.Contains(body.Query, "recommendations"):
			_, _ = fmt.Fprintf(w, `{"data":{"Media":{"genres":["Action"],"averageScore":80,"relations":{"edges":[{"relationType":"SEQUEL","node":%s}]},"recommendations":{"nodes":[{"mediaRecommendation":%s},{"mediaRecommendation":%s}]}}}}`, fakeTileJSON(12, false, "MANGA"), fakeTileJSON(13, false, "MANGA"), fakeTileJSON(14, true, "MANGA"))
		default:
			_, _ = fmt.Fprintf(w, `{"data":{"Page":{"pageInfo":{"hasNextPage":false},"media":[%s,%s]}}}`, fakeTileJSON(7, false, "MANGA"), fakeTileJSON(9, false, "NOVEL"))
		}
	}))
	defer ani.Close()
	t.Setenv(mangaAniListURL, ani.URL)
	s, repo, _ := mangaTestServer(t)
	s.mangaMod().anilist.Gap = 0
	user := mangaUser(t, s, "finder")
	have, err := repo.AddSeries(manga.Series{AniListID: 7, Title: "In the library", LicensedEN: -1, MonitorNew: true}, nil)
	if err != nil {
		t.Fatal(err)
	}

	get := func(h http.HandlerFunc, url, id string) map[string]any {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, url, nil)
		r.SetPathValue("id", id)
		rec := httptest.NewRecorder()
		h(rec, withUser(r, user))
		var out map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("%s: %s", url, rec.Body.String())
		}
		return out
	}
	out := get(s.handleMangaDiscover, "/api/manga/discover?shelf=popular&genre=Action", "")
	results := out["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("a novel stays out: %v", results)
	}
	first := results[0].(map[string]any)
	if first["inLibrary"] != float64(have.ID) || first["score"] != float64(80) || !strings.HasPrefix(first["coverUrl"].(string), "/api/manga/image") {
		t.Errorf("%v", first)
	}
	if len(out["genres"].([]any)) == 0 {
		t.Error("no genres to choose from")
	}
	r := httptest.NewRequest(http.MethodGet, "/api/manga/discover?genre=Nope", nil)
	rec := httptest.NewRecorder()
	s.handleMangaDiscover(rec, withUser(r, user))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("a made-up genre: %d", rec.Code)
	}

	out = get(s.handleMangaAbout, "/", strconv.FormatInt(have.ID, 10))
	if out["score"] != float64(80) || len(out["related"].([]any)) != 1 {
		t.Errorf("%v", out)
	}
	rel := out["related"].([]any)[0].(map[string]any)
	if rel["relation"] != "sequel" || rel["anilistId"] != float64(12) {
		t.Errorf("%v", rel)
	}
	if sim := out["similar"].([]any); len(sim) != 1 {
		t.Errorf("an 18+ one is left out for this account: %v", sim)
	}
	// A series added by name just has nothing to add, and AniList isn't asked.
	byName, _ := repo.AddSeries(manga.Series{Title: "By name", LicensedEN: -1}, nil)
	before := asked
	out = get(s.handleMangaAbout, "/", strconv.FormatInt(byName.ID, 10))
	if asked != before || len(out["genres"].([]any)) != 0 {
		t.Errorf("%v, asked %d", out, asked-before)
	}

	out = get(s.handleMangaAniListList, "/api/manga/anilist-list?user=someone", "")
	if len(out["results"].([]any)) != 1 || out["results"].([]any)[0].(map[string]any)["listStatus"] != "reading" {
		t.Errorf("%v", out)
	}
	out = get(s.handleMangaAniListList, "/api/manga/anilist-list?user=nobody", "")
	if !strings.Contains(fmt.Sprint(out["problem"]), "nobody") {
		t.Errorf("%v", out)
	}
}

func TestNewVolumeGoesOnTheCalendar(t *testing.T) {
	volumes := 3
	ani := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, `{"data":{"Media":{"id":7,"title":{"english":"Harbor"},"synonyms":[],"format":"MANGA","status":"RELEASING","startDate":{"year":2015},"volumes":%d,"countryOfOrigin":"JP","isAdult":false,"description":"","coverImage":{"large":""},"siteUrl":"","staff":{"edges":[]},"externalLinks":[]}}}`, volumes)
	}))
	defer ani.Close()
	t.Setenv(mangaAniListURL, ani.URL)
	s, repo, _ := mangaTestServer(t)
	s.mangaMod().anilist.Gap = 0
	sr, err := repo.AddSeries(manga.Series{AniListID: 7, Title: "Harbor", LicensedEN: -1, MonitorNew: true, VolumeCount: 2, CheckedAt: ""}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"1", "2"} {
		if _, _, err := repo.EnsureVolume(sr.ID, n, false); err != nil {
			t.Fatal(err)
		}
	}
	// The night's refresh finds a third volume.
	s.refreshDueManga(t.Context(), time.Now())
	entries := s.mangaCalendarEntries()
	if len(entries) != 1 || entries[0].Kind != "manga" || entries[0].Subtitle != "Volume 3" || entries[0].MangaSeriesID != sr.ID {
		t.Fatalf("%+v", entries)
	}
	if entries[0].ReleaseDate != time.Now().Format("2006-01-02") {
		t.Errorf("%+v", entries[0])
	}
	// Refreshing again is no news, and a series that isn't followed makes none.
	s.refreshDueManga(t.Context(), time.Now().Add(48*time.Hour))
	if got := s.mangaCalendarEntries(); len(got) != 1 {
		t.Errorf("%+v", got)
	}
	quiet, _ := repo.AddSeries(manga.Series{AniListID: 8, Title: "Not followed", LicensedEN: -1}, nil)
	if _, _, err := repo.EnsureVolume(quiet.ID, "1", false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.refreshMangaSeries(t.Context(), quiet); err != nil {
		t.Fatal(err)
	}
	if got := s.mangaCalendarEntries(); len(got) != 1 {
		t.Errorf("%+v", got)
	}
}

func TestMangaStatsCountWhatsRead(t *testing.T) {
	s, repo, root := mangaTestServer(t)
	user := mangaUser(t, s, "reader")
	sr := addSeries(t, repo, "Counted", 1, 1)
	one := fileVolume(t, repo, root, sr, "1", 1)
	two := fileVolume(t, repo, root, sr, "2", 1)
	now := time.Now()
	if err := repo.MarkRead(user, []int64{one.ID}, true, now); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkRead(user, []int64{two.ID}, true, now.AddDate(-1, 0, 0)); err != nil {
		t.Fatal(err)
	}
	st, err := repo.ReadStatsFor(user, now)
	if err != nil || st.Volumes != 2 || st.Series != 1 || st.ThisMonth != 1 {
		t.Errorf("%v %+v", err, st)
	}
}

func TestMangaRefreshJobKeepsToItsHours(t *testing.T) {
	if !manga.RefreshDue("ongoing", "", time.Now()) || mangaScanFromHour != 2 || mangaScanToHour != 9 {
		t.Fatal("heavy work runs between 2 and 9")
	}
}
