package musictidy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// The ffmpeg and ffprobe programs; tests may point them elsewhere.
var (
	FFmpeg  = "ffmpeg"
	FFprobe = "ffprobe"
)

// toolTimeout bounds one ffprobe, fingerprint or rewrite of a file.
const toolTimeout = 2 * time.Minute

// Available reports whether ffmpeg and ffprobe can be run.
func Available() bool {
	_, e1 := exec.LookPath(FFmpeg)
	_, e2 := exec.LookPath(FFprobe)
	return e1 == nil && e2 == nil
}

// Probe is what ffprobe says about a file.
type Probe struct {
	DurationSec  float64
	Codec        string // the first audio stream's codec
	AudioStreams int
	Pictures     int               // embedded cover pictures
	Tags         map[string]string // the file's tags with lowercased keys (container and audio stream merged)
}

type probeJSON struct {
	Streams []struct {
		CodecType   string            `json:"codec_type"`
		CodecName   string            `json:"codec_name"`
		Disposition map[string]int    `json:"disposition"`
		Tags        map[string]string `json:"tags"`
	} `json:"streams"`
	Format struct {
		Duration string            `json:"duration"`
		Tags     map[string]string `json:"tags"`
	} `json:"format"`
}

// ProbeFile runs ffprobe on a file.
func ProbeFile(ctx context.Context, path string) (Probe, error) {
	ctx, cancel := context.WithTimeout(ctx, toolTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, FFprobe, "-v", "error", "-show_entries",
		"stream=codec_type,codec_name:stream_disposition=attached_pic:stream_tags:format=duration:format_tags", "-of", "json", "--", path)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return Probe{}, fmt.Errorf("ffprobe %s: %v: %s", path, err, firstLine(stderr.String()))
	}
	return parseProbe(out.Bytes())
}

func parseProbe(data []byte) (Probe, error) {
	var j probeJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return Probe{}, fmt.Errorf("ffprobe: %w", err)
	}
	p := Probe{Tags: map[string]string{}}
	p.DurationSec, _ = strconv.ParseFloat(j.Format.Duration, 64)
	for k, v := range j.Format.Tags {
		p.Tags[strings.ToLower(k)] = v
	}
	for _, s := range j.Streams {
		switch {
		case s.CodecType == "audio":
			if p.AudioStreams == 0 {
				p.Codec = s.CodecName
				for k, v := range s.Tags { // Ogg keeps its tags on the stream
					if _, ok := p.Tags[strings.ToLower(k)]; !ok {
						p.Tags[strings.ToLower(k)] = v
					}
				}
			}
			p.AudioStreams++
		case s.CodecType == "video" && s.Disposition["attached_pic"] == 1:
			p.Pictures++
		}
	}
	if p.AudioStreams == 0 {
		return p, errors.New("no audio in the file")
	}
	return p, nil
}

// Fingerprint computes a file's Chromaprint fingerprint over its first two
// minutes, in the compressed form AcoustID takes, with ffmpeg's chromaprint
// output.
func Fingerprint(ctx context.Context, path string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, toolTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, FFmpeg, "-hide_banner", "-loglevel", "error", "-i", path,
		"-t", "120", "-ac", "2", "-f", "chromaprint", "-fp_format", "base64", "-")
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("fingerprint %s: %v: %s", path, err, firstLine(stderr.String()))
	}
	fp := strings.TrimSpace(out.String())
	if fp == "" {
		return "", fmt.Errorf("fingerprint %s: ffmpeg gave none", path)
	}
	return fp, nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
