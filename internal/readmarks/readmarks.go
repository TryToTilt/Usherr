// Package readmarks keeps each person's bookmarks and highlights in books and
// comics: where they are, the words a highlight holds, and a note.
package readmarks

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Kinds.
const (
	Bookmark  = "bookmark"
	Highlight = "highlight"
)

// Limits on what one mark may hold, and how many one person keeps in one book.
const (
	MaxPosition = 2000
	MaxLabel    = 300
	MaxExcerpt  = 4000
	MaxNote     = 10000
	MaxPerItem  = 2000
)

// Colours a highlight can have ("" is the reader's own).
var Colors = []string{"", "yellow", "green", "blue", "pink"}

var (
	// ErrNotFound is a mark that isn't there, or isn't this person's.
	ErrNotFound = errors.New("not found")
	// ErrTooMany is a book with MaxPerItem marks already.
	ErrTooMany = errors.New("too many marks")
)

// ItemPattern is what an item may be: book:<id> (an ebook), audio:<id> (the
// same book's audiobook, whose places are "track:seconds") or comic:<issue id>.
var ItemPattern = regexp.MustCompile(`^(book|audio|comic|manga:v):[1-9][0-9]{0,17}$`)

// Mark is one bookmark or highlight.
type Mark struct {
	ID        int64   `json:"id"`
	Item      string  `json:"item"`
	Kind      string  `json:"kind"`
	Position  string  `json:"position"`
	Label     string  `json:"label"`
	Excerpt   string  `json:"excerpt"`
	Note      string  `json:"note"`
	Color     string  `json:"color"`
	Percent   float64 `json:"percent"`
	CreatedAt string  `json:"createdAt"`
	UpdatedAt string  `json:"updatedAt"`
}

// Problem says what is wrong with a mark before it is saved, or "".
func (m *Mark) Problem() string {
	m.Position = strings.TrimSpace(m.Position)
	m.Label = strings.TrimSpace(m.Label)
	m.Excerpt = strings.TrimSpace(m.Excerpt)
	m.Note = strings.TrimSpace(m.Note)
	switch {
	case !ItemPattern.MatchString(m.Item):
		return "That isn't a book or a comic."
	case m.Kind != Bookmark && m.Kind != Highlight:
		return "A mark is a bookmark or a highlight."
	case m.Position == "" || len(m.Position) > MaxPosition:
		return "That place in the book can't be kept."
	case m.Kind == Highlight && m.Excerpt == "":
		return "A highlight needs some words."
	}
	if !validColor(m.Color) {
		return "That colour isn't one of the choices."
	}
	if m.Percent < 0 || m.Percent > 100 || m.Percent != m.Percent {
		m.Percent = 0
	}
	m.Label = clip(m.Label, MaxLabel)
	m.Excerpt = clip(m.Excerpt, MaxExcerpt)
	if utf8.RuneCountInString(m.Note) > MaxNote {
		return "The note is too long."
	}
	return ""
}

func validColor(c string) bool {
	for _, ok := range Colors {
		if c == ok {
			return true
		}
	}
	return false
}

// clip shortens s to n characters, ending with "…" when it was cut.
func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:n-1])) + "…"
}

// Repo stores marks.
type Repo struct{ db *sql.DB }

// NewRepo returns a Repo over db.
func NewRepo(db *sql.DB) *Repo { return &Repo{db: db} }

const cols = `id, item, kind, position, label, excerpt, note, color, percent, created_at, updated_at`

func scan(sc interface{ Scan(...any) error }) (Mark, error) {
	var m Mark
	err := sc.Scan(&m.ID, &m.Item, &m.Kind, &m.Position, &m.Label, &m.Excerpt, &m.Note, &m.Color, &m.Percent, &m.CreatedAt, &m.UpdatedAt)
	return m, err
}

// List returns a person's marks in the given items, in reading order (then
// oldest first).
func (r *Repo) List(userID int64, items ...string) ([]Mark, error) {
	out := []Mark{}
	if len(items) == 0 {
		return out, nil
	}
	args := []any{userID}
	marks := make([]string, len(items))
	for i, it := range items {
		marks[i] = "?"
		args = append(args, it)
	}
	rows, err := r.db.Query(`SELECT `+cols+` FROM home_reading_marks WHERE user_id = ? AND item IN (`+strings.Join(marks, ",")+`)
		ORDER BY item, percent, id`, args...)
	if err != nil {
		return nil, fmt.Errorf("list marks: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		m, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Add saves a new mark (call Problem first). A bookmark at a place that
// already has one is not added twice: the one there is returned.
func (r *Repo) Add(userID int64, m Mark) (Mark, error) {
	if m.Kind == Bookmark {
		got, err := scan(r.db.QueryRow(`SELECT `+cols+` FROM home_reading_marks WHERE user_id = ? AND item = ? AND kind = ? AND position = ?`,
			userID, m.Item, Bookmark, m.Position))
		if err == nil {
			return got, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return Mark{}, fmt.Errorf("find mark: %w", err)
		}
	}
	var n int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM home_reading_marks WHERE user_id = ? AND item = ?`, userID, m.Item).Scan(&n); err != nil {
		return Mark{}, fmt.Errorf("count marks: %w", err)
	}
	if n >= MaxPerItem {
		return Mark{}, ErrTooMany
	}
	res, err := r.db.Exec(`INSERT INTO home_reading_marks (user_id, item, kind, position, label, excerpt, note, color, percent)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, userID, m.Item, m.Kind, m.Position, m.Label, m.Excerpt, m.Note, m.Color, m.Percent)
	if err != nil {
		return Mark{}, fmt.Errorf("add mark: %w", err)
	}
	id, _ := res.LastInsertId()
	return r.Get(userID, id)
}

// Get returns one of a person's marks.
func (r *Repo) Get(userID, id int64) (Mark, error) {
	m, err := scan(r.db.QueryRow(`SELECT `+cols+` FROM home_reading_marks WHERE user_id = ? AND id = ?`, userID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Mark{}, ErrNotFound
	}
	return m, err
}

// Update changes a mark's note, colour and label.
func (r *Repo) Update(userID, id int64, note, color, label string) (Mark, error) {
	res, err := r.db.Exec(`UPDATE home_reading_marks SET note = ?, color = ?, label = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
		WHERE user_id = ? AND id = ?`, note, color, label, userID, id)
	if err != nil {
		return Mark{}, fmt.Errorf("update mark: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Mark{}, ErrNotFound
	}
	return r.Get(userID, id)
}

// Delete removes one of a person's marks.
func (r *Repo) Delete(userID, id int64) error {
	res, err := r.db.Exec(`DELETE FROM home_reading_marks WHERE user_id = ? AND id = ?`, userID, id)
	if err != nil {
		return fmt.Errorf("delete mark: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ValidUpdate checks a changed note, colour and label, and tidies them.
func ValidUpdate(note, color, label *string) string {
	*note = strings.TrimSpace(*note)
	*label = clip(strings.TrimSpace(*label), MaxLabel)
	if utf8.RuneCountInString(*note) > MaxNote {
		return "The note is too long."
	}
	if !validColor(*color) {
		return "That colour isn't one of the choices."
	}
	return ""
}
