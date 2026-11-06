// Adapted from DroppedNeedle (AGPL-3.0), backend/services/native/library_management_planner.py

package musictidy

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/rdborg/mediarium/internal/music"
)

// Target is the album a folder was identified as, with its tracklist.
type Target struct {
	Artist         string // the album artist as credited
	ArtistID       string // the album artist's MusicBrainz id
	Album          string
	Year           int
	Date           string // "YYYY[-MM[-DD]]"
	ReleaseGroupID string
	ReleaseID      string
	Tracks         []TargetTrack
}

// TargetTrack is one track of the target's tracklist.
type TargetTrack struct {
	LibraryID   int64 // the track in Usherr's library, 0 when the album isn't in it
	Disc        int
	Position    int
	Title       string
	LengthMs    int
	TrackID     string // the release track's MusicBrainz id
	RecordingID string
	Artist      string // the track's credit, "" when unknown
	ArtistIDs   []string
}

// SourceFile is one audio file of the folder as it is now.
type SourceFile struct {
	Path     string
	Size     int64
	ModNs    int64
	Tags     map[string]string // ReadTags of what ffprobe read
	Raw      map[string]string // ffprobe's own tag names (lowercased)
	Pictures int
	Link     bool // the file is a symbolic link
	Track    int  // index in Target.Tracks; -1 when it matched none
}

// Change is one tag that tidying changes.
type Change struct {
	Tag string `json:"tag"`
	Old string `json:"old"`
	New string `json:"new"`
}

// FilePlan is what tidying does to one file.
type FilePlan struct {
	OldPath string            `json:"oldPath"`
	NewPath string            `json:"newPath"`
	TrackID int64             `json:"trackId,omitempty"`
	Set     map[string]string `json:"set,omitempty"` // the tags written (none: the file is only moved)
	Changes []Change          `json:"changes,omitempty"`
	Cover   bool              `json:"cover,omitempty"`
	Size    int64             `json:"size"`
	ModNs   int64             `json:"modNs"`
	// Sidecars are the files that belong to this one by name (its .lrc
	// lyrics), which move along with it.
	Sidecars []Sidecar `json:"sidecars,omitempty"`
}

// Sidecar is a file that moves along with an audio file.
type Sidecar struct {
	OldPath string `json:"oldPath"`
	NewPath string `json:"newPath"`
}

// sidecarExtensions are the files named after a track that belong to it.
var sidecarExtensions = []string{".lrc", ".LRC"}

// Moves reports whether the file gets a new name or folder.
func (f FilePlan) Moves() bool { return f.OldPath != f.NewPath }

// Rewrites reports whether the file is written anew (tags or a cover).
func (f FilePlan) Rewrites() bool { return len(f.Changes) > 0 || f.Cover }

// AlbumPlan is what tidying does to one album folder.
type AlbumPlan struct {
	OldFolder string
	NewFolder string
	Files     []FilePlan
	// Blocked says why the folder is left alone; "" when it can be tidied.
	Blocked string
}

// Unchanged reports whether the folder already looks the way tidying would
// make it.
func (p AlbumPlan) Unchanged() bool {
	if p.Blocked != "" || p.OldFolder != p.NewFolder {
		return false
	}
	for _, f := range p.Files {
		if f.Moves() || f.Rewrites() {
			return false
		}
	}
	return true
}

// Options are the parts of a plan that come from the settings or the
// network.
type Options struct {
	Clean music.Sanitizer
	// CoverAvailable: the Cover Art Archive has a front cover for the
	// album, to embed in files that have none.
	CoverAvailable bool
}

// PlanAlbum works out the new names and tags of one album folder under
// root: the folder becomes <root>/<Artist>/<Album> (<Year>), each file
// "NN - Title.ext" (or "D-NN - Title.ext" on several discs), and the tags
// those of the target. A folder is left alone (Blocked) when any of its audio
// files matched no track, a file is a link, two files would get one name, or
// the new folder is already taken by something else.
func PlanAlbum(root, folder string, t Target, files []SourceFile, o Options) AlbumPlan {
	p := AlbumPlan{OldFolder: folder, NewFolder: music.AlbumPath(root, t.Artist, t.Album, t.Year, o.Clean)}
	unmatched, links := 0, 0
	for _, f := range files {
		if f.Track < 0 || f.Track >= len(t.Tracks) {
			unmatched++
		}
		if f.Link {
			links++
		}
	}
	switch {
	case len(files) == 0:
		p.Blocked = "No audio files in the folder."
		return p
	case unmatched > 0:
		p.Blocked = fmt.Sprintf("%d of %d files couldn't be matched to a track of the album, so the folder is left as it is.", unmatched, len(files))
		return p
	case links > 0:
		p.Blocked = "Some files are links to files elsewhere, so the folder is left as it is."
		return p
	}

	multiDisc, discs := false, 0
	perDisc := map[int]int{}
	for _, tr := range t.Tracks {
		perDisc[tr.Disc]++
		discs = max(discs, tr.Disc)
		if tr.Disc > 1 {
			multiDisc = true
		}
	}

	taken := map[string]string{}
	for _, f := range files {
		tr := t.Tracks[f.Track]
		fam := FamilyOf(f.Path)
		name := music.TrackFileName(tr.Disc, tr.Position, tr.Title, multiDisc, filepath.Ext(f.Path), o.Clean)
		fp := FilePlan{OldPath: f.Path, NewPath: filepath.Join(p.NewFolder, name), TrackID: tr.LibraryID, Size: f.Size, ModNs: f.ModNs}
		key := strings.ToLower(fp.NewPath)
		if other, dup := taken[key]; dup {
			p.Blocked = fmt.Sprintf("%s and %s would get the same name, so the folder is left as it is.", filepath.Base(other), filepath.Base(f.Path))
			return p
		}
		taken[key] = f.Path
		for _, ext := range sidecarExtensions {
			old := strings.TrimSuffix(f.Path, filepath.Ext(f.Path)) + ext
			if _, err := os.Stat(old); err == nil && fp.Moves() {
				fp.Sidecars = append(fp.Sidecars, Sidecar{OldPath: old, NewPath: strings.TrimSuffix(fp.NewPath, filepath.Ext(fp.NewPath)) + strings.ToLower(ext)})
			}
		}

		if fam != FamilyNone {
			want := wantedTags(t, tr, f, perDisc[tr.Disc], discs)
			for _, tag := range TagOrder {
				v, ok := want[tag]
				if !ok || !fam.Writable(tag) {
					continue
				}
				if old := f.Tags[tag]; !sameTag(tag, old, v) {
					fp.Changes = append(fp.Changes, Change{Tag: tag, Old: old, New: v})
				}
			}
			fp.Cover = o.CoverAvailable && fam.CanEmbedCover() && f.Pictures == 0
			if fp.Rewrites() {
				if fam == FamilyID3 && hasLyricsFrame(f.Raw) {
					p.Blocked = fmt.Sprintf("%s has lyrics that rewriting its tags could lose, so the folder is left as it is.", filepath.Base(f.Path))
					return p
				}
				fp.Set = want
			}
		}
		p.Files = append(p.Files, fp)
	}
	sort.Slice(p.Files, func(i, j int) bool { return p.Files[i].NewPath < p.Files[j].NewPath })

	if p.NewFolder != p.OldFolder {
		if inside(p.NewFolder, p.OldFolder) || inside(p.OldFolder, p.NewFolder) {
			p.Blocked = "The new folder would be inside the old one, so the folder is left as it is."
			return p
		}
		if st, err := os.Stat(p.NewFolder); err == nil {
			if old, err := os.Stat(p.OldFolder); err != nil || !os.SameFile(st, old) {
				p.Blocked = fmt.Sprintf("There is already a folder called %s, so this one is left as it is.", p.NewFolder)
				return p
			}
		}
	} else {
		// Files stay in their folder: a new name must not be another file's.
		own := map[string]bool{}
		for _, f := range files {
			own[strings.ToLower(f.Path)] = true
		}
		for _, f := range p.Files {
			if f.Moves() && !own[strings.ToLower(f.NewPath)] {
				if _, err := os.Lstat(f.NewPath); err == nil {
					p.Blocked = fmt.Sprintf("%s is already there, so the folder is left as it is.", filepath.Base(f.NewPath))
					return p
				}
			}
		}
	}
	return p
}

// wantedTags are the tags a file should have.
func wantedTags(t Target, tr TargetTrack, f SourceFile, discTracks, discs int) map[string]string {
	want := map[string]string{
		TagTitle:       tr.Title,
		TagAlbum:       t.Album,
		TagAlbumArtist: t.Artist,
		TagTrack:       strconv.Itoa(tr.Position),
		TagDisc:        strconv.Itoa(max(tr.Disc, 1)),
	}
	switch {
	case tr.Artist != "":
		want[TagArtist] = tr.Artist
	case strings.TrimSpace(f.Tags[TagArtist]) == "":
		want[TagArtist] = t.Artist // no credit known and none in the file
	}
	if discTracks > 0 {
		want[TagTrackTotal] = strconv.Itoa(discTracks)
	}
	if discs > 0 {
		want[TagDiscTotal] = strconv.Itoa(discs)
	}
	if t.Date != "" {
		want[TagDate] = t.Date
	}
	ids := map[string]string{
		TagReleaseGroupID: t.ReleaseGroupID,
		TagReleaseID:      t.ReleaseID,
		TagRecordingID:    tr.RecordingID,
		TagReleaseTrackID: tr.TrackID,
		TagAlbumArtistID:  t.ArtistID,
	}
	if len(tr.ArtistIDs) > 0 {
		ids[TagArtistID] = tr.ArtistIDs[0]
	} else if tr.Artist == "" && want[TagArtist] == t.Artist {
		ids[TagArtistID] = t.ArtistID
	}
	for k, v := range ids {
		if v != "" {
			want[k] = v
		}
	}
	return want
}

// sameTag is SameValue, with a date the file has more precisely (or only
// as a year) taken as the same date.
func sameTag(tag, old, new string) bool {
	if tag == TagDate && old != "" && new != "" && (strings.HasPrefix(old, new) || strings.HasPrefix(new, old)) {
		return true
	}
	return SameValue(tag, old, new)
}

// hasLyricsFrame reports whether an MP3 has lyrics (USLT/SYLT frames), which
// ffmpeg reads but would write back as plain text tags.
func hasLyricsFrame(raw map[string]string) bool {
	for k := range raw {
		if strings.HasPrefix(k, "lyrics") {
			return true
		}
	}
	return false
}

// inside reports whether path is dir or under it.
func inside(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
