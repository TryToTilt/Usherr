package api

// Home fork: finding music you already have that MusicBrainz files as a
// soundtrack, compilation, live album or Christmas record. An artist's
// discography in Mediarium is its studio albums, EPs and singles, so an
// album folder like "Elvis Presley/1961 - Blue Hawaii" (a soundtrack) or
// "Elton John/2017 - Diamonds" (a compilation) never matched. When a
// folder matches nothing, the scan now also looks among the artist's other
// release groups and adds just the one you have.
//
// Also: a joint credit ("Elvis Presley with The Jordanaires", "Prince & The
// New Power Generation") falls back to the main artist.

import (
	"context"
	"regexp"
	"strings"

	"github.com/rdborg/mediarium/internal/music"
	"github.com/rdborg/mediarium/internal/musicbrainz"
)

// scanSecondary caches, per artist in a scan, the release groups with a
// secondary type that aren't in the library yet.
type scanSecondary struct {
	loaded bool
	groups []musicbrainz.ReleaseGroup
	cands  []music.AlbumCandidate
}

// matchSecondaryAlbum looks for title among the artist's soundtracks,
// compilations, live albums and the like. On a match it adds that one
// release group to the artist (unmonitored) and returns its index in
// sa.albums; -1 when nothing fits.
func (s *Server) matchSecondaryAlbum(ctx context.Context, sa *scanArtist, titles []string, year int) int {
	if sa.secondary == nil {
		sa.secondary = &scanSecondary{}
	}
	sec := sa.secondary
	if !sec.loaded {
		sec.loaded = true
		have := map[string]bool{}
		for _, a := range sa.albums {
			have[a.MBID] = true
		}
		groups, err := s.MusicBrainz().ArtistReleaseGroups(ctx, sa.artist.MBID, nil, true)
		if err != nil {
			return -1
		}
		for _, g := range groups {
			if len(g.SecondaryTypes) == 0 || have[g.ID] {
				continue
			}
			sec.groups = append(sec.groups, g)
			sec.cands = append(sec.cands, music.AlbumCandidate{Title: g.Title, Year: releaseYear(g.FirstReleaseDate)})
		}
	}
	if len(sec.cands) == 0 {
		return -1
	}
	j := -1
	for _, t := range titles {
		if t == "" {
			continue
		}
		if j = music.BestAlbum(t, year, sec.cands); j >= 0 {
			break
		}
	}
	if j < 0 {
		return -1
	}
	g := sec.groups[j]
	added, err := s.MusicRepo.AddAlbums(sa.artist.ID, []music.Album{{
		MBID: g.ID, Title: g.Title, Type: albumType(g.PrimaryType), ReleaseDate: g.FirstReleaseDate,
	}})
	if err != nil || len(added) == 0 {
		return -1
	}
	// It is in the library now: drop it from the extras, add it to the
	// artist's albums so a second folder of the same album finds it.
	sec.groups = append(sec.groups[:j], sec.groups[j+1:]...)
	sec.cands = append(sec.cands[:j], sec.cands[j+1:]...)
	sa.albums = append(sa.albums, added[0])
	sa.cands = append(sa.cands, music.AlbumCandidate{Title: added[0].Title, Year: added[0].Year()})
	return len(sa.albums) - 1
}

func releaseYear(date string) int {
	if len(date) < 4 {
		return 0
	}
	y := 0
	for _, c := range date[:4] {
		if c < '0' || c > '9' {
			return 0
		}
		y = y*10 + int(c-'0')
	}
	return y
}

var jointCredit = regexp.MustCompile(`(?i)\s+(?:with|feat\.?|featuring|ft\.?|&|and his|and her|and the|/|_)\s+`)

// mainArtist is the first name of a joint credit ("Elvis Presley with The
// Jordanaires" -> "Elvis Presley"), or "" when the name isn't one.
func mainArtist(name string) string {
	loc := jointCredit.FindStringIndex(name)
	if loc == nil || loc[0] == 0 {
		return ""
	}
	return strings.TrimSpace(name[:loc[0]])
}

// matchOtherArtist tries the artist's other close search results for an
// album folder none of the chosen artist's releases fit. An artist is only
// added to the library when the album really is theirs.
func (s *Server) matchOtherArtist(ctx context.Context, sa *scanArtist, titles []string, year int, userID int64) (*scanArtist, int) {
	for _, alt := range sa.alts {
		groups, err := s.MusicBrainz().ArtistReleaseGroups(ctx, alt.ID, nil, true)
		if err != nil {
			continue
		}
		cands := make([]music.AlbumCandidate, len(groups))
		for i, g := range groups {
			cands[i] = music.AlbumCandidate{Title: g.Title, Year: releaseYear(g.FirstReleaseDate)}
		}
		j := -1
		for _, t := range titles {
			if t != "" {
				if j = music.BestAlbum(t, year, cands); j >= 0 {
					break
				}
			}
		}
		if j < 0 {
			continue
		}
		g := groups[j]
		artist, ok, err := s.MusicRepo.GetArtistByMBID(alt.ID)
		if err == nil && !ok {
			artist, _, err = s.addArtistFromMusicBrainz(ctx, alt.ID, monitorNone, 0, userID)
			if err == nil {
				_ = s.MusicRepo.SetArtistMonitored(artist.ID, true)
				artist.Monitored = true
			}
		}
		if err != nil {
			continue
		}
		albums, err := s.MusicRepo.ListAlbums(artist.ID)
		if err != nil {
			continue
		}
		idx := -1
		for k, a := range albums {
			if a.MBID == g.ID {
				idx = k
			}
		}
		if idx < 0 { // a soundtrack, compilation or live album: add just this one
			added, err := s.MusicRepo.AddAlbums(artist.ID, []music.Album{{MBID: g.ID, Title: g.Title, Type: albumType(g.PrimaryType), ReleaseDate: g.FirstReleaseDate}})
			if err != nil || len(added) == 0 {
				continue
			}
			albums = append(albums, added[0])
			idx = len(albums) - 1
		}
		other := &scanArtist{artist: artist, albums: albums}
		for _, a := range albums {
			other.cands = append(other.cands, music.AlbumCandidate{Title: a.Title, Year: a.Year()})
		}
		return other, idx
	}
	return nil, -1
}
