package api

// Usherr: Read-to-Me's Chatterbox controls and its copied voices.
//
// How Chatterbox reads (rtvTune): emotion (exaggeration), pacing (cfg weight),
// variety (temperature) and a seed, so the same text is read the same way
// again. They go with a job, a preview and a preset. Turbo has no emotion or
// pacing dial; it takes the variety and the seed.
//
// Copied voices: a short recording uploaded in Usherr, made into a clean mono
// WAV and kept by Usherr. Chatterbox reads in that voice once it has the
// recording; Usherr gives it to whichever model runs (standard or Turbo) the
// first time the voice is used there, so a voice can be picked while
// Chatterbox is stopped. (Chatterbox quietly reads in its own voice when it
// doesn't know a name, so it is never left to guess.)
//
// A WAV put in the voices folder (config/readtome/voices) by hand is taken in
// as a copied voice named after the file, the next time the voices are listed;
// taking the file out again deletes that voice.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// rtvTune is how Chatterbox reads; a field left out is its own usual way.
type rtvTune struct {
	Exaggeration *float64 `json:"exaggeration,omitempty"` // emotion, 0.25 to 2 (usual 0.5)
	Cfg          *float64 `json:"cfg,omitempty"`          // pacing, 0 to 1 (usual 0.5; lower is slower)
	Temperature  *float64 `json:"temperature,omitempty"`  // variety, 0.05 to 2 (usual 0.8)
	Seed         int64    `json:"seed,omitempty"`         // the same take every time; 0: a new one
}

// clamped keeps the dials within what Chatterbox takes; nothing set is nil.
func (t *rtvTune) clamped() *rtvTune {
	if t == nil {
		return nil
	}
	c := *t
	keep := func(p *float64, lo, hi float64) *float64 {
		if p == nil || math.IsNaN(*p) {
			return nil
		}
		v := math.Round(math.Max(lo, math.Min(hi, *p))*100) / 100
		return &v
	}
	c.Exaggeration = keep(c.Exaggeration, 0.25, 2)
	c.Cfg = keep(c.Cfg, 0, 1)
	c.Temperature = keep(c.Temperature, 0.05, 2)
	if c.Seed < 0 || c.Seed > math.MaxInt32 {
		c.Seed = 0
	}
	if c.Exaggeration == nil && c.Cfg == nil && c.Temperature == nil && c.Seed == 0 {
		return nil
	}
	return &c
}

// tuned is the engine reading the way t says (Kokoro has no such dials).
func tuned(e rtvEngine, t *rtvTune) rtvEngine {
	if c, ok := e.(chatterboxEngine); ok {
		c.tune = t
		return c
	}
	return e
}

// ---- copied voices ----

const (
	cbVoiceMax     = 25 << 20 // the largest recording taken
	cbVoiceMinSecs = 5.0
	cbVoiceKeep    = 20 // seconds of a recording kept (Chatterbox needs about ten)
)

var cbVoiceName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 _.-]{0,39}$`)

// Names Chatterbox takes as its own voice (OpenAI's), and its own voice.
var cbTakenNames = map[string]bool{"default": true, "alloy": true, "echo": true, "fable": true, "onyx": true, "nova": true, "shimmer": true}

func (s *Server) cbVoiceDir() string { return filepath.Join(s.cfg.ConfigDir, "readtome", "voices") }

func (s *Server) cbVoiceFile(name string) string { return filepath.Join(s.cbVoiceDir(), name+".wav") }

type copiedVoice struct {
	Name      string  `json:"name"`
	Seconds   float64 `json:"seconds"`
	CreatedMs int64   `json:"createdMs"`
	CanChange bool    `json:"canChange"` // may delete it
	userID    int64
}

// copiedVoices are the voices Usherr keeps (deleted: the ones deleted).
func (s *Server) copiedVoices(ctx context.Context, deleted bool) []copiedVoice {
	if !deleted {
		s.takeInVoiceFilesSoon()
	}
	out := []copiedVoice{}
	rows, err := s.db.QueryContext(ctx, `SELECT name, seconds, created_ms, user_id FROM readtome_voices WHERE deleted = ? ORDER BY name COLLATE NOCASE`, deleted)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var v copiedVoice
		if rows.Scan(&v.Name, &v.Seconds, &v.CreatedMs, &v.userID) == nil {
			out = append(out, v)
		}
	}
	return out
}

var takingIn sync.Mutex

// takeInVoiceFiles makes a copied voice of each WAV put in the voices folder
// by hand: cleaned like an upload, named after the file, deletable by an
// administrator. (A deleted voice's file is removed, so a file there is new.)
// One taken in this way goes again when its file is taken out.
//
// It runs in the background, apart from the request that set it off: cleaning
// dozens of files takes longer than a page waits, and a request cut off half
// way mustn't leave files marked unusable. The voices appear as they are done.
func (s *Server) takeInVoiceFilesSoon() {
	if !takingIn.TryLock() {
		return // already at it
	}
	go func() {
		defer takingIn.Unlock()
		s.takeInVoiceFiles(context.Background())
	}()
}

func (s *Server) takeInVoiceFiles(ctx context.Context) {
	// A voice taken in from the folder whose file was taken out is deleted.
	if rows, err := s.db.QueryContext(ctx, `SELECT name FROM readtome_voices WHERE deleted = 0 AND user_id = 0`); err == nil {
		var gone []string
		for rows.Next() {
			var name string
			if rows.Scan(&name) == nil {
				if _, err := os.Stat(s.cbVoiceFile(name)); errors.Is(err, os.ErrNotExist) {
					gone = append(gone, name)
				}
			}
		}
		rows.Close()
		for _, name := range gone {
			if _, err := s.db.ExecContext(ctx, `UPDATE readtome_voices SET deleted = 1, given = '' WHERE name = ?`, name); err == nil {
				s.forgetVoiceSamples(name)
				forgetVoices()
				slog.Info("read-to-me: a voice's file was taken out; the voice is deleted", "name", name)
			}
		}
	}
	files, _ := filepath.Glob(filepath.Join(s.cbVoiceDir(), "*.wav"))
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".wav")
		if strings.HasPrefix(name, "upload-") || !cbVoiceName.MatchString(name) || cbTakenNames[strings.ToLower(name)] || s.isCopiedVoice(ctx, name) {
			continue
		}
		clean := f + ".clean"
		cctx, cancel := context.WithTimeout(ctx, time.Minute)
		secs, err := cleanRecording(cctx, f, clean+".wav")
		cancel()
		if err != nil || secs < cbVoiceMinSecs || os.Rename(clean+".wav", s.cbVoiceFile(name)) != nil {
			_ = os.Remove(clean + ".wav")
			_ = os.Rename(f, f+".unusable") // not tried again
			slog.Warn("read-to-me: a voice file couldn't be taken in", "file", f, "err", err, "seconds", secs)
			continue
		}
		_, err = s.db.ExecContext(ctx, `INSERT INTO readtome_voices (name, seconds, given, deleted, user_id, created_ms) VALUES (?, ?, '', 0, 0, ?)
			ON CONFLICT(name) DO UPDATE SET name = excluded.name, seconds = excluded.seconds, given = '', deleted = 0, user_id = 0, created_ms = excluded.created_ms`,
			name, secs, time.Now().UnixMilli())
		if err == nil {
			s.forgetVoiceSamples(name)
			forgetVoices()
			slog.Info("read-to-me: took in a voice file", "name", name)
		}
	}
}

func copiedVoiceOf(name string) rtvVoice {
	return rtvVoice{Engine: engineChatterbox, ID: name, Name: name, Label: name + " · copied voice", Note: "expressive, slower"}
}

// withCopied is Chatterbox's voices as Usherr shows them: what it lists (less
// the voices deleted in Usherr) and every voice Usherr keeps.
func (s *Server) withCopied(ctx context.Context, vs []rtvVoice) []rtvVoice {
	gone := map[string]bool{}
	for _, v := range s.copiedVoices(ctx, true) {
		gone[strings.ToLower(v.Name)] = true
	}
	out := []rtvVoice{}
	seen := map[string]bool{}
	for _, v := range vs {
		if v.Engine == engineChatterbox && gone[strings.ToLower(v.ID)] {
			continue
		}
		seen[strings.ToLower(v.ID)] = v.Engine == engineChatterbox
		out = append(out, v)
	}
	for _, v := range s.copiedVoices(ctx, false) {
		if !seen[strings.ToLower(v.Name)] {
			out = append(out, copiedVoiceOf(v.Name))
		}
	}
	return out
}

// isCopiedVoice says whether Usherr keeps a voice by that name.
func (s *Server) isCopiedVoice(ctx context.Context, name string) bool {
	var n int
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM readtome_voices WHERE name = ? AND deleted = 0`, name).Scan(&n)
	return n > 0
}

// giveVoice makes sure the Chatterbox answering at base has a copied voice's
// recording before it reads with it. A voice Usherr doesn't keep is left alone.
func (s *Server) giveVoice(ctx context.Context, base, voice string) error {
	if voice == "" || voice == "default" {
		return nil
	}
	var name, given string
	if s.db.QueryRowContext(ctx, `SELECT name, given FROM readtome_voices WHERE name = ? AND deleted = 0`, voice).Scan(&name, &given) != nil {
		return nil
	}
	model := s.chatterboxModel(ctx)
	if model == "" {
		return nil // not answering: speaking says so
	}
	for _, g := range strings.Split(given, ",") {
		if g == model {
			return nil
		}
	}
	data, err := os.ReadFile(s.cbVoiceFile(name))
	if err != nil {
		return fmt.Errorf("the recording of the voice %s is missing; copy it again", name)
	}
	if err := uploadVoice(ctx, base, name, data); err != nil {
		return fmt.Errorf("Chatterbox couldn't be given the voice %s: %w", name, err)
	}
	if given != "" {
		given += ","
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE readtome_voices SET given = ? WHERE name = ?`, given+model, name)
	return nil
}

// uploadVoice sends a recording to Chatterbox's voice library (its POST
// /voices), in place of one it has by that name.
func uploadVoice(ctx context.Context, base, name string, wav []byte) error {
	send := func() (int, string, error) {
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		_ = mw.WriteField("voice_name", name)
		_ = mw.WriteField("language", "en")
		part, _ := mw.CreateFormFile("voice_file", name+".wav")
		_, _ = part.Write(wav)
		_ = mw.Close()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/voices", &body)
		if err != nil {
			return 0, "", err
		}
		req.Header.Set("Content-Type", mw.FormDataContentType())
		resp, err := rtvHTTP.Do(req)
		if err != nil {
			return 0, "", err
		}
		defer resp.Body.Close()
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return resp.StatusCode, strings.TrimSpace(string(msg)), nil
	}
	code, msg, err := send()
	if err != nil {
		return err
	}
	if code == http.StatusConflict { // an older recording by that name: it goes
		removeVoice(ctx, base, name)
		if code, msg, err = send(); err != nil {
			return err
		}
	}
	if code != http.StatusOK && code != http.StatusCreated {
		return fmt.Errorf("answered %d: %s", code, msg)
	}
	return nil
}

// removeVoice deletes a voice from Chatterbox's library, if it is there.
func removeVoice(ctx context.Context, base, name string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, base+"/voices/"+url.PathEscape(name), nil)
	if err != nil {
		return
	}
	if resp, err := rtvHTTP.Do(req); err == nil {
		resp.Body.Close()
	}
}

// cleanRecording makes an uploaded recording into what Chatterbox reads best
// from: mono, 24 kHz, the silence at the start cut, even loudness, the first
// 20 seconds. It says how long it is.
func cleanRecording(ctx context.Context, in, out string) (float64, error) {
	err := ffmpeg(ctx, "-i", in, "-vn", "-af", "silenceremove=start_periods=1:start_threshold=-50dB,loudnorm=I=-20:TP=-2",
		"-t", fmt.Sprint(cbVoiceKeep), "-ac", "1", "-ar", "24000", "-c:a", "pcm_s16le", out)
	if err != nil {
		return 0, errors.New("That file couldn't be read as a recording. Try a WAV, MP3, M4A or FLAC.")
	}
	d, err := audioDuration(ctx, out)
	if err != nil {
		return 0, err
	}
	return d.Seconds(), nil
}

// ---- routes ----

// GET /api/home/readtome/chatterbox/voices: the copied voices.
func (s *Server) handleReadToMeCopiedVoices(w http.ResponseWriter, r *http.Request) {
	vs := s.copiedVoices(r.Context(), false)
	for i := range vs {
		vs[i].CanChange = isAdminRequest(r) || (vs[i].userID != 0 && vs[i].userID == requestUserID(r))
	}
	writeJSON(w, http.StatusOK, map[string]any{"voices": vs})
}

// POST /api/home/readtome/chatterbox/voices: a multipart form with the
// recording in "file", the voice's "name", and "rights" ("yes": the person may
// use this voice).
func (s *Server) handleCopyReadToMeVoice(w http.ResponseWriter, r *http.Request) {
	allowLongUpload(w)
	r.Body = http.MaxBytesReader(w, r.Body, cbVoiceMax+(1<<20))
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "That recording is too large (25 MB at most; 10 to 20 seconds is enough).")
			return
		}
		writeError(w, http.StatusBadRequest, "Choose a recording to upload.")
		return
	}
	defer r.MultipartForm.RemoveAll()
	name := strings.Join(strings.Fields(r.FormValue("name")), " ")
	if !cbVoiceName.MatchString(name) || cbTakenNames[strings.ToLower(name)] {
		writeError(w, http.StatusBadRequest, "A voice needs a name of up to 40 letters, numbers, spaces, dots, dashes or underscores (and not default).")
		return
	}
	if r.FormValue("rights") != "yes" {
		writeError(w, http.StatusBadRequest, "Only copy a voice you have the right to use, such as your own, and tick the box that says so.")
		return
	}
	if s.isCopiedVoice(r.Context(), name) {
		writeError(w, http.StatusConflict, "There is a voice by that name already. Delete it first, or pick another name.")
		return
	}
	fh := r.MultipartForm.File["file"]
	if len(fh) == 0 {
		writeError(w, http.StatusBadRequest, "Choose a recording to upload.")
		return
	}
	src, err := fh[0].Open()
	if err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that file. Try choosing it again.")
		return
	}
	defer src.Close()
	if err := os.MkdirAll(s.cbVoiceDir(), 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	tmp, err := os.CreateTemp(s.cbVoiceDir(), "upload-*")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer os.Remove(tmp.Name())
	_, err = io.Copy(tmp, src)
	tmp.Close()
	if err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that file. Try choosing it again.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	clean := tmp.Name() + ".wav"
	defer os.Remove(clean)
	secs, err := cleanRecording(ctx, tmp.Name(), clean)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if secs < cbVoiceMinSecs {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("That recording has only %.0f seconds of sound. Chatterbox needs at least 5, best 10 to 20, of one person speaking clearly.", secs))
		return
	}
	if err := os.Rename(clean, s.cbVoiceFile(name)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_, err = s.db.ExecContext(r.Context(), `INSERT INTO readtome_voices (name, seconds, given, deleted, user_id, created_ms) VALUES (?, ?, '', 0, ?, ?)
		ON CONFLICT(name) DO UPDATE SET name = excluded.name, seconds = excluded.seconds, given = '', deleted = 0, user_id = excluded.user_id, created_ms = excluded.created_ms`,
		name, secs, requestUserID(r), time.Now().UnixMilli())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.forgetVoiceSamples(name)
	forgetVoices()
	writeJSON(w, http.StatusOK, map[string]any{"name": name, "seconds": secs})
}

// DELETE /api/home/readtome/chatterbox/voices/{name}: deletes a copied voice
// (for an administrator or the person who copied it).
func (s *Server) handleDeleteReadToMeVoice(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var userID int64
	if s.db.QueryRowContext(r.Context(), `SELECT name, user_id FROM readtome_voices WHERE name = ? AND deleted = 0`, name).Scan(&name, &userID) != nil {
		writeError(w, http.StatusNotFound, "That voice isn't there.")
		return
	}
	if !isAdminRequest(r) && (userID == 0 || userID != requestUserID(r)) {
		writeError(w, http.StatusForbidden, "Only an administrator or the person who copied it can delete this voice.")
		return
	}
	if _, err := s.db.ExecContext(r.Context(), `UPDATE readtome_voices SET deleted = 1, given = '' WHERE name = ?`, name); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = os.Remove(s.cbVoiceFile(name))
	if base := s.engineAddress(engineChatterbox); base != "" && s.chatterboxModel(r.Context()) != "" {
		removeVoice(r.Context(), base, name)
	}
	s.forgetVoiceSamples(name)
	forgetVoices()
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/home/readtome/chatterbox/voices/{name}/recording: the recording as kept.
func (s *Server) handleReadToMeVoiceRecording(w http.ResponseWriter, r *http.Request) {
	var name string
	if s.db.QueryRowContext(r.Context(), `SELECT name FROM readtome_voices WHERE name = ? AND deleted = 0`, r.PathValue("name")).Scan(&name) != nil {
		writeError(w, http.StatusNotFound, "That voice isn't there.")
		return
	}
	data, err := os.ReadFile(s.cbVoiceFile(name))
	if err != nil {
		writeError(w, http.StatusNotFound, "The recording is missing.")
		return
	}
	w.Header().Set("Content-Type", "audio/wav")
	w.Header().Set("Cache-Control", "private, no-cache")
	_, _ = w.Write(data)
}

// forgetVoiceSamples deletes the samples made with a voice (it has a new recording, or is gone).
func (s *Server) forgetVoiceSamples(name string) {
	safe := rtvUnsafeName.ReplaceAllString(name, "_")
	files, _ := filepath.Glob(s.readToMeCache("samples", "chatterbox-*-"+safe+".mp3"))
	for _, f := range files {
		_ = os.Remove(f)
	}
}
