package manga

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const tile = `"title":{"romaji":"R","english":"E%d","native":""},"synonyms":[],"status":"RELEASING","startDate":{"year":2024},"volumes":null,"countryOfOrigin":"JP","isAdult":false,"description":"","coverImage":{"large":""},"siteUrl":"","genres":["Action","Drama"],"averageScore":86`

func fakeAni(t *testing.T, answer func(vars map[string]any, query string) (int, string)) (*AniList, *atomic.Int32) {
	t.Helper()
	var asked atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		var body struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		code, out := answer(body.Variables, body.Query)
		w.WriteHeader(code)
		_, _ = w.Write([]byte(out))
	}))
	t.Cleanup(srv.Close)
	a := NewAniList("test", srv.URL)
	a.Gap = 0
	return a, &asked
}

func TestDiscoverShelvesAskAndKeep(t *testing.T) {
	var last map[string]any
	a, asked := fakeAni(t, func(vars map[string]any, query string) (int, string) {
		last = vars
		if !strings.Contains(query, "isAdult: false") || !strings.Contains(query, "format_not: NOVEL") {
			t.Errorf("asked %s", query)
		}
		return 200, `{"data":{"Page":{"pageInfo":{"hasNextPage":true},"media":[{"id":1,"type":"MANGA","format":"MANGA",` + strings.Replace(tile, "%d", "1", 1) + `}]}}}`
	})
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	list, more, err := a.Discover(context.Background(), ShelfTrending, "", 1, now)
	if err != nil || !more || len(list) != 1 {
		t.Fatalf("%v %v %+v", err, more, list)
	}
	if list[0].Score != 86 || len(list[0].Genres) != 2 || list[0].Status != "ongoing" {
		t.Errorf("%+v", list[0])
	}
	if s := last["sort"].([]any)[0]; s != "TRENDING_DESC" {
		t.Errorf("sort %v", s)
	}
	if _, has := last["genre"]; has {
		t.Errorf("a genre was asked for: %v", last)
	}
	// The same shelf again asks nothing more; another one does.
	if _, _, err := a.Discover(context.Background(), ShelfTrending, "", 1, now); err != nil || asked.Load() != 1 {
		t.Errorf("%v, asked %d times", err, asked.Load())
	}
	if _, _, err := a.Discover(context.Background(), ShelfPopular, "Action", 2, now); err != nil {
		t.Fatal(err)
	}
	if last["genre"] != "Action" || last["page"] != float64(2) || last["sort"].([]any)[0] != "POPULARITY_DESC" {
		t.Errorf("%v", last)
	}
	if _, _, err := a.Discover(context.Background(), ShelfNew, "", 1, now); err != nil {
		t.Fatal(err)
	}
	if last["since"] != float64(20260610) {
		t.Errorf("since %v", last["since"])
	}
}

func TestAboutSeriesKeepsWhatsWorthReading(t *testing.T) {
	a, asked := fakeAni(t, func(map[string]any, string) (int, string) {
		edge := func(rel, typ, format string, id string) string {
			return `{"relationType":"` + rel + `","node":{"id":` + id + `,"type":"` + typ + `","format":"` + format + `",` + strings.Replace(tile, "%d", id, 1) + `}}`
		}
		return 200, `{"data":{"Media":{"genres":["Action"],"averageScore":90,
			"relations":{"edges":[` + edge("SEQUEL", "MANGA", "MANGA", "2") + `,` + edge("ADAPTATION", "ANIME", "TV", "3") + `,` + edge("SPIN_OFF", "MANGA", "NOVEL", "4") + `,` + edge("CHARACTER", "MANGA", "MANGA", "5") + `,` + edge("SIDE_STORY", "MANGA", "ONE_SHOT", "6") + `]},
			"recommendations":{"nodes":[{"mediaRecommendation":null},{"mediaRecommendation":{"id":7,"type":"MANGA","format":"MANGA",` + strings.Replace(tile, "%d", "7", 1) + `}},{"mediaRecommendation":{"id":8,"type":"ANIME","format":"TV",` + strings.Replace(tile, "%d", "8", 1) + `}}]}}}}`
	})
	got, err := a.AboutSeries(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.Score != 90 || len(got.Genres) != 1 {
		t.Errorf("%+v", got)
	}
	if len(got.Related) != 2 || got.Related[0].Relation != "sequel" || got.Related[1].Relation != "side" {
		t.Errorf("related %+v", got.Related)
	}
	if len(got.Similar) != 1 || got.Similar[0].AniListID != 7 {
		t.Errorf("similar %+v", got.Similar)
	}
	if _, err := a.AboutSeries(context.Background(), 1); err != nil || asked.Load() != 1 {
		t.Errorf("%v, asked %d", err, asked.Load())
	}
}

func TestUserListReadingFirst(t *testing.T) {
	a, _ := fakeAni(t, func(vars map[string]any, _ string) (int, string) {
		if vars["name"] == "nobody" {
			return 404, `{"errors":[{"message":"User not found"}]}`
		}
		entry := func(status, id string) string {
			return `{"status":"` + status + `","media":{"id":` + id + `,"type":"MANGA","format":"MANGA",` + strings.Replace(tile, "%d", id, 1) + `}}`
		}
		return 200, `{"data":{"MediaListCollection":{"lists":[{"entries":[` + entry("PLANNING", "1") + `]},{"entries":[` + entry("CURRENT", "2") + `,` + entry("PLANNING", "2") + `]}]}}}`
	})
	got, err := a.UserList(context.Background(), " someone ")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].AniListID != 2 || got[0].ListStatus != "reading" || got[1].ListStatus != "planning" {
		t.Errorf("%+v", got)
	}
	if _, err := a.UserList(context.Background(), "nobody"); !errors.Is(err, ErrNoSuchList) {
		t.Errorf("%v", err)
	}
}

func TestSayingVolumes(t *testing.T) {
	vols := func(ns ...string) []Volume {
		var out []Volume
		for _, n := range ns {
			out = append(out, Volume{Number: n})
		}
		return out
	}
	for want, in := range map[string][]Volume{
		"":                nil,
		"Volume 3":        vols("3"),
		"Volumes 3, 4":    vols("3", "4"),
		"Volumes 3 to 40": vols("3", "4", "5", "6", "40"),
	} {
		if got := NumbersOf(in); got != want {
			t.Errorf("%q, want %q", got, want)
		}
	}
	if got := NewVolumesAfter(vols("2", "6", "7"), 5, true); len(got) != 2 || got[0].Number != "6" {
		t.Errorf("%+v", got)
	}
	if got := NewVolumesAfter(vols("6"), -1, false); got != nil {
		t.Errorf("a series' first list isn't news: %+v", got)
	}
}

func TestRefreshDue(t *testing.T) {
	now := time.Date(2026, 11, 7, 3, 0, 0, 0, time.UTC)
	at := func(d time.Duration) string { return now.Add(-d).Format(time.RFC3339) }
	cases := []struct {
		status, checked string
		want            bool
	}{
		{"ongoing", at(21 * time.Hour), true},
		{"ongoing", at(2 * time.Hour), false},
		{"finished", at(21 * time.Hour), false},
		{"finished", at(31 * 24 * time.Hour), true},
		{"", "", true},
	}
	for _, c := range cases {
		if got := RefreshDue(c.status, c.checked, now); got != c.want {
			t.Errorf("%+v: got %v", c, got)
		}
	}
}
