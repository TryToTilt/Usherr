// Package directdl downloads comics from direct-download sites (home fork,
// after Pullbox's direct sources and Kapowarr's GetComics support): GetComics
// posts, and the file hosts they link to (GetComics' own server, PixelDrain,
// MediaFire, MEGA). Pages behind a Cloudflare challenge are fetched through
// FlareSolverr, when one is set.
package directdl

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// UserAgent is what the requests say they are (sites turn away Go's default).
const UserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36"

// Client fetches pages and files.
type Client struct {
	HTTP       *http.Client
	Flare      func() string // FlareSolverr's address ("" for none)
	GetComics  string        // base address, https://getcomics.org
	noRedirect *http.Client
}

// New returns a Client.
func New(flare func() string) *Client {
	return &Client{
		HTTP:      &http.Client{Timeout: 60 * time.Second},
		Flare:     flare,
		GetComics: "https://getcomics.org",
		noRedirect: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
	}
}

// ErrChallenge means a page is behind a Cloudflare challenge and no
// FlareSolverr is set (or it failed).
var ErrChallenge = errors.New("the site is behind a Cloudflare check: set FlareSolverr's address in Settings > Extras")

func challenged(status int, body string) bool {
	if status != http.StatusForbidden && status != http.StatusServiceUnavailable && status != 429 {
		return false
	}
	return strings.Contains(body, "cf-chl") || strings.Contains(body, "Just a moment") || strings.Contains(body, "challenge-platform")
}

// Page fetches an HTML page, through FlareSolverr when Cloudflare asks.
func (c *Client) Page(ctx context.Context, u string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	resp.Body.Close()
	body := string(b)
	if challenged(resp.StatusCode, body) {
		return c.flarePage(ctx, u)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s answered %d", hostOf(u), resp.StatusCode)
	}
	return body, nil
}

func hostOf(u string) string {
	if p, err := url.Parse(u); err == nil {
		return p.Host
	}
	return u
}

// ---- GetComics ----

// Post is a GetComics post: one release (an issue, a pack, a collection).
type Post struct {
	Title     string `json:"title"`
	URL       string `json:"url"`
	SizeBytes int64  `json:"sizeBytes"`
	Year      int    `json:"year,omitempty"`
}

var (
	postTitle = regexp.MustCompile(`(?s)<h1 class="post-title"><a href="([^"]+)"[^>]*>(.*?)</a></h1>`)
	postSize  = regexp.MustCompile(`(?i)Size\s*:\s*([\d.,]+)\s*(KB|MB|GB|TB)`)
	postYear  = regexp.MustCompile(`(?i)Year\s*:\s*(\d{4})`)
	tagRe     = regexp.MustCompile(`<[^>]+>`)
)

func plain(s string) string {
	return strings.Join(strings.Fields(html.UnescapeString(tagRe.ReplaceAllString(s, " "))), " ")
}

func parseSize(num, unit string) int64 {
	v, err := strconv.ParseFloat(strings.ReplaceAll(num, ",", ""), 64)
	if err != nil {
		return 0
	}
	mult := map[string]float64{"KB": 1 << 10, "MB": 1 << 20, "GB": 1 << 30, "TB": 1 << 40}[strings.ToUpper(unit)]
	return int64(v * mult)
}

// Search lists GetComics posts for a query (the first page of results).
func (c *Client) Search(ctx context.Context, query string) ([]Post, error) {
	body, err := c.Page(ctx, c.GetComics+"/?s="+url.QueryEscape(query))
	if err != nil {
		return nil, err
	}
	return parseSearch(body), nil
}

func parseSearch(body string) []Post {
	var out []Post
	// Each result is an <article>; read its title link and its size line.
	parts := strings.Split(body, "<article")
	for _, art := range parts[1:] {
		m := postTitle.FindStringSubmatch(art)
		if m == nil {
			continue
		}
		p := Post{URL: html.UnescapeString(m[1]), Title: plain(m[2])}
		text := plain(art) // "<strong>Size :</strong> 42 MB"
		if s := postSize.FindStringSubmatch(text); s != nil {
			p.SizeBytes = parseSize(s[1], s[2])
		}
		if y := postYear.FindStringSubmatch(text); y != nil {
			p.Year, _ = strconv.Atoi(y[1])
		}
		out = append(out, p)
	}
	return out
}

// Link is one download button of a post.
type Link struct {
	Host string `json:"host"` // main, mega, mediafire, pixeldrain, terabox, other
	URL  string `json:"url"`
	Name string `json:"name"` // the button's label
}

var button = regexp.MustCompile(`(?is)<a\s[^>]*href="([^"]+)"[^>]*>(.*?)</a>`)

// Links lists a post's download buttons, best host first.
func (c *Client) Links(ctx context.Context, postURL string) ([]Link, error) {
	body, err := c.Page(ctx, postURL)
	if err != nil {
		return nil, err
	}
	return parseLinks(body), nil
}

func parseLinks(body string) []Link {
	var out []Link
	seen := map[string]bool{}
	for _, m := range button.FindAllStringSubmatch(body, -1) {
		href := html.UnescapeString(m[1])
		if !strings.Contains(href, "/dls/") && !strings.Contains(href, "pixeldrain.com") && !strings.Contains(href, "mega.nz") && !strings.Contains(href, "mediafire.com") {
			continue
		}
		label := plain(m[2])
		l := Link{URL: href, Name: label}
		switch low := strings.ToLower(label + " " + href); {
		case strings.Contains(low, "mega"):
			l.Host = "mega"
		case strings.Contains(low, "mediafire"):
			l.Host = "mediafire"
		case strings.Contains(low, "pixeldrain"):
			l.Host = "pixeldrain"
		case strings.Contains(low, "terabox"):
			l.Host = "terabox"
		case strings.Contains(low, "download now") || strings.Contains(low, "main server"):
			l.Host = "main"
		default:
			l.Host = "other"
		}
		if !seen[href] {
			seen[href] = true
			out = append(out, l)
		}
	}
	rank := map[string]int{"main": 0, "pixeldrain": 1, "mediafire": 2, "mega": 3, "other": 4, "terabox": 9}
	for i := 1; i < len(out); i++ { // small list: insertion sort, stable
		for j := i; j > 0 && rank[out[j].Host] < rank[out[j-1].Host]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
