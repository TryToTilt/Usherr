package api

// Usherr: how Live TV channels make their picture, in place of ErsatzTV.
//
//   - The video encoder: Automatic (the first that works of NVIDIA NVENC,
//     Intel Quick Sync, VAAPI and the CPU), or one chosen by hand. Each is
//     tried once with a one-second test picture; the answers are kept until
//     the settings are saved again or "Test again" is pressed.
//   - The watermark (off by default): the channel's logo, one uploaded
//     picture, or the channel's number or name, in a corner of the picture.
//     A channel can be left without it. With no watermark the tuner keeps
//     copying the picture as it is; with one it draws it and encodes.
//
// Settings are one JSON value (usherr.tv_video). The per-channel switch is
// in the channel's rules (noWatermark).

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/tv"
)

const keyTVVideo = "usherr.tv_video"

// Encoder kinds.
const (
	encAuto  = "auto"
	encCPU   = "cpu"
	encNVENC = "nvenc"
	encQSV   = "qsv"
	encVAAPI = "vaapi"
)

// tvEncoderOrder is the order Automatic tries them in.
var tvEncoderOrder = []string{encNVENC, encQSV, encVAAPI, encCPU}

type tvWatermark struct {
	On       bool   `json:"on"`
	Show     string `json:"show"`     // logo (the channel's), image (the uploaded one), number, name
	Position string `json:"position"` // top-left, top-right, bottom-left, bottom-right
	Size     int    `json:"size"`     // % of the picture's width, 5-25
	Opacity  int    `json:"opacity"`  // %, 10-100
	Margin   int    `json:"margin"`   // % of the picture's width, 0-10
}

type tvVideoConfig struct {
	Encoder   string      `json:"encoder"`
	Watermark tvWatermark `json:"watermark"`
}

func clampPct(v, lo, hi, def int) int {
	if v == 0 {
		return def
	}
	return max(lo, min(hi, v))
}

// normalized fills in defaults and keeps everything in range.
func (c tvVideoConfig) normalized() tvVideoConfig {
	switch c.Encoder {
	case encCPU, encNVENC, encQSV, encVAAPI:
	default:
		c.Encoder = encAuto
	}
	w := &c.Watermark
	switch w.Show {
	case "image", "number", "name":
	default:
		w.Show = "logo"
	}
	switch w.Position {
	case "top-left", "top-right", "bottom-left":
	default:
		w.Position = "bottom-right"
	}
	w.Size = clampPct(w.Size, 5, 25, 10)
	w.Opacity = clampPct(w.Opacity, 10, 100, 60)
	if w.Margin < 0 {
		w.Margin = 0
	}
	w.Margin = min(w.Margin, 10)
	return c
}

func defaultTVVideo() tvVideoConfig {
	return tvVideoConfig{Encoder: encAuto, Watermark: tvWatermark{Margin: 3}}.normalized()
}

func (s *Server) tvVideo() tvVideoConfig {
	c := defaultTVVideo()
	if raw, err := s.Settings.Get(keyTVVideo); err == nil && strings.TrimSpace(raw) != "" {
		_ = json.Unmarshal([]byte(raw), &c)
	}
	return c.normalized()
}

// ---- encoders ----

type tvEncoder struct {
	hwInit []string // before the inputs
	tail   string   // the last filters before the encoder
	codec  []string
}

// tvKeyframes: a key frame every two seconds, so players can start soon.
var tvKeyframes = []string{"-force_key_frames", "expr:gte(t,n_forced*2)"}

// vaapiDevice is the first render device, /dev/dri/renderD128 when none is seen.
func vaapiDevice() string {
	if m, _ := filepath.Glob("/dev/dri/renderD*"); len(m) > 0 {
		return m[0]
	}
	return "/dev/dri/renderD128"
}

func tvEncoderFor(kind string) tvEncoder {
	switch kind {
	case encNVENC:
		return tvEncoder{tail: "format=yuv420p", codec: append([]string{"-c:v", "h264_nvenc", "-preset", "p4", "-tune", "ll", "-rc", "vbr", "-cq", "23", "-b:v", "0"}, tvKeyframes...)}
	case encQSV:
		return tvEncoder{tail: "format=nv12", codec: append([]string{"-c:v", "h264_qsv", "-preset", "veryfast", "-global_quality", "23"}, tvKeyframes...)}
	case encVAAPI:
		return tvEncoder{hwInit: []string{"-vaapi_device", vaapiDevice()}, tail: "format=nv12,hwupload", codec: append([]string{"-c:v", "h264_vaapi", "-qp", "23"}, tvKeyframes...)}
	}
	return tvEncoder{tail: "format=yuv420p", codec: append([]string{"-c:v", "libx264", "-preset", "veryfast", "-crf", "21"}, tvKeyframes...)}
}

// tvEncoderTestArgs encodes one second of a test picture with an encoder.
func tvEncoderTestArgs(kind string) []string {
	e := tvEncoderFor(kind)
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin"}
	args = append(args, e.hwInit...)
	args = append(args, "-f", "lavfi", "-i", "testsrc2=size=1280x720:rate=30", "-t", "1", "-vf", e.tail)
	args = append(args, e.codec...)
	return append(args, "-f", "null", "-")
}

type tvEncoderResult struct {
	Kind    string `json:"kind"`
	Works   bool   `json:"works"`
	Message string `json:"message"`
}

var (
	tvEncMu      sync.Mutex
	tvEncResults []tvEncoderResult // nil until tested
	tvEncWarned  sync.Map          // kind -> true, once it failed while playing

	// tvRunEncoderTest runs ffmpeg for a test (replaced in tests).
	tvRunEncoderTest = func(ctx context.Context, args []string) error {
		ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "ffmpeg", args...)
		var stderr strings.Builder
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				return errors.New("no answer within 20 seconds")
			}
			if msg := strings.TrimSpace(firstLine(strings.TrimSpace(stderr.String()))); msg != "" {
				return errors.New(msg)
			}
			return err
		}
		return nil
	}
)

// testTVEncoders tries every encoder and keeps the answers.
func testTVEncoders(ctx context.Context) []tvEncoderResult {
	tvEncMu.Lock()
	defer tvEncMu.Unlock()
	return testTVEncodersLocked(ctx)
}

func testTVEncodersLocked(ctx context.Context) []tvEncoderResult {
	out := make([]tvEncoderResult, 0, len(tvEncoderOrder))
	for _, k := range tvEncoderOrder {
		r := tvEncoderResult{Kind: k, Works: true, Message: "works"}
		if err := tvRunEncoderTest(ctx, tvEncoderTestArgs(k)); err != nil {
			r.Works, r.Message = false, err.Error()
		}
		out = append(out, r)
	}
	tvEncResults = out
	return out
}

// tvEncoderResults are the kept answers, testing first when there are none.
func tvEncoderResults(ctx context.Context) []tvEncoderResult {
	tvEncMu.Lock()
	defer tvEncMu.Unlock()
	if tvEncResults != nil {
		return tvEncResults
	}
	return testTVEncodersLocked(ctx)
}

// pickTVEncoder is the encoder to use: the chosen one, or for Automatic the
// first that worked (the CPU when none did).
func pickTVEncoder(choice string, results []tvEncoderResult) string {
	if choice != encAuto && choice != "" {
		return choice
	}
	for _, k := range tvEncoderOrder {
		for _, r := range results {
			if r.Kind == k && r.Works {
				return k
			}
		}
	}
	return encCPU
}

// tvVideoEncoder is the encoder channels use now.
func (s *Server) tvVideoEncoder(ctx context.Context) string {
	c := s.tvVideo()
	if c.Encoder != encAuto {
		return c.Encoder
	}
	return pickTVEncoder(encAuto, tvEncoderResults(context.WithoutCancel(ctx)))
}

// tvEncoderFailed logs, once per encoder, that it failed while playing.
func tvEncoderFailed(kind string, err error) {
	if _, warned := tvEncWarned.LoadOrStore(kind, true); !warned {
		slog.Warn("tv: video encoder failed, using the CPU (libx264) for this program", "encoder", kind, "err", err)
	}
}

// ---- the watermark ----

// tvMark is a watermark ready to draw on one program.
type tvMark struct {
	image    string // a picture to lay over, or
	textFile string // a file with the text to draw
	font     string
	width    int // the program's picture width
	w        tvWatermark
}

// tvMarkGraph is the filter graph that draws the watermark on input 0's
// picture (the picture as input 1), then tail, as [v].
func tvMarkGraph(m tvMark, tail string) string {
	width := m.width
	if width <= 0 {
		width = 1920
	}
	margin := width * m.w.Margin / 100
	left := strings.HasSuffix(m.w.Position, "left")
	top := strings.HasPrefix(m.w.Position, "top")
	alpha := strconv.FormatFloat(float64(m.w.Opacity)/100, 'f', 2, 64)
	end := ""
	if tail != "" {
		end = "," + tail
	}
	if m.image != "" {
		x, y := fmt.Sprintf("main_w-overlay_w-%d", margin), fmt.Sprintf("main_h-overlay_h-%d", margin)
		if left {
			x = strconv.Itoa(margin)
		}
		if top {
			y = strconv.Itoa(margin)
		}
		lw := max(16, width*m.w.Size/100)
		lw -= lw % 2
		return fmt.Sprintf("[1:v]scale=%d:-2,format=rgba,colorchannelmixer=aa=%s[wm];[0:v:0][wm]overlay=x=%s:y=%s:format=auto%s[v]", lw, alpha, x, y, end)
	}
	x, y := fmt.Sprintf("w-text_w-%d", margin), fmt.Sprintf("h-text_h-%d", margin)
	if left {
		x = strconv.Itoa(margin)
	}
	if top {
		y = strconv.Itoa(margin)
	}
	size := max(12, width*m.w.Size/400)
	esc := func(p string) string { return strings.ReplaceAll(p, ":", `\:`) }
	return fmt.Sprintf("[0:v:0]drawtext=fontfile=%s:textfile=%s:fontsize=%d:fontcolor=white@%s:shadowcolor=black@%s:shadowx=2:shadowy=2:x=%s:y=%s%s[v]",
		esc(m.font), esc(m.textFile), size, alpha, alpha, x, y, end)
}

var tvWidths sync.Map // file -> picture width

// tvVideoWidth is a file's picture width (1920 when it can't be read).
func tvVideoWidth(ctx context.Context, file string) int {
	if v, ok := tvWidths.Load(file); ok {
		return v.(int)
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=width", "-of", "csv=p=0", file).Output()
	w, perr := strconv.Atoi(strings.TrimSpace(firstLine(strings.TrimSpace(string(out)))))
	if err != nil || perr != nil || w <= 0 {
		return 1920
	}
	tvWidths.Store(file, w)
	return w
}

var tvWatermarkTypes = map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp"}

const tvWatermarkMax = 2 << 20

// tvWatermarkImage is the uploaded watermark picture, if there is one.
func (s *Server) tvWatermarkImage() (string, bool) {
	for _, ext := range []string{".png", ".jpg", ".webp"} {
		p := filepath.Join(s.cfg.ConfigDir, "tv-watermark"+ext)
		if fileExists(p) {
			return p, true
		}
	}
	return "", false
}

var tvLogoFetch = &http.Client{Timeout: 20 * time.Second}

// tvChannelLogoFile is a channel's logo as a file ffmpeg reads: the uploaded
// one, or a linked one fetched and kept for a day.
func (s *Server) tvChannelLogoFile(ctx context.Context, ch tv.Channel) (string, bool) {
	if ch.LogoURL == tvLogoUploaded {
		return s.tvLogoFile(ch.ID)
	}
	if !strings.HasPrefix(ch.LogoURL, "http://") && !strings.HasPrefix(ch.LogoURL, "https://") {
		return "", false
	}
	sum := sha1.Sum([]byte(ch.LogoURL))
	dir := filepath.Join(s.cfg.ConfigDir, "tv-watermark-cache")
	p := filepath.Join(dir, hex.EncodeToString(sum[:])+".img")
	if fi, err := os.Stat(p); err == nil && time.Since(fi.ModTime()) < 24*time.Hour {
		return p, true
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ch.LogoURL, nil)
	if err != nil {
		return "", false
	}
	resp, err := tvLogoFetch.Do(req)
	if err != nil {
		if fileExists(p) { // an older copy is better than none
			return p, true
		}
		return "", false
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, tvWatermarkMax+1))
	if err != nil || resp.StatusCode != http.StatusOK || len(data) > tvWatermarkMax {
		return "", false
	}
	if _, ok := tvLogoTypes[http.DetectContentType(data)]; !ok {
		return "", false
	}
	if os.MkdirAll(dir, 0o755) != nil || os.WriteFile(p, data, 0o644) != nil {
		return "", false
	}
	return p, true
}

// tvMarkWith makes a watermark for a channel from settings w. It is nil when
// there is nothing to draw (no logo, no font). done removes its text file.
func (s *Server) tvMarkWith(ctx context.Context, w tvWatermark, ch tv.Channel, width int) (*tvMark, func()) {
	m := &tvMark{width: width, w: w}
	switch w.Show {
	case "logo":
		p, ok := s.tvChannelLogoFile(ctx, ch)
		if !ok {
			return nil, func() {}
		}
		m.image = p
	case "image":
		p, ok := s.tvWatermarkImage()
		if !ok {
			return nil, func() {}
		}
		m.image = p
	default:
		m.font = tvFont(true)
		if m.font == "" {
			return nil, func() {}
		}
		text := strconv.Itoa(ch.Number)
		if w.Show == "name" {
			text = ch.Name
		}
		f, err := os.CreateTemp("", "tvmark*.txt")
		if err != nil {
			return nil, func() {}
		}
		_, _ = f.WriteString(text)
		f.Close()
		m.textFile = f.Name()
		return m, func() { _ = os.Remove(f.Name()) }
	}
	return m, func() {}
}

// tvMarkFor is the watermark for a program of a channel, nil when it's off.
func (s *Server) tvMarkFor(ctx context.Context, ch tv.Channel, file string) (*tvMark, func()) {
	c := s.tvVideo()
	if !c.Watermark.On || ch.Rules.NoWatermark || ch.Kind == "live" {
		return nil, func() {}
	}
	return s.tvMarkWith(ctx, c.Watermark, ch, tvVideoWidth(ctx, file))
}

// ---- settings ----

func (s *Server) registerTVVideoRoutes(admin routeGroup) {
	admin.HandleFunc("GET /api/tv/video", s.handleTVVideoGet)
	admin.HandleFunc("PUT /api/tv/video", s.handleTVVideoPut)
	admin.HandleFunc("POST /api/tv/video/test", s.handleTVVideoTest)
	admin.HandleFunc("POST /api/tv/video/image", s.handleTVWatermarkUpload)
	admin.HandleFunc("DELETE /api/tv/video/image", s.handleTVWatermarkDelete)
	admin.HandleFunc("POST /api/tv/video/preview", s.handleTVWatermarkPreview)
	admin.HandleFunc("PUT /api/tv/channels/{id}/watermark", s.handleTVChannelWatermark)
}

type tvVideoChannel struct {
	ID        int64  `json:"id"`
	Number    int    `json:"number"`
	Name      string `json:"name"`
	Logo      bool   `json:"logo"`
	Live      bool   `json:"live"`
	Watermark bool   `json:"watermark"`
}

func (s *Server) tvVideoPayload(results []tvEncoderResult) map[string]any {
	c := s.tvVideo()
	chans, _ := s.tvRepo().Channels()
	list := make([]tvVideoChannel, 0, len(chans))
	for _, ch := range chans {
		list = append(list, tvVideoChannel{ID: ch.ID, Number: ch.Number, Name: ch.Name, Logo: ch.LogoURL != "", Live: ch.Kind == "live", Watermark: !ch.Rules.NoWatermark})
	}
	_, hasImage := s.tvWatermarkImage()
	return map[string]any{
		"encoder":   c.Encoder,
		"watermark": c.Watermark,
		"results":   results,
		"using":     pickTVEncoder(c.Encoder, results),
		"hasImage":  hasImage,
		"font":      tvFont(true) != "",
		"channels":  list,
	}
}

// GET /api/tv/video
func (s *Server) handleTVVideoGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.tvVideoPayload(tvEncoderResults(r.Context())))
}

// PUT /api/tv/video: save, and test the encoders again.
func (s *Server) handleTVVideoPut(w http.ResponseWriter, r *http.Request) {
	var c tvVideoConfig
	if err := decodeJSON(r, &c); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	c = c.normalized()
	raw, _ := json.Marshal(c)
	if err := s.Settings.Set(keyTVVideo, string(raw), false); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	tvEncWarned.Clear()
	writeJSON(w, http.StatusOK, s.tvVideoPayload(testTVEncoders(r.Context())))
}

// POST /api/tv/video/test
func (s *Server) handleTVVideoTest(w http.ResponseWriter, r *http.Request) {
	tvEncWarned.Clear()
	writeJSON(w, http.StatusOK, s.tvVideoPayload(testTVEncoders(r.Context())))
}

// POST /api/tv/video/image (multipart "file"): the watermark picture.
func (s *Server) handleTVWatermarkUpload(w http.ResponseWriter, r *http.Request) {
	bad := "Choose a PNG, JPG or WebP picture of 2 MB or less."
	r.Body = http.MaxBytesReader(w, r.Body, tvWatermarkMax+64<<10)
	f, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, bad)
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, tvWatermarkMax+1))
	if err != nil || len(data) > tvWatermarkMax {
		writeError(w, http.StatusBadRequest, bad)
		return
	}
	ext, ok := tvWatermarkTypes[http.DetectContentType(data)]
	if !ok {
		writeError(w, http.StatusBadRequest, bad)
		return
	}
	s.removeTVWatermarkImage()
	if err := os.WriteFile(filepath.Join(s.cfg.ConfigDir, "tv-watermark"+ext), data, 0o644); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.tvVideoPayload(tvEncoderResults(r.Context())))
}

func (s *Server) removeTVWatermarkImage() {
	for {
		p, ok := s.tvWatermarkImage()
		if !ok || os.Remove(p) != nil {
			return
		}
	}
}

// DELETE /api/tv/video/image
func (s *Server) handleTVWatermarkDelete(w http.ResponseWriter, r *http.Request) {
	s.removeTVWatermarkImage()
	writeJSON(w, http.StatusOK, s.tvVideoPayload(tvEncoderResults(r.Context())))
}

// PUT /api/tv/channels/{id}/watermark {"on": bool}: a channel with or
// without the watermark. Its schedule stays as it is.
func (s *Server) handleTVChannelWatermark(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a channel.")
		return
	}
	var req struct {
		On bool `json:"on"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	repo := s.tvRepo()
	ch, err := repo.Channel(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	ch.Rules.NoWatermark = !req.On
	if _, err := repo.Save(ch); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"on": req.On})
}

// tvPreviewArgs makes one JPEG frame of src (from offset) with the mark on.
// src "" is a plain test picture.
func tvPreviewArgs(src string, offset time.Duration, m tvMark) []string {
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin"}
	if src == "" {
		args = append(args, "-f", "lavfi", "-i", "color=c=0x14213d:s=1280x720")
	} else {
		if offset > time.Second {
			args = append(args, "-ss", strconv.FormatFloat(offset.Seconds(), 'f', 3, 64))
		}
		args = append(args, "-i", src)
	}
	if m.image != "" {
		args = append(args, "-i", m.image)
	}
	return append(args, "-filter_complex", tvMarkGraph(m, ""), "-map", "[v]", "-frames:v", "1", "-c:v", "mjpeg", "-q:v", "3", "-f", "image2", "pipe:1")
}

// POST /api/tv/video/preview {"channelId", "watermark"}: one frame of what
// the channel shows now, with the watermark as set (saved or not).
func (s *Server) handleTVWatermarkPreview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ChannelID int64       `json:"channelId"`
		Watermark tvWatermark `json:"watermark"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	ch, err := s.tvRepo().Channel(req.ChannelID)
	if err != nil {
		writeError(w, http.StatusNotFound, "Pick a channel.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	wm := tvVideoConfig{Watermark: req.Watermark}.normalized().Watermark
	src, offset := "", time.Duration(0)
	if ch.Kind != "live" {
		now := time.Now()
		if p, ok, _ := s.tvRepo().At(ch.ID, now); ok {
			switch p.Kind {
			case "card":
			case "stream":
				if st, err := s.tvStreamSource(ctx, p.FilePath); err == nil {
					src, offset = st.url, now.Sub(p.Start)+p.Offset
				}
			default:
				if p.FilePath != "" {
					src, offset = p.FilePath, now.Sub(p.Start)+p.Offset
				}
			}
		}
	}
	width := 1280
	if src != "" {
		width = tvVideoWidth(ctx, src)
	}
	m, done := s.tvMarkWith(ctx, wm, ch, width)
	defer done()
	if m == nil {
		msg := "This channel has no logo to show."
		switch wm.Show {
		case "image":
			msg = "Upload a picture first."
		case "number", "name":
			msg = "No font for text was found in this Usherr image."
		}
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	cmd := exec.CommandContext(ctx, "ffmpeg", tvPreviewArgs(src, offset, *m)...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		writeError(w, http.StatusBadGateway, "Couldn't make a preview: "+strings.TrimSpace(firstLine(strings.TrimSpace(stderr.String()))))
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(out)
}
