package music

import (
	"regexp"
	"strings"
)

// Release sanity checks before a grab: the wrong kind of edition, and a
// size too small for the music it claims to be.
//
// Adapted from DroppedNeedle (AGPL-3.0),
// backend/services/native/acquisition/specs (wrong_edition) and
// backend/services/native/newznab_release_scorer.py (size plausibility).

// wrongEditionWords are kinds of release that are not the album itself,
// unless the album's own name says so. "Remaster" and "deluxe" are fine.
var wrongEditionWords = regexp.MustCompile(`(?i)\b(live|bootleg|karaoke|tribute|instrumentals?|a ?cappella|acapella|outtakes|rehearsals?|greatest hits|best of|cover versions?|in the style of|ringtones?)\b`)

// WrongEdition returns the word that makes a release title another kind of
// release than the album (a karaoke or tribute version, a live bootleg), or
// "" when it fits. Words that are in the artist's or album's own name don't count.
func WrongEdition(title, artist, album string) string {
	flat := strings.NewReplacer("_", " ", ".", " ").Replace(title)
	own := strings.ToLower(artist + " " + album)
	for _, m := range wrongEditionWords.FindAllString(flat, -1) {
		w := strings.ToLower(m)
		if !strings.Contains(own, w) && !strings.Contains(strings.ReplaceAll(own, " ", ""), strings.ReplaceAll(w, " ", "")) {
			return w
		}
	}
	return ""
}

// nominalKbps is the bitrate a tier's files have at least on average.
func nominalKbps(t Tier) int {
	switch t {
	case TierFLAC, TierFLAC24:
		return 700
	case TierMP3320:
		return 320
	case TierMP3256, TierAAC256:
		return 256
	case TierMP3192:
		return 192
	}
	return 0
}

// TooSmall reports whether a release of sizeBytes is too small to be an
// album of albumMs (its tracks' total length) in tier t: under 35% of what
// the tier's bitrate would need. A fake, a sample pack or a single track
// passed off as the album. Unknown sizes, lengths or tiers are never too small.
func TooSmall(sizeBytes int64, albumMs int64, t Tier) bool {
	kbps := nominalKbps(t)
	if sizeBytes <= 0 || albumMs <= 0 || kbps == 0 {
		return false
	}
	need := float64(kbps) * 1000 / 8 * float64(albumMs) / 1000
	return float64(sizeBytes) < 0.35*need
}
