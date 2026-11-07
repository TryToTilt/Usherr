package manga

// ComicInfo.xml, so other readers (Komga, Kavita) know a file's series, its
// number, and that it reads right to left. Only files Usherr downloaded get one,
// and never when they already have one: files a person brought aren't touched.

import (
	"archive/zip"
	"encoding/xml"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// ComicInfo is the part of the format Usherr writes.
type ComicInfo struct {
	XMLName         xml.Name `xml:"ComicInfo"`
	Title           string   `xml:"Title,omitempty"`
	Series          string   `xml:"Series"`
	LocalizedSeries string   `xml:"LocalizedSeries,omitempty"`
	Number          string   `xml:"Number,omitempty"`
	Volume          string   `xml:"Volume,omitempty"`
	Notes           string   `xml:"Notes,omitempty"`
	Writer          string   `xml:"Writer,omitempty"`
	Translator      string   `xml:"Translator,omitempty"`
	Publisher       string   `xml:"Publisher,omitempty"`
	Web             string   `xml:"Web,omitempty"`
	LanguageISO     string   `xml:"LanguageISO,omitempty"`
	Manga           string   `xml:"Manga,omitempty"` // Yes, YesAndRightToLeft
	Format          string   `xml:"Format,omitempty"`
	ScanInformation string   `xml:"ScanInformation,omitempty"`
}

// InfoFor is the ComicInfo of a series' volume.
func InfoFor(s Series, number, class string) ComicInfo {
	info := ComicInfo{Series: s.Title, Number: numOf(number), Volume: numOf(number), Writer: s.Author, Publisher: s.Publisher, Web: s.SiteURL, Manga: "YesAndRightToLeft"}
	if s.Format != "manga" && s.Format != "oneshot" && s.Format != "" {
		info.Manga = "Yes" // manhwa and manhua read left to right, or top to bottom
	}
	if class == string(ClassOfficial) || class == string(ClassFan) {
		info.LanguageISO = "en" // the language of the translation, not the original's
	}
	return info
}

// WriteComicInfo adds ComicInfo.xml to a CBZ that has none. It only replaces the
// file once the new one has been checked; a file that isn't a ZIP is left alone.
func WriteComicInfo(path string, info ComicInfo) error {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil // a CBR or a PDF, whatever its name says
	}
	defer zr.Close()
	for _, f := range zr.File {
		if strings.EqualFold(filepath.Base(f.Name), "ComicInfo.xml") {
			return nil
		}
	}
	data, err := xml.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".comicinfo-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	zw := zip.NewWriter(tmp)
	for _, f := range zr.File {
		if err := zw.Copy(f); err != nil {
			tmp.Close()
			return err
		}
	}
	w, err := zw.Create("ComicInfo.xml")
	if err == nil {
		_, err = w.Write(append([]byte(xml.Header), data...))
	}
	if err == nil {
		err = zw.Close()
	}
	if err == nil {
		err = tmp.Close()
	} else {
		tmp.Close()
	}
	if err != nil {
		return err
	}
	check, err := zip.OpenReader(tmp.Name())
	if err != nil {
		return err
	}
	n := len(check.File)
	check.Close()
	if n != len(zr.File)+1 {
		return errors.New("the new file came out short")
	}
	if st, err := os.Stat(path); err == nil {
		_ = os.Chmod(tmp.Name(), st.Mode())
	}
	zr.Close()
	return os.Rename(tmp.Name(), path)
}
