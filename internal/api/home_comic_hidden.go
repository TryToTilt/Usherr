package api

// Home fork: some posts hide their file names (the download is "file_0.bin").
// Before a comic download is filed, files whose names say nothing are looked
// at: a ZIP or RAR of page images is a comic (.cbz / .cbr, named after the
// release); a RAR holding comic files is unpacked.

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/nwaples/rardecode/v2"

	"github.com/rdborg/mediarium/internal/comics"
	"github.com/rdborg/mediarium/internal/organizer"
)

// knownExt are extensions that say what a file is.
var knownExt = regexp.MustCompile(`(?i)^\.(rar|r\d{2,3}|zip|7z|z\d{2}|\d{3}|par2|nfo|sfv|nzb|txt|jpe?g|png|webp|gif|url|srr|srs|md5|sha1|cbz|cbr|cb7|cbt|pdf|epub|db|ini)$`)

// nameSaysNothing reports whether a file's name gives no clue what it is
// ("file_0.bin", "a1b2c3d4", "x.1234abcd").
func nameSaysNothing(name string) bool {
	ext := filepath.Ext(name)
	return ext == "" || strings.EqualFold(ext, ".bin") || !knownExt.MatchString(ext)
}

// archiveHolds says what an archive holds: page images, comic files.
func archiveHolds(file string) (images, comicFiles int, rar bool, err error) {
	head := make([]byte, 8)
	f, err := os.Open(file)
	if err != nil {
		return 0, 0, false, err
	}
	n, _ := io.ReadFull(f, head)
	f.Close()
	head = head[:n]
	count := func(name string) {
		switch {
		case isPageImage(name):
			images++
		case comics.IsComicFile(name):
			comicFiles++
		}
	}
	switch {
	case bytes.HasPrefix(head, []byte("PK")):
		zr, err := zip.OpenReader(file)
		if err != nil {
			return 0, 0, false, err
		}
		defer zr.Close()
		for _, zf := range zr.File {
			count(zf.Name)
		}
		return images, comicFiles, false, nil
	case bytes.HasPrefix(head, []byte("Rar!")):
		rc, err := rardecode.OpenReader(file, rardecode.BufferSize(1<<20))
		if err != nil {
			return 0, 0, true, err
		}
		defer rc.Close()
		for {
			h, err := rc.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return images, comicFiles, true, err
			}
			count(h.Name)
		}
		return images, comicFiles, true, nil
	}
	return 0, 0, false, errors.New("not an archive")
}

// nameHiddenComics gives hidden-name comics in dir a comic's name, and
// unpacks RARs that hold comic files.
func nameHiddenComics(dir, releaseTitle string) {
	var hidden []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		if !nameSaysNothing(d.Name()) {
			return nil // a .rar piece, a .cbz, a .nfo…: it says what it is
		}
		if info, err := d.Info(); err == nil && info.Size() > 64<<10 {
			hidden = append(hidden, p)
		}
		return nil
	})
	base := organizer.Sanitize(strings.TrimSpace(releaseTitle), organizer.SanitizeReplace, "_")
	if base == "" {
		base = "comic"
	}
	for k, p := range hidden {
		images, comicFiles, rar, err := archiveHolds(p)
		if err != nil {
			continue
		}
		name := base
		if len(hidden) > 1 {
			name += " (" + string(rune('a'+k%26)) + ")"
		}
		switch {
		case images > 0 && comicFiles == 0:
			ext := ".cbz"
			if rar {
				ext = ".cbr"
			}
			dst := freeName(filepath.Join(filepath.Dir(p), name+ext))
			if os.Rename(p, dst) == nil {
				slog.Info("comics: a hidden-name download is a comic", "file", filepath.Base(p), "now", filepath.Base(dst))
			}
		case comicFiles > 0 && rar:
			arc := freeName(filepath.Join(filepath.Dir(p), name+".rar"))
			if os.Rename(p, arc) != nil {
				continue
			}
			if err := organizer.NewExtractor().Extract(arc, filepath.Dir(arc)); err != nil {
				slog.Info("comics: unpack a hidden-name download", "file", filepath.Base(arc), "err", err)
			}
		}
	}
}
