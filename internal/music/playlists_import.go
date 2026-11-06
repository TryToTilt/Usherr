package music

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

// ReplacePlaylistTracks makes a list hold exactly trackIDs, in that order
// (a playlist brought in from a file, read again). Tracks that no longer exist
// are skipped. It doesn't check who owns the list: the caller does.
func (r *Repo) ReplacePlaylistTracks(id int64, trackIDs []int64) error {
	if len(trackIDs) > MaxPlaylistTracks {
		trackIDs = trackIDs[:MaxPlaylistTracks]
	}
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("replace playlist tracks: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM music_playlist_tracks WHERE playlist_id = ?`, id); err != nil {
		return fmt.Errorf("replace playlist tracks: %w", err)
	}
	pos := 0
	for _, tid := range trackIDs {
		pos++
		if _, err := tx.Exec(`INSERT INTO music_playlist_tracks (playlist_id, track_id, position)
			SELECT ?, id, ? FROM tracks WHERE id = ?`, id, pos, tid); err != nil {
			return fmt.Errorf("replace playlist tracks: %w", err)
		}
	}
	if err := touch(tx, id); err != nil {
		return err
	}
	return tx.Commit()
}

// PlaylistTrackIDs is a list's track ids in order.
func (r *Repo) PlaylistTrackIDs(id int64) ([]int64, error) {
	rows, err := r.db.Query(`SELECT track_id FROM music_playlist_tracks WHERE playlist_id = ? ORDER BY position, id`, id)
	if err != nil {
		return nil, fmt.Errorf("read playlist %d: %w", id, err)
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var t int64
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

var (
	reLeadingNumber = regexp.MustCompile(`^\d{1,4}\s*[-_.]\s*`)
)

// PlaylistNameFromFile makes a readable name from a playlist's file name:
// "00-elton_john-21_at_33-web-flac-1980.m3u" becomes
// "elton john-21 at 33-web-flac-1980".
func PlaylistNameFromFile(file string) string {
	name := path.Base(slashed(file))
	name = strings.TrimSuffix(name, path.Ext(name))
	name = reLeadingNumber.ReplaceAllString(name, "")
	if !strings.Contains(name, " ") {
		name = strings.NewReplacer("_", " ", ".", " ").Replace(name)
	}
	name = strings.TrimSpace(reSpaces.ReplaceAllString(name, " "))
	if name == "" {
		return "Playlist"
	}
	return name
}
