package api_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// One site failing while the other has nothing must not look like "no
// subtitles": the person is told a site could not be asked.
func TestSearchShowsAFailingSourceInsteadOfNoSubtitles(t *testing.T) {
	env := newTVAutoEnv(t, nil, nil)
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer broken.Close()
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"status":false,"error":"can't find movie or tv"}`)
	}))
	defer empty.Close()
	env.server.TestSetSubtitlesBaseURL("fixture-key", broken.URL)
	env.server.TestSetSubDLBaseURL("subdl-key", empty.URL)

	dir := t.TempDir()
	video := filepath.Join(dir, "Some Movie (2001).mkv")
	os.WriteFile(video, []byte("v"), 0o644)
	m := addDownloadedMovie(t, env, video)

	resp, err := env.client.Get(fmt.Sprintf("%s/api/movies/%d/subtitles?lang=en", env.baseURL, m.ID))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusBadGateway || !strings.Contains(string(body), "OpenSubtitles") {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
}
