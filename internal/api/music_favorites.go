package api

// Usherr: liked tracks, albums and artists, per account. The heart in the
// player and on album and artist pages, and the star in Subsonic apps. A liked
// track can be loved on Last.fm too (music_lastfm.go). Modelled on
// DroppedNeedle's user_favorites (AGPL-3.0), backend/api/compat/subsonic/router.py.

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
)

// Kinds of favourite.
const (
	favTrack  = "track"
	favAlbum  = "album"
	favArtist = "artist"
)

// favoriteExists reports whether the item is in the library.
func (s *Server) favoriteExists(kind string, id int64) bool {
	var err error
	switch kind {
	case favTrack:
		_, err = s.MusicRepo.GetTrack(id)
	case favAlbum:
		_, err = s.MusicRepo.GetAlbum(id)
	case favArtist:
		_, err = s.MusicRepo.GetArtist(id)
	default:
		return false
	}
	return err == nil
}

// setFavorite likes or unlikes an item. It reports whether the item exists.
func (s *Server) setFavorite(userID int64, kind string, id int64, on bool) (bool, error) {
	if !s.favoriteExists(kind, id) {
		return false, nil
	}
	var res sql.Result
	var err error
	if on {
		res, err = s.db.Exec(`INSERT OR IGNORE INTO music_favorites (user_id, kind, item_id) VALUES (?, ?, ?)`, userID, kind, id)
	} else {
		res, err = s.db.Exec(`DELETE FROM music_favorites WHERE user_id = ? AND kind = ? AND item_id = ?`, userID, kind, id)
	}
	if err != nil {
		return true, err
	}
	if n, _ := res.RowsAffected(); n > 0 && kind == favTrack {
		s.loveOnLastFM(userID, id, on)
	}
	return true, nil
}

// favoriteIDs is an account's liked items of one kind, newest first.
func (s *Server) favoriteIDs(userID int64, kind string) ([]int64, error) {
	rows, err := s.db.Query(`SELECT item_id FROM music_favorites WHERE user_id = ? AND kind = ? ORDER BY created_at DESC, item_id`, userID, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// favoriteTimes maps an account's liked items of one kind to when they were liked.
func (s *Server) favoriteTimes(userID int64, kind string) map[int64]string {
	out := map[int64]string{}
	rows, err := s.db.Query(`SELECT item_id, created_at FROM music_favorites WHERE user_id = ? AND kind = ?`, userID, kind)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var at string
		if rows.Scan(&id, &at) == nil {
			out[id] = at
		}
	}
	return out
}

// GET /api/music/favorites: {tracks, albums, artists} ids.
func (s *Server) handleListFavorites(w http.ResponseWriter, r *http.Request) {
	me, ok := signedInAccount(w, r)
	if !ok {
		return
	}
	out := map[string][]int64{}
	for _, k := range []string{favTrack, favAlbum, favArtist} {
		ids, err := s.favoriteIDs(me, k)
		if err != nil {
			slog.Warn("read favourites", "err", err)
			writeError(w, http.StatusInternalServerError, "Couldn't read your likes. Check the log for details.")
			return
		}
		out[k+"s"] = ids
	}
	writeJSON(w, http.StatusOK, out)
}

// PUT and DELETE /api/music/favorites/{kind}/{id}
func (s *Server) handleSetFavorite(w http.ResponseWriter, r *http.Request) {
	me, ok := signedInAccount(w, r)
	if !ok {
		return
	}
	kind := r.PathValue("kind")
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 || (kind != favTrack && kind != favAlbum && kind != favArtist) {
		writeError(w, http.StatusBadRequest, "Say what to like: a track, album or artist.")
		return
	}
	on := r.Method == http.MethodPut
	found, err := s.setFavorite(me, kind, id, on)
	if !found {
		writeError(w, http.StatusNotFound, "That isn't in your library.")
		return
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		slog.Warn("save favourite", "err", err)
		writeError(w, http.StatusInternalServerError, "Couldn't save that. Check the log for details.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"kind": kind, "id": id, "liked": on})
}
