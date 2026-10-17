package api_test

import (
	"net/http"
	"testing"
)

func TestCopiesOfATitleThatDoesNotExistIsNotFound(t *testing.T) {
	_, base, client := loginNewServer(t)
	for _, path := range []string{"/api/movies/987/copies", "/api/series/987/copies"} {
		if code, _ := doStatus(t, client, http.MethodGet, base+path); code != http.StatusNotFound {
			t.Errorf("GET %s answered %d, want 404", path, code)
		}
	}
}
