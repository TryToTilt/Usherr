package api

import (
	"testing"

	"github.com/rdborg/mediarium/internal/quality"
)

func TestExistingTier(t *testing.T) {
	tests := []struct {
		name    string
		stored  string
		release string
		file    string
		want    quality.Tier
		known   bool
	}{
		{"the stored quality wins", "Remux-2160p", "", "/m/x/whatever.mkv", quality.TierRemux2160p, true},
		{"TRaSH name, nothing stored", "", "", "/m/Movie Title (2020) {imdb-tt1234567} {tmdb-123} [Bluray-1080p][DTS 5.1][x264]-GROUP.mkv", quality.TierBluray1080p, true},
		{"TRaSH remux name", "", "", "/m/Movie Title (2020) {imdb-tt1234567} {tmdb-123} [Remux-2160p][DV HDR10][TrueHD Atmos 7.1][h265]-GROUP.mkv", quality.TierRemux2160p, true},
		{"unknown stored quality is read from the name", "Unknown", "", "/m/Movie Title (2020) [WEBDL-1080p][EAC3 5.1][h264]-GROUP.mkv", quality.TierWebDL1080p, true},
		{"the release it came from is the last resort", "", "Movie.Title.2020.2160p.BluRay.x265-GRP", "/m/movie.mkv", quality.TierBluray2160p, true},
		{"nothing to go by", "", "", "/m/movie.mkv", "", false},
		{"no file either", "", "", "", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := existingTier(tc.stored, tc.release, tc.file)
			if got != tc.want || ok != tc.known {
				t.Errorf("existingTier = %q, %v; want %q, %v", got, ok, tc.want, tc.known)
			}
		})
	}
}
