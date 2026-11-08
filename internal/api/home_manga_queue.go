package api

import (
	"fmt"
	"strconv"

	"github.com/rdborg/mediarium/internal/manga"
)

// describeMangaItem names a manga download in the queue: the series, which
// volume, and its cover, instead of a bare release name and a stand-in picture.
// A pack says the volumes it holds, even when it was taken for one of them.
func (s *Server) describeMangaItem(queueID int64) (title, subtitle, poster string, ok bool) {
	repo := s.mangaRepo()
	d, err := repo.DownloadFor(queueID)
	if err != nil || d.SeriesID == 0 {
		return "", "", "", false
	}
	sr, err := repo.GetSeries(d.SeriesID)
	if err != nil {
		return "", "", "", false
	}
	subtitle = "Manga"
	if item, err := s.QueueRepo.Get(queueID); err == nil && d.Source != mangaSourceDex {
		if what := mangaPackLabel(manga.ParseFor(item.ReleaseTitle, repo.Names(sr))); what != "" {
			return sr.Title, "Manga · " + what, sr.CoverURL, true
		}
	}
	if d.VolumeID > 0 {
		if v, err := repo.GetVolume(d.VolumeID); err == nil && v.Number != "" {
			subtitle = fmt.Sprintf("Manga · Volume %s", v.Number)
		}
	} else if d.Source == mangaSourceDex {
		subtitle = "Manga · Chapters"
	}
	return sr.Title, subtitle, sr.CoverURL, true
}

// mangaPackLabel says which volumes a pack's name holds: "Volumes 1–41", or
// "All volumes" for a complete series. It's "" for anything else.
func mangaPackLabel(n manga.Name) string {
	switch n.Kind {
	case manga.KindPack, manga.KindRange:
		if n.Hi > n.Lo {
			f := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
			return "Volumes " + f(n.Lo) + "–" + f(n.Hi)
		}
	case manga.KindWhole:
		return "All volumes"
	}
	return ""
}
