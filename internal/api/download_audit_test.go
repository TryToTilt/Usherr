package api_test

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
)

// Removing a download that waits on a file conflict hands its movie back: it
// does not stay "downloading" with nothing behind it.
func TestRemovingAConflictDownloadResetsTheMovie(t *testing.T) {
	movieContent := []byte("new fixture movie content, different from what's already there")
	client, httpSrv, movie, _ := newImportConflictTestServer(t, movieContent)

	item := grabFixtureAndWaitForConflict(t, client, httpSrv, movie, movieContent)
	queueID := int64(item["id"].(float64))
	if got := getJSON[map[string]any](t, client, fmt.Sprintf("%s/api/movies/%d", httpSrv.URL, movie.ID)); got["status"] != "downloading" {
		t.Fatalf("a parked download holds its movie as downloading, got %v", got["status"])
	}

	req, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/queue/%d?deleteFiles=1", httpSrv.URL, queueID), nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("removing the entry answered %d", resp.StatusCode)
	}
	if got := getJSON[map[string]any](t, client, fmt.Sprintf("%s/api/movies/%d", httpSrv.URL, movie.ID)); got["status"] != "missing" {
		t.Fatalf("the movie must be handed back after the entry is removed, got %v", got["status"])
	}
}

// A season pack picked while one episode is still missing imports each file to
// its own episode: the missing episode gets its file, not the biggest one.
func TestTVPackWithOneMissingEpisodeImportsEachFileToItsEpisode(t *testing.T) {
	e1 := []byte("episode one, much bigger than the other " + strings.Repeat("a", 3000))
	e2 := []byte("episode two " + strings.Repeat("b", 300))
	env := newTVEnv(t, map[string]nntpArticle{
		"e1@example": {fileName: "Fixture.Show.S01E01.1080p.BluRay.x264-GRP.mkv", content: e1},
		"e2@example": {fileName: "Fixture.Show.S01E02.1080p.BluRay.x264-GRP.mkv", content: e2},
	})
	// Episode one is in the library already.
	have := filepath.Join(env.tvRoot, "have-e01.mkv")
	if err := os.WriteFile(have, []byte("the copy already in the library"), 0o644); err != nil {
		t.Fatal(err)
	}
	one, err := env.server.MovieRepo.GetEpisode(env.seriesID, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := env.server.MovieRepo.SetEpisodeStatus(one.ID, library.StatusDownloaded, "Bluray-1080p", have); err != nil {
		t.Fatal(err)
	}
	postJSONMethod[map[string]any](t, env.client, http.MethodPut, env.httpSrv.URL+"/api/settings", map[string]any{
		"importConflictPolicy": "skip",
	}, http.StatusOK)

	env.grab(t, "Fixture.Show.S01.1080p.BluRay.x264-GRP")
	if status, errMsg := env.waitForTerminal(t); status != "completed" {
		t.Fatalf("expected the pack to complete, got status=%q error=%q", status, errMsg)
	}
	two, err := env.server.MovieRepo.GetEpisode(env.seriesID, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(two.FilePath)
	if err != nil || string(got) != string(e2) {
		t.Fatalf("the missing episode must get its own file, got %q (%v)", got, err)
	}
	if statuses := env.episodeStatuses(t); statuses["S01E02"] != "downloaded" {
		t.Fatalf("expected S01E02 downloaded, got %v", statuses)
	}
}
