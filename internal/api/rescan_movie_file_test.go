package api_test

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/queue"
	"github.com/rdborg/mediarium/internal/settings"
)

// Rescan files on a movie that has no file yet: a video somebody put in the
// movie's folder by hand is found and added where it lies. Nothing is moved,
// renamed or deleted.
func TestRescanGivesAMovieTheFileInItsFolder(t *testing.T) {
	const movieFile = "The.Matrix.1999.1080p.BluRay.x264-GRP.mkv"
	tests := []struct {
		name string
		// files to create under the movies folder, with their sizes in bytes
		files map[string]int
		// another movie that already owns the file at this path (relative to the movies folder)
		ownedBy string
		// a download of the movie is running
		downloading bool
		wantFile    string // relative to the movies folder; "" = the movie stays without a file
		wantLooked  string // the folder reported when nothing was found, relative to the movies folder
		wantBusy    bool
	}{
		{
			name:     "the video in the movie's own folder",
			files:    map[string]int{"The Matrix (1999)/" + movieFile: 500},
			wantFile: "The Matrix (1999)/" + movieFile,
		},
		{
			name: "the biggest video wins, extras and samples are left out",
			files: map[string]int{
				"The Matrix (1999)/" + movieFile:                     500,
				"The Matrix (1999)/Matrix-small.mkv":                 100,
				"The Matrix (1999)/Extras/Making of.mkv":             900,
				"The Matrix (1999)/The Matrix (1999)-trailer.mkv":    800,
				"The Matrix (1999)/Sample/sample.mkv":                700,
				"The Matrix (1999)/The Matrix (1999).en.srt":         50,
				"The Matrix (1999)/The Matrix (1999).nfo":            10,
				"The Matrix (1999)/The.Matrix.1999.sample.mkv":       600,
				"The Matrix (1999)/.hidden-stub.mkv":                 950,
				"The Matrix (1999)/Season Extras/behind the scenes/": 0,
			},
			wantFile: "The Matrix (1999)/" + movieFile,
		},
		{
			name:     "a video in a folder inside the movie's folder",
			files:    map[string]int{"The Matrix (1999)/CD1/" + movieFile: 500},
			wantFile: "The Matrix (1999)/CD1/" + movieFile,
		},
		{
			name:       "an empty folder",
			files:      map[string]int{"The Matrix (1999)/The Matrix (1999).en.srt": 10},
			wantLooked: "The Matrix (1999)",
		},
		{
			name:       "no folder at all",
			files:      map[string]int{"Another Film (2001)/another.mkv": 500},
			wantLooked: "The Matrix (1999)",
		},
		{
			name:     "a folder named a little differently",
			files:    map[string]int{"the.matrix.1999.1080p/" + movieFile: 500},
			wantFile: "the.matrix.1999.1080p/" + movieFile,
		},
		{
			name: "two folders that both look like the movie are not guessed between",
			files: map[string]int{
				"the.matrix.1999.1080p/" + movieFile:      500,
				"The Matrix 1999 Remastered/matrix.mkv":   500,
				"The Matrix (1999) [2160p]/matrix.4k.mkv": 500,
			},
			wantLooked: "The Matrix (1999)",
		},
		{
			name:       "a folder of another year is not this movie",
			files:      map[string]int{"The Matrix (2003)/matrix.reloaded.mkv": 500},
			wantLooked: "The Matrix (1999)",
		},
		{
			name:       "a folder with no year is not taken for a movie that has one",
			files:      map[string]int{"The Matrix/matrix.mkv": 500},
			wantLooked: "The Matrix (1999)",
		},
		{
			name:       "a file that another movie already has",
			files:      map[string]int{"The Matrix (1999)/" + movieFile: 500},
			ownedBy:    "The Matrix (1999)/" + movieFile,
			wantLooked: "The Matrix (1999)",
		},
		{
			name:        "left alone while it is downloading",
			files:       map[string]int{"The Matrix (1999)/" + movieFile: 500},
			downloading: true,
			wantBusy:    true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, base, admin, member, _ := familyServer(t)
			movies := filepath.Join(t.TempDir(), "movies")
			if err := os.MkdirAll(movies, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := server.Settings.Set(settings.KeyMoviesPath, movies, false); err != nil {
				t.Fatal(err)
			}
			sizes := map[string]int{}
			for rel, n := range tc.files {
				path := filepath.Join(movies, filepath.FromSlash(rel))
				if rel[len(rel)-1] == '/' {
					if err := os.MkdirAll(path, 0o755); err != nil {
						t.Fatal(err)
					}
					continue
				}
				writeBody(t, path, string(make([]byte, n)))
				sizes[rel] = n
			}
			m, err := server.MovieRepo.Add(library.Movie{TMDBID: 603, Title: "The Matrix", Year: 1999, Monitored: true})
			if err != nil {
				t.Fatal(err)
			}
			if tc.ownedBy != "" {
				other, err := server.MovieRepo.Add(library.Movie{TMDBID: 604, Title: "The Matrix Again", Year: 1999})
				if err != nil {
					t.Fatal(err)
				}
				if err := server.MovieRepo.SetStatus(other.ID, library.StatusDownloaded, "", filepath.Join(movies, filepath.FromSlash(tc.ownedBy))); err != nil {
					t.Fatal(err)
				}
			}
			if tc.downloading {
				if _, err := server.QueueRepo.Enqueue(queue.Item{MovieID: m.ID, ReleaseTitle: "The.Matrix.1999.1080p.BluRay.x264-GRP"}); err != nil {
					t.Fatal(err)
				}
			}
			url := fmt.Sprintf("%s/api/movies/%d/rescan", base, m.ID)

			// Only an administrator may press the button.
			if status, _ := doStatus(t, member, http.MethodPost, url); status != http.StatusForbidden {
				t.Fatalf("a member's rescan = %d, want 403", status)
			}

			got := postJSON[map[string]any](t, admin, url, nil, http.StatusOK)
			after, _ := server.MovieRepo.Get(m.ID)
			if tc.wantFile != "" {
				want := filepath.Join(movies, filepath.FromSlash(tc.wantFile))
				if got["added"] != filepath.Base(want) || got["looked"] != nil {
					t.Fatalf("report = %v, want it to say it added %s", got, filepath.Base(want))
				}
				if after.Status != library.StatusDownloaded || after.FilePath != want {
					t.Fatalf("movie = %+v, want downloaded with %s", after, want)
				}
				if after.Quality == "" {
					t.Errorf("the movie should get a quality read from the file name: %+v", after)
				}
			} else {
				if got["added"] != nil || after.FilePath != "" || after.Status == library.StatusDownloaded {
					t.Fatalf("nothing should have been added: report %v, movie %+v", got, after)
				}
				if tc.wantBusy {
					if got["busy"] != true || got["looked"] != nil {
						t.Fatalf("report = %v, want busy", got)
					}
				} else if want := filepath.Join(movies, filepath.FromSlash(tc.wantLooked)); got["looked"] != want {
					t.Fatalf("looked = %v, want %s", got["looked"], want)
				}
			}
			// Nothing is moved, renamed or deleted.
			for rel, n := range sizes {
				info, err := os.Stat(filepath.Join(movies, filepath.FromSlash(rel)))
				if err != nil || info.Size() != int64(n) {
					t.Errorf("%s was touched: %v", rel, err)
				}
			}
		})
	}
}

// A second copy with no folder set for second copies is refused: it must not
// go looking in the main title's folder and take its file.
func TestRescanSecondCopyWithoutCopiesFolder(t *testing.T) {
	server, base, admin, _, _ := familyServer(t)
	movies := filepath.Join(t.TempDir(), "movies")
	writeBody(t, filepath.Join(movies, "The Matrix (1999)", "The.Matrix.1999.1080p.BluRay.x264-GRP.mkv"), "video")
	if err := server.Settings.Set(settings.KeyMoviesPath, movies, false); err != nil {
		t.Fatal(err)
	}
	main, err := server.MovieRepo.Add(library.Movie{TMDBID: 603, Title: "The Matrix", Year: 1999, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	cp, err := server.MovieRepo.Add(library.Movie{TMDBID: 604, Title: "The Matrix", Year: 1999, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.TestExec("UPDATE movies SET copy_of = ? WHERE id = ?", main.ID, cp.ID); err != nil {
		t.Fatal(err)
	}
	if status, _ := doStatus(t, admin, http.MethodPost, fmt.Sprintf("%s/api/movies/%d/rescan", base, cp.ID)); status != http.StatusConflict {
		t.Fatalf("rescan of a copy without a copies folder = %d, want 409", status)
	}
	if after, _ := server.MovieRepo.Get(cp.ID); after.FilePath != "" || after.Status == library.StatusDownloaded {
		t.Fatalf("the copy was given a file: %+v", after)
	}
}

// A second press does nothing new, and a movie that has its file is looked at
// the way it always was.
func TestRescanMovieWithAFileStillChecksIt(t *testing.T) {
	server, base, admin, _, _ := familyServer(t)
	movies := filepath.Join(t.TempDir(), "movies")
	file := filepath.Join(movies, "The Matrix (1999)", "The Matrix (1999).mkv")
	writeBody(t, file, "video")
	writeBody(t, filepath.Join(movies, "The Matrix (1999)", "Bigger.File.mkv"), "a much bigger video file")
	if err := server.Settings.Set(settings.KeyMoviesPath, movies, false); err != nil {
		t.Fatal(err)
	}
	m, err := server.MovieRepo.Add(library.Movie{TMDBID: 603, Title: "The Matrix", Year: 1999, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.MovieRepo.SetStatus(m.ID, library.StatusDownloaded, "WEBDL-1080p", file); err != nil {
		t.Fatal(err)
	}
	url := fmt.Sprintf("%s/api/movies/%d/rescan", base, m.ID)
	for i := 0; i < 2; i++ {
		got := postJSON[map[string]any](t, admin, url, nil, http.StatusOK)
		if got["found"] != float64(1) || got["added"] != nil {
			t.Fatalf("report = %v, want its own file found and nothing added", got)
		}
		if after, _ := server.MovieRepo.Get(m.ID); after.FilePath != file {
			t.Fatalf("the movie's file changed to %s", after.FilePath)
		}
	}
}
