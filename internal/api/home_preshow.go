package api

// Usherr: the pre-show, played before a film in Usherr's player (a Showtime,
// or any film when asked), with the Marquee following along. After NeXroll's
// ideas, built in and simple:
//
//   - Clips are the video files in the pre-show folder. Each folder is a
//     category (Bumpers, Sound, Intermission...); a folder named after a
//     holiday (Halloween, Christmas...) plays in that holiday's season.
//   - A theme is the pre-show as a list of blocks: a card Usherr draws
//     (welcome, silence your phones, the rating, the countdown...), clips
//     from a category, the season's clip, coming-soon trailers. A block can
//     ask for a kind of film: a genre, Atmos sound, 4K, IMAX, a kids' film,
//     the time of day, a booked Showtime.
//   - Schedules say when a theme is the one that plays (weekends, a holiday,
//     some dates); a Showtime can pick its own.
//
// "Movie night" and "Quick start" come ready (9048_home_preshow.sql), so a
// pre-show plays with nothing set up; clips only add to it. The plan for one
// film is in home_preshow_plan.go.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	keyPreshowFolder   = "home.preshow.folder"   // where the clips are (default: /data/preshow when mapped, else preshow in the config folder)
	keyPreshowStarted  = "home.preshow.started"  // the folder the starter categories were made in (once a folder)
	keyPreshowTheater  = "home.preshow.theater"  // the theater's name on the welcome card
	keyPreshowTheme    = "home.preshow.theme"    // the theme that plays when no schedule says otherwise
	keyPreshowTrailerS = "home.preshow.trailerS" // how long a trailer plays at most, in seconds
	preshowClipMax     = 4 << 30                 // 4 GB: a 4K clip fits
	preshowScanEvery   = 10 * time.Minute
)

// The clips a browser plays as they are.
var preshowVideoExt = map[string]string{".mp4": "video/mp4", ".m4v": "video/mp4", ".webm": "video/webm", ".mov": "video/quicktime"}

type preshowClip struct {
	ID       int64   `json:"id"`
	Path     string  `json:"path"`
	Category string  `json:"category"`
	Title    string  `json:"title"`
	Seconds  float64 `json:"seconds"`
	Width    int     `json:"width,omitempty"`
	Height   int     `json:"height,omitempty"`
	Size     int64   `json:"size"`
	Enabled  bool    `json:"enabled"`
	FileURL  string  `json:"fileUrl"`
}

// A block of a theme.
type preshowBlock struct {
	Type     string       `json:"type"`               // card, clips, seasonal or trailers
	Card     string       `json:"card,omitempty"`     // card: welcome, popcorn, silence, coming, rating, countdown, feature
	Category string       `json:"category,omitempty"` // clips: the folder
	ClipID   int64        `json:"clipId,omitempty"`   // clips: one clip, always
	Count    int          `json:"count,omitempty"`    // clips, seasonal, trailers: how many (1 when not said)
	InOrder  bool         `json:"inOrder,omitempty"`  // clips: in name order instead of the least lately played
	Text     string       `json:"text,omitempty"`     // card: announce's words, or qr's address (Usherr's own when empty)
	When     *preshowWhen `json:"when,omitempty"`
}

// When a block plays: every part that is set has to fit.
type preshowWhen struct {
	Genres    []string `json:"genres,omitempty"`    // one of these genres
	NotGenres []string `json:"notGenres,omitempty"` // none of these
	Kinds     []string `json:"kinds,omitempty"`     // movie, episode
	Sound     []string `json:"sound,omitempty"`     // atmos, dtsx, truehd, dts
	Picture   []string `json:"picture,omitempty"`   // 4k, hdr, dv, imax
	EitherAV  bool     `json:"eitherAv,omitempty"`  // the sound or the picture will do (not both)
	Kids      *bool    `json:"kids,omitempty"`      // a film for children (G, PG, TV-Y, TV-G...) or not
	FromHour  *int     `json:"fromHour,omitempty"`  // from this hour (0-23)...
	ToHour    *int     `json:"toHour,omitempty"`    // ...to before this one (round midnight when smaller)
	Showtime  *bool    `json:"showtime,omitempty"`  // a booked Showtime, or just pressing play
	Holidays  []string `json:"holidays,omitempty"`  // only in one of these holidays' seasons ("any": any holiday's)
}

var preshowCards = map[string]float64{"welcome": 8, "popcorn": 8, "silence": 7, "coming": 6.5, "rating": 7, "countdown": 6, "feature": 7.5,
	"soon": 6, "qr": 10, "announce": 8, "reel": 10} // soon: before each trailer, by itself

type preshowTheme struct {
	ID     int64          `json:"id"`
	Name   string         `json:"name"`
	Blocks []preshowBlock `json:"blocks"`
}

// When a schedule's theme plays.
type preshowRule struct {
	Kind     string `json:"kind"`               // holiday, dates or weekly
	Holiday  string `json:"holiday,omitempty"`  // holiday: its name (preshowHolidays)
	From     string `json:"from,omitempty"`     // dates: MM-DD, every year
	To       string `json:"to,omitempty"`       // dates: MM-DD, the last day (round New Year when before From)
	Days     []int  `json:"days,omitempty"`     // weekly: 0 Sunday ... 6 Saturday
	FromHour *int   `json:"fromHour,omitempty"` // any kind: from this hour...
	ToHour   *int   `json:"toHour,omitempty"`   // ...to before this one
}

type preshowSchedule struct {
	ID       int64       `json:"id"`
	Name     string      `json:"name"`
	ThemeID  int64       `json:"themeId"`
	Rule     preshowRule `json:"rule"`
	Priority int         `json:"priority"`
	Enabled  bool        `json:"enabled"`
}

// ---- holidays ----

type preshowHoliday struct {
	Name     string `json:"name"`
	From, To string `json:"-"` // this year's days, YYYY-MM-DD
}

// preshowHolidays are the seasons a holiday's folder plays in, for the year
// of t (New Year's runs into the next one).
func preshowHolidays(t time.Time) []preshowHoliday {
	y := t.Year()
	day := func(m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, t.Location()) }
	f := func(x time.Time) string { return x.Format("2006-01-02") }
	thanks := day(time.November, 1)
	for thanks.Weekday() != time.Thursday {
		thanks = thanks.AddDate(0, 0, 1)
	}
	thanks = thanks.AddDate(0, 0, 21) // the fourth Thursday
	easter := easterSunday(y, t.Location())
	return []preshowHoliday{
		{"New Year", f(day(time.January, 1).AddDate(0, 0, -5)), f(day(time.January, 2))},
		{"Valentine's Day", f(day(time.February, 7)), f(day(time.February, 14))},
		{"St. Patrick's Day", f(day(time.March, 10)), f(day(time.March, 17))},
		{"Easter", f(easter.AddDate(0, 0, -7)), f(easter)},
		{"Fourth of July", f(day(time.June, 28)), f(day(time.July, 4))},
		{"Halloween", f(day(time.October, 1)), f(day(time.October, 31))},
		{"Thanksgiving", f(day(time.November, 1)), f(thanks)},
		{"Christmas", f(day(time.December, 1)), f(day(time.December, 26))},
		{"New Year", f(day(time.December, 27)), f(day(time.December, 31))},
	}
}

// easterSunday by the Gregorian computus (Meeus/Jones/Butcher).
func easterSunday(y int, loc *time.Location) time.Time {
	a, b, c := y%19, y/100, y%100
	d, e := b/4, b%4
	g := (8*b + 13) / 25
	h := (19*a + b - d - g + 15) % 30
	i, k := c/4, c%4
	l := (32 + 2*e + 2*i - h - k) % 7
	m := (a + 11*h + 19*l) / 433
	month := (h + l - 7*m + 90) / 25
	dd := (h + l - 7*m + 33*month + 19) % 32
	return time.Date(y, time.Month(month), dd, 0, 0, 0, 0, loc)
}

// preshowHolidayNow is the holiday whose season t is in, or "".
func preshowHolidayNow(t time.Time) string {
	today := t.Format("2006-01-02")
	for _, h := range preshowHolidays(t) {
		if today >= h.From && today <= h.To {
			return h.Name
		}
	}
	return ""
}

// preshowHolidayNames lists each holiday once, for the pages.
func preshowHolidayNames() []string {
	seen := map[string]bool{}
	var out []string
	for _, h := range preshowHolidays(time.Now()) {
		if !seen[h.Name] {
			seen[h.Name] = true
			out = append(out, h.Name)
		}
	}
	return out
}

// sameName compares folder and holiday names loosely ("st patricks day").
func sameName(a, b string) bool {
	norm := func(s string) string {
		return strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, strings.ToLower(s))
	}
	return norm(a) != "" && norm(a) == norm(b)
}

// ---- rules ----

func hourIn(h int, from, to *int) bool {
	if from == nil && to == nil {
		return true
	}
	f, t := 0, 24
	if from != nil {
		f = *from
	}
	if to != nil {
		t = *to
	}
	if f <= t {
		return h >= f && h < t
	}
	return h >= f || h < t // round midnight
}

// matches says whether the rule covers t.
func (r preshowRule) matches(t time.Time) bool {
	if !hourIn(t.Hour(), r.FromHour, r.ToHour) {
		return false
	}
	switch r.Kind {
	case "holiday":
		return sameName(preshowHolidayNow(t), r.Holiday)
	case "dates":
		md := t.Format("01-02")
		if r.From == "" || r.To == "" {
			return false
		}
		if r.From <= r.To {
			return md >= r.From && md <= r.To
		}
		return md >= r.From || md <= r.To
	case "weekly":
		for _, d := range r.Days {
			if int(t.Weekday()) == d {
				return true
			}
		}
		return false
	}
	return false
}

var mmdd = regexp.MustCompile(`^(0[1-9]|1[0-2])-(0[1-9]|[12][0-9]|3[01])$`)

func (r preshowRule) check() string {
	for _, h := range []*int{r.FromHour, r.ToHour} {
		if h != nil && (*h < 0 || *h > 24) {
			return "An hour is from 0 to 24."
		}
	}
	switch r.Kind {
	case "holiday":
		for _, n := range preshowHolidayNames() {
			if sameName(n, r.Holiday) {
				return ""
			}
		}
		return "Pick one of the holidays."
	case "dates":
		if !mmdd.MatchString(r.From) || !mmdd.MatchString(r.To) {
			return "Dates are month and day, like 12-24."
		}
	case "weekly":
		if len(r.Days) == 0 {
			return "Pick at least one day."
		}
		for _, d := range r.Days {
			if d < 0 || d > 6 {
				return "Days are 0 (Sunday) to 6 (Saturday)."
			}
		}
	default:
		return "A schedule is for a holiday, some dates or days of the week."
	}
	return ""
}

func (b preshowBlock) check() string {
	switch b.Type {
	case "card":
		if _, ok := preshowCards[b.Card]; !ok || b.Card == "soon" {
			return "That card isn't one Usherr draws."
		}
		if b.Card == "announce" && strings.TrimSpace(b.Text) == "" {
			return "Write what the announcement says."
		}
		if len([]rune(b.Text)) > 120 {
			return "At most 120 letters on a card."
		}
		if b.Card == "qr" && b.Text != "" && !strings.HasPrefix(b.Text, "https://") && !strings.HasPrefix(b.Text, "http://") {
			return "A QR code's address starts with http:// or https://."
		}
	case "clips":
		if b.Category == "" && b.ClipID == 0 {
			return "Pick a category or a clip."
		}
	case "seasonal", "trailers":
	default:
		return "A block is a card, clips, the season's clip or trailers."
	}
	if b.Count < 0 || b.Count > 10 {
		return "At most 10 at once."
	}
	if b.When != nil {
		known := map[string]bool{"any": true}
		for _, n := range preshowHolidayNames() {
			known[strings.ToLower(n)] = true
		}
		for _, h := range b.When.Holidays {
			if !known[strings.ToLower(strings.TrimSpace(h))] {
				return "That holiday isn't one Usherr knows."
			}
		}
	}
	return ""
}

// ---- the folder ----

func (s *Server) preshowDir() string {
	if v, _ := s.Settings.Get(keyPreshowFolder); strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	// Usherr: a folder of the household's own, mapped into the container (as
	// /data/trailers is), so they can drop clips in from their computer.
	if st, err := os.Stat(preshowMapped); err == nil && st.IsDir() {
		return preshowMapped
	}
	return filepath.Join(s.cfg.ConfigDir, "preshow")
}

// preshowMapped is where a mapped clips folder is looked for.
var preshowMapped = "/data/preshow"

// preshowStarters are the categories a new clips folder starts with, so
// there's somewhere to put each kind of clip (a holiday's plays in its season).
var preshowStarters = []string{"Bumpers", "Policy", "Commercials", "Sound", "Intermission", "Halloween", "Christmas"}

// preshowStart makes the starter categories in a clips folder, once (a
// category deleted later stays deleted). The first time Usherr uses a mapped
// folder, clips from the one in its config are copied over, so none are left
// behind.
func (s *Server) preshowStart(dir string) {
	if done, _ := s.Settings.Get(keyPreshowStarted); done == dir {
		return
	}
	for _, c := range preshowStarters {
		_ = os.MkdirAll(filepath.Join(dir, c), 0o755)
	}
	old := filepath.Join(s.cfg.ConfigDir, "preshow")
	if dir != old {
		_ = filepath.WalkDir(old, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if strings.HasPrefix(d.Name(), ".") && p != old {
					return filepath.SkipDir
				}
				return nil
			}
			if _, ok := preshowVideoExt[strings.ToLower(filepath.Ext(d.Name()))]; !ok {
				return nil
			}
			rel, err := filepath.Rel(old, p)
			if err != nil {
				return nil
			}
			to := filepath.Join(dir, rel)
			if _, err := os.Stat(to); err == nil {
				return nil
			}
			if os.MkdirAll(filepath.Dir(to), 0o755) == nil {
				if err := copyFile(p, to); err != nil {
					slog.Info("pre-show: a clip wasn't copied to the new folder", "clip", rel, "err", err)
				}
			}
			return nil
		})
	}
	_ = s.Settings.Set(keyPreshowStarted, dir, false)
}

// copyFile copies a file whole (to a temporary name first).
func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := to + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, to)
}

// preshowSettle is how long a file must sit unchanged before it's a clip
// (a copy into the folder may still be under way).
const preshowSettle = 20 * time.Second

var preshowScan = struct {
	sync.Mutex
	at  time.Time
	dir string
}{}

// preshowScanIfStale looks through the folder when it hasn't lately.
func (s *Server) preshowScanIfStale(ctx context.Context) {
	preshowScan.Lock()
	stale := time.Since(preshowScan.at) > preshowScanEvery || preshowScan.dir != s.preshowDir()
	preshowScan.Unlock()
	if stale {
		_ = s.preshowScanFolder(ctx)
	}
}

// preshowScanFolder brings the clips list in line with the folder: new files
// are measured (ffprobe), gone ones dropped. One folder down is a category.
func (s *Server) preshowScanFolder(ctx context.Context) error {
	preshowScan.Lock()
	defer preshowScan.Unlock()
	dir := s.preshowDir()
	_ = os.MkdirAll(dir, 0o755)
	s.preshowStart(dir)
	known := map[string]preshowClip{}
	rows, err := s.db.QueryContext(ctx, `SELECT id, path, size FROM preshow_clips`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var c preshowClip
		if rows.Scan(&c.ID, &c.Path, &c.Size) == nil {
			known[c.Path] = c
		}
	}
	rows.Close()
	seen := map[string]bool{}
	arriving := false
	err = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, ".") && p != dir {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if _, ok := preshowVideoExt[strings.ToLower(filepath.Ext(name))]; !ok {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		info, err := d.Info()
		if err != nil {
			return nil
		}
		seen[rel] = true
		if c, ok := known[rel]; ok && c.Size == info.Size() {
			return nil
		}
		// Still being copied in: it's measured on a later look.
		if time.Since(info.ModTime()) < preshowSettle {
			arriving = true
			return nil
		}
		category := ""
		if i := strings.Index(rel, "/"); i > 0 {
			category = rel[:i]
		}
		secs, w, h := probeClip(ctx, p)
		title := strings.TrimSuffix(name, filepath.Ext(name))
		title = strings.TrimSpace(strings.NewReplacer("_", " ", ".", " ").Replace(title))
		_, _ = s.db.ExecContext(ctx, `INSERT INTO preshow_clips (path, category, title, seconds, width, height, size, added_ms) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (path) DO UPDATE SET seconds = excluded.seconds, width = excluded.width, height = excluded.height, size = excluded.size`,
			rel, category, trimRunes(title, 200), secs, w, h, info.Size(), time.Now().UnixMilli())
		return nil
	})
	for p, c := range known {
		if !seen[p] {
			_, _ = s.db.ExecContext(ctx, `DELETE FROM preshow_clips WHERE id = ?`, c.ID)
		}
	}
	preshowScan.at, preshowScan.dir = time.Now(), dir
	if arriving {
		preshowScan.at = time.Now().Add(preshowSettle - preshowScanEvery) // look again soon
	}
	return err
}

// probeClip reads a clip's length and picture size.
func probeClip(ctx context.Context, file string) (secs float64, w, h int) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-select_streams", "v:0", "-show_entries", "format=duration:stream=width,height", "-of", "json", file).Output()
	if err != nil {
		return 0, 0, 0
	}
	var p struct {
		Format  struct{ Duration string } `json:"format"`
		Streams []struct{ Width, Height int }
	}
	if json.Unmarshal(out, &p) != nil {
		return 0, 0, 0
	}
	secs, _ = strconv.ParseFloat(p.Format.Duration, 64)
	if len(p.Streams) > 0 {
		w, h = p.Streams[0].Width, p.Streams[0].Height
	}
	return secs, w, h
}

func (s *Server) preshowClips(ctx context.Context) ([]preshowClip, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, path, category, title, seconds, width, height, size, enabled FROM preshow_clips ORDER BY category COLLATE NOCASE, path COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []preshowClip{}
	for rows.Next() {
		var c preshowClip
		if rows.Scan(&c.ID, &c.Path, &c.Category, &c.Title, &c.Seconds, &c.Width, &c.Height, &c.Size, &c.Enabled) == nil {
			c.FileURL = "/api/home/preshow/clips/" + strconv.FormatInt(c.ID, 10) + "/file"
			out = append(out, c)
		}
	}
	return out, rows.Err()
}

func (s *Server) preshowThemes(ctx context.Context) ([]preshowTheme, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, blocks FROM preshow_themes ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []preshowTheme{}
	for rows.Next() {
		var t preshowTheme
		var raw string
		if rows.Scan(&t.ID, &t.Name, &raw) == nil {
			_ = json.Unmarshal([]byte(raw), &t.Blocks)
			if t.Blocks == nil {
				t.Blocks = []preshowBlock{}
			}
			out = append(out, t)
		}
	}
	return out, rows.Err()
}

func (s *Server) preshowSchedules(ctx context.Context) ([]preshowSchedule, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, theme_id, rule, priority, enabled FROM preshow_schedules ORDER BY priority DESC, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []preshowSchedule{}
	for rows.Next() {
		var sc preshowSchedule
		var raw string
		if rows.Scan(&sc.ID, &sc.Name, &sc.ThemeID, &raw, &sc.Priority, &sc.Enabled) == nil {
			_ = json.Unmarshal([]byte(raw), &sc.Rule)
			out = append(out, sc)
		}
	}
	return out, rows.Err()
}

type preshowSettings struct {
	Folder         string `json:"folder"`
	InConfig       bool   `json:"inConfig,omitempty"` // Usherr: the folder is the one in Usherr's config (often out of sight)
	Theater        string `json:"theater"`
	ThemeID        int64  `json:"themeId"`
	TrailerSeconds int    `json:"trailerSeconds"`
}

func (s *Server) preshowSettingsNow() preshowSettings {
	theater, _ := s.Settings.Get(keyPreshowTheater)
	themeRaw, _ := s.Settings.Get(keyPreshowTheme)
	trRaw, _ := s.Settings.Get(keyPreshowTrailerS)
	theme, _ := strconv.ParseInt(themeRaw, 10, 64)
	tr, _ := strconv.Atoi(trRaw)
	if tr <= 0 {
		tr = 150
	}
	dir := s.preshowDir()
	return preshowSettings{Folder: dir, InConfig: dir == filepath.Join(s.cfg.ConfigDir, "preshow"), Theater: strings.TrimSpace(theater), ThemeID: theme, TrailerSeconds: tr}
}

// ---- the admin's pages ----

// GET /api/home/preshow (admin): everything the Pre-show settings show.
func (s *Server) handlePreshow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if r.URL.Query().Get("scan") == "1" {
		_ = s.preshowScanFolder(ctx)
	} else {
		s.preshowScanIfStale(ctx)
	}
	clips, err := s.preshowClips(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The pre-show couldn't be read.")
		return
	}
	themes, _ := s.preshowThemes(ctx)
	schedules, _ := s.preshowSchedules(ctx)
	cats := map[string]int{}
	for _, c := range clips {
		if c.Enabled {
			cats[c.Category]++
		}
	}
	type cat struct {
		Name  string `json:"name"`
		Clips int    `json:"clips"`
	}
	categories := []cat{}
	for n, k := range cats {
		categories = append(categories, cat{n, k})
	}
	sort.Slice(categories, func(i, j int) bool { return strings.ToLower(categories[i].Name) < strings.ToLower(categories[j].Name) })
	now := time.Now()
	type hol struct {
		Name string `json:"name"`
		From string `json:"from"`
		To   string `json:"to"`
		Now  bool   `json:"now"`
	}
	holidays := []hol{}
	current := preshowHolidayNow(now)
	for _, h := range preshowHolidays(now) {
		holidays = append(holidays, hol{h.Name, h.From, h.To, h.Name == current && now.Format("2006-01-02") >= h.From && now.Format("2006-01-02") <= h.To})
	}
	active := s.preshowThemeFor(ctx, 0, now)
	writeJSON(w, http.StatusOK, map[string]any{
		"settings": s.preshowSettingsNow(), "clips": clips, "categories": categories, "themes": themes, "schedules": schedules,
		"holidays": holidays, "holidayNow": current, "activeTheme": active.ID, "cards": preshowCardNames(),
	})
}

func preshowCardNames() []string {
	return []string{"welcome", "popcorn", "silence", "coming", "reel", "announce", "qr", "rating", "countdown", "feature"}
}

// GET /api/home/preshow/themes: the themes by name, for booking a Showtime.
func (s *Server) handlePreshowThemeNames(w http.ResponseWriter, r *http.Request) {
	themes, err := s.preshowThemes(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The themes couldn't be read.")
		return
	}
	type named struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	out := []named{}
	for _, t := range themes {
		out = append(out, named{t.ID, t.Name})
	}
	writeJSON(w, http.StatusOK, map[string]any{"themes": out, "active": s.preshowThemeFor(r.Context(), 0, time.Now()).ID})
}

// PUT /api/home/preshow/settings (admin).
func (s *Server) handlePutPreshowSettings(w http.ResponseWriter, r *http.Request) {
	var b preshowSettings
	if err := decodeJSON(r, &b); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	folder := strings.TrimSpace(b.Folder)
	if folder != "" && folder != filepath.Join(s.cfg.ConfigDir, "preshow") {
		if !filepath.IsAbs(folder) {
			writeError(w, http.StatusBadRequest, "The folder is a full path, as Usherr sees it (like /preshow).")
			return
		}
		if st, err := os.Stat(folder); err != nil || !st.IsDir() {
			writeError(w, http.StatusBadRequest, "Usherr can't see that folder.")
			return
		}
	} else {
		folder = ""
	}
	if b.TrailerSeconds < 0 || b.TrailerSeconds > 600 {
		writeError(w, http.StatusBadRequest, "A trailer plays for at most 600 seconds.")
		return
	}
	_ = s.Settings.Set(keyPreshowFolder, folder, false)
	_ = s.Settings.Set(keyPreshowTheater, trimRunes(strings.TrimSpace(b.Theater), 60), false)
	_ = s.Settings.Set(keyPreshowTheme, strconv.FormatInt(max(b.ThemeID, 0), 10), false)
	_ = s.Settings.Set(keyPreshowTrailerS, strconv.Itoa(b.TrailerSeconds), false)
	_ = s.preshowScanFolder(r.Context())
	writeJSON(w, http.StatusOK, s.preshowSettingsNow())
}

func (t preshowTheme) check() string {
	if strings.TrimSpace(t.Name) == "" {
		return "Give the theme a name."
	}
	if len(t.Blocks) > 40 {
		return "At most 40 blocks."
	}
	for _, b := range t.Blocks {
		if msg := b.check(); msg != "" {
			return msg
		}
	}
	return ""
}

// POST /api/home/preshow/themes and PUT /api/home/preshow/themes/{id} (admin).
func (s *Server) handleSavePreshowTheme(w http.ResponseWriter, r *http.Request) {
	var t preshowTheme
	if err := decodeJSON(r, &t); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	if t.Blocks == nil {
		t.Blocks = []preshowBlock{}
	}
	if msg := t.check(); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	raw, _ := json.Marshal(t.Blocks)
	name := trimRunes(strings.TrimSpace(t.Name), 60)
	now := time.Now().UnixMilli()
	if idText := r.PathValue("id"); idText != "" {
		id, _ := strconv.ParseInt(idText, 10, 64)
		res, err := s.db.ExecContext(r.Context(), `UPDATE preshow_themes SET name = ?, blocks = ?, updated_ms = ? WHERE id = ?`, name, string(raw), now, id)
		if n, _ := res.RowsAffected(); err != nil || n == 0 {
			writeError(w, http.StatusNotFound, "That theme isn't known.")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": id})
		return
	}
	res, err := s.db.ExecContext(r.Context(), `INSERT INTO preshow_themes (name, blocks, updated_ms) VALUES (?, ?, ?)`, name, string(raw), now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The theme couldn't be saved.")
		return
	}
	id, _ := res.LastInsertId()
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

// DELETE /api/home/preshow/themes/{id} (admin): its schedules go with it.
func (s *Server) handleDeletePreshowTheme(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var n int
	_ = s.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM preshow_themes`).Scan(&n)
	if n <= 1 {
		writeError(w, http.StatusBadRequest, "Keep at least one theme.")
		return
	}
	if _, err := s.db.ExecContext(r.Context(), `DELETE FROM preshow_themes WHERE id = ?`, id); err != nil {
		writeError(w, http.StatusInternalServerError, "The theme couldn't be removed.")
		return
	}
	_, _ = s.db.ExecContext(r.Context(), `DELETE FROM preshow_schedules WHERE theme_id = ?`, id)
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/home/preshow/schedules and PUT /api/home/preshow/schedules/{id} (admin).
func (s *Server) handleSavePreshowSchedule(w http.ResponseWriter, r *http.Request) {
	var sc preshowSchedule
	if err := decodeJSON(r, &sc); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	if strings.TrimSpace(sc.Name) == "" {
		sc.Name = sc.Rule.Holiday
	}
	if strings.TrimSpace(sc.Name) == "" {
		writeError(w, http.StatusBadRequest, "Give the schedule a name.")
		return
	}
	if msg := sc.Rule.check(); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	var ok int
	_ = s.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM preshow_themes WHERE id = ?`, sc.ThemeID).Scan(&ok)
	if ok == 0 {
		writeError(w, http.StatusBadRequest, "Pick a theme.")
		return
	}
	raw, _ := json.Marshal(sc.Rule)
	name := trimRunes(strings.TrimSpace(sc.Name), 60)
	if idText := r.PathValue("id"); idText != "" {
		id, _ := strconv.ParseInt(idText, 10, 64)
		res, err := s.db.ExecContext(r.Context(), `UPDATE preshow_schedules SET name = ?, theme_id = ?, rule = ?, priority = ?, enabled = ? WHERE id = ?`,
			name, sc.ThemeID, string(raw), sc.Priority, sc.Enabled, id)
		if n, _ := res.RowsAffected(); err != nil || n == 0 {
			writeError(w, http.StatusNotFound, "That schedule isn't known.")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": id})
		return
	}
	res, err := s.db.ExecContext(r.Context(), `INSERT INTO preshow_schedules (name, theme_id, rule, priority, enabled) VALUES (?, ?, ?, ?, ?)`,
		name, sc.ThemeID, string(raw), sc.Priority, sc.Enabled)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The schedule couldn't be saved.")
		return
	}
	id, _ := res.LastInsertId()
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

// DELETE /api/home/preshow/schedules/{id} (admin).
func (s *Server) handleDeletePreshowSchedule(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if _, err := s.db.ExecContext(r.Context(), `DELETE FROM preshow_schedules WHERE id = ?`, id); err != nil {
		writeError(w, http.StatusInternalServerError, "The schedule couldn't be removed.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- clips ----

var preshowNameRe = regexp.MustCompile(`[^\p{L}\p{N} ._'()&-]+`)

// cleanFolderName keeps a category a plain folder name.
func cleanFolderName(s string) string {
	s = strings.TrimSpace(preshowNameRe.ReplaceAllString(s, ""))
	s = strings.Trim(s, ". ")
	return trimRunes(s, 60)
}

// POST /api/home/preshow/clips (admin): a clip uploaded into a category's folder.
func (s *Server) handleUploadPreshowClip(w http.ResponseWriter, r *http.Request) {
	allowLongUpload(w)
	r.Body = http.MaxBytesReader(w, r.Body, preshowClipMax+1<<20)
	mr, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, "Choose a video to upload.")
		return
	}
	category := ""
	var saved string
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, "The upload didn't arrive whole. Try again.")
			return
		}
		if part.FormName() == "category" {
			b, _ := io.ReadAll(io.LimitReader(part, 512))
			category = cleanFolderName(string(b))
			continue
		}
		if part.FormName() != "file" || saved != "" {
			continue
		}
		name := filepath.Base(part.FileName())
		ext := strings.ToLower(filepath.Ext(name))
		if _, ok := preshowVideoExt[ext]; !ok {
			writeError(w, http.StatusBadRequest, "A clip is an MP4, M4V, WebM or MOV video.")
			return
		}
		base := cleanFolderName(strings.TrimSuffix(name, filepath.Ext(name)))
		if base == "" {
			base = "Clip"
		}
		dir := filepath.Join(s.preshowDir(), category)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			writeError(w, http.StatusInternalServerError, "The clip couldn't be saved.")
			return
		}
		target := filepath.Join(dir, base+ext)
		for i := 2; ; i++ {
			if _, err := os.Stat(target); errors.Is(err, os.ErrNotExist) {
				break
			}
			target = filepath.Join(dir, base+" "+strconv.Itoa(i)+ext)
		}
		// Written under a hidden name (the folder's scan skips it) and named
		// once whole, so nothing plays a clip still arriving.
		tmp := filepath.Join(dir, ".upload-"+strconv.FormatInt(time.Now().UnixNano(), 36)+ext)
		f, err := os.Create(tmp)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "The clip couldn't be saved.")
			return
		}
		_, err = io.Copy(f, part)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			_ = os.Remove(tmp)
			writeError(w, http.StatusBadRequest, "The upload didn't arrive whole. Try again.")
			return
		}
		if err := os.Rename(tmp, target); err != nil {
			_ = os.Remove(tmp)
			writeError(w, http.StatusInternalServerError, "The clip couldn't be saved.")
			return
		}
		// Whole now: dated past the settling time, so the scan below takes it at once.
		settled := time.Now().Add(-preshowSettle)
		_ = os.Chtimes(target, settled, settled)
		saved = target
	}
	if saved == "" {
		writeError(w, http.StatusBadRequest, "Choose a video to upload.")
		return
	}
	_ = s.preshowScanFolder(r.Context())
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true})
}

func (s *Server) preshowClipByID(ctx context.Context, idText string) (preshowClip, bool) {
	id, err := strconv.ParseInt(idText, 10, 64)
	if err != nil {
		return preshowClip{}, false
	}
	var c preshowClip
	err = s.db.QueryRowContext(ctx, `SELECT id, path, category, title, seconds, enabled FROM preshow_clips WHERE id = ?`, id).Scan(&c.ID, &c.Path, &c.Category, &c.Title, &c.Seconds, &c.Enabled)
	return c, err == nil
}

// preshowClipFile is the clip's file, kept inside the pre-show folder.
func (s *Server) preshowClipFile(c preshowClip) (string, bool) {
	dir := s.preshowDir()
	p := filepath.Join(dir, filepath.FromSlash(path.Clean("/"+c.Path)))
	rel, err := filepath.Rel(dir, p)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", false
	}
	return p, true
}

// PUT /api/home/preshow/clips/{id} (admin): its title, on or off, or another category (the file moves).
func (s *Server) handlePutPreshowClip(w http.ResponseWriter, r *http.Request) {
	c, ok := s.preshowClipByID(r.Context(), r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "That clip isn't known.")
		return
	}
	var b struct {
		Title    *string `json:"title"`
		Enabled  *bool   `json:"enabled"`
		Category *string `json:"category"`
	}
	if err := decodeJSON(r, &b); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	if b.Title != nil && strings.TrimSpace(*b.Title) != "" {
		c.Title = trimRunes(strings.TrimSpace(*b.Title), 200)
	}
	if b.Enabled != nil {
		c.Enabled = *b.Enabled
	}
	if b.Category != nil && cleanFolderName(*b.Category) != c.Category {
		cat := cleanFolderName(*b.Category)
		from, ok := s.preshowClipFile(c)
		if !ok {
			writeError(w, http.StatusBadRequest, "That clip can't be moved.")
			return
		}
		dir := filepath.Join(s.preshowDir(), cat)
		to := filepath.Join(dir, filepath.Base(from))
		if _, err := os.Stat(to); err == nil {
			writeError(w, http.StatusConflict, "That category already has a clip of the same name.")
			return
		}
		if err := os.MkdirAll(dir, 0o755); err != nil || os.Rename(from, to) != nil {
			writeError(w, http.StatusInternalServerError, "The clip couldn't be moved.")
			return
		}
		rel, _ := filepath.Rel(s.preshowDir(), to)
		c.Path, c.Category = filepath.ToSlash(rel), cat
	}
	if _, err := s.db.ExecContext(r.Context(), `UPDATE preshow_clips SET title = ?, enabled = ?, category = ?, path = ? WHERE id = ?`, c.Title, c.Enabled, c.Category, c.Path, c.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "The clip couldn't be saved.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": c.ID})
}

// DELETE /api/home/preshow/clips/{id} (admin): the file goes to the folder's
// .removed folder, from where it can be put back by hand.
func (s *Server) handleDeletePreshowClip(w http.ResponseWriter, r *http.Request) {
	c, ok := s.preshowClipByID(r.Context(), r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "That clip isn't known.")
		return
	}
	if from, ok := s.preshowClipFile(c); ok {
		bin := filepath.Join(s.preshowDir(), ".removed")
		_ = os.MkdirAll(bin, 0o755)
		to := filepath.Join(bin, strconv.FormatInt(time.Now().Unix(), 10)+" "+filepath.Base(from))
		if err := os.Rename(from, to); err != nil && !errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusInternalServerError, "The clip couldn't be removed.")
			return
		}
	}
	_, _ = s.db.ExecContext(r.Context(), `DELETE FROM preshow_clips WHERE id = ?`, c.ID)
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/home/preshow/clips/{id}/file: the clip, to play.
func (s *Server) handlePreshowClipFile(w http.ResponseWriter, r *http.Request) {
	c, ok := s.preshowClipByID(r.Context(), r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	p, ok := s.preshowClipFile(c)
	if !ok {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(p)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", preshowVideoExt[strings.ToLower(filepath.Ext(p))])
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "", st.ModTime(), f)
}

// preshowThemeFor is the theme that plays: the one asked for, else the
// schedule's (highest priority first), else the chosen default, else the
// first.
func (s *Server) preshowThemeFor(ctx context.Context, asked int64, now time.Time) preshowTheme {
	themes, _ := s.preshowThemes(ctx)
	byID := map[int64]preshowTheme{}
	for _, t := range themes {
		byID[t.ID] = t
	}
	if t, ok := byID[asked]; ok {
		return t
	}
	if list, err := s.preshowSchedules(ctx); err == nil {
		for _, sc := range list {
			if t, ok := byID[sc.ThemeID]; ok && sc.Enabled && sc.Rule.matches(now) {
				return t
			}
		}
	}
	if t, ok := byID[s.preshowSettingsNow().ThemeID]; ok {
		return t
	}
	if len(themes) > 0 {
		return themes[0]
	}
	return preshowTheme{Blocks: []preshowBlock{}}
}
