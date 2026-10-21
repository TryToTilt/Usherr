package download

// Usherr: playing the video inside a set of RAR volumes without unpacking.
// Most releases are stored, not compressed: each volume holds a stretch of
// the video as it is, after a small header. Reading the headers (in the
// first article of each volume) says where each stretch is, and the volumes
// in order make the whole video. Compressed or locked archives can't be read
// this way; they're refused so another release can be tried.

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	rar4Sig = []byte("Rar!\x1a\x07\x00")
	rar5Sig = []byte("Rar!\x1a\x07\x01\x00")
)

// ErrNeedsUnpack means the archive is compressed or locked: it can only be
// played after unpacking.
var ErrNeedsUnpack = errors.New("the release's archives are compressed or locked")

// rarPiece is a volume's stretch of the file inside.
type rarPiece struct {
	inner      string // the file inside
	innerSize  int64  // its whole size
	dataOff    int64  // where its stretch starts in the volume
	dataSize   int64
	volume     int // the volume's number, when the archive says (-1 when not)
	splitAfter bool
}

// parseRARHead reads the first file header in a volume's first bytes.
func parseRARHead(b []byte) (rarPiece, error) {
	switch {
	case len(b) >= len(rar5Sig) && string(b[:len(rar5Sig)]) == string(rar5Sig):
		return parseRAR5(b)
	case len(b) >= len(rar4Sig) && string(b[:len(rar4Sig)]) == string(rar4Sig):
		return parseRAR4(b)
	}
	return rarPiece{}, errors.New("not a RAR volume")
}

func parseRAR4(b []byte) (rarPiece, error) {
	p := rarPiece{volume: -1}
	pos := len(rar4Sig)
	for pos+7 <= len(b) {
		typ := b[pos+2]
		flags := binary.LittleEndian.Uint16(b[pos+3:])
		size := int(binary.LittleEndian.Uint16(b[pos+5:]))
		if size < 7 {
			return p, errors.New("a damaged RAR header")
		}
		switch typ {
		case 0x73: // archive header
			if flags&0x0080 != 0 {
				return p, ErrNeedsUnpack // locked headers
			}
		case 0x74: // file header
			if pos+32 > len(b) {
				return p, errors.New("a RAR header past the first article")
			}
			pack := int64(binary.LittleEndian.Uint32(b[pos+7:]))
			unp := int64(binary.LittleEndian.Uint32(b[pos+11:]))
			method := b[pos+25]
			nameLen := int(binary.LittleEndian.Uint16(b[pos+26:]))
			nameAt := pos + 32
			if flags&0x0100 != 0 { // sizes over 4 GB
				if pos+40 > len(b) {
					return p, errors.New("a RAR header past the first article")
				}
				pack |= int64(binary.LittleEndian.Uint32(b[pos+32:])) << 32
				unp |= int64(binary.LittleEndian.Uint32(b[pos+36:])) << 32
				nameAt = pos + 40
			}
			if nameAt+nameLen > len(b) {
				return p, errors.New("a RAR header past the first article")
			}
			name := b[nameAt : nameAt+nameLen]
			if i := strings.IndexByte(string(name), 0); i >= 0 {
				name = name[:i] // the plain name, before the Unicode one
			}
			if flags&0x0004 != 0 || method != 0x30 {
				return p, ErrNeedsUnpack
			}
			p.inner, p.innerSize = string(name), unp
			p.dataOff, p.dataSize = int64(pos+size), pack
			p.splitAfter = flags&0x0002 != 0
			return p, nil
		}
		add := 0
		if flags&0x8000 != 0 && pos+11 <= len(b) {
			add = int(binary.LittleEndian.Uint32(b[pos+7:]))
		}
		pos += size + add
	}
	return p, errors.New("no file in the RAR volume's first article")
}

// vint reads a RAR5 variable-length number.
func vint(b []byte, pos int) (uint64, int, error) {
	var v uint64
	for i := 0; i < 10 && pos+i < len(b); i++ {
		c := b[pos+i]
		v |= uint64(c&0x7f) << (7 * i)
		if c&0x80 == 0 {
			return v, pos + i + 1, nil
		}
	}
	return 0, pos, errors.New("a damaged RAR5 number")
}

func parseRAR5(b []byte) (rarPiece, error) {
	p := rarPiece{volume: -1}
	pos := len(rar5Sig)
	for pos+6 < len(b) {
		start := pos
		pos += 4 // header CRC
		hsize, next, err := vint(b, pos)
		if err != nil {
			return p, err
		}
		hstart := next
		hend := hstart + int(hsize)
		if hend > len(b) {
			return p, errors.New("a RAR header past the first article")
		}
		typ, q, err := vint(b, hstart)
		if err != nil {
			return p, err
		}
		hflags, q, err := vint(b, q)
		if err != nil {
			return p, err
		}
		var extra, dataSize uint64
		if hflags&0x0001 != 0 {
			if extra, q, err = vint(b, q); err != nil {
				return p, err
			}
		}
		if hflags&0x0002 != 0 {
			if dataSize, q, err = vint(b, q); err != nil {
				return p, err
			}
		}
		switch typ {
		case 4: // the archive's headers are locked
			return p, ErrNeedsUnpack
		case 1: // archive header
			aflags, r, err := vint(b, q)
			if err != nil {
				return p, err
			}
			if aflags&0x0001 != 0 { // a volume
				p.volume = 0
				if aflags&0x0002 != 0 {
					n, _, err := vint(b, r)
					if err != nil {
						return p, err
					}
					p.volume = int(n)
				}
			}
		case 2: // file header
			fflags, r, err := vint(b, q)
			if err != nil {
				return p, err
			}
			unp, r, err := vint(b, r)
			if err != nil {
				return p, err
			}
			if _, r, err = vint(b, r); err != nil { // attributes
				return p, err
			}
			if fflags&0x0002 != 0 {
				r += 4 // modification time
			}
			if fflags&0x0004 != 0 {
				r += 4 // data CRC
			}
			comp, r, err := vint(b, r)
			if err != nil {
				return p, err
			}
			if _, r, err = vint(b, r); err != nil { // host OS
				return p, err
			}
			nameLen, r, err := vint(b, r)
			if err != nil || r+int(nameLen) > hend {
				return p, errors.New("a damaged RAR5 file header")
			}
			name := string(b[r : r+int(nameLen)])
			// Extra records: an encryption record means the file is locked.
			if extra > 0 {
				e := hend - int(extra)
				for e < hend {
					rsize, x, err := vint(b, e)
					if err != nil {
						break
					}
					rtype, _, err := vint(b, x)
					if err == nil && rtype == 0x01 {
						return p, ErrNeedsUnpack
					}
					e = x + int(rsize)
				}
			}
			if (comp>>7)&7 != 0 {
				return p, ErrNeedsUnpack // compressed
			}
			p.inner, p.innerSize = name, int64(unp)
			p.dataOff, p.dataSize = int64(hend), int64(dataSize)
			p.splitAfter = hflags&0x0010 != 0
			return p, nil
		}
		pos = hend + int(dataSize)
		if pos <= start {
			return p, errors.New("a damaged RAR5 header")
		}
	}
	return p, errors.New("no file in the RAR volume's first article")
}

// rar4VolumeNumber reads a RAR4 volume's number from its end-of-archive
// block (in the volume's last bytes), or -1.
func rar4VolumeNumber(tail []byte) int {
	for p := len(tail) - 7; p >= 0; p-- {
		if tail[p+2] != 0x7b {
			continue
		}
		size := int(binary.LittleEndian.Uint16(tail[p+5:]))
		if size < 7 || p+size > len(tail) || p+size < len(tail)-16 {
			continue
		}
		flags := binary.LittleEndian.Uint16(tail[p+3:])
		at := p + 7
		if flags&0x0002 != 0 {
			at += 4
		}
		if flags&0x0008 != 0 && at+2 <= p+size {
			return int(binary.LittleEndian.Uint16(tail[at:]))
		}
	}
	return -1
}

var (
	partVol = regexp.MustCompile(`(?i)\.part0*(\d+)\.rar$`)
	oldVol  = regexp.MustCompile(`(?i)\.r(\d\d)$`)
	sVol    = regexp.MustCompile(`(?i)\.s(\d\d)$`)
)

// volumeOrder ranks a volume by its name: name.part01.rar, or name.rar then
// name.r00, .r01 … .r99, .s00 ….
func volumeOrder(name string) (int, bool) {
	if m := partVol.FindStringSubmatch(name); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n, true
	}
	if strings.HasSuffix(strings.ToLower(name), ".rar") {
		return 0, true
	}
	if m := oldVol.FindStringSubmatch(name); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n + 1, true
	}
	if m := sVol.FindStringSubmatch(name); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n + 101, true
	}
	return 0, false
}

// rarStream is the biggest video stored in the release's RAR volumes.
func rarStream(opened []*nzbStream) (*StreamFile, error) {
	type vol struct {
		s     *nzbStream
		piece rarPiece
		order int
		named bool
	}
	byInner := map[string][]vol{}
	var refused error
	for _, s := range opened {
		head := make([]byte, min(int64(64<<10), s.size))
		n, err := s.readAt(context.Background(), head, 0, false)
		if n == 0 && err != nil {
			continue
		}
		piece, err := parseRARHead(head[:n])
		if err != nil {
			if errors.Is(err, ErrNeedsUnpack) {
				refused = err
			}
			continue
		}
		o, named := volumeOrder(s.name)
		if piece.volume < 0 && !named && head[len(rar4Sig)-1] == 0 {
			// Names that say nothing (an obfuscated post): RAR4 volumes say
			// their number at their end.
			tail := make([]byte, min(int64(256), s.size))
			if m, _ := s.readAt(context.Background(), tail, s.size-int64(len(tail)), false); m == len(tail) {
				piece.volume = rar4VolumeNumber(tail)
			}
		}
		byInner[piece.inner] = append(byInner[piece.inner], vol{s: s, piece: piece, order: o, named: named})
	}
	var best *StreamFile
	for inner, vols := range byInner {
		if !isVideoName(inner) {
			continue
		}
		// In order: by the archive's own volume numbers (RAR5), else by name.
		sort.SliceStable(vols, func(a, b int) bool {
			va, vb := vols[a].piece.volume, vols[b].piece.volume
			if va >= 0 && vb >= 0 && va != vb {
				return va < vb
			}
			return vols[a].order < vols[b].order
		})
		f := &StreamFile{Name: inner, Size: vols[0].piece.innerSize}
		var off int64
		for _, v := range vols {
			f.spans = append(f.spans, span{src: v.s, srcOff: v.piece.dataOff, off: off, n: v.piece.dataSize})
			off += v.piece.dataSize
		}
		if off != f.Size {
			continue // volumes missing, or out of order
		}
		if best == nil || f.Size > best.Size {
			best = f
		}
	}
	if best == nil {
		if refused != nil {
			return nil, refused
		}
		return nil, fmt.Errorf("no stored video in the release's archives")
	}
	return best, nil
}
