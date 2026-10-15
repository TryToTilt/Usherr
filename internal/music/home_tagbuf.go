package music

// Home fork: dhowden/tag reads an ID3v2 tag that uses unsynchronisation one
// byte at a time. Straight from a file that is one read call per byte: a
// 364 KB embedded cover is 364,000 calls, and on a slow folder mount (Docker
// Desktop's Windows folders) a single album took hours, which looked like a
// hung import. readSeekBuffer serves those small reads from a 64 KB buffer.

import (
	"errors"
	"io"
)

const tagBufSize = 64 << 10

// readSeekBuffer is a buffered io.ReadSeeker over an io.ReaderAt of a known
// size. Seeking only moves the position; a buffer still covering the new
// position is reused.
type readSeekBuffer struct {
	r     io.ReaderAt
	size  int64
	pos   int64
	buf   []byte
	bufAt int64 // file offset of buf[0]
}

func newReadSeekBuffer(r io.ReaderAt, size int64) *readSeekBuffer {
	return &readSeekBuffer{r: r, size: size, bufAt: -1}
}

func (b *readSeekBuffer) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if b.pos >= b.size {
		return 0, io.EOF
	}
	if b.bufAt < 0 || b.pos < b.bufAt || b.pos >= b.bufAt+int64(len(b.buf)) {
		if b.buf == nil {
			b.buf = make([]byte, tagBufSize)
		}
		b.buf = b.buf[:cap(b.buf)]
		n, err := b.r.ReadAt(b.buf, b.pos)
		if n == 0 {
			b.bufAt = -1
			if err == nil {
				err = io.ErrUnexpectedEOF
			}
			return 0, err
		}
		b.buf, b.bufAt = b.buf[:n], b.pos
	}
	n := copy(p, b.buf[b.pos-b.bufAt:])
	b.pos += int64(n)
	return n, nil
}

func (b *readSeekBuffer) Seek(offset int64, whence int) (int64, error) {
	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = b.pos + offset
	case io.SeekEnd:
		abs = b.size + offset
	default:
		return 0, errors.New("seek: invalid whence")
	}
	if abs < 0 {
		return 0, errors.New("seek: negative position")
	}
	b.pos = abs
	return abs, nil
}

// IsVariousArtists reports whether an artist (or folder) name stands for
// compilations by many artists ("Various Artists", "VA"). Home fork: the
// music import skips these folders.
func IsVariousArtists(name string) bool { return variousArtists(name) }
