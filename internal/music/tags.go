package music

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/dhowden/tag"
)

// Reading the tags of one file is capped, so a damaged file can't hold up a
// whole import. A FLAC file that is a header followed by zeros (an
// interrupted copy, a preallocated download) looks like millions of empty
// metadata blocks, and the tag reader walks them all, a few bytes at a time.
// Real tags, cover picture included, sit in the first few MB of a file, and a
// FLAC metadata block is never bigger than 16 MB.
var (
	// TagReadLimit is the most bytes read from one file while looking for
	// its tags.
	TagReadLimit int64 = 16 << 20
	// TagReadTimeout is the most time spent reading one file's tags.
	TagReadTimeout = 5 * time.Second
)

// ErrTagReadLimit is returned when a file's tags weren't found within
// TagReadLimit bytes or TagReadTimeout. The file is then identified by its
// name alone.
var ErrTagReadLimit = errors.New("tags not found within the read limit")

// FileTagReader reads the tags embedded in audio files (ID3, Vorbis
// comments, MP4 atoms) with github.com/dhowden/tag. It only ever reads:
// nothing in a user's files is written or rewritten.
type FileTagReader struct{}

// ReadTags implements TagReader. A file without tags is not an error
// (ok is false); a damaged one is reported as one, and the caller falls
// back to the file name.
func (FileTagReader) ReadTags(path string) (info FileInfo, ok bool, err error) {
	// A tag reader parses whatever bytes it is given; never let a malformed
	// file bring the import down.
	defer func() {
		if r := recover(); r != nil {
			info, ok, err = FileInfo{}, false, fmt.Errorf("read tags of %s: unreadable tag data", path)
		}
	}()
	f, err := os.Open(path)
	if err != nil {
		return FileInfo{}, false, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	r, err := newCappedReader(f, TagReadLimit, time.Now().Add(TagReadTimeout))
	if err != nil {
		return FileInfo{}, false, fmt.Errorf("read tags of %s: %w", path, err)
	}
	m, err := tag.ReadFrom(r)
	if r.hitLimit {
		return FileInfo{}, false, fmt.Errorf("read tags of %s: %w", path, ErrTagReadLimit)
	}
	if errors.Is(err, tag.ErrNoTagsFound) {
		return FileInfo{}, false, nil
	}
	if err != nil {
		return FileInfo{}, false, fmt.Errorf("read tags of %s: %w", path, err)
	}
	track, _ := m.Track()
	disc, _ := m.Disc()
	info = FileInfo{
		Path: path, Disc: disc, Track: track,
		Title:       strings.TrimSpace(m.Title()),
		Artist:      strings.TrimSpace(m.Artist()),
		AlbumArtist: strings.TrimSpace(m.AlbumArtist()),
		Album:       strings.TrimSpace(m.Album()),
		Year:        m.Year(),
		HasPicture:  m.Picture() != nil,
		FileType:    string(m.FileType()),
	}
	return info, true, nil
}

// cappedReader reads a file through a 64 KB buffer (the tag reader asks for
// a few bytes at a time, which over a network share is one round trip each),
// and fails once it has read more than limit bytes or the deadline has
// passed.
type cappedReader struct {
	f        io.ReaderAt
	size     int64
	pos      int64
	read     int64 // bytes read from the file so far
	limit    int64
	deadline time.Time
	buf      []byte
	bufAt    int64 // file offset of buf[0]
	hitLimit bool
}

func newCappedReader(f *os.File, limit int64, deadline time.Time) (*cappedReader, error) {
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	return &cappedReader{f: f, size: st.Size(), limit: limit, deadline: deadline}, nil
}

func (c *cappedReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if c.pos >= c.size {
		return 0, io.EOF
	}
	if c.pos < c.bufAt || c.pos >= c.bufAt+int64(len(c.buf)) {
		if c.read >= c.limit || time.Now().After(c.deadline) {
			c.hitLimit = true
			return 0, ErrTagReadLimit
		}
		if c.buf == nil {
			c.buf = make([]byte, 64<<10)
		}
		c.buf = c.buf[:cap(c.buf)]
		n, err := c.f.ReadAt(c.buf, c.pos)
		c.buf, c.bufAt = c.buf[:n], c.pos
		c.read += int64(n)
		if n == 0 {
			if err == nil {
				err = io.EOF
			}
			return 0, err
		}
	}
	n := copy(p, c.buf[c.pos-c.bufAt:])
	c.pos += int64(n)
	return n, nil
}

func (c *cappedReader) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		offset += c.pos
	case io.SeekEnd:
		offset += c.size
	default:
		return c.pos, errors.New("seek: bad whence")
	}
	if offset < 0 {
		return c.pos, errors.New("seek: negative position")
	}
	c.pos = offset
	return c.pos, nil
}

// variousArtists names the album artist compilations carry.
func variousArtists(name string) bool {
	switch NormalizeName(name) {
	case "variousartists", "various", "va", "compilation":
		return true
	}
	return false
}

// TagConsensus is what most of an album's files say about it. Empty or
// zero fields are ones the files do not agree on (or do not have).
type TagConsensus struct {
	Artist string // the album artist, else the track artist; never "Various Artists"
	Album  string
	Year   int
}

// ConsensusOf reads the album, artist and year most of the files agree on:
// a value counts only when more than half of the files that have one say
// it. The album artist tag wins over the track artist (a compilation's
// tracks each have their own artist).
func ConsensusOf(infos []FileInfo) TagConsensus {
	pick := func(values []string) string {
		count := map[string]int{}
		spelling := map[string]string{}
		n := 0
		for _, v := range values {
			if strings.TrimSpace(v) == "" {
				continue
			}
			n++
			k := NormalizeName(v)
			count[k]++
			if _, ok := spelling[k]; !ok {
				spelling[k] = strings.TrimSpace(v)
			}
		}
		for k, c := range count {
			if c*2 > n {
				return spelling[k]
			}
		}
		return ""
	}
	albumArtists := make([]string, 0, len(infos))
	artists := make([]string, 0, len(infos))
	albums := make([]string, 0, len(infos))
	years := make([]string, 0, len(infos))
	for _, in := range infos {
		albumArtists = append(albumArtists, in.AlbumArtist)
		artists = append(artists, in.Artist)
		albums = append(albums, in.Album)
		if in.Year > 0 {
			years = append(years, fmt.Sprint(in.Year))
		}
	}
	c := TagConsensus{Album: pick(albums)}
	if a := pick(albumArtists); a != "" && !variousArtists(a) {
		c.Artist = a
	} else if a == "" {
		if t := pick(artists); !variousArtists(t) {
			c.Artist = t
		}
	}
	fmt.Sscanf(pick(years), "%d", &c.Year)
	return c
}
