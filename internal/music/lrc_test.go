package music_test

import (
	"reflect"
	"testing"

	"github.com/rdborg/mediarium/internal/music"
)

func TestParseLRC(t *testing.T) {
	text := "\xef\xbb\xbf[ar:Fixture Band]\r\n[ti:Opening]\r\n[offset:+500]\r\n" +
		"[00:12.34]first line\r\n" +
		"[01:02.5][00:30.00] chorus <00:31.00>with <00:32.00>words\r\n" +
		"[00:05]\r\n" +
		"no stamp here\r\n" +
		"[00:00.10]early\n"
	got := music.ParseLRC(text)
	want := []music.LRCLine{
		{StartMs: 0, Text: "early"}, // 100 ms - 500 ms offset, kept at 0
		{StartMs: 4500, Text: ""},
		{StartMs: 11840, Text: "first line"},
		{StartMs: 29500, Text: "chorus with words"},
		{StartMs: 62000, Text: "chorus with words"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
	if len(music.ParseLRC("just words\nno times")) != 0 {
		t.Fatal("plain text has no synced lines")
	}
	if got := music.ParseLRC("[00:01.005]three digits"); got[0].StartMs != 1005 {
		t.Fatalf("milliseconds: %+v", got)
	}
}
