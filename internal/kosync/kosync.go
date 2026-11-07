// Package kosync holds what Usherr needs to be a KOSync server, the progress
// sync of KOReader and Readest: the way KOReader names a file, the places its
// apps reported, and turning Usherr's places into ones KOReader can open.
// The routes are in internal/api/kosync_api.go.
package kosync

import (
	"crypto/md5" //nolint:gosec // KOReader's document ids and keys are MD5; nothing here relies on it being strong
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
)

const sampleSize = 1024

// partialOffsets are the twelve places KOReader reads: 1024 << 2i for i from
// -1 to 10. The first is 0 and not 256: LuaJIT masks the shift count to five
// bits, so 1024 << -2 is 1024 << 30, which overflows 32 bits to 0.
var partialOffsets = [...]int64{0, 1024, 4096, 16384, 65536, 262144, 1048576, 4194304, 16777216, 67108864, 268435456, 1073741824}

// PartialMD5 is KOReader's document id for a file: the MD5 of up to 1 KB from
// each of the offsets, stopping at the first one past the end.
func PartialMD5(r io.ReaderAt) (string, error) {
	h := md5.New() //nolint:gosec // see the import
	buf := make([]byte, sampleSize)
	for _, off := range partialOffsets {
		n, err := r.ReadAt(buf, off)
		if err != nil && !errors.Is(err, io.EOF) {
			return "", fmt.Errorf("read the book at %d: %w", off, err)
		}
		if n == 0 {
			break
		}
		h.Write(buf[:n])
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// PartialMD5File is PartialMD5 of a file on disk.
func PartialMD5File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open the book: %w", err)
	}
	defer f.Close()
	return PartialMD5(f)
}

// MD5Hex is the MD5 of a text in lower-case hex: how KOReader makes its key
// from a password and, on the file-name setting, a document id from a name.
func MD5Hex(s string) string {
	sum := md5.Sum([]byte(s)) //nolint:gosec // see the import
	return hex.EncodeToString(sum[:])
}

// spineStep finds the book's place in the reading order in an EPUB CFI:
// epubcfi(/6/14!/4/2...) is the 7th item (/6 is the spine, items count in twos).
var spineStep = regexp.MustCompile(`^epubcfi\(/6/(\d+)(?:\[[^\]]*\])?!`)

// SpineIndexOfCFI is the 0-based reading-order item of a CFI; ok is false when
// the position isn't a CFI that says.
func SpineIndexOfCFI(cfi string) (index int, ok bool) {
	m := spineStep.FindStringSubmatch(cfi)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n < 2 || n%2 != 0 {
		return 0, false
	}
	return n/2 - 1, true
}

// SpineIndexOfPercent is the reading-order item a percentage (0-100) falls in,
// counting each item by its size in bytes. sizes has one entry per item.
func SpineIndexOfPercent(percent float64, sizes []int64) (index int, ok bool) {
	var total int64
	for _, s := range sizes {
		total += s
	}
	if total <= 0 {
		return 0, false
	}
	target := percent / 100 * float64(total)
	var seen int64
	for i, s := range sizes {
		seen += s
		if float64(seen) > target {
			return i, true
		}
	}
	return len(sizes) - 1, true
}

// ChapterXPointer is the KOReader place for the start of a reading-order item.
// KOReader names the items DocFragment[1], DocFragment[2]...
func ChapterXPointer(spineIndex int) string {
	return "/body/DocFragment[" + strconv.Itoa(spineIndex+1) + "]/body"
}
