package comics

// HideMovedToManga hides a series from Comics once it has moved to Manga (and shows
// it again with 0). Its rows stay, so an undo gets it back as it was. Nothing is
// looked for while it's hidden.
func (r *Repo) HideMovedToManga(id, mangaSeriesID int64) error {
	if mangaSeriesID > 0 {
		if _, err := r.db.Exec(`UPDATE comic_series SET monitor_new = 0 WHERE id = ?`, id); err != nil {
			return err
		}
	}
	_, err := r.db.Exec(`UPDATE comic_series SET moved_to_manga = ? WHERE id = ?`, mangaSeriesID, id)
	return err
}

// MovedToManga is the Manga series a comic series moved to, or 0.
func (r *Repo) MovedToManga(id int64) int64 {
	var n int64
	_ = r.db.QueryRow(`SELECT moved_to_manga FROM comic_series WHERE id = ?`, id).Scan(&n)
	return n
}
