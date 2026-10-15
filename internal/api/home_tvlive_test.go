package api

import (
	"strings"
	"testing"
)

func TestPlutoChannel(t *testing.T) {
	for in, want := range map[string]string{
		"https://jmp2.uk/plu-6350fdd266e9ea0007bedec5.m3u8":                                         "6350fdd266e9ea0007bedec5",
		"https://stitcher-ipv4.pluto.tv/v2/stitch/hls/channel/6350fdd266e9ea0007bedec5/master.m3u8": "6350fdd266e9ea0007bedec5",
		"https://pbs.lls.cdn.pbs.org/est/index.m3u8":                                                "",
	} {
		if got := plutoChannel(in); got != want {
			t.Errorf("plutoChannel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsPlaylist(t *testing.T) {
	if !isPlaylist([]byte("\ufeff#EXTM3U\n#EXT")) || !isPlaylist([]byte("\n#EXTM3U")) {
		t.Error("playlists not recognised")
	}
	if isPlaylist([]byte{0x6b, 0x33, 0x78, 0x10, 0xb0, 0xe9}) { // an AES key labelled as a playlist
		t.Error("a key taken for a playlist")
	}
}

func TestRewritePlaylist(t *testing.T) {
	in := "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin?k=1\"\n#EXT-X-MEDIA:TYPE=AUDIO,URI=\"audio/a.m3u8\"\n#EXTINF:6.0,\nseg1.ts?x=y\nhttps://cdn.example/other/seg2.ts\n"
	out := rewritePlaylist(in, "https://up.example/live/main.m3u8?tok=1", "key", 100)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if lines[0] != "#EXTM3U" || !strings.HasPrefix(lines[3], "#EXTINF") {
		t.Fatalf("tags changed:\n%s", out)
	}
	seg := lines[4]
	if !strings.HasPrefix(seg, "/api/tvlive/key/100/x/") || !strings.HasSuffix(seg, "/seg1.ts") {
		t.Fatalf("segment not relayed: %s", seg)
	}
	parts := strings.Split(seg, "/")
	if u, ok := liveRecall(parts[len(parts)-2]); !ok || u != "https://up.example/live/seg1.ts?x=y" {
		t.Errorf("segment address = %q", u)
	}
	if !strings.HasSuffix(lines[1], `.bin"`) || !strings.HasSuffix(lines[2], `/a.m3u8"`) {
		t.Errorf("names: %s / %s", lines[1], lines[2])
	}
	// No extension: named for what the playlist says it is.
	if got := livePath("key", 1, "https://x.example/beacon/abc?z=1", ".ts"); !strings.HasSuffix(got, "/abc.ts") {
		t.Errorf("livePath = %s", got)
	}
	if !strings.Contains(lines[1], `URI="/api/tvlive/key/100/x/`) || !strings.Contains(lines[2], `/a.m3u8"`) {
		t.Errorf("URI attributes not relayed:\n%s", out)
	}
}
