package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/fsinfo"
	"github.com/rdborg/mediarium/internal/organizer"
	"github.com/rdborg/mediarium/internal/settings"
)

func TestCheckLibraryRoot(t *testing.T) {
	old := onContainerDisk
	onContainerDisk = func(string) bool { return false }
	t.Cleanup(func() { onContainerDisk = old })
	marker := func(root string) string { return filepath.Join(root, fsinfo.RootMarker) }
	tests := []struct {
		name    string
		setup   func(t *testing.T, l tempLayout)
		wantErr string // "" = allowed
		// wantMarker: whether the movies folder holds the marker afterwards.
		wantMarker bool
	}{
		{
			name:    "a folder that is not there is refused",
			setup:   func(t *testing.T, l tempLayout) { _ = os.RemoveAll(l.movies) },
			wantErr: "can't be found",
		},
		{
			name:  "a new empty folder works and is not marked yet",
			setup: func(t *testing.T, l tempLayout) {},
		},
		{
			name: "a folder with content that was never marked works and gets its marker",
			setup: func(t *testing.T, l tempLayout) {
				if err := os.WriteFile(filepath.Join(l.movies, "old.mkv"), []byte("x"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			wantMarker: true,
		},
		{
			name: "a folder that lost its marker is refused",
			setup: func(t *testing.T, l tempLayout) {
				if err := os.WriteFile(filepath.Join(l.movies, "old.mkv"), []byte("x"), 0o644); err != nil {
					t.Fatal(err)
				}
				if err := l.s.checkLibraryRoot(filepath.Join(l.movies, "A", "a.mkv")); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(marker(l.movies)); err != nil {
					t.Fatal(err)
				}
			},
			wantErr: "has lost its .mediarium-root file",
		},
		{
			name: "putting the marker back lets imports through again",
			setup: func(t *testing.T, l tempLayout) {
				if err := os.WriteFile(filepath.Join(l.movies, "old.mkv"), []byte("x"), 0o644); err != nil {
					t.Fatal(err)
				}
				_ = l.s.checkLibraryRoot(filepath.Join(l.movies, "a.mkv"))
				_ = os.Remove(marker(l.movies))
				if err := os.WriteFile(marker(l.movies), nil, 0o644); err != nil {
					t.Fatal(err)
				}
			},
			wantMarker: true,
		},
		{
			name: "a folder chosen in settings is marked at once, even when empty",
			setup: func(t *testing.T, l tempLayout) {
				l.s.confirmLibraryRoot(l.movies, true)
				if err := os.Remove(marker(l.movies)); err != nil {
					t.Fatal(err)
				}
			},
			wantErr: "has lost its",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l := newTempLayout(t)
			tc.setup(t, l)
			err := l.s.checkLibraryRoot(filepath.Join(l.movies, "Film (2020)", "Film (2020).mkv"))
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("refused: %v", err)
			case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
				t.Fatalf("error = %v, want one containing %q", err, tc.wantErr)
			}
			if tc.wantErr != "" && !isLibraryRootError(err) {
				t.Errorf("%v isn't recognised as a library folder refusal", err)
			}
			if tc.wantErr == "" && fsinfo.HasRootMarker(l.movies) != tc.wantMarker {
				t.Errorf("marker present = %v, want %v", !tc.wantMarker, tc.wantMarker)
			}
		})
	}
}

func TestCheckLibraryRootIgnoresOtherPaths(t *testing.T) {
	l := newTempLayout(t)
	if err := l.s.checkLibraryRoot(filepath.Join(l.root, "elsewhere", "file.mkv")); err != nil {
		t.Fatalf("a path outside the library folders was refused: %v", err)
	}
}

// An import into a folder that lost its marker must refuse before it creates
// anything under it.
func TestImportDownloadedRefusesUnmountedFolder(t *testing.T) {
	old := onContainerDisk
	onContainerDisk = func(string) bool { return false }
	t.Cleanup(func() { onContainerDisk = old })
	l := newTempLayout(t)
	if err := os.WriteFile(filepath.Join(l.movies, "old.mkv"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := l.s.checkLibraryRoot(filepath.Join(l.movies, "x.mkv")); err != nil {
		t.Fatal(err)
	}
	// The drive goes away: an empty folder with no marker takes its place.
	if err := os.RemoveAll(l.movies); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(l.movies, 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(l.downloads, "Film.2020.1080p.mkv")
	if err := os.WriteFile(src, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(l.movies, "Film (2020)", "Film (2020).mkv")
	_, err := l.s.importDownloaded(src, dest, organizer.ConflictSkip, nil)
	if !isLibraryRootError(err) {
		t.Fatalf("importDownloaded error = %v, want a library folder refusal", err)
	}
	if _, statErr := os.Stat(filepath.Join(l.movies, "Film (2020)")); statErr == nil {
		t.Error("the import created a folder in the unmounted library folder")
	}
	if _, statErr := os.Stat(src); statErr != nil {
		t.Error("the download was touched")
	}
	// The drive is back with its marker: the same import goes through.
	if err := os.WriteFile(filepath.Join(l.movies, fsinfo.RootMarker), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := l.s.importDownloaded(src, dest, organizer.ConflictSkip, nil); err != nil {
		t.Fatalf("import with the marker back: %v", err)
	}
}

// A folder known to be on the container's own disk is never marked, so it can't
// be mistaken for a drive that was connected once.
func TestContainerDiskFolderIsNotMarked(t *testing.T) {
	old := onContainerDisk
	onContainerDisk = func(string) bool { return true }
	t.Cleanup(func() { onContainerDisk = old })
	l := newTempLayout(t)
	l.s.confirmLibraryRoot(l.movies, true)
	if fsinfo.HasRootMarker(l.movies) {
		t.Error("a folder on the container's disk got a marker")
	}
}

// A library folder inside another one is judged on its own marker: when the
// inner one is a drive that dropped off, the outer folder's marker says
// nothing about it.
func TestCheckLibraryRootUsesInnermostFolder(t *testing.T) {
	old := onContainerDisk
	onContainerDisk = func(string) bool { return false }
	t.Cleanup(func() { onContainerDisk = old })
	l := newTempLayout(t)
	tv := filepath.Join(l.movies, "tv")
	if err := os.MkdirAll(tv, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := l.s.Settings.Set(settings.KeyTVPath, tv, false); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tv, "old.mkv"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(l.movies, "old.mkv"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(tv, "Show", "S01E01.mkv")
	l.s.confirmLibraryRoot(tv, true)
	if err := l.s.checkLibraryRoot(dest); err != nil {
		t.Fatal(err)
	}
	if err := l.s.checkLibraryRoot(filepath.Join(l.movies, "a.mkv")); err != nil {
		t.Fatal(err)
	}
	// The inner drive goes away; the outer folder is untouched.
	if err := os.Remove(filepath.Join(tv, fsinfo.RootMarker)); err != nil {
		t.Fatal(err)
	}
	if err := l.s.checkLibraryRoot(dest); !isLibraryRootError(err) {
		t.Fatalf("a nested folder that lost its marker was allowed: %v", err)
	}
}
