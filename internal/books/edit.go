package books

import (
	"database/sql"
	"fmt"
	"strings"
)

// Details are the parts of a book a person can correct by hand.
type Details struct {
	Title          string
	Author         string
	AuthorKey      string
	Year           int
	Description    string
	SeriesName     string
	SeriesPosition string
}

// UpdateDetails saves corrected details. lockSeries keeps the series as it is
// now against the look-ups that fill it in (SetSeries). Files are not moved or
// renamed: nothing in the library follows a book's name once it is filed.
func (r *Repo) UpdateDetails(id int64, d Details, lockSeries bool) error {
	res, err := r.db.Exec(`UPDATE books SET title = ?, author = ?, author_key = ?, year = ?, description = ?,
		series_name = ?, series_position = ?, series_locked = CASE WHEN ? THEN 1 ELSE series_locked END WHERE id = ?`,
		d.Title, d.Author, d.AuthorKey, d.Year, d.Description, d.SeriesName, d.SeriesPosition, lockSeries, id)
	if err != nil {
		return fmt.Errorf("save book details: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// Rematch points a book at another entry in the catalogues: the new entry's
// key, details and automatic cover, and everything looked up for the old one
// is forgotten (it is looked up again). The formats, files and reading places
// stay. A key another book already has answers ErrExists.
func (r *Repo) Rematch(id int64, b Book) error {
	res, err := r.db.Exec(`UPDATE books SET ol_key = ?, title = ?, author = ?, author_key = ?, year = ?, cover_id = ?, description = ?,
		series_name = '', series_position = '', series_locked = 0, release_date = '',
		asin = '', narrators = '', runtime_min = 0, audio_checked_at = '' WHERE id = ?`,
		b.OLKey, b.Title, b.Author, b.AuthorKey, b.Year, b.CoverID, b.Description, id)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return ErrExists
		}
		return fmt.Errorf("match the book again: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
