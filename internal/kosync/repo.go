package kosync

import (
	"database/sql"
	"errors"
	"fmt"
)

// Position is the place an app last reported for a document.
type Position struct {
	Document   string
	Percentage float64 // 0-1
	Progress   string  // kept word for word: an XPointer or a page number
	Device     string
	DeviceID   string
	Timestamp  int64 // unix seconds, of the server
}

// maxDocuments is how many documents are kept for an account; the ones not
// reported for the longest go first.
const maxDocuments = 5000

// Repo stores what the apps reported and the document ids of the books.
type Repo struct{ db *sql.DB }

// NewRepo returns a Repo over db.
func NewRepo(db *sql.DB) *Repo { return &Repo{db: db} }

// Save replaces the account's place in a document.
func (r *Repo) Save(userID int64, p Position) error {
	_, err := r.db.Exec(`INSERT INTO kosync_progress (user_id, document, percentage, progress, device, device_id, timestamp)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (user_id, document) DO UPDATE SET percentage = excluded.percentage, progress = excluded.progress,
			device = excluded.device, device_id = excluded.device_id, timestamp = excluded.timestamp`,
		userID, p.Document, p.Percentage, p.Progress, p.Device, p.DeviceID, p.Timestamp)
	if err != nil {
		return fmt.Errorf("save reading place: %w", err)
	}
	if _, err := r.db.Exec(`DELETE FROM kosync_progress WHERE rowid IN (
		SELECT rowid FROM kosync_progress WHERE user_id = ? ORDER BY timestamp DESC LIMIT -1 OFFSET ?)`, userID, maxDocuments); err != nil {
		return fmt.Errorf("tidy reading places: %w", err)
	}
	return nil
}

// Get returns the account's place in a document; ok is false when no app has
// reported one.
func (r *Repo) Get(userID int64, document string) (p Position, ok bool, err error) {
	err = r.db.QueryRow(`SELECT document, percentage, progress, device, device_id, timestamp FROM kosync_progress WHERE user_id = ? AND document = ?`,
		userID, document).Scan(&p.Document, &p.Percentage, &p.Progress, &p.Device, &p.DeviceID, &p.Timestamp)
	if errors.Is(err, sql.ErrNoRows) {
		return Position{}, false, nil
	}
	if err != nil {
		return Position{}, false, fmt.Errorf("read reading place: %w", err)
	}
	return p, true, nil
}

// Digest says which document ids one version of a book's file goes by.
type Digest struct {
	BookID     int64
	Path       string
	Sig        string
	Digest     string
	NameDigest string
}

// Digests returns what is known, by book.
func (r *Repo) Digests() (map[int64]Digest, error) {
	rows, err := r.db.Query(`SELECT book_id, path, sig, digest, name_digest FROM book_kosync_digests`)
	if err != nil {
		return nil, fmt.Errorf("read book document ids: %w", err)
	}
	defer rows.Close()
	out := map[int64]Digest{}
	for rows.Next() {
		var d Digest
		if err := rows.Scan(&d.BookID, &d.Path, &d.Sig, &d.Digest, &d.NameDigest); err != nil {
			return nil, fmt.Errorf("read book document ids: %w", err)
		}
		out[d.BookID] = d
	}
	return out, rows.Err()
}

// SaveDigest remembers a book's document ids.
func (r *Repo) SaveDigest(d Digest) error {
	_, err := r.db.Exec(`INSERT INTO book_kosync_digests (book_id, path, sig, digest, name_digest) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (book_id) DO UPDATE SET path = excluded.path, sig = excluded.sig, digest = excluded.digest, name_digest = excluded.name_digest`,
		d.BookID, d.Path, d.Sig, d.Digest, d.NameDigest)
	if err != nil {
		return fmt.Errorf("save book document ids: %w", err)
	}
	return nil
}

// ForgetDigest drops a book's document ids (its file went away).
func (r *Repo) ForgetDigest(bookID int64) error {
	if _, err := r.db.Exec(`DELETE FROM book_kosync_digests WHERE book_id = ?`, bookID); err != nil {
		return fmt.Errorf("forget book document ids: %w", err)
	}
	return nil
}

// BookFor finds the book a document id belongs to. Two books with the same id
// (the same file twice, or the same file name) link to neither: a place in the
// wrong book is worse than none.
func (r *Repo) BookFor(document string) (Digest, bool, error) {
	rows, err := r.db.Query(`SELECT book_id, path, sig, digest, name_digest FROM book_kosync_digests WHERE digest = ? OR name_digest = ? LIMIT 2`, document, document)
	if err != nil {
		return Digest{}, false, fmt.Errorf("find book by document id: %w", err)
	}
	defer rows.Close()
	var found []Digest
	for rows.Next() {
		var d Digest
		if err := rows.Scan(&d.BookID, &d.Path, &d.Sig, &d.Digest, &d.NameDigest); err != nil {
			return Digest{}, false, fmt.Errorf("find book by document id: %w", err)
		}
		found = append(found, d)
	}
	if err := rows.Err(); err != nil {
		return Digest{}, false, fmt.Errorf("find book by document id: %w", err)
	}
	if len(found) != 1 {
		return Digest{}, false, nil
	}
	return found[0], true, nil
}
