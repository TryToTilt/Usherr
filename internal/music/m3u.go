package music

import (
	"bytes"
	"net/url"
	"path"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/unicode"
)

// Reading .m3u and .m3u8 playlists. The format is loose: one entry per line,
// paths relative to the playlist or absolute (often from another computer,
// with Windows drive letters and backslashes), optional #EXTINF lines with the
// length and "Artist - Title", and files saved in UTF-8, UTF-16 or the old
// Windows code page. ParseM3U reads all of these; it never opens the files.

// MaxM3UBytes caps how much of a playlist file is read.
const MaxM3UBytes = 2 << 20

// M3UEntry is one entry of a playlist.
type M3UEntry struct {
	Line     int    // the line it was on, from 1
	Path     string // as written, without a file:// prefix
	Artist   string // from #EXTINF, "" when not given
	Title    string // from #EXTINF, "" when not given
	Duration int    // seconds from #EXTINF, -1 when not given
}

// Display is how the entry is shown in a report: "Artist - Title" when the
// playlist said so, else the file name.
func (e M3UEntry) Display() string {
	switch {
	case e.Artist != "" && e.Title != "":
		return e.Artist + " - " + e.Title
	case e.Title != "":
		return e.Title
	}
	return path.Base(slashed(e.Path))
}

// M3U is a parsed playlist. Name is from a #PLAYLIST line, "" when it has none.
type M3U struct {
	Name    string
	Entries []M3UEntry
}

// DecodeText turns the bytes of a text file into a string: UTF-8 (with or
// without a byte order mark), UTF-16 with a byte order mark, or, when the
// bytes aren't valid UTF-8, Windows-1252 (which Windows tools used for .m3u).
func DecodeText(b []byte) string {
	switch {
	case bytes.HasPrefix(b, []byte{0xEF, 0xBB, 0xBF}):
		return strings.ToValidUTF8(string(b[3:]), "\uFFFD")
	case bytes.HasPrefix(b, []byte{0xFF, 0xFE}):
		if s, err := unicode.UTF16(unicode.LittleEndian, unicode.ExpectBOM).NewDecoder().Bytes(b); err == nil {
			return string(s)
		}
	case bytes.HasPrefix(b, []byte{0xFE, 0xFF}):
		if s, err := unicode.UTF16(unicode.BigEndian, unicode.ExpectBOM).NewDecoder().Bytes(b); err == nil {
			return string(s)
		}
	}
	if utf8.Valid(b) {
		return string(b)
	}
	if s, err := charmap.Windows1252.NewDecoder().Bytes(b); err == nil {
		return string(s)
	}
	return strings.ToValidUTF8(string(b), "\uFFFD")
}

// ParseM3U reads a playlist. Lines that are web addresses are left out (a
// library playlist names files).
func ParseM3U(b []byte) M3U {
	if len(b) > MaxM3UBytes {
		b = b[:MaxM3UBytes]
	}
	text := DecodeText(b)
	var out M3U
	var pending *M3UEntry // the #EXTINF waiting for its file line
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		line = strings.TrimPrefix(line, "\ufeff")
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			upper := strings.ToUpper(line)
			switch {
			case strings.HasPrefix(upper, "#EXTINF:"):
				e := parseExtinf(line[len("#EXTINF:"):])
				pending = &e
			case strings.HasPrefix(upper, "#PLAYLIST:"):
				out.Name = strings.TrimSpace(line[len("#PLAYLIST:"):])
			}
			continue
		}
		p, ok := entryPath(line)
		if !ok {
			pending = nil
			continue
		}
		e := M3UEntry{Duration: -1}
		if pending != nil {
			e = *pending
			pending = nil
		}
		e.Line, e.Path = i+1, p
		out.Entries = append(out.Entries, e)
	}
	return out
}

// parseExtinf reads what follows "#EXTINF:": the length, optional key="value"
// attributes, a comma, then "Artist - Title" or just a title.
func parseExtinf(s string) M3UEntry {
	e := M3UEntry{Duration: -1}
	comma := commaOutsideQuotes(s)
	head, name := s, ""
	if comma >= 0 {
		head, name = s[:comma], strings.TrimSpace(s[comma+1:])
	}
	if f := strings.Fields(head); len(f) > 0 {
		if d, err := strconv.ParseFloat(f[0], 64); err == nil && d >= 0 {
			e.Duration = int(d + 0.5)
		}
	}
	if a, t, ok := strings.Cut(name, " - "); ok && strings.TrimSpace(a) != "" && strings.TrimSpace(t) != "" {
		e.Artist, e.Title = strings.TrimSpace(a), strings.TrimSpace(t)
	} else {
		e.Title = name
	}
	return e
}

func commaOutsideQuotes(s string) int {
	quoted := false
	for i, c := range s {
		switch c {
		case '"':
			quoted = !quoted
		case ',':
			if !quoted {
				return i
			}
		}
	}
	return -1
}

// entryPath cleans an entry line: a file:// address becomes its path; any
// other address is not a file and ok is false.
func entryPath(line string) (string, bool) {
	lower := strings.ToLower(line)
	if strings.HasPrefix(lower, "file:") {
		u, err := url.Parse(line)
		if err != nil {
			return "", false
		}
		p := u.Path
		if u.Host != "" && u.Host != "localhost" {
			p = `\\` + u.Host + p // a network share
		}
		// file:///C:/Music/x.flac has the drive after the first slash.
		if len(p) >= 3 && p[0] == '/' && p[2] == ':' {
			p = p[1:]
		}
		return p, p != ""
	}
	if i := strings.Index(lower, "://"); i > 0 && !strings.ContainsAny(lower[:i], `/\`) {
		return "", false
	}
	return line, true
}

// slashed turns backslashes into forward slashes.
func slashed(p string) string { return strings.ReplaceAll(p, `\`, "/") }

// IsAbsM3UPath reports whether an entry's path is absolute on some computer:
// "/music/x.flac", "C:\Music\x.flac", "C:/Music/x.flac" or "\\server\share\x.flac".
func IsAbsM3UPath(p string) bool {
	s := slashed(p)
	if strings.HasPrefix(s, "/") {
		return true
	}
	return len(s) >= 3 && s[1] == ':' && s[2] == '/' && isLetter(s[0])
}

func isLetter(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }

// ResolveM3UPath is where an entry points on this computer, in forward
// slashes: a relative path is joined to the playlist's folder (given with
// forward slashes); an absolute one is only cleaned. A Windows path from
// another computer can't be found as it is; MatchM3U also matches by the end
// of the path.
func ResolveM3UPath(playlistDir, p string) string {
	s := slashed(p)
	if IsAbsM3UPath(s) {
		return path.Clean(s)
	}
	return path.Join(playlistDir, s)
}

// PathTail is the last n parts of a path, lower-cased and joined with "/".
// It is "" when the path has fewer than n parts.
func PathTail(p string, n int) string {
	parts := strings.Split(strings.Trim(slashed(p), "/"), "/")
	if len(parts) < n || n <= 0 {
		return ""
	}
	return strings.ToLower(strings.Join(parts[len(parts)-n:], "/"))
}
