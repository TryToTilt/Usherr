package api

// Usherr: Manga in the OPDS catalogue, in the same shape as Comics (opds_comics.go):
// the series, each series' volumes with the file to download, covers, and OPDS Page
// Streaming so an app such as Chunky, Panels or KOReader can read a page at a time.
// The pages are the ones the web reader shows, and your place is in the feed. These
// feeds aren't kept in the feed cache, as your place changes as you read.

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/manga"
	"github.com/rdborg/mediarium/internal/opds"
)

func opdsMangaUpdated(sr manga.Series) time.Time {
	if t, err := time.Parse(time.RFC3339, sr.AddedAt); err == nil {
		return t
	}
	if t, err := time.Parse("2006-01-02T15:04:05.999Z", sr.AddedAt); err == nil {
		return t
	}
	return time.Unix(0, 0).UTC()
}

// opdsMangaVisible are the series this account may see that have something to read.
func (s *Server) opdsMangaVisible(user *auth.User) ([]manga.Series, error) {
	list, err := s.mangaRepo().ListSeries()
	if err != nil {
		return nil, err
	}
	adult := s.opdsMayAdult(user)
	var out []manga.Series
	for _, sr := range list {
		if sr.Have > 0 && (!sr.IsAdult || adult) {
			out = append(out, sr)
		}
	}
	return out, nil
}

// opdsMangaSeriesList: the series, each leading to its volumes.
func (s *Server) opdsMangaSeriesList(w http.ResponseWriter, r *http.Request, v opdsView, user *auth.User) {
	list, err := s.opdsMangaVisible(user)
	if err != nil {
		slog.Error("opds: list manga", "err", err)
		http.Error(w, "Couldn't read the manga.", http.StatusInternalServerError)
		return
	}
	total := len(list)
	page := opdsPage(r, total, opdsNavPage)
	start := (page - 1) * opdsNavPage
	end := min(start+opdsNavPage, total)
	self := v.href("/manga")
	if page > 1 {
		self += "?page=" + strconv.Itoa(page)
	}
	updated := time.Unix(0, 0).UTC()
	for _, sr := range list {
		if t := opdsMangaUpdated(sr); t.After(updated) {
			updated = t
		}
	}
	f := opds.Feed{
		ID: "urn:mediarium:opds:manga", Title: "Manga", Updated: updated, Kind: opds.Navigation,
		Links:        v.startAndSearch(opds.Link{Rel: opds.RelSelf, Href: self, Type: v.navType()}),
		TotalResults: total, ItemsPerPage: opdsNavPage, StartIndex: start + 1, Page: page,
	}
	f.Links = append(f.Links, opds.Link{Rel: opds.RelUp, Href: v.href("/"), Type: v.navType()})
	v.pageLinks(&f, "/manga", nil, page, total, opdsNavPage, v.navType())
	for _, sr := range list[start:end] {
		id := strconv.FormatInt(sr.ID, 10)
		count := fmt.Sprintf("%d volumes", sr.Have)
		if sr.Have == 1 {
			count = "1 volume"
		}
		f.Entries = append(f.Entries, opds.Entry{
			ID: "urn:mediarium:manga-series:" + id, Title: sr.Title, Updated: opdsMangaUpdated(sr), Content: count,
			Links: []opds.Link{
				{Rel: opds.RelSubsection, Href: v.href("/manga/series/" + id), Type: v.acqType()},
				{Rel: opds.RelImage, Href: s.cfg.URLBase + "/opds/manga/series/" + id + "/cover", Type: "image/jpeg"},
				{Rel: opds.RelThumbnail, Href: s.cfg.URLBase + "/opds/manga/series/" + id + "/cover", Type: "image/jpeg"},
			},
		})
	}
	s.writeFeed(w, r, v, f)
}

// mangaPageCount is how many pages the reader shows for a volume, or 0 when that
// isn't known without unpacking the whole file (a RAR not read yet, a PDF): such a
// volume is offered for download only.
func (s *Server) mangaPageCount(vol manga.Volume) int {
	info, err := os.Stat(vol.FilePath)
	if err != nil {
		return 0
	}
	key := countKey(vol.FilePath, info)
	if v, ok := comicCounts.Load(key); ok {
		return v.(int)
	}
	n := 0
	switch strings.ToLower(filepath.Ext(vol.FilePath)) {
	case ".pdf":
	case ".epub":
		if pages, err := s.mangaPages(vol); err == nil {
			n = len(pages)
		}
	default:
		n = zipPageCount(vol.FilePath)
	}
	if n > 0 {
		comicCounts.Store(key, n)
	}
	return n
}

// opdsMangaVolumes: one series' volumes with files, in reading order.
func (s *Server) opdsMangaVolumes(w http.ResponseWriter, r *http.Request, v opdsView, user *auth.User, idText string) {
	id, err := strconv.ParseInt(idText, 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	repo := s.mangaRepo()
	sr, err := repo.GetSeries(id)
	if err != nil || (sr.IsAdult && !s.opdsMayAdult(user)) {
		http.NotFound(w, r)
		return
	}
	all, err := repo.Volumes(sr.ID)
	if err != nil {
		slog.Error("opds: list volumes", "err", err)
		http.Error(w, "Couldn't read the manga.", http.StatusInternalServerError)
		return
	}
	_ = repo.FillReading(user.ID, sr.ID, all)
	var list []manga.Volume
	for _, vol := range all {
		if vol.Status == manga.StatusDownloaded && vol.FilePath != "" {
			list = append(list, vol)
		}
	}
	p := "/manga/series/" + idText
	total := len(list)
	page := opdsPage(r, total, opdsPageSize)
	start := (page - 1) * opdsPageSize
	end := min(start+opdsPageSize, total)
	self := v.href(p)
	if page > 1 {
		self += "?page=" + strconv.Itoa(page)
	}
	f := opds.Feed{
		ID: "urn:mediarium:opds:manga-series:" + idText, Title: sr.Title, Updated: opdsMangaUpdated(sr), Kind: opds.Acquisition,
		Links:        v.startAndSearch(opds.Link{Rel: opds.RelSelf, Href: self, Type: v.acqType()}),
		TotalResults: total, ItemsPerPage: opdsPageSize, StartIndex: start + 1, Page: page,
	}
	f.Links = append(f.Links, opds.Link{Rel: opds.RelUp, Href: v.href("/manga"), Type: v.navType()})
	v.pageLinks(&f, p, nil, page, total, opdsPageSize, v.acqType())
	for _, vol := range list[start:end] {
		f.Entries = append(f.Entries, s.opdsMangaEntry(v, user.ID, sr, vol))
	}
	s.writeFeed(w, r, v, f)
}

func (s *Server) opdsMangaEntry(v opdsView, user int64, sr manga.Series, vol manga.Volume) opds.Entry {
	id := strconv.FormatInt(vol.ID, 10)
	base := s.cfg.URLBase + "/opds/manga/volumes/" + id
	e := opds.Entry{
		ID: "urn:mediarium:manga:" + id, Title: fmt.Sprintf("%s, Volume %s", sr.Title, vol.Number), Updated: opdsMangaUpdated(sr),
		Series: sr.Title, SeriesPos: vol.Number,
	}
	if sr.Author != "" {
		e.Authors = []string{sr.Author}
	}
	ct := opdsFileTypes[strings.TrimPrefix(strings.ToLower(filepath.Ext(vol.FilePath)), ".")]
	if ct == "" {
		ct = "application/octet-stream"
	}
	e.Links = []opds.Link{
		{Rel: opds.RelAcquisition, Href: base + "/download", Type: ct, Length: vol.FileSize},
		{Rel: opds.RelImage, Href: base + "/cover", Type: "image/jpeg"},
		{Rel: opds.RelThumbnail, Href: base + "/thumbnail", Type: "image/jpeg"},
	}
	if !v.json {
		if n := s.mangaPageCount(vol); n > 0 {
			l := opds.Link{Rel: opds.RelPSEStream, Href: base + "/pages/{pageNumber}?width={maxWidth}", Type: "image/jpeg", PSECount: n}
			if vol.Page > 0 {
				l.PSELastRead = min(vol.Page, n) - 1 // PSE counts pages from 0
				var ms int64
				_ = s.db.QueryRow(`SELECT updated_ms FROM home_positions WHERE user_id = ? AND item = ?`, user, manga.PositionItem(vol.ID)).Scan(&ms)
				l.LastReadAt = time.UnixMilli(ms).UTC()
			}
			e.Links = append(e.Links, l)
		}
	}
	return e
}

// opdsMangaFile answers /opds/manga/series/{id}/cover and
// /opds/manga/volumes/{id}/{download,cover,thumbnail,pages/{n}}.
func (s *Server) opdsMangaFile(w http.ResponseWriter, r *http.Request, user *auth.User, parts []string) {
	id, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	repo := s.mangaRepo()
	if parts[1] == "series" {
		sr, err := repo.GetSeries(id)
		if err != nil || len(parts) != 4 || parts[3] != "cover" || (sr.IsAdult && !s.opdsMayAdult(user)) {
			http.NotFound(w, r)
			return
		}
		volumes, _ := repo.Volumes(sr.ID)
		for _, vol := range volumes {
			if vol.Status == manga.StatusDownloaded && vol.FilePath != "" {
				s.opdsMangaPage(w, r, vol, 0, 400)
				return
			}
		}
		http.NotFound(w, r)
		return
	}
	vol, err := repo.GetVolume(id)
	if err != nil || vol.Status != manga.StatusDownloaded || vol.FilePath == "" {
		http.NotFound(w, r)
		return
	}
	if sr, err := repo.GetSeries(vol.SeriesID); err != nil || (sr.IsAdult && !s.opdsMayAdult(user)) {
		http.NotFound(w, r)
		return
	}
	switch {
	case len(parts) == 4 && parts[3] == "download":
		s.opdsFileDownload(w, r, vol.FilePath)
	case len(parts) == 4 && parts[3] == "cover":
		s.opdsMangaPage(w, r, vol, 0, 1200)
	case len(parts) == 4 && parts[3] == "thumbnail":
		s.opdsMangaPage(w, r, vol, 0, 400)
	case len(parts) == 5 && parts[3] == "pages":
		n, err := strconv.Atoi(parts[4])
		if err != nil || n < 0 {
			http.NotFound(w, r)
			return
		}
		s.opdsMangaPage(w, r, vol, n, pseWidth(r))
	default:
		http.NotFound(w, r)
	}
}

// opdsMangaPage sends one page (0-based, as PSE counts).
func (s *Server) opdsMangaPage(w http.ResponseWriter, r *http.Request, vol manga.Volume, n, width int) {
	if strings.EqualFold(filepath.Ext(vol.FilePath), ".pdf") {
		http.NotFound(w, r)
		return
	}
	pages, err := s.mangaPages(vol)
	if err != nil || n >= len(pages) {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Cache-Control", "private, max-age=86400")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "sandbox")
	if width > 0 {
		if sized := sizedPage(pages[n], width); sized != "" {
			http.ServeFile(w, r, sized)
			return
		}
	}
	http.ServeFile(w, r, pages[n])
}
