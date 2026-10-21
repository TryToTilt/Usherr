package mediaservers

import "testing"

// Usherr: the Marquee's badges name the picture and the sound of what plays.
func TestDescribeStreams(t *testing.T) {
	cases := []struct {
		name    string
		streams []mediaStream
		audio   int
		want    Playing
	}{
		{"4K Dolby Vision with Atmos (the second audio track chosen)", []mediaStream{
			{Type: "Video", Width: 3840, Height: 1608, VideoRangeType: "DOVIWithHDR10"},
			{Type: "Audio", Codec: "ac3", Channels: 6, Index: 1},
			{Type: "Audio", Codec: "truehd", Channels: 8, DisplayTitle: "English - Dolby TrueHD + Dolby Atmos - 7.1", Index: 2},
		}, 2, Playing{Resolution: "4K", HDR: "Dolby Vision", Audio: "Dolby Atmos", Channels: "7.1"}},
		{"1080p HDR10 DTS-HD MA", []mediaStream{
			{Type: "Video", Width: 1920, Height: 800, VideoRangeType: "HDR10"},
			{Type: "Audio", Codec: "dts", Profile: "DTS-HD MA", Channels: 6, Index: 1},
		}, 1, Playing{Resolution: "1080p", HDR: "HDR10", Audio: "DTS-HD MA", Channels: "5.1"}},
		{"720p stereo AAC, SDR", []mediaStream{
			{Type: "Video", Width: 1280, Height: 720, VideoRangeType: "SDR"},
			{Type: "Audio", Codec: "aac", Channels: 2, Index: 1},
		}, 1, Playing{Resolution: "720p", Channels: "Stereo"}},
		{"nothing known", nil, 0, Playing{}},
	}
	for _, c := range cases {
		var p Playing
		describeStreams(&p, c.streams, c.audio)
		if p.Resolution != c.want.Resolution || p.HDR != c.want.HDR || p.Audio != c.want.Audio || p.Channels != c.want.Channels {
			t.Errorf("%s: got %q %q %q %q", c.name, p.Resolution, p.HDR, p.Audio, p.Channels)
		}
	}
}
