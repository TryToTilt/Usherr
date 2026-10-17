package api

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"

	"github.com/rdborg/mediarium/internal/config"
	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/debrid"
	"github.com/rdborg/mediarium/internal/queue"
	"github.com/rdborg/mediarium/internal/settings"
	"github.com/rdborg/mediarium/internal/store"
)

// newAuditServer is a whole server on a temporary folder, for the tests below
// that need its settings, queue and torrent registry.
func newAuditServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Config{ConfigDir: dir, DownloadsDir: dir, DownloadsIncomplete: dir, DownloadsComplete: dir, MoviesDir: dir}
	db, err := store.Open(filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	box, err := crypto.LoadOrCreateKey(filepath.Join(dir, "secret.key"))
	if err != nil {
		t.Fatalf("load key: %v", err)
	}
	s, err := New(db, cfg, box, "", "test")
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	return s
}

// A multi-disc audiobook keeps every file: names that clash across folders get
// their folder in front, and nothing is ever given the same name twice.
func TestAudiobookFileNames(t *testing.T) {
	dir := filepath.Join("work", "queue-1")
	p := func(parts ...string) string { return filepath.Join(append([]string{dir}, parts...)...) }
	tests := []struct {
		name  string
		files []string
		want  []string
	}{
		{"one folder keeps its names", []string{p("01.m4b"), p("02.m4b")}, []string{"01.m4b", "02.m4b"}},
		{"discs with the same track names",
			[]string{p("Book", "CD1", "Track 01.mp3"), p("Book", "CD1", "Track 02.mp3"), p("Book", "CD2", "Track 01.mp3")},
			[]string{"CD1 - Track 01.mp3", "Track 02.mp3", "CD2 - Track 01.mp3"}},
		{"parts as folders",
			[]string{p("Part 1", "01.m4b"), p("Part 2", "01.m4b")},
			[]string{"Part 1 - 01.m4b", "Part 2 - 01.m4b"}},
		{"names differing only in case",
			[]string{p("a", "Track.mp3"), p("b", "track.mp3")},
			[]string{"a - Track.mp3", "b - track.mp3"}},
		{"same folder name at two depths still ends up unique",
			[]string{p("x", "CD1", "01.mp3"), p("CD1", "01.mp3"), p("01.mp3")},
			[]string{"x - CD1 - 01.mp3", "CD1 - 01.mp3", "01.mp3"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := audiobookFileNames(dir, tc.files)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("names = %q, want %q", got, tc.want)
			}
			seen := map[string]bool{}
			for _, n := range got {
				if seen[n] {
					t.Fatalf("%q is used twice in %q", n, got)
				}
				seen[n] = true
			}
		})
	}
}

// A torrent whose download failed after it was added stops seeding, and a
// torrent that is still wanted is left alone.
func TestStopFailedTorrent(t *testing.T) {
	s := newAuditServer(t)
	root := t.TempDir()
	failedID, err := s.QueueRepo.Enqueue(queue.Item{ReleaseTitle: "Failed.Release", NZBURL: "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567", Protocol: queue.ProtocolTorrent})
	if err != nil {
		t.Fatal(err)
	}
	runningID, err := s.QueueRepo.Enqueue(queue.Item{ReleaseTitle: "Running.Release", NZBURL: "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234568", Protocol: queue.ProtocolTorrent})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{failedID, runningID} {
		if _, err := s.torrents.start(id, filepath.Join(root, fmt.Sprint(id)), root, 0, nil, func() {}); err != nil {
			t.Fatalf("start %d: %v", id, err)
		}
	}
	t.Cleanup(func() { s.torrents.stopAll() })

	_ = s.QueueRepo.SetStatus(runningID, queue.StatusDownloading, "")
	s.stopFailedTorrent(runningID)
	if !s.torrents.active(runningID) {
		t.Fatal("a torrent that is still downloading must keep running")
	}
	_ = s.QueueRepo.SetStatus(failedID, queue.StatusFailed, "the import failed")
	s.stopFailedTorrent(failedID)
	if s.torrents.active(failedID) {
		t.Fatal("a torrent whose download failed must stop seeding")
	}
	if !s.torrents.active(runningID) {
		t.Fatal("the other torrent must not be touched")
	}

	// Taking an entry off the list stops what is still seeding for it.
	_ = s.QueueRepo.SetStatus(runningID, queue.StatusCompleted, "")
	item, _ := s.QueueRepo.Get(runningID)
	if err := s.removeQueueEntry(item, false, false); err != nil {
		t.Fatal(err)
	}
	if s.torrents.active(runningID) {
		t.Fatal("removing an entry must stop its torrent")
	}
}

// Torrents that started without a VPN are stopped once the kill switch says
// torrents need one; with the switch off they carry on.
func TestDirectTorrentAllowed(t *testing.T) {
	s := newAuditServer(t)
	if !s.directTorrentAllowed() {
		t.Fatal("with no VPN and the kill switch off, a direct torrent may carry on")
	}
	if err := s.Settings.Set(settings.KeyVPNRequireForTorrents, "1", false); err != nil {
		t.Fatal(err)
	}
	if s.directTorrentAllowed() {
		t.Fatal("with the kill switch on and no VPN, a direct torrent must stop")
	}
}

// Archives inside a folder of the download (a torrent's own folder) are
// unpacked next to where they lie.
func TestUnpackArchivesInASubfolder(t *testing.T) {
	dir := t.TempDir()
	folder := filepath.Join(dir, "Movie.2001.1080p-GRP")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(folder, "release.zip"))
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, _ := zw.Create("Movie.2001.mkv")
	w.Write([]byte("video"))
	zw.Close()
	f.Close()

	if err := unpackArchives(dir); err != nil {
		t.Fatalf("unpack: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(folder, "Movie.2001.mkv")); err != nil || string(got) != "video" {
		t.Fatalf("extracted file: %q, %v", got, err)
	}
}

// PAR2 trouble that comes from this machine does not blame the release.
func TestRepairFailureBlamesTheReleaseOnlyWhenItIsAtFault(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantBad bool
	}{
		{"too much missing", errors.New("PAR2 can't repair this download: too much of it is missing (par2: exit status 1)"), true},
		{"articles missing", errors.New("3 articles couldn't be found on your Usenet servers and PAR2 repair isn't available"), true},
		{"disk full", errors.New("PAR2 couldn't repair this download: write error: No space left on device"), false},
		{"disk full by code", &os.PathError{Op: "write", Path: "x", Err: syscall.ENOSPC}, false},
		{"not allowed to write", errors.New("PAR2 couldn't repair this download: Permission denied"), false},
		{"read-only folder", errors.New("Read-only file system"), false},
		{"too many files for the system", fmt.Errorf("run par2 repair: %w", syscall.E2BIG), false},
		{"time limit", fmt.Errorf("par2 repair x: it took too long and was stopped: %w", context.DeadlineExceeded), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isBadRelease(repairFailure(tt.err)); got != tt.wantBad {
				t.Errorf("isBadRelease = %v, want %v", got, tt.wantBad)
			}
		})
	}
	for _, e := range []error{syscall.EACCES, syscall.EROFS, syscall.EIO} {
		if isBadRelease(unpackFailure(&os.PathError{Op: "write", Path: "x", Err: e})) {
			t.Errorf("a local write error (%v) must not blame the release", e)
		}
	}
}

// A debrid download leaves out samples and notes, like the built-in client,
// and counts what an earlier try already saved.
func TestDebridWantedFiles(t *testing.T) {
	s := newAuditServer(t)
	id, err := s.QueueRepo.Enqueue(queue.Item{ReleaseTitle: "Some.Movie.2020.1080p", NZBURL: "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567", Protocol: queue.ProtocolTorrent})
	if err != nil {
		t.Fatal(err)
	}
	files := []debrid.File{
		{Name: "Some.Movie.2020.1080p/Some.Movie.2020.1080p.mkv", Size: 4 << 30},
		{Name: "Some.Movie.2020.1080p/Sample/sample.mkv", Size: 20 << 20},
		{Name: "Some.Movie.2020.1080p/readme.nfo", Size: 1000},
	}
	got := s.debridWantedFiles(id, files)
	if len(got) != 1 || got[0].Name != files[0].Name {
		t.Fatalf("wanted files = %v, want only the movie", got)
	}

	dir := t.TempDir()
	part := filepath.Join(dir, debrid.SafeName(files[0].Name)+".part")
	if err := os.MkdirAll(filepath.Dir(part), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(part, make([]byte, 1234), 0o644); err != nil {
		t.Fatal(err)
	}
	if n := debridOnDisk(dir, files[0]); n != 1234 {
		t.Fatalf("on disk = %d, want the 1234 bytes of the part file", n)
	}
}
