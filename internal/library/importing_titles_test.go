package library_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/library"
)

// onePieceSeasonTitles is One Piece's season 22 as the movie database lists it
// (1089 to 1155), with the real titles around the episodes in the issue, where
// TheTVDB's season 22 starts at 1086 and so counts three episodes more.
func onePieceSeasonTitles() []library.Episode {
	real := map[int]string{
		1126: "Looming Despair! Admiral Kizaru's Depressing Mission",
		1127: "Luffy vs. Kizaru! A Fierce Kaleidoscopic Battle",
		1128: "The Nightmare Strikes - Godhead of Science & Defense, St. Saturn",
		1129: "Kuma's Past - Better Off Dead in This World",
		1130: "A History Erased! God Valley of Despair",
		1131: "A Fleeting Moment of Happiness - Kumachi and Ginny",
		1132: "A Pledge to Ginny - Kuma Becomes a Father",
		1133: "To Save His Daughter - Kuma the Timid Pacifist",
		1134: "Cruel Fate - Kuma's Decision as a Father",
		1135: "To the Sea Where My Father Is! The Future Bonney Chooses",
	}
	eps := []library.Episode{{Season: 0, Episode: 1, Title: "Special"}}
	for n := 1089; n <= 1155; n++ {
		title, ok := real[n]
		if !ok {
			title = fmt.Sprintf("Egghead Chapter %d", n)
		}
		eps = append(eps, library.Episode{Season: 22, Episode: n, Title: title, AirDate: "2025-01-01"})
	}
	return eps
}

func titledFile(season, episode int, name string) library.ImportFile {
	return library.ImportFile{
		Path:     fmt.Sprintf("/tv/One Piece (1999)/Season %d/One Piece - S%02dE%02d - %s", season, season, episode, name),
		Season:   season,
		Episodes: []int{episode},
	}
}

func TestImportPlacesAnimeFilesByTheirTitles(t *testing.T) {
	season22 := onePieceSeasonTitles()
	sameTitle := append([]library.Episode{}, season22...)
	for i := range sameTitle {
		if sameTitle[i].Episode == 1100 || sameTitle[i].Episode == 1101 {
			sameTitle[i].Title = "Recap Special Edition"
		}
	}
	// The reporter's files: season 22 counted by TheTVDB, E41 is episode 1126.
	var reporter []library.ImportFile
	for i, title := range []string{
		"Looming Despair! Admiral Kizaru's Depressing Mission.mkv",
		"Luffy vs. Kizaru! A Fierce Kaleidoscopic Battle.mkv",
		"The Nightmare Strikes - Godhead of Science & Defense, St. Saturn.mkv",
		"Kuma's Past - Better Off Dead in This World.mkv",
		"A History Erased! God Valley of Despair.mkv",
		"A Fleeting Moment of Happiness - Kumachi and Ginny.mkv",
		"A Pledge to Ginny - Kuma Becomes a Father.mkv",
		"To Save His Daughter - Kuma the Timid Pacifist.mkv",
		"Cruel Fate - Kuma's Decision as a Father.mkv",
		"To the Sea Where My Father Is! The Future Bonney Chooses.mkv",
	} {
		reporter = append(reporter, titledFile(22, 41+i, title))
	}
	range1126 := func(from, to int) [][2]int {
		var out [][2]int
		for n := from; n <= to; n++ {
			out = append(out, [2]int{22, n})
		}
		return out
	}

	tests := []struct {
		name       string
		episodes   []library.Episode
		anime      bool
		files      []library.ImportFile
		wantEps    [][2]int
		wantTitled int
		wantBy     int
		wantLeft   int
		wantNote   string
	}{
		{
			name: "the reporter's files land on 1126 to 1135, not three episodes late", episodes: season22, anime: true, files: reporter,
			wantEps: range1126(1126, 1135), wantTitled: 10, wantNote: "Placed 10 episodes by their titles.",
		},
		{
			name: "release tags, a language tag and ampersands do not stop a match", episodes: season22, anime: true,
			files: []library.ImportFile{
				titledFile(22, 41, "Looming Despair! Admiral Kizaru's Depressing Mission [WEBDL-1080p][8bit][h264]-GROUP.mkv"),
				titledFile(22, 43, "The Nightmare Strikes - Godhead of Science and Defense, St Saturn.en.mkv"),
			},
			wantEps: [][2]int{{22, 1126}, {22, 1128}}, wantTitled: 2, wantNote: "Placed 2 episodes by their titles.",
		},
		{
			name: "a file whose title matches nothing is left out and named", episodes: season22, anime: true,
			files: []library.ImportFile{
				titledFile(22, 41, "Looming Despair! Admiral Kizaru's Depressing Mission.mkv"),
				titledFile(22, 60, "Not An Episode Anyone Lists.mkv"),
			},
			wantEps: range1126(1126, 1126), wantTitled: 1, wantLeft: 1,
			wantNote: "Placed 1 episode by its title.\nLeft out 1 episode that the movie database doesn't list for this show. Its number doesn't match the movie database's list, so nothing was guessed. Rename the file to the season and episode shown on the show's page and import the folder again.\nFiles left out: One Piece - S22E60 - Not An Episode Anyone Lists.mkv.",
		},
		{
			name: "a title two episodes share places nothing", episodes: sameTitle, anime: true,
			files: []library.ImportFile{
				titledFile(22, 12, "Recap Special Edition.mkv"),
				titledFile(22, 41, "Looming Despair! Admiral Kizaru's Depressing Mission.mkv"),
			},
			wantEps: range1126(1126, 1126), wantTitled: 1, wantLeft: 1,
			wantNote: "Placed 1 episode by its title.\nLeft out 1 episode that the movie database doesn't list for this show. Its number doesn't match the movie database's list, so nothing was guessed. Rename the file to the season and episode shown on the show's page and import the folder again.\nFiles left out: One Piece - S22E12 - Recap Special Edition.mkv.",
		},
		{
			name: "two files with one title place neither", episodes: season22, anime: true,
			files: []library.ImportFile{
				titledFile(22, 41, "Looming Despair! Admiral Kizaru's Depressing Mission.mkv"),
				{Path: "/tv/other/One Piece - S23E02 - Looming Despair! Admiral Kizaru's Depressing Mission.mkv", Season: 23, Episodes: []int{2}},
			},
			wantLeft: 2,
			wantNote: "Left out 2 episodes that the movie database doesn't list for this show. Their numbers don't match the movie database's list, so nothing was guessed. Rename the files to the season and episode shown on the show's page and import the folder again.\nFiles left out: One Piece - S22E41 - Looming Despair! Admiral Kizaru's Depressing Mission.mkv, One Piece - S23E02 - Looming Despair! Admiral Kizaru's Depressing Mission.mkv.",
		},
		{
			name: "a show that is not an anime is left alone", episodes: season22, anime: false,
			files:    []library.ImportFile{titledFile(22, 41, "Looming Despair! Admiral Kizaru's Depressing Mission.mkv")},
			wantLeft: 1,
			wantNote: "Left out 1 episode that the movie database doesn't list for this show.\nFiles left out: One Piece - S22E41 - Looming Despair! Admiral Kizaru's Depressing Mission.mkv.",
		},
		{
			name: "a title that agrees with the season offset keeps the stronger rule", episodes: season22, anime: true,
			files: []library.ImportFile{
				titledFile(22, 1, "Egghead Chapter 1089.mkv"),
				titledFile(22, 2, "Egghead Chapter 1090.mkv"),
			},
			wantEps: range1126(1089, 1090), wantBy: 2, wantNote: "Placed 2 episodes by the TheTVDB season numbers.",
		},
		{
			name: "a title never moves a file the season offset rule places where no title disagrees", episodes: season22, anime: true,
			files:   []library.ImportFile{titledFile(22, 41, "A Name Nobody Knows.mkv")},
			wantEps: range1126(1129, 1129), wantBy: 1, wantNote: "Placed 1 episode by the TheTVDB season numbers.",
		},
		{
			name: "more than five files left out are listed as five and a count", episodes: season22, anime: true,
			files: func() []library.ImportFile {
				var out []library.ImportFile
				for n := 41; n < 48; n++ {
					out = append(out, titledFile(22, n, fmt.Sprintf("Mystery %d.mkv", n)))
				}
				out = append(out, titledFile(22, 48, "Looming Despair! Admiral Kizaru's Depressing Mission.mkv"))
				return out
			}(),
			wantEps: range1126(1126, 1126), wantTitled: 1, wantLeft: 7,
			wantNote: "Placed 1 episode by its title.\nLeft out 7 episodes that the movie database doesn't list for this show. Their numbers don't match the movie database's list, so nothing was guessed. Rename the files to the season and episode shown on the show's page and import the folder again.\nFiles left out: One Piece - S22E41 - Mystery 41.mkv, One Piece - S22E42 - Mystery 42.mkv, One Piece - S22E43 - Mystery 43.mkv, One Piece - S22E44 - Mystery 44.mkv, One Piece - S22E45 - Mystery 45.mkv and 2 more.",
		},
	}
	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmdbID := 3000 + i
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
			res, err := repo.CompleteSeriesImport(items[0], library.SeriesDetails{Title: "Show"}, tc.episodes)
			if err != nil {
				t.Fatal(err)
			}
			if res.Titled != tc.wantTitled || res.BySeason != tc.wantBy || res.Unknown != tc.wantLeft || res.Imported != len(tc.wantEps) {
				t.Fatalf("result %+v, want %d by title, %d by season, %d left out, %d imported", res, tc.wantTitled, tc.wantBy, tc.wantLeft, len(tc.wantEps))
			}
			series, _, _ = repo.GetSeriesByTMDBID(tmdbID)
			got := map[[2]int]string{}
			eps, _ := repo.ListEpisodes(series.ID)
			for _, e := range eps {
				if e.Status == library.StatusDownloaded {
					got[[2]int{e.Season, e.Episode}] = e.FilePath
				}
			}
			for _, k := range tc.wantEps {
				if got[k] == "" {
					t.Fatalf("season %d episode %d should have a file, got %v", k[0], k[1], got)
				}
			}
			done, _ := repo.ImportBatchItems(items[0].BatchID)
			if done[0].Note != tc.wantNote {
				t.Fatalf("note %q, want %q", done[0].Note, tc.wantNote)
			}
		})
	}
}

// Every file of the reporter's season ends on the episode with its own title.
func TestImportTitlesPutEachFileOnItsOwnEpisode(t *testing.T) {
	episodes := onePieceSeasonTitles()
	var files []library.ImportFile
	want := map[string]int{}
	for _, e := range episodes {
		if e.Season != 22 || e.Episode < 1126 || e.Episode > 1135 {
			continue
		}
		f := titledFile(22, e.Episode-1085, e.Title+".mkv")
		files = append(files, f)
		want[f.Path] = e.Episode
	}
	place := library.ImportPlacer(episodes, true, library.ImportMaps{}, files)
	for _, f := range files {
		season, eps, ok := place(f)
		if !ok || season != 22 || len(eps) != 1 || eps[0] != want[f.Path] {
			t.Fatalf("%s: placed %d %v %v, want season 22 episode %d", f.Path, season, eps, ok, want[f.Path])
		}
	}
	if strings.Contains(files[0].Path, "S22E41") == false {
		t.Fatal("the first file should be S22E41")
	}
}
