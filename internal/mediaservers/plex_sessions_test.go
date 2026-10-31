package mediaservers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Plex's sessions come back in the same shape as Jellyfin's: a 4K Dolby
// Vision film in Atmos played directly, and a transcoded episode whose show
// is looked up once for its TMDB id.
func TestPlexNowPlaying(t *testing.T) {
	showLookups := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Plex-Token") != "tok" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/status/sessions":
			_ = json.NewEncoder(w).Encode(map[string]any{"MediaContainer": map[string]any{"Metadata": []map[string]any{
				{
					"type": "movie", "title": "Nobody 2", "year": 2025, "duration": 5_512_000, "viewOffset": 1_147_000,
					"summary": "Hutch goes on holiday.", "contentRating": "R", "audienceRating": 6.8,
					"Genre": []map[string]any{{"tag": "Action"}}, "Guid": []map[string]any{{"id": "imdb://tt1"}, {"id": "tmdb://1007734"}},
					"Player": map[string]any{"title": "Living Room", "state": "playing"},
					"Media": []map[string]any{{"aspectRatio": 2.39, "Part": []map[string]any{{"Stream": []map[string]any{
						{"streamType": 1, "codec": "hevc", "width": 3840, "height": 1608, "DOVIPresent": true, "displayTitle": "4K DoVi/HDR10 (HEVC Main 10)"},
						{"streamType": 2, "codec": "eac3", "channels": 6, "displayTitle": "English (EAC3 5.1)", "selected": false},
						{"streamType": 2, "codec": "truehd", "channels": 8, "extendedDisplayTitle": "English (TrueHD Atmos 7.1)", "selected": true},
					}}}}},
				},
				{
					"type": "episode", "title": "Good News About Hell", "grandparentTitle": "Severance", "grandparentRatingKey": "77",
					"parentIndex": 1, "index": 1, "duration": 3_420_000, "viewOffset": 0,
					"Player":           map[string]any{"device": "iPhone", "state": "paused"},
					"TranscodeSession": map[string]any{"videoDecision": "transcode", "audioDecision": "copy"},
					"Media": []map[string]any{{"Part": []map[string]any{{"Stream": []map[string]any{
						{"streamType": 1, "codec": "h264", "width": 1920, "height": 1080},
						{"streamType": 2, "codec": "aac", "channels": 2},
					}}}}},
				},
			}}})
		case "/library/metadata/77":
			showLookups++
			_ = json.NewEncoder(w).Encode(map[string]any{"MediaContainer": map[string]any{"Metadata": []map[string]any{
				{"ratingKey": "77", "type": "show", "title": "Severance", "Guid": []map[string]any{{"id": "tmdb://95396"}}},
			}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := NewClient("test")
	s := Server{ID: 9, Name: "Plex", Kind: KindPlex, BaseURL: srv.URL, Token: "tok", Enabled: true}

	for round := 0; round < 2; round++ {
		list, err := c.NowPlaying(context.Background(), s)
		if err != nil || len(list) != 2 {
			t.Fatalf("NowPlaying: %v, %+v", err, list)
		}
		m, e := list[0], list[1]
		if m.Kind != "movie" || m.TMDBID != 1007734 || m.Year != 2025 || m.Paused || m.Device != "Living Room" || m.Runtime != 91 {
			t.Errorf("movie: %+v", m)
		}
		if m.Resolution != "4K" || m.HDR != "Dolby Vision" || m.Audio != "Dolby Atmos" || m.Channels != "7.1" || m.Aspect != "2.39:1" || m.PlayMethod != "DirectPlay" {
			t.Errorf("movie picture and sound: %+v", m)
		}
		if m.Progress < 0.2 || m.Progress > 0.21 || m.Rated != "R" || m.Rating != 6.8 || len(m.Genres) != 1 || m.Overview == "" {
			t.Errorf("movie details: %+v", m)
		}
		if e.Kind != "episode" || e.Series != "Severance" || e.Season != 1 || e.Episode != 1 || !e.Paused || e.Device != "iPhone" || e.PlayMethod != "Transcode" || e.SeriesTMDB != 95396 || e.Resolution != "1080p" || e.Channels != "Stereo" {
			t.Errorf("episode: %+v", e)
		}
	}
	if showLookups != 1 {
		t.Errorf("the show was looked up %d times; once is enough", showLookups)
	}
}
