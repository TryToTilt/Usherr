package comics

// ComicVine (comicvine.gamespot.com) has every comic series and issue. Its
// API needs a free key and allows 200 requests per resource an hour, and it
// dislikes bursts, so requests are spaced out (one every ~1.2 s).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrNoKey means no ComicVine API key is set.
var ErrNoKey = errors.New("add your ComicVine API key in Comics settings first")

// ErrKeyRefused means ComicVine refused the key.
var ErrKeyRefused = errors.New("ComicVine refused the API key")

// ComicVine is a ComicVine API client.
type ComicVine struct {
	Base      string
	UserAgent string
	Key       func() string
	HTTP      *http.Client
	Gap       time.Duration

	mu   sync.Mutex
	last time.Time
}

// NewComicVine returns a client that reads its key with key().
func NewComicVine(userAgent string, key func() string) *ComicVine {
	return &ComicVine{Base: "https://comicvine.gamespot.com/api", UserAgent: userAgent, Key: key,
		HTTP: &http.Client{Timeout: 30 * time.Second}, Gap: 1200 * time.Millisecond}
}

type cvImage struct {
	Medium   string `json:"medium_url"`
	Original string `json:"original_url"`
}

func (i *cvImage) url() string {
	if i == nil {
		return ""
	}
	if i.Medium != "" {
		return i.Medium
	}
	return i.Original
}

type cvVolume struct {
	ID          int64    `json:"id"`
	Name        string   `json:"name"`
	StartYear   string   `json:"start_year"`
	CountIssues int      `json:"count_of_issues"`
	Deck        string   `json:"deck"`
	Description string   `json:"description"`
	Image       *cvImage `json:"image"`
	Publisher   *struct {
		Name string `json:"name"`
	} `json:"publisher"`
}

type cvIssue struct {
	ID          int64    `json:"id"`
	IssueNumber string   `json:"issue_number"`
	Name        string   `json:"name"`
	CoverDate   string   `json:"cover_date"`
	StoreDate   string   `json:"store_date"`
	Image       *cvImage `json:"image"`
}

func (c *ComicVine) get(ctx context.Context, path string, q url.Values, into any) error {
	key := ""
	if c.Key != nil {
		key = strings.TrimSpace(c.Key())
	}
	if key == "" {
		return ErrNoKey
	}
	c.mu.Lock()
	if wait := c.Gap - time.Since(c.last); wait > 0 {
		select {
		case <-ctx.Done():
			c.mu.Unlock()
			return ctx.Err()
		case <-time.After(wait):
		}
	}
	c.last = time.Now()
	c.mu.Unlock()
	if q == nil {
		q = url.Values{}
	}
	q.Set("api_key", key)
	q.Set("format", "json")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.Base, "/")+path+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("ComicVine didn't answer: %w", errors.Unwrap(err))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	var env struct {
		StatusCode int             `json:"status_code"`
		Error      string          `json:"error"`
		Total      int             `json:"number_of_total_results"`
		Results    json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return ErrKeyRefused
		}
		return fmt.Errorf("ComicVine answered with status %d", resp.StatusCode)
	}
	switch env.StatusCode {
	case 1:
	case 100:
		return ErrKeyRefused
	case 101:
		return ErrNotFound
	case 107:
		return errors.New("ComicVine's hourly limit is reached; try again in a while")
	default:
		return fmt.Errorf("ComicVine: %s", env.Error)
	}
	if t, ok := into.(*cvPage); ok {
		t.total = env.Total
		return json.Unmarshal(env.Results, &t.raw)
	}
	return json.Unmarshal(env.Results, into)
}

type cvPage struct {
	total int
	raw   json.RawMessage
}

// Test checks the key with one small request.
func (c *ComicVine) Test(ctx context.Context) error {
	var v []cvVolume
	return c.get(ctx, "/search/", url.Values{"resources": {"volume"}, "query": {"batman"}, "limit": {"1"}, "field_list": {"id"}}, &v)
}

const volumeFields = "id,name,start_year,count_of_issues,deck,image,publisher"

var tags = regexp.MustCompile(`<[^>]*>`)

func plain(s string) string {
	s = tags.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	return strings.Join(strings.Fields(s), " ")
}

func (v cvVolume) series() Series {
	y, _ := strconv.Atoi(strings.TrimSpace(v.StartYear))
	s := Series{CVID: v.ID, Name: strings.TrimSpace(v.Name), StartYear: y, IssueCount: v.CountIssues, ImageURL: v.Image.url()}
	if v.Publisher != nil {
		s.Publisher = v.Publisher.Name
	}
	s.Description = plain(v.Deck)
	if s.Description == "" {
		s.Description = plain(v.Description)
	}
	if len(s.Description) > 1200 {
		s.Description = s.Description[:1200] + "…"
	}
	return s
}

// SearchSeries finds series by name.
func (c *ComicVine) SearchSeries(ctx context.Context, query string, limit int) ([]Series, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	var vs []cvVolume
	if err := c.get(ctx, "/search/", url.Values{"resources": {"volume"}, "query": {query}, "limit": {strconv.Itoa(limit)}, "field_list": {volumeFields}}, &vs); err != nil {
		return nil, err
	}
	out := make([]Series, 0, len(vs))
	for _, v := range vs {
		out = append(out, v.series())
	}
	return out, nil
}

// GetSeries returns one series' details.
func (c *ComicVine) GetSeries(ctx context.Context, cvID int64) (Series, error) {
	var v cvVolume
	if err := c.get(ctx, fmt.Sprintf("/volume/4050-%d/", cvID), url.Values{"field_list": {volumeFields + ",description"}}, &v); err != nil {
		return Series{}, err
	}
	return v.series(), nil
}

// SeriesIssues returns every issue of a series.
func (c *ComicVine) SeriesIssues(ctx context.Context, cvID int64) ([]Issue, error) {
	var out []Issue
	for offset := 0; offset < 5000; offset += 100 {
		page := &cvPage{}
		if err := c.get(ctx, "/issues/", url.Values{
			"filter":     {fmt.Sprintf("volume:%d", cvID)},
			"field_list": {"id,issue_number,name,cover_date,store_date,image"},
			"sort":       {"id:asc"},
			"limit":      {"100"},
			"offset":     {strconv.Itoa(offset)},
		}, page); err != nil {
			return nil, err
		}
		var list []cvIssue
		if err := json.Unmarshal(page.raw, &list); err != nil {
			return nil, err
		}
		for _, i := range list {
			out = append(out, Issue{CVID: i.ID, Number: strings.TrimSpace(i.IssueNumber), Title: strings.TrimSpace(i.Name),
				CoverDate: i.CoverDate, StoreDate: i.StoreDate, ImageURL: i.Image.url()})
		}
		if len(list) < 100 || offset+100 >= page.total {
			break
		}
	}
	return out, nil
}
