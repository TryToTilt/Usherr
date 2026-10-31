package mediaservers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// Signing in with Plex tries the nearby address (host.docker.internal for a
// Plex on the same computer) before the ones plex.tv lists, which change
// with the computer's network address; but only when the server answering
// there is the same one, so its token never goes to another.
func TestConnectPlexPrefersNearby(t *testing.T) {
	plex := newFakePlex(t, "srv-token")
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()
	r := PlexResource{Name: "JellyPlex", ClientIdentifier: plex.machine, AccessToken: "srv-token", Connections: []PlexConnection{
		{Protocol: "https", URI: deadURL, Local: true},
	}}
	c := NewClient("test")
	c.NearbyPlex = []string{plex.srv.URL}
	s, _, err := c.ConnectPlex(context.Background(), r, "")
	if err != nil || s.BaseURL != plex.srv.URL {
		t.Fatalf("connected at %q (%v); want the nearby address", s.BaseURL, err)
	}

	// Another server answering nearby: not used, and never sent the token.
	var tokens atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("X-Plex-Token") != "" {
			tokens.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"MediaContainer":{"machineIdentifier":"someone-else","version":"1.0"}}`))
	}))
	defer other.Close()
	r.Connections = append(r.Connections, PlexConnection{Protocol: "http", URI: plex.srv.URL, Local: true})
	c.NearbyPlex = []string{other.URL}
	s, _, err = c.ConnectPlex(context.Background(), r, "")
	if err != nil || s.BaseURL != plex.srv.URL {
		t.Fatalf("connected at %q (%v); want plex.tv's address", s.BaseURL, err)
	}
	if tokens.Load() != 0 {
		t.Error("the token was sent to a different server")
	}
}
