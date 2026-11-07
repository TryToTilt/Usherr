package ebookconv

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"
)

// FixForKindle mends the things Amazon's Send to Kindle is known to refuse an
// EPUB for, in a copy: pages without an XML declaration, a missing language, a
// picture with no source, and contents links that point at a page's own body
// tag. The behaviour follows what Calibre-Web-Automated's EPUB fixer lists; no
// code is copied. data comes back as it is when nothing needed mending.
// A book locked with DRM answers ErrDRM.
func FixForKindle(data []byte) (out []byte, fixed []string, err error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, nil, fmt.Errorf("read the book: %w", err)
	}
	const maxText = 32 << 20
	texts := map[string][]byte{}
	bodyIDs := map[string]string{} // page's file name -> the id on its body tag
	for _, f := range zr.File {
		name := strings.ToLower(f.Name)
		switch {
		case name == "meta-inf/encryption.xml":
			enc, err := readEntry(f, 8<<20)
			if err != nil {
				return nil, nil, err
			}
			if lockedByDRM(enc) {
				return nil, nil, ErrDRM
			}
		case isPage(name), strings.HasSuffix(name, ".opf"), strings.HasSuffix(name, ".ncx"):
			if f.UncompressedSize64 > maxText {
				continue
			}
			text, err := readEntry(f, maxText)
			if err != nil {
				return nil, nil, err
			}
			texts[f.Name] = text
			if isPage(name) {
				if m := bodyID.FindSubmatch(text); m != nil {
					bodyIDs[path.Base(f.Name)] = string(m[1])
				}
			}
		}
	}

	notes := map[string]bool{}
	changed := map[string][]byte{}
	for name, text := range texts {
		low := strings.ToLower(name)
		next := text
		switch {
		case strings.HasSuffix(low, ".opf"):
			var ok bool
			if next, ok = addLanguage(next); ok {
				notes["set a language"] = true
			}
		case isPage(low):
			if !bytes.HasPrefix(bytes.TrimLeft(bytes.TrimPrefix(next, []byte("\xef\xbb\xbf")), " \t\r\n"), []byte("<?xml")) {
				next = append([]byte(`<?xml version="1.0" encoding="utf-8"?>`+"\n"), next...)
				notes["added XML declarations"] = true
			}
			if strays := dropStrayImages(next); !bytes.Equal(strays, next) {
				next = strays
				notes["removed pictures with no source"] = true
			}
		}
		// Contents (the NCX, and an EPUB 3 nav page) must not link to a body tag's id.
		if strings.HasSuffix(low, ".ncx") || isPage(low) {
			for page, id := range bodyIDs {
				for _, q := range []string{`"`, `'`} {
					from, to := []byte(page+"#"+id+q), []byte(page+q)
					if bytes.Contains(next, from) {
						next = bytes.ReplaceAll(next, from, to)
						notes["fixed contents links"] = true
					}
				}
			}
		}
		if !bytes.Equal(next, text) {
			changed[name] = next
		}
	}
	if len(changed) == 0 {
		return data, nil, nil
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range zr.File {
		text, ok := changed[f.Name]
		if !ok {
			// As it is, so "mimetype" stays first and uncompressed.
			if err := zw.Copy(f); err != nil {
				return nil, nil, fmt.Errorf("copy %s: %w", f.Name, err)
			}
			continue
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: f.Name, Method: zip.Deflate, Modified: f.Modified})
		if err != nil {
			return nil, nil, fmt.Errorf("write %s: %w", f.Name, err)
		}
		if _, err := w.Write(text); err != nil {
			return nil, nil, fmt.Errorf("write %s: %w", f.Name, err)
		}
	}
	if err := zw.Close(); err != nil {
		return nil, nil, fmt.Errorf("finish the book: %w", err)
	}
	for _, n := range []string{"added XML declarations", "set a language", "removed pictures with no source", "fixed contents links"} {
		if notes[n] {
			fixed = append(fixed, n)
		}
	}
	return buf.Bytes(), fixed, nil
}

var (
	bodyID       = regexp.MustCompile(`(?is)<body\b[^>]*?\bid\s*=\s*["']([^"']+)["']`)
	languageTag  = regexp.MustCompile(`(?is)<((?:\w+:)?language)\b[^>]*>(.*?)</(?:\w+:)?language>`)
	metadataEnd  = regexp.MustCompile(`(?i)</(?:\w+:)?metadata>`)
	imageTag     = regexp.MustCompile(`(?is)<img\b[^>]*?>`)
	imageWithSrc = regexp.MustCompile(`(?is)\bsrc\s*=\s*("[^"]+"|'[^']+')`)
)

func isPage(lowerName string) bool {
	return strings.HasSuffix(lowerName, ".xhtml") || strings.HasSuffix(lowerName, ".html") || strings.HasSuffix(lowerName, ".htm")
}

func readEntry(f *zip.File, limit int64) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", f.Name, err)
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, limit))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", f.Name, err)
	}
	return data, nil
}

// lockedByDRM reads META-INF/encryption.xml: hiding fonts is allowed, encrypting
// anything else is DRM.
func lockedByDRM(enc []byte) bool {
	var doc struct {
		Data []struct {
			Method struct {
				Algorithm string `xml:"Algorithm,attr"`
			} `xml:"EncryptionMethod"`
		} `xml:"EncryptedData"`
	}
	dec := xml.NewDecoder(bytes.NewReader(enc))
	dec.Strict = false
	if err := dec.Decode(&doc); err != nil {
		return true
	}
	for _, d := range doc.Data {
		switch d.Method.Algorithm {
		case "http://www.idpf.org/2008/embedding", "http://ns.adobe.com/pdf/enc#RC":
		default:
			return true
		}
	}
	return false
}

// addLanguage sets the book's language to English when it has none (or says
// "und"): Amazon refuses a book without one, and English is what it can be
// told when nothing says otherwise.
func addLanguage(opf []byte) ([]byte, bool) {
	if m := languageTag.FindSubmatchIndex(opf); m != nil {
		v := strings.ToLower(strings.TrimSpace(string(opf[m[4]:m[5]])))
		if v != "" && v != "und" {
			return opf, false
		}
		out := append([]byte{}, opf[:m[4]]...)
		out = append(out, "en"...)
		return append(out, opf[m[5]:]...), true
	}
	end := metadataEnd.FindIndex(opf)
	if end == nil {
		return opf, false
	}
	tag := "<dc:language>en</dc:language>"
	if !bytes.Contains(opf, []byte("dc:")) {
		tag = "<language>en</language>"
	}
	out := append([]byte{}, opf[:end[0]]...)
	out = append(out, tag...)
	return append(out, opf[end[0]:]...), true
}

func dropStrayImages(page []byte) []byte {
	return imageTag.ReplaceAllFunc(page, func(tag []byte) []byte {
		if imageWithSrc.Match(tag) {
			return tag
		}
		return nil
	})
}
