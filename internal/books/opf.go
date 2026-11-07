package books

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// FileDetails is what a book's own file says about itself (its OPF), to offer
// when a person corrects a book's details.
type FileDetails struct {
	Title          string `json:"title,omitempty"`
	Author         string `json:"author,omitempty"`
	SeriesName     string `json:"seriesName,omitempty"`
	SeriesPosition string `json:"seriesPosition,omitempty"`
	Year           int    `json:"year,omitempty"`
	Description    string `json:"description,omitempty"`
	ISBN           string `json:"isbn,omitempty"`
	Language       string `json:"language,omitempty"`
	Publisher      string `json:"publisher,omitempty"`
}

// ErrNoFileDetails is returned for a file that isn't an EPUB with readable metadata.
var ErrNoFileDetails = errors.New("the file has no details Usherr can read")

type opfText struct {
	Value  string `xml:",chardata"`
	Role   string `xml:"role,attr"`
	Scheme string `xml:"scheme,attr"`
}

type opfFullMeta struct {
	Name     string `xml:"name,attr"`
	Content  string `xml:"content,attr"`
	Property string `xml:"property,attr"`
	Refines  string `xml:"refines,attr"`
	ID       string `xml:"id,attr"`
	Value    string `xml:",chardata"`
}

type opfFull struct {
	Metadata struct {
		Titles      []opfText     `xml:"title"`
		Creators    []opfText     `xml:"creator"`
		Dates       []opfText     `xml:"date"`
		Description []opfText     `xml:"description"`
		Identifiers []opfText     `xml:"identifier"`
		Languages   []opfText     `xml:"language"`
		Publishers  []opfText     `xml:"publisher"`
		Meta        []opfFullMeta `xml:"meta"`
	} `xml:"metadata"`
	Manifest []opfItem `xml:"manifest>item"`
	Spine    []struct {
		IDRef string `xml:"idref,attr"`
	} `xml:"spine>itemref"`
}

// readOPF reads a book's package file and says where it is.
func readOPF(zr *zip.Reader) (opfFull, string, error) {
	var pkg opfFull
	cont, err := readZipFile(zr, "META-INF/container.xml", 1<<20)
	if err != nil {
		return pkg, "", fmt.Errorf("read the book's container: %w", err)
	}
	var c containerXML
	if err := xml.Unmarshal(cont, &c); err != nil || len(c.Rootfiles) == 0 {
		return pkg, "", ErrNoFileDetails
	}
	opfPath := c.Rootfiles[0].FullPath
	data, err := readZipFile(zr, opfPath, 8<<20)
	if err != nil {
		return pkg, "", fmt.Errorf("read the book's package: %w", err)
	}
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	dec.Entity = xml.HTMLEntity
	if err := dec.Decode(&pkg); err != nil {
		return pkg, "", ErrNoFileDetails
	}
	return pkg, opfPath, nil
}

var (
	markupTag  = regexp.MustCompile(`<[^>]*>`)
	yearInDate = regexp.MustCompile(`^\s*(\d{4})`)
)

// plainText is a description without its HTML. Descriptions are often escaped
// HTML inside the XML, so that is undone first.
func plainText(s string) string {
	s = markupTag.ReplaceAllString(html.UnescapeString(s), " ")
	return strings.Join(strings.Fields(html.UnescapeString(s)), " ")
}

func firstText(list []opfText) string {
	for _, t := range list {
		if v := strings.TrimSpace(t.Value); v != "" {
			return v
		}
	}
	return ""
}

// ReadFileDetails reads what an EPUB says about itself.
func ReadFileDetails(file string) (FileDetails, error) {
	zr, err := zip.OpenReader(file)
	if err != nil {
		return FileDetails{}, ErrNoFileDetails
	}
	defer zr.Close()
	pkg, _, err := readOPF(&zr.Reader)
	if err != nil {
		return FileDetails{}, err
	}
	return detailsOf(pkg), nil
}

func detailsOf(pkg opfFull) FileDetails {
	m := pkg.Metadata
	d := FileDetails{
		Title:       firstText(m.Titles),
		Description: plainText(firstText(m.Description)),
		Language:    firstText(m.Languages),
		Publisher:   firstText(m.Publishers),
	}
	if strings.EqualFold(d.Title, "unknown") {
		d.Title = ""
	}
	// The writers: anyone marked as the author, and anyone not marked as something else.
	var authors []string
	for _, c := range m.Creators {
		v := strings.TrimSpace(c.Value)
		if v == "" || strings.EqualFold(v, "unknown") || (c.Role != "" && !strings.EqualFold(c.Role, "aut")) {
			continue
		}
		authors = append(authors, v)
	}
	d.Author = strings.Join(authors, ", ")
	for _, t := range m.Dates {
		// Below 1000 is how Calibre writes "no date" (0101-01-01).
		if g := yearInDate.FindStringSubmatch(t.Value); g != nil {
			if y, _ := strconv.Atoi(g[1]); y >= 1000 {
				d.Year = y
				break
			}
		}
	}
	for _, t := range m.Identifiers {
		if isbn := isbnOf(t); isbn != "" {
			d.ISBN = isbn
			break
		}
	}
	d.SeriesName, d.SeriesPosition = seriesOf(m.Meta)
	return d
}

// isbnOf reads an identifier that is an ISBN, by its scheme, "urn:isbn:" or,
// for 13 digits starting with 978 or 979, its shape.
func isbnOf(t opfText) string {
	v := strings.TrimSpace(t.Value)
	low := strings.ToLower(v)
	marked := strings.EqualFold(t.Scheme, "isbn") || strings.HasPrefix(low, "urn:isbn:") || strings.HasPrefix(low, "isbn:")
	v = v[strings.LastIndex(v, ":")+1:]
	v = strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(v))
	if len(v) != 10 && len(v) != 13 {
		return ""
	}
	for i, c := range v {
		if (c < '0' || c > '9') && !(c == 'X' && i == 9 && len(v) == 10) {
			return ""
		}
	}
	if !marked && (len(v) == 10 || (!strings.HasPrefix(v, "978") && !strings.HasPrefix(v, "979"))) {
		return ""
	}
	return v
}

// seriesOf reads the series: Calibre's tags, or EPUB 3's belongs-to-collection.
func seriesOf(meta []opfFullMeta) (name, position string) {
	for _, x := range meta {
		switch strings.ToLower(x.Name) {
		case "calibre:series":
			name = strings.TrimSpace(x.Content)
		case "calibre:series_index":
			position = tidyPosition(x.Content)
		}
	}
	if name != "" {
		return name, position
	}
	for _, x := range meta {
		if x.Property != "belongs-to-collection" || strings.TrimSpace(x.Value) == "" {
			continue
		}
		for _, y := range meta {
			if y.Property == "group-position" && x.ID != "" && y.Refines == "#"+x.ID {
				position = tidyPosition(y.Value)
			}
		}
		return strings.TrimSpace(x.Value), position
	}
	return "", ""
}

// tidyPosition writes Calibre's "2.0" as "2".
func tidyPosition(s string) string {
	s = strings.TrimSpace(s)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return s
}

// EPUBSpineSizes is the size in bytes of each item in the book's reading order.
func EPUBSpineSizes(file string) ([]int64, error) {
	zr, err := zip.OpenReader(file)
	if err != nil {
		return nil, ErrNoFileDetails
	}
	defer zr.Close()
	pkg, opfPath, err := readOPF(&zr.Reader)
	if err != nil {
		return nil, err
	}
	hrefs := map[string]string{}
	for _, it := range pkg.Manifest {
		hrefs[it.ID] = it.Href
	}
	sizes := map[string]int64{}
	for _, f := range zr.File {
		sizes[f.Name] = int64(f.UncompressedSize64)
	}
	out := make([]int64, 0, len(pkg.Spine))
	for _, ref := range pkg.Spine {
		href := hrefs[ref.IDRef]
		if u, err := url.PathUnescape(href); err == nil {
			href = u
		}
		out = append(out, sizes[path.Clean(path.Join(path.Dir(opfPath), href))])
	}
	return out, nil
}
