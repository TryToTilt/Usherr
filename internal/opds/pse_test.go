package opds

import (
	"encoding/json"
	"encoding/xml"
	"strings"
	"testing"
)

// A page-streaming link carries pse:count, and pse:lastRead with its date
// once the reader has a place; the feed declares the pse namespace. OPDS 2.0
// has no such link, so the JSON leaves it out.
func TestPageStreamingLink(t *testing.T) {
	f := Feed{ID: "urn:x", Title: "Comics", Updated: day, Kind: Acquisition, Entries: []Entry{{
		ID: "urn:mediarium:comic:4", Title: "Night Owls #1", Updated: day,
		Links: []Link{
			{Rel: RelAcquisition, Href: "/opds/comics/issues/4/download", Type: "application/vnd.comicbook+zip"},
			{Rel: RelPSEStream, Href: "/opds/comics/issues/4/pages/{pageNumber}?width={maxWidth}", Type: "image/jpeg", PSECount: 22, PSELastRead: 5, LastReadAt: day},
			{Rel: RelPSEStream, Href: "/other/{pageNumber}", Type: "image/jpeg", PSECount: 3},
		},
	}}}
	atom := f.Atom()
	var parsed struct {
		Entries []struct {
			Links []struct {
				Rel      string `xml:"rel,attr"`
				Href     string `xml:"href,attr"`
				Count    string `xml:"http://vaemendis.net/opds-pse/ns count,attr"`
				LastRead string `xml:"http://vaemendis.net/opds-pse/ns lastRead,attr"`
				Date     string `xml:"http://vaemendis.net/opds-pse/ns lastReadDate,attr"`
			} `xml:"link"`
		} `xml:"entry"`
	}
	if err := xml.Unmarshal(atom, &parsed); err != nil {
		t.Fatalf("not XML: %v\n%s", err, atom)
	}
	links := parsed.Entries[0].Links
	if len(links) != 3 {
		t.Fatalf("links: %+v", links)
	}
	if l := links[1]; l.Rel != RelPSEStream || l.Count != "22" || l.LastRead != "5" || l.Date != "2026-10-09T12:30:00Z" || !strings.Contains(l.Href, "{pageNumber}") {
		t.Fatalf("stream link: %+v", l)
	}
	if l := links[2]; l.Count != "3" || l.LastRead != "" || l.Date != "" {
		t.Fatalf("unread stream link: %+v", l)
	}
	if links[0].Count != "" {
		t.Fatal("an acquisition link has a page count")
	}

	data, err := f.JSON()
	if err != nil {
		t.Fatal(err)
	}
	var j map[string]any
	if err := json.Unmarshal(data, &j); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "opds-pse") || strings.Contains(string(data), "{pageNumber}") {
		t.Fatalf("OPDS 2.0 has a page stream: %s", data)
	}
}
