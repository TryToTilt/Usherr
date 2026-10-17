package api_test

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/queue"
)

func (e *locateEnv) addShow(t *testing.T, tmdb int, title string, anime bool, numbers ...[2]int) (library.Series, []library.Episode) {
	t.Helper()
	var eps []library.Episode
	for _, n := range numbers {
		eps = append(eps, library.Episode{Season: n[0], Episode: n[1], AirDate: "2020-01-01"})
	}
	series := library.Series{TMDBID: tmdb, Title: title, Year: 2020, Monitored: true}
	if anime {
		series.SeriesType = library.SeriesAnime
	}
	sr, err := e.server.MovieRepo.AddSeries(series, eps)
	if err != nil {
		t.Fatal(err)
	}
	listed, err := e.server.MovieRepo.ListEpisodes(sr.ID)
	if err != nil {
		t.Fatal(err)
	}
	return sr, listed
}

// Pointing a show at a folder never takes a file that already belongs to a
// movie or to an episode of another show.
func TestChangeSeriesFolderSkipsFilesOfOtherTitles(t *testing.T) {
	e := newLocateEnv(t)
	sr, _ := e.addShow(t, 99, "Show", false, [2]int{1, 1}, [2]int{1, 2}, [2]int{1, 3})
	other, otherEps := e.addShow(t, 100, "Other", false, [2]int{1, 1})
	_ = other

	dir := filepath.Join(e.tv, "Picked")
	otherFile := filepath.Join(dir, "Other.S01E01.mkv")
	movieFile := filepath.Join(dir, "Show.S01E02.mkv")
	ownFile := filepath.Join(dir, "Show.S01E03.mkv")
	for _, p := range []string{otherFile, movieFile, ownFile} {
		writeBody(t, p, "video")
	}
	if err := e.server.MovieRepo.SetEpisodeStatus(otherEps[0].ID, library.StatusDownloaded, "", otherFile); err != nil {
		t.Fatal(err)
	}
	m := e.addMovie(t, 603, "The Matrix")
	if err := e.server.MovieRepo.SetStatus(m.ID, library.StatusDownloaded, "", movieFile); err != nil {
		t.Fatal(err)
	}

	url := fmt.Sprintf("%s/api/series/%d/change-folder", e.base, sr.ID)
	got := postJSON[map[string]any](t, e.admin, url, map[string]any{"path": dir}, http.StatusOK)
	if got["found"] != float64(1) {
		t.Fatalf("report = %v, want only the show's own file found", got)
	}
	if got["notPlaced"] != float64(2) {
		t.Fatalf("report = %v, want the two files of other titles left out", got)
	}
	eps, _ := e.server.MovieRepo.ListEpisodes(sr.ID)
	for _, ep := range eps {
		want := ""
		if ep.Episode == 3 {
			want = ownFile
		}
		if ep.FilePath != want {
			t.Errorf("episode %d has %q, want %q", ep.Episode, ep.FilePath, want)
		}
	}
	// the other title keeps its file
	theirs, _ := e.server.MovieRepo.ListEpisodes(other.ID)
	if theirs[0].FilePath != otherFile {
		t.Errorf("the other show lost its file: %q", theirs[0].FilePath)
	}
}

// A movie's folder is not pointed at a file that is an episode of a show.
func TestLocateMovieFolderSkipsEpisodeFiles(t *testing.T) {
	e := newLocateEnv(t)
	m := e.addMovie(t, 603, "The Matrix")
	_, eps := e.addShow(t, 99, "Show", false, [2]int{1, 1})
	dir := filepath.Join(e.movies, "Mixed")
	episodeFile := filepath.Join(dir, "show.s01e01.big.mkv")
	free := filepath.Join(dir, "matrix.mkv")
	writeBody(t, episodeFile, strings.Repeat("x", 100))
	writeBody(t, free, "small")
	if err := e.server.MovieRepo.SetEpisodeStatus(eps[0].ID, library.StatusDownloaded, "", episodeFile); err != nil {
		t.Fatal(err)
	}
	url := fmt.Sprintf("%s/api/movies/%d/locate-folder", e.base, m.ID)
	got := postJSON[map[string]any](t, e.admin, url, map[string]any{"path": dir}, http.StatusOK)
	if got["added"] != "matrix.mkv" {
		t.Fatalf("report = %v, want the file nobody has", got)
	}
}

func TestChangeSeriesFolderRefusedWhileTheShowDownloads(t *testing.T) {
	e := newLocateEnv(t)
	sr, eps := e.addShow(t, 99, "Show", false, [2]int{1, 1})
	dir := filepath.Join(e.tv, "Picked")
	writeBody(t, filepath.Join(dir, "Show.S01E01.mkv"), "video")
	if _, err := e.server.QueueRepo.Enqueue(queue.Item{SeriesID: sr.ID, Season: 1, Episode: 1, ReleaseTitle: "Show.S01E01.1080p.WEB-DL-GRP"}); err != nil {
		t.Fatal(err)
	}
	url := fmt.Sprintf("%s/api/series/%d/change-folder", e.base, sr.ID)
	postJSON[map[string]any](t, e.admin, url, map[string]any{"path": dir}, http.StatusConflict)
	after, _ := e.server.MovieRepo.ListEpisodes(sr.ID)
	if after[0].ID != eps[0].ID || after[0].FilePath != "" {
		t.Fatalf("a refused change touched the episode: %+v", after[0])
	}
}

// When two files in the folder are the same episode, the bigger one is used,
// whichever the scan met first.
func TestChangeSeriesFolderTakesTheBiggerFileForOneEpisode(t *testing.T) {
	for _, tc := range []struct {
		name         string
		numberedSize int
		absoluteSize int
		want         string
	}{
		{"the absolute-numbered file is bigger", 10, 50, "[Group] Show - 02.mkv"},
		{"the season-numbered file is bigger", 50, 10, "Show.S01E02.mkv"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newLocateEnv(t)
			sr, _ := e.addShow(t, 99, "Show", true, [2]int{1, 1}, [2]int{1, 2})
			dir := filepath.Join(e.tv, "Picked")
			writeBody(t, filepath.Join(dir, "Show.S01E02.mkv"), strings.Repeat("a", tc.numberedSize))
			writeBody(t, filepath.Join(dir, "[Group] Show - 02.mkv"), strings.Repeat("b", tc.absoluteSize))
			url := fmt.Sprintf("%s/api/series/%d/change-folder", e.base, sr.ID)
			got := postJSON[map[string]any](t, e.admin, url, map[string]any{"path": dir}, http.StatusOK)
			if got["found"] != float64(1) || got["notPlaced"] != float64(1) {
				t.Fatalf("report = %v, want one episode found and the other file not placed", got)
			}
			eps, _ := e.server.MovieRepo.ListEpisodes(sr.ID)
			if eps[1].FilePath != filepath.Join(dir, tc.want) {
				t.Fatalf("episode 2 has %q, want %q", eps[1].FilePath, tc.want)
			}
		})
	}
}

// A link inside a title's folder that leads out of the library is not used.
func TestFolderScansIgnoreLinksOutOfTheLibrary(t *testing.T) {
	e := newLocateEnv(t)
	target := filepath.Join(e.outside, "secret.mkv")
	writeBody(t, target, strings.Repeat("x", 500))
	epTarget := filepath.Join(e.outside, "Show.S01E01.mkv")
	writeBody(t, epTarget, "video")

	// a movie's folder: the link is the biggest file, and is skipped
	m := e.addMovie(t, 603, "The Matrix")
	movieDir := filepath.Join(e.movies, "Matrix")
	writeBody(t, filepath.Join(movieDir, "matrix.mkv"), "ok")
	if err := os.Symlink(target, filepath.Join(movieDir, "bigger.mkv")); err != nil {
		t.Skip("symlinks are not available here")
	}
	got := postJSON[map[string]any](t, e.admin, fmt.Sprintf("%s/api/movies/%d/locate-folder", e.base, m.ID), map[string]any{"path": movieDir}, http.StatusOK)
	if got["added"] != "matrix.mkv" {
		t.Fatalf("report = %v, want the real file and not the link", got)
	}
	after, _ := e.server.MovieRepo.Get(m.ID)
	if after.FilePath != filepath.Join(movieDir, "matrix.mkv") {
		t.Fatalf("movie file = %q", after.FilePath)
	}

	// a show's folder: an episode that is only a link out has no file
	sr, _ := e.addShow(t, 99, "Show", false, [2]int{1, 1})
	showDir := filepath.Join(e.tv, "Picked")
	if err := os.MkdirAll(showDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(epTarget, filepath.Join(showDir, "Show.S01E01.mkv")); err != nil {
		t.Skip("symlinks are not available here")
	}
	rep := postJSON[map[string]any](t, e.admin, fmt.Sprintf("%s/api/series/%d/change-folder", e.base, sr.ID), map[string]any{"path": showDir}, http.StatusOK)
	if rep["found"] != float64(0) {
		t.Fatalf("report = %v, want nothing found", rep)
	}
	eps, _ := e.server.MovieRepo.ListEpisodes(sr.ID)
	if eps[0].FilePath != "" {
		t.Fatalf("the episode took a file outside the library: %q", eps[0].FilePath)
	}
}
