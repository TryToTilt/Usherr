package library_test

import (
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/library"
)

// onePieceEpisodes is One Piece as the movie database lists it: one long
// season (and a special), where TheTVDB, and so Sonarr, counts 23 seasons.
func onePieceEpisodes() []library.Episode {
	eps := make([]library.Episode, 0, 1181)
	eps = append(eps, library.Episode{Season: 0, Episode: 1, Title: "Special"})
	for n := 1; n <= 1180; n++ {
		eps = append(eps, library.Episode{Season: 1, Episode: n, Title: "Episode", AirDate: "2000-01-01"})
	}
	return eps
}

func TestCompleteSeriesImportPlacesFilesByAbsoluteNumber(t *testing.T) {
	const (
		leftOut       = "Left out 1 episode that the movie database doesn't list for this show."
		noMatch       = leftOut + " Its number doesn't match the movie database's list, so nothing was guessed. Rename the file to the season and episode shown on the show's page and import the folder again."
		switchToAnime = leftOut + " Its name carries the episode number counted from the start of the show, so set this show's Episode numbering to Anime and import the folder again."
	)
	tests := []struct {
		name         string
		guessed      string // what the movie database's details suggest
		seed         string // series type the show already has
		scene        func(season, episode int) (int, int, bool)
		files        []library.ImportFile
		wantImported int
		wantUnknown  int
		wantEpisodes []int // season 1 episodes that must now be downloaded
		wantNote     string
		wantType     string
	}{
		{
			name: "an anime's S23E10 file is episode 1100", guessed: library.SeriesAnime, seed: "standard",
			files:        []library.ImportFile{{Path: "/tv/One Piece - S23E10 - 1100 - Title.mkv", Season: 23, Episodes: []int{10}, Absolute: []int{1100}}},
			wantImported: 1, wantEpisodes: []int{1100}, wantType: library.SeriesAnime,
		},
		{
			name: "a file in a Season 22 folder", guessed: library.SeriesAnime, seed: "anime",
			files:        []library.ImportFile{{Path: "/tv/Season 22/One Piece - S22E05 - 1015.mkv", Season: 22, Episodes: []int{5}, Absolute: []int{1015}}},
			wantImported: 1, wantEpisodes: []int{1015}, wantType: library.SeriesAnime,
		},
		{
			name: "a file named only by its number", guessed: library.SeriesAnime, seed: "anime",
			files:        []library.ImportFile{{Path: "/tv/[Group] One Piece - 1100 [1080p].mkv", Absolute: []int{1100}}},
			wantImported: 1, wantEpisodes: []int{1100}, wantType: library.SeriesAnime,
		},
		{
			name: "two episodes in one file", guessed: library.SeriesAnime, seed: "anime",
			files:        []library.ImportFile{{Path: "/tv/a.mkv", Season: 22, Episodes: []int{5, 6}, Absolute: []int{1015, 1016}}},
			wantImported: 2, wantEpisodes: []int{1015, 1016}, wantType: library.SeriesAnime,
		},
		{
			name: "a season and episode the show lists are taken as named", guessed: library.SeriesAnime, seed: "anime",
			files:        []library.ImportFile{{Path: "/tv/a.mkv", Season: 1, Episodes: []int{7}, Absolute: []int{1100}}},
			wantImported: 1, wantEpisodes: []int{7}, wantType: library.SeriesAnime,
		},
		{
			name: "a number past the last episode stays out and says what to do", guessed: library.SeriesAnime, seed: "anime",
			files:       []library.ImportFile{{Path: "/tv/a.mkv", Season: 24, Episodes: []int{3}, Absolute: []int{1500}}},
			wantUnknown: 1, wantNote: noMatch, wantType: library.SeriesAnime,
		},
		{
			name: "a file with no absolute number stays out", guessed: library.SeriesAnime, seed: "anime",
			files:       []library.ImportFile{{Path: "/tv/a.mkv", Season: 23, Episodes: []int{10}}},
			wantUnknown: 1, wantNote: noMatch, wantType: library.SeriesAnime,
		},
		{
			name: "the show's other numbering places a file with no absolute number", guessed: library.SeriesAnime, seed: "anime",
			scene: func(season, episode int) (int, int, bool) {
				if season == 23 && episode == 10 {
					return 1, 1100, true
				}
				return 0, 0, false
			},
			files:        []library.ImportFile{{Path: "/tv/a.mkv", Season: 23, Episodes: []int{10}}},
			wantImported: 1, wantEpisodes: []int{1100}, wantType: library.SeriesAnime,
		},
		{
			name: "a show that is not an anime is left alone and told to switch", guessed: library.SeriesStandard, seed: "standard",
			files:       []library.ImportFile{{Path: "/tv/a.mkv", Season: 23, Episodes: []int{10}, Absolute: []int{1100}}},
			wantUnknown: 1, wantNote: switchToAnime, wantType: library.SeriesStandard,
		},
		{
			name: "a plain unlisted episode of a standard show keeps the short note", guessed: library.SeriesStandard, seed: "standard",
			files:       []library.ImportFile{{Path: "/tv/a.mkv", Season: 23, Episodes: []int{10}}},
			wantUnknown: 1, wantNote: leftOut, wantType: library.SeriesStandard,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := newRepo(t)
			if _, _, err := repo.RegisterImport(library.ImportOptions{Kind: "tv"}, []library.ImportEntry{
				{Kind: "series", TMDBID: 37854, Title: "One Piece", Year: 1999, Files: tc.files},
			}); err != nil {
				t.Fatal(err)
			}
			series, _, _ := repo.GetSeriesByTMDBID(37854)
			if tc.seed == "anime" {
				if err := repo.SetSeriesType(series.ID, library.SeriesAnime); err != nil {
					t.Fatal(err)
				}
			}
			items, _ := repo.DueImportItems(time.Now(), 10)
			if len(items) != 1 {
				t.Fatalf("due: %+v", items)
			}
			res, err := repo.CompleteSeriesImport(items[0], library.SeriesDetails{Title: "One Piece", GuessedType: tc.guessed, SceneMap: tc.scene}, onePieceEpisodes())
			if err != nil {
				t.Fatal(err)
			}
			if res.Imported != tc.wantImported || res.Unknown != tc.wantUnknown {
				t.Fatalf("result %+v, want %d imported and %d unknown", res, tc.wantImported, tc.wantUnknown)
			}
			series, _, _ = repo.GetSeriesByTMDBID(37854)
			if series.SeriesType != tc.wantType {
				t.Fatalf("series type %q, want %q", series.SeriesType, tc.wantType)
			}
			downloaded := map[int]bool{}
			eps, _ := repo.ListEpisodes(series.ID)
			for _, e := range eps {
				if e.Status == library.StatusDownloaded {
					if e.Season != 1 || e.FilePath == "" {
						t.Fatalf("unexpected downloaded episode %+v", e)
					}
					downloaded[e.Episode] = true
				}
			}
			if len(downloaded) != len(tc.wantEpisodes) {
				t.Fatalf("downloaded %v, want %v", downloaded, tc.wantEpisodes)
			}
			for _, n := range tc.wantEpisodes {
				if !downloaded[n] {
					t.Fatalf("episode %d should be downloaded, got %v", n, downloaded)
				}
			}
			done, _ := repo.ImportBatchItems(items[0].BatchID)
			if done[0].Note != tc.wantNote {
				t.Fatalf("note %q, want %q", done[0].Note, tc.wantNote)
			}
		})
	}
}
