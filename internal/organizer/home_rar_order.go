package organizer

// Home fork: some posts label their .partNN.rar files out of order: the file
// called part30 holds the last volume, part31 holds volume 30, and so on.
// The RAR reader goes by the names and stops with "bad volume number". Each
// RAR5 volume records its own number in its header, so the set is renamed
// to match before unpacking, as SABnzbd does. Only files next to the first
// volume, in the download's own working folder, are renamed, and only when
// every volume's header could be read and the numbers make a whole set.

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var rar5Signature = []byte("Rar!\x1a\x07\x01\x00")

// rar5VolumeNumber reads a RAR5 volume's number (the first volume is 0).
func rar5VolumeNumber(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 256)
	sig := make([]byte, len(rar5Signature))
	if _, err := io.ReadFull(r, sig); err != nil || !bytes.Equal(sig, rar5Signature) {
		return 0, errors.New("not a RAR5 volume")
	}
	if _, err := r.Discard(4); err != nil { // header CRC
		return 0, err
	}
	vint := func() (uint64, error) { return binary.ReadUvarint(r) }
	if _, err := vint(); err != nil { // header size
		return 0, err
	}
	typ, err := vint()
	if err != nil || typ != 1 {
		return 0, errors.New("no main archive header")
	}
	flags, err := vint()
	if err != nil {
		return 0, err
	}
	if flags&0x0001 != 0 { // extra area size
		if _, err := vint(); err != nil {
			return 0, err
		}
	}
	if flags&0x0002 != 0 { // data size
		if _, err := vint(); err != nil {
			return 0, err
		}
	}
	arcFlags, err := vint()
	if err != nil {
		return 0, err
	}
	if arcFlags&0x0001 == 0 {
		return 0, errors.New("not part of a volume set")
	}
	if arcFlags&0x0002 == 0 {
		return 0, nil // the first volume carries no number
	}
	n, err := vint()
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

// orderRARVolumesByHeader renames a .partNN.rar set so each name matches
// the number in the volume's own header. It returns the path of the first
// volume afterwards (unchanged when nothing needed renaming).
func orderRARVolumesByHeader(first string) (string, error) {
	dir, base := filepath.Split(first)
	m := rarSetRe.FindStringSubmatch(base)
	if m == nil {
		return first, nil
	}
	prefix := m[1]
	entries, err := os.ReadDir(filepath.Clean(dir))
	if err != nil {
		return first, nil
	}
	type volume struct {
		name      string
		named, is int // the number in the name, and the one in the header (both counting from 1)
	}
	var vols []volume
	seen := map[int]bool{}
	wrong := false
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		vm := rarSetRe.FindStringSubmatch(e.Name())
		if vm == nil || !strings.EqualFold(vm[1], prefix) {
			continue
		}
		named, err := strconv.Atoi(vm[2])
		if err != nil {
			return first, nil
		}
		hdr, err := rar5VolumeNumber(filepath.Join(dir, e.Name()))
		if err != nil {
			return first, nil // RAR4, damaged or unreadable: leave the names alone
		}
		is := hdr + 1
		if seen[is] {
			return first, nil // two volumes say the same number: not one set
		}
		seen[is] = true
		vols = append(vols, volume{name: e.Name(), named: named, is: is})
		wrong = wrong || named != is
	}
	if !wrong {
		return first, nil
	}
	for i := 1; i <= len(vols); i++ {
		if !seen[i] {
			return first, nil // a gap: a volume is missing, renaming won't help
		}
	}
	width := max(2, len(strconv.Itoa(len(vols))))
	// Two steps, so no name is taken while another file still has it.
	for i, v := range vols {
		tmp := filepath.Join(dir, fmt.Sprintf(".mediarium-reorder-%d", i))
		if err := os.Rename(filepath.Join(dir, v.name), tmp); err != nil {
			return first, fmt.Errorf("rename %s: %w", v.name, err)
		}
	}
	firstPath := first
	for i, v := range vols {
		target := filepath.Join(dir, fmt.Sprintf("%s.part%0*d.rar", prefix, width, v.is))
		if err := os.Rename(filepath.Join(dir, fmt.Sprintf(".mediarium-reorder-%d", i)), target); err != nil {
			return first, fmt.Errorf("rename %s: %w", v.name, err)
		}
		if v.is == 1 {
			firstPath = target
		}
	}
	return firstPath, nil
}
