package api

import (
	"strings"
	"testing"
	"time"
)

func TestPickAudioPrefersTheWantedLanguage(t *testing.T) {
	tracks := []playTrack{
		{Index: 0, Kind: "text"},
		{Index: 1, Kind: "audio", Lang: "jpn", Codec: "eac3"},
		{Index: 2, Kind: "audio", Lang: "eng", Codec: "aac"},
	}
	if tr, _ := pickAudio(tracks, nil, "en"); tr.Index != 2 {
		t.Fatalf("English wanted: got track %d", tr.Index)
	}
	if tr, _ := pickAudio(tracks, nil, ""); tr.Index != 1 {
		t.Fatalf("no language: the first, got %d", tr.Index)
	}
	asked := 1
	if tr, _ := pickAudio(tracks, &asked, "en"); tr.Index != 1 {
		t.Fatalf("the one asked for wins, got %d", tr.Index)
	}
	unknown := []playTrack{{Index: 1, Kind: "audio"}, {Index: 2, Kind: "audio", Lang: "eng"}}
	if tr, _ := pickAudio(unknown, nil, "eng"); tr.Index != 1 {
		t.Fatalf("a first track of unknown language stays, got %d", tr.Index)
	}
}

func TestShiftVTT(t *testing.T) {
	in := "WEBVTT\n\n00:00:05.000 --> 00:00:07.000\nGone\n\n00:01:01.500 --> 00:01:03.000 align:start\nKept\n\n"
	out := string(shiftVTT([]byte(in), 60*time.Second))
	if strings.Contains(out, "Gone") || !strings.Contains(out, "00:00:01.500 --> 00:00:03.000 align:start\nKept") {
		t.Fatalf("got %q", out)
	}
}
