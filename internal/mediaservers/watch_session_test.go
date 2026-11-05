package mediaservers

import (
	"encoding/json"
	"strings"
	"testing"
)

// Usherr: Jellyfin's transcode reasons come as a list on newer servers and as
// one comma-separated text on older ones; both read as plain words.
func TestTranscodeReasons(t *testing.T) {
	for raw, want := range map[string]string{
		`["ContainerNotSupported","VideoBitrateNotSupported"]`: "Container not supported|Video bitrate not supported",
		`"AudioCodecNotSupported,SubtitleCodecNotSupported"`:   "Audio codec not supported|Subtitle codec not supported",
		`null`: "",
		`42`:   "",
	} {
		if got := strings.Join(transcodeReasons(json.RawMessage(raw)), "|"); got != want {
			t.Errorf("%s: %q, want %q", raw, got, want)
		}
	}
}

func TestLocationOf(t *testing.T) {
	for addr, want := range map[string]string{
		"192.168.1.20":   "lan",
		"10.0.0.2:51234": "lan",
		"[::1]:8096":     "lan",
		"100.64.0.1":     "remote", // Tailscale's range isn't a private network
		"203.0.113.5":    "remote",
		"2001:db8::1":    "remote",
		"":               "",
		"not an address": "",
	} {
		if got := LocationOf(addr); got != want {
			t.Errorf("%q: %q, want %q", addr, got, want)
		}
	}
}

// Plex gives no reasons, so they are worked out: a converted picture, a
// converted sound, or only the container changed.
func TestPlexSessionReasons(t *testing.T) {
	mk := func(video, audio string) plexSession {
		var it plexSession
		it.TranscodeSession = &struct {
			VideoDecision    string `json:"videoDecision"`
			AudioDecision    string `json:"audioDecision"`
			VideoCodec       string `json:"videoCodec"`
			AudioCodec       string `json:"audioCodec"`
			SourceVideoCodec string `json:"sourceVideoCodec"`
			SourceAudioCodec string `json:"sourceAudioCodec"`
			Width            int    `json:"width"`
			Height           int    `json:"height"`
		}{VideoDecision: video, AudioDecision: audio, VideoCodec: "h264", Width: 1280, Height: 720}
		return it
	}
	if got := plexSessionInfo(mk("transcode", "copy")); strings.Join(got.TranscodeReasons, ",") != "Video converted" || got.ResolutionOut != "720p" || got.VideoOut != "h264" {
		t.Errorf("video: %+v", got)
	}
	if got := plexSessionInfo(mk("copy", "copy")); strings.Join(got.TranscodeReasons, ",") != "Container changed" {
		t.Errorf("container: %+v", got)
	}
	var direct plexSession
	if got := plexSessionInfo(direct); len(got.TranscodeReasons) != 0 {
		t.Errorf("direct play has reasons: %+v", got)
	}
}
