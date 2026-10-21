package api_test

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/settings"
)

// TheTVDB counts One Piece in 23 seasons, the movie database in one long one.
// A file named by the TheTVDB season is placed by the number counted from the
// first episode that its name carries.
func TestManualImportOfAnAnimeFileNamedByTheTVDBSeason(t *testing.T) {
	tests := []struct {
		name       string
		seriesType string
		file       string
		season     int
		episode    int
		wantStatus int
		wantEp     int    // the episode of season 1 that must hold the file
		wantInErr  string // part of the message the person reads
	}{
		{"an anime, S23E10 with the absolute number", library.SeriesAnime, "One Piece - S23E10 - 1100 - Title.mkv", 23, 10, http.StatusOK, 1100, ""},
		{"an anime, a bare absolute number", library.SeriesAnime, "[Group] One Piece - 1100 [1080p].mkv", 23, 10, http.StatusOK, 1100, ""},
		{"an anime, no absolute number in the name", library.SeriesAnime, "One Piece - S23E10 - Title.mkv", 23, 10, http.StatusNotFound, 0, "pick the season and episode shown on the show's page"},
		{"an anime, a number past the last episode", library.SeriesAnime, "One Piece - S24E10 - 1500 - Title.mkv", 24, 10, http.StatusNotFound, 0, "has no season 24 episode 10"},
		{"a standard show is never placed by a number", library.SeriesStandard, "One Piece - S23E10 - 1100 - Title.mkv", 23, 10, http.StatusNotFound, 0, "has no season 23 episode 10"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, base, client := loginNewServer(t)
			downloads := filepath.Dir(server.TestWorkDir())
			src := filepath.Join(downloads, "complete", tc.file)
			if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(src, make([]byte, 21<<20), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(downloads, "tv"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := server.Settings.Set(settings.KeyTVPath, filepath.Join(downloads, "tv"), false); err != nil {
				t.Fatal(err)
			}
			var list []library.Episode
			for n := 1; n <= 1180; n++ {
				list = append(list, library.Episode{Season: 1, Episode: n, Title: fmt.Sprintf("Episode %d", n), AirDate: "2000-01-01"})
			}
			sr, err := server.MovieRepo.AddSeries(library.Series{TMDBID: 37854, Title: "One Piece", Year: 1999, Monitored: true, SeriesType: tc.seriesType}, list)
			if err != nil {
				t.Fatal(err)
			}
			code, body := doJSONStatus(t, client, http.MethodPost, base+"/api/manual-import", map[string]any{
				"path": "complete/" + tc.file, "seriesId": sr.ID, "season": tc.season, "episode": tc.episode,
			})
			if code != tc.wantStatus {
				t.Fatalf("status %d (%v), want %d", code, body, tc.wantStatus)
			}
			if tc.wantStatus != http.StatusOK {
				if msg, _ := body["error"].(string); !strings.Contains(msg, tc.wantInErr) {
					t.Fatalf("message %q should mention %q", msg, tc.wantInErr)
				}
				return
			}
			ep, err := server.MovieRepo.GetEpisode(sr.ID, 1, tc.wantEp)
			if err != nil || ep.Status != library.StatusDownloaded || ep.FilePath == "" {
				t.Fatalf("episode %d after import: %+v (err %v)", tc.wantEp, ep, err)
			}
		})
	}
}

// Sonarr's One Piece S22E41 has no absolute number in its name. The movie
// database numbers that season 1089 to 1155, so the 41st of them is 1129.
func TestManualImportOfAnAnimeFileCountedFromTheSeasonsFirstEpisode(t *testing.T) {
	tests := []struct {
		name       string
		seriesType string
		season     int
		episode    int
		wantStatus int
		wantEp     int
	}{
		{"S22E41 is episode 1129", library.SeriesAnime, 22, 41, http.StatusOK, 1129},
		{"the first of the season", library.SeriesAnime, 22, 1, http.StatusOK, 1089},
		{"past the end of the season", library.SeriesAnime, 22, 68, http.StatusNotFound, 0},
		{"a standard show is not placed this way", library.SeriesStandard, 22, 41, http.StatusNotFound, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, base, client := loginNewServer(t)
			downloads := filepath.Dir(server.TestWorkDir())
			file := "One Piece - S22E41 - Looming Despair! Admiral Kizaru's Depressing Mission.mkv"
			src := filepath.Join(downloads, "complete", file)
			if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(src, make([]byte, 21<<20), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(downloads, "tv"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := server.Settings.Set(settings.KeyTVPath, filepath.Join(downloads, "tv"), false); err != nil {
				t.Fatal(err)
			}
			var list []library.Episode
			for n := 1089; n <= 1155; n++ {
				list = append(list, library.Episode{Season: 22, Episode: n, Title: fmt.Sprintf("Episode %d", n), AirDate: "2024-01-07"})
			}
			sr, err := server.MovieRepo.AddSeries(library.Series{TMDBID: 37854, Title: "One Piece", Year: 1999, Monitored: true, SeriesType: tc.seriesType}, list)
			if err != nil {
				t.Fatal(err)
			}
			code, body := doJSONStatus(t, client, http.MethodPost, base+"/api/manual-import", map[string]any{
				"path": "complete/" + file, "seriesId": sr.ID, "season": tc.season, "episode": tc.episode,
			})
			if code != tc.wantStatus {
				t.Fatalf("status %d (%v), want %d", code, body, tc.wantStatus)
			}
			if tc.wantStatus != http.StatusOK {
				return
			}
			ep, err := server.MovieRepo.GetEpisode(sr.ID, 22, tc.wantEp)
			if err != nil || ep.Status != library.StatusDownloaded || ep.FilePath == "" {
				t.Fatalf("episode %d after import: %+v (err %v)", tc.wantEp, ep, err)
			}
		})
	}
}
