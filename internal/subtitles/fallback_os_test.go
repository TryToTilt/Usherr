package subtitles_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/subtitles"
)

func osAnswer(n int, page, pages int) string {
	var items []string
	for i := 0; i < n; i++ {
		items = append(items, fmt.Sprintf(`{"attributes":{"language":"en","release":"R%d-%d","files":[{"file_id":%d}]}}`, page, i, page*100+i+1))
	}
	return fmt.Sprintf(`{"total_pages":%d,"page":%d,"data":[%s]}`, pages, page, strings.Join(items, ","))
}

// OpenSubtitles doesn't know the TMDB id of every title; the IMDb id and the
// name must be tried before giving up.
func TestOpenSubtitlesFallsBack(t *testing.T) {
	tests := []struct {
		name string
		hit  string // the lookup that finds something
		want []string
	}{
		{"tmdb", "tmdb_id", []string{"tmdb_id"}},
		{"imdb", "imdb_id", []string{"tmdb_id", "imdb_id"}},
		{"name", "query", []string{"tmdb_id", "imdb_id", "query"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var seen []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				q := r.URL.Query()
				for _, k := range []string{"tmdb_id", "imdb_id", "query"} {
					if q.Get(k) != "" {
						seen = append(seen, k)
						if k == "query" && (q.Get("query") != "The Sixth Sense" || q.Get("year") != "1999") {
							t.Errorf("name search: %s", q.Encode())
						}
						if k == tc.hit {
							fmt.Fprint(w, osAnswer(2, 1, 1))
							return
						}
					}
				}
				fmt.Fprint(w, `{"total_pages":0,"data":[]}`)
			}))
			defer srv.Close()
			c := subtitles.NewWithBaseURL("k", srv.URL)
			res, err := c.Find(context.Background(), subtitles.Query{TMDBID: 745, IMDBID: "tt0167404", Title: "The Sixth Sense", Year: 1999, Type: "movie", Language: "en"})
			if err != nil || len(res) != 2 {
				t.Fatalf("%v %v", res, err)
			}
			if strings.Join(seen, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("lookups %v, want %v", seen, tc.want)
			}
		})
	}
}

func TestOpenSubtitlesPages(t *testing.T) {
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Query().Get("page")
		asked = append(asked, p)
		switch p {
		case "", "1":
			fmt.Fprint(w, osAnswer(2, 1, 9))
		case "2":
			fmt.Fprint(w, osAnswer(2, 2, 9))
		default:
			fmt.Fprint(w, osAnswer(2, 3, 9))
		}
	}))
	defer srv.Close()
	res, err := subtitles.NewWithBaseURL("k", srv.URL).Find(context.Background(), subtitles.Query{TMDBID: 1, Type: "movie", Language: "en"})
	if err != nil || len(res) != 6 || len(asked) != 3 {
		t.Fatalf("%d results, pages %v, %v", len(res), asked, err)
	}
}
