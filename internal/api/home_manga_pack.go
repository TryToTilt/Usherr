package api

// Usherr: a manga pack fetched by torrent downloads only the volumes it's
// needed for. The files of volumes the library already has, or that were
// unticked, are left out, as a TV pack leaves out the other seasons; a pack
// taken to swap one volume keeps only that volume. Files whose names don't
// say which volume they are, and volumes the library doesn't list, stay in.

import (
	"path"
	"strings"

	"github.com/rdborg/mediarium/internal/manga"
	"github.com/rdborg/mediarium/internal/queue"
	"github.com/rdborg/mediarium/internal/torrentclient"
)

// mangaPackSkips lists the files of a manga torrent not worth downloading for
// the queue item. It's nil for anything that isn't a manga download.
func (s *Server) mangaPackSkips(item queue.Item, files []torrentclient.FileState) []string {
	if !s.mangaEnabled() {
		return nil
	}
	repo := s.mangaRepo()
	d, err := repo.DownloadFor(item.ID)
	if err != nil || d.SeriesID == 0 || d.Source == mangaSourceDex {
		return nil
	}
	sr, err := repo.GetSeries(d.SeriesID)
	if err != nil {
		return nil
	}
	vols, err := repo.Volumes(sr.ID)
	if err != nil {
		return nil
	}
	only := "" // a swap: the one volume wanted from the pack
	if d.Replace && d.VolumeID > 0 {
		for _, v := range vols {
			if v.ID == d.VolumeID {
				only = manga.NormalNumber(v.Number)
			}
		}
	}
	return mangaSkipsFor(files, repo.Names(sr), vols, only)
}

// mangaSkipsFor decides which files of a pack to leave out: the volumes the
// library has or doesn't want, or, with only set, every volume but that one.
func mangaSkipsFor(files []torrentclient.FileState, names []string, vols []manga.Volume, only string) []string {
	have := map[string]bool{} // volumes not to fetch
	for _, v := range vols {
		if v.Status == manga.StatusDownloaded || !v.Wanted {
			have[manga.NormalNumber(v.Number)] = true
		}
	}
	var skip []string
	for _, f := range files {
		base := path.Base(f.Path)
		if !manga.IsPictureFile(base) && !wrappedVolume(base) {
			continue
		}
		n := manga.ParseFor(base, names)
		if n.Kind != manga.KindVolume || n.Number == "" || n.Edition != "" || n.Extra != "" {
			continue
		}
		num := manga.NormalNumber(n.Number)
		if (only != "" && num != only) || (only == "" && have[num]) {
			skip = append(skip, f.Path)
		}
	}
	return skip
}

// wrappedVolume: a volume packed on its own in an archive ("Name v01.zip"). The
// parts of a split RAR (.r00, .part2.rar) aren't, and stay in.
func wrappedVolume(name string) bool {
	low := strings.ToLower(name)
	if strings.Contains(low, ".part") {
		return false
	}
	return strings.HasSuffix(low, ".zip") || strings.HasSuffix(low, ".rar") || strings.HasSuffix(low, ".7z")
}
