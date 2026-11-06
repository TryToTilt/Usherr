// Adapted from DroppedNeedle (AGPL-3.0), backend/infrastructure/audio/metadata_writer.py

package musictidy

import (
	"path/filepath"
	"strconv"
	"strings"
)

// The tags tidying writes, by Usherr's own names. Each format stores them
// under its own names (see fieldNames).
const (
	TagTitle          = "title"
	TagArtist         = "artist"
	TagAlbum          = "album"
	TagAlbumArtist    = "albumartist"
	TagTrack          = "track"
	TagTrackTotal     = "tracktotal"
	TagDisc           = "disc"
	TagDiscTotal      = "disctotal"
	TagDate           = "date"
	TagReleaseGroupID = "mb_releasegroupid"
	TagReleaseID      = "mb_releaseid"
	TagRecordingID    = "mb_recordingid"
	TagReleaseTrackID = "mb_releasetrackid"
	TagArtistID       = "mb_artistid"
	TagAlbumArtistID  = "mb_albumartistid"
)

// TagOrder is the order tags are listed in.
var TagOrder = []string{TagTitle, TagArtist, TagAlbum, TagAlbumArtist, TagTrack, TagTrackTotal, TagDisc, TagDiscTotal, TagDate,
	TagReleaseGroupID, TagReleaseID, TagRecordingID, TagReleaseTrackID, TagArtistID, TagAlbumArtistID}

// Family is how a file format stores tags.
type Family string

const (
	FamilyVorbis Family = "vorbis" // FLAC, Ogg Vorbis, Opus: Vorbis comments
	FamilyOgg    Family = "ogg"    // Vorbis comments kept on the Ogg stream
	FamilyID3    Family = "id3"    // MP3: ID3v2
	FamilyMP4    Family = "mp4"    // M4A/ALAC: iTunes atoms
	FamilyNone   Family = ""       // no tags tidying can write (raw AAC)
)

// FamilyOf is the tag family of a file, by its extension.
func FamilyOf(path string) Family {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".flac":
		return FamilyVorbis
	case ".ogg", ".opus":
		return FamilyOgg
	case ".mp3":
		return FamilyID3
	case ".m4a", ".alac", ".mp4":
		return FamilyMP4
	}
	return FamilyNone
}

// CanEmbedCover reports whether ffmpeg can put a cover picture in this
// family's files.
func (f Family) CanEmbedCover() bool {
	return f == FamilyVorbis || f == FamilyID3 || f == FamilyMP4
}

// Writable reports whether tidying writes this tag in this family. ffmpeg
// can't write MusicBrainz ids into MP4 files (they are freeform atoms) nor
// the recording id into MP3 (a UFID frame), so those are left as they are.
func (f Family) Writable(tag string) bool {
	switch f {
	case FamilyVorbis, FamilyOgg:
		return true
	case FamilyID3:
		return tag != TagRecordingID
	case FamilyMP4:
		return !strings.HasPrefix(tag, "mb_")
	}
	return false
}

// readNames are the (lowercased) names ffprobe may show each tag under, in
// any family.
var readNames = map[string][]string{
	TagTitle:          {"title"},
	TagArtist:         {"artist"},
	TagAlbum:          {"album"},
	TagAlbumArtist:    {"album_artist", "albumartist", "album artist"},
	TagDate:           {"date", "year"},
	TagReleaseGroupID: {"musicbrainz_releasegroupid", "musicbrainz release group id"},
	TagReleaseID:      {"musicbrainz_albumid", "musicbrainz album id"},
	TagRecordingID:    {"musicbrainz_trackid", "musicbrainz track id"},
	TagReleaseTrackID: {"musicbrainz_releasetrackid", "musicbrainz release track id"},
	TagArtistID:       {"musicbrainz_artistid", "musicbrainz artist id"},
	TagAlbumArtistID:  {"musicbrainz_albumartistid", "musicbrainz album artist id"},
}

// ReadTags turns what ffprobe read (lowercased keys) into Usherr's tag
// names. Numbers are plain ("3", not "03" or "3/12").
func ReadTags(raw map[string]string) map[string]string {
	out := map[string]string{}
	for tag, names := range readNames {
		for _, n := range names {
			if v := strings.TrimSpace(raw[n]); v != "" {
				out[tag] = v
				break
			}
		}
	}
	num := func(tag, total string, names []string, totals []string) {
		for _, n := range names {
			if v := strings.TrimSpace(raw[n]); v != "" {
				a, b, _ := strings.Cut(v, "/")
				if x := atoi(a); x > 0 {
					out[tag] = strconv.Itoa(x)
				}
				if y := atoi(b); y > 0 {
					out[total] = strconv.Itoa(y)
				}
				break
			}
		}
		for _, n := range totals {
			if y := atoi(raw[n]); y > 0 {
				out[total] = strconv.Itoa(y)
				break
			}
		}
	}
	num(TagTrack, TagTrackTotal, []string{"track", "tracknumber"}, []string{"tracktotal", "totaltracks"})
	num(TagDisc, TagDiscTotal, []string{"disc", "discnumber"}, []string{"disctotal", "totaldiscs"})
	return out
}

func atoi(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}

// SameValue compares two values of a tag: numbers as numbers, the rest
// exactly (after trimming).
func SameValue(tag, a, b string) bool {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	switch tag {
	case TagTrack, TagTrackTotal, TagDisc, TagDiscTotal:
		return atoi(a) == atoi(b)
	case TagReleaseGroupID, TagReleaseID, TagRecordingID, TagReleaseTrackID, TagArtistID, TagAlbumArtistID:
		return strings.EqualFold(a, b)
	}
	return a == b
}

// mbNames are the native names of the MusicBrainz ids: Vorbis comments
// (Picard's names) and ID3 TXXX descriptions.
var mbNames = map[string][2]string{
	TagReleaseGroupID: {"MUSICBRAINZ_RELEASEGROUPID", "MusicBrainz Release Group Id"},
	TagReleaseID:      {"MUSICBRAINZ_ALBUMID", "MusicBrainz Album Id"},
	TagRecordingID:    {"MUSICBRAINZ_TRACKID", ""},
	TagReleaseTrackID: {"MUSICBRAINZ_RELEASETRACKID", "MusicBrainz Release Track Id"},
	TagArtistID:       {"MUSICBRAINZ_ARTISTID", "MusicBrainz Artist Id"},
	TagAlbumArtistID:  {"MUSICBRAINZ_ALBUMARTISTID", "MusicBrainz Album Artist Id"},
}

// MetadataArgs are the ffmpeg -metadata arguments that set tags in a file
// of family f. want holds the tags to set (all of them, not only the
// changed ones, so the numbers and their totals stay together). ffmpeg's
// own generic names are used where it has one, so a tag is never written
// twice under two names.
func MetadataArgs(f Family, want map[string]string) []string {
	flag := "-metadata"
	if f == FamilyOgg {
		flag = "-metadata:s:a:0" // Ogg keeps its comments on the stream
	}
	var args []string
	set := func(k, v string) { args = append(args, flag, k+"="+v) }
	numbered := func(n, total string) string {
		if total != "" && atoi(total) > 0 {
			return n + "/" + total
		}
		return n
	}
	for _, tag := range TagOrder {
		v, ok := want[tag]
		if !ok || !f.Writable(tag) {
			continue
		}
		switch tag {
		case TagTitle, TagArtist, TagAlbum, TagDate:
			set(tag, v)
		case TagAlbumArtist:
			set("album_artist", v)
		case TagTrack:
			if f == FamilyVorbis || f == FamilyOgg {
				set("track", v)
			} else {
				set("track", numbered(v, want[TagTrackTotal]))
			}
		case TagDisc:
			if f == FamilyVorbis || f == FamilyOgg {
				set("disc", v)
			} else {
				set("disc", numbered(v, want[TagDiscTotal]))
			}
		case TagTrackTotal:
			if f == FamilyVorbis || f == FamilyOgg { // both names, as Picard writes them
				set("TRACKTOTAL", v)
				set("TOTALTRACKS", v)
			}
		case TagDiscTotal:
			if f == FamilyVorbis || f == FamilyOgg {
				set("DISCTOTAL", v)
				set("TOTALDISCS", v)
			}
		default:
			names := mbNames[tag]
			if f == FamilyID3 {
				if names[1] != "" {
					set(names[1], v)
				}
			} else {
				set(names[0], v)
			}
		}
	}
	return args
}
