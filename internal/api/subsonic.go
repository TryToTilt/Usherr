package api

// Usherr: the Subsonic / OpenSubsonic API at /rest/, so phone and desktop
// music apps (Symfonium, DSub, Substreamer, play:Sub, Feishin, Tempo…) can
// browse and play Usherr's music like Navidrome's. Answers are XML, or JSON
// with f=json. Sign-in is with an app password (subsonic_auth.go). Member
// permissions apply: playing and downloading need "Play", and a view-only
// account can't change playlists, likes or plays.
//
// The endpoint list and their behaviour follow DroppedNeedle (AGPL-3.0),
// backend/api/compat/subsonic/router.py: among others, an empty or `""`
// search3 query lists everything (Symfonium syncs that way), getIndexes
// honours ifModifiedSince, and one music folder with id 1.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"mime"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/mediafiles"
	"github.com/rdborg/mediarium/internal/music"
	"github.com/rdborg/mediarium/internal/subsonic"
)

const (
	ssListMax     = 500
	ssBodyMax     = 64 << 10
	ssFolderID    = 1
	ssDefaultSize = 10
)

// ssRequest is one signed-in Subsonic call.
type ssRequest struct {
	w     http.ResponseWriter
	r     *http.Request
	f     subsonic.Format
	user  *auth.User
	perms auth.Permissions
	ctx   *ssContext
}

func (q *ssRequest) str(k string) string    { return q.r.Form.Get(k) }
func (q *ssRequest) strs(k string) []string { return q.r.Form[k] }
func (q *ssRequest) num(k string, def int) int {
	if v, err := strconv.Atoi(q.r.Form.Get(k)); err == nil {
		return v
	}
	return def
}
func (q *ssRequest) bool(k string, def bool) bool {
	switch strings.ToLower(q.r.Form.Get(k)) {
	case "true", "1":
		return true
	case "false", "0":
		return false
	}
	return def
}

// size is a list size: def when not given, at most ssListMax.
func (q *ssRequest) size(k string, def int) int {
	n := q.num(k, def)
	if n < 0 {
		n = 0
	}
	return min(n, ssListMax)
}

func (s *Server) ssServer() subsonic.Server {
	return subsonic.Server{Type: "usherr", Version: s.version}
}

func (s *Server) ssOK(q *ssRequest, payload subsonic.Map) { s.ssServer().OK(q.w, q.f, payload) }
func (s *Server) ssFail(q *ssRequest, code int, msg string) {
	s.ssServer().Fail(q.w, q.f, code, msg)
}

// handleSubsonic answers everything under /rest/.
func (s *Server) handleSubsonic(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Access-Control-Allow-Origin", "*") // web players (Feishin in a browser); sign-in is in the request, never a cookie
	h.Set("Access-Control-Allow-Headers", "Content-Type, Range")
	h.Set("Access-Control-Expose-Headers", "Content-Length, Content-Range, Accept-Ranges")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, ssBodyMax)
	if err := r.ParseForm(); err != nil || len(r.Form) > 1024 {
		s.ssServer().Fail(w, subsonic.FormatOf(r.URL.Query()), subsonic.ErrGeneric, "The request is too big or not readable.")
		return
	}
	q := &ssRequest{w: w, r: r, f: subsonic.FormatOf(r.Form)}
	endpoint := subsonic.Endpoint(r.URL.Path)
	if endpoint == "getopensubsonicextensions" {
		s.ssOK(q, subsonic.Map{"openSubsonicExtensions": []subsonic.Map{
			{"name": "apiKeyAuthentication", "versions": []int{1}},
			{"name": "formPost", "versions": []int{1}},
			{"name": "songLyrics", "versions": []int{1}},
			{"name": "transcodeOffset", "versions": []int{1}},
		}})
		return
	}
	if !s.musicEnabled() || !s.subsonicOn() {
		s.ssFail(q, subsonic.ErrNotAuthorized, "Phone apps are switched off on this server.")
		return
	}
	user, code, msg := s.subsonicUser(r)
	if user == nil {
		s.ssFail(q, code, msg)
		return
	}
	q.user = user
	if !user.IsAdmin {
		q.perms, _ = s.Auth.PermissionsOf(user.ID)
	}
	h2, ok := ssHandlers[endpoint]
	if !ok {
		s.ssFail(q, subsonic.ErrNotFound, "Usherr doesn't know "+endpoint+".")
		return
	}
	if h2.write && user.Viewer && !user.IsAdmin {
		s.ssFail(q, subsonic.ErrNotAuthorized, "This account can only look, not change anything.")
		return
	}
	if h2.play && !user.IsAdmin && !q.perms.Play {
		s.ssFail(q, subsonic.ErrNotAuthorized, "This account isn't allowed to play music.")
		return
	}
	if h2.admin && !user.IsAdmin {
		s.ssFail(q, subsonic.ErrNotAuthorized, "Only an administrator can do this.")
		return
	}
	if h2.needsCtx {
		q.ctx = s.newSSContext(user.ID)
	}
	h2.fn(s, q)
}

type ssHandler struct {
	fn       func(*Server, *ssRequest)
	write    bool // changes something: not for view-only accounts
	play     bool // needs the Play permission
	admin    bool
	needsCtx bool // reads likes and plays
}

var ssHandlers map[string]ssHandler

func init() {
	read := func(fn func(*Server, *ssRequest)) ssHandler { return ssHandler{fn: fn, needsCtx: true} }
	ssHandlers = map[string]ssHandler{
		"ping":              {fn: func(s *Server, q *ssRequest) { s.ssOK(q, nil) }},
		"getlicense":        {fn: (*Server).ssLicense},
		"getmusicfolders":   {fn: (*Server).ssMusicFolders},
		"getindexes":        read((*Server).ssIndexes),
		"getartists":        read((*Server).ssArtistsIndex),
		"getartist":         read((*Server).ssArtist),
		"getalbum":          read((*Server).ssAlbum),
		"getsong":           read((*Server).ssSong),
		"getmusicdirectory": read((*Server).ssMusicDirectory),
		"getalbumlist":      read(func(s *Server, q *ssRequest) { s.ssAlbumList(q, "albumList") }),
		"getalbumlist2":     read(func(s *Server, q *ssRequest) { s.ssAlbumList(q, "albumList2") }),
		"getrandomsongs":    read((*Server).ssRandomSongs),
		"search2":           read(func(s *Server, q *ssRequest) { s.ssSearch(q, "searchResult2") }),
		"search3":           read(func(s *Server, q *ssRequest) { s.ssSearch(q, "searchResult3") }),
		"getplaylists":      read((*Server).ssPlaylists),
		"getplaylist":       read((*Server).ssPlaylist),
		"createplaylist":    {fn: (*Server).ssCreatePlaylist, write: true, needsCtx: true},
		"updateplaylist":    {fn: (*Server).ssUpdatePlaylist, write: true},
		"deleteplaylist":    {fn: (*Server).ssDeletePlaylist, write: true},
		"stream":            {fn: func(s *Server, q *ssRequest) { s.ssStream(q, false) }, play: true},
		"download":          {fn: func(s *Server, q *ssRequest) { s.ssStream(q, true) }, play: true},
		"getcoverart":       {fn: (*Server).ssCoverArt},
		"scrobble":          {fn: (*Server).ssScrobble, write: true},
		"star":              {fn: func(s *Server, q *ssRequest) { s.ssStar(q, true) }, write: true},
		"unstar":            {fn: func(s *Server, q *ssRequest) { s.ssStar(q, false) }, write: true},
		"getstarred":        read(func(s *Server, q *ssRequest) { s.ssStarred(q, "starred") }),
		"getstarred2":       read(func(s *Server, q *ssRequest) { s.ssStarred(q, "starred2") }),
		"getlyricsbysongid": {fn: (*Server).ssLyricsBySongID},
		"getlyrics":         {fn: (*Server).ssLyrics},
		"getuser":           {fn: (*Server).ssUser},
		"getgenres": {fn: func(s *Server, q *ssRequest) {
			s.ssOK(q, subsonic.Map{"genres": subsonic.Map{"genre": []subsonic.Map{}}})
		}},
		"getsongsbygenre": {fn: func(s *Server, q *ssRequest) {
			s.ssOK(q, subsonic.Map{"songsByGenre": subsonic.Map{"song": []subsonic.Map{}}})
		}},
		"getnowplaying": {fn: func(s *Server, q *ssRequest) {
			s.ssOK(q, subsonic.Map{"nowPlaying": subsonic.Map{"entry": []subsonic.Map{}}})
		}},
		"getscanstatus":    {fn: (*Server).ssScanStatus},
		"startscan":        {fn: (*Server).ssScanStatus, admin: true},
		"getplayqueue":     read((*Server).ssGetPlayQueue),
		"saveplayqueue":    {fn: (*Server).ssSavePlayQueue, write: true},
		"getartistinfo":    {fn: func(s *Server, q *ssRequest) { s.ssArtistInfo(q, "artistInfo") }},
		"getartistinfo2":   {fn: func(s *Server, q *ssRequest) { s.ssArtistInfo(q, "artistInfo2") }},
		"getalbuminfo":     {fn: func(s *Server, q *ssRequest) { s.ssAlbumInfo(q) }},
		"getalbuminfo2":    {fn: func(s *Server, q *ssRequest) { s.ssAlbumInfo(q) }},
		"gettopsongs":      read((*Server).ssTopSongs),
		"getsimilarsongs":  read(func(s *Server, q *ssRequest) { s.ssSimilarSongs(q, "similarSongs") }),
		"getsimilarsongs2": read(func(s *Server, q *ssRequest) { s.ssSimilarSongs(q, "similarSongs2") }),
		"getbookmarks": {fn: func(s *Server, q *ssRequest) {
			s.ssOK(q, subsonic.Map{"bookmarks": subsonic.Map{"bookmark": []subsonic.Map{}}})
		}},
		"getpodcasts": {fn: func(s *Server, q *ssRequest) {
			s.ssOK(q, subsonic.Map{"podcasts": subsonic.Map{"channel": []subsonic.Map{}}})
		}},
		"getnewestpodcasts": {fn: func(s *Server, q *ssRequest) {
			s.ssOK(q, subsonic.Map{"newestPodcasts": subsonic.Map{"episode": []subsonic.Map{}}})
		}},
		"getinternetradiostations": {fn: func(s *Server, q *ssRequest) {
			s.ssOK(q, subsonic.Map{"internetRadioStations": subsonic.Map{"internetRadioStation": []subsonic.Map{}}})
		}},
		"getshares": {fn: func(s *Server, q *ssRequest) {
			s.ssOK(q, subsonic.Map{"shares": subsonic.Map{"share": []subsonic.Map{}}})
		}},
		"setrating": {fn: func(s *Server, q *ssRequest) { s.ssOK(q, nil) }, write: true}, // ratings aren't kept; a like is a star
	}
}

func (s *Server) ssLicense(q *ssRequest) {
	s.ssOK(q, subsonic.Map{"license": subsonic.Map{"valid": true, "email": "", "licenseExpires": "2099-12-31T00:00:00.000Z"}})
}

func (s *Server) ssMusicFolders(q *ssRequest) {
	s.ssOK(q, subsonic.Map{"musicFolders": subsonic.Map{"musicFolder": []subsonic.Map{{"id": ssFolderID, "name": "Music"}}}})
}

// folderOK refuses a musicFolderId other than the one folder.
func (s *Server) folderOK(q *ssRequest) bool {
	if v := q.str("musicFolderId"); v != "" && v != strconv.Itoa(ssFolderID) {
		s.ssFail(q, subsonic.ErrNotFound, "Music folder not found.")
		return false
	}
	return true
}

// libraryChanged is when the library last changed, in milliseconds, for
// getIndexes' lastModified.
func (s *Server) libraryChanged() int64 {
	var newest string
	_ = s.db.QueryRow(`SELECT COALESCE(MAX(added_at), '') FROM artists`).Scan(&newest)
	var tracks int64
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM tracks WHERE file_path <> ''`).Scan(&tracks)
	t, err := time.Parse("2006-01-02T15:04:05.000Z", newest)
	if err != nil {
		return tracks
	}
	return t.UnixMilli() + tracks // moves when an artist or a track file is added
}

func (s *Server) ssIndexGroups(q *ssRequest) ([]subsonic.Map, error) {
	artists, err := s.ssArtists("", "")
	if err != nil {
		return nil, err
	}
	sort.SliceStable(artists, func(i, j int) bool {
		a, b := artists[i].SortName, artists[j].SortName
		if a == "" {
			a = artists[i].Name
		}
		if b == "" {
			b = artists[j].Name
		}
		return strings.ToLower(a) < strings.ToLower(b)
	})
	groups := map[string][]subsonic.Map{}
	var letters []string
	for _, a := range artists {
		l := indexLetter(a.Name)
		if _, ok := groups[l]; !ok {
			letters = append(letters, l)
		}
		groups[l] = append(groups[l], q.ctx.artist(a))
	}
	sort.Strings(letters)
	out := make([]subsonic.Map, 0, len(letters))
	for _, l := range letters {
		out = append(out, subsonic.Map{"name": l, "artist": groups[l]})
	}
	return out, nil
}

func (s *Server) ssIndexes(q *ssRequest) {
	if !s.folderOK(q) {
		return
	}
	changed := s.libraryChanged()
	if since, err := strconv.ParseInt(q.str("ifModifiedSince"), 10, 64); err == nil && since >= changed {
		s.ssOK(q, subsonic.Map{"indexes": subsonic.Map{"lastModified": changed, "ignoredArticles": strings.Join(ignoredArticles, " ")}})
		return
	}
	idx, err := s.ssIndexGroups(q)
	if err != nil {
		s.ssError(q, err)
		return
	}
	s.ssOK(q, subsonic.Map{"indexes": subsonic.Map{"lastModified": changed, "ignoredArticles": strings.Join(ignoredArticles, " "), "index": idx}})
}

func (s *Server) ssArtistsIndex(q *ssRequest) {
	if !s.folderOK(q) {
		return
	}
	idx, err := s.ssIndexGroups(q)
	if err != nil {
		s.ssError(q, err)
		return
	}
	s.ssOK(q, subsonic.Map{"artists": subsonic.Map{"ignoredArticles": strings.Join(ignoredArticles, " "), "index": idx}})
}

func (s *Server) ssError(q *ssRequest, err error) {
	slog.Warn("subsonic: request failed", "err", err)
	s.ssFail(q, subsonic.ErrGeneric, "Something went wrong on the server. Check Usherr's log for details.")
}

// idOf reads a required id of one kind.
func (s *Server) idOf(q *ssRequest, param, want string) (int64, bool) {
	v := q.str(param)
	if v == "" {
		s.ssFail(q, subsonic.ErrMissingParam, "Required parameter "+param+" is missing.")
		return 0, false
	}
	kind, id, ok := parseSubsonicID(v)
	if !ok || kind != want {
		s.ssFail(q, subsonic.ErrNotFound, "Not found.")
		return 0, false
	}
	return id, true
}

func (s *Server) ssArtist(q *ssRequest) {
	id, ok := s.idOf(q, "id", idArtist)
	if !ok {
		return
	}
	list, err := s.ssArtists("AND ar.id = ?", "", id)
	if err != nil || len(list) == 0 {
		s.ssFail(q, subsonic.ErrNotFound, "Artist not found.")
		return
	}
	albums, err := s.ssAlbums("WHERE al.artist_id = ?", "ORDER BY al.release_date, al.title", id)
	if err != nil {
		s.ssError(q, err)
		return
	}
	m := q.ctx.artist(list[0])
	m["album"] = q.ctx.albums(albums)
	s.ssOK(q, subsonic.Map{"artist": m})
}

func (s *Server) ssAlbum(q *ssRequest) {
	id, ok := s.idOf(q, "id", idAlbum)
	if !ok {
		return
	}
	list, err := s.ssAlbums("WHERE al.id = ?", "", id)
	if err != nil || len(list) == 0 {
		s.ssFail(q, subsonic.ErrNotFound, "Album not found.")
		return
	}
	songs, err := s.ssSongs("AND t.album_id = ? ORDER BY t.disc, t.position", id)
	if err != nil {
		s.ssError(q, err)
		return
	}
	m := q.ctx.album(list[0])
	m["song"] = q.ctx.songs(songs)
	s.ssOK(q, subsonic.Map{"album": m})
}

func (s *Server) ssSong(q *ssRequest) {
	id, ok := s.idOf(q, "id", idTrack)
	if !ok {
		return
	}
	songs, err := s.ssSongs("AND t.id = ?", id)
	if err != nil || len(songs) == 0 {
		s.ssFail(q, subsonic.ErrNotFound, "Song not found.")
		return
	}
	s.ssOK(q, subsonic.Map{"song": q.ctx.song(songs[0])})
}

// ssMusicDirectory is the old folder view: the folder (id 1) lists artists,
// an artist its albums, an album its songs.
func (s *Server) ssMusicDirectory(q *ssRequest) {
	v := q.str("id")
	if v == "" {
		s.ssFail(q, subsonic.ErrMissingParam, "Required parameter id is missing.")
		return
	}
	if v == strconv.Itoa(ssFolderID) {
		artists, err := s.ssArtists("", "ORDER BY ar.sort_name, ar.name")
		if err != nil {
			s.ssError(q, err)
			return
		}
		kids := make([]subsonic.Map, 0, len(artists))
		for _, a := range artists {
			kids = append(kids, subsonic.Map{"id": sid(idArtist, a.ID), "parent": "1", "isDir": true, "title": a.Name, "artist": a.Name, "coverArt": sid(idArtist, a.ID)})
		}
		s.ssOK(q, subsonic.Map{"directory": subsonic.Map{"id": "1", "name": "Music", "child": kids}})
		return
	}
	kind, id, ok := parseSubsonicID(v)
	if !ok {
		s.ssFail(q, subsonic.ErrNotFound, "Directory not found.")
		return
	}
	switch kind {
	case idArtist:
		list, err := s.ssArtists("AND ar.id = ?", "", id)
		if err != nil || len(list) == 0 {
			s.ssFail(q, subsonic.ErrNotFound, "Directory not found.")
			return
		}
		albums, err := s.ssAlbums("WHERE al.artist_id = ?", "ORDER BY al.release_date, al.title", id)
		if err != nil {
			s.ssError(q, err)
			return
		}
		s.ssOK(q, subsonic.Map{"directory": subsonic.Map{"id": v, "parent": "1", "name": list[0].Name, "child": q.ctx.albums(albums)}})
	case idAlbum:
		list, err := s.ssAlbums("WHERE al.id = ?", "", id)
		if err != nil || len(list) == 0 {
			s.ssFail(q, subsonic.ErrNotFound, "Directory not found.")
			return
		}
		songs, err := s.ssSongs("AND t.album_id = ? ORDER BY t.disc, t.position", id)
		if err != nil {
			s.ssError(q, err)
			return
		}
		s.ssOK(q, subsonic.Map{"directory": subsonic.Map{"id": v, "parent": sid(idArtist, list[0].ArtistID), "name": list[0].Title, "child": q.ctx.songs(songs)}})
	default:
		s.ssFail(q, subsonic.ErrNotFound, "Directory not found.")
	}
}

func (s *Server) ssAlbumList(q *ssRequest, element string) {
	if !s.folderOK(q) {
		return
	}
	size, offset := q.size("size", ssDefaultSize), max(q.num("offset", 0), 0)
	page := fmt.Sprintf(" LIMIT %d OFFSET %d", size, offset)
	var albums []ssAlbum
	var err error
	switch q.str("type") {
	case "random":
		albums, err = s.ssAlbums("", "ORDER BY RANDOM() LIMIT ?", size)
	case "newest":
		albums, err = s.ssAlbums("", "ORDER BY MAX(t.id) DESC"+page)
	case "alphabeticalByName":
		albums, err = s.ssAlbums("", "ORDER BY al.title COLLATE NOCASE"+page)
	case "alphabeticalByArtist":
		albums, err = s.ssAlbums("", "ORDER BY ar.sort_name COLLATE NOCASE, ar.name COLLATE NOCASE, al.release_date"+page)
	case "starred":
		albums, err = s.ssAlbums("WHERE al.id IN (SELECT item_id FROM music_favorites WHERE user_id = ? AND kind = 'album')", "ORDER BY al.title COLLATE NOCASE"+page, q.user.ID)
	case "recent":
		albums, err = s.ssAlbums("", `ORDER BY (SELECT MAX(h.played_at) FROM music_play_history h JOIN tracks t2 ON t2.id = h.track_id
			WHERE h.user_id = ? AND t2.album_id = al.id) DESC NULLS LAST`+page, q.user.ID)
		albums = s.keepPlayed(albums, q.user.ID)
	case "frequent", "highest":
		albums, err = s.ssAlbums("", `ORDER BY (SELECT COUNT(*) FROM music_play_history h JOIN tracks t2 ON t2.id = h.track_id
			WHERE h.user_id = ? AND t2.album_id = al.id) DESC`+page, q.user.ID)
		albums = s.keepPlayed(albums, q.user.ID)
	case "byYear":
		from, to := q.num("fromYear", 0), q.num("toYear", 9999)
		order := "ASC"
		if from > to {
			from, to, order = to, from, "DESC"
		}
		albums, err = s.ssAlbums("WHERE CAST(SUBSTR(al.release_date, 1, 4) AS INTEGER) BETWEEN ? AND ? AND al.release_date <> ''",
			"ORDER BY al.release_date "+order+page, from, to)
	case "byGenre":
		albums = nil // Usherr keeps no genres on albums yet
	case "":
		s.ssFail(q, subsonic.ErrMissingParam, "Required parameter type is missing.")
		return
	default:
		s.ssFail(q, subsonic.ErrGeneric, "Unknown list type.")
		return
	}
	if err != nil {
		s.ssError(q, err)
		return
	}
	s.ssOK(q, subsonic.Map{element: subsonic.Map{"album": q.ctx.albums(albums)}})
}

// keepPlayed leaves out albums the account has never played.
func (s *Server) keepPlayed(albums []ssAlbum, userID int64) []ssAlbum {
	var out []ssAlbum
	for _, a := range albums {
		var n int
		_ = s.db.QueryRow(`SELECT COUNT(*) FROM music_play_history h JOIN tracks t ON t.id = h.track_id WHERE h.user_id = ? AND t.album_id = ?`, userID, a.ID).Scan(&n)
		if n > 0 {
			out = append(out, a)
		}
	}
	return out
}

func (s *Server) ssRandomSongs(q *ssRequest) {
	size := q.size("size", ssDefaultSize)
	where, args := "", []any{}
	if from := q.num("fromYear", 0); from > 0 {
		where += " AND CAST(SUBSTR(al.release_date, 1, 4) AS INTEGER) >= ?"
		args = append(args, from)
	}
	if to := q.num("toYear", 0); to > 0 {
		where += " AND CAST(SUBSTR(al.release_date, 1, 4) AS INTEGER) <= ?"
		args = append(args, to)
	}
	args = append(args, size)
	songs, err := s.ssSongs(where+" ORDER BY RANDOM() LIMIT ?", args...)
	if err != nil {
		s.ssError(q, err)
		return
	}
	s.ssOK(q, subsonic.Map{"randomSongs": subsonic.Map{"song": q.ctx.songs(songs)}})
}

// searchTerm cleans a search: quotes and wildcards dropped; "" (Symfonium's
// sync) and an empty query match everything.
func searchTerm(v string) string {
	v = strings.TrimSpace(v)
	v = strings.Trim(v, `"*`)
	return strings.TrimSpace(v)
}

func likeArg(term string) string {
	return "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(strings.ToLower(term)) + "%"
}

func (s *Server) ssSearch(q *ssRequest, element string) {
	if !s.folderOK(q) {
		return
	}
	term := searchTerm(q.str("query"))
	arg := likeArg(term)
	ac, ao := q.size("artistCount", 20), max(q.num("artistOffset", 0), 0)
	bc, bo := q.size("albumCount", 20), max(q.num("albumOffset", 0), 0)
	sc, so := q.size("songCount", 20), max(q.num("songOffset", 0), 0)
	artists, err := s.ssArtists(`AND LOWER(ar.name) LIKE ? ESCAPE '\'`, "ORDER BY ar.sort_name COLLATE NOCASE, ar.name LIMIT ? OFFSET ?", arg, ac, ao)
	if err != nil {
		s.ssError(q, err)
		return
	}
	albums, err := s.ssAlbums(`WHERE (LOWER(al.title) LIKE ? ESCAPE '\' OR LOWER(ar.name) LIKE ? ESCAPE '\')`, "ORDER BY ar.name COLLATE NOCASE, al.release_date LIMIT ? OFFSET ?", arg, arg, bc, bo)
	if err != nil {
		s.ssError(q, err)
		return
	}
	songs, err := s.ssSongs(`AND (LOWER(t.title) LIKE ? ESCAPE '\' OR LOWER(ar.name) LIKE ? ESCAPE '\' OR LOWER(al.title) LIKE ? ESCAPE '\')
		ORDER BY ar.name COLLATE NOCASE, al.release_date, t.disc, t.position LIMIT ? OFFSET ?`, arg, arg, arg, sc, so)
	if err != nil {
		s.ssError(q, err)
		return
	}
	res := subsonic.Map{"album": q.ctx.albums(albums), "song": q.ctx.songs(songs)}
	as := make([]subsonic.Map, 0, len(artists))
	for _, a := range artists {
		as = append(as, q.ctx.artist(a))
	}
	res["artist"] = as
	s.ssOK(q, subsonic.Map{element: res})
}

// ---- Playlists ----

func (s *Server) ssPlaylistMap(p music.Playlist, owners map[int64]string) subsonic.Map {
	owner, ok := owners[p.UserID]
	if !ok {
		if u, err := s.Auth.GetUser(p.UserID); err == nil && u != nil {
			owner = u.Username
		}
		owners[p.UserID] = owner
	}
	m := subsonic.Map{"id": sid(idPlaylist, p.ID), "name": p.Name, "owner": owner, "public": p.Shared, "songCount": p.TrackCount,
		"duration": (p.DurationMs + 500) / 1000, "created": subsonicTime(p.CreatedAt), "changed": subsonicTime(p.UpdatedAt), "coverArt": sid(idPlaylist, p.ID), "comment": ""}
	return m
}

func (s *Server) ssPlaylists(q *ssRequest) {
	if u := q.str("username"); u != "" && !strings.EqualFold(u, q.user.Username) && !q.user.IsAdmin {
		s.ssFail(q, subsonic.ErrNotAuthorized, "You can only list your own playlists.")
		return
	}
	lists, err := s.MusicRepo.ListPlaylists(q.user.ID)
	if err != nil {
		s.ssError(q, err)
		return
	}
	owners := map[int64]string{}
	out := make([]subsonic.Map, 0, len(lists))
	for _, p := range lists {
		out = append(out, s.ssPlaylistMap(p, owners))
	}
	s.ssOK(q, subsonic.Map{"playlists": subsonic.Map{"playlist": out}})
}

func (s *Server) ssPlaylistDetail(q *ssRequest, id int64) {
	p, err := s.MusicRepo.GetPlaylist(id, q.user.ID)
	if err != nil {
		s.ssFail(q, subsonic.ErrNotFound, "Playlist not found.")
		return
	}
	ids, err := s.MusicRepo.PlaylistTrackIDs(id)
	if err != nil {
		s.ssError(q, err)
		return
	}
	songs, err := s.ssSongsByID(ids)
	if err != nil {
		s.ssError(q, err)
		return
	}
	if q.ctx == nil {
		q.ctx = s.newSSContext(q.user.ID)
	}
	m := s.ssPlaylistMap(p, map[int64]string{})
	m["entry"] = q.ctx.songs(songs)
	m["songCount"] = len(songs)
	s.ssOK(q, subsonic.Map{"playlist": m})
}

// ssSongsByID returns the songs for ids, in that order, leaving out tracks without a file.
func (s *Server) ssSongsByID(ids []int64) ([]ssSong, error) {
	out := make([]ssSong, 0, len(ids))
	cache := map[int64]ssSong{}
	for i := 0; i < len(ids); i += 400 {
		chunk := ids[i:min(i+400, len(ids))]
		ph := strings.TrimSuffix(strings.Repeat("?,", len(chunk)), ",")
		args := make([]any, len(chunk))
		for j, id := range chunk {
			args[j] = id
		}
		songs, err := s.ssSongs("AND t.id IN ("+ph+")", args...)
		if err != nil {
			return nil, err
		}
		for _, x := range songs {
			cache[x.ID] = x
		}
	}
	for _, id := range ids {
		if x, ok := cache[id]; ok {
			out = append(out, x)
		}
	}
	return out, nil
}

func (s *Server) ssPlaylist(q *ssRequest) {
	id, ok := s.idOf(q, "id", idPlaylist)
	if !ok {
		return
	}
	s.ssPlaylistDetail(q, id)
}

// songIDs reads every songId given; ok is false (answered) for one that isn't a song.
func (s *Server) songIDs(q *ssRequest, param string) ([]int64, bool) {
	var out []int64
	for _, v := range q.strs(param) {
		kind, id, ok := parseSubsonicID(v)
		if !ok || kind != idTrack {
			s.ssFail(q, subsonic.ErrNotFound, "Song not found: "+v)
			return nil, false
		}
		out = append(out, id)
	}
	return out, true
}

// createPlaylist: a new list (name, songId…), or, with playlistId, the
// list's songs replaced.
func (s *Server) ssCreatePlaylist(q *ssRequest) {
	songs, ok := s.songIDs(q, "songId")
	if !ok {
		return
	}
	if v := q.str("playlistId"); v != "" {
		kind, id, ok := parseSubsonicID(v)
		if !ok || kind != idPlaylist {
			s.ssFail(q, subsonic.ErrNotFound, "Playlist not found.")
			return
		}
		if p, err := s.MusicRepo.GetPlaylist(id, q.user.ID); err != nil || p.UserID != q.user.ID {
			s.ssFail(q, subsonic.ErrNotAuthorized, "Only the owner can change this playlist.")
			return
		}
		if name := q.str("name"); name != "" {
			if err := s.MusicRepo.UpdatePlaylist(id, q.user.ID, &name, nil); err != nil {
				s.ssError(q, err)
				return
			}
		}
		if err := s.MusicRepo.ReplacePlaylistTracks(id, songs); err != nil {
			s.ssError(q, err)
			return
		}
		s.ssPlaylistDetail(q, id)
		return
	}
	name := q.str("name")
	if _, ok := music.CleanPlaylistName(name); !ok {
		s.ssFail(q, subsonic.ErrMissingParam, "Required parameter name is missing.")
		return
	}
	p, err := s.MusicRepo.CreatePlaylist(q.user.ID, name, false)
	if err != nil {
		s.ssError(q, err)
		return
	}
	if err := s.MusicRepo.ReplacePlaylistTracks(p.ID, songs); err != nil {
		s.ssError(q, err)
		return
	}
	s.ssPlaylistDetail(q, p.ID)
}

func (s *Server) ssUpdatePlaylist(q *ssRequest) {
	id, ok := s.idOf(q, "playlistId", idPlaylist)
	if !ok {
		return
	}
	if p, err := s.MusicRepo.GetPlaylist(id, q.user.ID); err != nil {
		s.ssFail(q, subsonic.ErrNotFound, "Playlist not found.")
		return
	} else if p.UserID != q.user.ID {
		s.ssFail(q, subsonic.ErrNotAuthorized, "Only the owner can change this playlist.")
		return
	}
	var name *string
	if v := q.str("name"); v != "" {
		name = &v
	}
	var shared *bool
	if v := q.str("public"); v != "" {
		b := q.bool("public", false)
		shared = &b
	}
	if name != nil || shared != nil {
		if err := s.MusicRepo.UpdatePlaylist(id, q.user.ID, name, shared); err != nil {
			s.ssError(q, err)
			return
		}
	}
	if remove := q.strs("songIndexToRemove"); len(remove) > 0 {
		entries, err := s.MusicRepo.PlaylistEntries(id)
		if err != nil {
			s.ssError(q, err)
			return
		}
		seen := map[int]bool{}
		for _, v := range remove {
			i, err := strconv.Atoi(v)
			if err != nil || i < 0 || i >= len(entries) || seen[i] {
				continue
			}
			seen[i] = true
			if err := s.MusicRepo.RemoveFromPlaylist(id, q.user.ID, entries[i].EntryID); err != nil {
				s.ssError(q, err)
				return
			}
		}
	}
	add, ok := s.songIDs(q, "songIdToAdd")
	if !ok {
		return
	}
	if len(add) > 0 {
		if _, err := s.MusicRepo.AddToPlaylist(id, q.user.ID, add); err != nil {
			if errors.Is(err, music.ErrPlaylistFull) {
				s.ssFail(q, subsonic.ErrGeneric, "A playlist can hold up to 5000 tracks.")
				return
			}
			s.ssError(q, err)
			return
		}
	}
	s.ssOK(q, nil)
}

func (s *Server) ssDeletePlaylist(q *ssRequest) {
	id, ok := s.idOf(q, "id", idPlaylist)
	if !ok {
		return
	}
	if err := s.MusicRepo.DeletePlaylist(id, q.user.ID); err != nil {
		if errors.Is(err, music.ErrPlaylistNotFound) {
			s.ssFail(q, subsonic.ErrNotAuthorized, "That playlist isn't there, or it isn't yours.")
			return
		}
		s.ssError(q, err)
		return
	}
	s.ssOK(q, nil)
}

// ---- Files ----

// trackFolderOf finds a track's file inside the music folder.
func (s *Server) trackFolderOf(t music.Track) (mediafiles.Folder, string, error) {
	if t.FilePath == "" {
		return mediafiles.Folder{}, "", errNoSuchItem
	}
	folder, err := mediafiles.ForFile(t.FilePath, []string{s.musicRoot()}, false)
	if err != nil {
		return mediafiles.Folder{}, "", err
	}
	rel, ok := folder.Rel(t.FilePath)
	if !ok {
		return mediafiles.Folder{}, "", errNoSuchItem
	}
	return folder, rel, nil
}

// ssStream sends a song: as it is for download, else converted when asked
// (maxBitRate, format, timeOffset; music_transcode.go). Binary answers are
// plain HTTP errors except for sign-in, as Subsonic apps expect.
func (s *Server) ssStream(q *ssRequest, download bool) {
	kind, id, ok := parseSubsonicID(q.str("id"))
	if !ok || kind != idTrack {
		s.ssFail(q, subsonic.ErrNotFound, "Song not found.")
		return
	}
	t, err := s.MusicRepo.GetTrack(id)
	if err != nil {
		s.ssFail(q, subsonic.ErrNotFound, "Song not found.")
		return
	}
	folder, rel, err := s.trackFolderOf(t)
	if err != nil {
		s.ssFail(q, subsonic.ErrNotFound, "This song's file isn't there.")
		return
	}
	if download {
		q.w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filepath.Base(rel)}))
		s.serveOriginal(q.w, q.r, folder, rel)
		return
	}
	sq := streamQuery{Format: q.str("format"), MaxKbps: max(q.num("maxBitRate", 0), 0), Estimate: q.bool("estimateContentLength", false)}
	if v, err := strconv.ParseFloat(q.str("timeOffset"), 64); err == nil && v > 0 {
		sq.OffsetSec = v
		if sq.Format == "" && sq.MaxKbps == 0 {
			sq.Format = s.transcodeSettings().Format // a start part-way needs a conversion
		}
	}
	s.serveTrackStream(q.w, q.r, q.user.ID, folder, rel, sq, float64(t.LengthMs)/1000)
}

// ssCoverArt sends a cover: an album's, an artist's, a song's album's or a
// playlist's first album's. size up to 300 gets the small thumbnail.
func (s *Server) ssCoverArt(q *ssRequest) {
	kind, id, ok := parseSubsonicID(q.str("id"))
	if !ok {
		http.NotFound(q.w, q.r)
		return
	}
	var albumID int64
	switch kind {
	case idAlbum:
		albumID = id
	case idTrack:
		t, err := s.MusicRepo.GetTrack(id)
		if err != nil {
			http.NotFound(q.w, q.r)
			return
		}
		albumID = t.AlbumID
	case idPlaylist:
		p, err := s.MusicRepo.GetPlaylist(id, q.user.ID)
		if err != nil {
			http.NotFound(q.w, q.r)
			return
		}
		ids, _ := s.MusicRepo.PlaylistTrackIDs(p.ID)
		if len(ids) == 0 {
			http.NotFound(q.w, q.r)
			return
		}
		t, err := s.MusicRepo.GetTrack(ids[0])
		if err != nil {
			http.NotFound(q.w, q.r)
			return
		}
		albumID = t.AlbumID
	case idArtist:
		res, err := s.artistCoverFor(q.r.Context(), id)
		if err != nil || res.err != nil {
			http.NotFound(q.w, q.r)
			return
		}
		s.ssServeCover(q, res.path)
		return
	}
	album, err := s.MusicRepo.GetAlbum(albumID)
	if err != nil {
		http.NotFound(q.w, q.r)
		return
	}
	p, err := s.coverFor(q.r.Context(), album)
	if err != nil {
		http.NotFound(q.w, q.r)
		return
	}
	s.ssServeCover(q, p)
}

func (s *Server) ssServeCover(q *ssRequest, src string) {
	if n := q.num("size", 0); n > 0 && n <= 300 {
		if thumb, err := s.thumbFor(q.r.Context(), src); err == nil {
			serveCoverFile(q.w, q.r, thumb, thumbMaxAge)
			return
		}
	}
	serveCoverFile(q.w, q.r, src, coverMaxAge)
}

// ---- Plays and likes ----

// ssScrobble: submission=false is "now playing"; otherwise each id is a
// play that counted, at time (ms) or now.
func (s *Server) ssScrobble(q *ssRequest) {
	ids, ok := s.songIDs(q, "id")
	if !ok {
		return
	}
	if len(ids) == 0 {
		s.ssFail(q, subsonic.ErrMissingParam, "Required parameter id is missing.")
		return
	}
	times := q.strs("time")
	if len(times) > 0 && len(times) != len(ids) {
		s.ssFail(q, subsonic.ErrGeneric, "There must be one time for each id.")
		return
	}
	if !q.bool("submission", true) {
		userID, track := q.user.ID, ids[0]
		go func() { // the app needn't wait for ListenBrainz and Last.fm
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			s.nowPlaying(ctx, userID, track)
		}()
		s.ssOK(q, nil)
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(q.r.Context()), 30*time.Second)
	defer cancel()
	now := time.Now()
	for i, id := range ids {
		t, err := s.MusicRepo.GetTrack(id)
		if err != nil {
			continue
		}
		at := now.Add(-time.Duration(t.LengthMs) * time.Millisecond).Unix() // the start, when the app sends at the end
		if len(times) > 0 {
			if ms, err := strconv.ParseInt(times[i], 10, 64); err == nil && ms > 0 {
				at = ms / 1000
			}
		}
		if at < listenMinTime || time.Unix(at, 0).After(now.Add(listenMaxAhead)) || time.Unix(at, 0).Before(now.Add(-listenMaxAge)) {
			continue
		}
		s.recordPlay(ctx, q.user.ID, id, at, -1, "subsonic")
	}
	s.ssOK(q, nil)
}

func (s *Server) ssStar(q *ssRequest, on bool) {
	type item struct {
		kind string
		id   int64
	}
	var items []item
	add := func(param, want string) bool {
		for _, v := range q.strs(param) {
			kind, id, ok := parseSubsonicID(v)
			if !ok || (want != "" && kind != want) {
				s.ssFail(q, subsonic.ErrNotFound, "Not found: "+v)
				return false
			}
			switch kind {
			case idTrack:
				items = append(items, item{favTrack, id})
			case idAlbum:
				items = append(items, item{favAlbum, id})
			case idArtist:
				items = append(items, item{favArtist, id})
			default:
				s.ssFail(q, subsonic.ErrNotFound, "Not found: "+v)
				return false
			}
		}
		return true
	}
	if !add("id", "") || !add("albumId", idAlbum) || !add("artistId", idArtist) {
		return
	}
	for _, it := range items {
		found, err := s.setFavorite(q.user.ID, it.kind, it.id, on)
		if !found {
			s.ssFail(q, subsonic.ErrNotFound, "Not found.")
			return
		}
		if err != nil {
			s.ssError(q, err)
			return
		}
	}
	s.ssOK(q, nil)
}

func (s *Server) ssStarred(q *ssRequest, element string) {
	artists, err := s.ssArtists("AND ar.id IN (SELECT item_id FROM music_favorites WHERE user_id = ? AND kind = 'artist')", "ORDER BY ar.name COLLATE NOCASE", q.user.ID)
	if err != nil {
		s.ssError(q, err)
		return
	}
	albums, err := s.ssAlbums("WHERE al.id IN (SELECT item_id FROM music_favorites WHERE user_id = ? AND kind = 'album')", "ORDER BY al.title COLLATE NOCASE", q.user.ID)
	if err != nil {
		s.ssError(q, err)
		return
	}
	songs, err := s.ssSongs(`AND t.id IN (SELECT item_id FROM music_favorites WHERE user_id = ? AND kind = 'track') ORDER BY ar.name COLLATE NOCASE, al.title, t.disc, t.position`, q.user.ID)
	if err != nil {
		s.ssError(q, err)
		return
	}
	as := make([]subsonic.Map, 0, len(artists))
	for _, a := range artists {
		as = append(as, q.ctx.artist(a))
	}
	s.ssOK(q, subsonic.Map{element: subsonic.Map{"artist": as, "album": q.ctx.albums(albums), "song": q.ctx.songs(songs)}})
}

// ---- Lyrics ----

func (s *Server) ssLyricsBySongID(q *ssRequest) {
	kind, id, ok := parseSubsonicID(q.str("id"))
	if !ok || kind != idTrack {
		s.ssFail(q, subsonic.ErrNotFound, "Song not found.")
		return
	}
	songs, err := s.ssSongs("AND t.id = ?", id)
	if err != nil || len(songs) == 0 {
		s.ssFail(q, subsonic.ErrNotFound, "Song not found.")
		return
	}
	x := songs[0]
	list := []subsonic.Map{}
	t, _ := s.MusicRepo.GetTrack(id)
	if folder, rel, err := s.trackFolderOf(t); err == nil {
		ctx, cancel := context.WithTimeout(q.r.Context(), 20*time.Second)
		l := s.trackLyrics(ctx, t, folder, rel)
		cancel()
		base := subsonic.Map{"lang": "und", "displayArtist": x.Artist, "displayTitle": x.Title, "offset": 0}
		if l.Synced != "" {
			lines := []subsonic.Map{}
			for _, ln := range music.ParseLRC(l.Synced) {
				lines = append(lines, subsonic.Map{"start": ln.StartMs, "value": ln.Text})
			}
			m := subsonic.Map{"synced": true, "line": lines}
			for k, v := range base {
				m[k] = v
			}
			list = append(list, m)
		}
		if l.Plain != "" {
			lines := []subsonic.Map{}
			for _, ln := range strings.Split(strings.ReplaceAll(l.Plain, "\r\n", "\n"), "\n") {
				lines = append(lines, subsonic.Map{"value": ln})
			}
			m := subsonic.Map{"synced": false, "line": lines}
			for k, v := range base {
				m[k] = v
			}
			list = append(list, m)
		}
	}
	s.ssOK(q, subsonic.Map{"lyricsList": subsonic.Map{"structuredLyrics": list}})
}

// getLyrics (the old call): plain words for an artist and title.
func (s *Server) ssLyrics(q *ssRequest) {
	artist, title := q.str("artist"), q.str("title")
	songs, err := s.ssSongs(`AND LOWER(t.title) = LOWER(?) AND (? = '' OR LOWER(ar.name) = LOWER(?)) LIMIT 1`, title, artist, artist)
	if err != nil || len(songs) == 0 {
		s.ssOK(q, subsonic.Map{"lyrics": subsonic.Map{}})
		return
	}
	x := songs[0]
	t, _ := s.MusicRepo.GetTrack(x.ID)
	m := subsonic.Map{"artist": x.Artist, "title": x.Title}
	if folder, rel, err := s.trackFolderOf(t); err == nil {
		l := s.trackLyrics(q.r.Context(), t, folder, rel)
		words := l.Plain
		if words == "" && l.Synced != "" {
			var b []string
			for _, ln := range music.ParseLRC(l.Synced) {
				b = append(b, ln.Text)
			}
			words = strings.Join(b, "\n")
		}
		m["value"] = words
	}
	s.ssOK(q, subsonic.Map{"lyrics": m})
}

// ---- Account, queue, info ----

func (s *Server) ssUser(q *ssRequest) {
	name := q.str("username")
	u := q.user
	if name != "" && !strings.EqualFold(name, u.Username) {
		if !u.IsAdmin {
			s.ssFail(q, subsonic.ErrNotAuthorized, "You can only see your own account.")
			return
		}
		other, err := s.Auth.UserByName(name)
		if err != nil {
			s.ssFail(q, subsonic.ErrNotFound, "User not found.")
			return
		}
		u = other
	}
	perms := auth.Permissions{Play: true}
	if !u.IsAdmin {
		perms, _ = s.Auth.PermissionsOf(u.ID)
	}
	write := !u.Viewer || u.IsAdmin
	scrobbling := false
	if r, ok, _ := s.scrobbleRow(u.ID); ok && r.Enabled {
		scrobbling = true
	}
	if r, ok, _ := s.lastfmRow(u.ID); ok && r.Enabled {
		scrobbling = true
	}
	s.ssOK(q, subsonic.Map{"user": subsonic.Map{
		"username": u.Username, "email": u.Email, "scrobblingEnabled": scrobbling, "adminRole": u.IsAdmin, "settingsRole": false,
		"downloadRole": perms.Play || u.IsAdmin, "uploadRole": false, "playlistRole": write, "coverArtRole": true, "commentRole": false,
		"podcastRole": false, "streamRole": perms.Play || u.IsAdmin, "jukeboxRole": false, "shareRole": false, "videoConversionRole": false,
		"maxBitRate": s.transcodeSettings().MaxKbps, "folder": []int{ssFolderID},
	}})
}

func (s *Server) ssScanStatus(q *ssRequest) {
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM tracks WHERE file_path <> ''`).Scan(&n)
	s.ssOK(q, subsonic.Map{"scanStatus": subsonic.Map{"scanning": false, "count": n}})
}

func (s *Server) ssGetPlayQueue(q *ssRequest) {
	var current, pos int64
	var idsJSON, changed, by string
	err := s.db.QueryRow(`SELECT current, position_ms, track_ids, changed_at, changed_by FROM subsonic_play_queues WHERE user_id = ?`, q.user.ID).
		Scan(&current, &pos, &idsJSON, &changed, &by)
	if errors.Is(err, sql.ErrNoRows) {
		s.ssOK(q, nil) // no queue saved: an empty answer, as the spec says
		return
	}
	if err != nil {
		s.ssError(q, err)
		return
	}
	var ids []int64
	_ = json.Unmarshal([]byte(idsJSON), &ids)
	songs, err := s.ssSongsByID(ids)
	if err != nil {
		s.ssError(q, err)
		return
	}
	m := subsonic.Map{"position": pos, "username": q.user.Username, "changed": subsonicTime(changed), "changedBy": by, "entry": q.ctx.songs(songs)}
	if current > 0 {
		m["current"] = sid(idTrack, current)
	}
	s.ssOK(q, subsonic.Map{"playQueue": m})
}

func (s *Server) ssSavePlayQueue(q *ssRequest) {
	ids, ok := s.songIDs(q, "id")
	if !ok {
		return
	}
	if len(ids) > ssListMax*2 {
		ids = ids[:ssListMax*2]
	}
	if len(ids) == 0 {
		_, _ = s.db.Exec(`DELETE FROM subsonic_play_queues WHERE user_id = ?`, q.user.ID)
		s.ssOK(q, nil)
		return
	}
	var current int64
	if v := q.str("current"); v != "" {
		if kind, id, ok := parseSubsonicID(v); ok && kind == idTrack {
			current = id
		}
	}
	b, _ := json.Marshal(ids)
	client := q.str("c")
	if r := []rune(client); len(r) > 64 {
		client = string(r[:64])
	}
	if _, err := s.db.Exec(`INSERT INTO subsonic_play_queues (user_id, current, position_ms, track_ids, changed_by) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET current = excluded.current, position_ms = excluded.position_ms, track_ids = excluded.track_ids,
			changed_by = excluded.changed_by, changed_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`,
		q.user.ID, current, max(q.num("position", 0), 0), string(b), client); err != nil {
		s.ssError(q, err)
		return
	}
	s.ssOK(q, nil)
}

func (s *Server) ssArtistInfo(q *ssRequest, element string) {
	kind, id, ok := parseSubsonicID(q.str("id"))
	m := subsonic.Map{}
	if ok && kind == idArtist {
		if a, err := s.MusicRepo.GetArtist(id); err == nil && a.MBID != "" {
			m["musicBrainzId"] = a.MBID
		}
	}
	s.ssOK(q, subsonic.Map{element: m})
}

func (s *Server) ssAlbumInfo(q *ssRequest) {
	kind, id, ok := parseSubsonicID(q.str("id"))
	m := subsonic.Map{}
	if ok && kind == idAlbum {
		if a, err := s.MusicRepo.GetAlbum(id); err == nil && a.MBID != "" {
			m["musicBrainzId"] = a.MBID
		}
	}
	s.ssOK(q, subsonic.Map{"albumInfo": m})
}

// getTopSongs: the account's most played songs of an artist, then the rest of
// the artist's songs.
func (s *Server) ssTopSongs(q *ssRequest) {
	count := q.size("count", 50)
	songs, err := s.ssSongs(`AND LOWER(ar.name) = LOWER(?)
		ORDER BY (SELECT COUNT(*) FROM music_play_history h WHERE h.user_id = ? AND h.track_id = t.id) DESC, al.release_date DESC, t.disc, t.position LIMIT ?`,
		q.str("artist"), q.user.ID, count)
	if err != nil {
		s.ssError(q, err)
		return
	}
	s.ssOK(q, subsonic.Map{"topSongs": subsonic.Map{"song": q.ctx.songs(songs)}})
}

// getSimilarSongs: songs from the same artist, shuffled (from the library only).
func (s *Server) ssSimilarSongs(q *ssRequest, element string) {
	count := q.size("count", 50)
	kind, id, ok := parseSubsonicID(q.str("id"))
	var artistID int64
	if ok {
		switch kind {
		case idArtist:
			artistID = id
		case idAlbum:
			if a, err := s.MusicRepo.GetAlbum(id); err == nil {
				artistID = a.ArtistID
			}
		case idTrack:
			if t, err := s.MusicRepo.GetTrack(id); err == nil {
				if a, err := s.MusicRepo.GetAlbum(t.AlbumID); err == nil {
					artistID = a.ArtistID
				}
			}
		}
	}
	if artistID == 0 {
		s.ssFail(q, subsonic.ErrNotFound, "Not found.")
		return
	}
	songs, err := s.ssSongs("AND al.artist_id = ? AND t.id <> ?", artistID, id)
	if err != nil {
		s.ssError(q, err)
		return
	}
	rand.Shuffle(len(songs), func(i, j int) { songs[i], songs[j] = songs[j], songs[i] })
	if len(songs) > count {
		songs = songs[:count]
	}
	s.ssOK(q, subsonic.Map{element: subsonic.Map{"song": q.ctx.songs(songs)}})
}
