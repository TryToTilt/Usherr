package netguard

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// PublicTransport refuses to connect to this machine, whatever name was used.
func TestPublicTransportRefusesLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("the request reached the server")
	}))
	defer srv.Close()
	client := &http.Client{Transport: PublicTransport()}
	_, err := client.Get(srv.URL)
	if err == nil || !errors.Is(err, ErrBlocked) {
		t.Fatalf("got %v, want the connection refused", err)
	}
}
