package api

import (
	"strings"
	"testing"
)

// A browser gets the file as it is when it plays it; a phone gets a 4K
// trailer's 1080p copy; what plays nowhere as it is (Dolby Vision HEVC in MKV
// with TrueHD) is converted; and what plays always has a converted fallback.
func TestFitPreshow(t *testing.T) {
	pc := browserCaps{VP9: true, HEVC: true, MKV: true}
	phone := browserCaps{VP9: true}
	vp9 := fileCodecs{Format: "mov,mp4,m4a,3gp,3g2,mj2", Video: "vp9", PixFmt: "yuv420p", Height: 2160, Audio: "aac", Channels: 2}
	h264 := fileCodecs{Format: "mov,mp4,m4a,3gp,3g2,mj2", Video: "h264", PixFmt: "yuv420p", Height: 1080, Audio: "aac", Channels: 2}
	dv := fileCodecs{Format: "matroska,webm", Video: "hevc", PixFmt: "yuv420p10le", Transfer: "smpte2084", Height: 2160, Audio: "truehd", Channels: 8}
	const tr = "/api/home/preshow/trailers/7"

	if f := fitPreshow(tr, vp9, "t.mp4", &h264, "t.1080.mp4", pc, false); f.URL != tr+"/file" || f.Fallback == "" {
		t.Errorf("a 4K screen that plays VP9: %+v", f)
	}
	if f := fitPreshow(tr, vp9, "t.mp4", &h264, "t.1080.mp4", phone, true); f.URL != tr+"/file?copy=1" {
		t.Errorf("a phone and a 4K trailer with a copy: %+v", f)
	}
	if f := fitPreshow(tr, vp9, "t.mp4", nil, "", phone, true); f.URL != tr+"/convert?cv=0&ca=1" {
		t.Errorf("a phone and a 4K trailer without a copy: %+v (want the picture made 1080p, the sound kept)", f)
	}
	const clip = "/api/home/preshow/clips/3"
	f := fitPreshow(clip, dv, "Dolby.mkv", nil, "", pc, false)
	if !strings.HasPrefix(f.URL, clip+"/convert?") || !strings.Contains(f.URL, "ca=0") || f.Fallback != "" {
		t.Errorf("Dolby Vision with TrueHD: %+v", f)
	}
}

// A conversion copies what plays and makes the rest H.264 and AAC, never
// above 1080p.
func TestPreshowConvertArgs(t *testing.T) {
	vp9 := fileCodecs{Video: "vp9", Height: 2160, Audio: "aac", Channels: 2}
	got := strings.Join(preshowConvertArgs("t.mp4", vp9, false, true), " ")
	for _, want := range []string{"libx264", "min(1080,ih)", "-c:a copy", "pipe:1"} {
		if !strings.Contains(got, want) {
			t.Errorf("args lack %q: %s", want, got)
		}
	}
	if got := strings.Join(preshowConvertArgs("c.mkv", fileCodecs{Video: "hevc", Audio: "truehd", Channels: 8}, true, false), " "); !strings.Contains(got, "-c:v copy -tag:v hvc1") || !strings.Contains(got, "-c:a aac") {
		t.Errorf("a picture that plays, a sound that doesn't: %s", got)
	}
	if trailerCopyName("movie-1.mp4") != "movie-1.1080.mp4" {
		t.Error(trailerCopyName("movie-1.mp4"))
	}
}

// A reset email names the address a request came from only when it says
// something: Docker Desktop shows every device at home as 172.x.x.x.
func TestResetAskedFrom(t *testing.T) {
	for ip, want := range map[string]string{
		"172.19.0.1":   "from a device on your home network",
		"192.168.1.20": "from a device on your home network",
		"127.0.0.1":    "from a device on your home network",
		"100.101.1.2":  "from the address 100.101.1.2", // Tailscale
		"bad":          "from an unknown address",
	} {
		if got := askedFrom(ip); got != want {
			t.Errorf("askedFrom(%q) = %q, want %q", ip, got, want)
		}
	}
}
