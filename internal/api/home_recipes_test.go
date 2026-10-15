package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestRecipeParams(t *testing.T) {
	now := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	f := RecipeFilters{Genres: []int{53}, LastMonths: 12, MinRating: 7.5, Language: "ko", People: []recipeNamed{{ID: 137427, Name: "Denis Villeneuve"}}, Certs: []string{"PG", "PG-13"}, RuntimeMax: 120, Sort: "rating"}
	v := f.tmdbParams("movie", []int{8, 15}, "US", 2, now)
	want := map[string]string{
		"with_genres": "53", "with_original_language": "ko", "primary_release_date.gte": "2025-10-13", "primary_release_date.lte": "2026-10-13",
		"vote_average.gte": "7.5", "vote_count.gte": "100", "with_people": "137427", "certification": "PG|PG-13", "certification_country": "US",
		"with_runtime.lte": "120", "with_watch_providers": "8|15", "watch_region": "US", "sort_by": "vote_average.desc", "page": "2",
	}
	for k, w := range want {
		if got := v.Get(k); got != w {
			t.Errorf("%s = %q, want %q", k, got, w)
		}
	}
	// Shows: no people or age ratings (TMDB has none for shows); anime is Japanese animation.
	tv := RecipeFilters{Anime: true, Genres: []int{10759}, People: []recipeNamed{{ID: 1}}, Certs: []string{"PG"}, YearFrom: 2010, Sort: "newest"}.tmdbParams("tv", nil, "US", 1, now)
	if tv.Get("with_genres") != "16,10759" || tv.Get("with_original_language") != "ja" || tv.Get("with_people") != "" || tv.Get("certification") != "" {
		t.Errorf("tv params: %v", tv)
	}
	if tv.Get("first_air_date.gte") != "2010-01-01" || tv.Get("first_air_date.lte") != "2026-10-13" || tv.Get("sort_by") != "first_air_date.desc" {
		t.Errorf("tv dates/sort: %v", tv)
	}
}

func TestRecipeScoreFilters(t *testing.T) {
	f := RecipeFilters{MinTomatoes: 90, MinIMDb: 7.5, MaxAge: 10}
	if !f.wantsScores() {
		t.Fatal("score filters not seen")
	}
	if !f.passes(titleScores{Tomatoes: 95, IMDb: 8.1, CommonSense: 8}, true) {
		t.Error("a family favourite should pass")
	}
	if f.passes(titleScores{Tomatoes: 95, IMDb: 8.1, CommonSense: 13}, true) {
		t.Error("13+ should not pass an age limit of 10")
	}
	if f.passes(titleScores{Tomatoes: 95, IMDb: 8.1}, true) {
		t.Error("no Common Sense age should not pass an age limit")
	}
	if f.passes(titleScores{}, false) {
		t.Error("unknown scores should not pass")
	}
}

func TestMDBListScores(t *testing.T) {
	raw := `{"ids":{"tmdb":578,"imdb":"tt0073195"},"commonsense":true,"age_rating":13,"ratings":[
	{"source":"imdb","value":8.1,"score":81,"votes":673852,"url":99},
	{"source":"tomatoes","value":97,"score":97,"votes":102,"url":"/m/jaws"},
	{"source":"popcorn","value":90,"score":90},
	{"source":"metacritic","value":87,"score":87},
	{"source":"letterboxd","value":8,"score":80,"url":"/film/jaws/"},
	{"source":"metacriticuser","value":null,"score":null}]}`
	var it mdbItem
	if err := json.Unmarshal([]byte(raw), &it); err != nil {
		t.Fatal(err)
	}
	sc := it.scores()
	if sc.IMDb != 8.1 || sc.IMDbVotes != 673852 || sc.Tomatoes != 97 || sc.Popcorn != 90 || sc.Metacritic != 87 || sc.Letterboxd != 4 || sc.CommonSense != 13 || sc.RTPath != "/m/jaws" || sc.IMDbID != "tt0073195" {
		t.Errorf("scores: %+v", sc)
	}
}

func TestReadyRowFilters(t *testing.T) {
	now := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	gems := RecipeFilters{MinRating: 7.3, MinVotes: 150, MaxVotes: 1500, Sort: "rating"}.tmdbParams("movie", nil, "US", 1, now)
	if gems.Get("vote_count.gte") != "150" || gems.Get("vote_count.lte") != "1500" || gems.Get("sort_by") != "vote_average.desc" {
		t.Fatalf("hidden gems: %v", gems)
	}
	box := RecipeFilters{LastMonths: 12, Sort: "revenue"}.tmdbParams("movie", nil, "US", 1, now)
	if box.Get("sort_by") != "revenue.desc" {
		t.Fatalf("box office: %v", box)
	}
	if tv := (RecipeFilters{Sort: "revenue"}).tmdbParams("tv", nil, "US", 1, now); tv.Get("sort_by") != "vote_count.desc" {
		t.Fatalf("shows have no box office: %v", tv)
	}
	world := RecipeFilters{Language: notEnglish, MinRating: 7.2}.tmdbParams("movie", nil, "US", 1, now)
	if l := world.Get("with_original_language"); !strings.Contains(l, "ko|ja|fr") || strings.Contains(l, "en") {
		t.Fatalf("not English asks for the other languages: %v", world)
	}
	for k, want := range map[string]bool{"movie:top": true, "tv:world": true, "anime:gems": true, "book:top": false, "movie:": false, "movie:Top": false, "movie": false} {
		if validRowKey(k) != want {
			t.Fatalf("validRowKey(%q) = %v", k, !want)
		}
	}
}
