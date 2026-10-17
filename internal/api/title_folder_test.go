package api_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/rdborg/mediarium/internal/api"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/settings"
)

type folderPayload struct {
	FolderPath string `json:"folderPath"`
}

// A movie's page says which folder its files are in, or go to: the folder
// the import would create, a look-alike folder somebody made by hand, the
// folder its file really is in, or the folder of the second copies.
func TestMovieFolderPath(t *testing.T) {
	tests := []struct {
		name string
		// folders to create under the movies folder
		folders []string
		// the movie's file, relative to the movies folder ("" = none)
		file string
		// put the movie in this extra folder (relative to the temp dir)
		extra string
		// make the movie a second copy, kept in the copies folder
		copy bool
		// a title that needs cleaning for a folder name
		title string
		want  string // relative to the temp dir
	}{
		{name: "no files yet: the folder an import would create", want: "movies/The Matrix (1999)"},
		{name: "the folder is already there", folders: []string{"The Matrix (1999)"}, want: "movies/The Matrix (1999)"},
		{name: "a look-alike folder made by hand", folders: []string{"the.matrix.1999.1080p"}, want: "movies/the.matrix.1999.1080p"},
		{name: "two look-alike folders are not guessed between", folders: []string{"the.matrix.1999.1080p", "The Matrix 1999 Remastered"}, want: "movies/The Matrix (1999)"},
		{name: "the folder its file really is in", folders: []string{"matrix-oddly-named"}, file: "matrix-oddly-named/matrix.mkv", want: "movies/matrix-oddly-named"},
		{name: "a file straight in the library folder", file: "matrix.mkv", want: "movies"},
		{name: "kept in another folder", extra: "movies4k", want: "movies4k/The Matrix (1999)"},
		{name: "a second copy goes to the copies folder", copy: true, want: "copies/The Matrix (1999)"},
		{name: "characters a folder can't hold are taken out", title: "Who: Framed?", want: "movies/Who Framed (1999)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, base, admin, _, _ := familyServer(t)
			tmp := t.TempDir()
			movies := filepath.Join(tmp, "movies")
			if err := os.MkdirAll(movies, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := server.Settings.Set(settings.KeyMoviesPath, movies, false); err != nil {
				t.Fatal(err)
			}
			for _, f := range tc.folders {
				if err := os.MkdirAll(filepath.Join(movies, f), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if tc.file != "" {
				writeBody(t, filepath.Join(movies, filepath.FromSlash(tc.file)), "video")
			}
			m := library.Movie{TMDBID: 603, Title: "The Matrix", Year: 1999, Monitored: true}
			if tc.title != "" {
				m.Title = tc.title
			}
			if tc.extra != "" {
				extra := filepath.Join(tmp, tc.extra)
				list, _ := json.Marshal([]string{extra})
				if err := server.Settings.Set(settings.KeyMoviesExtraPaths, string(list), false); err != nil {
					t.Fatal(err)
				}
				m.RootPath = extra
			}
			if tc.copy {
				if err := server.Settings.Set(settings.KeyMovieCopiesPath, filepath.Join(tmp, "copies"), false); err != nil {
					t.Fatal(err)
				}
			}
			added, err := server.MovieRepo.Add(m)
			if err != nil {
				t.Fatal(err)
			}
			if tc.copy {
				if added, err = server.MovieRepo.AddCopy(added.ID, firstProfileID(t, server)); err != nil {
					t.Fatal(err)
				}
			}
			if tc.file != "" {
				if err := server.MovieRepo.SetStatus(added.ID, library.StatusDownloaded, "", filepath.Join(movies, filepath.FromSlash(tc.file))); err != nil {
					t.Fatal(err)
				}
			}
			got := getJSON[folderPayload](t, admin, fmt.Sprintf("%s/api/movies/%d", base, added.ID))
			if want := filepath.Join(tmp, filepath.FromSlash(tc.want)); got.FolderPath != want {
				t.Fatalf("folderPath = %q, want %q", got.FolderPath, want)
			}
		})
	}
}

// A show's page names the show folder: the one its episodes are in (the
// folder above the season folders), else the one an import would create.
func TestSeriesFolderPath(t *testing.T) {
	tests := []struct {
		name  string
		files []string // episode files, relative to the TV folder
		extra string
		copy  bool
		want  string // relative to the temp dir
	}{
		{name: "no files yet: the folder an import would create", want: "tv/Fixture Show (2008)"},
		{name: "the show folder its episodes are in", files: []string{"Fixture Show 2008 Complete/Season 1/e01.mkv", "Fixture Show 2008 Complete/Season 1/e02.mkv"}, want: "tv/Fixture Show 2008 Complete"},
		{name: "episodes in the show folder itself", files: []string{"Fixture Show (2008)/e01.mkv"}, want: "tv/Fixture Show (2008)"},
		{name: "episodes dumped in the library folder", files: []string{"e01.mkv", "e02.mkv"}, want: "tv"},
		{name: "kept in another folder", extra: "tv4k", want: "tv4k/Fixture Show (2008)"},
		{name: "a second copy goes to the copies folder", copy: true, want: "tvcopies/Fixture Show (2008)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, base, admin, _, _ := familyServer(t)
			tmp := t.TempDir()
			tv := filepath.Join(tmp, "tv")
			if err := os.MkdirAll(tv, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := server.Settings.Set(settings.KeyTVPath, tv, false); err != nil {
				t.Fatal(err)
			}
			s := library.Series{TMDBID: 900, Title: "Fixture Show", Year: 2008, Monitored: true}
			if tc.extra != "" {
				extra := filepath.Join(tmp, tc.extra)
				list, _ := json.Marshal([]string{extra})
				if err := server.Settings.Set(settings.KeyTVExtraPaths, string(list), false); err != nil {
					t.Fatal(err)
				}
				s.RootPath = extra
			}
			if tc.copy {
				if err := server.Settings.Set(settings.KeyTVCopiesPath, filepath.Join(tmp, "tvcopies"), false); err != nil {
					t.Fatal(err)
				}
			}
			var eps []library.Episode
			for i := range tc.files {
				eps = append(eps, library.Episode{Season: 1, Episode: i + 1, Monitored: true})
			}
			added, err := server.MovieRepo.AddSeries(s, eps)
			if err != nil {
				t.Fatal(err)
			}
			if tc.copy {
				if added, err = server.MovieRepo.AddSeriesCopy(added.ID, firstProfileID(t, server)); err != nil {
					t.Fatal(err)
				}
			}
			for i, f := range tc.files {
				path := filepath.Join(tv, filepath.FromSlash(f))
				writeBody(t, path, "video")
				if err := server.MovieRepo.SetEpisodeStatus(episodeID(t, server.MovieRepo, added.ID, 1, i+1), library.StatusDownloaded, "", path); err != nil {
					t.Fatal(err)
				}
			}
			got := getJSON[folderPayload](t, admin, fmt.Sprintf("%s/api/series/%d", base, added.ID))
			if want := filepath.Join(tmp, filepath.FromSlash(tc.want)); got.FolderPath != want {
				t.Fatalf("folderPath = %q, want %q", got.FolderPath, want)
			}
		})
	}
}

func firstProfileID(t *testing.T, server *api.Server) int64 {
	t.Helper()
	profiles, err := server.QualityRepo.List()
	if err != nil || len(profiles) == 0 {
		t.Fatalf("no quality profile to give the copy: %v", err)
	}
	return profiles[0].ID
}

func episodeID(t *testing.T, repo *library.Repo, seriesID int64, season, episode int) int64 {
	t.Helper()
	ep, err := repo.GetEpisode(seriesID, season, episode)
	if err != nil {
		t.Fatal(err)
	}
	return ep.ID
}
