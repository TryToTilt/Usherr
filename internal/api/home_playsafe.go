package api

// Usherr: what a browser can really play, read from the file itself, and a
// converted copy made here when Jellyfin can't make one. Most libraries are
// MKV with Dolby (AC3, E-AC3), TrueHD or DTS sound: browsers often say they
// can play those and then play the picture without a sound.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type fileCodecs struct {
	Format   string // ffprobe's format_name, "matroska,webm"
	Video    string // first video stream's codec
	PixFmt   string
	Transfer string // color_transfer: smpte2084 (HDR10), arib-std-b67 (HLG)
	Height   int    // the picture's height: 2160 for 4K
	Audio    string // first audio stream's codec, "" when silent
	Channels int    // that sound's channels: 6 for 5.1
}

type codecsKey struct {
	path string
	size int64
	mod  time.Time
}

var codecsCache sync.Map // codecsKey -> fileCodecs

// probeCodecs reads file's container and first video and audio codecs.
func probeCodecs(ctx context.Context, file string) (fileCodecs, error) {
	fi, err := os.Stat(file)
	if err != nil {
		return fileCodecs{}, err
	}
	key := codecsKey{file, fi.Size(), fi.ModTime()}
	if v, ok := codecsCache.Load(key); ok {
		return v.(fileCodecs), nil
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-show_entries",
		"format=format_name:stream=codec_type,codec_name,pix_fmt,color_transfer,height,channels", "-of", "json", file).Output()
	if err != nil {
		return fileCodecs{}, fmt.Errorf("ffprobe: %w", err)
	}
	var res struct {
		Format struct {
			FormatName string `json:"format_name"`
		} `json:"format"`
		Streams []struct {
			Type     string `json:"codec_type"`
			Codec    string `json:"codec_name"`
			PixFmt   string `json:"pix_fmt"`
			Transfer string `json:"color_transfer"`
			Height   int    `json:"height"`
			Channels int    `json:"channels"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return fileCodecs{}, err
	}
	fc := fileCodecs{Format: res.Format.FormatName}
	for _, st := range res.Streams {
		switch {
		case st.Type == "video" && fc.Video == "" && st.Codec != "mjpeg" && st.Codec != "png":
			fc.Video, fc.PixFmt, fc.Transfer, fc.Height = st.Codec, st.PixFmt, st.Transfer, st.Height
		case st.Type == "audio" && fc.Audio == "":
			fc.Audio, fc.Channels = st.Codec, st.Channels
		}
	}
	codecsCache.Store(key, fc)
	return fc, nil
}

// browserSafe says whether this browser plays file as it is, picture and
// sound. Dolby, TrueHD and DTS never count: browsers that claim them often
// play them silently.
func browserSafe(fc fileCodecs, file string, c browserCaps) bool {
	ext := strings.ToLower(filepath.Ext(file))
	switch {
	case strings.Contains(fc.Format, "mp4") || strings.Contains(fc.Format, "mov"):
	case ext == ".webm":
	case ext == ".mkv" && c.MKV:
	default:
		return false
	}
	return picturePlays(fc, c) && soundPlays(fc.Audio, c)
}

// picturePlays says whether this browser shows file's picture as it is.
func picturePlays(fc fileCodecs, c browserCaps) bool {
	switch fc.Video {
	case "h264":
		if fc.PixFmt != "" && fc.PixFmt != "yuv420p" && fc.PixFmt != "yuvj420p" {
			return false // 10-bit H.264 plays nowhere
		}
	case "hevc":
		if !c.HEVC {
			return false
		}
	case "av1":
		if !c.AV1 {
			return false
		}
	case "vp8", "vp9":
		if !c.VP9 {
			return false
		}
	default:
		return false
	}
	return !isHDR(fc.Transfer) || c.HDR
}

// soundPlays says whether this browser plays a sound codec. Dolby, TrueHD and
// DTS never count: browsers that claim them often play them silently.
func soundPlays(codec string, c browserCaps) bool {
	switch codec {
	case "", "aac", "mp3", "opus", "vorbis":
		return true
	case "flac":
		return c.FLAC
	}
	return false
}

func isHDR(transfer string) bool { return transfer == "smpte2084" || transfer == "arib-std-b67" }

// aacArgs converts sound to AAC, keeping its channels (5.1 stays 5.1; more
// than that becomes 5.1, the most a browser is sure to play).
func aacArgs(channels int) []string {
	switch {
	case channels > 6:
		return []string{"-c:a", "aac", "-b:a", "384k", "-ac", "6"}
	case channels > 2:
		return []string{"-c:a", "aac", "-b:a", "384k"}
	}
	return []string{"-c:a", "aac", "-b:a", "192k"}
}

// x264Args re-encodes a picture the browser can't show, at its own size: a
// 4K film stays 4K. HDR becomes SDR (8-bit H.264 is what every browser
// shows); a bigger picture takes a faster setting, to keep up as it plays.
func x264Args(fc fileCodecs) []string {
	preset := "veryfast"
	if fc.Height > 1080 {
		preset = "ultrafast"
	}
	return []string{"-c:v", "libx264", "-preset", preset, "-crf", "21", "-pix_fmt", "yuv420p"}
}

// convertArgs makes an ffmpeg command for file from at on, as fragmented MP4,
// converting only what the browser can't play: the picture is copied as it
// is (4K, HDR, HEVC) when copyVideo, and the sound when copyAudio. audio is
// the sound track's stream index (-1 the first) and channels its channels;
// burn a picture subtitle's stream index to draw into the picture (-1 none),
// which always re-encodes the picture.
func convertArgs(file string, fc fileCodecs, at float64, audio, channels, burn int, copyVideo, copyAudio bool) []string {
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin"}
	if at > 1 {
		args = append(args, "-ss", strconv.FormatFloat(at, 'f', 3, 64))
	}
	args = append(args, "-i", file)
	switch {
	case burn >= 0:
		args = append(args, "-filter_complex", fmt.Sprintf("[0:v:0][0:%d]overlay=format=auto,format=yuv420p[v]", burn), "-map", "[v]")
		args = append(args, x264Args(fc)...)
	case copyVideo:
		args = append(args, "-map", "0:v:0", "-c:v", "copy")
		if fc.Video == "hevc" {
			args = append(args, "-tag:v", "hvc1") // how browsers know HEVC in MP4
		}
	default:
		// HDR to SDR, roughly: no tone-mapping filter in every ffmpeg build.
		args = append(args, "-map", "0:v:0", "-vf", "format=yuv420p")
		args = append(args, x264Args(fc)...)
	}
	if audio >= 0 {
		args = append(args, "-map", "0:"+strconv.Itoa(audio))
	} else {
		args = append(args, "-map", "0:a:0?")
	}
	if copyAudio {
		args = append(args, "-c:a", "copy")
	} else {
		args = append(args, aacArgs(channels)...)
	}
	return append(args, "-movflags", "frag_keyframe+empty_moov+default_base_moof", "-f", "mp4", "pipe:1")
}

// GET /api/play/convert/{kind}/{id}?at=<seconds>: a title converted here for
// the browser, from at seconds in (for when Jellyfin can't convert it).
func (s *Server) handlePlayConvert(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	file, _, err := s.playFile(r, r.PathValue("kind"), id)
	if err != nil || file == "" {
		writeError(w, http.StatusNotFound, "This title has no file to play.")
		return
	}
	fc, err := probeCodecs(r.Context(), file)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "This file couldn't be read.")
		return
	}
	at, _ := strconv.ParseFloat(r.URL.Query().Get("at"), 64)
	// Only a sound track or picture subtitle that is really in the file.
	audio, burn, channels := -1, -1, fc.Channels
	if tracks, err := probeTracks(r.Context(), file); err == nil {
		a, _ := strconv.Atoi(r.URL.Query().Get("audio"))
		b, _ := strconv.Atoi(r.URL.Query().Get("burn"))
		for _, tr := range tracks {
			if tr.Index == a && tr.Kind == "audio" && r.URL.Query().Get("audio") != "" {
				audio, channels = a, tr.Channels
			}
			if tr.Index == b && tr.Kind == "image" && r.URL.Query().Get("burn") != "" {
				burn = b
			}
		}
	}
	// What the browser plays as it is is kept (prepare checked it).
	copyVideo, copyAudio := r.URL.Query().Get("cv") == "1", r.URL.Query().Get("ca") == "1"
	cmd := exec.CommandContext(r.Context(), "ffmpeg", convertArgs(file, fc, at, audio, channels, burn, copyVideo, copyAudio)...)
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Cache-Control", "no-store")
	cmd.Stdout = flushWriter{w: w}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil && r.Context().Err() == nil {
		slog.Info("play: converted copy", "file", filepath.Base(file), "err", err, "ffmpeg", firstLine(stderr.String()))
	}
}
