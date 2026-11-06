package api

// Usherr: music converted while streaming, for phones on cellular. A request
// may ask for a format (opus, aac or mp3) and a top bitrate; the file is sent
// as it is when it already fits. Otherwise ffmpeg converts it once into a
// small cache, which is then served with ranges, so players can seek and know
// the length. A Subsonic timeOffset is converted on the fly from that point.
//
// The decision follows DroppedNeedle (AGPL-3.0),
// backend/services/compat/transcode_service.py: convert only when the format
// differs or the client's cap is under the source; the server's cap is a
// ceiling, never a reason to convert; at least 64 kbit/s. The ffmpeg command
// lines and the limits on conversions at once follow it too
// (services/compat/stream_concurrency.py). AAC and the seekable cache are
// Usherr's own.

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/mediafiles"
	"github.com/rdborg/mediarium/internal/music"
)

const (
	keyTranscodeOn      = "music.transcode"        // "0" stops converting music while streaming
	keyTranscodeMaxKbps = "music.transcode_max"    // the server's top bitrate for a conversion (kbit/s)
	keyTranscodeFormat  = "music.transcode_format" // the format used when a request names none: opus, aac or mp3
	keyTranscodeCacheMB = "music.transcode_cache"  // how big the cache of converted files may grow (MB)
	defaultTranscodeMax = 320
	defaultCacheMB      = 2048
	minTranscodeKbps    = 64
	transcodesAtOnce    = 3 // conversions running together on the server
	transcodesPerUser   = 2
	transcodeWait       = 20 * time.Second
	transcodeTimeout    = 5 * time.Minute
)

// transcodeFormats are the formats a track can be converted to.
var transcodeFormats = map[string]struct {
	codec, muxer, ext, contentType string
}{
	"opus": {"libopus", "ogg", "opus", "audio/ogg"},
	"aac":  {"aac", "adts", "aac", "audio/aac"},
	"mp3":  {"libmp3lame", "mp3", "mp3", "audio/mpeg"},
}

// sourceFormat is the transcode format name a file already is, "" for none.
func sourceFormat(codec, ext string) string {
	switch {
	case codec == "opus" || ext == ".opus":
		return "opus"
	case codec == "mp3" || ext == ".mp3":
		return "mp3"
	case codec == "aac" && (ext == ".aac" || ext == ".m4a"):
		return "aac"
	}
	return ""
}

// transcodePlan is what is sent: the file as it is (Format ""), or converted.
type transcodePlan struct {
	Format string
	Kbps   int
}

// decideTranscode picks what to send. requested is the client's format ("",
// "raw" for the original, or opus/aac/mp3; anything else means the server's
// default), clientMax its top bitrate (0 = none), srcKbps the file's bitrate
// (0 when unknown), serverMax the server's ceiling and defaultFormat the
// format used when converting for the bitrate alone.
func decideTranscode(requested string, clientMax int, srcFormat string, srcKbps int, lossless bool, serverMax int, defaultFormat string) transcodePlan {
	requested = strings.ToLower(strings.TrimSpace(requested))
	if requested == "raw" {
		return transcodePlan{}
	}
	if _, ok := transcodeFormats[requested]; requested != "" && !ok {
		requested = defaultFormat
	}
	formatDiffers := requested != "" && requested != srcFormat
	// A lossless file has no small bitrate to keep; an unknown one counts as large.
	tooBig := clientMax > 0 && (lossless || srcKbps <= 0 || srcKbps > clientMax)
	if !formatDiffers && !tooBig {
		return transcodePlan{}
	}
	format := requested
	if format == "" {
		format = defaultFormat
	}
	kbps := serverMax
	if clientMax > 0 && clientMax < kbps {
		kbps = clientMax
	}
	if srcKbps > 0 && !lossless && srcKbps < kbps {
		kbps = srcKbps // converting can't add what the source doesn't have
	}
	if kbps < minTranscodeKbps {
		kbps = minTranscodeKbps
	}
	return transcodePlan{Format: format, Kbps: kbps}
}

// ffmpegTranscodeArgs is the conversion, written to out ("pipe:1" for a stream).
func ffmpegTranscodeArgs(in string, offsetSec float64, format string, kbps int, out string) []string {
	f := transcodeFormats[format]
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin"}
	if offsetSec > 0 {
		args = append(args, "-ss", strconv.FormatFloat(offsetSec, 'f', 3, 64))
	}
	args = append(args, "-i", in, "-vn", "-map", "0:a:0", "-map_metadata", "-1", "-c:a", f.codec, "-b:a", strconv.Itoa(kbps)+"k")
	if format == "opus" {
		args = append(args, "-vbr", "on")
	}
	return append(args, "-f", f.muxer, "-y", out)
}

type transcodeSettings struct {
	On      bool   `json:"transcode"`
	MaxKbps int    `json:"transcodeMaxKbps"`
	Format  string `json:"transcodeFormat"`
	CacheMB int    `json:"transcodeCacheMB"`
}

func (s *Server) transcodeSettings() transcodeSettings {
	on, _ := s.Settings.Get(keyTranscodeOn)
	st := transcodeSettings{On: on != "0", MaxKbps: defaultTranscodeMax, Format: "opus", CacheMB: defaultCacheMB}
	if v, _ := s.Settings.Get(keyTranscodeMaxKbps); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 32 && n <= 1411 {
			st.MaxKbps = n
		}
	}
	if v, _ := s.Settings.Get(keyTranscodeFormat); v != "" {
		if _, ok := transcodeFormats[v]; ok {
			st.Format = v
		}
	}
	if v, _ := s.Settings.Get(keyTranscodeCacheMB); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 100 {
			st.CacheMB = n
		}
	}
	return st
}

// transcodeGate limits conversions running at once, overall and per account.
type transcodeGate struct {
	mu       sync.Mutex
	running  int
	perUser  map[int64]int
	inFlight map[string]chan struct{} // cache key -> closed when done
}

var transcodes = &transcodeGate{perUser: map[int64]int{}, inFlight: map[string]chan struct{}{}}

func (g *transcodeGate) acquire(ctx context.Context, userID int64) bool {
	deadline := time.Now().Add(transcodeWait)
	for {
		g.mu.Lock()
		if g.running < transcodesAtOnce && g.perUser[userID] < transcodesPerUser {
			g.running++
			g.perUser[userID]++
			g.mu.Unlock()
			return true
		}
		g.mu.Unlock()
		if time.Now().After(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (g *transcodeGate) release(userID int64) {
	g.mu.Lock()
	g.running--
	if g.perUser[userID]--; g.perUser[userID] <= 0 {
		delete(g.perUser, userID)
	}
	g.mu.Unlock()
}

func (s *Server) transcodeCacheDir() string {
	return filepath.Join(s.cfg.ConfigDir, "music-transcodes")
}

// errTranscodeBusy is returned when too many conversions are running.
var errTranscodeBusy = errors.New("too many conversions running")

// cachedTranscode converts a file once into the cache and returns the cached
// file's path. Two requests for the same conversion share one ffmpeg.
func (s *Server) cachedTranscode(ctx context.Context, userID int64, src string, info os.FileInfo, plan transcodePlan) (string, error) {
	h := sha1.Sum([]byte(fmt.Sprintf("%s|%d|%d|%s|%d", src, info.Size(), info.ModTime().UnixNano(), plan.Format, plan.Kbps)))
	key := hex.EncodeToString(h[:])
	dir := s.transcodeCacheDir()
	out := filepath.Join(dir, key[:2], key+"."+transcodeFormats[plan.Format].ext)
	for {
		if _, err := os.Stat(out); err == nil {
			now := time.Now()
			_ = os.Chtimes(out, now, now) // recently used, kept longest
			return out, nil
		}
		transcodes.mu.Lock()
		wait, busy := transcodes.inFlight[key]
		if !busy {
			wait = make(chan struct{})
			transcodes.inFlight[key] = wait
		}
		transcodes.mu.Unlock()
		if !busy {
			break
		}
		select {
		case <-wait:
			continue // done (or failed): look again
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	defer func() {
		transcodes.mu.Lock()
		close(transcodes.inFlight[key])
		delete(transcodes.inFlight, key)
		transcodes.mu.Unlock()
	}()
	if !transcodes.acquire(ctx, userID) {
		return "", errTranscodeBusy
	}
	defer transcodes.release(userID)
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return "", err
	}
	tmp := out + ".part"
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), transcodeTimeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, "ffmpeg", ffmpegTranscodeArgs(src, 0, plan.Format, plan.Kbps, tmp)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("ffmpeg: %s", firstLine(strings.TrimSpace(stderr.String())))
	}
	if err := os.Rename(tmp, out); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	go s.pruneTranscodeCache()
	return out, nil
}

var pruneMu sync.Mutex

// pruneTranscodeCache removes the least recently used conversions once the
// cache is over its size.
func (s *Server) pruneTranscodeCache() {
	if !pruneMu.TryLock() {
		return
	}
	defer pruneMu.Unlock()
	limit := int64(s.transcodeSettings().CacheMB) << 20
	type entry struct {
		path string
		size int64
		at   time.Time
	}
	var all []entry
	var total int64
	_ = filepath.WalkDir(s.transcodeCacheDir(), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		if strings.HasSuffix(p, ".part") && time.Since(fi.ModTime()) > time.Hour {
			_ = os.Remove(p)
			return nil
		}
		all = append(all, entry{p, fi.Size(), fi.ModTime()})
		total += fi.Size()
		return nil
	})
	if total <= limit {
		return
	}
	sort.Slice(all, func(i, j int) bool { return all[i].at.Before(all[j].at) })
	for _, e := range all {
		if total <= limit*9/10 {
			break
		}
		if os.Remove(e.path) == nil {
			total -= e.size
		}
	}
}

// streamQuery is what a request asks of the stream.
type streamQuery struct {
	Format    string  // "", raw, opus, aac or mp3: convert to this
	Prefer    string  // the format to use if the bitrate calls for converting (the web player)
	MaxKbps   int     // 0 = no cap
	OffsetSec float64 // start here (converted streams only)
	Estimate  bool    // send an estimated Content-Length with a live conversion
}

// serveTrackStream sends a track's file (rel inside folder), converted when
// the query and the file call for it.
func (s *Server) serveTrackStream(w http.ResponseWriter, r *http.Request, userID int64, folder mediafiles.Folder, rel string, q streamQuery, durationSec float64) {
	st := s.transcodeSettings()
	plan := transcodePlan{}
	abs := filepath.Join(folder.Dir, filepath.FromSlash(rel))
	if st.On && (q.Format != "" || q.MaxKbps > 0) {
		// Opened through the folder first: that refuses anything leading out of it.
		f, _, err := folder.Open(rel)
		if !s.openedOK(w, err) {
			return
		}
		f.Close()
		ai, err := music.ReadAudioInfo(abs)
		if err != nil {
			ai = music.AudioInfo{}
		}
		ext := strings.ToLower(filepath.Ext(rel))
		def := st.Format
		if _, ok := transcodeFormats[q.Prefer]; ok {
			def = q.Prefer
		}
		plan = decideTranscode(q.Format, q.MaxKbps, sourceFormat(ai.Codec, ext), ai.BitrateKbps, ai.Lossless || ai.Codec == "flac" || ext == ".flac" || ext == ".wav", st.MaxKbps, def)
		if durationSec <= 0 {
			durationSec = ai.DurationSec
		}
	}
	if plan.Format == "" {
		s.serveOriginal(w, r, folder, rel)
		return
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		s.serveOriginal(w, r, folder, rel) // no ffmpeg here: the file as it is
		return
	}
	ct := transcodeFormats[plan.Format].contentType
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Usherr-Transcode", fmt.Sprintf("%s %dk", plan.Format, plan.Kbps))
	if q.OffsetSec > 0 {
		s.liveTranscode(w, r, userID, abs, plan, q, durationSec)
		return
	}
	info, err := os.Stat(abs)
	if err != nil {
		writeError(w, http.StatusNotFound, "This track's file is missing.")
		return
	}
	out, err := s.cachedTranscode(r.Context(), userID, abs, info, plan)
	if errors.Is(err, errTranscodeBusy) {
		w.Header().Set("Retry-After", "2")
		writeError(w, http.StatusServiceUnavailable, "Too many tracks are being converted right now. Try again in a moment.")
		return
	}
	if err != nil {
		if r.Context().Err() == nil {
			slog.Warn("music: convert for streaming", "file", rel, "err", err)
			writeError(w, http.StatusInternalServerError, "Couldn't convert this track. Check the log for details.")
		}
		return
	}
	f, err := os.Open(out)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Couldn't read the converted track.")
		return
	}
	defer f.Close()
	fi, _ := f.Stat()
	h.Set("Content-Type", ct)
	http.ServeContent(w, r, "", fi.ModTime(), f)
}

// liveTranscode converts from an offset straight to the response.
func (s *Server) liveTranscode(w http.ResponseWriter, r *http.Request, userID int64, abs string, plan transcodePlan, q streamQuery, durationSec float64) {
	if !transcodes.acquire(r.Context(), userID) {
		w.Header().Set("Retry-After", "2")
		writeError(w, http.StatusServiceUnavailable, "Too many tracks are being converted right now. Try again in a moment.")
		return
	}
	defer transcodes.release(userID)
	h := w.Header()
	h.Set("Content-Type", transcodeFormats[plan.Format].contentType)
	h.Set("Accept-Ranges", "none")
	h.Set("Cache-Control", "no-store")
	if q.Estimate && durationSec > q.OffsetSec {
		h.Set("Content-Length", strconv.FormatInt(int64(float64(plan.Kbps)*1000/8*(durationSec-q.OffsetSec)), 10))
	}
	cmd := exec.CommandContext(r.Context(), "ffmpeg", ffmpegTranscodeArgs(abs, q.OffsetSec, plan.Format, plan.Kbps, "pipe:1")...)
	cmd.Stdout = flushWriter{w: w}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil && r.Context().Err() == nil {
		slog.Info("music: live conversion", "err", err, "ffmpeg", firstLine(stderr.String()))
	}
}
