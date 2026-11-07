package manga

// Chapters: which upload of a chapter to take when several groups did it, how a
// chapter's file is named and credited, and the order of reading when a series
// has both volumes and chapters. Nothing here asks anyone anything.

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// OneshotNumber is the number of a chapter MangaDex gives none.
const OneshotNumber = "Oneshot"

// ChapterCopy is one upload of a chapter.
type ChapterCopy struct {
	DexID       string `json:"-"`
	GroupKey    string `json:"groupKey"`
	GroupName   string `json:"group"`
	Pages       int    `json:"pages"`
	ExternalURL string `json:"externalUrl,omitempty"`
	Unavailable bool   `json:"unavailable,omitempty"`
	PublishedAt string `json:"publishedAt,omitempty"`
	Volume      string `json:"-"`
	Title       string `json:"-"`
	Language    string `json:"-"`
}

// Readable reports whether its pages can be downloaded.
func (c ChapterCopy) Readable() bool {
	return c.ExternalURL == "" && !c.Unavailable && c.Pages > 0
}

// CopyOf turns an upload from the feed into a copy.
func CopyOf(d DexChapter) ChapterCopy {
	ids := make([]string, 0, len(d.Groups))
	names := make([]string, 0, len(d.Groups))
	for _, g := range d.Groups {
		ids = append(ids, g.ID)
		names = append(names, g.Name)
	}
	sort.Strings(ids)
	return ChapterCopy{DexID: d.ID, GroupKey: strings.Join(ids, ","), GroupName: strings.Join(names, " & "), Pages: d.Pages, ExternalURL: d.ExternalURL, Unavailable: d.Unavailable, PublishedAt: d.PublishedAt, Volume: d.Volume, Title: d.Title, Language: d.Language}
}

// ChooseCopy picks the upload to take. One that can be downloaded beats any that
// can't. Among those: the group chosen for this series; then the group that did the
// most chapters of it, so the translation stays in one voice; then the fullest; then
// the first published, so the choice doesn't change with every new upload. With
// nothing to download, a link to read elsewhere beats a chapter taken down.
func ChooseCopy(copies []ChapterCopy, prefer string, counts map[string]int) ChapterCopy {
	if len(copies) == 0 {
		return ChapterCopy{}
	}
	rank := func(c ChapterCopy) int {
		switch {
		case c.Readable():
			return 0
		case c.ExternalURL != "":
			return 1
		}
		return 2
	}
	best := copies[0]
	better := func(a, b ChapterCopy) bool {
		if x, y := rank(a), rank(b); x != y {
			return x < y
		}
		if prefer != "" {
			if x, y := a.GroupKey == prefer, b.GroupKey == prefer; x != y {
				return x
			}
		}
		if x, y := counts[a.GroupKey], counts[b.GroupKey]; x != y {
			return x > y
		}
		if a.Pages != b.Pages {
			return a.Pages > b.Pages
		}
		if a.PublishedAt != b.PublishedAt {
			return a.PublishedAt < b.PublishedAt
		}
		return a.DexID < b.DexID
	}
	for _, c := range copies[1:] {
		if better(c, best) {
			best = c
		}
	}
	return best
}

// ChapterFileName is a chapter's file name, without the extension:
// "Series c0123 [Group]". The credit stays on the file wherever it goes.
func ChapterFileName(series, number, group string) string {
	num := number
	if number != OneshotNumber {
		whole, frac, has := strings.Cut(numOf(number), ".")
		for len(whole) < 4 {
			whole = "0" + whole
		}
		num = "c" + whole
		if has {
			num += "." + frac
		}
	}
	name := series + " " + num
	if g := strings.TrimSpace(group); g != "" {
		name += " [" + g + "]"
	}
	return name
}

// InfoForChapter is the ComicInfo of a chapter from MangaDex, with its credits.
func InfoForChapter(s Series, c Chapter) ComicInfo {
	info := InfoFor(s, c.Number, string(ClassFan))
	info.Volume = c.Volume
	info.Title = c.Title
	info.LanguageISO = orDefault(c.Language, "en")
	group := c.Group
	if group == "" {
		group = "No group named"
	} else {
		info.Translator = group
	}
	info.ScanInformation = group + ", via MangaDex"
	info.Notes = "Translated by fans: " + group + ". From MangaDex (https://mangadex.org)."
	if c.DexID != "" {
		info.Web = "https://mangadex.org/chapter/" + c.DexID
	}
	return info
}

// WriteChapterCBZ makes a CBZ of pages, in the order given, with ComicInfo.xml. The
// pages are already pictures, so they're stored as they are. It's written beside
// dest and only takes dest's name once it's whole, so a stop can't leave half a file.
func WriteChapterCBZ(dest string, pages []string, info ComicInfo) error {
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".chapter-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	zw := zip.NewWriter(tmp)
	for i, page := range pages {
		if err := addStored(zw, fmt.Sprintf("%04d%s", i+1, strings.ToLower(filepath.Ext(page))), page); err != nil {
			tmp.Close()
			return err
		}
	}
	data, err := xml.MarshalIndent(info, "", "  ")
	if err == nil {
		var w io.Writer
		if w, err = zw.Create("ComicInfo.xml"); err == nil {
			_, err = w.Write(append([]byte(xml.Header), data...))
		}
	}
	if err == nil {
		err = zw.Close()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dest)
}

func addStored(zw *zip.Writer, name, file string) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
	if err != nil {
		return err
	}
	_, err = io.Copy(w, f)
	return err
}

// ReadingItem is one thing to read, in a series' order: a volume or a chapter.
type ReadingItem struct {
	Chapter bool
	ID      int64
	Number  string
	Have    bool // its file is here
	Wanted  bool
	Read    bool
	slot    float64
	sub     float64
}

// ReadingOrder puts a series' volumes and chapters in the order to read them.
// A chapter MangaDex files under a volume goes where that volume goes, and isn't
// listed once that volume is here: the volume holds it. One filed under no volume
// comes after every volume. Chapters that can't be read here aren't listed, and a
// volume that isn't here steps aside when its chapters are.
func ReadingOrder(volumes []Volume, chapters []Chapter) []ReadingItem {
	const after = 1e12
	haveChaptersOf := map[string]bool{}
	var out []ReadingItem
	for _, c := range chapters {
		if c.InVolumeID > 0 || c.ExternalURL != "" {
			continue
		}
		have := c.Status == StatusDownloaded
		if !have && c.Unavailable {
			continue
		}
		slot := float64(after)
		if c.Volume != "" {
			slot = SortKey(c.Volume)
			if have {
				haveChaptersOf[c.Volume] = true
			}
		}
		out = append(out, ReadingItem{Chapter: true, ID: c.ID, Number: c.Number, Have: have, Wanted: c.Wanted, Read: c.Read, slot: slot, sub: c.SortKey})
	}
	for _, v := range volumes {
		have := v.Status == StatusDownloaded
		if !have && haveChaptersOf[v.Number] {
			continue
		}
		out = append(out, ReadingItem{ID: v.ID, Number: v.Number, Have: have, Wanted: v.Wanted, Read: v.Read, slot: v.SortKey, sub: -1})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].slot != out[j].slot {
			return out[i].slot < out[j].slot
		}
		return out[i].sub < out[j].sub
	})
	return out
}

// NextInOrder is what comes after an item: the very next one, and the next one
// with a file (the same one when the very next has a file).
func NextInOrder(order []ReadingItem, chapter bool, id int64) (next, have *ReadingItem) {
	for i := range order {
		if order[i].Chapter != chapter || order[i].ID != id {
			continue
		}
		for j := i + 1; j < len(order); j++ {
			if next == nil {
				next = &order[j]
			}
			if order[j].Have {
				return next, &order[j]
			}
		}
		return next, nil
	}
	return nil, nil
}

// NextUnreadInOrder is the first item with a file after the last one read.
func NextUnreadInOrder(order []ReadingItem) *ReadingItem {
	last := -1
	for i, it := range order {
		if it.Read {
			last = i
		}
	}
	if last < 0 {
		return nil
	}
	for i := last + 1; i < len(order); i++ {
		if order[i].Have && !order[i].Read {
			return &order[i]
		}
	}
	return nil
}
