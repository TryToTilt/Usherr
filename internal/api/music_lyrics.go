package api

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/lrclib"
	"github.com/rdborg/mediarium/internal/mediafiles"
	"github.com/rdborg/mediarium/internal/music"
	"github.com/rdborg/mediarium/internal/settings"
)

// Lyrics, ReplayGain and the player settings that go with them.
//
// Lyrics come from, in this order: a .lrc file with the same name next to the
// track, the lyrics written in the file's tags, and then LRCLIB (lrclib.net).
// The online answer is kept in the database so a track is asked about once.
// Synced lyrics (with times) win over plain ones, wherever they come from.

// keyedLocks serialises work per id, so two people opening the lyrics of the
// same track make one request to LRCLIB.
type keyedLocks struct {
	mu sync.Mutex
	m  map[int64]*sync.Mutex
}

func (k *keyedLocks) lock(id int64) func() {
	k.mu.Lock()
	if k.m == nil || len(k.m) > 1000 {
		k.m = map[int64]*sync.Mutex{}
	}
	l := k.m[id]
	if l == nil {
		l = &sync.Mutex{}
		k.m[id] = l
	}
	k.mu.Unlock()
	l.Lock()
	return l.Unlock
}

// lyricsOnline is whether the player may ask LRCLIB for lyrics. On until an
// administrator turns it off.
func (s *Server) lyricsOnline() bool {
	v, _ := s.Settings.Get(settings.KeyMusicLyricsOnline)
	return v != "0"
}

func (s *Server) lyricsClient() *lrclib.Client {
	s.music.playerMu.Lock()
	defer s.music.playerMu.Unlock()
	if s.music.lyrics == nil {
		s.music.lyrics = lrclib.New(s.version)
	}
	return s.music.lyrics
}

// trackFile finds a track's file inside the music folder, answering the
// request itself when it can't.
func (s *Server) trackFile(w http.ResponseWriter, r *http.Request) (music.Track, mediafiles.Folder, string, bool) {
	id, ok := musicID(w, r, "track")
	if !ok {
		return music.Track{}, mediafiles.Folder{}, "", false
	}
	t, err := s.MusicRepo.GetTrack(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That track isn't in your library.")
		return music.Track{}, mediafiles.Folder{}, "", false
	}
	if err != nil {
		slog.Warn("read track", "track", id, "err", err)
		writeError(w, http.StatusInternalServerError, "Couldn't read that track. Check the log for details.")
		return music.Track{}, mediafiles.Folder{}, "", false
	}
	if t.FilePath == "" {
		writeError(w, http.StatusNotFound, "This track hasn't been downloaded yet.")
		return music.Track{}, mediafiles.Folder{}, "", false
	}
	folder, err := mediafiles.ForFile(t.FilePath, []string{s.musicRoot()}, false)
	if err != nil {
		writeStreamFolderError(w, err)
		return music.Track{}, mediafiles.Folder{}, "", false
	}
	rel, ok := folder.Rel(t.FilePath)
	if !ok {
		writeError(w, http.StatusForbidden, "This track's file isn't inside your music folder.")
		return music.Track{}, mediafiles.Folder{}, "", false
	}
	return t, folder, rel, true
}

type lyricsJSON struct {
	Source       string `json:"source"` // "sidecar", "file", "lrclib" or "" when there are none
	Synced       string `json:"synced"` // LRC text, "" when there is none
	Plain        string `json:"plain"`
	Instrumental bool   `json:"instrumental"`
	Online       bool   `json:"online"` // whether the online lookup is switched on
}

// fileLyrics reads the .lrc beside the track, then the tags of the file.
func fileLyrics(folder mediafiles.Folder, rel string) music.Lyrics {
	if f, _, err := folder.Open(music.SidecarName(rel)); err == nil {
		l, rerr := music.SidecarLyrics(f)
		f.Close()
		if rerr != nil {
			slog.Debug("read lyrics file", "err", rerr)
		} else if l.Has() {
			return l
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		slog.Debug("open lyrics file", "err", err)
	}
	f, _, err := folder.Open(rel)
	if err != nil {
		return music.Lyrics{}
	}
	defer f.Close()
	l, err := music.EmbeddedLyrics(f)
	if err != nil {
		slog.Debug("read lyrics tags", "err", err)
	}
	return l
}

// GET /api/music/tracks/{id}/lyrics
func (s *Server) handleTrackLyrics(w http.ResponseWriter, r *http.Request) {
	t, folder, rel, ok := s.trackFile(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, s.trackLyrics(r.Context(), t, folder, rel))
}

// trackLyrics is what a track has: its own lyrics, else (when switched on)
// LRCLIB's. The web player and Subsonic apps both use it.
func (s *Server) trackLyrics(ctx context.Context, t music.Track, folder mediafiles.Folder, rel string) lyricsJSON {
	online := s.lyricsOnline()
	out := fileLyrics(folder, rel)
	// Words from the file are kept, but synced ones found online are better
	// than plain ones in the file.
	if online && out.Synced == "" {
		if got, ok := s.onlineLyrics(ctx, t); ok {
			switch {
			case got.Synced != "":
				if out.Plain == "" {
					out.Plain = got.Plain
				}
				out.Synced, out.Source = got.Synced, got.Source
				s.saveLRC(folder, rel, got.Synced) // Usherr: only when switched on
			case out.Plain == "" && got.Plain != "":
				out.Plain, out.Source = got.Plain, got.Source
			case !out.Has() && got.Instrumental:
				out.Instrumental, out.Source = true, got.Source
			}
		}
	}
	return lyricsJSON{Source: out.Source, Synced: out.Synced, Plain: out.Plain, Instrumental: out.Instrumental, Online: online}
}

// onlineLyrics returns what LRCLIB has for a track, from the database when it
// was asked before. ok is false when nothing was found or the service could
// not be reached (the latter is not remembered, so the next try asks again).
func (s *Server) onlineLyrics(ctx context.Context, t music.Track) (music.Lyrics, bool) {
	unlock := s.music.lyricsAt.lock(t.ID)
	defer unlock()
	if c, ok, err := s.MusicRepo.GetCachedLyrics(t.ID); err != nil {
		slog.Warn("read cached lyrics", "track", t.ID, "err", err)
	} else if ok && c.Fresh(time.Now()) {
		return c.Lyrics, c.Found
	}
	album, err := s.MusicRepo.GetAlbum(t.AlbumID)
	if err != nil {
		return music.Lyrics{}, false
	}
	artist, err := s.MusicRepo.GetArtist(album.ArtistID)
	if err != nil {
		return music.Lyrics{}, false
	}
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	res, err := s.lyricsClient().Lookup(cctx, t.Title, artist.Name, album.Title, (t.LengthMs+500)/1000)
	if err != nil {
		slog.Info("online lyrics lookup failed", "track", t.ID, "err", err)
		return music.Lyrics{}, false
	}
	l := music.Lyrics{Source: "lrclib", Synced: res.Synced, Plain: res.Plain, Instrumental: res.Instrumental}
	if err := s.MusicRepo.PutCachedLyrics(t.ID, l, res.Found); err != nil {
		slog.Warn("keep lyrics", "track", t.ID, "err", err)
	}
	return l, res.Found
}

// GET /api/music/tracks/{id}/gain: the ReplayGain tags of a track's file, null
// for the ones it doesn't have.
func (s *Server) handleTrackGain(w http.ResponseWriter, r *http.Request) {
	_, folder, rel, ok := s.trackFile(w, r)
	if !ok {
		return
	}
	f, _, err := folder.Open(rel)
	if !s.openedOK(w, err) {
		return
	}
	defer f.Close()
	g, err := music.ReadReplayGain(f)
	if err != nil {
		// A file whose tags can't be read just plays at its own level.
		slog.Debug("read replaygain tags", "err", err)
		g = music.ReplayGain{}
	}
	w.Header().Set("Cache-Control", "private, max-age=3600")
	writeJSON(w, http.StatusOK, g)
}

// keyLyricsSaveLRC (Usherr): "1" writes synced lyrics found online as a .lrc
// file next to the track, so other apps see them too. Off unless switched on.
const keyLyricsSaveLRC = "music.lyrics_save_lrc"

func (s *Server) lyricsSaveLRC() bool {
	v, _ := s.Settings.Get(keyLyricsSaveLRC)
	return v == "1"
}

// saveLRC writes synced lyrics beside a track when that is switched on and
// there is no .lrc there yet. It never replaces a file.
func (s *Server) saveLRC(folder mediafiles.Folder, rel, synced string) {
	if !s.lyricsSaveLRC() || !music.IsSyncedLyrics(synced) {
		return
	}
	p := filepath.Join(folder.Dir, filepath.FromSlash(music.SidecarName(rel)))
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if !errors.Is(err, fs.ErrExist) {
			slog.Info("music: save lyrics file", "file", music.SidecarName(rel), "err", err)
		}
		return
	}
	_, werr := f.WriteString(strings.TrimSpace(synced) + "\n")
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		_ = os.Remove(p)
		slog.Info("music: save lyrics file", "file", music.SidecarName(rel), "err", werr)
		return
	}
}

type playerSettingsJSON struct {
	LyricsOnline  bool `json:"lyricsOnline"`
	LyricsSaveLRC bool `json:"lyricsSaveLrc"`
	Subsonic      bool `json:"subsonic"` // phone apps may sign in (each person still switches it on)
	transcodeSettings
}

func (s *Server) playerSettings() playerSettingsJSON {
	return playerSettingsJSON{LyricsOnline: s.lyricsOnline(), LyricsSaveLRC: s.lyricsSaveLRC(), Subsonic: s.subsonicOn(), transcodeSettings: s.transcodeSettings()}
}

// GET /api/music/player-settings
func (s *Server) handleGetPlayerSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.playerSettings())
}

// PUT /api/music/player-settings {lyricsOnline?, lyricsSaveLrc?, transcode?,
// transcodeMaxKbps?, transcodeFormat?, transcodeCacheMB?}
func (s *Server) handlePutPlayerSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		LyricsOnline     *bool   `json:"lyricsOnline"`
		LyricsSaveLRC    *bool   `json:"lyricsSaveLrc"`
		Subsonic         *bool   `json:"subsonic"`
		Transcode        *bool   `json:"transcode"`
		TranscodeMaxKbps *int    `json:"transcodeMaxKbps"`
		TranscodeFormat  *string `json:"transcodeFormat"`
		TranscodeCacheMB *int    `json:"transcodeCacheMB"`
	}
	if err := decodeJSON(r, &req); err != nil || (req.LyricsOnline == nil && req.LyricsSaveLRC == nil && req.Subsonic == nil && req.Transcode == nil &&
		req.TranscodeMaxKbps == nil && req.TranscodeFormat == nil && req.TranscodeCacheMB == nil) {
		writeError(w, http.StatusBadRequest, "Say which player setting to change.")
		return
	}
	if req.TranscodeMaxKbps != nil && (*req.TranscodeMaxKbps < 32 || *req.TranscodeMaxKbps > 1411) {
		writeError(w, http.StatusBadRequest, "The top bitrate is between 32 and 1411 kbit/s.")
		return
	}
	if req.TranscodeFormat != nil {
		if _, ok := transcodeFormats[*req.TranscodeFormat]; !ok {
			writeError(w, http.StatusBadRequest, "The format is opus, aac or mp3.")
			return
		}
	}
	if req.TranscodeCacheMB != nil && (*req.TranscodeCacheMB < 100 || *req.TranscodeCacheMB > 1<<20) {
		writeError(w, http.StatusBadRequest, "The cache is at least 100 MB.")
		return
	}
	flag := func(b bool) string {
		if b {
			return "1"
		}
		return "0"
	}
	var changes [][2]string
	if req.LyricsOnline != nil {
		changes = append(changes, [2]string{settings.KeyMusicLyricsOnline, flag(*req.LyricsOnline)})
	}
	if req.Subsonic != nil {
		changes = append(changes, [2]string{keySubsonicOn, flag(*req.Subsonic)})
	}
	if req.LyricsSaveLRC != nil {
		changes = append(changes, [2]string{keyLyricsSaveLRC, flag(*req.LyricsSaveLRC)})
	}
	if req.Transcode != nil {
		changes = append(changes, [2]string{keyTranscodeOn, flag(*req.Transcode)})
	}
	if req.TranscodeMaxKbps != nil {
		changes = append(changes, [2]string{keyTranscodeMaxKbps, strconv.Itoa(*req.TranscodeMaxKbps)})
	}
	if req.TranscodeFormat != nil {
		changes = append(changes, [2]string{keyTranscodeFormat, *req.TranscodeFormat})
	}
	if req.TranscodeCacheMB != nil {
		changes = append(changes, [2]string{keyTranscodeCacheMB, strconv.Itoa(*req.TranscodeCacheMB)})
	}
	for _, c := range changes {
		if err := s.Settings.Set(c[0], c[1], false); err != nil {
			slog.Warn("save player setting", "err", err)
			writeError(w, http.StatusInternalServerError, "Couldn't save the setting. Check the log for details.")
			return
		}
	}
	writeJSON(w, http.StatusOK, s.playerSettings())
}
