package mediaservers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Plex's markers become Skip intro and an early Up next: the intro, and
// the first credits (a mid-credits scene's later credits don't count).
func TestPlexMarkers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var c map[string]any
		switch r.URL.Path {
		case "/library/metadata/31":
			if r.URL.Query().Get("includeMarkers") != "1" {
				t.Error("markers weren't asked for")
			}
			c = map[string]any{"Metadata": []map[string]any{{"ratingKey": "31", "Marker": []map[string]any{
				{"type": "intro", "startTimeOffset": 32_000, "endTimeOffset": 92_500},
				{"type": "credits", "startTimeOffset": 3_300_000, "endTimeOffset": 3_400_000},
				{"type": "credits", "startTimeOffset": 3_100_000, "endTimeOffset": 3_250_000},
				{"type": "commercial", "startTimeOffset": 600_000, "endTimeOffset": 660_000},
			}}}}
		case "/library/metadata/77/allLeaves":
			c = map[string]any{"Metadata": []map[string]any{
				{"ratingKey": "21", "parentIndex": 1, "index": 1},
				{"ratingKey": "31", "parentIndex": 2, "index": 1},
			}}
		default:
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"MediaContainer": c})
	}))
	defer srv.Close()
	c := NewClient("test")
	s := Server{ID: 5, Name: "Plex", Kind: KindPlex, BaseURL: srv.URL, Token: "tok", Enabled: true}
	ctx := context.Background()

	key, ok := c.PlexEpisodeKey(ctx, s, "77", 2, 1)
	if !ok || key != "31" {
		t.Fatalf("S2E1 is %q, %v", key, ok)
	}
	if _, ok := c.PlexEpisodeKey(ctx, s, "77", 3, 1); ok {
		t.Error("an episode Plex doesn't have was found")
	}
	m, err := c.PlexMarkers(ctx, s, key)
	if err != nil || m.IntroStart != 32 || m.IntroEnd != 92.5 || m.CreditsStart != 3100 {
		t.Fatalf("markers: %+v, %v", m, err)
	}
}
