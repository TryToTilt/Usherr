package api

// Usherr: the pre-show in a browser plays what that browser can. Clips and
// trailers are files as they are (a 4K VP9 trailer, a Dolby Vision clip in
// MKV with Atmos), which a phone's Chrome often can't decode. Before the
// pre-show starts, the page says what it plays (the same check as the film
// player) and gets, for each clip and trailer, the address to play: the file
// itself when it plays as it is; a downloaded trailer's 1080p H.264 copy; or a
// copy converted here with ffmpeg as it plays, at 1080p at most (clips are
// short). Each also gets a converted address to fall back on, so a clip that
// won't play is converted rather than skipped.

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// preshowFileURL is a clip's or trailer's own address in a pre-show plan.
var preshowFileURL = regexp.MustCompile(`^/api/home/preshow/(clips|trailers)/(\d+)/file$`)

// trailerCopyName is the 1080p H.264 copy kept beside a trailer above 1080p
// or not in H.264, for screens and browsers that can't play the trailer.
func trailerCopyName(file string) string {
	return strings.TrimSuffix(file, ".mp4") + ".1080.mp4"
}

// preshowSource is the file behind a clip's or trailer's address, and the
// trailer's copy ("" when there's none).
func (s *Server) preshowSource(ctx context.Context, kind, id string) (file, copyFile string, ok bool) {
	switch kind {
	case "clips":
		c, found := s.preshowClipByID(ctx, id)
		if !found {
			return "", "", false
		}
		p, found := s.preshowClipFile(c)
		return p, "", found
	case "trailers":
		t, found := s.trailerByID(ctx, id)
		if !found || t.State != "ready" || t.File == "" {
			return "", "", false
		}
		dir := s.trailersDir()
		file = filepath.Join(dir, filepath.Base(t.File))
		if c := filepath.Join(dir, filepath.Base(trailerCopyName(t.File))); fileExists(c) {
			copyFile = c
		}
		return file, copyFile, true
	}
	return "", "", false
}

// preshowFit is where a browser plays one clip or trailer: url first, and
// fallback (converted here) when url won't play.
type preshowFit struct {
	URL      string `json:"url"`
	Fallback string `json:"fallback,omitempty"`
	Why      string `json:"why,omitempty"` // for the log: why it isn't the file as it is
}

// fitPreshow picks a clip's or trailer's address for this browser. small is
// a screen under 4K (a phone, most laptops): a 4K picture is played from the
// 1080p copy, or converted to 1080p, since a phone often can't decode it.
func fitPreshow(base string, fc fileCodecs, file string, copyFC *fileCodecs, copyFile string, c browserCaps, small bool) preshowFit {
	big := small && fc.Height > 1080
	convert := func() string {
		cv, ca := "0", "0"
		if picturePlays(fc, c) && !big {
			cv = "1"
		}
		if soundPlays(fc.Audio, c) {
			ca = "1"
		}
		return base + "/convert?cv=" + cv + "&ca=" + ca
	}
	switch {
	case browserSafe(fc, file, c) && !big:
		return preshowFit{URL: base + "/file", Fallback: base + "/convert?cv=0&ca=0"}
	case copyFC != nil && browserSafe(*copyFC, copyFile, c):
		return preshowFit{URL: base + "/file?copy=1", Fallback: base + "/convert?cv=0&ca=0", Why: "the 1080p copy"}
	}
	return preshowFit{URL: convert(), Why: fmt.Sprintf("converted (%s %dp, sound %s)", fc.Video, fc.Height, fc.Audio)}
}

// POST /api/home/preshow/fit {"caps": {...}, "small": bool, "urls": [...]}:
// where this browser plays each clip and trailer of a plan.
func (s *Server) handlePreshowFit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Caps  browserCaps `json:"caps"`
		Small bool        `json:"small"`
		URLs  []string    `json:"urls"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	if len(req.URLs) > 60 {
		req.URLs = req.URLs[:60]
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	out := map[string]preshowFit{}
	for _, u := range req.URLs {
		m := preshowFileURL.FindStringSubmatch(u)
		if m == nil || out[u].URL != "" {
			continue
		}
		file, copyFile, ok := s.preshowSource(ctx, m[1], m[2])
		if !ok {
			continue
		}
		base := strings.TrimSuffix(u, "/file")
		fc, err := probeCodecs(ctx, file)
		if err != nil {
			out[u] = preshowFit{URL: u, Fallback: base + "/convert?cv=0&ca=0"}
			continue
		}
		var copyFC *fileCodecs
		if copyFile != "" {
			if cf, err := probeCodecs(ctx, copyFile); err == nil {
				copyFC = &cf
			}
		}
		f := fitPreshow(base, fc, file, copyFC, copyFile, req.Caps, req.Small)
		if f.Why != "" {
			slog.Info("preshow: for this browser", "file", filepath.Base(file), "plays", f.Why)
		}
		out[u] = f
	}
	writeJSON(w, http.StatusOK, map[string]any{"fits": out})
}

// preshowConvertArgs converts a clip or trailer for a browser as it plays: a
// picture the browser shows is copied (cv), else made H.264 at 1080p at most
// (a pre-show clip is short, and a phone needs no more); a sound it plays is
// copied (ca), else made AAC.
func preshowConvertArgs(file string, fc fileCodecs, copyVideo, copyAudio bool) []string {
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-i", file, "-map", "0:v:0"}
	if copyVideo {
		args = append(args, "-c:v", "copy")
		if fc.Video == "hevc" {
			args = append(args, "-tag:v", "hvc1")
		}
	} else {
		args = append(args, "-vf", "scale=-2:'min(1080,ih)',format=yuv420p", "-c:v", "libx264", "-preset", "veryfast", "-crf", "21")
	}
	args = append(args, "-map", "0:a:0?")
	if copyAudio {
		args = append(args, "-c:a", "copy")
	} else {
		args = append(args, aacArgs(fc.Channels)...)
	}
	return append(args, "-movflags", "frag_keyframe+empty_moov+default_base_moof", "-f", "mp4", "pipe:1")
}

// GET /api/home/preshow/{clips|trailers}/{id}/convert?cv=&ca=: a clip or
// trailer converted here for the browser, as it plays.
func (s *Server) handlePreshowConvert(w http.ResponseWriter, r *http.Request) {
	kind := "clips"
	if strings.HasPrefix(r.URL.Path, "/api/home/preshow/trailers/") {
		kind = "trailers"
	}
	file, _, ok := s.preshowSource(r.Context(), kind, r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	fc, err := probeCodecs(r.Context(), file)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "This clip couldn't be read.")
		return
	}
	q := r.URL.Query()
	cmd := exec.CommandContext(r.Context(), "ffmpeg", preshowConvertArgs(file, fc, q.Get("cv") == "1", q.Get("ca") == "1")...)
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Cache-Control", "no-store")
	cmd.Stdout = flushWriter{w: w}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil && r.Context().Err() == nil {
		slog.Info("preshow: converted copy", "file", filepath.Base(file), "err", err, "ffmpeg", firstLine(stderr.String()))
	}
}

// trailerFetchCopy downloads a trailer's 1080p H.264 copy (YouTube has H.264
// up to 1080p), for browsers and screens that can't play the trailer itself.
// A failure is only logged: the trailer still plays, converted when needed.
func (s *Server) trailerFetchCopy(ctx context.Context, t preshowTrailer, key string) int64 {
	dir := s.trailersDir()
	final := filepath.Join(dir, filepath.Base(trailerCopyName(t.File)))
	tmp := filepath.Join(dir, ".dl-copy-"+strings.TrimSuffix(filepath.Base(t.File), ".mp4"))
	run, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(run, "yt-dlp", "--no-warnings", "--no-playlist", "--no-progress", "--no-mtime",
		"-f", "bv*[height<=1080][vcodec^=avc1]+ba[acodec^=mp4a]/b[height<=1080][vcodec^=avc1]",
		"--merge-output-format", "mp4", "-o", tmp+".%(ext)s", "https://www.youtube.com/watch?v="+key).CombinedOutput()
	if err == nil {
		err = os.Rename(tmp+".mp4", final)
	}
	if err != nil {
		matches, _ := filepath.Glob(tmp + ".*")
		for _, m := range matches {
			_ = os.Remove(m)
		}
		slog.Info("preshow: trailer's 1080p copy not fetched", "title", t.Title, "err", lastLines(string(out), 1))
		return 0
	}
	st, err := os.Stat(final)
	if err != nil {
		return 0
	}
	return st.Size()
}

// trailerNeedsCopy says whether a trailer file is one some browsers can't
// play: above 1080p, or not H.264.
func trailerNeedsCopy(ctx context.Context, file string) bool {
	fc, err := probeCodecs(ctx, file)
	return err == nil && (fc.Height > 1080 || fc.Video != "h264")
}

// trailersCopiesPerRun: older trailers are given their copy a few at a time.
const trailersCopiesPerRun = 2

// trailerBackfillCopies gives ready trailers fetched before copies existed
// their 1080p copy, a few each run.
func (s *Server) trailerBackfillCopies(ctx context.Context) {
	ready, err := s.preshowTrailers(ctx, `WHERE state = 'ready' AND file != '' AND youtube != '' ORDER BY release_date`)
	if err != nil {
		return
	}
	done := 0
	for _, t := range ready {
		if done >= trailersCopiesPerRun || ctx.Err() != nil {
			return
		}
		dir := s.trailersDir()
		if fileExists(filepath.Join(dir, filepath.Base(trailerCopyName(t.File)))) || !trailerNeedsCopy(ctx, filepath.Join(dir, filepath.Base(t.File))) {
			continue
		}
		if n := s.trailerFetchCopy(ctx, t, t.YouTube); n > 0 {
			_, _ = s.db.ExecContext(ctx, `UPDATE preshow_trailers SET size = size + ? WHERE id = ?`, n, t.ID)
		}
		done++
	}
}

// preshowCopyPath is a trailer copy's path, for the trailer file handler.
func (s *Server) preshowCopyPath(t preshowTrailer) string {
	return filepath.Join(s.trailersDir(), filepath.Base(trailerCopyName(t.File)))
}
