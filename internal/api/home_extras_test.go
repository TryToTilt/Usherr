package api

import "testing"

func TestExtrasAddressHelpers(t *testing.T) {
	for host, want := range map[string]bool{
		"localhost:8264": true, "127.0.0.1:8264": true, "[::1]:8264": true, "app.localhost": true,
		"192.168.1.20:8264": false, "media.example.com": false,
	} {
		if got := isLocalHostport(host); got != want {
			t.Errorf("isLocalHostport(%q) = %v", host, got)
		}
	}
	for in, want := range map[string]string{
		"192.168.1.20:8264": "http://192.168.1.20:8264", "https://tv.example.com/": "https://tv.example.com", "": "",
	} {
		if got, err := cleanBaseURL(in); err != nil || got != want {
			t.Errorf("cleanBaseURL(%q) = %q, %v", in, got, err)
		}
	}
	if _, err := cleanBaseURL("ftp://x"); err == nil {
		t.Error("ftp accepted")
	}
	if got := joinHostPath(`E:\Streamed\`, "Movies"); got != `E:\Streamed\Movies` {
		t.Errorf("windows join = %q", got)
	}
	if got := joinHostPath("/data/streamed", "TV"); got != "/data/streamed/TV" {
		t.Errorf("unix join = %q", got)
	}
}

func TestStripPlaylistKeys(t *testing.T) {
	in := "#EXTM3U\nmain.m3u8?DeviceId=d&api_key=SECRET&VideoCodec=h264\nhls1/main/0.ts?api_key=SECRET\nseg.ts?PlaySessionId=p&ApiKey=SECRET\n"
	got := string(stripPlaylistKeys([]byte(in)))
	want := "#EXTM3U\nmain.m3u8?DeviceId=d&VideoCodec=h264\nhls1/main/0.ts\nseg.ts?PlaySessionId=p\n"
	if got != want {
		t.Fatalf("got %q", got)
	}
}
