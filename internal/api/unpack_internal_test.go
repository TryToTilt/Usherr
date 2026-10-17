package api

import (
	"archive/zip"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/rdborg/mediarium/internal/download"
	"github.com/rdborg/mediarium/internal/organizer"
	"github.com/rdborg/mediarium/internal/queue"
)

// A full disk or a missing 7z is our setup's fault and must never blocklist
// a good release; corrupt, unsafe or password-protected archives are the
// release's fault.
func TestUnpackFailureBlamesTheReleaseOnlyWhenItIsAtFault(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantBad bool
	}{
		{"password protected", fmt.Errorf("extract x.rar: %w", organizer.ErrPasswordProtected), true},
		{"unsafe paths", fmt.Errorf("extract x.zip: %w", organizer.ErrUnsafeArchive), true},
		{"zip bomb", fmt.Errorf("extract x.zip: %w", organizer.ErrArchiveTooLarge), true},
		{"corrupt", errors.New("rardecode: bad header crc"), true},
		{"disk full", fmt.Errorf("extract x.rar: %w", organizer.ErrInsufficientSpace), false},
		{"7z missing", fmt.Errorf("extract x.7z: %w", organizer.ErrSevenZipMissing), false},
		{"disk filled up while writing", fmt.Errorf("extract x.rar: write a.mkv: %w", &os.PathError{Op: "write", Path: "a.mkv", Err: syscall.ENOSPC}), false},
		{"quota reached while writing", fmt.Errorf("extract x.rar: write a.mkv: %w", &os.PathError{Op: "write", Path: "a.mkv", Err: syscall.EDQUOT}), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := unpackFailure(tt.err)
			if isBadRelease(got) != tt.wantBad {
				t.Errorf("isBadRelease = %v, want %v", isBadRelease(got), tt.wantBad)
			}
			if !errors.Is(got, tt.err) {
				t.Error("the original error must stay reachable with errors.Is")
			}
		})
	}
}

// A password-protected RAR opens with the password from the NZB, or from the
// {{password}} after the release name, and a wrong one is a bad release that
// says the password is wrong. The fixture is a real RAR5 archive made with
// rar a -hp (file names encrypted too); its password is S3cret.
func TestUnpackArchivesUsesTheReleasePassword(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("..", "organizer", "testdata", "encrypted", "rar5-hp.rar"))
	if err != nil {
		t.Fatal(err)
	}
	s := newAuditServer(t)
	tests := []struct {
		name, nzbPassword, title string
		wantErr                  error
	}{
		{"password in the NZB", "S3cret", "Some.Movie.2020.1080p", nil},
		{"password after the release name", "", "Some.Movie.2020.1080p{{S3cret}}", nil},
		{"wrong password", "nope", "Some.Movie.2020.1080p", organizer.ErrWrongPassword},
		{"no password", "", "Some.Movie.2020.1080p", organizer.ErrPasswordProtected},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "release.rar"), fixture, 0o644); err != nil {
				t.Fatal(err)
			}
			id, err := s.QueueRepo.Enqueue(queue.Item{ReleaseTitle: tt.title, NZBURL: "http://indexer.invalid/" + tt.name})
			if err != nil {
				t.Fatal(err)
			}
			s.keepArchivePassword(id, &download.NZB{Password: tt.nzbPassword}, dir)

			err = unpackArchives(dir)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("unpack: %v", err)
				}
				if _, err := os.Stat(filepath.Join(dir, "movie.mkv")); err != nil {
					t.Fatalf("the movie was not unpacked: %v", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if !isBadRelease(unpackFailure(err)) {
				t.Error("an archive that can't be opened is the release's fault")
			}
		})
	}
}

// unpackArchives needs no external tool for ZIP any more.
func TestUnpackArchivesHandlesZipWithoutSevenZip(t *testing.T) {
	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, "release.zip"))
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
	if got, err := os.ReadFile(filepath.Join(dir, "Movie.2001.mkv")); err != nil || string(got) != "video" {
		t.Fatalf("extracted file: %q, %v", got, err)
	}
}
