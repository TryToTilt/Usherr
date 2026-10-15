// Package news reads news feeds (RSS 2.0 and Atom) for the home fork's
// entertainment news on the Dashboard.
package news

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Item is one story.
type Item struct {
	GUID      string
	Title     string
	Link      string
	Published time.Time
	Teaser    string // plain text, at most about 600 characters
}

var client = &http.Client{Timeout: 25 * time.Second}

// Fetch reads a feed.
func Fetch(ctx context.Context, url, userAgent string) ([]Item, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/xml, text/xml;q=0.9, */*;q=0.5")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("the feed answered %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

type rssDoc struct {
	Items []struct {
		Title       string `xml:"title"`
		Link        string `xml:"link"`
		GUID        string `xml:"guid"`
		PubDate     string `xml:"pubDate"`
		Date        string `xml:"http://purl.org/dc/elements/1.1/ date"`
		Description string `xml:"description"`
		Content     string `xml:"http://purl.org/rss/1.0/modules/content/ encoded"`
	} `xml:"channel>item"`
}

type atomDoc struct {
	Entries []struct {
		Title string `xml:"title"`
		ID    string `xml:"id"`
		Links []struct {
			Href string `xml:"href,attr"`
			Rel  string `xml:"rel,attr"`
		} `xml:"link"`
		Published string `xml:"published"`
		Updated   string `xml:"updated"`
		Summary   string `xml:"summary"`
		Content   string `xml:"content"`
	} `xml:"entry"`
}

// Parse reads an RSS 2.0 or Atom document.
func Parse(data []byte) ([]Item, error) {
	head := strings.ToLower(string(data[:min(len(data), 600)]))
	var out []Item
	if strings.Contains(head, "<feed") {
		var doc atomDoc
		if err := decode(data, &doc); err != nil {
			return nil, err
		}
		for _, e := range doc.Entries {
			link := ""
			for _, l := range e.Links {
				if l.Rel == "" || l.Rel == "alternate" {
					link = l.Href
					break
				}
			}
			body := e.Summary
			if strings.TrimSpace(body) == "" {
				body = e.Content
			}
			it := Item{GUID: firstNonEmpty(e.ID, link), Title: Plain(e.Title), Link: strings.TrimSpace(link), Published: parseTime(firstNonEmpty(e.Published, e.Updated)), Teaser: teaser(body)}
			if it.Title != "" && it.Link != "" {
				out = append(out, it)
			}
		}
		return out, nil
	}
	var doc rssDoc
	if err := decode(data, &doc); err != nil {
		return nil, err
	}
	if len(doc.Items) == 0 && !strings.Contains(head, "<rss") && !strings.Contains(head, "<rdf") {
		return nil, errors.New("that isn't an RSS or Atom feed")
	}
	for _, i := range doc.Items {
		body := i.Description
		if strings.TrimSpace(Plain(body)) == "" {
			body = i.Content
		}
		it := Item{GUID: firstNonEmpty(i.GUID, i.Link), Title: Plain(i.Title), Link: strings.TrimSpace(i.Link), Published: parseTime(firstNonEmpty(i.PubDate, i.Date)), Teaser: teaser(body)}
		if it.Title != "" && it.Link != "" {
			out = append(out, it)
		}
	}
	return out, nil
}

// decode reads XML, also in the Latin-1 and Windows-1252 encodings some
// feeds still declare.
func decode(data []byte, into any) error {
	d := xml.NewDecoder(bytes.NewReader(data))
	d.CharsetReader = func(label string, in io.Reader) (io.Reader, error) {
		switch strings.ToLower(label) {
		case "iso-8859-1", "iso8859-1", "latin1", "latin-1", "windows-1252", "cp1252", "us-ascii":
			raw, err := io.ReadAll(in)
			if err != nil {
				return nil, err
			}
			runes := make([]rune, len(raw))
			for i, b := range raw {
				runes[i] = rune(b)
			}
			return strings.NewReader(string(runes)), nil
		}
		return nil, fmt.Errorf("the feed's text encoding %q isn't supported", label)
	}
	return d.Decode(into)
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s = strings.TrimSpace(s); s != "" {
			return s
		}
	}
	return ""
}

var timeLayouts = []string{time.RFC1123Z, time.RFC1123, time.RFC3339, "Mon, 2 Jan 2006 15:04:05 -0700", "Mon, 2 Jan 2006 15:04:05 MST", "2006-01-02T15:04:05Z07:00", "2006-01-02 15:04:05", "2006-01-02"}

func parseTime(s string) time.Time {
	s = strings.TrimSpace(s)
	for _, l := range timeLayouts {
		if t, err := time.Parse(l, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

var (
	tagRE      = regexp.MustCompile(`(?s)<[^>]*>`)
	boilerRE   = regexp.MustCompile(`(?i)\s*(the post .* appeared first on .*|continue reading.*|read more.*|\[…\]|\[\.\.\.\])$`)
	scriptLike = regexp.MustCompile(`(?is)<(script|style)[^>]*>.*?</(script|style)>`)
)

// Plain turns HTML (or escaped HTML) into one line of text.
func Plain(s string) string {
	s = html.UnescapeString(s)
	s = scriptLike.ReplaceAllString(s, " ")
	s = tagRE.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	return strings.Join(strings.Fields(s), " ")
}

func teaser(s string) string {
	t := boilerRE.ReplaceAllString(Plain(s), "")
	if r := []rune(t); len(r) > 600 {
		t = string(r[:600]) + "…"
	}
	return strings.TrimSpace(t)
}
