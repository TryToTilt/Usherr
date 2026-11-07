package api

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/books"
	"github.com/rdborg/mediarium/internal/musictidy"
)

// Reading the real length of each audiobook file with ffprobe, so a book has
// one true timeline. Only the files' headers are read, one at a time, and what
// is read is stored at once: a book opened in the player is read right away,
// and everything else a little at a time at night, carrying on after a restart
// where it stopped.

const (
	lengthsFromHour = 2 // the night job's hours, local time (the house rule for whole-library work)
	lengthsToHour   = 9
	lengthsEvery    = 30 * time.Minute // how often the job looks; a run stays shorter than this
	lengthsRunFor   = 20 * time.Minute
	lengthsPause    = 250 * time.Millisecond // between files, so the disk has room for others
	lengthsNightGap = 20 * time.Hour         // a finished pass isn't repeated in the same night

	keyAudioLengthsCursor = "books.audio_lengths_cursor" // the last book a pass finished
	keyAudioLengthsDone   = "books.audio_lengths_done"   // when a whole pass last finished
)

// probeAudioFile and audioProberReady are ffprobe; tests swap them for fakes.
var (
	probeAudioFile books.AudioProber = func(ctx context.Context, path string) (books.AudioProbe, error) {
		p, err := musictidy.ProbeFile(ctx, path)
		if err != nil {
			return books.AudioProbe{}, err
		}
		return books.AudioProbe{Seconds: p.DurationSec, Tags: p.Tags}, nil
	}
	audioProberReady = func() bool {
		_, err := exec.LookPath(musictidy.FFprobe)
		return err == nil
	}
)

// lengthProbes holds the books being read on demand, so opening a book twice
// reads it once.
var lengthProbes sync.Map

// audioFile is one file of an audiobook, in playing order.
type audioFile struct {
	rel   string
	full  string
	size  int64
	modNs int64
	link  bool
	there bool
}

func (s *Server) audioFilesOf(b books.Book) ([]audioFile, error) {
	folder, rels, err := s.audiobookTracks(b)
	if err != nil {
		return nil, err
	}
	out := make([]audioFile, 0, len(rels))
	for _, rel := range rels {
		f := audioFile{rel: rel, full: filepath.Join(folder.Dir, filepath.FromSlash(rel))}
		if st, err := os.Stat(f.full); err == nil {
			f.size, f.modNs, f.there = st.Size(), st.ModTime().UnixNano(), true
		}
		if st, err := os.Lstat(f.full); err == nil {
			f.link = st.Mode()&os.ModeSymlink != 0
		}
		out = append(out, f)
	}
	return out, nil
}

// audioFileSeconds is a file's stored length, 0 when it isn't known for the
// file as it is now.
func audioFileSeconds(stored map[string]books.AudioLength, f audioFile) float64 {
	if l, ok := stored[f.rel]; ok && l.Fresh(f.size, f.modNs) {
		return l.Seconds
	}
	return 0
}

// lengthsMissing reports whether any file still has a length to be read.
func lengthsMissing(stored map[string]books.AudioLength, files []audioFile) bool {
	for _, f := range files {
		if !f.there {
			continue
		}
		if l, ok := stored[f.rel]; !ok || !l.Fresh(f.size, f.modNs) || !l.Settled() {
			return true
		}
	}
	return false
}

type lengthsOptions struct {
	stop      func() bool   // asked before each file: time to stop for now
	pause     time.Duration // between files
	skipLinks bool          // leave links alone: they can lead to a mount that reads over the network
}

// readBookLengths reads and stores the lengths still missing from a book. It
// returns false when it stopped before going through all the files.
func (s *Server) readBookLengths(ctx context.Context, b books.Book, opt lengthsOptions) bool {
	files, err := s.audioFilesOf(b)
	if err != nil {
		return true // the folder isn't there: nothing to read, and no reason to stop the others
	}
	stored, err := s.BookRepo.AudioLengths(b.ID)
	if err != nil {
		slog.Warn("books: read stored lengths", "book", b.ID, "err", err)
		return true
	}
	changed := false
	wanted := make(map[string]bool, len(files))
	for _, f := range files {
		wanted[f.rel] = true
		old, have := stored[f.rel]
		if !f.there || (have && old.Fresh(f.size, f.modNs) && old.Settled()) || (opt.skipLinks && f.link) {
			continue
		}
		if ctx.Err() != nil || (opt.stop != nil && opt.stop()) {
			return false
		}
		p, err := probeAudioFile(ctx, f.full)
		if ctx.Err() != nil {
			return false // cut short: says nothing about the file
		}
		l := books.AudioLength{Rel: f.rel, Size: f.size, ModNs: f.modNs, Seconds: p.Seconds}
		if err != nil || p.Seconds <= 0 {
			l.Seconds, l.Tries = 0, 1
			if have && old.Fresh(f.size, f.modNs) {
				l.Tries = old.Tries + 1
			}
			slog.Info("books: can't read an audiobook file's length", "book", b.ID, "file", filepath.Base(f.full), "err", err)
		}
		if err := s.BookRepo.SaveAudioLength(b.ID, l); err != nil {
			slog.Warn("books: save audiobook length", "book", b.ID, "err", err)
			return true
		}
		stored[f.rel] = l
		changed = true
		if opt.pause > 0 {
			select {
			case <-ctx.Done():
				return false
			case <-time.After(opt.pause):
			}
		}
	}
	for rel := range stored {
		if !wanted[rel] {
			_ = s.BookRepo.ForgetAudioLength(b.ID, rel)
		}
	}
	if changed {
		timeline := make(books.AudioTimeline, len(files))
		for i, f := range files {
			timeline[i] = audioFileSeconds(stored, f)
		}
		if timeline.Complete() {
			if _, err := s.BookRepo.RefreshAudioPercents(b.ID, timeline); err != nil {
				slog.Warn("books: refresh saved places", "book", b.ID, "err", err)
			}
			if err := s.BookRepo.FillAudioRuntime(b.ID, timeline.Total()); err != nil {
				slog.Warn("books: fill audiobook runtime", "book", b.ID, "err", err)
			}
		}
	}
	return true
}

// readLengthsSoon reads a book's missing lengths now, in the background: for a
// book someone has just opened, whatever the hour.
func (s *Server) readLengthsSoon(b books.Book) {
	if _, busy := lengthProbes.LoadOrStore(b.ID, true); busy {
		return
	}
	s.background(func() {
		defer lengthProbes.Delete(b.ID)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		s.readBookLengths(ctx, b, lengthsOptions{})
	})
}

// lengthsDue reports whether the night's pass may run at now: inside the night
// hours, and not finished already tonight.
func lengthsDue(lastDone string, now time.Time) bool {
	if now.Hour() < lengthsFromHour || now.Hour() >= lengthsToHour {
		return false
	}
	if t, err := time.Parse(time.RFC3339, lastDone); err == nil && now.Sub(t) < lengthsNightGap {
		return false
	}
	return true
}

// audiobookLengthsJob goes through the library at night, a run at a time.
func (s *Server) audiobookLengthsJob(ctx context.Context) {
	if !s.audiobooksEnabled() || !audioProberReady() {
		return
	}
	last, _ := s.Settings.Get(keyAudioLengthsDone)
	if !lengthsDue(last, time.Now()) {
		return
	}
	until := time.Now().Add(lengthsRunFor)
	s.audiobookLengthsPass(ctx, func() bool {
		now := time.Now()
		_, quiet := s.quietUntil()
		return quiet || now.After(until) || now.Hour() < lengthsFromHour || now.Hour() >= lengthsToHour
	}, lengthsPause)
}

// audiobookLengthsPass reads the books after the last one finished, in order,
// until stop says so. The place is kept, so the next run carries on from there
// (also after a restart). It returns true when it reached the end.
func (s *Server) audiobookLengthsPass(ctx context.Context, stop func() bool, pause time.Duration) bool {
	list, err := s.BookRepo.List()
	if err != nil {
		slog.Warn("books: audiobook lengths", "err", err)
		return false
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	v, _ := s.Settings.Get(keyAudioLengthsCursor)
	cursor, _ := strconv.ParseInt(v, 10, 64)
	opt := lengthsOptions{stop: stop, pause: pause, skipLinks: true}
	for _, b := range list {
		if b.ID <= cursor || b.AudioPath == "" || b.AudioStatus != books.StatusDownloaded {
			continue
		}
		if !s.readBookLengths(ctx, b, opt) {
			return false
		}
		if err := s.Settings.Set(keyAudioLengthsCursor, strconv.FormatInt(b.ID, 10), false); err != nil {
			slog.Warn("books: audiobook lengths", "err", err)
		}
	}
	_ = s.Settings.Set(keyAudioLengthsCursor, "", false)
	_ = s.Settings.Set(keyAudioLengthsDone, time.Now().UTC().Format(time.RFC3339), false)
	return true
}
