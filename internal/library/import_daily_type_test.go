package library_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/library"
)

func datedFiles(n int) []library.ImportFile {
	out := make([]library.ImportFile, n)
	for i := range out {
		out[i] = library.ImportFile{Path: fmt.Sprintf("/tv/Late Show/Late.Show.2026.10.%02d.1080p.mkv", i+1), AirDate: fmt.Sprintf("2026-10-%02d", i+1)}
	}
	return out
}

func numberedFiles(n int) []library.ImportFile {
	out := make([]library.ImportFile, n)
	for i := range out {
		out[i] = library.ImportFile{Path: fmt.Sprintf("/tv/Late Show/S01E%02d.mkv", i+1), Season: 1, Episodes: []int{i + 1}}
	}
	return out
}

func TestLooksDaily(t *testing.T) {
	unreadable := library.ImportFile{Path: "/tv/Late Show/Clip.mkv"}
	tests := []struct {
		name      string
		files     []library.ImportFile
		tmdbDaily bool
		want      bool
	}{
		{"no files", nil, true, false},
		{"two dated files are too few", datedFiles(2), true, false},
		{"three dated files and no numbers", datedFiles(3), false, true},
		{"three dated files and a talk show", datedFiles(3), true, true},
		{"exactly 60 percent dated", append(datedFiles(3), unreadable, unreadable), true, true},
		{"just under 60 percent dated", append(datedFiles(3), unreadable, unreadable, unreadable), true, false},
		{"mostly dated, a numbered file, TMDB not daily", append(datedFiles(5), numberedFiles(1)...), false, false},
		{"mostly dated, a numbered file, TMDB says daily", append(datedFiles(5), numberedFiles(1)...), true, true},
		{"mostly numbered", append(numberedFiles(5), datedFiles(3)...), true, false},
		{"a dated name that was already placed does not count", func() []library.ImportFile {
			f := datedFiles(3)
			f[0].Season, f[0].Episodes = 1, []int{1}
			return f
		}(), true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := library.LooksDaily(tt.files, tt.tmdbDaily); got != tt.want {
				t.Fatalf("LooksDaily = %v, want %v", got, tt.want)
			}
		})
	}
}

// A show an import adds switches itself to the Daily type when its files say
// so; a type that is already something else, and a show that was in the
// library before the import, are left as they are.
func TestCompleteSeriesImportSetsTheDailyType(t *testing.T) {
	episodes := make([]library.Episode, 0, 5)
	for i := 1; i <= 5; i++ {
		episodes = append(episodes, library.Episode{Season: 1, Episode: i, Title: "Show", AirDate: fmt.Sprintf("2026-10-%02d", i)})
	}
	tests := []struct {
		name      string
		seed      string // type set before the import runs
		inLibrary bool   // the show was in the library before the import
		daily     bool
		want      string
	}{
		{"a new show with dated files", "", false, true, library.SeriesDaily},
		{"files that don't say daily", "", false, false, library.SeriesStandard},
		{"a type chosen by hand stays", library.SeriesAnime, false, true, library.SeriesAnime},
		{"a show already in the library stays", "", true, true, library.SeriesStandard},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newRepo(t)
			if tt.inLibrary {
				if _, err := repo.AddSeries(library.Series{TMDBID: 7, Title: "Late Show", Year: 2020, Monitored: true}, episodes); err != nil {
					t.Fatal(err)
				}
			}
			files := library.PlaceByAirDate(datedFiles(3), episodes)
			if _, _, err := repo.RegisterImport(library.ImportOptions{Kind: "tv"}, []library.ImportEntry{{Kind: "series", TMDBID: 7, Title: "Late Show", Year: 2020, Files: files}}); err != nil {
				t.Fatal(err)
			}
			series, _, _ := repo.GetSeriesByTMDBID(7)
			if tt.seed != "" {
				if err := repo.SetSeriesType(series.ID, tt.seed); err != nil {
					t.Fatal(err)
				}
			}
			items, _ := repo.DueImportItems(time.Now(), 10)
			if len(items) != 1 {
				t.Fatalf("due: %+v", items)
			}
			res, err := repo.CompleteSeriesImport(items[0], library.SeriesDetails{Title: "Late Show", Daily: tt.daily}, episodes)
			if err != nil {
				t.Fatal(err)
			}
			if res.Imported+res.Skipped != 3 {
				t.Fatalf("the dated files should land on their episodes: %+v", res)
			}
			series, _, _ = repo.GetSeriesByTMDBID(7)
			if series.SeriesType != tt.want {
				t.Fatalf("series type %q, want %q", series.SeriesType, tt.want)
			}
		})
	}
}
