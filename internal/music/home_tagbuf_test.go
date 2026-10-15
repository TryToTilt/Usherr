package music

import (
	"bytes"
	"io"
	"testing"

	"github.com/dhowden/tag"
)

type countingReaderAt struct {
	r     *bytes.Reader
	calls int
}

func (c *countingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	c.calls++
	return c.r.ReadAt(p, off)
}

// unsyncID3 builds an ID3v2.3 tag with the unsynchronisation flag set, a
// title and a large picture frame, followed by fake audio.
func unsyncID3(picture int) []byte {
	frame := func(id string, body []byte) []byte {
		h := []byte(id)
		n := len(body)
		h = append(h, byte(n>>24), byte(n>>16), byte(n>>8), byte(n), 0, 0)
		return append(h, body...)
	}
	title := frame("TIT2", append([]byte{0}, "Share the Land"...))
	pic := append([]byte{0}, "image/jpeg"...)
	pic = append(pic, 0, 3, 0)
	pic = append(pic, bytes.Repeat([]byte{0x11}, picture)...)
	frames := append(title, frame("APIC", pic)...)
	size := len(frames)
	hdr := []byte{'I', 'D', '3', 3, 0, 0x80, byte(size >> 21 & 0x7f), byte(size >> 14 & 0x7f), byte(size >> 7 & 0x7f), byte(size & 0x7f)}
	out := append(hdr, frames...)
	return append(out, []byte("fLaC")...)
}

func TestTagsReadThroughBufferInFewCalls(t *testing.T) {
	data := unsyncID3(300 << 10)
	c := &countingReaderAt{r: bytes.NewReader(data)}
	m, err := tag.ReadFrom(newReadSeekBuffer(c, int64(len(data))))
	if err != nil {
		t.Fatalf("read tags: %v", err)
	}
	if m.Title() != "Share the Land" {
		t.Errorf("title = %q", m.Title())
	}
	if c.calls > 50 {
		t.Errorf("%d reads of the file for a 300 KB tag; the buffer should make it a handful", c.calls)
	}
}

func TestReadSeekBufferBehavesLikeAFile(t *testing.T) {
	data := make([]byte, 200<<10)
	for i := range data {
		data[i] = byte(i * 7)
	}
	b := newReadSeekBuffer(bytes.NewReader(data), int64(len(data)))
	if _, err := b.Seek(-128, io.SeekEnd); err != nil {
		t.Fatal(err)
	}
	tail := make([]byte, 128)
	if _, err := io.ReadFull(b, tail); err != nil || !bytes.Equal(tail, data[len(data)-128:]) {
		t.Fatalf("tail read wrong: %v", err)
	}
	if n, err := b.Read(tail); n != 0 || err != io.EOF {
		t.Errorf("read past end = %d, %v", n, err)
	}
	if _, err := b.Seek(100<<10, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	all, err := io.ReadAll(b)
	if err != nil || !bytes.Equal(all, data[100<<10:]) {
		t.Errorf("read from the middle wrong: %v", err)
	}
	if _, err := b.Seek(-1, io.SeekStart); err == nil {
		t.Error("negative seek accepted")
	}
}

func TestIsVariousArtists(t *testing.T) {
	for _, n := range []string{"Various Artists", "VA", "various"} {
		if !IsVariousArtists(n) {
			t.Errorf("%q not recognised", n)
		}
	}
	if IsVariousArtists("The Guess Who") {
		t.Error("a real artist counted as compilations")
	}
}
