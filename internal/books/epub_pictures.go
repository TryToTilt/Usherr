package books

// An EPUB can be a volume of pictures rather than a book of words: a manga sold as
// an ebook has one image a page. These read one as pages, in the book's own order.

import (
	"archive/zip"
	"io"
	"net/url"
	"path"
	"regexp"
	"strings"
)

// EPUB pictures: at least this many pages, with about this little text on each,
// are a volume of pictures. A novel with a few plates is neither.
const (
	epubPicturesLeast = 10
	epubPageTextMost  = 400 // characters of text on an average page
	epubPageReadLimit = 1 << 20
	epubPicturesShare = 0.6 // of the book's items that hold a picture
)

var (
	epubImageRef = regexp.MustCompile(`(?is)<(?:img|image)\b[^>]*?(?:\bsrc|xlink:href|\bhref)\s*=\s*["']([^"']+)["']`)
	epubTag      = regexp.MustCompile(`(?s)<[^>]*>`)
	epubHead     = regexp.MustCompile(`(?is)<head\b.*?</head>`)
)

// EPUBPictures lists the image of each page of an EPUB, by its name in the file and
// in reading order. pictures says whether it's a volume of pictures at all; a novel
// comes back with false.
func EPUBPictures(file string) (images []string, pictures bool, err error) {
	zr, err := zip.OpenReader(file)
	if err != nil {
		return nil, false, ErrNoFileDetails
	}
	defer zr.Close()
	pkg, opfPath, err := readOPF(&zr.Reader)
	if err != nil {
		return nil, false, err
	}
	hrefs := map[string]string{}
	for _, it := range pkg.Manifest {
		hrefs[it.ID] = it.Href
	}
	byName := map[string]*zip.File{}
	for _, f := range zr.File {
		byName[f.Name] = f
	}
	inZip := func(from, href string) string {
		if u, err := url.PathUnescape(href); err == nil {
			href = u
		}
		href, _, _ = strings.Cut(href, "#")
		return path.Clean(path.Join(path.Dir(from), href))
	}
	seen := map[string]bool{}
	items, text := 0, 0
	for _, ref := range pkg.Spine {
		name := inZip(opfPath, hrefs[ref.IDRef])
		f := byName[name]
		if f == nil {
			continue
		}
		items++
		rc, err := f.Open()
		if err != nil {
			continue
		}
		b, err := io.ReadAll(io.LimitReader(rc, epubPageReadLimit))
		rc.Close()
		if err != nil {
			continue
		}
		page := string(b)
		for _, m := range epubImageRef.FindAllStringSubmatch(page, -1) {
			img := inZip(name, m[1])
			if _, ok := byName[img]; ok && !seen[img] {
				seen[img] = true
				images = append(images, img)
			}
		}
		text += len(strings.TrimSpace(epubTag.ReplaceAllString(epubHead.ReplaceAllString(page, ""), "")))
	}
	if items == 0 {
		return images, false, nil
	}
	pictures = len(images) >= epubPicturesLeast && text/items <= epubPageTextMost && float64(len(images)) >= float64(items)*epubPicturesShare
	return images, pictures, nil
}
