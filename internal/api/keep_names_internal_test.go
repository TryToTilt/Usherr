package api

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/settings"
	"github.com/rdborg/mediarium/internal/subtitles"
)

// With "Keep the original file names" on, the video keeps the name it came
// with inside the usual title folder; a random (obfuscated) name falls back
// to the naming preset. Off, nothing changes.
func TestKeepOriginalNamesDestPath(t *testing.T) {
	s := newAuditServer(t)
	movies, tv := filepath.Join(t.TempDir(), "movies"), filepath.Join(t.TempDir(), "tv")
	for k, v := range map[string]string{settings.KeyMoviesPath: movies, settings.KeyTVPath: tv, settings.KeyNamingPreset: "plex"} {
		if err := s.Settings.Set(k, v, false); err != nil {
			t.Fatal(err)
		}
	}
	work := filepath.Join(t.TempDir(), "queue-7")
	movieFolder := filepath.Join(movies, "Example Movie (2024)")
	seasonFolder := filepath.Join(tv, "Example Show (2023)", "Season 01")
	series := library.Series{Title: "Example Show", Year: 2023, TMDBID: 1}
	pack := "Example.Show.S01.1080p.WEB-DL-GRP"

	cases := []struct {
		name string
		keep bool
		mode string // illegal character mode
		file string
		tv   bool
		ep   int
		want string
	}{
		{"off: renamed as before", false, "", "Example.Movie.2024.1080p.BluRay-GRP.mkv", false, 0, filepath.Join(movieFolder, "Example Movie (2024).mkv")},
		{"on: a normal file keeps its name", true, "", "Example.Movie.2024.1080p.BluRay-GRP.mkv", false, 0, filepath.Join(movieFolder, "Example.Movie.2024.1080p.BluRay-GRP.mkv")},
		{"on: an obfuscated name falls back", true, "", "a8f7d6e5c4b3.mkv", false, 0, filepath.Join(movieFolder, "Example Movie (2024).mkv")},
		{"on: a very short name falls back", true, "", "abc.mkv", false, 0, filepath.Join(movieFolder, "Example Movie (2024).mkv")},
		{"on: illegal characters removed", true, "", "Example: Movie? 2024.mkv", false, 0, filepath.Join(movieFolder, "Example Movie 2024.mkv")},
		{"on: illegal characters replaced", true, "replace", "Example: Movie? 2024.mkv", false, 0, filepath.Join(movieFolder, "Example- Movie- 2024.mkv")},
		{"off: an episode renamed as before", false, "", "Example.Show.S01E02.1080p.WEB-DL-GRP.mkv", true, 2, filepath.Join(seasonFolder, "Example Show - S01E02 - Two.mkv")},
		{"on: season pack episode 2 keeps its name", true, "", "Example.Show.S01E02.1080p.WEB-DL-GRP.mkv", true, 2, filepath.Join(seasonFolder, "Example.Show.S01E02.1080p.WEB-DL-GRP.mkv")},
		{"on: season pack episode 3 keeps its own name", true, "", "Example.Show.S01E03.1080p.WEB-DL-GRP.mkv", true, 3, filepath.Join(seasonFolder, "Example.Show.S01E03.1080p.WEB-DL-GRP.mkv")},
		{"on: an obfuscated episode falls back", true, "", "wFvDJHhDrbESDBLq.mkv", true, 3, filepath.Join(seasonFolder, "Example Show - S01E03 - Three.mkv")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := setBool(s.Settings, settings.KeyKeepOriginalNames, tc.keep); err != nil {
				t.Fatal(err)
			}
			mode := tc.mode
			if mode == "" {
				mode = "strip"
			}
			if err := s.Settings.Set(settings.KeyIllegalCharMode, mode, false); err != nil {
				t.Fatal(err)
			}
			var got string
			var err error
			if tc.tv {
				title := map[int]string{2: "Two", 3: "Three"}[tc.ep]
				got, err = s.buildTVDestPath(series, library.Episode{Season: 1, Episode: tc.ep, Title: title}, parser.Parse(pack), pack, filepath.Join(work, pack, tc.file))
			} else {
				got, err = s.buildDestPath("", "Example Movie", 2024, 5, "Example.Movie.2024.1080p.BluRay-GRP", filepath.Join(work, tc.file))
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
}

// Subtitles that came with the release are named after the kept video name.
func TestKeepOriginalNamesSideFilesMatch(t *testing.T) {
	s := newAuditServer(t)
	movies := filepath.Join(t.TempDir(), "movies")
	if err := s.Settings.Set(settings.KeyMoviesPath, movies, false); err != nil {
		t.Fatal(err)
	}
	if err := setBool(s.Settings, settings.KeyKeepOriginalNames, true); err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	sub := filepath.Join(work, "Example.Movie.2024.1080p.BluRay-GRP.en.srt")
	if err := os.WriteFile(sub, []byte("1\n00:00:01,000 --> 00:00:02,000\nHello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest, err := s.buildDestPath("", "Example Movie", 2024, 5, "Example.Movie.2024.1080p.BluRay-GRP", filepath.Join(work, "Example.Movie.2024.1080p.BluRay-GRP.mkv"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := subtitles.ImportSidecars(dest, []subtitles.Sidecar{{Lang: "en", Files: []string{sub}}}); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(movies, "Example Movie (2024)", "Example.Movie.2024.1080p.BluRay-GRP.en.srt")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("the subtitle should be named after the kept video name: %v", err)
	}
}

// The rename tool leaves every file alone while names are kept.
func TestKeepOriginalNamesRenamesNothing(t *testing.T) {
	s := newAuditServer(t)
	movies := filepath.Join(t.TempDir(), "movies")
	if err := s.Settings.Set(settings.KeyMoviesPath, movies, false); err != nil {
		t.Fatal(err)
	}
	m, err := s.MovieRepo.Add(library.Movie{TMDBID: 5, Title: "Example Movie", Year: 2024})
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(movies, "Example Movie (2024)", "Example.Movie.2024.1080p.BluRay-GRP.mkv")
	if err := s.MovieRepo.SetStatus(m.ID, library.StatusDownloaded, "Bluray-1080p", file); err != nil {
		t.Fatal(err)
	}
	if plan, err := s.renamePlan(""); err != nil || len(plan) != 1 {
		t.Fatalf("with names not kept the file would be renamed: %v %v", plan, err)
	}
	if err := setBool(s.Settings, settings.KeyKeepOriginalNames, true); err != nil {
		t.Fatal(err)
	}
	if plan, err := s.renamePlan(""); err != nil || len(plan) != 0 {
		t.Fatalf("with names kept nothing is renamed, got %v %v", plan, err)
	}
}
