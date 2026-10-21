package api

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/cleanup"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/organizer"
	"github.com/rdborg/mediarium/internal/queue"
	"github.com/rdborg/mediarium/internal/settings"
)

// tempServer is optionsServer with library, config and temporary folders laid
// out in a scratch directory.
type tempLayout struct {
	s         *Server
	root      string // scratch directory holding everything below
	movies    string
	downloads string
	config    string
	temp      string
}

func newTempLayout(t *testing.T) tempLayout {
	t.Helper()
	s, _ := optionsServer(t)
	root := t.TempDir()
	l := tempLayout{
		s: s, root: root,
		movies:    filepath.Join(root, "lib", "movies"),
		downloads: filepath.Join(root, "downloads"),
		config:    filepath.Join(root, "config"),
		temp:      filepath.Join(root, "cache"),
	}
	for _, d := range []string{l.movies, l.downloads, l.config, l.temp} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	s.cfg.MoviesDir, s.cfg.DownloadsDir, s.cfg.ConfigDir = l.movies, l.downloads, l.config
	s.cfg.DownloadsIncomplete = filepath.Join(l.downloads, "incomplete")
	return l
}

// runningItem queues a download and marks it as downloading.
func (l tempLayout) runningItem(t *testing.T, tmdb int) int64 {
	t.Helper()
	m, err := l.s.MovieRepo.Add(library.Movie{TMDBID: tmdb, Title: fmt.Sprintf("Film %d", tmdb), Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	id, err := l.s.QueueRepo.Enqueue(queue.Item{MovieID: m.ID, ReleaseTitle: "r", NZBURL: "http://x"})
	if err != nil {
		t.Fatal(err)
	}
	if err := l.s.QueueRepo.SetStatus(id, queue.StatusDownloading, ""); err != nil {
		t.Fatal(err)
	}
	return id
}

func (l tempLayout) setTemp(t *testing.T, v string) {
	t.Helper()
	if err := l.s.Settings.Set(settings.KeyDownloadsTempDir, v, false); err != nil {
		t.Fatal(err)
	}
}

func TestCheckTempFolder(t *testing.T) {
	l := newTempLayout(t)
	file := filepath.Join(l.root, "afile")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(l.root, "link")
	if err := os.Symlink(l.temp, link); err != nil {
		t.Fatal(err)
	}
	insideMovies := filepath.Join(l.movies, "cache")
	if err := os.MkdirAll(insideMovies, 0o755); err != nil {
		t.Fatal(err)
	}
	insideConfig := filepath.Join(l.config, "cache")
	if err := os.MkdirAll(insideConfig, 0o755); err != nil {
		t.Fatal(err)
	}
	insideWork := filepath.Join(l.downloads, "incomplete", "x")
	if err := os.MkdirAll(insideWork, 0o755); err != nil {
		t.Fatal(err)
	}
	viaLink := filepath.Join(l.root, "movielink")
	if err := os.Symlink(l.movies, viaLink); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		path string
		want string // part of the message, "" for accepted
	}{
		{"a folder of its own", l.temp, ""},
		{"not a full path", "cache/downloads", "isn't complete"},
		{"does not exist", filepath.Join(l.root, "nope"), "doesn't exist"},
		{"is a file", file, "not a folder"},
		{"is a symbolic link", link, "shortcut"},
		{"same as the downloads folder", l.downloads, "different folder from the downloads folder"},
		{"inside the downloads work folder", insideWork, "incomplete folder"},
		{"the library folder", l.movies, "library folder"},
		{"inside the library folder", insideMovies, "library folder"},
		{"holds the library folder", filepath.Dir(l.movies), "library folder"},
		{"the settings folder", l.config, "settings folder"},
		{"inside the settings folder", insideConfig, "settings folder"},
		{"the whole disk", "/", "whole disk"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := l.s.checkTempFolder(tc.path)
			if tc.want == "" {
				if got != "" {
					t.Fatalf("refused %s: %s", tc.path, got)
				}
				return
			}
			if !strings.Contains(got, tc.want) {
				t.Fatalf("message for %s = %q, want it to mention %q", tc.path, got, tc.want)
			}
		})
	}
}

func TestWorkDirSelection(t *testing.T) {
	l := newTempLayout(t)
	s := l.s
	defaultDir := func(id int64) string { return filepath.Join(l.downloads, "incomplete", fmt.Sprintf("queue-%d", id)) }
	tempDir := func(base string, id int64) string {
		return filepath.Join(base, "incomplete", fmt.Sprintf("queue-%d", id))
	}

	// Nothing set: as before.
	if got := s.workDirFor(1); got != defaultDir(1) {
		t.Fatalf("no temporary folder: %s, want %s", got, defaultDir(1))
	}

	// A download that started in the downloads folder stays there when a
	// temporary folder is set, while new ones go to the temporary folder.
	if err := os.MkdirAll(defaultDir(1), 0o755); err != nil {
		t.Fatal(err)
	}
	l.setTemp(t, l.temp)
	if got := s.workDirFor(1); got != defaultDir(1) {
		t.Fatalf("running download moved: %s", got)
	}
	if got := s.workDirFor(2); got != tempDir(l.temp, 2) {
		t.Fatalf("new download: %s, want %s", got, tempDir(l.temp, 2))
	}

	// The same when the temporary folder changes: the download that began in
	// the old one is still found there.
	if err := os.MkdirAll(tempDir(l.temp, 2), 0o755); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(l.root, "cache2")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	s.rememberTempBase(l.temp)
	l.setTemp(t, other)
	if got := s.workDirFor(2); got != tempDir(l.temp, 2) {
		t.Fatalf("download left in the earlier temporary folder: %s, want %s", got, tempDir(l.temp, 2))
	}
	if got := s.workDirFor(3); got != tempDir(other, 3) {
		t.Fatalf("new download after the change: %s, want %s", got, tempDir(other, 3))
	}

	// Switching it off sends new downloads back to the downloads folder; earlier
	// ones are still found.
	s.rememberTempBase(other)
	l.setTemp(t, "")
	if got := s.workDirFor(4); got != defaultDir(4) {
		t.Fatalf("temporary folder off: %s, want %s", got, defaultDir(4))
	}
	if got := s.workDirFor(2); got != tempDir(l.temp, 2) {
		t.Fatalf("download still in the temporary folder: %s", got)
	}

	// An external client's links never go to the temporary folder.
	l.setTemp(t, l.temp)
	if got := s.defaultWorkDirFor(9); got != defaultDir(9) {
		t.Fatalf("client folder: %s, want %s", got, defaultDir(9))
	}
}

func TestRememberTempBase(t *testing.T) {
	l := newTempLayout(t)
	s := l.s
	for i := 0; i < maxPreviousTempFolders+3; i++ {
		s.rememberTempBase(filepath.Join(l.root, fmt.Sprintf("old%d", i)))
	}
	s.rememberTempBase(filepath.Join(l.root, "old5")) // again: moves to the front, no repeat
	got := s.previousTempBases()
	if len(got) != maxPreviousTempFolders {
		t.Fatalf("kept %d folders, want %d", len(got), maxPreviousTempFolders)
	}
	if got[0] != filepath.Join(l.root, "old5") {
		t.Fatalf("most recent first: %v", got)
	}
	seen := map[string]bool{}
	for _, p := range got {
		if seen[p] {
			t.Fatalf("%s is listed twice", p)
		}
		seen[p] = true
	}
}

func TestCheckTempReady(t *testing.T) {
	l := newTempLayout(t)
	s := l.s
	l.setTemp(t, l.temp)
	dir := s.workDirFor(5)
	if err := s.checkTempReady(dir); err != nil {
		t.Fatalf("a mounted folder was refused: %v", err)
	}
	// The drive is not there: a new download is refused rather than started in a
	// folder made inside the container.
	if err := os.RemoveAll(l.temp); err != nil {
		t.Fatal(err)
	}
	if err := s.checkTempReady(dir); err == nil || !strings.Contains(err.Error(), "isn't there") {
		t.Fatalf("a missing folder was accepted: %v", err)
	}
	if _, err := os.Stat(l.temp); err == nil {
		t.Fatal("the check made the missing folder")
	}
	// The downloads folder's own work folder is not subject to this.
	if err := s.checkTempReady(s.defaultWorkDirFor(5)); err != nil {
		t.Fatalf("downloads folder refused: %v", err)
	}
}

func TestCleanupCoversTempFolder(t *testing.T) {
	l := newTempLayout(t)
	s := l.s
	l.setTemp(t, l.temp)
	work := tempWorkRoot(l.temp)
	old := time.Now().Add(-72 * time.Hour)
	mk := func(dir string, aged bool) {
		t.Helper()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "part.bin"), []byte("data"), 0o644); err != nil {
			t.Fatal(err)
		}
		if aged {
			for _, p := range []string{filepath.Join(dir, "part.bin"), dir} {
				if err := os.Chtimes(p, old, old); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	// 901: no download owns it and it is old. 902: owned by a running download.
	// 903: old but too new to judge is not simulated; the user's own file beside
	// the work folder must never be touched.
	mk(filepath.Join(work, "queue-901"), true)
	activeID := l.runningItem(t, 700)
	activeDir := filepath.Join(work, fmt.Sprintf("queue-%d", activeID))
	mk(activeDir, true)
	mk(filepath.Join(work, "queue-903"), false) // too new
	userFile := filepath.Join(l.temp, "my-own-file.mkv")
	if err := os.WriteFile(userFile, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(userFile, old, old); err != nil {
		t.Fatal(err)
	}
	// A folder in the downloads folder is handled as before, in the same run.
	mk(filepath.Join(l.downloads, "incomplete", "queue-904"), true)

	items, err := s.scanDownloads(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, it := range items {
		names = append(names, filepath.ToSlash(it.Path))
	}
	joined := strings.Join(names, "\n")
	for _, want := range []string{"queue-901", "queue-904"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("clean-up did not find %s:\n%s", want, joined)
		}
	}
	for _, keep := range []string{fmt.Sprintf("queue-%d", activeID), "queue-903", "my-own-file"} {
		if strings.Contains(joined, keep) {
			t.Fatalf("clean-up would remove %s:\n%s", keep, joined)
		}
	}
	removed, err := cleanup.RemoveAll(s.downloadsAreas(), items)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 2 {
		t.Fatalf("removed %d, want 2", len(removed))
	}
	for _, gone := range []string{filepath.Join(work, "queue-901"), filepath.Join(l.downloads, "incomplete", "queue-904")} {
		if _, err := os.Stat(gone); err == nil {
			t.Fatalf("%s is still there", gone)
		}
	}
	for _, kept := range []string{activeDir, filepath.Join(work, "queue-903"), userFile, work} {
		if _, err := os.Stat(kept); err != nil {
			t.Fatalf("%s was removed: %v", kept, err)
		}
	}
}

func TestCleanupKeepsEarlierTempFolderDownloads(t *testing.T) {
	l := newTempLayout(t)
	s := l.s
	// A download began in cache, then the setting moved on to cache2.
	l.setTemp(t, l.temp)
	id := l.runningItem(t, 701)
	dir := s.workDirFor(id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.part"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-72 * time.Hour)
	_ = os.Chtimes(dir, old, old)
	s.rememberTempBase(l.temp)
	l.setTemp(t, filepath.Join(l.root, "cache2"))

	if got := s.workDirFor(id); got != dir {
		t.Fatalf("the running download was orphaned: %s, want %s", got, dir)
	}
	items, err := s.scanDownloads(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if strings.Contains(it.Path, fmt.Sprintf("queue-%d", id)) {
			t.Fatalf("clean-up would remove a running download in the earlier folder: %s", it.Path)
		}
	}
}

func TestRemoveWorkDirInTempFolder(t *testing.T) {
	l := newTempLayout(t)
	s := l.s
	l.setTemp(t, l.temp)
	dir := s.workDirFor(11)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.part"), []byte("12345"), 0o644); err != nil {
		t.Fatal(err)
	}
	if freed := s.removeWorkDir(s.workDirFor(11)); freed != 5 {
		t.Fatalf("freed %d bytes, want 5", freed)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Fatal("the title's leftover folder is still in the temporary folder")
	}
	// Anything that is not a download's own folder is refused, even inside it.
	notQueue := filepath.Join(tempWorkRoot(l.temp), "precious")
	if err := os.MkdirAll(notQueue, 0o755); err != nil {
		t.Fatal(err)
	}
	if freed := s.removeWorkDir(notQueue); freed != 0 {
		t.Fatalf("removed something that is not a download folder (%d)", freed)
	}
	if _, err := os.Stat(notQueue); err != nil {
		t.Fatal("a folder that is not a download's was removed")
	}
	outside := filepath.Join(l.movies, "queue-12")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	s.removeWorkDir(outside)
	if _, err := os.Stat(outside); err != nil {
		t.Fatal("a folder outside every work folder was removed")
	}
}

func TestMoveKeptDownloadStaysInItsWorkFolder(t *testing.T) {
	l := newTempLayout(t)
	s := l.s
	l.setTemp(t, l.temp)
	old := filepath.Join(tempWorkRoot(l.temp), "queue-20")
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(old, "x.part"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !s.moveKeptDownload(old, 21) {
		t.Fatal("the kept files were not moved")
	}
	if _, err := os.Stat(filepath.Join(tempWorkRoot(l.temp), "queue-21", "x.part")); err != nil {
		t.Fatalf("the retry's folder has no files: %v", err)
	}
	if got := s.workDirFor(21); got != filepath.Join(tempWorkRoot(l.temp), "queue-21") {
		t.Fatalf("the retry looks elsewhere: %s", got)
	}
	// A folder that is not in a work folder is never moved.
	stray := filepath.Join(l.movies, "queue-30")
	if err := os.MkdirAll(stray, 0o755); err != nil {
		t.Fatal(err)
	}
	if s.moveKeptDownload(stray, 31) {
		t.Fatal("a folder outside the work folders was moved")
	}
}

func TestCheckRoomToMove(t *testing.T) {
	l := newTempLayout(t)
	s := l.s
	l.setTemp(t, l.temp)
	work := tempWorkRoot(l.temp)
	if err := os.MkdirAll(filepath.Join(work, "queue-40"), 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(work, "queue-40", "movie.mkv")
	if err := os.WriteFile(src, make([]byte, 1000), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(l.movies, "Film (2020)", "Film (2020).mkv")
	elsewhere := filepath.Join(l.downloads, "incomplete", "queue-41", "movie.mkv")
	if err := os.MkdirAll(filepath.Dir(elsewhere), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(elsewhere, make([]byte, 1000), 0o644); err != nil {
		t.Fatal(err)
	}

	defer func(f func(string) (string, bool), g func(string) (uint64, bool)) { filesystemOf, freeSpaceOf = f, g }(filesystemOf, freeSpaceOf)
	tests := []struct {
		name    string
		src     string
		sameFS  bool
		free    uint64
		freeOK  bool
		wantErr bool
	}{
		{"another drive with room", src, false, 10 << 30, true, false},
		{"another drive without room", src, false, 1 << 20, true, true},
		{"another drive, free space unknown", src, false, 0, false, false},
		{"same drive needs no room", src, true, 0, true, false},
		{"not from the temporary folder is unchanged", elsewhere, false, 0, true, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			filesystemOf = func(p string) (string, bool) {
				if tc.sameFS || !strings.HasPrefix(p, l.temp) {
					return "dev:1", true
				}
				return "dev:2", true
			}
			freeSpaceOf = func(string) (uint64, bool) { return tc.free, tc.freeOK }
			err := s.checkRoomToMove(tc.src, dest)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, want error: %v", err, tc.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), "free space") {
				t.Fatalf("message is not plain: %v", err)
			}
		})
	}
}

func TestCopiedFromTempNote(t *testing.T) {
	l := newTempLayout(t)
	s := l.s
	l.setTemp(t, l.temp)
	inTemp := filepath.Join(tempWorkRoot(l.temp), "queue-1", "a.mkv")
	inDownloads := filepath.Join(l.downloads, "incomplete", "queue-2", "a.mkv")
	tests := []struct {
		name string
		src  string
		res  organizer.ImportResult
		want bool
	}{
		{"copied from the temporary folder", inTemp, organizer.ImportResult{}, true},
		{"hardlinked from the temporary folder", inTemp, organizer.ImportResult{UsedHardlink: true}, false},
		{"skipped", inTemp, organizer.ImportResult{Skipped: true}, false},
		{"copied from the downloads folder", inDownloads, organizer.ImportResult{}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := s.copiedFromTempNote(tc.src, tc.res) != ""; got != tc.want {
				t.Fatalf("note = %v, want %v", got, tc.want)
			}
		})
	}
}
