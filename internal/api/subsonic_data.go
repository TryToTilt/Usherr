package api

// Usherr: the library as the Subsonic API shows it (subsonic.go). Only what
// has files is shown: artists with at least one album on disk, albums with at
// least one track on disk, tracks with a file. Ids carry a kind, as
// DroppedNeedle does (AGPL-3.0, backend/api/compat/subsonic/ids.py):
// ar-12 an artist, al-34 an album, tr-56 a track, pl-7 a playlist.

import (
	"fmt"
	"mime"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/mediafiles"
	"github.com/rdborg/mediarium/internal/subsonic"
)

// Id prefixes.
const (
	idArtist   = "ar-"
	idAlbum    = "al-"
	idTrack    = "tr-"
	idPlaylist = "pl-"
)

// parseSubsonicID splits "al-34" into "al-" and 34. A bare number is a track
// (some old apps drop the prefix they were given for songs).
func parseSubsonicID(s string) (string, int64, bool) {
	s = strings.TrimSpace(s)
	for _, p := range []string{idArtist, idAlbum, idTrack, idPlaylist} {
		if strings.HasPrefix(s, p) {
			n, err := strconv.ParseInt(s[len(p):], 10, 64)
			return p, n, err == nil && n > 0
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	return idTrack, n, err == nil && n > 0
}

func sid(prefix string, id int64) string { return prefix + strconv.FormatInt(id, 10) }

// ssSong is a track with what a Subsonic song needs.
type ssSong struct {
	ID, AlbumID, ArtistID int64
	Disc, Position        int
	Title                 string
	LengthMs              int
	FilePath              string
	Album                 string
	ReleaseDate           string
	Artist                string
}

const songSelect = `SELECT t.id, t.album_id, al.artist_id, t.disc, t.position, t.title, t.length_ms, t.file_path, al.title, al.release_date, ar.name
	FROM tracks t JOIN albums al ON al.id = t.album_id JOIN artists ar ON ar.id = al.artist_id
	WHERE t.file_path <> ''`

func (s *Server) ssSongs(where string, args ...any) ([]ssSong, error) {
	rows, err := s.db.Query(songSelect+" "+where, args...)
	if err != nil {
		return nil, fmt.Errorf("read songs: %w", err)
	}
	defer rows.Close()
	var out []ssSong
	for rows.Next() {
		var x ssSong
		if err := rows.Scan(&x.ID, &x.AlbumID, &x.ArtistID, &x.Disc, &x.Position, &x.Title, &x.LengthMs, &x.FilePath, &x.Album, &x.ReleaseDate, &x.Artist); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// ssAlbum is an album with files.
type ssAlbum struct {
	ID, ArtistID int64
	Title        string
	ReleaseDate  string
	Type         string
	Artist       string
	Created      string
	Songs        int
	LengthMs     int64
}

const albumSelect = `SELECT al.id, al.artist_id, al.title, al.release_date, al.type, ar.name, ar.added_at, COUNT(t.id), COALESCE(SUM(t.length_ms), 0)
	FROM albums al JOIN artists ar ON ar.id = al.artist_id JOIN tracks t ON t.album_id = al.id AND t.file_path <> ''`

// ssAlbums runs albumSelect with where (before GROUP BY) and tail (ORDER BY, LIMIT).
func (s *Server) ssAlbums(where, tail string, args ...any) ([]ssAlbum, error) {
	rows, err := s.db.Query(albumSelect+" "+where+" GROUP BY al.id "+tail, args...)
	if err != nil {
		return nil, fmt.Errorf("read albums: %w", err)
	}
	defer rows.Close()
	var out []ssAlbum
	for rows.Next() {
		var a ssAlbum
		if err := rows.Scan(&a.ID, &a.ArtistID, &a.Title, &a.ReleaseDate, &a.Type, &a.Artist, &a.Created, &a.Songs, &a.LengthMs); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ssArtist is an artist with albums on disk.
type ssArtist struct {
	ID       int64
	Name     string
	SortName string
	MBID     string
	Albums   int
}

const artistSelect = `SELECT ar.id, ar.name, ar.sort_name, ar.mbid, COUNT(DISTINCT al.id)
	FROM artists ar JOIN albums al ON al.artist_id = ar.id
	WHERE EXISTS (SELECT 1 FROM tracks t WHERE t.album_id = al.id AND t.file_path <> '')`

func (s *Server) ssArtists(where, tail string, args ...any) ([]ssArtist, error) {
	rows, err := s.db.Query(artistSelect+" "+where+" GROUP BY ar.id "+tail, args...)
	if err != nil {
		return nil, fmt.Errorf("read artists: %w", err)
	}
	defer rows.Close()
	var out []ssArtist
	for rows.Next() {
		var a ssArtist
		if err := rows.Scan(&a.ID, &a.Name, &a.SortName, &a.MBID, &a.Albums); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ssContext is what the answers for one account need besides the rows.
type ssContext struct {
	starred  map[string]map[int64]string // kind -> id -> when
	plays    map[int64][2]int64          // track -> count, last played (unix)
	root     string
	transOn  bool
	transFmt string
}

func (s *Server) newSSContext(userID int64) *ssContext {
	c := &ssContext{starred: map[string]map[int64]string{}, plays: map[int64][2]int64{}, root: s.musicRoot()}
	for _, k := range []string{favTrack, favAlbum, favArtist} {
		c.starred[k] = s.favoriteTimes(userID, k)
	}
	if rows, err := s.db.Query(`SELECT track_id, COUNT(*), MAX(played_at) FROM music_play_history WHERE user_id = ? GROUP BY track_id`, userID); err == nil {
		for rows.Next() {
			var id, n, last int64
			if rows.Scan(&id, &n, &last) == nil {
				c.plays[id] = [2]int64{n, last}
			}
		}
		rows.Close()
	}
	st := s.transcodeSettings()
	c.transOn, c.transFmt = st.On, st.Format
	return c
}

func year(date string) int {
	if len(date) >= 4 {
		if y, err := strconv.Atoi(date[:4]); err == nil {
			return y
		}
	}
	return 0
}

// subsonicTime turns a stored time into ISO 8601 as Subsonic wants it.
func subsonicTime(v string) string {
	for _, layout := range []string{"2006-01-02T15:04:05.000Z", time.RFC3339Nano, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, v); err == nil {
			return t.UTC().Format("2006-01-02T15:04:05.000Z")
		}
	}
	return v
}

func (c *ssContext) star(m subsonic.Map, kind string, id int64) {
	if at, ok := c.starred[kind][id]; ok {
		m["starred"] = subsonicTime(at)
	}
}

// song is a track as a Subsonic Child.
func (c *ssContext) song(x ssSong) subsonic.Map {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(x.FilePath), "."))
	ct := mediafiles.AudioContentType("x." + ext)
	if ct == "" {
		ct = mime.TypeByExtension("." + ext)
	}
	dur := (x.LengthMs + 500) / 1000
	m := subsonic.Map{
		"id": sid(idTrack, x.ID), "parent": sid(idAlbum, x.AlbumID), "isDir": false, "title": x.Title,
		"album": x.Album, "artist": x.Artist, "track": x.Position, "discNumber": x.Disc,
		"coverArt": sid(idAlbum, x.AlbumID), "contentType": ct, "suffix": ext, "duration": dur,
		"albumId": sid(idAlbum, x.AlbumID), "artistId": sid(idArtist, x.ArtistID), "type": "music", "mediaType": "song",
		"displayArtist": x.Artist, "displayAlbumArtist": x.Artist,
		"artists":      []subsonic.Map{{"id": sid(idArtist, x.ArtistID), "name": x.Artist}},
		"albumArtists": []subsonic.Map{{"id": sid(idArtist, x.ArtistID), "name": x.Artist}},
	}
	if y := year(x.ReleaseDate); y > 0 {
		m["year"] = y
	}
	if rel, err := filepath.Rel(c.root, x.FilePath); err == nil && !strings.HasPrefix(rel, "..") {
		m["path"] = filepath.ToSlash(rel)
	} else {
		m["path"] = path.Base(filepath.ToSlash(x.FilePath))
	}
	if fi, err := os.Stat(x.FilePath); err == nil {
		m["size"] = fi.Size()
		if dur > 0 {
			m["bitRate"] = int(fi.Size() * 8 / 1000 / int64(dur))
		}
	}
	if c.transOn {
		f := transcodeFormats[c.transFmt]
		m["transcodedContentType"], m["transcodedSuffix"] = f.contentType, f.ext
	}
	if p, ok := c.plays[x.ID]; ok {
		m["playCount"] = p[0]
		m["played"] = time.Unix(p[1], 0).UTC().Format("2006-01-02T15:04:05.000Z")
	} else {
		m["playCount"] = 0
	}
	c.star(m, favTrack, x.ID)
	return m
}

func (c *ssContext) songs(list []ssSong) []subsonic.Map {
	out := make([]subsonic.Map, 0, len(list))
	for _, x := range list {
		out = append(out, c.song(x))
	}
	return out
}

// album is an album as an AlbumID3.
func (c *ssContext) album(a ssAlbum) subsonic.Map {
	m := subsonic.Map{
		"id": sid(idAlbum, a.ID), "name": a.Title, "title": a.Title, "album": a.Title, "artist": a.Artist, "artistId": sid(idArtist, a.ArtistID),
		"coverArt": sid(idAlbum, a.ID), "songCount": a.Songs, "duration": int((a.LengthMs + 500) / 1000), "created": subsonicTime(a.Created),
		"isDir": true, "parent": sid(idArtist, a.ArtistID), "displayArtist": a.Artist,
		"artists":      []subsonic.Map{{"id": sid(idArtist, a.ArtistID), "name": a.Artist}},
		"releaseTypes": []string{},
	}
	if y := year(a.ReleaseDate); y > 0 {
		m["year"] = y
	}
	switch a.Type {
	case "ep":
		m["releaseTypes"] = []string{"EP"}
	case "":
	default:
		m["releaseTypes"] = []string{strings.ToUpper(a.Type[:1]) + a.Type[1:]}
	}
	c.star(m, favAlbum, a.ID)
	return m
}

func (c *ssContext) albums(list []ssAlbum) []subsonic.Map {
	out := make([]subsonic.Map, 0, len(list))
	for _, a := range list {
		out = append(out, c.album(a))
	}
	return out
}

// artist is an artist as an ArtistID3.
func (c *ssContext) artist(a ssArtist) subsonic.Map {
	m := subsonic.Map{"id": sid(idArtist, a.ID), "name": a.Name, "albumCount": a.Albums, "coverArt": sid(idArtist, a.ID)}
	if a.SortName != "" {
		m["sortName"] = a.SortName
	}
	if a.MBID != "" {
		m["musicBrainzId"] = a.MBID
	}
	c.star(m, favArtist, a.ID)
	return m
}

// ignoredArticles are left off the front of a name when sorting it into
// letters ("The Beatles" under B).
var ignoredArticles = []string{"The", "El", "La", "Los", "Las", "Le", "Les"}

// indexLetter is the letter an artist is listed under, "#" for anything else.
func indexLetter(name string) string {
	n := strings.TrimSpace(name)
	for _, a := range ignoredArticles {
		if len(n) > len(a)+1 && strings.EqualFold(n[:len(a)+1], a+" ") {
			n = strings.TrimSpace(n[len(a)+1:])
			break
		}
	}
	for _, r := range strings.ToUpper(n) {
		if r >= 'A' && r <= 'Z' {
			return string(r)
		}
		break
	}
	return "#"
}
