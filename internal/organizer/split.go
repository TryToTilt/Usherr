package organizer

// Usherr: files cut into numbered pieces by a plain file splitter
// ("Issue 001.cbr.000", ".001", ".002"... or "Film.mkv.001", ".002"...),
// not RAR volumes. Some releases pack those pieces inside their archives;
// once the archives are unpacked, the pieces are joined back into the file.

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// "<name>.<ext>.<3 digits>": the extension says what the joined file is.
var splitPiece = regexp.MustCompile(`^(.+\.[A-Za-z0-9]{2,4})\.(\d{3})$`)

// JoinSplitFiles joins every complete run of numbered pieces under dir into
// the file they were cut from, and removes the pieces. A run starts at .000
// or .001 and has no gaps. 7z volumes (.7z.001) are left to 7z. It returns
// the files it made.
func JoinSplitFiles(dir string) ([]string, error) {
	runs := map[string]map[int]string{} // joined path -> piece number -> piece path
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		m := splitPiece.FindStringSubmatch(d.Name())
		if m == nil || strings.EqualFold(filepath.Ext(m[1]), ".7z") {
			return nil
		}
		n, _ := strconv.Atoi(m[2])
		target := filepath.Join(filepath.Dir(path), m[1])
		if runs[target] == nil {
			runs[target] = map[int]string{}
		}
		runs[target][n] = path
		return nil
	})
	if err != nil {
		return nil, err
	}
	var made []string
	targets := make([]string, 0, len(runs))
	for t := range runs {
		targets = append(targets, t)
	}
	sort.Strings(targets)
	for _, target := range targets {
		pieces := runs[target]
		nums := make([]int, 0, len(pieces))
		for n := range pieces {
			nums = append(nums, n)
		}
		sort.Ints(nums)
		if len(nums) < 2 || nums[0] > 1 || nums[len(nums)-1]-nums[0] != len(nums)-1 {
			continue // one piece, or a run with a gap: not something to join
		}
		if _, err := os.Stat(target); err == nil {
			continue // already there
		}
		if err := joinPieces(target, nums, pieces); err != nil {
			return made, err
		}
		for _, n := range nums {
			_ = os.Remove(pieces[n])
		}
		made = append(made, target)
	}
	return made, nil
}

func joinPieces(target string, nums []int, pieces map[int]string) error {
	tmp := target + ".joining"
	out, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("join %s: %w", filepath.Base(target), err)
	}
	for _, n := range nums {
		in, err := os.Open(pieces[n])
		if err != nil {
			out.Close()
			os.Remove(tmp)
			return fmt.Errorf("join %s: %w", filepath.Base(target), err)
		}
		_, err = io.Copy(out, in)
		in.Close()
		if err != nil {
			out.Close()
			os.Remove(tmp)
			return fmt.Errorf("join %s: %w", filepath.Base(target), err)
		}
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("join %s: %w", filepath.Base(target), err)
	}
	return os.Rename(tmp, target)
}
