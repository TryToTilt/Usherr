package api

// Usherr: Read-to-Me. An EPUB, read as chapters of plain text for a voice to
// speak. The spine gives the order, the table of contents (EPUB 3's nav page, or
// EPUB 2's NCX) the chapter titles; a file the table doesn't name is a part of
// the chapter before it. Paragraph breaks are kept, footnote markers, scripts,
// styles and navigation are dropped, and the copyright page, the table of
// contents and pages with next to nothing on them are left out.

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

const (
	maxEPUBPart   = 24 << 20 // the biggest file read out of an EPUB
	minChapterLen = 20       // words; a chapter with fewer is left out
)

type rtvChapter struct {
	Title  string
	Text   string
	Edited bool // the text is Read-to-Me's own (home_readtome_edit.go)
}

type rtvBook struct {
	Title    string
	Author   string
	Chapters []rtvChapter
}

// ---- the package file ----

type epubOPF struct {
	Metadata struct {
		Title   []string `xml:"title"`
		Creator []string `xml:"creator"`
	} `xml:"metadata"`
	Manifest []opfEntry `xml:"manifest>item"`
	Spine    struct {
		TOC   string `xml:"toc,attr"`
		Items []struct {
			IDRef  string `xml:"idref,attr"`
			Linear string `xml:"linear,attr"`
		} `xml:"itemref"`
	} `xml:"spine"`
	Guide []struct {
		Type string `xml:"type,attr"`
		Href string `xml:"href,attr"`
	} `xml:"guide>reference"`
}

type opfEntry struct {
	ID         string `xml:"id,attr"`
	Href       string `xml:"href,attr"`
	MediaType  string `xml:"media-type,attr"`
	Properties string `xml:"properties,attr"`
}

type epubNCX struct {
	Points []ncxPoint `xml:"navMap>navPoint"`
}

type ncxPoint struct {
	Label   string `xml:"navLabel>text"`
	Content struct {
		Src string `xml:"src,attr"`
	} `xml:"content"`
	Children []ncxPoint `xml:"navPoint"`
}

func flattenNCX(points []ncxPoint, out map[string]string) {
	for _, p := range points {
		if href, _, _ := strings.Cut(p.Content.Src, "#"); href != "" {
			if _, seen := out[href]; !seen {
				out[href] = strings.TrimSpace(p.Label)
			}
		}
		flattenNCX(p.Children, out)
	}
}

func epubFile(zr *zip.Reader, name string) ([]byte, error) {
	name = strings.TrimPrefix(path.Clean("/"+name), "/")
	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		data, err := io.ReadAll(io.LimitReader(rc, maxEPUBPart+1))
		if err != nil {
			return nil, err
		}
		if len(data) > maxEPUBPart {
			return nil, fmt.Errorf("%s is too big", name)
		}
		return data, nil
	}
	return nil, fmt.Errorf("%s isn't in the book", name)
}

// readEPUBBook reads an EPUB into chapters of text.
func readEPUBBook(file string) (rtvBook, error) {
	zr, err := zip.OpenReader(file)
	if err != nil {
		return rtvBook{}, fmt.Errorf("the ebook couldn't be opened: %w", err)
	}
	defer zr.Close()
	return readEPUBZip(&zr.Reader)
}

func readEPUBZip(zr *zip.Reader) (rtvBook, error) {
	cont, err := epubFile(zr, "META-INF/container.xml")
	if err != nil {
		return rtvBook{}, errors.New("this isn't an EPUB (no container.xml)")
	}
	var c struct {
		Rootfiles []struct {
			FullPath string `xml:"full-path,attr"`
		} `xml:"rootfiles>rootfile"`
	}
	if xml.Unmarshal(cont, &c) != nil || len(c.Rootfiles) == 0 || c.Rootfiles[0].FullPath == "" {
		return rtvBook{}, errors.New("this EPUB has no package file")
	}
	opfPath := c.Rootfiles[0].FullPath
	opfData, err := epubFile(zr, opfPath)
	if err != nil {
		return rtvBook{}, err
	}
	var opf epubOPF
	if err := xml.Unmarshal(opfData, &opf); err != nil {
		return rtvBook{}, errors.New("this EPUB's package file couldn't be read")
	}
	base := path.Dir(opfPath)
	resolve := func(href string) string {
		href, _, _ = strings.Cut(href, "#")
		if u, err := url.PathUnescape(href); err == nil {
			href = u
		}
		return path.Clean(path.Join(base, href))
	}
	book := rtvBook{}
	if len(opf.Metadata.Title) > 0 {
		book.Title = strings.TrimSpace(opf.Metadata.Title[0])
	}
	if len(opf.Metadata.Creator) > 0 {
		book.Author = strings.TrimSpace(opf.Metadata.Creator[0])
	}
	byID := map[string]opfEntry{}
	var navHref string
	for _, it := range opf.Manifest {
		byID[it.ID] = it
		if strings.Contains(" "+it.Properties+" ", " nav ") {
			navHref = it.Href
		}
	}

	// The table of contents: chapter title by file.
	titles := map[string]string{}
	if navHref != "" {
		if data, err := epubFile(zr, resolve(navHref)); err == nil {
			navBase := path.Dir(resolve(navHref))
			for href, title := range navTitles(data) {
				titles[path.Clean(path.Join(navBase, mustUnescape(href)))] = title
			}
		}
	}
	if len(titles) == 0 {
		ncxID := opf.Spine.TOC
		for _, it := range opf.Manifest {
			if ncxID != "" && it.ID == ncxID || it.MediaType == "application/x-dtbncx+xml" {
				if data, err := epubFile(zr, resolve(it.Href)); err == nil {
					var ncx epubNCX
					if xml.Unmarshal(data, &ncx) == nil {
						raw := map[string]string{}
						flattenNCX(ncx.Points, raw)
						ncxBase := path.Dir(resolve(it.Href))
						for href, title := range raw {
							titles[path.Clean(path.Join(ncxBase, mustUnescape(href)))] = title
						}
					}
				}
				break
			}
		}
	}
	skipRef := map[string]bool{}
	for _, g := range opf.Guide {
		switch strings.ToLower(g.Type) {
		case "cover", "title-page", "copyright-page", "toc", "colophon":
			skipRef[resolve(g.Href)] = true
		}
	}

	var cur *rtvChapter
	flush := func() {
		if cur != nil && len(strings.Fields(cur.Text)) >= minChapterLen {
			book.Chapters = append(book.Chapters, *cur)
		}
		cur = nil
	}
	for _, ref := range opf.Spine.Items {
		it, ok := byID[ref.IDRef]
		if !ok || !strings.Contains(it.MediaType, "html") {
			continue
		}
		name := resolve(it.Href)
		data, err := epubFile(zr, name)
		if err != nil {
			continue
		}
		text, heading, kinds := epubText(data)
		title, named := titles[name]
		if skipRef[name] || skippedPage(title, name, it.ID, kinds) {
			continue
		}
		if named || cur == nil {
			flush()
			if title == "" {
				title = heading
			}
			cur = &rtvChapter{Title: title}
		}
		if cur.Text != "" && text != "" {
			cur.Text += "\n\n"
		}
		cur.Text += text
	}
	flush()
	for i := range book.Chapters {
		if book.Chapters[i].Title == "" {
			book.Chapters[i].Title = fmt.Sprintf("Part %d", i+1)
		}
	}
	if len(book.Chapters) == 0 {
		return rtvBook{}, errors.New("there is no text to read in this EPUB")
	}
	return book, nil
}

func mustUnescape(s string) string {
	if u, err := url.PathUnescape(s); err == nil {
		return u
	}
	return s
}

var skipTitle = regexp.MustCompile(`(?i)^\s*(copyright( page| notice)?|table of contents|contents|title page|half[- ]?title|cover|also by .*|other books by .*|about the author|acknowledg(e)?ments?|credits|colophon|imprint|permissions|newsletter|get a free .*|sign up .*|praise for .*|reviews?)\s*$`)
var skipName = regexp.MustCompile(`(?i)(^|[/_.-])(cover|copyright|copyrights|toc|titlepage|title-page|halftitle|colophon|nav|imprint)([_.-]|\d|$)`)

// skippedPage says whether a spine file is a page nobody wants read aloud: the
// cover, the copyright page, the table of contents.
func skippedPage(title, file, id string, kinds map[string]bool) bool {
	if kinds["toc"] || kinds["copyright-page"] || kinds["cover"] || kinds["titlepage"] || kinds["colophon"] || kinds["landmarks"] {
		return true
	}
	if title != "" && skipTitle.MatchString(title) {
		return true
	}
	base := path.Base(file)
	return skipName.MatchString(base) || skipName.MatchString(id)
}

// ---- the table of contents page ----

func navTitles(data []byte) map[string]string {
	out := map[string]string{}
	doc, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		return out
	}
	var inToc func(n *html.Node, in bool)
	inToc = func(n *html.Node, in bool) {
		if n.Type == html.ElementNode && n.Data == "nav" {
			t := attr(n, "epub:type") + " " + attr(n, "type") + " " + attr(n, "role")
			in = strings.Contains(t, "toc") || strings.Contains(t, "doc-toc")
		}
		if in && n.Type == html.ElementNode && n.Data == "a" {
			if href := attr(n, "href"); href != "" {
				h, _, _ := strings.Cut(href, "#")
				if _, seen := out[h]; !seen && h != "" {
					out[h] = strings.Join(strings.Fields(nodeText(n)), " ")
				}
			}
		}
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			inToc(ch, in)
		}
	}
	inToc(doc, false)
	return out
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key || strings.HasSuffix(a.Key, ":"+key) || (a.Namespace != "" && a.Key == key) {
			return a.Val
		}
	}
	return ""
}

func nodeText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(n)
	return b.String()
}

// ---- a chapter's text ----

var blockTags = map[string]bool{
	"p": true, "div": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"li": true, "blockquote": true, "tr": true, "section": true, "article": true, "pre": true, "figcaption": true,
	"dt": true, "dd": true, "ul": true, "ol": true, "table": true, "hr": true,
}

// epubText turns an XHTML page into plain text with a blank line between
// paragraphs. heading is the first heading on the page; kinds are the page's
// epub:type words (toc, copyright-page...).
func epubText(data []byte) (text, heading string, kinds map[string]bool) {
	kinds = map[string]bool{}
	doc, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		return "", "", kinds
	}
	var b strings.Builder
	paragraph := func() {
		s := b.String()
		if s != "" && !strings.HasSuffix(s, "\n\n") {
			b.WriteString("\n\n")
		}
	}
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		switch n.Type {
		case html.TextNode:
			b.WriteString(n.Data)
			return
		case html.ElementNode:
			switch n.Data {
			case "script", "style", "head", "nav", "svg", "math", "noscript", "audio", "video", "img", "image":
				return
			case "body", "section", "div":
				for _, k := range strings.Fields(attr(n, "type")) {
					kinds[k] = true
				}
			case "br":
				b.WriteString("\n")
				return
			case "hr":
				paragraph()
				b.WriteString(sceneMark) // a scene break
				paragraph()
				return
			case "sup", "sub":
				// a footnote marker: a number, or a symbol, inside a note link
				if len(strings.TrimSpace(nodeText(n))) <= 4 {
					return
				}
			case "a":
				if t := attr(n, "type"); strings.Contains(t, "noteref") || strings.Contains(t, "footnote") {
					return
				}
			}
			if attr(n, "type") != "" && (n.Data == "aside" || n.Data == "div") && strings.Contains(attr(n, "type"), "footnote") {
				return
			}
			if heading == "" && len(n.Data) == 2 && n.Data[0] == 'h' && n.Data[1] >= '1' && n.Data[1] <= '6' {
				heading = strings.Join(strings.Fields(nodeText(n)), " ")
			}
			block := blockTags[n.Data]
			if block {
				paragraph()
			}
			for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
				walk(ch)
			}
			if block {
				paragraph()
			}
			return
		}
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(doc)
	return tidyText(b.String()), heading, kinds
}

var spaceRun = regexp.MustCompile(`[ \t\r\f\v\x{a0}]+`)

// tidyText squeezes white space inside paragraphs and keeps one blank line
// between them.
func tidyText(s string) string {
	var paras []string
	for _, p := range strings.Split(strings.ReplaceAll(s, "\r", ""), "\n\n") {
		lines := strings.Split(p, "\n")
		for i := range lines {
			lines[i] = strings.TrimSpace(spaceRun.ReplaceAllString(lines[i], " "))
		}
		p = strings.TrimSpace(strings.Join(lines, " "))
		if p != "" {
			paras = append(paras, p)
		}
	}
	return strings.Join(paras, "\n\n")
}

// firstWords is the first n words of a text, with its paragraph breaks, ending
// at a sentence end when one is close.
func firstWords(text string, n int) string {
	// Where each word ends, in the text's own bytes.
	var ends []int
	inWord := false
	for i, r := range text {
		space := r == ' ' || r == '\n' || r == '\t' || r == '\r'
		if inWord && space {
			ends = append(ends, i)
		}
		inWord = !space
	}
	if inWord {
		ends = append(ends, len(text))
	}
	if len(ends) <= n {
		return strings.TrimSpace(text)
	}
	// Run on to the end of the sentence when it is within 40 more words.
	for k := n; k < len(ends) && k < n+40; k++ {
		if w := strings.TrimSpace(text[ends[k-1]:ends[k]]); strings.HasSuffix(w, ".") || strings.HasSuffix(w, "!") || strings.HasSuffix(w, "?") || strings.HasSuffix(w, "\"") {
			return strings.TrimSpace(text[:ends[k]])
		}
	}
	return strings.TrimSpace(text[:ends[n-1]])
}
