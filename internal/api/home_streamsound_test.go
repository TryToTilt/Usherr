package api

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/mediaservers"
)

func TestPickStreamAudio(t *testing.T) {
	multi := []streamAudio{{index: 1, lang: "fre"}, {index: 2, lang: "eng"}, {index: 3, lang: "spa"}}
	cases := []struct {
		list  []streamAudio
		asked int
		lang  string
		want  int
	}{
		{nil, -1, "en", -1},
		{multi, -1, "en", 2}, // a MULTi release: the language wanted, not the first
		{multi, -1, "fr", 1}, // the first is the language wanted
		{multi, -1, "", 1},   // no language: the first
		{multi, -1, "de", 1}, // not in the file: the first
		{multi, 3, "en", 3},  // picked in the player
		{multi, 9, "en", 2},  // a track that isn't there: as if not asked
		{[]streamAudio{{index: 1}, {index: 2, lang: "eng"}}, -1, "en", 1}, // an unmarked first track is kept
	}
	for _, c := range cases {
		if got := pickStreamAudio(c.list, c.asked, c.lang).index; got != c.want {
			t.Errorf("pickStreamAudio(%v, %d, %q) = %d, want %d", c.list, c.asked, c.lang, got, c.want)
		}
	}
}

// has says whether args has flag followed by value.
func has(args []string, flag, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}

func TestStreamSoundArgs(t *testing.T) {
	args := streamSoundArgs("http://x/v.mkv", streamAudio{index: 2, channels: 6}, 61.5)
	for _, want := range [][2]string{{"-ss", "61.500"}, {"-map", "0:2"}, {"-c:a", "aac"}, {"-f", "adts"}, {"-readrate", "1"}} {
		if !has(args, want[0], want[1]) {
			t.Errorf("args lack %v: %v", want, args)
		}
	}
	if slices.Contains(args, "-ac") {
		t.Errorf("5.1 should stay 5.1: %v", args)
	}
	if slices.Index(args, "-ss") > slices.Index(args, "-i") {
		t.Error("-ss must come before -i, to seek the input")
	}
	if slices.Contains(streamSoundArgs("http://x/v.mkv", streamAudio{index: 1}, 0), "-ss") {
		t.Error("no -ss from the start")
	}
	if a := streamSoundArgs("u", streamAudio{index: 1, channels: 8}, 0); !has(a, "-ac", "6") {
		t.Errorf("7.1 becomes 5.1: %v", a)
	}
	// Sound that begins half a second into the film waits for it.
	if a := streamSoundArgs("u", streamAudio{index: 1, lead: 0.5}, 0); !has(a, "-af", "adelay=500:all=1") {
		t.Errorf("a late-starting sound is padded: %v", a)
	}
	if a := streamSoundArgs("u", streamAudio{index: 1, lead: 0.5}, 60); slices.Contains(a, "-af") {
		t.Errorf("no padding past its start: %v", a)
	}
}

// A conversion keeps what the browser plays: 4K stays 4K.
func TestConvertArgsKeepTheBest(t *testing.T) {
	uhd := fileCodecs{Format: "matroska,webm", Video: "hevc", PixFmt: "yuv420p10le", Transfer: "smpte2084", Height: 2160, Audio: "eac3", Channels: 6}

	// The browser shows the picture; only the sound is converted, with its 5.1.
	args := convertArgs("/m/a.mkv", uhd, 0, -1, 6, -1, true, false)
	if !has(args, "-c:v", "copy") || !has(args, "-tag:v", "hvc1") {
		t.Errorf("picture should be copied: %v", args)
	}
	if !has(args, "-c:a", "aac") || slices.Contains(args, "-ac") {
		t.Errorf("sound should become AAC with its channels: %v", args)
	}
	// The picture must be converted: same size, no scale.
	args = convertArgs("/m/a.mkv", uhd, 0, -1, 6, -1, false, false)
	if !has(args, "-c:v", "libx264") || strings.Contains(strings.Join(args, " "), "scale") {
		t.Errorf("re-encode at full size: %v", args)
	}
	if !has(args, "-preset", "ultrafast") {
		t.Errorf("4K takes the faster setting: %v", args)
	}
	// Both play: both copied.
	args = convertArgs("/m/a.mkv", uhd, 0, 3, 2, -1, true, true)
	if !has(args, "-c:a", "copy") || !has(args, "-map", "0:3") {
		t.Errorf("sound copied, track kept: %v", args)
	}
	// A picture subtitle is drawn in: the picture is re-encoded, at its size.
	args = convertArgs("/m/a.mkv", uhd, 0, -1, 6, 5, true, false)
	if !has(args, "-c:v", "libx264") || strings.Contains(strings.Join(args, " "), "scale") {
		t.Errorf("burn-in re-encodes at full size: %v", args)
	}
}

func TestPrepareChoices(t *testing.T) {
	chrome := browserCaps{HEVC: true, HDR: true, MKV: true}
	uhd := fileCodecs{Video: "hevc", Transfer: "smpte2084", Audio: "eac3"}
	if !picturePlays(uhd, chrome) {
		t.Error("HEVC HDR plays in a browser that shows both")
	}
	if picturePlays(uhd, browserCaps{HEVC: true}) {
		t.Error("HDR needs HDR")
	}
	if soundPlays("eac3", chrome) || soundPlays("truehd", chrome) || !soundPlays("aac", chrome) {
		t.Error("Dolby never counts; AAC does")
	}
}

func TestTVBrowserArgs(t *testing.T) {
	args := tvBrowserArgs("http://x/a.avi", 30*time.Second, true)
	if !has(args, "-c:v", "copy") || strings.Contains(strings.Join(args, " "), "scale") {
		t.Errorf("H.264 in an AVI keeps its picture: %v", args)
	}
	args = tvBrowserArgs("http://x/a.avi", 0, false)
	if !has(args, "-c:v", "libx264") || strings.Contains(strings.Join(args, " "), "scale") || slices.Contains(args, "-ac") {
		t.Errorf("re-encode at its own size, with its channels: %v", args)
	}
}

func TestDeviceProfileKeepsTheBest(t *testing.T) {
	p := browserCaps{HEVC: true}.deviceProfile()
	tp := p["TranscodingProfiles"].([]map[string]any)[0]
	if tp["Container"] != "mp4" || !strings.HasPrefix(tp["VideoCodec"].(string), "hevc") {
		t.Errorf("HEVC is kept, in MP4 pieces: %v", tp)
	}
	if tp["MaxAudioChannels"] != "6" {
		t.Errorf("5.1 stays 5.1: %v", tp)
	}
	if p["MaxStreamingBitrate"].(int) < 200_000_000 {
		t.Error("the bitrate allowed is above any 4K film's")
	}
	tp = browserCaps{}.deviceProfile()["TranscodingProfiles"].([]map[string]any)[0]
	if tp["Container"] != "ts" || tp["VideoCodec"] != "h264" {
		t.Errorf("a browser without HEVC gets H.264: %v", tp)
	}
}

func TestReleaseSound(t *testing.T) {
	cases := []struct{ release, sound, chans string }{
		{"Star.Wars.1977.2160p.UHD.BluRay.REMUX.HEVC.HDR10.10bit.TrueHD.7.1.Multi", "TrueHD", "7.1"},
		{"Dune.Part.Two.2024.2160p.WEB-DL.DDP5.1.Atmos.DV.HDR.H.265", "Dolby Atmos", "5.1"},
		{"Show.S01E01.1080p.WEB.h264-GRP", "", ""},
		{"Film.2020.1080p.BluRay.DTS-HD.MA.5.1.x264", "DTS-HD MA", "5.1"},
		{"Film.2020.1080p.AMZN.WEB-DL.DD+.2.0.H.264", "Dolby Digital+", "Stereo"},
	}
	for _, c := range cases {
		if s, ch := releaseSound(c.release); s != c.sound || ch != c.chans {
			t.Errorf("releaseSound(%q) = %q %q, want %q %q", c.release, s, ch, c.sound, c.chans)
		}
	}
}

// The media info's codec names read as the right sound.
func TestMediaInfoSound(t *testing.T) {
	cases := []struct {
		codec, title string
		atmos        bool
		ch           int
		want         string
	}{
		{"TrueHD", "", true, 8, "Dolby Atmos"},
		{"TrueHD", "", false, 8, "TrueHD"},
		{"E-AC-3", "", false, 6, "Dolby Digital+"},
		{"E-AC-3", "", true, 6, "Dolby Atmos"},
		{"DTS-HD MA", "DTS:X 7.1", false, 8, "DTS:X"},
		{"DTS-HD MA", "", false, 6, "DTS-HD MA"},
		{"AC-3", "", false, 6, "Dolby Digital"},
	}
	for _, c := range cases {
		about := c.codec + " " + c.title
		if c.atmos {
			about += " atmos"
		}
		if got, _ := mediaservers.SoundLabel(mediaInfoCodec(c.codec), about, c.ch); got != c.want {
			t.Errorf("%s %q atmos=%v: %q, want %q", c.codec, c.title, c.atmos, got, c.want)
		}
	}
}
