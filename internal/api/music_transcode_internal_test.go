package api

import (
	"strings"
	"testing"
)

func TestDecideTranscodeKeepsWhatFitsAndCapsTheRest(t *testing.T) {
	for _, tc := range []struct {
		name      string
		req       string
		clientMax int
		src       string
		srcKbps   int
		lossless  bool
		want      transcodePlan
	}{
		{"nothing asked", "", 0, "", 900, true, transcodePlan{}},
		{"raw always wins", "raw", 96, "", 900, true, transcodePlan{}},
		{"mp3 under the cap stays", "", 192, "mp3", 128, false, transcodePlan{}},
		{"mp3 over the cap", "", 128, "mp3", 320, false, transcodePlan{"opus", 128}},
		{"flac with a cap", "", 160, "", 900, true, transcodePlan{"opus", 160}},
		{"format asked, same as the file", "mp3", 0, "mp3", 320, false, transcodePlan{}},
		{"format asked, other than the file", "aac", 0, "mp3", 192, false, transcodePlan{"aac", 192}},
		{"server ceiling, not a reason", "mp3", 0, "", 1000, true, transcodePlan{"mp3", 320}},
		{"no upsampling of a small mp3", "opus", 0, "mp3", 96, false, transcodePlan{"opus", 96}},
		{"at least 64", "", 32, "", 900, true, transcodePlan{"opus", 64}},
		{"unknown format means the default", "flac", 0, "", 900, true, transcodePlan{"opus", 320}},
		{"unknown bitrate with a cap", "", 128, "", 0, false, transcodePlan{"opus", 128}},
	} {
		if got := decideTranscode(tc.req, tc.clientMax, tc.src, tc.srcKbps, tc.lossless, 320, "opus"); got != tc.want {
			t.Errorf("%s: got %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestFFmpegTranscodeArgs(t *testing.T) {
	got := strings.Join(ffmpegTranscodeArgs("/music/a.flac", 12.5, "mp3", 192, "pipe:1"), " ")
	want := "-hide_banner -loglevel error -nostdin -ss 12.500 -i /music/a.flac -vn -map 0:a:0 -map_metadata -1 -c:a libmp3lame -b:a 192k -f mp3 -y pipe:1"
	if got != want {
		t.Fatalf("mp3:\n got %s\nwant %s", got, want)
	}
	if got := strings.Join(ffmpegTranscodeArgs("in", 0, "opus", 96, "out.opus"), " "); !strings.Contains(got, "-c:a libopus -b:a 96k -vbr on -f ogg") || strings.Contains(got, "-ss") {
		t.Fatalf("opus: %s", got)
	}
	if got := strings.Join(ffmpegTranscodeArgs("in", 0, "aac", 128, "out.aac"), " "); !strings.Contains(got, "-c:a aac -b:a 128k -f adts") {
		t.Fatalf("aac: %s", got)
	}
}
