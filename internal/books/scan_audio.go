package books

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// What an audiobook's folder says about itself, following Audiobookshelf's
// folder conventions (its server/utils/scandir.js and scanner/, GPL-3.0; the
// rules are re-written here, see docs/home/CREDITS.md):
//
//	<Author>/<Series>/<Title>
//	Vol 2 - Title {Narrator} [B0015T963C]
//
// and the files beside the audio: metadata.json, an .opf, desc.txt and
// reader.txt. Nearer the book wins: metadata.json, then the .opf, then the text
// files, then the folder name. Tags inside the audio only fill what is left
// (see ApplyAudioTags). Files are only read, never changed.

const (
	maxSideFile    = 2 << 20 // the most read of one file beside the audio
	maxDescription = 20000   // characters
)

var (
	folderASIN     = regexp.MustCompile(`(?:^|\s)\[([A-Z0-9]{10})\](?:\s|$)`)
	folderNarrator = regexp.MustCompile(`\s*\{([^{}]*)\}`)
	// One " - " part of a folder name that gives the series number: "Vol 2",
	// "Volume 12. Title", "Book 3 Title", "6. Title", "0.5". A number with words
	// after it and neither a label nor a dot is a title ("101 Dalmatians").
	folderSequence = regexp.MustCompile(`(?i)^(vol\.?\s*|volume\s*|book\s*)?(\d{0,3}(?:\.\d{1,2})?)(\.?)(?: (.*))?$`)
	// The same at the end of a title in a series: "The Name of It, Book 2".
	folderTailSequence = regexp.MustCompile(`(?i)[\s,(\[]+(?:vol\.?|volume|book)\s*(\d{1,3}(?:\.\d{1,2})?)[)\]]?$`)
	nameSeparators     = regexp.MustCompile(`\s*(?:;|&|\band\b)\s*`)
	seriesWithNumber   = regexp.MustCompile(`^(.*\S)\s*#\s*(\d+(?:\.\d+)?)\s*$`)
	yearDigits         = regexp.MustCompile(`\b((?:1[5-9]|20)\d\d)\b`)
	htmlTags           = regexp.MustCompile(`<[^>]*>`)
	sameLineSpaces     = regexp.MustCompile(`[ \t]+`)
)

// audioFolderName is what one folder's name says.
type audioFolderName struct {
	rest      string // the name without what was read from it
	narrators string
	asin      string
	sequence  string
}

// parseAudioFolderName reads the ASIN, narrator and series number from a
// book's folder name. inSeries says the folder is inside a series folder;
// outside one only a labelled number ("Book 2") counts.
func parseAudioFolderName(name string, inSeries bool) audioFolderName {
	out := audioFolderName{rest: strings.ReplaceAll(name, "_", " ")}
	if m := folderASIN.FindStringSubmatch(out.rest); m != nil {
		out.asin = m[1]
		out.rest = strings.TrimSpace(strings.Replace(out.rest, strings.TrimSpace(m[0]), "", 1))
	}
	if m := folderNarrator.FindStringSubmatch(out.rest); m != nil {
		out.narrators = cleanNames(m[1])
		out.rest = strings.TrimSpace(strings.Replace(out.rest, m[0], "", 1))
	}
	out.rest = strings.TrimSpace(spaces.ReplaceAllString(out.rest, " "))
	parts := strings.Split(out.rest, " - ")
	for i, p := range parts {
		m := folderSequence.FindStringSubmatch(strings.TrimSpace(p))
		if m == nil || !strings.ContainsAny(m[2], "0123456789") {
			continue
		}
		label, number, dot, words := m[1], m[2], m[3], m[4]
		if words != "" && label == "" && dot == "" {
			continue
		}
		if !inSeries && label == "" {
			continue
		}
		out.sequence = cleanSequence(number)
		if words != "" {
			parts[i] = words
		} else {
			parts = append(parts[:i], parts[i+1:]...)
		}
		out.rest = strings.Join(parts, " - ")
		return out
	}
	if inSeries {
		if m := folderTailSequence.FindStringSubmatch(out.rest); m != nil {
			out.sequence = cleanSequence(m[1])
			out.rest = strings.TrimSpace(strings.TrimRight(out.rest[:len(out.rest)-len(m[0])], " -–,"))
		}
	}
	return out
}

// cleanSequence writes a series number the way a person does: "02" is "2".
func cleanSequence(s string) string {
	if n, err := strconv.ParseFloat(s, 64); err == nil {
		return strconv.FormatFloat(n, 'f', -1, 64)
	}
	return s
}

// cleanNames writes a list of people as "One Name, Other Name".
func cleanNames(s string) string {
	var names []string
	for _, n := range nameSeparators.Split(strings.TrimSpace(s), -1) {
		if n = strings.TrimSpace(strings.Trim(n, ",")); n != "" {
			names = append(names, n)
		}
	}
	return strings.Join(names, ", ")
}

// guessAudiobook reads a book from its folders under the library root. The
// nearest three count: <Author>/<Series>/<Title>.
func guessAudiobook(parts []string, loose bool) LocalBook {
	n := len(parts)
	if n == 0 {
		return LocalBook{}
	}
	if n > 3 {
		parts, n = parts[n-3:], 3
	}
	inSeries := n == 3
	series := ""
	if inSeries {
		series, _ = cleanName(parts[1])
		inSeries = series != ""
	}
	info := parseAudioFolderName(parts[n-1], inSeries)
	rest := info.rest
	if t, _ := cleanName(rest); t == "" {
		// The folder is only "Vol 3": the name of the series has to do.
		rest = strings.TrimSpace(series + " " + parts[n-1])
	}
	cut := append(append([]string{}, parts[:n-1]...), rest)
	author, title, year := Guess(cut, loose)
	return LocalBook{Author: author, Title: title, Year: year, Series: series, Position: info.sequence, Narrators: info.narrators, ASIN: info.asin}
}

// sideFiles finds the files beside the audio that say something about the book.
func sideFiles(dir string) (metadata, opf, desc, reader string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && !hidden(e.Name()) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		p := filepath.Join(dir, name)
		switch low := strings.ToLower(name); {
		case low == "metadata.json":
			metadata = p
		case low == "desc.txt":
			desc = p
		case low == "reader.txt":
			reader = p
		case strings.HasSuffix(low, ".opf") && opf == "":
			opf = p
		}
	}
	return
}

// readSideFile reads at most maxSideFile bytes of a file as text.
func readSideFile(path string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxSideFile))
	if err != nil {
		return "", false
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	return strings.ToValidUTF8(string(data), ""), true
}

// cleanDescription makes a description plain text of a sensible length.
func cleanDescription(s string) string {
	s = html.UnescapeString(htmlTags.ReplaceAllString(s, " "))
	s = strings.TrimSpace(sameLineSpaces.ReplaceAllString(s, " "))
	if utf8.RuneCountInString(s) > maxDescription {
		s = strings.TrimSpace(string([]rune(s)[:maxDescription-1])) + "…"
	}
	return s
}

// bookWords is what one source says about a book; "" says nothing.
type bookWords struct {
	title, author, narrators, asin, description, series, position string
	year                                                          int
}

// over lets what w says replace what lb has.
func (w bookWords) over(lb *LocalBook) {
	set := func(to *string, v string) {
		if v = strings.TrimSpace(v); v != "" {
			*to = v
		}
	}
	set(&lb.Title, w.title)
	set(&lb.Author, w.author)
	set(&lb.Narrators, w.narrators)
	set(&lb.Description, w.description)
	if asinPattern(w.asin) {
		lb.ASIN = w.asin
	}
	if w.series != "" {
		if !strings.EqualFold(lb.Series, w.series) {
			lb.Position = "" // the folder's number was in another series
		}
		lb.Series = w.series
	}
	set(&lb.Position, w.position)
	if w.year > 0 {
		lb.Year = w.year
	}
}

func firstYear(s string) int {
	if m := yearDigits.FindStringSubmatch(s); m != nil {
		y, _ := strconv.Atoi(m[1])
		return y
	}
	return 0
}

// readSideFilesInto adds what the files beside the audio say to lb.
func readSideFilesInto(dir string, lb *LocalBook) {
	metadata, opf, desc, reader := sideFiles(dir)
	if s, ok := readSideFile(desc); ok {
		bookWords{description: cleanDescription(s)}.over(lb)
	}
	if s, ok := readSideFile(reader); ok {
		first, _, _ := strings.Cut(strings.ReplaceAll(strings.TrimSpace(s), "\r\n", "\n"), "\n") // only the first line, as Audiobookshelf reads it
		bookWords{narrators: cleanNames(first)}.over(lb)
	}
	if s, ok := readSideFile(opf); ok {
		parseOPF(s).over(lb)
	}
	if s, ok := readSideFile(metadata); ok {
		if w, err := parseAudioMetadataJSON(s); err == nil {
			w.over(lb)
		}
	}
}

// flexStrings reads a JSON list of strings, of objects with a name, or a single
// string, as the versions of metadata.json write them.
type flexStrings []string

func (f *flexStrings) UnmarshalJSON(data []byte) error {
	var one string
	if json.Unmarshal(data, &one) == nil {
		if one != "" {
			*f = flexStrings{one}
		}
		return nil
	}
	var list []json.RawMessage
	if err := json.Unmarshal(data, &list); err != nil {
		return nil // something else: says nothing
	}
	for _, raw := range list {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			*f = append(*f, s)
			continue
		}
		var o struct {
			Name     string `json:"name"`
			Sequence string `json:"sequence"`
		}
		if json.Unmarshal(raw, &o) == nil && o.Name != "" {
			if o.Sequence != "" {
				o.Name += " #" + o.Sequence
			}
			*f = append(*f, o.Name)
		}
	}
	return nil
}

// flexText reads a JSON string or number as text.
type flexText string

func (f *flexText) UnmarshalJSON(data []byte) error {
	var s string
	if json.Unmarshal(data, &s) == nil {
		*f = flexText(s)
		return nil
	}
	var n json.Number
	if json.Unmarshal(data, &n) == nil {
		*f = flexText(n.String())
	}
	return nil
}

// parseAudioMetadataJSON reads Audiobookshelf's metadata.json.
func parseAudioMetadataJSON(s string) (bookWords, error) {
	var j struct {
		Title         flexText    `json:"title"`
		Authors       flexStrings `json:"authors"`
		Narrators     flexStrings `json:"narrators"`
		Series        flexStrings `json:"series"`
		PublishedYear flexText    `json:"publishedYear"`
		PublishedDate flexText    `json:"publishedDate"`
		Description   flexText    `json:"description"`
		ASIN          flexText    `json:"asin"`
	}
	if err := json.Unmarshal([]byte(s), &j); err != nil {
		return bookWords{}, fmt.Errorf("metadata.json: %w", err)
	}
	w := bookWords{title: string(j.Title), asin: strings.TrimSpace(string(j.ASIN)), description: cleanDescription(string(j.Description)),
		narrators: strings.Join(j.Narrators, ", ")}
	if len(j.Authors) > 0 {
		w.author = j.Authors[0]
	}
	if len(j.Series) > 0 {
		w.series, w.position = splitSeries(j.Series[0])
	}
	if w.year = firstYear(string(j.PublishedYear)); w.year == 0 {
		w.year = firstYear(string(j.PublishedDate))
	}
	return w, nil
}

// splitSeries reads "Series Name #2".
func splitSeries(s string) (name, position string) {
	s = strings.TrimSpace(s)
	if m := seriesWithNumber.FindStringSubmatch(s); m != nil {
		return m[1], cleanSequence(m[2])
	}
	return s, ""
}

// parseOPF reads an .opf file (Calibre, Libation and others write them).
func parseOPF(s string) bookWords {
	d := xml.NewDecoder(strings.NewReader(s))
	d.Strict = false
	d.CharsetReader = func(_ string, in io.Reader) (io.Reader, error) { return in, nil }
	var w bookWords
	var firstCreator, date string
	var authors, narrators []string
	attr := func(e xml.StartElement, name string) string {
		for _, a := range e.Attr {
			if strings.EqualFold(a.Name.Local, name) {
				return strings.TrimSpace(a.Value)
			}
		}
		return ""
	}
	for {
		tok, err := d.Token()
		if err != nil {
			if !errors.Is(err, io.EOF) && w.title == "" && len(authors) == 0 {
				return bookWords{}
			}
			break
		}
		e, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		name := strings.ToLower(e.Name.Local)
		if name == "meta" {
			switch strings.ToLower(attr(e, "name")) {
			case "calibre:series":
				w.series = attr(e, "content")
			case "calibre:series_index":
				w.position = cleanSequence(attr(e, "content"))
			}
			continue
		}
		var text string
		switch name {
		case "title", "creator", "description", "date", "identifier":
			if d.DecodeElement(&text, &e) != nil {
				continue
			}
			text = strings.TrimSpace(text)
		default:
			continue
		}
		switch name {
		case "title":
			if w.title == "" {
				w.title = text
			}
		case "creator":
			switch strings.ToLower(attr(e, "role")) {
			case "aut":
				authors = append(authors, text)
			case "nrt":
				narrators = append(narrators, text)
			case "":
				if firstCreator == "" {
					firstCreator = text
				}
			}
		case "description":
			w.description = cleanDescription(text)
		case "date":
			if date == "" {
				date = text
			}
		case "identifier":
			if s := strings.ToUpper(attr(e, "scheme")); s == "ASIN" || s == "AUDIBLE_ASIN" {
				w.asin = text
			} else if u := strings.ToUpper(text); strings.HasPrefix(u, "ASIN:") {
				w.asin = strings.TrimSpace(text[5:])
			}
		}
	}
	switch {
	case len(authors) > 0:
		w.author = authors[0]
	case firstCreator != "":
		w.author = firstCreator
	}
	w.narrators = strings.Join(narrators, ", ")
	w.year = firstYear(date)
	return w
}

// tagNamed is the first of the named tags that has something in it.
func tagNamed(tags map[string]string, names ...string) string {
	for _, n := range names {
		if v := strings.TrimSpace(tags[n]); v != "" {
			return v
		}
	}
	return ""
}

// NeedsAudioTags reports whether a book's tags could add anything.
func (lb LocalBook) NeedsAudioTags() bool {
	return lb.FirstAudio != "" && !lb.TagsRead && (lb.Narrators == "" || lb.ASIN == "" || lb.Description == "" || lb.Series == "" || lb.Author == "" || lb.Year == 0)
}

// ApplyAudioTags fills what is still empty from the tags of the book's first
// audio file (names lower-case). The title is left alone unless the book is a
// loose file: a folder's name is usually better than a track's album tag.
func ApplyAudioTags(lb *LocalBook, tags map[string]string) {
	lb.TagsRead = true
	if lb.Loose {
		if t := tagNamed(tags, "album", "title"); t != "" {
			lb.Title = t
		}
	}
	if lb.Author == "" {
		lb.Author = tagNamed(tags, "artist", "album_artist", "albumartist")
	}
	if lb.Narrators == "" {
		lb.Narrators = cleanNames(tagNamed(tags, "narrator", "narrated_by", "composer"))
	}
	if lb.ASIN == "" {
		if a := tagNamed(tags, "asin", "audible_asin"); asinPattern(a) {
			lb.ASIN = a
		}
	}
	if lb.Description == "" {
		lb.Description = cleanDescription(tagNamed(tags, "description", "desc"))
		// A comment tag is often only who ripped it; a real blurb is longer.
		if c := cleanDescription(tagNamed(tags, "comment")); lb.Description == "" && utf8.RuneCountInString(c) >= 80 {
			lb.Description = c
		}
	}
	if lb.Year == 0 {
		lb.Year = firstYear(tagNamed(tags, "date", "year", "originalyear"))
	}
	if lb.Series == "" {
		name, number := splitSeries(tagNamed(tags, "series", "show", "mvnm", "grouping"))
		lb.Series = name
		if number == "" {
			part, _, _ := strings.Cut(tagNamed(tags, "series-part", "episode_id", "mvin", "part"), "/") // "3/12"
			number = cleanSequence(strings.TrimSpace(part))
		}
		if name != "" && lb.Position == "" {
			lb.Position = number
		}
	}
}

// FillFromScan gives a book what the scan read from its folder, only where the
// book has nothing: what a person set, or a look-up found, stays. Files stay
// where they are.
func (r *Repo) FillFromScan(id int64, lb LocalBook) error {
	_, err := r.db.Exec(`UPDATE books SET
			series_position = CASE WHEN series_position = '' AND ? <> '' AND (series_name = '' OR lower(series_name) = lower(?)) THEN ? ELSE series_position END,
			series_name     = CASE WHEN series_name = '' THEN ? ELSE series_name END,
			narrators       = CASE WHEN narrators = '' THEN ? ELSE narrators END,
			asin            = CASE WHEN asin = '' THEN ? ELSE asin END,
			description     = CASE WHEN description = '' THEN ? ELSE description END,
			year            = CASE WHEN year = 0 THEN ? ELSE year END
		WHERE id = ?`,
		lb.Series, lb.Series, lb.Position, lb.Series, lb.Narrators, lb.ASIN, lb.Description, lb.Year, id)
	if err != nil {
		return fmt.Errorf("fill in a book from its folder: %w", err)
	}
	return nil
}
