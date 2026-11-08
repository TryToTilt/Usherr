package manga

// MangaDex (mangadex.org): a fan-scan host with a public API, used only when
// someone switches it on. Its rules, from https://api.mangadex.org/docs/:
//
//   - A real User-Agent, not a browser's, and no Via header.
//   - About 5 requests a second per address for the API, and 40 a minute for
//     /at-home/server. Going on after a 429 can bring a temporary ban (403), and
//     the ban's clock restarts with every request, so after either one this client
//     asks nothing at all until the time is up.
//   - Pages come from the at-home address it gives for a chapter, used exactly as
//     given and for about 15 minutes. No login headers go to image servers.
//   - Every image from an address without mangadex.org is reported to
//     api.mangadex.network/report, good or bad. On a failure: report it, then ask
//     /at-home/server again.
//   - Credit MangaDex and the scanlation groups. See docs/manga.md.
//
// No account is used here, and no credentials are ever sent.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	dexBaseURL   = "https://api.mangadex.org"
	dexReportURL = "https://api.mangadex.network/report"

	dexAPIGap    = 300 * time.Millisecond // under the 5 a second
	dexAtHomeGap = 2 * time.Second        // 30 a minute, under the 40
	dexImageGap  = 250 * time.Millisecond // one at a time, and not in a rush
	dexFeedPage  = 500                    // the most a feed page may hold
	dexMaxOffset = 10000                  // offset + limit may not pass this
	dexMaxImage  = 64 << 20

	dexBusyWait    = time.Minute     // a 429 that doesn't say how long
	dexRefusedWait = time.Hour       // a 403 from the API: a ban, whose clock restarts if we ask
	dexDownWait    = 2 * time.Minute // a failed answer, before anyone may ask again
)

// DexError says why MangaDex can't be asked now. It's about the whole service,
// never one series: everything waits until Until.
type DexError struct {
	Kind  string // busy (429), refused (403), down
	Until time.Time
	Note  string
}

func (e *DexError) Error() string {
	switch e.Kind {
	case DexBusy:
		return "MangaDex asked Usherr to slow down"
	case DexRefused:
		return "MangaDex is refusing requests from this address for now"
	}
	if e.Note != "" {
		return "MangaDex couldn't be reached: " + e.Note
	}
	return "MangaDex couldn't be reached"
}

// The kinds of DexError.
const (
	DexBusy    = "busy"
	DexRefused = "refused"
	DexDown    = "down"
)

// ErrDexPage means one page couldn't be fetched from its at-home server. It's
// been reported, and asking /at-home/server again is the next step.
var ErrDexPage = errors.New("a page couldn't be fetched")

// DexSeries is a series on MangaDex.
type DexSeries struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	AltTitles    []string `json:"altTitles,omitempty"`
	AniListID    int64    `json:"anilistId,omitempty"`
	LicensedLink string   `json:"licensedLink,omitempty"` // its "official English translation" link
	Year         int      `json:"year,omitempty"`
	Status       string   `json:"status,omitempty"`
	Rating       string   `json:"rating,omitempty"`
	Languages    []string `json:"languages,omitempty"`
}

// DexGroup is a scanlation group.
type DexGroup struct {
	ID   string
	Name string
}

// DexChapter is one upload of a chapter on MangaDex.
type DexChapter struct {
	ID          string
	Number      string // "" for a one-shot
	Volume      string
	Title       string
	Language    string
	Pages       int
	ExternalURL string
	Unavailable bool
	PublishedAt string
	Groups      []DexGroup
}

// AtHome is where a chapter's pages are, for the next quarter hour or so.
type AtHome struct {
	BaseURL   string
	Hash      string
	Data      []string
	DataSaver []string
}

// Files are the page names at a quality.
func (a AtHome) Files(saver bool) []string {
	if saver {
		return a.DataSaver
	}
	return a.Data
}

// MangaDex is a MangaDex API client. It keeps to one pace for everything that
// goes through it, however many callers there are.
type MangaDex struct {
	Base      string
	ReportURL string
	UserAgent string
	HTTP      *http.Client
	Images    *http.Client
	APIGap    time.Duration
	AtHomeGap time.Duration
	ImageGap  time.Duration
	Uploads   string // where covers are: uploads.mangadex.org, or a test copy (mdbrowse.go)

	mu        sync.Mutex
	nextAPI   time.Time
	nextHome  time.Time
	nextImage time.Time
	nextCover time.Time
	pause     *DexError

	reportOnce sync.Once
	reports    chan dexReport
	reporting  sync.WaitGroup
}

// NewMangaDex returns a client. base "" is MangaDex itself; another base is a
// test copy, which is then told the reports too.
func NewMangaDex(userAgent, base string) *MangaDex {
	report, uploads := dexReportURL, dexUploadsURL
	if base == "" {
		base = dexBaseURL
	} else {
		report = strings.TrimRight(base, "/") + "/network/report"
		uploads = strings.TrimRight(base, "/") + "/uploads"
	}
	return &MangaDex{
		Base: strings.TrimRight(base, "/"), ReportURL: report, UserAgent: userAgent, Uploads: uploads,
		HTTP: &http.Client{Timeout: 30 * time.Second}, Images: &http.Client{Timeout: 2 * time.Minute},
		APIGap: dexAPIGap, AtHomeGap: dexAtHomeGap, ImageGap: dexImageGap,
	}
}

// Paused says what MangaDex asked for, while it lasts.
func (c *MangaDex) Paused() *DexError {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pause != nil && time.Now().Before(c.pause.Until) {
		return c.pause
	}
	c.pause = nil
	return nil
}

// Pause holds everything back until a time: what was asked for before a restart.
func (c *MangaDex) Pause(kind string, until time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pause == nil || until.After(c.pause.Until) {
		c.pause = &DexError{Kind: kind, Until: until}
	}
}

// turn waits for a place at the given pace, unless MangaDex asked for quiet.
func (c *MangaDex) turn(ctx context.Context, next *time.Time, gap time.Duration) error {
	c.mu.Lock()
	if c.pause != nil && time.Now().Before(c.pause.Until) {
		p := c.pause
		c.mu.Unlock()
		return p
	}
	now := time.Now()
	at := *next
	if at.Before(now) {
		at = now
	}
	*next = at.Add(gap)
	c.mu.Unlock()
	if wait := time.Until(at); wait > 0 {
		t := time.NewTimer(wait)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
	return ctx.Err()
}

func (c *MangaDex) hold(kind string, until time.Time, note string) *DexError {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pause == nil || until.After(c.pause.Until) {
		c.pause = &DexError{Kind: kind, Until: until, Note: note}
	}
	return c.pause
}

// waitFrom reads how long a 429 or an endpoint's limit says to wait.
func waitFrom(h http.Header, fallback time.Duration) time.Time {
	if v, err := strconv.ParseInt(strings.TrimSpace(h.Get("X-RateLimit-Retry-After")), 10, 64); err == nil && v > 0 {
		if t := time.Unix(v, 0); t.After(time.Now()) {
			return t
		}
	}
	if secs, err := strconv.Atoi(strings.TrimSpace(h.Get("Retry-After"))); err == nil && secs > 0 {
		return time.Now().Add(time.Duration(secs) * time.Second)
	}
	return time.Now().Add(fallback)
}

// get asks the API once. It never tries again by itself: the caller's schedule does.
func (c *MangaDex) get(ctx context.Context, path string, q url.Values, next *time.Time, gap time.Duration, out any) error {
	if err := c.turn(ctx, next, gap); err != nil {
		return err
	}
	target := c.Base + path
	if len(q) > 0 {
		target += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return c.hold(DexDown, time.Now().Add(dexDownWait), "no answer")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return c.hold(DexBusy, waitFrom(resp.Header, dexBusyWait), "")
	case resp.StatusCode == http.StatusForbidden:
		return c.hold(DexRefused, time.Now().Add(dexRefusedWait), "")
	case resp.StatusCode == http.StatusNotFound:
		return ErrNotFound
	case resp.StatusCode >= 500:
		return c.hold(DexDown, time.Now().Add(dexDownWait), fmt.Sprintf("it answered %d", resp.StatusCode))
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("MangaDex turned the request down (%d)", resp.StatusCode)
	case err != nil:
		return c.hold(DexDown, time.Now().Add(dexDownWait), "the answer broke off")
	}
	// The last one allowed in this period: wait for the next, rather than be told off.
	if resp.Header.Get("X-RateLimit-Remaining") == "0" {
		c.hold(DexBusy, waitFrom(resp.Header, dexBusyWait), "")
	}
	if err := json.Unmarshal(data, out); err != nil {
		// Usherr: an answer in a shape Usherr doesn't expect is Usherr's to fix,
		// not MangaDex being away, so nothing else is held up by it.
		slog.Warn("manga: a MangaDex answer couldn't be read", "path", path, "err", err)
		return fmt.Errorf("MangaDex's answer to %s couldn't be read", path)
	}
	return nil
}

type dexRelation struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Attributes *struct {
		Name string `json:"name"`
	} `json:"attributes"`
}

type dexMangaJSON struct {
	ID         string `json:"id"`
	Attributes struct {
		Title     map[string]string   `json:"title"`
		AltTitles []map[string]string `json:"altTitles"`
		Links     map[string]string   `json:"links"`
		Year      int                 `json:"year"`
		Status    string              `json:"status"`
		Rating    string              `json:"contentRating"`
		Languages []string            `json:"availableTranslatedLanguages"`
	} `json:"attributes"`
}

func (m dexMangaJSON) series() DexSeries {
	a := m.Attributes
	out := DexSeries{ID: m.ID, Year: a.Year, Status: a.Status, Rating: a.Rating, Languages: a.Languages, LicensedLink: a.Links["engtl"]}
	out.AniListID, _ = strconv.ParseInt(strings.TrimSpace(a.Links["al"]), 10, 64)
	titles := []string{a.Title["en"]}
	for _, lang := range sortedKeys(a.Title) {
		titles = append(titles, a.Title[lang])
	}
	for _, alt := range a.AltTitles {
		for _, lang := range sortedKeys(alt) {
			titles = append(titles, alt[lang])
		}
	}
	seen := map[string]bool{}
	for _, t := range titles {
		t = strings.TrimSpace(t)
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		if out.Title == "" {
			out.Title = t
		} else {
			out.AltTitles = append(out.AltTitles, t)
		}
	}
	return out
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// dexRatings are the content ratings to ask for: adult ones only for a series
// already known to be adult, so a same-named doujinshi can't be matched by mistake.
func dexRatings(adult bool) []string {
	if adult {
		return []string{"safe", "suggestive", "erotica", "pornographic"}
	}
	return []string{"safe", "suggestive", "erotica"}
}

// Search finds series by a title.
func (c *MangaDex) Search(ctx context.Context, title string, adult bool) ([]DexSeries, error) {
	q := url.Values{"title": {title}, "limit": {"20"}, "order[relevance]": {"desc"}, "contentRating[]": dexRatings(adult)}
	var answer struct {
		Data []dexMangaJSON `json:"data"`
	}
	if err := c.get(ctx, "/manga", q, &c.nextAPI, c.APIGap, &answer); err != nil {
		return nil, err
	}
	out := make([]DexSeries, 0, len(answer.Data))
	for _, m := range answer.Data {
		out = append(out, m.series())
	}
	return out, nil
}

// Get reads one series.
func (c *MangaDex) Get(ctx context.Context, id string) (DexSeries, error) {
	if !dexIDLike.MatchString(id) {
		return DexSeries{}, ErrNotFound
	}
	var answer struct {
		Data dexMangaJSON `json:"data"`
	}
	if err := c.get(ctx, "/manga/"+id, nil, &c.nextAPI, c.APIGap, &answer); err != nil {
		return DexSeries{}, err
	}
	return answer.Data.series(), nil
}

type dexChapterJSON struct {
	ID         string `json:"id"`
	Attributes struct {
		Volume      *string `json:"volume"`
		Chapter     *string `json:"chapter"`
		Title       *string `json:"title"`
		Language    string  `json:"translatedLanguage"`
		ExternalURL *string `json:"externalUrl"`
		Unavailable bool    `json:"isUnavailable"`
		PublishAt   string  `json:"publishAt"`
		Pages       int     `json:"pages"`
	} `json:"attributes"`
	Relationships []dexRelation `json:"relationships"`
}

func (j dexChapterJSON) chapter() DexChapter {
	a := j.Attributes
	str := func(p *string) string {
		if p == nil {
			return ""
		}
		return strings.TrimSpace(*p)
	}
	out := DexChapter{ID: j.ID, Number: str(a.Chapter), Volume: str(a.Volume), Title: str(a.Title), Language: a.Language, Pages: a.Pages, Unavailable: a.Unavailable, PublishedAt: a.PublishAt, ExternalURL: safeLink(str(a.ExternalURL))}
	for _, r := range j.Relationships {
		if r.Type == "scanlation_group" && r.Attributes != nil {
			out.Groups = append(out.Groups, DexGroup{ID: r.ID, Name: strings.TrimSpace(r.Attributes.Name)})
		}
	}
	return out
}

// safeLink keeps only a plain web address, as it ends up behind a link on a page.
func safeLink(s string) string {
	if u, err := url.Parse(s); err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" {
		return s
	}
	return ""
}

// Feed lists a series' chapters in a language, every upload of each, with those
// hosted elsewhere and those taken down: telling those apart from "nothing" is
// the whole point. Chapters not out yet aren't listed.
func (c *MangaDex) Feed(ctx context.Context, id, language string, adult bool) ([]DexChapter, error) {
	if !dexIDLike.MatchString(id) {
		return nil, ErrNotFound
	}
	var out []DexChapter
	for offset := 0; offset+dexFeedPage <= dexMaxOffset; offset += dexFeedPage {
		q := url.Values{
			"limit": {strconv.Itoa(dexFeedPage)}, "offset": {strconv.Itoa(offset)}, "translatedLanguage[]": {language},
			"order[volume]": {"asc"}, "order[chapter]": {"asc"}, "includes[]": {"scanlation_group"}, "contentRating[]": dexRatings(adult),
			"includeUnavailable": {"1"}, "includeExternalUrl": {"1"}, "includeEmptyPages": {"1"}, "includeFuturePublishAt": {"0"},
		}
		var answer struct {
			Data  []dexChapterJSON `json:"data"`
			Total int              `json:"total"`
		}
		if err := c.get(ctx, "/manga/"+id+"/feed", q, &c.nextAPI, c.APIGap, &answer); err != nil {
			return nil, err
		}
		for _, j := range answer.Data {
			out = append(out, j.chapter())
		}
		if len(answer.Data) == 0 || offset+len(answer.Data) >= answer.Total {
			break
		}
	}
	return out, nil
}

// AtHome asks where a chapter's pages are. Its pace is its own, slower one.
func (c *MangaDex) AtHome(ctx context.Context, chapterID string) (AtHome, error) {
	if !dexIDLike.MatchString(chapterID) {
		return AtHome{}, ErrNotFound
	}
	var answer struct {
		BaseURL string `json:"baseUrl"`
		Chapter struct {
			Hash      string   `json:"hash"`
			Data      []string `json:"data"`
			DataSaver []string `json:"dataSaver"`
		} `json:"chapter"`
	}
	if err := c.get(ctx, "/at-home/server/"+chapterID, nil, &c.nextHome, c.AtHomeGap, &answer); err != nil {
		return AtHome{}, err
	}
	if safeLink(answer.BaseURL) == "" {
		return AtHome{}, c.hold(DexDown, time.Now().Add(dexDownWait), "the answer couldn't be read")
	}
	return AtHome{BaseURL: answer.BaseURL, Hash: answer.Chapter.Hash, Data: answer.Chapter.Data, DataSaver: answer.Chapter.DataSaver}, nil
}

// Page fetches one page into w, and reports how that went when the server asks
// for it. Any failure comes back as ErrDexPage: report, then ask /at-home/server.
func (c *MangaDex) Page(ctx context.Context, at AtHome, saver bool, file string, w io.Writer) (int64, error) {
	if err := c.turn(ctx, &c.nextImage, c.ImageGap); err != nil {
		return 0, err
	}
	quality := "data"
	if saver {
		quality = "data-saver"
	}
	target := strings.TrimRight(at.BaseURL, "/") + "/" + quality + "/" + at.Hash + "/" + file
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return 0, ErrDexPage
	}
	req.Header.Set("User-Agent", c.UserAgent)
	start := time.Now()
	resp, err := c.Images.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err() // we stopped it: nothing to say about the server
		}
		c.report(ctx, target, dexReport{URL: target, Success: false, Bytes: 0, Duration: time.Since(start).Milliseconds()})
		return 0, ErrDexPage
	}
	defer resp.Body.Close()
	var n int64
	ok := resp.StatusCode == http.StatusOK
	if ok {
		n, err = io.Copy(w, io.LimitReader(resp.Body, dexMaxImage))
		ok = err == nil && n > 0 && (resp.ContentLength < 0 || resp.ContentLength == n)
		if err != nil && ctx.Err() != nil {
			return n, ctx.Err()
		}
	} else {
		n, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	}
	c.report(ctx, target, dexReport{URL: target, Success: ok, Cached: strings.HasPrefix(resp.Header.Get("X-Cache"), "HIT"), Bytes: n, Duration: time.Since(start).Milliseconds()})
	if !ok {
		return n, ErrDexPage
	}
	return n, nil
}

type dexReport struct {
	URL      string `json:"url"`
	Success  bool   `json:"success"`
	Cached   bool   `json:"cached"`
	Bytes    int64  `json:"bytes"`
	Duration int64  `json:"duration"`
}

// report tells the at-home network how an image went, when its address doesn't
// have mangadex.org in it. A report that can't be sent doesn't stop anything, and
// reports are sent one at a time, so a slow report address can't slow the pages
// beyond a full line.
func (c *MangaDex) report(ctx context.Context, target string, r dexReport) {
	u, err := url.Parse(target)
	if err != nil || strings.Contains(strings.ToLower(u.Hostname()), "mangadex.org") {
		return
	}
	c.reportOnce.Do(func() {
		c.reports = make(chan dexReport, 256)
		go c.sendReports()
	})
	c.reporting.Add(1)
	select {
	case c.reports <- r:
	case <-ctx.Done():
		c.reporting.Done()
	}
}

func (c *MangaDex) sendReports() {
	client := &http.Client{Timeout: 5 * time.Second}
	for r := range c.reports {
		body, _ := json.Marshal(r)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.ReportURL, bytes.NewReader(body))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("User-Agent", c.UserAgent)
			if resp, err := client.Do(req); err != nil {
				slog.Debug("mangadex: a report wasn't sent", "err", err)
			} else {
				_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
				resp.Body.Close()
			}
		}
		cancel()
		c.reporting.Done()
	}
}

// WaitReports waits until every report so far has been sent.
func (c *MangaDex) WaitReports() { c.reporting.Wait() }

var dexIDLike = regexp.MustCompile(`^[0-9a-fA-F-]{36}$`)

// IsDexID reports whether s looks like a MangaDex id.
func IsDexID(s string) bool { return dexIDLike.MatchString(s) }

var dexTitleURL = regexp.MustCompile(`(?i)mangadex\.org/(?:title|manga)/([0-9a-f-]{36})`)

// DexIDFromLink reads a MangaDex id out of a link to its title page.
func DexIDFromLink(link string) string {
	if m := dexTitleURL.FindStringSubmatch(link); m != nil {
		return strings.ToLower(m[1])
	}
	return ""
}

// The ways a series is matched to its entry on MangaDex.
const (
	DexLinked = "linked"
	DexUnsure = "unsure"
	DexNone   = "none"
)

// DexMatch picks a series' entry out of a search: a cross-link to the same AniList
// entry is proof; a single exact title, in the right years, is good enough. Anything
// else is for a person to confirm, and an entry linked to another AniList series or
// marked a doujinshi is never it.
func DexMatch(s Series, names []string, found []DexSeries) (best *DexSeries, how string, unsure []DexSeries) {
	keys := map[string]bool{}
	for _, n := range names {
		if k := Key(n); k != "" {
			keys[k] = true
		}
	}
	var exact, near []DexSeries
	for i := range found {
		f := found[i]
		if s.AniListID > 0 && f.AniListID == s.AniListID {
			return &found[i], "anilist", nil
		}
		if s.AniListID > 0 && f.AniListID > 0 {
			continue // it says it's another series
		}
		if strings.Contains(strings.ToLower(f.Title), "doujinshi") {
			continue
		}
		matched := false
		for _, t := range append([]string{f.Title}, f.AltTitles...) {
			if keys[Key(t)] {
				matched = true
				break
			}
		}
		if matched {
			exact = append(exact, f)
		} else {
			near = append(near, f)
		}
	}
	if len(exact) == 1 && exact[0].Year > 0 && s.StartYear > 0 && absInt(exact[0].Year-s.StartYear) <= 1 {
		return &exact[0], "title", nil
	}
	unsure = append(exact, near...)
	if len(unsure) > 6 {
		unsure = unsure[:6]
	}
	return nil, "", unsure
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
