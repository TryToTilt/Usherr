package books

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// The real length of an audiobook's files, and the one timeline they make
// together. A saved place stays "track:seconds", a spot in one real file, so
// a place saved before the lengths were known means exactly the same spot
// afterwards; only the percent, once an estimate from file sizes, becomes
// true (see RefreshAudioPercents).

// MaxLengthTries is how many times an unreadable file is tried before it is
// left alone until it changes.
const MaxLengthTries = 3

// AudioProbe is what reading one audio file gives: its length in seconds, and
// its tags with lower-case names.
type AudioProbe struct {
	Seconds float64
	Tags    map[string]string
}

// AudioProber reads one audio file (ffprobe in the app, a fake in tests).
type AudioProber func(ctx context.Context, path string) (AudioProbe, error)

// AudioLength is the stored length of one file of a book.
type AudioLength struct {
	Rel     string
	Size    int64
	ModNs   int64
	Seconds float64
	Tries   int
}

// Fresh reports whether this was read from the file as it is now.
func (l AudioLength) Fresh(size, modNs int64) bool { return l.Size == size && l.ModNs == modNs }

// Settled reports whether there is nothing more to try for this version of
// the file.
func (l AudioLength) Settled() bool { return l.Seconds > 0 || l.Tries >= MaxLengthTries }

// AudioLengths returns a book's stored lengths by file.
func (r *Repo) AudioLengths(bookID int64) (map[string]AudioLength, error) {
	rows, err := r.db.Query(`SELECT rel, size, mtime_ns, seconds, tries FROM book_audio_lengths WHERE book_id = ?`, bookID)
	if err != nil {
		return nil, fmt.Errorf("read audiobook lengths: %w", err)
	}
	defer rows.Close()
	out := map[string]AudioLength{}
	for rows.Next() {
		var l AudioLength
		if err := rows.Scan(&l.Rel, &l.Size, &l.ModNs, &l.Seconds, &l.Tries); err != nil {
			return nil, fmt.Errorf("read audiobook lengths: %w", err)
		}
		out[l.Rel] = l
	}
	return out, rows.Err()
}

// SaveAudioLength stores a file's length.
func (r *Repo) SaveAudioLength(bookID int64, l AudioLength) error {
	_, err := r.db.Exec(`INSERT INTO book_audio_lengths (book_id, rel, size, mtime_ns, seconds, tries) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (book_id, rel) DO UPDATE SET size = excluded.size, mtime_ns = excluded.mtime_ns, seconds = excluded.seconds, tries = excluded.tries`,
		bookID, l.Rel, l.Size, l.ModNs, l.Seconds, l.Tries)
	if err != nil {
		return fmt.Errorf("save audiobook length: %w", err)
	}
	return nil
}

// ForgetAudioLength removes a file that is no longer part of the book.
func (r *Repo) ForgetAudioLength(bookID int64, rel string) error {
	if _, err := r.db.Exec(`DELETE FROM book_audio_lengths WHERE book_id = ? AND rel = ?`, bookID, rel); err != nil {
		return fmt.Errorf("forget audiobook length: %w", err)
	}
	return nil
}

// FillAudioRuntime sets the book's running time when it has none.
func (r *Repo) FillAudioRuntime(bookID int64, seconds float64) error {
	if _, err := r.db.Exec(`UPDATE books SET runtime_min = ? WHERE id = ? AND runtime_min = 0`, int(math.Round(seconds/60)), bookID); err != nil {
		return fmt.Errorf("fill audiobook runtime: %w", err)
	}
	return nil
}

// AudioTimeline is the real length of each file of a book, in playing order.
type AudioTimeline []float64

// Complete reports whether every file's length is known.
func (t AudioTimeline) Complete() bool {
	for _, s := range t {
		if s <= 0 {
			return false
		}
	}
	return len(t) > 0
}

// Total is the length of the whole book.
func (t AudioTimeline) Total() float64 {
	sum := 0.0
	for _, s := range t {
		sum += s
	}
	return sum
}

// Absolute is how far into the whole book a place in one file is. A place past
// the end of its file (a file's length can differ by a moment between readers)
// counts as that file's end, never as the next file.
func (t AudioTimeline) Absolute(track int, seconds float64) float64 {
	if len(t) == 0 {
		return 0
	}
	track = min(max(track, 0), len(t)-1)
	at := 0.0
	for _, s := range t[:track] {
		at += s
	}
	return at + min(max(seconds, 0), t[track])
}

// Locate is the file and the place in it at a point of the whole book.
func (t AudioTimeline) Locate(at float64) (track int, seconds float64) {
	if len(t) == 0 {
		return 0, 0
	}
	at = max(at, 0)
	for i, s := range t {
		if at < s || i == len(t)-1 {
			return i, min(at, s)
		}
		at -= s
	}
	return len(t) - 1, t[len(t)-1]
}

var audioPosition = regexp.MustCompile(`^(\d+):(\d+(?:\.\d+)?)$`)

// ParseAudioPosition reads a saved place, "track:seconds".
func ParseAudioPosition(s string) (track int, seconds float64, ok bool) {
	m := audioPosition.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, 0, false
	}
	track, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, 0, false
	}
	seconds, err = strconv.ParseFloat(m[2], 64)
	if err != nil {
		return 0, 0, false
	}
	return track, seconds, true
}

// ConvertAudioPosition works out how far through the whole book a saved place
// is, in percent. ok is false when it can't be told: the place can't be read,
// its file is no longer there, or a length is missing. Then the saved percent
// stays as it is.
func ConvertAudioPosition(position string, t AudioTimeline) (percent float64, ok bool) {
	track, seconds, ok := ParseAudioPosition(position)
	if !ok || track >= len(t) || !t.Complete() {
		return 0, false
	}
	return math.Round(t.Absolute(track, seconds)/t.Total()*1000) / 10, true
}

// RefreshAudioPercents makes everyone's saved percent in a book true, now its
// timeline is known. The places themselves don't change, and neither does when
// they were saved, so a place kept offline elsewhere is still judged the same
// way. A finished book stays finished.
func (r *Repo) RefreshAudioPercents(bookID int64, t AudioTimeline) (int, error) {
	if !t.Complete() {
		return 0, nil
	}
	type row struct {
		user     int64
		position string
	}
	rows, err := r.db.Query(`SELECT user_id, position FROM book_progress WHERE book_id = ? AND format = ? AND finished = 0`, bookID, string(Audiobook))
	if err != nil {
		return 0, fmt.Errorf("read audiobook places: %w", err)
	}
	var list []row
	for rows.Next() {
		var x row
		if err := rows.Scan(&x.user, &x.position); err != nil {
			rows.Close()
			return 0, fmt.Errorf("read audiobook places: %w", err)
		}
		list = append(list, x)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, fmt.Errorf("read audiobook places: %w", err)
	}
	n := 0
	for _, x := range list {
		pct, ok := ConvertAudioPosition(x.position, t)
		if !ok {
			continue
		}
		// Only if that place is still the saved one: a save may have come in meanwhile.
		res, err := r.db.Exec(`UPDATE book_progress SET percent = ? WHERE user_id = ? AND book_id = ? AND format = ? AND position = ?`,
			pct, x.user, bookID, string(Audiobook), x.position)
		if err != nil {
			return n, fmt.Errorf("save audiobook percent: %w", err)
		}
		if c, _ := res.RowsAffected(); c > 0 {
			n++
		}
	}
	return n, nil
}
