package music_test

import (
	"testing"

	"github.com/rdborg/mediarium/internal/music"
)

func TestWrongEdition(t *testing.T) {
	for _, tc := range []struct{ title, artist, album, want string }{
		{"Fixture_Band-First_Album-WEB-FLAC-2020", "Fixture Band", "First Album", ""},
		{"Fixture Band - First Album (Deluxe Remastered) FLAC", "Fixture Band", "First Album", ""},
		{"Fixture.Band.First.Album.Karaoke.Version.MP3", "Fixture Band", "First Album", "karaoke"},
		{"Various - A Tribute To Fixture Band First Album", "Fixture Band", "First Album", "tribute"},
		{"Fixture Band - First Album Live Bootleg 1999", "Fixture Band", "First Album", "live"},
		{"Fixture Band - Live At The Hall FLAC", "Fixture Band", "Live At The Hall", ""}, // the album is live
		{"Live - Throwing Copper FLAC", "Live", "Throwing Copper", ""},                   // the band is called Live
		{"Fixture Band - Greatest Hits", "Fixture Band", "Greatest Hits", ""},
	} {
		if got := music.WrongEdition(tc.title, tc.artist, tc.album); got != tc.want {
			t.Errorf("%q: %q, want %q", tc.title, got, tc.want)
		}
	}
}

func TestTooSmall(t *testing.T) {
	album := int64(40 * 60 * 1000) // 40 minutes
	// FLAC at 700 kbit/s for 40 min is 210 MB; 35% is 73.5 MB.
	if !music.TooSmall(50<<20, album, music.TierFLAC) {
		t.Error("a 50 MB 'FLAC' album of 40 minutes is too small")
	}
	if music.TooSmall(250<<20, album, music.TierFLAC) {
		t.Error("a 250 MB FLAC album is fine")
	}
	if !music.TooSmall(5<<20, album, music.TierMP3320) || music.TooSmall(90<<20, album, music.TierMP3320) {
		t.Error("MP3-320: 5 MB is too small, 90 MB fine")
	}
	if music.TooSmall(1, 0, music.TierFLAC) || music.TooSmall(0, album, music.TierFLAC) || music.TooSmall(1, album, music.TierUnknown) {
		t.Error("unknown size, length or tier is never too small")
	}
}
