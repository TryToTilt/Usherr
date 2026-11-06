package api

// Adapted from DroppedNeedle (AGPL-3.0), backend/services/native/library_management_planner.py

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rdborg/mediarium/internal/music"
	"github.com/rdborg/mediarium/internal/musicbrainz"
	"github.com/rdborg/mediarium/internal/musictidy"
)

const (
	// tidySamples is how many files of a folder are fingerprinted to find
	// which album it is.
	tidySamples = 3
	// tidyRescues is how many files of an album that fit no track are
	// fingerprinted, one by one, to place them.
	tidyRescues = 5
)

// Where a match came from.
const (
	tidyFromLibrary     = "library"     // Usherr already knows the album
	tidyFromFingerprint = "fingerprint" // AcoustID
	tidyFromNames       = "names"       // the tags and names alone
)

// tidyDetail is what the page shows besides names and tags. It is stored with
// the album row.
type tidyDetail struct {
	Source         string     `json:"source,omitempty"`
	Quality        string     `json:"quality,omitempty"`
	ReleaseGroupID string     `json:"releaseGroupId,omitempty"`
	Score          float64    `json:"score,omitempty"` // the best fingerprint score, when one was used
	Cover          bool       `json:"cover,omitempty"` // a cover will be embedded
	Copies         []tidyCopy `json:"copies,omitempty"`
}

// tidyCopy is another folder holding the same album.
type tidyCopy struct {
	Folder  string `json:"folder"`
	Quality string `json:"quality"`
	Better  bool   `json:"better"` // this copy is better than the album's
}

// tidyFile is one audio file of a folder during a check.
type tidyFile struct {
	musictidy.SourceFile
	info music.FileInfo
}

// tidyMatch is what a folder was identified as. reason is set when it was
// not.
type tidyMatch struct {
	target  musictidy.Target
	albumID int64
	source  string
	score   float64
	reason  string
}

// tidyLibrary is what the library knows, by folder.
type tidyLibrary struct {
	byPath  map[string]music.Album
	artists map[int64]music.Artist
}

func (s *Server) tidyLibraryIndex() (*tidyLibrary, error) {
	albums, err := s.MusicRepo.ListAllAlbums()
	if err != nil {
		return nil, err
	}
	lib := &tidyLibrary{byPath: map[string]music.Album{}, artists: map[int64]music.Artist{}}
	for _, a := range albums {
		if a.Path != "" && a.MBID != "" {
			lib.byPath[filepath.Clean(a.Path)] = a
		}
	}
	return lib, nil
}

func (s *Server) tidyArtist(lib *tidyLibrary, id int64) (music.Artist, error) {
	if a, ok := lib.artists[id]; ok {
		return a, nil
	}
	a, err := s.MusicRepo.GetArtist(id)
	if err == nil {
		lib.artists[id] = a
	}
	return a, err
}

// runTidyCheck looks at every album folder and stores what tidying would do.
// Nothing on disk or in the library changes.
func (s *Server) runTidyCheck(job *tidyJob, root string) {
	ctx := job.ctx
	store := s.tidyStore()
	planID, err := store.NewPlan(root)
	if err != nil {
		slog.Warn("tidy: start a check", "err", err)
		job.finish(errors.New("couldn't start the check"))
		return
	}
	job.setPlan(planID)
	slog.Info("tidy: check started", "root", root, "plan", planID)
	artists, err := music.ScanLibrary(root)
	if err != nil {
		slog.Warn("tidy: read the music folder", "root", root, "err", err)
		job.finish(errors.New("couldn't read the music folder"))
		return
	}
	lib, err := s.tidyLibraryIndex()
	if err != nil {
		slog.Warn("tidy: read the library", "err", err)
		job.finish(errors.New("couldn't read the library"))
		return
	}
	total := 0
	for _, a := range artists {
		total += len(a.Albums)
	}
	job.setTotal(total)

	leftAlone := func(artist, title, folder, reason string) {
		p := musictidy.AlbumPlan{OldFolder: folder, NewFolder: folder}
		if _, err := store.AddAlbum(planID, 0, artist, title, p, musictidy.StatusBlocked, reason, tidyDetail{}); err != nil {
			slog.Warn("tidy: store an album", "folder", folder, "err", err)
		}
	}
	covers := map[string]bool{}
	copies := map[string][]tidyCopyRow{}
	for _, a := range artists {
		if len(a.Loose) > 0 {
			leftAlone(a.Name, "", a.Path, "Audio files directly in the artist folder can't be told apart. Put them in an album folder.")
		}
		various := music.IsVariousArtists(a.Name)
		for _, al := range a.Albums {
			if ctx.Err() != nil {
				job.finish(nil)
				slog.Info("tidy: check stopped", "plan", planID)
				return
			}
			job.working(al.Path)
			if various {
				leftAlone(a.Name, al.Title, al.Path, "Compilations (Various Artists) are left as they are.")
			} else {
				s.tidyCheckAlbum(ctx, planID, root, lib, a, al, covers, copies)
			}
			job.step()
		}
	}
	s.tidyMarkCopies(root, copies)
	job.finish(nil)
	slog.Info("tidy: check finished", "plan", planID, "albums", total)
}

// tidyCopyRow is an album row with the release group it was identified as.
type tidyCopyRow struct {
	row     int64
	folder  string
	tier    music.Tier
	library bool
	status  string
	reason  string
	detail  tidyDetail
}

// tidyCheckAlbum works out what tidying would do to one folder and stores it.
func (s *Server) tidyCheckAlbum(ctx context.Context, planID int64, root string, lib *tidyLibrary, a music.ScannedArtist, al music.ScannedAlbum, covers map[string]bool, copies map[string][]tidyCopyRow) {
	store := s.tidyStore()
	block := func(reason string) {
		p := musictidy.AlbumPlan{OldFolder: al.Path, NewFolder: al.Path}
		if _, err := store.AddAlbum(planID, 0, a.Name, al.Title, p, musictidy.StatusBlocked, reason, tidyDetail{}); err != nil {
			slog.Warn("tidy: store an album", "folder", al.Path, "err", err)
		}
	}
	files, unreadable := tidyReadFiles(ctx, al.Files)
	if unreadable != "" {
		block(fmt.Sprintf("%s couldn't be read, so the folder is left as it is. It may be damaged.", unreadable))
		return
	}
	m := s.tidyIdentify(ctx, lib, a, al, files)
	if m.reason != "" {
		block(m.reason)
		return
	}
	sources := make([]musictidy.SourceFile, len(files))
	wantsCover := false
	for i, f := range files {
		sources[i] = f.SourceFile
		wantsCover = wantsCover || (f.Pictures == 0 && musictidy.FamilyOf(f.Path).CanEmbedCover())
	}
	rg := m.target.ReleaseGroupID
	if wantsCover && rg != "" {
		has, seen := covers[rg]
		if !seen {
			_, _, err := s.MusicBrainz().FrontCover(ctx, rg)
			has = err == nil
			covers[rg] = has
		}
		wantsCover = has
	} else {
		wantsCover = false
	}
	plan := musictidy.PlanAlbum(root, al.Path, m.target, sources, musictidy.Options{Clean: music.Sanitizer(s.musicSanitizer()), CoverAvailable: wantsCover})
	status := musictidy.StatusEligible
	switch {
	case plan.Blocked != "":
		status = musictidy.StatusBlocked
	case plan.Unchanged():
		status = musictidy.StatusUnchanged
	}
	tier := music.AlbumTier(al.Files)
	detail := tidyDetail{Source: m.source, Quality: string(tier), ReleaseGroupID: rg, Score: m.score, Cover: wantsCover && plan.Blocked == ""}
	row, err := store.AddAlbum(planID, m.albumID, m.target.Artist, m.target.Album, plan, status, plan.Blocked, detail)
	if err != nil {
		slog.Warn("tidy: store an album", "folder", al.Path, "err", err)
		return
	}
	if rg != "" {
		copies[rg] = append(copies[rg], tidyCopyRow{row: row, folder: al.Path, tier: tier, library: m.albumID > 0, status: status, reason: plan.Blocked, detail: detail})
	}
}

// tidyMarkCopies reports folders holding the same album: each lists the
// others, and all but the best are left alone. Nothing is ever deleted: that
// stays the administrator's call.
func (s *Server) tidyMarkCopies(root string, copies map[string][]tidyCopyRow) {
	store := s.tidyStore()
	rel := func(p string) string {
		if r, err := filepath.Rel(root, p); err == nil {
			return r
		}
		return p
	}
	for _, rows := range copies {
		if len(rows) < 2 {
			continue
		}
		best := 0
		for i, r := range rows {
			if rb := rows[best]; music.Rank(r.tier) > music.Rank(rb.tier) || (music.Rank(r.tier) == music.Rank(rb.tier) && r.library && !rb.library) {
				best = i
			}
		}
		for i, r := range rows {
			d := r.detail
			for j, o := range rows {
				if i != j {
					d.Copies = append(d.Copies, tidyCopy{Folder: rel(o.folder), Quality: string(o.tier), Better: j == best})
				}
			}
			status, reason := r.status, r.reason
			if i != best && status != musictidy.StatusBlocked {
				status = musictidy.StatusBlocked
				reason = fmt.Sprintf("Another copy of this album is in %s, and it is the better one (%s). This one is left alone: remove it yourself if you don't need it.", rel(rows[best].folder), rows[best].tier)
			}
			if err := store.UpdateAlbum(r.row, status, reason, d); err != nil {
				slog.Warn("tidy: store a duplicate", "folder", r.folder, "err", err)
			}
		}
	}
}

// tidyReadFiles reads a folder's files: tags and length with ffprobe, what
// the file name and tags say with the import's own reader. The name of a file
// that can't be read is returned.
func tidyReadFiles(ctx context.Context, paths []string) ([]tidyFile, string) {
	out := make([]tidyFile, len(paths))
	for i, p := range paths {
		st, link, err := musictidy.StateOf(p)
		if err != nil {
			return nil, filepath.Base(p)
		}
		probe, err := musictidy.ProbeFile(ctx, p)
		if err != nil {
			return nil, filepath.Base(p)
		}
		info := music.IdentifyFile(p, musicTagReader)
		info.DurationSec = probe.DurationSec
		out[i] = tidyFile{SourceFile: musictidy.SourceFile{Path: p, Size: st.Size, ModNs: st.ModNs, Tags: musictidy.ReadTags(probe.Tags), Raw: probe.Tags,
			Pictures: probe.Pictures, Link: link, Track: -1}, info: info}
	}
	return out, ""
}

// tidyIdentify works out which album a folder is: what the library knows
// first; else by the sound of a few files (AcoustID); else, with no AcoustID
// key or no answer, by the tags and names. It sets each file's Track.
func (s *Server) tidyIdentify(ctx context.Context, lib *tidyLibrary, a music.ScannedArtist, al music.ScannedAlbum, files []tidyFile) tidyMatch {
	if album, ok := lib.byPath[filepath.Clean(al.Path)]; ok {
		return s.tidyFromLibrary(ctx, lib, album, files)
	}
	notFound := "This folder isn't in your library and wasn't recognised. Add it with \"Import my music collection\", or fix its names."
	if s.acoustIDKey() == "" {
		notFound = "This folder isn't in your library and wasn't recognised by its names. An AcoustID key lets Usherr recognise it by its sound."
	}
	if rg, score := s.tidyFingerprintAlbum(ctx, files); rg != "" {
		m := s.tidyFromReleaseGroup(ctx, rg, files, tidyFromFingerprint)
		m.score = score
		return m
	}
	if rg := s.tidyNameAlbum(ctx, a, al, files); rg != "" {
		return s.tidyFromReleaseGroup(ctx, rg, files, tidyFromNames)
	}
	return tidyMatch{reason: notFound}
}

// tidyFromLibrary is a folder the library knows: its release's tracklist,
// with the ids and credits MusicBrainz has for it.
func (s *Server) tidyFromLibrary(ctx context.Context, lib *tidyLibrary, album music.Album, files []tidyFile) tidyMatch {
	artist, err := s.tidyArtist(lib, album.ArtistID)
	if err != nil {
		return tidyMatch{reason: "This album's artist couldn't be read from the library."}
	}
	var rel musicbrainz.Release
	mb := s.MusicBrainz()
	if album.ReleaseMBID != "" {
		rel, err = mb.GetRelease(musicbrainz.Patient(ctx), album.ReleaseMBID)
	} else {
		rel, err = mb.CanonicalTracklist(musicbrainz.Patient(ctx), album.MBID)
	}
	if err != nil {
		slog.Info("tidy: no release from the music database", "album", album.Title, "err", err)
		return tidyMatch{reason: "The music database didn't answer about this album. Check your music again later."}
	}
	refs := rel.Tracklist()
	tracks, err := s.MusicRepo.ListTracks(album.ID)
	if err != nil {
		return tidyMatch{reason: "This album's tracks couldn't be read from the library."}
	}
	target := musictidy.Target{Artist: artist.Name, ArtistID: artist.MBID, Album: album.Title, Year: album.Year(), Date: album.ReleaseDate,
		ReleaseGroupID: album.MBID, ReleaseID: rel.ID}
	linked := map[string]int{}
	if len(tracks) == 0 {
		target.Tracks = tidyTracks(refs)
	}
	for i, t := range tracks {
		tt := musictidy.TargetTrack{LibraryID: t.ID, Disc: t.Disc, Position: t.Position, Title: t.Title, LengthMs: t.LengthMs}
		for _, r := range refs {
			if r.Disc == t.Disc && r.Position == t.Position {
				tt.TrackID, tt.RecordingID, tt.Artist, tt.ArtistIDs = r.TrackID, r.RecordingID, r.Artist, r.ArtistIDs
				break
			}
		}
		target.Tracks = append(target.Tracks, tt)
		if t.FilePath != "" {
			linked[filepath.Clean(t.FilePath)] = i
		}
	}
	score := s.tidyAssign(ctx, target, files, linked)
	return tidyMatch{target: target, albumID: album.ID, source: tidyFromLibrary, score: score}
}

// tidyTracks turns a release's tracklist into a target's.
func tidyTracks(refs []musicbrainz.TrackRef) []musictidy.TargetTrack {
	out := make([]musictidy.TargetTrack, len(refs))
	for i, r := range refs {
		out[i] = musictidy.TargetTrack{Disc: r.Disc, Position: r.Position, Title: r.Title, LengthMs: r.LengthMs,
			TrackID: r.TrackID, RecordingID: r.RecordingID, Artist: r.Artist, ArtistIDs: r.ArtistIDs}
	}
	return out
}

// tidyFromReleaseGroup is a folder identified as a release group outside
// the library: the edition whose track count fits, and its files on it.
func (s *Server) tidyFromReleaseGroup(ctx context.Context, rg string, files []tidyFile, source string) tidyMatch {
	unknown := "The music database didn't answer about this album. Check your music again later."
	mb := s.MusicBrainz()
	pctx := musicbrainz.Patient(ctx)
	details, err := mb.GetReleaseGroup(pctx, rg)
	if err != nil {
		slog.Info("tidy: no release group from the music database", "id", rg, "err", err)
		return tidyMatch{reason: unknown}
	}
	releases, err := mb.ReleaseGroupReleases(pctx, rg)
	if err != nil {
		slog.Info("tidy: no releases from the music database", "id", rg, "err", err)
		return tidyMatch{reason: unknown}
	}
	pick, ok := musicbrainz.EditionFor(releases, len(files))
	if !ok {
		return tidyMatch{reason: "The music database lists no tracks for this album."}
	}
	rel, err := mb.GetRelease(pctx, pick.ID)
	if err != nil {
		slog.Info("tidy: no release from the music database", "id", pick.ID, "err", err)
		return tidyMatch{reason: unknown}
	}
	artist, artistID := musicbrainz.CreditName(rel.ArtistCredit), ""
	if ids := musicbrainz.CreditIDs(rel.ArtistCredit); len(ids) > 0 {
		artistID = ids[0]
	}
	if artist == "" && len(details.ArtistCredit) > 0 {
		artist, artistID = details.ArtistCredit[0].Artist.Name, details.ArtistCredit[0].Artist.ID
	}
	if artist == "" {
		return tidyMatch{reason: "The music database names no artist for this album."}
	}
	if music.IsVariousArtists(artist) {
		return tidyMatch{reason: "Compilations (Various Artists) are left as they are."}
	}
	target := musictidy.Target{Artist: artist, ArtistID: artistID, Album: details.Title, Year: details.Year(), Date: details.FirstReleaseDate,
		ReleaseGroupID: rg, ReleaseID: rel.ID, Tracks: tidyTracks(rel.Tracklist())}
	score := s.tidyAssign(ctx, target, files, nil)
	return tidyMatch{target: target, source: source, score: score}
}

// tidyAssign sets each file's Track: by the import's matching (tags, names,
// lengths); a file left over by the track the library linked it to; then by
// its sound for what is still left. It returns the best fingerprint score
// used.
func (s *Server) tidyAssign(ctx context.Context, target musictidy.Target, files []tidyFile, linked map[string]int) float64 {
	infos := make([]music.FileInfo, len(files))
	for i, f := range files {
		infos[i] = f.info
	}
	slots := make([]music.TrackSlot, len(target.Tracks))
	for i, t := range target.Tracks {
		slots[i] = music.TrackSlot{Disc: t.Disc, Position: t.Position, Title: t.Title, LengthMs: t.LengthMs}
	}
	used := make([]bool, len(slots))
	matches, _ := music.MatchTracks(infos, slots)
	for _, m := range matches {
		files[m.File].Track = m.Slot
		used[m.Slot] = true
	}
	for i := range files {
		if files[i].Track >= 0 {
			continue
		}
		if slot, ok := linked[filepath.Clean(files[i].Path)]; ok && !used[slot] {
			files[i].Track = slot
			used[slot] = true
		}
	}
	return s.tidyRescue(ctx, target, files, used)
}

// tidyRescue places files that fit no track by their sound: a fingerprint
// whose recording is a free track of the album.
func (s *Server) tidyRescue(ctx context.Context, target musictidy.Target, files []tidyFile, used []bool) float64 {
	var best float64
	tried := 0
	for i := range files {
		if files[i].Track >= 0 {
			continue
		}
		if tried >= tidyRescues {
			break
		}
		tried++
		match, ok := s.tidyLookup(ctx, files[i])
		if !ok {
			continue
		}
		for _, rec := range match.Recordings {
			placed := false
			for slot, t := range target.Tracks {
				if !used[slot] && t.RecordingID != "" && t.RecordingID == rec.ID {
					files[i].Track, used[slot], placed = slot, true, true
					best = math.Max(best, match.Score)
					break
				}
			}
			if placed {
				break
			}
		}
	}
	return best
}

// tidyLookup fingerprints a file and asks AcoustID about it. Any failure is
// no answer: tidying carries on with tags and names.
func (s *Server) tidyLookup(ctx context.Context, f tidyFile) (musictidy.FPMatch, bool) {
	if s.acoustIDKey() == "" || ctx.Err() != nil {
		return musictidy.FPMatch{}, false
	}
	fp, err := musictidy.Fingerprint(ctx, f.Path)
	if err != nil {
		slog.Info("tidy: no fingerprint", "file", f.Path, "err", err)
		return musictidy.FPMatch{}, false
	}
	seconds := int(math.Round(f.info.DurationSec))
	match, ok, err := s.tidyAcoustID().Lookup(ctx, fp, seconds)
	if err != nil {
		slog.Info("tidy: no answer from AcoustID", "file", f.Path, "err", err)
		return musictidy.FPMatch{}, false
	}
	return match, ok
}

// tidySample picks up to n files spread over the album.
func tidySample(count, n int) []int {
	if count <= n {
		out := make([]int, count)
		for i := range out {
			out[i] = i
		}
		return out
	}
	out := make([]int, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, i*(count-1)/(n-1))
	}
	return out
}

// tidyFingerprintAlbum finds the album a folder is by the sound of a few of
// its files: the release group most of the answers name. "" when no key is
// set or AcoustID doesn't recognise them.
func (s *Server) tidyFingerprintAlbum(ctx context.Context, files []tidyFile) (string, float64) {
	if s.acoustIDKey() == "" {
		return "", 0
	}
	votes := map[string]int{}
	albumType := map[string]bool{}
	answers := 0
	var best float64
	for _, i := range tidySample(len(files), tidySamples) {
		match, ok := s.tidyLookup(ctx, files[i])
		if !ok {
			continue
		}
		answers++
		best = math.Max(best, match.Score)
		seen := map[string]bool{}
		for _, rec := range match.Recordings {
			for _, g := range rec.ReleaseGroups {
				if !seen[g.ID] {
					seen[g.ID] = true
					votes[g.ID]++
				}
				if strings.EqualFold(g.Type, "album") {
					albumType[g.ID] = true
				}
			}
		}
	}
	if answers == 0 {
		return "", 0
	}
	groups := make([]string, 0, len(votes))
	for g := range votes {
		groups = append(groups, g)
	}
	sort.Slice(groups, func(i, j int) bool {
		a, b := groups[i], groups[j]
		if votes[a] != votes[b] {
			return votes[a] > votes[b]
		}
		if albumType[a] != albumType[b] {
			return albumType[a]
		}
		return a < b
	})
	if len(groups) == 0 || votes[groups[0]]*2 <= answers-1 {
		return "", 0
	}
	return groups[0], best
}

// tidyNameAlbum finds the album a folder is by its tags and names, without
// adding anything to the library. "" when nothing fits.
func (s *Server) tidyNameAlbum(ctx context.Context, a music.ScannedArtist, al music.ScannedAlbum, files []tidyFile) string {
	infos := make([]music.FileInfo, len(files))
	for i, f := range files {
		infos[i] = f.info
	}
	tags := music.ConsensusOf(infos)
	mb := s.MusicBrainz()
	pctx := musicbrainz.Patient(ctx)
	for _, name := range scanNames(tags.Artist, a.Name) {
		if ctx.Err() != nil {
			return ""
		}
		found, err := mb.SearchArtists(pctx, name, 5)
		if err != nil {
			slog.Info("tidy: no artist search", "artist", name, "err", err)
			return ""
		}
		cands := make([]music.NameCandidate, len(found))
		for i, f := range found {
			cands[i] = music.NameCandidate{Name: f.Name, SortName: f.SortName, Score: f.Score}
		}
		idx := music.BestArtist(name, cands)
		if idx < 0 {
			continue
		}
		groups, err := mb.ArtistReleaseGroups(pctx, found[idx].ID, nil, true)
		if err != nil {
			slog.Info("tidy: no albums from the music database", "artist", name, "err", err)
			return ""
		}
		albums := make([]music.AlbumCandidate, len(groups))
		for i, g := range groups {
			albums[i] = music.AlbumCandidate{Title: g.Title, Year: g.Year(), Type: strings.ToLower(g.PrimaryType)}
		}
		year := tags.Year
		if year == 0 {
			year = al.Year
		}
		for _, title := range []string{tags.Album, al.Title} {
			if title == "" {
				continue
			}
			if i := music.BestAlbumForFiles(title, year, len(files), albums); i >= 0 {
				return groups[i].ID
			}
		}
	}
	return ""
}
