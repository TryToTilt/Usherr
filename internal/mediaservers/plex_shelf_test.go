package mediaservers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// What a Plex server has to watch: its films and shows (adult libraries
// left out), the owner's On Deck, and a show's next episode.
func TestPlexShelfOnDeckAndNext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var c map[string]any
		switch r.URL.Path {
		case "/library/sections":
			c = map[string]any{"Directory": []map[string]any{
				{"key": "1", "type": "movie", "title": "Movies-Stream", "Location": []map[string]any{{"path": `U:\Movies`}}},
				{"key": "2", "type": "show", "title": "TV Shows"},
				{"key": "3", "type": "movie", "title": "Adult 18+"},
				{"key": "4", "type": "artist", "title": "Music"},
			}}
		case "/library/sections/1/all":
			c = map[string]any{"Metadata": []map[string]any{
				{"ratingKey": "10", "type": "movie", "title": "Nobody 2", "year": 2025, "audienceRating": 6.8, "contentRating": "R",
					"Genre": []map[string]any{{"tag": "Action"}}, "Guid": []map[string]any{{"id": "tmdb://1007734"}},
					"Media": []map[string]any{{"Part": []map[string]any{{"file": `U:\Movies\Nobody 2 (2025) [tmdbid-1007734]\Nobody 2 (2025).mkv`}}}}},
				{"ratingKey": "11", "type": "movie", "title": "No TMDB id"},
			}}
		case "/library/sections/2/all":
			c = map[string]any{"Metadata": []map[string]any{
				{"ratingKey": "77", "type": "show", "title": "Severance", "year": 2022, "Guid": []map[string]any{{"id": "tmdb://95396"}}, "Location": []map[string]any{{"path": `G:\TV Shows\Severance`}}},
			}}
		case "/library/sections/3/all":
			t.Error("an adult library was read")
		case "/library/onDeck":
			c = map[string]any{"Metadata": []map[string]any{
				{"ratingKey": "10", "type": "movie", "title": "Nobody 2", "year": 2025, "viewOffset": 60000, "Guid": []map[string]any{{"id": "tmdb://1007734"}}},
				{"ratingKey": "23", "type": "episode", "title": "Half Loop", "grandparentTitle": "Severance", "grandparentRatingKey": "77", "parentIndex": 1, "index": 2},
			}}
		case "/library/metadata/77":
			c = map[string]any{"Metadata": []map[string]any{{"ratingKey": "77", "type": "show", "Guid": []map[string]any{{"id": "tmdb://95396"}}}}}
		case "/library/metadata/77/allLeaves":
			c = map[string]any{"Metadata": []map[string]any{
				{"ratingKey": "20", "type": "episode", "parentIndex": 0, "index": 1, "title": "Special"},
				{"ratingKey": "21", "type": "episode", "parentIndex": 1, "index": 1, "title": "Good News About Hell"},
				{"ratingKey": "23", "type": "episode", "parentIndex": 1, "index": 2, "title": "Half Loop"},
				{"ratingKey": "31", "type": "episode", "parentIndex": 2, "index": 1, "title": "Hello, Ms. Cobel"},
			}}
		default:
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"MediaContainer": c})
	}))
	defer srv.Close()
	c := NewClient("test")
	s := Server{ID: 4, Name: "Plex", Kind: KindPlex, BaseURL: srv.URL, Token: "tok", Enabled: true}
	ctx := context.Background()

	shelf, err := c.PlexShelf(ctx, s)
	if err != nil || len(shelf) != 2 {
		t.Fatalf("shelf: %+v, %v", shelf, err)
	}
	if m := shelf[0]; m.Kind != "movie" || m.TMDBID != 1007734 || m.Rating != 6.8 || len(m.Genres) != 1 || m.File == "" {
		t.Errorf("film: %+v", m)
	}
	if sh := shelf[1]; sh.Kind != "show" || sh.TMDBID != 95396 || sh.File != `G:\TV Shows\Severance` {
		t.Errorf("show: %+v", sh)
	}

	deck, err := c.PlexOnDeck(ctx, s)
	if err != nil || len(deck) != 2 || !deck[0].Started || deck[0].TMDBID != 1007734 || deck[1].TMDBID != 95396 || deck[1].Episode != 2 || deck[1].Started {
		t.Fatalf("on deck: %+v, %v", deck, err)
	}

	for _, tc := range []struct{ season, episode, wantS, wantE int }{{1, 1, 1, 2}, {1, 2, 2, 1}, {0, 0, 1, 1}} {
		next, ok := c.PlexNextEpisode(ctx, s, "77", tc.season, tc.episode)
		if !ok || next.Season != tc.wantS || next.Episode != tc.wantE {
			t.Errorf("after S%dE%d: %+v %v", tc.season, tc.episode, next, ok)
		}
	}
	if _, ok := c.PlexNextEpisode(ctx, s, "77", 2, 1); ok {
		t.Error("there's nothing after the last episode")
	}
}
