package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
)

// The lookup term arrives already decoded once by the query parser. A title
// with a plus or a percent sign in it must reach TMDB exactly as typed.
func TestArrLookupTermIsDecodedOnce(t *testing.T) {
	var mu sync.Mutex
	var got []string
	mux := http.NewServeMux()
	seen := func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = append(got, r.URL.Query().Get("query"))
		mu.Unlock()
		json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
	}
	mux.HandleFunc("/search/movie", seen)
	mux.HandleFunc("/search/tv", seen)
	fake := httptest.NewServer(mux)
	t.Cleanup(fake.Close)

	server, base, admin, _, _ := familyServer(t)
	server.TestSetTMDBBaseURL("fixture-key", fake.URL)
	key := postJSON[map[string]any](t, admin, base+"/api/auth/api-keys", map[string]any{"name": "Overseerr"}, http.StatusCreated)["key"].(string)

	cases := []struct {
		name string
		raw  string // the term as it sits in the URL
		want string // what TMDB should be asked for
	}{
		{"encoded plus", "C%2B%2B%20Primer", "C++ Primer"},
		{"form-encoded space", "a+b", "a b"},
		{"encoded plus with spaces around", "Notting%20Hill%20%2B%20friends", "Notting Hill + friends"},
		{"literal percent", "100%25%20Wolf", "100% Wolf"},
		{"percent after a space", "50%25", "50%"},
		{"unicode", "Am%C3%A9lie", "Amélie"},
		{"unicode unescaped", "Amélie", "Amélie"},
		{"plain", "Night%20Harbour", "Night Harbour"},
		{"encoded colon is not a prefix split", "Spider%3AMan", "Spider:Man"},
	}
	for _, ep := range []string{"/radarr/api/v3/movie/lookup", "/sonarr/api/v3/series/lookup"} {
		for _, c := range cases {
			t.Run(ep+"/"+c.name, func(t *testing.T) {
				mu.Lock()
				got = nil
				mu.Unlock()
				if _, err := url.ParseQuery("term=" + c.raw); err != nil {
					t.Fatalf("bad test input %q: %v", c.raw, err)
				}
				arrCall(t, "GET", base+ep+"?term="+c.raw, key, nil, http.StatusOK)
				mu.Lock()
				defer mu.Unlock()
				if len(got) != 1 || got[0] != c.want {
					t.Fatalf("TMDB was asked for %q, want %q", got, c.want)
				}
			})
		}
	}
}
