package library_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/library"
)

// onePieceSeasons is One Piece as the movie database lists it around the
// episodes in the issue: a special, season 22 numbered 1089 to 1155 and
// season 23 numbered from 1156, where TheTVDB (and Sonarr) count each season
// from 1.
func onePieceSeasons() []library.Episode {
	eps := []library.Episode{{Season: 0, Episode: 1, Title: "Special"}}
	for n := 1089; n <= 1155; n++ {
		eps = append(eps, library.Episode{Season: 22, Episode: n, Title: "Episode", AirDate: "2024-01-07"})
	}
	for n := 1156; n <= 1165; n++ {
		eps = append(eps, library.Episode{Season: 23, Episode: n, Title: "Episode", AirDate: "2025-04-06"})
	}
	return eps
}

// jujutsuKaisen is the show as the movie database lists it: one season of 59
// episodes, where TheTVDB has seasons of 24, 23 and 12.
func jujutsuKaisen() []library.Episode {
	eps := make([]library.Episode, 0, 59)
	for n := 1; n <= 59; n++ {
		eps = append(eps, library.Episode{Season: 1, Episode: n, Title: "Episode", AirDate: "2020-10-03"})
	}
	return eps
}

// normalShow has two ordinary seasons of 10 episodes.
func normalShow() []library.Episode {
	var eps []library.Episode
	for s := 1; s <= 2; s++ {
		for n := 1; n <= 10; n++ {
			eps = append(eps, library.Episode{Season: s, Episode: n, Title: "Episode", AirDate: "2015-01-01"})
		}
	}
	return eps
}

func importFile(dir string, season, episode int, title string) library.ImportFile {
	name := fmt.Sprintf("Show - S%02dE%02d - %s.mkv", season, episode, title)
	return library.ImportFile{Path: filepath.ToSlash(filepath.Join("/tv", dir, fmt.Sprintf("Season %d", season), name)), Season: season, Episodes: []int{episode}}
}

func seasonFiles(dir string, season, from, to int) []library.ImportFile {
	var out []library.ImportFile
	for n := from; n <= to; n++ {
		out = append(out, importFile(dir, season, n, "Title"))
	}
	return out
}

func TestImportPlacesFilesByTheTVDBSeasonNumbers(t *testing.T) {
	const (
		leftOutOne = "Left out 1 episode that the movie database doesn't list for this show."
		rename     = "Left out %d episodes that the movie database doesn't list for this show. Their numbers don't match the movie database's list, so nothing was guessed. Rename the files to the season and episode shown on the show's page and import the folder again."
		renameOn   = "Left out 1 episode that the movie database doesn't list for this show. Its number doesn't match the movie database's list, so nothing was guessed. Rename the file to the season and episode shown on the show's page and import the folder again."
	)
	onePieceTitles := []string{
		"Looming Despair! Admiral Kizaru's Depressing Mission",
		"Luffy vs. Kizaru! A Fierce Kaleidoscopic Battle",
		"The Nightmare Strikes - Godhead of Science & Defense, St. Saturn",
		"Kuma's Past - Better Off Dead in This World",
		"A History Erased! God Valley of Despair",
		"A Fleeting Moment of Happiness - Kumachi and Ginny",
		"A Pledge to Ginny - Kuma Becomes a Father",
		"To Save His Daughter - Kuma the Timid Pacifist",
		"Cruel Fate - Kuma's Decision as a Father",
		"To the Sea Where My Father Is! The Future Bonney Chooses",
	}
	var onePiece22 []library.ImportFile
	for i, title := range onePieceTitles {
		onePiece22 = append(onePiece22, importFile("One Piece (1999)", 22, 41+i, title))
	}
	jjkAll := append(append(seasonFiles("Jujutsu Kaisen", 1, 1, 24), seasonFiles("Jujutsu Kaisen", 2, 1, 23)...), seasonFiles("Jujutsu Kaisen", 3, 1, 12)...)
	// The anime lists' answer for TheTVDB's seasons 2 and 3 of Jujutsu Kaisen.
	jjkLists := func(season, episode int) (int, int, bool) {
		switch season {
		case 2:
			return 1, 24 + episode, true
		case 3:
			return 1, 47 + episode, true
		}
		return 0, 0, false
	}

	tests := []struct {
		name         string
		episodes     []library.Episode
		anime        bool
		files        []library.ImportFile
		tvdb         func(season, episode int) (int, int, bool)
		wantEpisodes [][2]int // episodes that must now have a file
		wantBySeason int
		wantUnknown  int
		wantNote     string
	}{
		{
			name: "One Piece S22E41 to S22E50 land on 1129 to 1138", episodes: onePieceSeasons(), anime: true, files: onePiece22,
			wantEpisodes: [][2]int{{22, 1129}, {22, 1130}, {22, 1131}, {22, 1132}, {22, 1133}, {22, 1134}, {22, 1135}, {22, 1136}, {22, 1137}, {22, 1138}},
			wantBySeason: 10, wantNote: "Placed 10 episodes by the TheTVDB season numbers.",
		},
		{
			name: "the first and last episode of a season", episodes: onePieceSeasons(), anime: true,
			files:        []library.ImportFile{importFile("One Piece (1999)", 22, 1, "First"), importFile("One Piece (1999)", 22, 67, "Last"), importFile("One Piece (1999)", 23, 1, "Next season")},
			wantEpisodes: [][2]int{{22, 1089}, {22, 1155}, {23, 1156}},
			wantBySeason: 3, wantNote: "Placed 3 episodes by the TheTVDB season numbers.",
		},
		{
			name: "one episode says so in the singular", episodes: onePieceSeasons(), anime: true,
			files:        []library.ImportFile{importFile("One Piece (1999)", 22, 41, "Looming Despair")},
			wantEpisodes: [][2]int{{22, 1129}}, wantBySeason: 1, wantNote: "Placed 1 episode by the TheTVDB season numbers.",
		},
		{
			name: "a number past the season's last episode stays out", episodes: onePieceSeasons(), anime: true,
			files:       []library.ImportFile{importFile("One Piece (1999)", 22, 68, "Too far"), importFile("One Piece (1999)", 22, 41, "Fine")},
			wantUnknown: 1, wantEpisodes: [][2]int{{22, 1129}}, wantBySeason: 1,
			wantNote: "Placed 1 episode by the TheTVDB season numbers.\n" + renameOn,
		},
		{
			name: "a file that names an episode for certain is never taken by a season count", episodes: onePieceSeasons(), anime: true,
			files: []library.ImportFile{
				importFile("One Piece (1999)", 22, 41, "Counted by season"),
				{Path: "/tv/other/One Piece - S24E03 - 41.mkv", Season: 24, Episodes: []int{3}, Absolute: []int{41}},
			},
			wantEpisodes: [][2]int{{22, 1129}}, wantUnknown: 1, wantNote: renameOn,
		},
		{
			name: "a show that is not an anime is left alone", episodes: onePieceSeasons(), anime: false,
			files:       []library.ImportFile{importFile("One Piece (1999)", 22, 41, "Looming Despair")},
			wantUnknown: 1, wantNote: leftOutOne,
		},
		{
			name: "a normal show's files stay as named", episodes: normalShow(), anime: true,
			files:        []library.ImportFile{importFile("Show", 1, 5, "Five"), importFile("Show", 2, 10, "Ten")},
			wantEpisodes: [][2]int{{1, 5}, {2, 10}},
		},
		{
			name: "a missing number in a normal season is not remapped", episodes: normalShow(), anime: true,
			files:       []library.ImportFile{importFile("Show", 1, 30, "Missing")},
			wantUnknown: 1, wantNote: renameOn,
		},
		{
			name: "a missing number in a normal season of a standard show is not remapped", episodes: normalShow(), anime: false,
			files:       []library.ImportFile{importFile("Show", 2, 11, "Missing")},
			wantUnknown: 1, wantNote: leftOutOne,
		},
		{
			name: "Jujutsu Kaisen's three TheTVDB seasons fill the one season in order", episodes: jujutsuKaisen(), anime: true, files: jjkAll,
			wantBySeason: 35, wantNote: "Placed 35 episodes by the TheTVDB season numbers.",
			wantEpisodes: func() [][2]int {
				var out [][2]int
				for n := 1; n <= 59; n++ {
					out = append(out, [2]int{1, n})
				}
				return out
			}(),
		},
		{
			name: "the anime lists place Jujutsu Kaisen even when the files are incomplete", episodes: jujutsuKaisen(), anime: true,
			files: append(seasonFiles("Jujutsu Kaisen", 1, 1, 20), seasonFiles("Jujutsu Kaisen", 2, 1, 23)...), tvdb: jjkLists,
			wantBySeason: 23, wantNote: "Placed 23 episodes by the TheTVDB season numbers.",
			wantEpisodes: func() [][2]int {
				var out [][2]int
				for n := 1; n <= 20; n++ {
					out = append(out, [2]int{1, n})
				}
				for n := 25; n <= 47; n++ {
					out = append(out, [2]int{1, n})
				}
				return out
			}(),
		},
		{
			name: "without the lists, files that don't add up to the show are not guessed", episodes: jujutsuKaisen(), anime: true,
			files:       append(seasonFiles("Jujutsu Kaisen", 1, 1, 20), seasonFiles("Jujutsu Kaisen", 2, 1, 23)...),
			wantUnknown: 23, wantNote: fmt.Sprintf(rename, 23),
			wantEpisodes: func() [][2]int {
				var out [][2]int
				for n := 1; n <= 20; n++ {
					out = append(out, [2]int{1, n})
				}
				return out
			}(),
		},
		{
			name: "a season missing from the middle is not guessed", episodes: jujutsuKaisen(), anime: true,
			files:       append(seasonFiles("Jujutsu Kaisen", 1, 1, 24), seasonFiles("Jujutsu Kaisen", 3, 1, 12)...),
			wantUnknown: 12, wantNote: fmt.Sprintf(rename, 12),
			wantEpisodes: func() [][2]int {
				var out [][2]int
				for n := 1; n <= 24; n++ {
					out = append(out, [2]int{1, n})
				}
				return out
			}(),
		},
	}
	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmdbID := 1000 + i
			repo := newRepo(t)
			if _, _, err := repo.RegisterImport(library.ImportOptions{Kind: "tv"}, []library.ImportEntry{
				{Kind: "series", TMDBID: tmdbID, Title: "Show", Year: 2000, Files: tc.files},
			}); err != nil {
				t.Fatal(err)
			}
			series, _, _ := repo.GetSeriesByTMDBID(tmdbID)
			if tc.anime {
				if err := repo.SetSeriesType(series.ID, library.SeriesAnime); err != nil {
					t.Fatal(err)
				}
			}
			items, _ := repo.DueImportItems(time.Now(), 10)
			if len(items) != 1 {
				t.Fatalf("due: %+v", items)
			}
			res, err := repo.CompleteSeriesImport(items[0], library.SeriesDetails{Title: "Show", TVDBMap: tc.tvdb}, tc.episodes)
			if err != nil {
				t.Fatal(err)
			}
			if res.BySeason != tc.wantBySeason || res.Unknown != tc.wantUnknown || res.Imported != len(tc.wantEpisodes) {
				t.Fatalf("result %+v, want %d by season, %d unknown, %d imported", res, tc.wantBySeason, tc.wantUnknown, len(tc.wantEpisodes))
			}
			series, _, _ = repo.GetSeriesByTMDBID(tmdbID)
			got := map[[2]int]bool{}
			eps, _ := repo.ListEpisodes(series.ID)
			for _, e := range eps {
				if e.Status == library.StatusDownloaded {
					if e.FilePath == "" {
						t.Fatalf("downloaded episode without a file: %+v", e)
					}
					got[[2]int{e.Season, e.Episode}] = true
				}
			}
			if len(got) != len(tc.wantEpisodes) {
				t.Fatalf("downloaded %d episodes, want %d", len(got), len(tc.wantEpisodes))
			}
			for _, k := range tc.wantEpisodes {
				if !got[k] {
					t.Fatalf("season %d episode %d should have a file", k[0], k[1])
				}
			}
			done, _ := repo.ImportBatchItems(items[0].BatchID)
			if got := withoutFileList(done[0].Note); got != tc.wantNote {
				t.Fatalf("note %q, want %q", got, tc.wantNote)
			}
		})
	}
}

// The file for One Piece's S22E41 goes to the episode numbered 1129, with its
// own name, not to another one.
func TestImportPlacerCountsEachSeasonFromItsFirstEpisode(t *testing.T) {
	place := library.ImportPlacer(onePieceSeasons(), true, library.ImportMaps{}, nil)
	season, eps, ok := place(importFile("One Piece (1999)", 22, 41, "Looming Despair! Admiral Kizaru's Depressing Mission"))
	if !ok || season != 22 || len(eps) != 1 || eps[0] != 1129 {
		t.Fatalf("placed %d %v %v, want season 22 episode 1129", season, eps, ok)
	}
	if _, _, ok := library.ImportPlacer(onePieceSeasons(), false, library.ImportMaps{}, nil)(importFile("One Piece (1999)", 22, 41, "x")); ok {
		t.Fatal("a standard show is not placed this way")
	}
}

// withoutFileList drops the last line of an import note when it is the list of
// the files that were left out, which the tests of the other lines don't read.
func withoutFileList(note string) string {
	i := strings.LastIndex(note, "\nFiles left out: ")
	if i >= 0 {
		return note[:i]
	}
	if strings.HasPrefix(note, "Files left out: ") {
		return ""
	}
	return note
}
