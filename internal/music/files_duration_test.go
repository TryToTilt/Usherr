package music_test

import (
	"testing"

	"github.com/rdborg/mediarium/internal/music"
)

func TestMatchTracksLeavesOutFilesOfTheWrongLength(t *testing.T) {
	slots := []music.TrackSlot{
		{Disc: 1, Position: 1, Title: "Opening", LengthMs: 200000},
		{Disc: 1, Position: 2, Title: "Middle Song", LengthMs: 600000},
		{Disc: 1, Position: 3, Title: "Closing Time", LengthMs: 0}, // unknown: no check
	}
	files := []music.FileInfo{
		{Path: "01.flac", Disc: 1, Track: 1, Title: "Opening", DurationSec: 212},     // 12 s off: fine (15 s)
		{Path: "02.flac", Disc: 1, Track: 2, Title: "Middle Song", DurationSec: 30},  // a 30-second sample
		{Path: "03.flac", Disc: 1, Track: 3, Title: "Closing Time", DurationSec: 99}, // track length unknown
		{Path: "04.flac", Disc: 1, Track: 2, DurationSec: 655},                       // 55 s off a 10-min track: within 10%
	}
	matches, unmatched := music.MatchTracks(files, slots)
	got := map[int]int{}
	for _, m := range matches {
		got[m.Slot] = m.File
	}
	if len(matches) != 3 || got[0] != 0 || got[1] != 3 || got[2] != 2 {
		t.Fatalf("matches: %+v", matches)
	}
	if len(unmatched) != 1 || unmatched[0] != 1 {
		t.Fatalf("the sample is left out: %v", unmatched)
	}
	// Files without a known length match as before.
	files[1].DurationSec = 0
	files[3].DurationSec = 0
	if m, _ := music.MatchTracks(files, slots); len(m) != 3 {
		t.Fatalf("unknown lengths: %+v", m)
	}
}
