package download

import (
	"encoding/binary"
	"errors"
	"testing"
)

// rar4Volume makes a RAR4 volume's start: the marker, an archive header and a
// stored file header for name, pack bytes of data following.
func rar4Volume(name string, pack, unp uint32, method byte, splitAfter bool) []byte {
	b := append([]byte(nil), rar4Sig...)
	b = append(b, 0, 0, 0x73, 0x01, 0x00, 13, 0, 0, 0, 0, 0, 0, 0) // archive header, a volume
	flags := uint16(0x8000)
	if splitAfter {
		flags |= 0x0002
	}
	h := make([]byte, 32+len(name))
	h[2] = 0x74
	binary.LittleEndian.PutUint16(h[3:], flags)
	binary.LittleEndian.PutUint16(h[5:], uint16(len(h)))
	binary.LittleEndian.PutUint32(h[7:], pack)
	binary.LittleEndian.PutUint32(h[11:], unp)
	h[25] = method
	binary.LittleEndian.PutUint16(h[26:], uint16(len(name)))
	copy(h[32:], name)
	return append(b, h...)
}

func TestParseRAR4StoredVolume(t *testing.T) {
	b := rar4Volume("Show.S01E01.mkv", 50000, 120000, 0x30, true)
	p, err := parseRARHead(b)
	if err != nil {
		t.Fatal(err)
	}
	if p.inner != "Show.S01E01.mkv" || p.dataSize != 50000 || p.innerSize != 120000 || p.dataOff != int64(len(b)) || !p.splitAfter {
		t.Fatalf("%+v", p)
	}
	if _, err := parseRARHead(rar4Volume("x.mkv", 1, 1, 0x33, false)); !errors.Is(err, ErrNeedsUnpack) {
		t.Fatalf("a compressed archive: %v", err)
	}
}

func putVint(b []byte, v uint64) []byte {
	for {
		c := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			b = append(b, c|0x80)
		} else {
			return append(b, c)
		}
	}
}

// rar5Header wraps fields as a RAR5 header: CRC, size, then the fields.
func rar5Header(fields []byte) []byte {
	b := []byte{0, 0, 0, 0}
	b = putVint(b, uint64(len(fields)))
	return append(b, fields...)
}

func TestParseRAR5StoredVolume(t *testing.T) {
	b := append([]byte(nil), rar5Sig...)
	// Archive header: a volume, number 3.
	var main []byte
	main = putVint(main, 1)    // type
	main = putVint(main, 0)    // flags
	main = putVint(main, 0x03) // archive flags: volume, number follows
	main = putVint(main, 3)
	b = append(b, rar5Header(main)...)
	// File header with 70000 bytes of data, stored, continuing in the next volume.
	name := "Movie.2026.2160p.mkv"
	var f []byte
	f = putVint(f, 2)             // type
	f = putVint(f, 0x0002|0x0010) // flags: data size follows, continues in next volume
	f = putVint(f, 70000)         // data size
	f = putVint(f, 0)             // file flags
	f = putVint(f, 9_000_000_000) // unpacked size
	f = putVint(f, 0)             // attributes
	f = putVint(f, 0)             // compression: stored
	f = putVint(f, 1)             // host OS
	f = putVint(f, uint64(len(name)))
	f = append(f, name...)
	b = append(b, rar5Header(f)...)
	p, err := parseRARHead(b)
	if err != nil {
		t.Fatal(err)
	}
	if p.inner != name || p.innerSize != 9_000_000_000 || p.dataSize != 70000 || p.dataOff != int64(len(b)) || p.volume != 3 || !p.splitAfter {
		t.Fatalf("%+v", p)
	}
}

func TestVolumeOrder(t *testing.T) {
	cases := map[string]int{"a.part01.rar": 1, "a.part12.rar": 12, "a.rar": 0, "a.r00": 1, "a.r09": 10, "a.s00": 101}
	for name, want := range cases {
		if got, ok := volumeOrder(name); !ok || got != want {
			t.Errorf("%s: %d %v, want %d", name, got, ok, want)
		}
	}
}

func TestRAR4VolumeNumberFromTheEnd(t *testing.T) {
	tail := append(make([]byte, 40), 0, 0, 0x7b, 0x0a, 0x40, 13, 0, 1, 2, 3, 4, 7, 0)
	// flags 0x400a: data CRC and volume number present; size 13
	if got := rar4VolumeNumber(tail); got != 7 {
		t.Fatalf("got %d", got)
	}
}
