// Adapted from DroppedNeedle (AGPL-3.0), backend/services/native/library_management_publisher.py

package musictidy

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// RecycleFolder is the folder under the music root where originals are kept
// after tidying, one sub-folder per plan. Its name starts with a dot, so
// library scans and media servers skip it.
const RecycleFolder = ".usherr-recycle"

// RecycleDir is where a plan's originals are kept.
func RecycleDir(root string, planID int64) string {
	return filepath.Join(root, RecycleFolder, strconv.FormatInt(planID, 10))
}

// ErrExists is returned when a move would replace a file that is already
// there.
var ErrExists = errors.New("a file is already there")

// State is a file's size and modification time, to tell whether it changed.
type State struct {
	Size  int64
	ModNs int64
}

// StateOf reads a file's State (of the link itself for a link).
func StateOf(path string) (State, bool, error) {
	st, err := os.Lstat(path)
	if err != nil {
		return State{}, st != nil && st.Mode()&os.ModeSymlink != 0, err
	}
	return State{Size: st.Size(), ModNs: st.ModTime().UnixNano()}, st.Mode()&os.ModeSymlink != 0, nil
}

// Move renames from to to without ever replacing another file. A change of
// letter case only (the same file on a case-insensitive disk) goes through a
// temporary name. Missing folders on the way are made.
func Move(from, to string) error {
	if from == to {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	if dst, err := os.Lstat(to); err == nil {
		src, err := os.Lstat(from)
		if err != nil {
			return err
		}
		if !os.SameFile(src, dst) {
			return fmt.Errorf("%s: %w", to, ErrExists)
		}
		mid := filepath.Join(filepath.Dir(to), ".usherr-case-"+randomHex())
		if err := os.Rename(from, mid); err != nil {
			return err
		}
		if err := os.Rename(mid, to); err != nil {
			_ = os.Rename(mid, from)
			return err
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(from, to)
}

// TempName is a hidden name next to path for a file being worked on, with the
// same extension (ffmpeg picks the format by it).
func TempName(path string) string {
	return filepath.Join(filepath.Dir(path), ".usherr-tidy-"+randomHex()+strings.ToLower(filepath.Ext(path)))
}

func randomHex() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// RemoveIfUnchanged removes a file Usherr wrote when it is still exactly as
// written. It reports whether it removed it.
func RemoveIfUnchanged(path string, want State) (bool, error) {
	got, _, err := StateOf(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if got != want {
		return false, nil
	}
	return true, os.Remove(path)
}

// Rewrite writes a copy of src at dst (a TempName) with the tags in set and,
// when cover is a picture file, that picture embedded. The audio is copied as
// it is (no re-encoding) and the file's other tags and pictures are kept.
// The copy is then checked: the same length (within half a second), codec
// and number of audio streams, every tag the file had still there, and the
// new tags read back. A copy that fails a check is removed and the original
// is never touched.
func Rewrite(ctx context.Context, src, dst string, set map[string]string, cover string) error {
	before, err := ProbeFile(ctx, src)
	if err != nil {
		return err
	}
	fam := FamilyOf(src)
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-i", src}
	addCover := cover != "" && fam.CanEmbedCover() && before.Pictures == 0
	if addCover {
		args = append(args, "-i", cover)
	}
	args = append(args, "-map", "0:a", "-map", "0:v?")
	if addCover {
		args = append(args, "-map", "1:v")
	}
	args = append(args, "-c", "copy", "-map_metadata", "0")
	if addCover {
		args = append(args, "-disposition:v:0", "attached_pic")
	}
	args = append(args, MetadataArgs(fam, set)...)
	args = append(args, "-fflags", "+bitexact", "-n", dst)

	rctx, cancel := context.WithTimeout(ctx, toolTimeout)
	defer cancel()
	cmd := exec.CommandContext(rctx, FFmpeg, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		_ = os.Remove(dst)
		return fmt.Errorf("ffmpeg: %v: %s", err, firstLine(stderr.String()))
	}
	after, err := ProbeFile(ctx, dst)
	if err == nil {
		err = verify(fam, before, after, set, addCover)
	}
	if err != nil {
		_ = os.Remove(dst)
		return fmt.Errorf("the new copy of %s failed its check: %w", filepath.Base(src), err)
	}
	// The copy can be read by whoever could read the original (a media server
	// running as another user).
	info, err := os.Stat(src)
	if err == nil {
		err = os.Chmod(dst, info.Mode().Perm())
	}
	if err != nil {
		_ = os.Remove(dst)
		return fmt.Errorf("the new copy of %s: %w", filepath.Base(src), err)
	}
	return nil
}

// ignoredKeys are tags ffmpeg itself writes or drops, which say nothing
// about the music.
var ignoredKeys = map[string]bool{"encoder": true}

func verify(fam Family, before, after Probe, set map[string]string, addCover bool) error {
	if before.AudioStreams != after.AudioStreams {
		return fmt.Errorf("%d audio streams, was %d", after.AudioStreams, before.AudioStreams)
	}
	if before.Codec != after.Codec {
		return fmt.Errorf("codec %s, was %s", after.Codec, before.Codec)
	}
	if math.Abs(before.DurationSec-after.DurationSec) > 0.5 {
		return fmt.Errorf("length %.2fs, was %.2fs", after.DurationSec, before.DurationSec)
	}
	wantPics := before.Pictures
	if addCover {
		wantPics++
	}
	if after.Pictures != wantPics {
		return fmt.Errorf("%d pictures, expected %d", after.Pictures, wantPics)
	}
	for k := range before.Tags {
		if ignoredKeys[k] {
			continue
		}
		if _, ok := after.Tags[k]; !ok {
			return fmt.Errorf("the %q tag was lost", k)
		}
	}
	got := ReadTags(after.Tags)
	for tag, v := range set {
		if !fam.Writable(tag) {
			continue
		}
		if !SameValue(tag, got[tag], v) {
			return fmt.Errorf("the %s tag reads %q, expected %q", tag, got[tag], v)
		}
	}
	return nil
}
