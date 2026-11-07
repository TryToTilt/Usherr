package organizer

import "testing"

func TestIsVideoFileIgnoresCase(t *testing.T) {
	for _, name := range []string{"a.mkv", "a.MKV", "a.Mkv", "/x/y.MP4"} {
		if !IsVideoFile(name) {
			t.Errorf("%s is a video", name)
		}
	}
	if IsVideoFile("a.TXT") {
		t.Error("a.TXT is not a video")
	}
}
