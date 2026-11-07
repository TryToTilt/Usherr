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

const sampleAnswer = `{"data":{"Page":{"media":[
 {"id":101,"title":{"romaji":"Chochin Minato","english":"Lanternfish Harbor","native":"提灯港"},"synonyms":["Lantern Harbour"],"format":"MANGA","status":"FINISHED","startDate":{"year":2015},"volumes":37,"countryOfOrigin":"JP","isAdult":false,"description":"A ferry &amp; a keeper.<br>~!Spoiler!~","coverImage":{"large":"https://s4.anilist.co/x.jpg"},"siteUrl":"https://anilist.co/manga/101",
  "staff":{"edges":[{"role":"Story & Art","node":{"name":{"full":"Ren Aoyagi"}}},{"role":"Assistant","node":{"name":{"full":"Nobody"}}}]},
  "externalLinks":[{"site":"Twitter","url":"https://example.test/t","type":"SOCIAL","language":"Japanese"},{"site":"Viz","url":"https://example.test/viz","type":"INFO","language":"English"}]},
 {"id":102,"title":{"romaji":"Saebyeok-ui Tap","english":null,"native":null},"synonyms":[],"format":"MANGA","status":"RELEASING","startDate":{"year":2020},"volumes":null,"countryOfOrigin":"KR","isAdult":false,"description":null,"coverImage":{"large":""},"siteUrl":"","staff":{"edges":[]},"externalLinks":[{"site":"Viz","url":"https://example.test/fr","type":"INFO","language":"French"}]},
 {"id":103,"title":{"romaji":"Hanashi","english":"A Novel Thing","native":null},"synonyms":[],"format":"NOVEL","status":"FINISHED","startDate":{"year":2001},"volumes":3,"countryOfOrigin":"JP","isAdult":true,"description":"","coverImage":{"large":""},"siteUrl":"","staff":{"edges":[]},"externalLinks":[]}
]}}}`

func TestAniListSearchReadsAndKeeps(t *testing.T) {
	var asked atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		var body struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if !strings.Contains(body.Query, "type: MANGA") {
			t.Errorf("asked %s", body.Query)
		}
		_, _ = w.Write([]byte(sampleAnswer))
	}))
	defer srv.Close()
	a := NewAniList("test", srv.URL)
	a.Gap = 0
	list, err := a.Search(context.Background(), "harbor")
	if err != nil || len(list) != 3 {
		t.Fatalf("%v, %+v", err, list)
	}
	f := list[0]
	if f.Title != "Lanternfish Harbor" || f.Format != "manga" || f.Status != "finished" || f.Volumes != 37 || f.StartYear != 2015 {
		t.Errorf("%+v", f)
	}
	if f.Author != "Ren Aoyagi" {
		t.Errorf("author %q", f.Author)
	}
	if f.LicensedEN != 1 || f.Publisher != "VIZ" || f.BuyURL == "" {
		t.Errorf("licence %+v", f)
	}
	if f.Description != "A ferry & a keeper." {
		t.Errorf("description %q", f.Description)
	}
	if len(f.Aliases) != 3 || f.Aliases[0].Alias != "Chochin Minato" || f.Aliases[0].Kind != "romaji" {
		t.Errorf("aliases %+v", f.Aliases)
	}
	// No English title: romaji. A French link doesn't make it licensed in English.
	if g := list[1]; g.Title != "Saebyeok-ui Tap" || g.Format != "manhwa" || g.LicensedEN != -1 || g.Publisher != "" {
		t.Errorf("%+v", g)
	}
	if n := list[2]; !n.IsNovel || !n.IsAdult {
		t.Errorf("%+v", n)
	}
	if _, err := a.Search(context.Background(), "  HARBOR "); err != nil {
		t.Fatal(err)
	}
	if asked.Load() != 1 {
		t.Errorf("asked %d times, the second should come from what's kept", asked.Load())
	}
}

func TestAniListWaitsBetweenRequests(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"Page":{"media":[]}}}`))
	}))
	defer srv.Close()
	a := NewAniList("test", srv.URL)
	a.Gap = 60 * time.Millisecond
	start := time.Now()
	for _, q := range []string{"a1", "a2", "a3"} {
		if _, err := a.Search(context.Background(), q); err != nil {
			t.Fatal(err)
		}
	}
	if d := time.Since(start); d < 100*time.Millisecond {
		t.Errorf("three requests in %v", d)
	}
}

func TestAniListSlowDown(t *testing.T) {
	var asked atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	a := NewAniList("test", srv.URL)
	a.Gap = 0
	if _, err := a.Search(context.Background(), "x"); !errors.Is(err, ErrAniListBusy) {
		t.Fatalf("%v", err)
	}
	if _, err := a.Search(context.Background(), "y"); !errors.Is(err, ErrAniListBusy) {
		t.Fatalf("%v", err)
	}
	if asked.Load() != 1 {
		t.Errorf("asked %d times while told to wait", asked.Load())
	}
}

func TestBackoff(t *testing.T) {
	want := []time.Duration{24 * time.Hour, 3 * 24 * time.Hour, 7 * 24 * time.Hour, 30 * 24 * time.Hour, 30 * 24 * time.Hour}
	for i, w := range want {
		if got := BackoffAfter(i + 1); got != w {
			t.Errorf("%d empties: %v", i+1, got)
		}
	}
}
