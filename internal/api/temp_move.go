package api

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/rdborg/mediarium/internal/fsinfo"
	"github.com/rdborg/mediarium/internal/organizer"
)

// Seams for the checks below, so tests can stand in for two drives.
var (
	filesystemOf = fsinfo.FilesystemID
	freeSpaceOf  = func(path string) (uint64, bool) {
		u, err := fsinfo.DiskUsage(path)
		if err != nil || u.TotalBytes == 0 {
			return 0, false
		}
		return u.FreeBytes, true
	}
)

// moveReserve is the room left over on the library's drive when a finished
// download is copied over from the temporary folder.
const moveReserve = 256 << 20

// inTempWork reports whether path is inside a work folder of a temporary
// download folder (the current one or an earlier one).
func (s *Server) inTempWork(path string) bool {
	path = filepath.Clean(path)
	if b := s.tempBase(); b != "" && isWithin(tempWorkRoot(b), path) {
		return true
	}
	for _, b := range s.previousTempBases() {
		if isWithin(tempWorkRoot(b), path) {
			return true
		}
	}
	return false
}

// crossesDrives reports whether moving src to dest crosses from one drive to
// another, so a hardlink is impossible and the file has to be copied. When that
// can't be told it says yes, which only means the room is checked.
func crossesDrives(src, dest string) bool {
	a, okA := filesystemOf(filepath.Dir(src))
	b, okB := filesystemOf(nearestExisting(filepath.Dir(dest)))
	return !okA || !okB || a != b
}

// checkRoomToMove stops a finished download in the temporary folder from being
// copied to a library drive that can't hold it. A move within the same drive (a
// hardlink) needs no room, and a download that isn't in the temporary folder is
// handled as it always was.
func (s *Server) checkRoomToMove(src, dest string) error {
	if !s.inTempWork(src) {
		return nil
	}
	if _, err := os.Lstat(dest); err == nil {
		return nil // already there: nothing is copied
	}
	info, err := os.Lstat(src)
	if err != nil || !info.Mode().IsRegular() || !crossesDrives(src, dest) {
		return nil
	}
	free, ok := freeSpaceOf(nearestExisting(filepath.Dir(dest)))
	if !ok {
		return nil // can't tell: never block on a guess
	}
	if need := uint64(info.Size()) + moveReserve; need > free {
		return fmt.Errorf("there isn't enough free space in the library folder to move this download from the temporary folder: it needs %s and %s is free", humanBytes(info.Size()), humanBytes(int64(free)))
	}
	return nil
}

// copiedFromTempNote is added to the activity line of an import that had to copy
// the file because the temporary folder is on another drive than the library.
func (s *Server) copiedFromTempNote(src string, res organizer.ImportResult) string {
	if res.UsedHardlink || res.Skipped || !s.inTempWork(src) {
		return ""
	}
	return " (copied over from the temporary download folder, which is on another drive)"
}
