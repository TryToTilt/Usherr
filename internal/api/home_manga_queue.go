package api

import "fmt"

// describeMangaItem names a manga download in the queue: the series, which
// volume, and its cover, instead of a bare release name and a stand-in picture.
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
	if d.VolumeID > 0 {
		if v, err := repo.GetVolume(d.VolumeID); err == nil && v.Number != "" {
			subtitle = fmt.Sprintf("Manga · Volume %s", v.Number)
		}
	} else if d.Source == mangaSourceDex {
		subtitle = "Manga · Chapters"
	}
	return sr.Title, subtitle, sr.CoverURL, true
}
