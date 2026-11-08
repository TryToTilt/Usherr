package manga

// AniList (anilist.co) has manga by English, romaji and native title, with
// other titles and a volume count. Its GraphQL API needs no key and allows 90
// requests a minute, sometimes 30, so requests are spaced out a second apart,
// and an answer is kept for a while: looking up the same title twice asks once.
// Its terms bar keeping it as a copy of their data, so only series somebody
// adds are stored; a search saves nothing. See docs/manga.md.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrAniListBusy means AniList asked to slow down.
var ErrAniListBusy = errors.New("AniList is busy right now. Try again in a minute")

// ErrAniListDown means AniList couldn't be reached or answered badly.
var ErrAniListDown = errors.New("AniList couldn't be reached")

// AniList is an AniList API client.
type AniList struct {
	Base      string
	UserAgent string
	HTTP      *http.Client
	Gap       time.Duration // the least time between requests
	KeepFor   time.Duration // how long an answer is kept

	mu    sync.Mutex
	last  time.Time
	until time.Time // asked to wait until then
	cache map[string]cachedAnswer
}

type cachedAnswer struct {
	more  bool
	about About
	at    time.Time
	list  []Found
}

// NewAniList returns a client. base "" is AniList's own address.
func NewAniList(userAgent, base string) *AniList {
	if base == "" {
		base = "https://graphql.anilist.co"
	}
	return &AniList{Base: base, UserAgent: userAgent, HTTP: &http.Client{Timeout: 20 * time.Second}, Gap: time.Second, KeepFor: 6 * time.Hour, cache: map[string]cachedAnswer{}}
}

// Found is a series as AniList knows it. Nothing is saved until someone adds it.
type Found struct {
	AniListID   int64    `json:"anilistId"`
	Title       string   `json:"title"` // English when it has one
	Aliases     []Alias  `json:"aliases"`
	Format      string   `json:"format"` // manga, manhwa, manhua, oneshot
	Status      string   `json:"status"`
	StartYear   int      `json:"startYear"`
	Author      string   `json:"author"`
	Publisher   string   `json:"publisher"`
	BuyURL      string   `json:"buyUrl,omitempty"`
	SiteURL     string   `json:"siteUrl,omitempty"`
	LicensedEN  int      `json:"licensedEn"` // 1 known, -1 unknown
	IsAdult     bool     `json:"isAdult,omitempty"`
	Description string   `json:"description,omitempty"`
	CoverURL    string   `json:"coverUrl,omitempty"`
	Volumes     int      `json:"volumes"`
	IsNovel     bool     `json:"-"`
	InLibrary   int64    `json:"inLibrary,omitempty"`
	Genres      []string `json:"genres,omitempty"`
	Score       int      `json:"score,omitempty"`      // 0 to 100
	ListStatus  string   `json:"listStatus,omitempty"` // on an AniList list: reading or planning
	Relation    string   `json:"relation,omitempty"`   // how it is related to another series
	MangaDexID  string   `json:"mangadexId,omitempty"` // when AniList links to its MangaDex entry
}

const mediaFields = `id title { romaji english native } synonyms format status startDate { year } volumes countryOfOrigin isAdult
	description(asHtml: false) coverImage { large } siteUrl
	staff(perPage: 6, sort: RELEVANCE) { edges { role node { name { full } } } }
	externalLinks { site url type language }`

const searchQuery = `query ($q: String) { Page(page: 1, perPage: 15) { media(search: $q, type: MANGA, sort: SEARCH_MATCH) { ` + mediaFields + ` } } }`
const byIDQuery = `query ($id: Int) { Media(id: $id, type: MANGA) { ` + mediaFields + ` } }`

type aniMedia struct {
	ID    int64  `json:"id"`
	Type  string `json:"type"`
	Title struct {
		Romaji  string `json:"romaji"`
		English string `json:"english"`
		Native  string `json:"native"`
	} `json:"title"`
	Synonyms  []string `json:"synonyms"`
	Format    string   `json:"format"`
	Status    string   `json:"status"`
	StartDate struct {
		Year int `json:"year"`
	} `json:"startDate"`
	Volumes         int    `json:"volumes"`
	CountryOfOrigin string `json:"countryOfOrigin"`
	IsAdult         bool   `json:"isAdult"`
	Description     string `json:"description"`
	CoverImage      struct {
		Large string `json:"large"`
	} `json:"coverImage"`
	SiteURL      string   `json:"siteUrl"`
	Genres       []string `json:"genres"`
	AverageScore int      `json:"averageScore"`
	Staff        struct {
		Edges []struct {
			Role string `json:"role"`
			Node struct {
				Name struct {
					Full string `json:"full"`
				} `json:"name"`
			} `json:"node"`
		} `json:"edges"`
	} `json:"staff"`
	ExternalLinks []struct {
		Site     string `json:"site"`
		URL      string `json:"url"`
		Type     string `json:"type"`
		Language string `json:"language"`
	} `json:"externalLinks"`
}

// The stores and publishers whose English link says a series has an official
// English edition. AniList's own isLicensed only says it's licensed somewhere,
// so it's not used.
var englishPublishers = map[string]string{
	"viz": "VIZ", "viz media": "VIZ", "viz manga": "VIZ", "shonen jump": "VIZ", "shonen jump+": "VIZ",
	"k manga": "Kodansha", "kodansha": "Kodansha", "kodansha comics": "Kodansha", "kodansha usa": "Kodansha",
	"yen press": "Yen Press", "seven seas": "Seven Seas", "seven seas entertainment": "Seven Seas", "dark horse": "Dark Horse", "dark horse comics": "Dark Horse",
	"vertical": "Vertical", "square enix": "Square Enix", "square enix manga": "Square Enix", "manga plus": "MANGA Plus", "manga up!": "Square Enix",
	"tokyopop": "Tokyopop", "titan manga": "Titan Manga", "one peace books": "One Peace", "denpa": "Denpa", "azuki": "Azuki", "inkr": "INKR", "comikey": "Comikey",
	"bookwalker": "BOOK☆WALKER", "book walker": "BOOK☆WALKER", "webtoon": "WEBTOON", "webtoons": "WEBTOON", "tapas": "Tapas", "tappytoon": "Tappytoon", "lezhin": "Lezhin", "tokyo pop": "Tokyopop",
}

var (
	htmlTag  = regexp.MustCompile(`<[^>]*>`)
	spoilers = regexp.MustCompile(`(?s)~!.*?!~`)
)

func (m aniMedia) found() Found {
	f := Found{AniListID: m.ID, StartYear: m.StartDate.Year, Volumes: m.Volumes, IsAdult: m.IsAdult, CoverURL: m.CoverImage.Large, SiteURL: m.SiteURL, LicensedEN: -1, Genres: m.Genres, Score: m.AverageScore}
	f.Title = strings.TrimSpace(m.Title.English)
	if f.Title == "" {
		f.Title = strings.TrimSpace(m.Title.Romaji)
	}
	if f.Title == "" {
		f.Title = strings.TrimSpace(m.Title.Native)
	}
	seen := map[string]bool{Key(f.Title): true}
	add := func(alias, kind string) {
		alias = strings.TrimSpace(alias)
		k := Key(alias)
		if alias == "" || k == "" || seen[k] {
			return
		}
		seen[k] = true
		f.Aliases = append(f.Aliases, Alias{Alias: alias, Kind: kind})
	}
	add(m.Title.Romaji, "romaji")
	add(m.Title.English, "english")
	for _, syn := range m.Synonyms {
		add(syn, "synonym")
	}
	add(m.Title.Native, "native")

	switch {
	case m.Format == "NOVEL":
		f.IsNovel = true
		f.Format = "novel"
	case m.CountryOfOrigin == "KR":
		f.Format = "manhwa"
	case m.CountryOfOrigin == "CN" || m.CountryOfOrigin == "TW":
		f.Format = "manhua"
	case m.Format == "ONE_SHOT":
		f.Format = "oneshot"
	default:
		f.Format = "manga"
	}
	f.Status = map[string]string{"FINISHED": "finished", "RELEASING": "ongoing", "HIATUS": "hiatus", "CANCELLED": "cancelled", "NOT_YET_RELEASED": "upcoming"}[m.Status]

	var writers []string
	for _, e := range m.Staff.Edges {
		role := strings.ToLower(e.Role)
		if strings.Contains(role, "story") || strings.Contains(role, "art") || strings.Contains(role, "original") {
			if name := strings.TrimSpace(e.Node.Name.Full); name != "" && len(writers) < 2 {
				dup := false
				for _, w := range writers {
					dup = dup || w == name
				}
				if !dup {
					writers = append(writers, name)
				}
			}
		}
	}
	f.Author = strings.Join(writers, ", ")

	for _, l := range m.ExternalLinks {
		if id := DexIDFromLink(l.URL); id != "" && f.MangaDexID == "" {
			f.MangaDexID = id
		}
	}
	for _, l := range m.ExternalLinks {
		pub, ok := englishPublishers[strings.ToLower(strings.TrimSpace(l.Site))]
		if !ok || (l.Language != "" && !strings.EqualFold(l.Language, "English")) {
			continue
		}
		f.Publisher, f.BuyURL, f.LicensedEN = pub, l.URL, 1
		break
	}

	d := spoilers.ReplaceAllString(m.Description, "")
	d = strings.ReplaceAll(strings.ReplaceAll(d, "<br>", "\n"), "<br/>", "\n")
	f.Description = strings.TrimSpace(html.UnescapeString(htmlTag.ReplaceAllString(d, "")))
	return f
}

func (a *AniList) ask(ctx context.Context, query string, vars map[string]any, out any) error {
	a.mu.Lock()
	if wait := time.Until(a.until); wait > 0 {
		a.mu.Unlock()
		return ErrAniListBusy
	}
	wait := a.Gap - time.Since(a.last)
	if wait > 0 {
		a.last = time.Now().Add(wait)
	} else {
		a.last = time.Now()
	}
	a.mu.Unlock()
	if wait > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
	body, _ := json.Marshal(map[string]any{"query": query, "variables": vars})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.Base, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", a.UserAgent)
	resp, err := a.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrAniListDown, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrAniListDown, err)
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		secs, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
		if secs <= 0 {
			secs = 60
		}
		a.mu.Lock()
		a.until = time.Now().Add(time.Duration(secs) * time.Second)
		a.mu.Unlock()
		return ErrAniListBusy
	}
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound // Media(id:) answers 404 for one that's gone
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: it answered %d", ErrAniListDown, resp.StatusCode)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("%w: %v", ErrAniListDown, err)
	}
	return nil
}

// Search finds manga by any of its titles. Novels come along marked IsNovel, and
// adult ones marked IsAdult: who may see what is for the caller to decide.
func (a *AniList) Search(ctx context.Context, q string) ([]Found, error) {
	key := strings.ToLower(strings.TrimSpace(q))
	a.mu.Lock()
	if c, ok := a.cache[key]; ok && time.Since(c.at) < a.KeepFor {
		a.mu.Unlock()
		return c.list, nil
	}
	a.mu.Unlock()
	var answer struct {
		Data struct {
			Page struct {
				Media []aniMedia `json:"media"`
			} `json:"Page"`
		} `json:"data"`
	}
	if err := a.ask(ctx, searchQuery, map[string]any{"q": q}, &answer); err != nil {
		return nil, err
	}
	list := make([]Found, 0, len(answer.Data.Page.Media))
	for _, m := range answer.Data.Page.Media {
		list = append(list, m.found())
	}
	a.mu.Lock()
	if len(a.cache) > 500 { // small and short-lived: start again rather than keep a list
		a.cache = map[string]cachedAnswer{}
	}
	a.cache[key] = cachedAnswer{at: time.Now(), list: list}
	a.mu.Unlock()
	return list, nil
}

// Get reads one series by its AniList id.
func (a *AniList) Get(ctx context.Context, id int64) (Found, error) {
	var answer struct {
		Data struct {
			Media *aniMedia `json:"Media"`
		} `json:"data"`
	}
	if err := a.ask(ctx, byIDQuery, map[string]any{"id": id}, &answer); err != nil {
		return Found{}, err
	}
	if answer.Data.Media == nil {
		return Found{}, ErrNotFound
	}
	return answer.Data.Media.found(), nil
}

// AsSeries is a series to save, from what AniList says.
func (f Found) AsSeries() Series {
	return Series{AniListID: f.AniListID, Title: f.Title, Format: f.Format, Status: f.Status, StartYear: f.StartYear, Author: f.Author, Publisher: f.Publisher,
		BuyURL: f.BuyURL, SiteURL: f.SiteURL, LicensedEN: f.LicensedEN, IsAdult: f.IsAdult, Description: f.Description, CoverURL: f.CoverURL, VolumeCount: f.Volumes}
}
