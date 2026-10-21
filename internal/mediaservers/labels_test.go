package mediaservers

import "testing"

func TestAspectLabel(t *testing.T) {
	cases := []struct {
		w, h float64
		want string
	}{
		{3840, 1600, "2.39:1"}, {1920, 800, "2.39:1"}, {1920, 816, "2.39:1"}, {3840, 2160, "16:9"},
		{1998, 1080, "1.85:1"}, {1920, 1038, "1.85:1"}, {1440, 1080, "4:3"}, {3840, 2010, "1.90:1"},
		{2048, 1432, "1.43:1"}, {3840, 1920, "2:1"}, {1000, 333, "3.00:1"}, {0, 1080, ""},
	}
	for _, c := range cases {
		if got := AspectLabel(c.w, c.h); got != c.want {
			t.Errorf("AspectLabel(%v, %v) = %q, want %q", c.w, c.h, got, c.want)
		}
	}
}

func TestSoundLabel(t *testing.T) {
	cases := []struct {
		codec, about string
		ch           int
		sound, chans string
	}{
		{"truehd", "Dolby TrueHD + Dolby Atmos", 8, "Dolby Atmos", "7.1"},
		{"eac3", "Dolby Digital Plus + Dolby Atmos", 6, "Dolby Atmos", "5.1"},
		{"dts", "DTS-HD MA + DTS:X", 8, "DTS:X", "7.1"},
		{"dts", "DTS-HD MA", 6, "DTS-HD MA", "5.1"},
		{"truehd", "", 6, "TrueHD", "5.1"},
		{"ac3", "", 2, "Dolby Digital", "Stereo"},
		{"aac", "LC", 2, "", "Stereo"},
	}
	for _, c := range cases {
		s, ch := SoundLabel(c.codec, c.about, c.ch)
		if s != c.sound || ch != c.chans {
			t.Errorf("SoundLabel(%q, %q, %d) = %q %q, want %q %q", c.codec, c.about, c.ch, s, ch, c.sound, c.chans)
		}
	}
}
