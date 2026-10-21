package api_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
)

// Files named the TRaSH way carry their quality in brackets. A title whose
// stored quality is empty ("Not stated") is judged by that name, and a file
// whose quality cannot be told at all is never taken for something to
// upgrade. The release the fixture downloads is WEBDL-1080p.
func TestUpgradeJudgesAnUnstatedFileByItsTrashName(t *testing.T) {
	const imdb = " {imdb-tt0133093} {tmdb-603} "
	tests := []struct {
		name     string
		oldName  string
		replaced bool
	}{
		{"a better file with a TRaSH name stays", "The Fixture Movie (1999)" + imdb + "[Bluray-1080p][DTS 5.1][x264]-GROUP.mkv", false},
		{"a much better remux stays", "The Fixture Movie (1999)" + imdb + "[Remux-2160p][DV HDR10][TrueHD Atmos 7.1][h265]-GROUP.mkv", false},
		{"a worse file with a TRaSH name is replaced", "The Fixture Movie (1999)" + imdb + "[HDTV-720p][AAC 2.0][x264]-GROUP.mkv", true},
		{"a file whose quality cannot be told stays", "movie.mkv", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newControlEnv(t, -1)
			oldPath := filepath.Join(e.movies, "The Fixture Movie (1999)", tc.oldName)
			putFile(t, oldPath, "the old version")
			if err := e.server.MovieRepo.SetStatus(e.movie.ID, library.StatusDownloaded, "", oldPath); err != nil {
				t.Fatal(err)
			}
			id := e.grab(t)
			waitFor(t, "the download to end", func() bool { s := e.status(t, id); return s == "completed" || s == "failed" })
			if got := e.status(t, id); got != "completed" {
				t.Fatalf("the download ended %s: %v", got, e.item(t, id)["error"])
			}
			newPath := filepath.Join(e.movies, "The Fixture Movie (1999)", "The Fixture Movie (1999).mkv")
			if tc.replaced {
				if exists(oldPath) || !exists(newPath) {
					t.Errorf("the old file should have been replaced: %s", tree(t, e.movies))
				}
				return
			}
			if !exists(oldPath) || exists(newPath) {
				t.Errorf("the old file should stay, with no second copy: %s", tree(t, e.movies))
			}
			if m, _ := e.server.MovieRepo.Get(e.movie.ID); m.FilePath != oldPath {
				t.Errorf("the movie points at %s, want %s", m.FilePath, oldPath)
			}
		})
	}
}

// The replaced file goes to the recycle bin together with the files named
// after it (subtitles, the info file), not away for good, and a file that
// belongs to something else in the folder is not touched.
func TestReplacedFileGoesToTheRecycleBinWithItsSidecars(t *testing.T) {
	e := newControlEnv(t, -1)
	putSettings(t, e, map[string]any{"importConflictPolicy": "overwrite"})
	dir := filepath.Join(e.movies, "The Fixture Movie (1999)")
	oldPath := filepath.Join(dir, "The Fixture Movie (1999) [HDTV-720p]-OLD.mkv")
	sub := filepath.Join(dir, "The Fixture Movie (1999) [HDTV-720p]-OLD.en.srt")
	stranger := filepath.Join(dir, "notes.txt")
	putFile(t, oldPath, "the old version")
	putFile(t, sub, "1\n00:00:01,000 --> 00:00:02,000\nhello\n")
	putFile(t, stranger, "mine")
	if err := e.server.MovieRepo.SetStatus(e.movie.ID, library.StatusDownloaded, "HDTV-720p", oldPath); err != nil {
		t.Fatal(err)
	}
	id := e.grab(t)
	waitFor(t, "the download to end", func() bool { s := e.status(t, id); return s == "completed" || s == "failed" })
	if got := e.status(t, id); got != "completed" {
		t.Fatalf("the download ended %s: %v", got, e.item(t, id)["error"])
	}
	if exists(oldPath) || exists(sub) {
		t.Errorf("the old file and its subtitle should have left the folder: %s", tree(t, e.movies))
	}
	if !exists(stranger) {
		t.Error("a file that is not the old video's was removed")
	}
	bin := 0
	_ = filepath.Walk(filepath.Join(e.movies, ".mediarium-trash"), func(p string, info os.FileInfo, err error) error {
		if err == nil && info.Mode().IsRegular() && (strings.HasSuffix(p, "-OLD.mkv") || strings.HasSuffix(p, "-OLD.en.srt")) {
			bin++
		}
		return nil
	})
	if bin != 2 {
		t.Errorf("the replaced video and subtitle should be in the recycle bin, found %d", bin)
	}
}

// An episode release that is not better than the file under another name is
// turned away instead of landing next to it.
func TestEpisodeThatIsNotBetterIsNotImportedNextToTheFile(t *testing.T) {
	content := []byte("episode two bytes " + strings.Repeat("x", 500))
	env := newTVEnv(t, map[string]nntpArticle{"ep-two@example": {fileName: "release-file.mkv", content: content}})

	season := filepath.Join(env.tvRoot, "Fixture Show (2011)", "Season 01")
	oldE2 := filepath.Join(season, "Fixture Show (2011) - S01E02 - Second [Bluray-2160p][DTS-HD MA 5.1][x265]-GRP.mkv")
	putFile(t, oldE2, "old e2")
	e2, err := env.server.MovieRepo.GetEpisode(env.seriesID, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := env.server.MovieRepo.SetEpisodeStatus(e2.ID, library.StatusDownloaded, "Bluray-2160p", oldE2); err != nil {
		t.Fatal(err)
	}

	env.grab(t, "Fixture.Show.S01E02.1080p.WEB-DL.x264-GRP")
	if status, errMsg := env.waitForTerminal(t); status != "completed" {
		t.Fatalf("expected the TV grab to end as a kept file, got status=%q error=%q", status, errMsg)
	}
	if got := tree(t, season); strings.Count(got, "S01E02") != 1 || !exists(oldE2) {
		t.Errorf("only the better file should be in the season folder, got %s", got)
	}
	if ep, _ := env.server.MovieRepo.GetEpisode(env.seriesID, 1, 2); ep.FilePath != oldE2 || ep.Quality != "Bluray-2160p" {
		t.Errorf("the episode should keep its file, got %s (%s)", ep.FilePath, ep.Quality)
	}
}
