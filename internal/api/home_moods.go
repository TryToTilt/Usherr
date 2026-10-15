package api

// Home fork: moods for the Library's shelves ("Rows by: Mood"). A background
// job has the AI tag each film and show once with up to three moods from a
// fixed list, in batches, from its title, year, genres and the start of its
// overview. The answer is kept short (numbers, not words), so tagging a
// library of a few thousand titles costs a dollar or two, once.

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/ai"
)

// The moods, in the order the AI numbers them (1 = feel-good). The web
// shelves use the same names.
var titleMoods = []string{"feel-good", "edge-of-seat", "mind-bending", "dark", "laugh", "family", "comfort", "tearjerker", "romantic", "epic", "scary", "true-story"}

const (
	moodsEvery    = 6 * time.Hour
	moodsBatch    = 60
	moodsPerRun   = 1800 // the whole library is done in a few runs
	moodsOverview = 120
)

const moodsSystem = `You tag films and TV shows with moods for a home media library.
Moods (use the numbers):
1 feel-good, 2 edge of your seat (thrillers, tense action), 3 mind-bending, 4 dark & gritty, 5 laugh out loud, 6 family night (fine for kids), 7 comfort rewatch, 8 tearjerker, 9 romantic, 10 epic (big scale, adventure), 11 scary, 12 true story.
For each title give 1 to 3 moods that fit best, most fitting first. Use 0 when none fits.
Answer only with one line per title: its id, a colon, the numbers separated by spaces. Example:
17: 2 4
18: 5 7`

type moodTitle struct {
	Kind     string
	TMDB     int64
	Title    string
	Year     int
	Genres   string
	Overview string
}

var moodsRunning sync.Mutex

// moodsJob tags the titles that have no moods yet (the automation job).
func (s *Server) moodsJob(ctx context.Context) {
	cfg, ok := s.aiConfig()
	if !ok || !moodsRunning.TryLock() {
		return
	}
	defer moodsRunning.Unlock()
	todo := s.untaggedTitles(ctx, moodsPerRun)
	for start := 0; start < len(todo); start += moodsBatch {
		if ctx.Err() != nil {
			return
		}
		batch := todo[start:min(start+moodsBatch, len(todo))]
		var sb strings.Builder
		for i, t := range batch {
			fmt.Fprintf(&sb, "%d | %s (%d) | %s | %s\n", i+1, t.Title, t.Year, t.Genres, firstN(t.Overview, moodsOverview))
		}
		raw, err := ai.Ask(ctx, cfg, ai.Request{System: moodsSystem, Prompt: sb.String(), MaxTokens: 1500})
		if err != nil {
			log.Printf("moods: %v", err)
			return
		}
		got := parseMoods(raw, len(batch))
		if len(got) == 0 {
			log.Printf("moods: the AI's answer couldn't be read")
			continue
		}
		for i, moods := range got {
			t := batch[i]
			_, _ = s.db.ExecContext(ctx, `INSERT INTO home_title_moods (kind, tmdb_id, moods) VALUES (?, ?, ?)
				ON CONFLICT (kind, tmdb_id) DO UPDATE SET moods = excluded.moods, tagged_at = excluded.tagged_at`, t.Kind, t.TMDB, strings.Join(moods, " "))
		}
	}
	s.tagBookGenres(ctx, cfg)
}

var moodLine = regexp.MustCompile(`(?m)^\s*(\d+)\s*[:.)-]\s*([\d ,]*)`)

// parseMoods reads "id: n n" lines into moods by batch position (0-based).
// A title the answer left out is missing from the map, so it's asked again.
func parseMoods(raw string, n int) map[int][]string { return parseNumbered(raw, n, titleMoods, 3) }

// parseNumbered reads "id: n n" lines, n naming names[n-1], at most most of
// them per line.
func parseNumbered(raw string, n int, names []string, most int) map[int][]string {
	out := map[int][]string{}
	for _, m := range moodLine.FindAllStringSubmatch(raw, -1) {
		id, _ := strconv.Atoi(m[1])
		if id < 1 || id > n {
			continue
		}
		picked := []string{}
		for _, f := range strings.FieldsFunc(m[2], func(r rune) bool { return r == ' ' || r == ',' }) {
			k, err := strconv.Atoi(f)
			if err != nil || k < 1 || k > len(names) || len(picked) == most {
				continue
			}
			if v := names[k-1]; !containsStr(picked, v) {
				picked = append(picked, v)
			}
		}
		out[id-1] = picked
	}
	return out
}

func containsStr(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// untaggedTitles lists the library's films and shows, kept or streamed, that
// have no moods yet, newest first.
func (s *Server) untaggedTitles(ctx context.Context, limit int) []moodTitle {
	rows, err := s.db.QueryContext(ctx, `
		SELECT kind, tmdb_id, title, year, genres, overview FROM (
			SELECT 'movie' AS kind, tmdb_id, title, COALESCE(year, 0) AS year, COALESCE(genres, '') AS genres, COALESCE(overview, '') AS overview FROM movies
			UNION
			SELECT 'tv', tmdb_id, title, COALESCE(year, 0), COALESCE(genres, ''), COALESCE(overview, '') FROM series
			UNION
			SELECT CASE kind WHEN 'movie' THEN 'movie' ELSE 'tv' END, tmdb_id, title, year, genres, overview FROM streamed_titles
		) t
		WHERE tmdb_id > 0 AND NOT EXISTS (SELECT 1 FROM home_title_moods m WHERE m.kind = t.kind AND m.tmdb_id = t.tmdb_id)
		GROUP BY kind, tmdb_id
		ORDER BY year DESC
		LIMIT ?`, limit)
	if err != nil {
		log.Printf("moods: %v", err)
		return nil
	}
	defer rows.Close()
	var out []moodTitle
	for rows.Next() {
		var t moodTitle
		if rows.Scan(&t.Kind, &t.TMDB, &t.Title, &t.Year, &t.Genres, &t.Overview) == nil {
			t.Genres = strings.NewReplacer(`[`, ``, `]`, ``, `"`, ``).Replace(t.Genres)
			out = append(out, t)
		}
	}
	return out
}

// handleMoods: GET /api/home/moods?kind=movie|tv, the moods by TMDB id.
func (s *Server) handleMoods(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	if kind != "movie" {
		kind = "tv"
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT tmdb_id, moods FROM home_title_moods WHERE kind = ? AND moods != ''`, kind)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	out := map[int64][]string{}
	for rows.Next() {
		var id int64
		var moods string
		if rows.Scan(&id, &moods) == nil {
			out[id] = strings.Fields(moods)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// ---- book genres ----

// The genres books are sorted into (the web shelves show these names).
var bookGenres = []string{"Fantasy", "Science fiction", "Mystery", "Thriller", "Horror", "Romance", "Historical fiction", "Literary fiction", "Humor", "Young adult", "Children's", "Biography & memoir", "History", "Science & nature", "Self-help", "Business", "True crime", "Other nonfiction"}

func bookGenresSystem() string {
	var sb strings.Builder
	sb.WriteString("You sort books into genres for a home library.\nGenres (use the numbers):\n")
	for i, g := range bookGenres {
		fmt.Fprintf(&sb, "%d %s\n", i+1, g)
	}
	sb.WriteString(`For each book give 1 or 2 genres that fit best, the main one first.
Answer only with one line per book: its id, a colon, the numbers separated by spaces. Example:
3: 1 7
4: 12`)
	return sb.String()
}

// tagBookGenres gives the books without genres theirs (part of the moods job).
func (s *Server) tagBookGenres(ctx context.Context, cfg ai.Config) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, title, author, COALESCE(year, 0), COALESCE(series_name, ''), COALESCE(description, '') FROM books b
		WHERE NOT EXISTS (SELECT 1 FROM home_book_genres g WHERE g.book_id = b.id) ORDER BY id DESC LIMIT ?`, moodsPerRun)
	if err != nil {
		return
	}
	type book struct {
		id                          int64
		title, author, series, desc string
		year                        int
	}
	var todo []book
	for rows.Next() {
		var b book
		if rows.Scan(&b.id, &b.title, &b.author, &b.year, &b.series, &b.desc) == nil {
			todo = append(todo, b)
		}
	}
	rows.Close()
	system := bookGenresSystem()
	for start := 0; start < len(todo); start += moodsBatch {
		if ctx.Err() != nil {
			return
		}
		batch := todo[start:min(start+moodsBatch, len(todo))]
		var sb strings.Builder
		for i, b := range batch {
			fmt.Fprintf(&sb, "%d | %s by %s (%d) %s | %s\n", i+1, b.title, b.author, b.year, b.series, firstN(b.desc, moodsOverview))
		}
		raw, err := ai.Ask(ctx, cfg, ai.Request{System: system, Prompt: sb.String(), MaxTokens: 1500})
		if err != nil {
			log.Printf("book genres: %v", err)
			return
		}
		got := parseNumbered(raw, len(batch), bookGenres, 2)
		for i, gs := range got {
			_, _ = s.db.ExecContext(ctx, `INSERT OR REPLACE INTO home_book_genres (book_id, genres) VALUES (?, ?)`, batch[i].id, strings.Join(gs, "|"))
		}
	}
}

// handleBookGenres: GET /api/home/book-genres, the genres by book id.
func (s *Server) handleBookGenres(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT book_id, genres FROM home_book_genres WHERE genres != ''`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	out := map[int64][]string{}
	for rows.Next() {
		var id int64
		var gs string
		if rows.Scan(&id, &gs) == nil {
			out[id] = strings.Split(gs, "|")
		}
	}
	writeJSON(w, http.StatusOK, out)
}
