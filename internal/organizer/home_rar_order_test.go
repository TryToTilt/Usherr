package organizer

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// fakeRAR5Volume writes just the start of a RAR5 volume: the signature and a
// main header saying which volume it is.
func fakeRAR5Volume(t *testing.T, path string, num int) {
	t.Helper()
	b := append([]byte{}, rar5Signature...)
	b = append(b, 0, 0, 0, 0) // header CRC (not checked)
	body := []byte{1, 0}      // type main, no flags
	if num == 0 {
		body = append(body, 1) // a volume, first: no number
	} else {
		body = append(body, 3)
		body = binary.AppendUvarint(body, uint64(num))
	}
	b = binary.AppendUvarint(b, uint64(len(body)))
	b = append(b, body...)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestOrderRARVolumesByHeader(t *testing.T) {
	dir := t.TempDir()
	name := func(n int) string {
		return filepath.Join(dir, "Thor.005.[2026].part"+[]string{"", "01", "02", "03", "04"}[n]+".rar")
	}
	// part03 holds the last volume and part04 holds volume 3.
	fakeRAR5Volume(t, name(1), 0)
	fakeRAR5Volume(t, name(2), 1)
	fakeRAR5Volume(t, name(3), 3)
	fakeRAR5Volume(t, name(4), 2)
	first, err := orderRARVolumesByHeader(name(1))
	if err != nil || first != name(1) {
		t.Fatalf("first = %q, %v", first, err)
	}
	for n := 1; n <= 4; n++ {
		got, err := rar5VolumeNumber(name(n))
		if err != nil || got != n-1 {
			t.Fatalf("part%d holds volume %d (%v)", n, got+1, err)
		}
	}
	// A set in order is left alone; so is one with a gap.
	if _, err := orderRARVolumesByHeader(name(1)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(name(2)); err != nil {
		t.Fatal(err)
	}
	fakeRAR5Volume(t, name(3), 2)
	fakeRAR5Volume(t, name(4), 3)
	if first, err := orderRARVolumesByHeader(name(1)); err != nil || first != name(1) {
		t.Fatalf("gap: %q, %v", first, err)
	}
}
