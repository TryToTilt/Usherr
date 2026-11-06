package api

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/books"
	"github.com/rdborg/mediarium/internal/comics"
	"github.com/rdborg/mediarium/internal/opds"
	"github.com/rdborg/mediarium/internal/organizer"
)

// Comics in the OPDS catalogue: the Comics library's series, then each
// series' issues, with covers, the file to download, and OPDS Page Streaming
// (OPDS-PSE) so an app such as Chunky, Panels or KOReader can read an issue a
// page at a time without downloading the whole file. The pages are the ones
// the web reader shows (home_comics_reader.go). Comic books kept in the Books
// library stream the same way (opdsBookComicCount).
//
// These feeds aren't kept in the feed cache: the cache follows the book list,
// and the place each person got to changes as they read.

// comicCounts keeps how many pages a comic file has, by its path, size and
// modification time, so a feed of fifty issues doesn't open fifty files each
// time it is asked for.
var comicCounts sync.Map // string -> int

func countKey(path string, info os.FileInfo) string { return path + "|" + fileSig(info) }

// issuePageCount is how many pages the reader shows for an issue, or 0 when
// that isn't known without unpacking the whole file (a RAR not read yet, a
// PDF): such an issue is offered for download only.
func (s *Server) issuePageCount(is comics.Issue) int {
	if is.FilePath == "" {
		return 0
	}
	info, err := os.Stat(is.FilePath)
	if err != nil {
		return 0
	}
	key := countKey(is.FilePath, info)
	if v, ok := comicCounts.Load(key); ok {
		return v.(int)
	}
	n := 0
	dir := filepath.Join(s.issuePagesDir(), strconv.FormatInt(is.ID, 10))
	if done, err := os.ReadFile(filepath.Join(dir, ".done")); err == nil && string(done) == is.FilePath {
		// Unpacked already (it was read in the browser): count those.
		if entries, err := os.ReadDir(dir); err == nil {
			for _, e := range entries {
				if !e.IsDir() && isPageImage(e.Name()) {
					n++
				}
			}
		}
	} else {
		n = zipPageCount(is.FilePath)
	}
	if n > 0 {
		comicCounts.Store(key, n)
	}
	return n
}

// zipPageCount counts the pictures in a ZIP the way unpackComic picks them.
// It reads only the ZIP's list of files. Anything else counts 0.
func zipPageCount(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	head := make([]byte, 4)
	_, _ = io.ReadFull(f, head)
	f.Close()
	if !bytes.HasPrefix(head, []byte("PK")) {
		return 0
	}
	zr, err := zip.OpenReader(path)
	if err != nil {
		return 0
	}
	defer zr.Close()
	n := 0
	for _, zf := range zr.File {
		if !zf.FileInfo().IsDir() && isPageImage(zf.Name) {
			n++
		}
	}
	return n
}

// opdsBookComicCount is how many pages a comic in the Books library has, when
// that is quick to learn (a ZIP); 0 otherwise.
func (s *Server) opdsBookComicCount(b books.Book) int {
	if !books.ComicFormat(ebookExt(b)) {
		return 0
	}
	f, info, full, err := s.openEbook(b)
	if err != nil {
		return 0
	}
	key := countKey(full, info)
	if v, ok := comicCounts.Load(key); ok {
		f.Close()
		return v.(int)
	}
	format, ok := organizer.DetectFormat(f)
	f.Close()
	if !ok || format != organizer.FormatZip {
		return 0
	}
	c, err := s.openComic(b)
	if err != nil {
		return 0
	}
	n := len(c.pages)
	c.Close()
	if n > 0 {
		comicCounts.Store(key, n)
	}
	return n
}

// pseWidth reads the {maxWidth} an app filled in; apps that leave the
// template as it is get the page as it is.
func pseWidth(r *http.Request) int {
	n, err := strconv.Atoi(r.URL.Query().Get("width"))
	if err != nil || n < 600 {
		return 0
	}
	return min(n, 2400) / 100 * 100
}

// ---- Feeds ----

func issueUpdated(is comics.Issue) time.Time {
	for _, d := range []string{is.StoreDate, is.CoverDate} {
		if t, err := time.Parse("2006-01-02", d); err == nil {
			return t
		}
	}
	return time.Unix(0, 0).UTC()
}

func seriesTitle(sr comics.Series) string {
	if sr.StartYear > 0 {
		return fmt.Sprintf("%s (%d)", sr.Name, sr.StartYear)
	}
	return sr.Name
}

func issueTitle(sr comics.Series, is comics.Issue) string {
	t := sr.Name + " #" + is.Number
	if strings.TrimSpace(is.Title) != "" {
		t += ": " + strings.TrimSpace(is.Title)
	}
	return t
}

// comicLastRead is the page (1-based) each issue was left at by this person,
// with when.
func (s *Server) comicLastRead(userID int64) map[int64]struct {
	page int
	at   time.Time
} {
	out := map[int64]struct {
		page int
		at   time.Time
	}{}
	rows, err := s.db.Query(`SELECT item, position, updated_ms FROM home_positions WHERE user_id = ? AND item LIKE 'comic:%'`, userID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var item string
		var pos float64
		var ms int64
		if rows.Scan(&item, &pos, &ms) != nil {
			continue
		}
		id, err := strconv.ParseInt(strings.TrimPrefix(item, "comic:"), 10, 64)
		if err != nil || pos < 1 {
			continue
		}
		out[id] = struct {
			page int
			at   time.Time
		}{int(pos), time.UnixMilli(ms).UTC()}
	}
	return out
}

func (s *Server) opdsIssueEntry(v opdsView, sr comics.Series, is comics.Issue, read map[int64]struct {
	page int
	at   time.Time
}) opds.Entry {
	id := strconv.FormatInt(is.ID, 10)
	base := s.cfg.URLBase + "/opds/comics/issues/" + id
	e := opds.Entry{
		ID: "urn:mediarium:comic:" + id, Title: issueTitle(sr, is), Updated: issueUpdated(is),
		Series: sr.Name, SeriesPos: is.Number, Summary: strings.TrimSpace(is.Title),
	}
	if sr.Publisher != "" {
		e.Authors = []string{sr.Publisher}
	}
	if d := is.CoverDate; len(d) >= 4 {
		e.Issued = d
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(is.FilePath)), ".")
	ct := opdsFileTypes[ext]
	if ct == "" {
		ct = "application/octet-stream"
	}
	e.Links = []opds.Link{
		{Rel: opds.RelAcquisition, Href: base + "/download", Type: ct, Length: is.FileSize},
		{Rel: opds.RelImage, Href: base + "/cover", Type: "image/jpeg"},
		{Rel: opds.RelThumbnail, Href: base + "/thumbnail", Type: "image/jpeg"},
	}
	if !v.json {
		if n := s.issuePageCount(is); n > 0 {
			l := opds.Link{Rel: opds.RelPSEStream, Href: base + "/pages/{pageNumber}?width={maxWidth}", Type: "image/jpeg", PSECount: n}
			if at, ok := read[is.ID]; ok {
				l.PSELastRead = min(at.page, n) - 1 // PSE counts pages from 0
				l.LastReadAt = at.at
			}
			e.Links = append(e.Links, l)
		}
	}
	return e
}

// comicShelf is the series with at least one issue to read, by name, and
// their issues with files.
func (s *Server) comicShelf() ([]comics.Series, map[int64][]comics.Issue, error) {
	issues, err := s.comicRepo().DownloadedIssues()
	if err != nil {
		return nil, nil, err
	}
	bySeries := map[int64][]comics.Issue{}
	for _, is := range issues {
		bySeries[is.SeriesID] = append(bySeries[is.SeriesID], is)
	}
	all, err := s.comicRepo().ListSeries()
	if err != nil {
		return nil, nil, err
	}
	var list []comics.Series
	for _, sr := range all {
		if len(bySeries[sr.ID]) > 0 {
			list = append(list, sr)
		}
	}
	return list, bySeries, nil
}

func latestIssue(list []comics.Issue) time.Time {
	var latest time.Time
	for _, is := range list {
		if t := issueUpdated(is); t.After(latest) {
			latest = t
		}
	}
	if latest.IsZero() {
		latest = time.Unix(0, 0).UTC()
	}
	return latest
}

// opdsComicSeriesList: the series, each leading to its issues.
func (s *Server) opdsComicSeriesList(w http.ResponseWriter, r *http.Request, v opdsView) {
	list, bySeries, err := s.comicShelf()
	if err != nil {
		slog.Error("opds: list comics", "err", err)
		http.Error(w, "Couldn't read the comics.", http.StatusInternalServerError)
		return
	}
	total := len(list)
	page := opdsPage(r, total, opdsNavPage)
	start := (page - 1) * opdsNavPage
	end := min(start+opdsNavPage, total)
	self := v.href("/comics")
	if page > 1 {
		self += "?page=" + strconv.Itoa(page)
	}
	var everything []comics.Issue
	for _, l := range bySeries {
		everything = append(everything, l...)
	}
	f := opds.Feed{
		ID: "urn:mediarium:opds:comics", Title: "Comics", Updated: latestIssue(everything), Kind: opds.Navigation,
		Links:        v.startAndSearch(opds.Link{Rel: opds.RelSelf, Href: self, Type: v.navType()}),
		TotalResults: total, ItemsPerPage: opdsNavPage, StartIndex: start + 1, Page: page,
	}
	f.Links = append(f.Links, opds.Link{Rel: opds.RelUp, Href: v.href("/"), Type: v.navType()})
	v.pageLinks(&f, "/comics", nil, page, total, opdsNavPage, v.navType())
	for _, sr := range list[start:end] {
		n := len(bySeries[sr.ID])
		count := fmt.Sprintf("%d issues", n)
		if n == 1 {
			count = "1 issue"
		}
		if sr.Publisher != "" {
			count = sr.Publisher + " · " + count
		}
		id := strconv.FormatInt(sr.ID, 10)
		f.Entries = append(f.Entries, opds.Entry{
			ID: "urn:mediarium:comic-series:" + id, Title: seriesTitle(sr), Updated: latestIssue(bySeries[sr.ID]), Content: count,
			Links: []opds.Link{
				{Rel: opds.RelSubsection, Href: v.href("/comics/series/" + id), Type: v.acqType()},
				{Rel: opds.RelImage, Href: s.cfg.URLBase + "/opds/comics/series/" + id + "/cover", Type: "image/jpeg"},
				{Rel: opds.RelThumbnail, Href: s.cfg.URLBase + "/opds/comics/series/" + id + "/cover", Type: "image/jpeg"},
			},
		})
	}
	s.writeFeed(w, r, v, f)
}

// opdsComicIssues: one series' issues, in reading order.
func (s *Server) opdsComicIssues(w http.ResponseWriter, r *http.Request, v opdsView, user int64, idText string) {
	id, err := strconv.ParseInt(idText, 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	sr, err := s.comicRepo().GetSeries(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	all, err := s.comicRepo().Issues(sr.ID)
	if err != nil {
		slog.Error("opds: list issues", "err", err)
		http.Error(w, "Couldn't read the comics.", http.StatusInternalServerError)
		return
	}
	var list []comics.Issue
	for _, is := range all {
		if is.Status == comics.StatusDownloaded && is.FilePath != "" {
			list = append(list, is)
		}
	}
	p := "/comics/series/" + idText
	s.writeIssueFeed(w, r, v, user, p, "urn:mediarium:opds:comic-series:"+idText, seriesTitle(sr), nil, func(int64) (comics.Series, bool) { return sr, true }, list)
}

// writeIssueFeed writes a paged acquisition feed of issues.
func (s *Server) writeIssueFeed(w http.ResponseWriter, r *http.Request, v opdsView, user int64, p, id, title string, extra url.Values,
	seriesOf func(int64) (comics.Series, bool), list []comics.Issue) {
	total := len(list)
	page := opdsPage(r, total, opdsPageSize)
	start := (page - 1) * opdsPageSize
	end := min(start+opdsPageSize, total)
	q := url.Values{}
	for k, vs := range extra {
		q[k] = vs
	}
	if page > 1 {
		q.Set("page", strconv.Itoa(page))
	}
	self := v.href(p)
	if len(q) > 0 {
		self += "?" + q.Encode()
	}
	f := opds.Feed{
		ID: id, Title: title, Updated: latestIssue(list), Kind: opds.Acquisition,
		Links:        v.startAndSearch(opds.Link{Rel: opds.RelSelf, Href: self, Type: v.acqType()}),
		TotalResults: total, ItemsPerPage: opdsPageSize, StartIndex: start + 1, Page: page,
	}
	f.Links = append(f.Links, opds.Link{Rel: opds.RelUp, Href: v.href("/comics"), Type: v.navType()})
	v.pageLinks(&f, p, extra, page, total, opdsPageSize, v.acqType())
	read := s.comicLastRead(user)
	for _, is := range list[start:end] {
		if sr, ok := seriesOf(is.SeriesID); ok {
			f.Entries = append(f.Entries, s.opdsIssueEntry(v, sr, is, read))
		}
	}
	s.writeFeed(w, r, v, f)
}

// matchingIssues are the issues with files whose series name, publisher,
// number or title hold every word of q.
func (s *Server) matchingIssues(q string) ([]comics.Issue, map[int64]comics.Series, error) {
	terms := strings.Fields(strings.ToLower(q))
	series := map[int64]comics.Series{}
	if len(terms) == 0 {
		return nil, series, nil
	}
	list, bySeries, err := s.comicShelf()
	if err != nil {
		return nil, nil, err
	}
	var out []comics.Issue
	for _, sr := range list {
		series[sr.ID] = sr
		for _, is := range bySeries[sr.ID] {
			hay := strings.ToLower(sr.Name + " " + sr.Publisher + " #" + is.Number + " " + is.Title + " " + strconv.Itoa(sr.StartYear))
			all := true
			for _, t := range terms {
				if !strings.Contains(hay, strings.TrimPrefix(t, "#")) {
					all = false
					break
				}
			}
			if all {
				out = append(out, is)
			}
		}
	}
	return out, series, nil
}

// opdsSearchAll answers a search over the books and the comics together:
// books first, by title, then issues by series. Used when comics are on.
func (s *Server) opdsSearchAll(w http.ResponseWriter, r *http.Request, v opdsView, user int64, q string) {
	var entries []opds.Entry
	var updated time.Time
	if s.ebooksEnabled() {
		list, err := s.ebookBooks()
		if err != nil {
			slog.Error("opds: list books", "err", err)
			http.Error(w, "Couldn't read the library.", http.StatusInternalServerError)
			return
		}
		keep := searchMatch(q)
		var picked []books.Book
		for _, b := range list {
			if keep(b) {
				picked = append(picked, b)
			}
		}
		sort.SliceStable(picked, func(i, j int) bool { return byTitle(picked[i], picked[j]) })
		for _, b := range picked {
			entries = append(entries, s.opdsBookEntry(v, b))
		}
		updated = latestAdded(picked)
	}
	issues, series, err := s.matchingIssues(q)
	if err != nil {
		slog.Error("opds: search comics", "err", err)
		http.Error(w, "Couldn't read the comics.", http.StatusInternalServerError)
		return
	}
	if t := latestIssue(issues); t.After(updated) {
		updated = t
	}
	total := len(entries) + len(issues)
	page := opdsPage(r, total, opdsPageSize)
	start := (page - 1) * opdsPageSize
	end := min(start+opdsPageSize, total)
	extra := url.Values{"q": {q}}
	self := v.href("/search") + "?" + extra.Encode()
	if page > 1 {
		self += "&page=" + strconv.Itoa(page)
	}
	f := opds.Feed{
		ID: "urn:mediarium:opds:search:" + q, Title: "Search: " + q, Updated: updated, Kind: opds.Acquisition,
		Links:        v.startAndSearch(opds.Link{Rel: opds.RelSelf, Href: self, Type: v.acqType()}),
		TotalResults: total, ItemsPerPage: opdsPageSize, StartIndex: start + 1, Page: page,
	}
	f.Links = append(f.Links, opds.Link{Rel: opds.RelUp, Href: v.href("/"), Type: v.navType()})
	v.pageLinks(&f, "/search", extra, page, total, opdsPageSize, v.acqType())
	var read map[int64]struct {
		page int
		at   time.Time
	}
	for i := start; i < end; i++ {
		if i < len(entries) {
			f.Entries = append(f.Entries, entries[i])
			continue
		}
		if read == nil {
			read = s.comicLastRead(user)
		}
		is := issues[i-len(entries)]
		f.Entries = append(f.Entries, s.opdsIssueEntry(v, series[is.SeriesID], is, read))
	}
	s.writeFeed(w, r, v, f)
}

// ---- Files, covers and pages ----

// opdsComicFile answers /opds/comics/series/{id}/cover and
// /opds/comics/issues/{id}/{download,cover,thumbnail,pages/{n}}.
func (s *Server) opdsComicFile(w http.ResponseWriter, r *http.Request, parts []string) {
	id, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	if parts[1] == "series" {
		if len(parts) != 4 || parts[3] != "cover" {
			http.NotFound(w, r)
			return
		}
		sr, err := s.comicRepo().GetSeries(id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if sr.ImageURL != "" {
			s.proxyComicVineImage(w, r, sr.ImageURL)
			return
		}
		// No picture from ComicVine: the first issue's.
		issues, _ := s.comicRepo().Issues(sr.ID)
		for _, is := range issues {
			if is.Status == comics.StatusDownloaded && is.FilePath != "" {
				s.opdsIssueCover(w, r, is, 400)
				return
			}
		}
		http.NotFound(w, r)
		return
	}
	is, err := s.comicRepo().GetIssue(id)
	if err != nil || is.Status != comics.StatusDownloaded || is.FilePath == "" {
		http.NotFound(w, r)
		return
	}
	switch {
	case len(parts) == 4 && parts[3] == "download":
		s.opdsIssueDownload(w, r, is)
	case len(parts) == 4 && parts[3] == "cover":
		s.opdsIssueCover(w, r, is, 0)
	case len(parts) == 4 && parts[3] == "thumbnail":
		s.opdsIssueCover(w, r, is, 400)
	case len(parts) == 5 && parts[3] == "pages":
		n, err := strconv.Atoi(parts[4])
		if err != nil || n < 0 {
			http.NotFound(w, r)
			return
		}
		s.opdsIssuePage(w, r, is, n, pseWidth(r))
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) opdsIssueDownload(w http.ResponseWriter, r *http.Request, is comics.Issue) {
	f, err := os.Open(is.FilePath)
	if err != nil {
		http.Error(w, "Couldn't find the file. It may have been moved or deleted.", http.StatusNotFound)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		http.Error(w, "Couldn't read the file.", http.StatusInternalServerError)
		return
	}
	name := filepath.Base(is.FilePath)
	ct := opdsFileTypes[strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")]
	if ct == "" {
		ct = "application/octet-stream"
	}
	h := w.Header()
	h.Set("Content-Type", ct)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "sandbox")
	h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	http.ServeContent(w, r, name, st.ModTime(), f)
}

// opdsIssueCover sends the issue's ComicVine cover, or its first page.
func (s *Server) opdsIssueCover(w http.ResponseWriter, r *http.Request, is comics.Issue, width int) {
	if is.ImageURL != "" {
		s.proxyComicVineImage(w, r, is.ImageURL)
		return
	}
	if width == 0 {
		width = 1200
	}
	s.opdsIssuePage(w, r, is, 0, width)
}

// opdsIssuePage sends one page (0-based, as PSE counts), cut down to width
// when that is given.
func (s *Server) opdsIssuePage(w http.ResponseWriter, r *http.Request, is comics.Issue, n, width int) {
	if strings.EqualFold(filepath.Ext(is.FilePath), ".pdf") {
		http.NotFound(w, r)
		return
	}
	pages, err := s.comicPages(is.ID, is.FilePath)
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

// opdsBookComicPage sends one page (0-based) of a comic in the Books library.
func (s *Server) opdsBookComicPage(w http.ResponseWriter, r *http.Request, b books.Book, n int) {
	c, err := s.openComic(b)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer c.Close()
	if n < 0 || n >= len(c.pages) {
		http.NotFound(w, r)
		return
	}
	page, closePage, ct, err := openComicPage(c, n)
	if err != nil {
		http.Error(w, "This page can't be shown.", http.StatusUnprocessableEntity)
		return
	}
	defer closePage()
	h := w.Header()
	h.Set("Content-Type", ct)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "sandbox")
	h.Set("Cache-Control", "private, max-age=86400")
	_, _ = io.Copy(w, page)
}
