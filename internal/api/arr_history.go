package api

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/quality"
	"github.com/rdborg/mediarium/internal/queue"
)

// History, files, /parse, /rename and /manualimport of the arr-style API (see
// arr.go). Apps such as nzb360 fill their "recently downloaded" cards from
// /history, so it is answered from Mediarium's own download list: every
// download is a "grabbed" line, and one that finished is a
// "downloadFolderImported" line, one that failed a "downloadFailed" line.

// The event ids the apps filter history with (eventType=1,3).
const (
	arrEventGrabbed  = 1
	arrEventImported = 3
	arrEventFailed   = 4
)

var arrEventNames = map[int]string{
	arrEventGrabbed:  "grabbed",
	arrEventImported: "downloadFolderImported",
	arrEventFailed:   "downloadFailed",
}

type arrHistoryRecord struct {
	ID                  int64           `json:"id"`
	MovieID             int64           `json:"movieId,omitempty"`
	SeriesID            int64           `json:"seriesId,omitempty"`
	EpisodeID           int64           `json:"episodeId,omitempty"`
	SourceTitle         string          `json:"sourceTitle"`
	Languages           []arrLanguage   `json:"languages"`
	Quality             arrQualityModel `json:"quality"`
	CustomFormats       []any           `json:"customFormats"`
	CustomFormatScore   int             `json:"customFormatScore"`
	QualityCutoffNotMet bool            `json:"qualityCutoffNotMet"`
	Date                string          `json:"date"`
	DownloadID          string          `json:"downloadId"`
	EventType           string          `json:"eventType"`
	Data                map[string]any  `json:"data"`
	Movie               *arrMovie       `json:"movie,omitempty"`
	Series              *arrSeries      `json:"series,omitempty"`
	Episode             *arrEpisode     `json:"episode,omitempty"`

	when time.Time
}

// arrEventFilter reads eventType (1, "1,3" or repeated).
func arrEventFilter(r *http.Request) map[int]bool {
	out := map[int]bool{}
	q := r.URL.Query()
	for _, key := range []string{"eventType", "eventtype"} {
		for _, v := range q[key] {
			for _, part := range strings.Split(v, ",") {
				if n, err := strconv.Atoi(strings.TrimSpace(part)); err == nil {
					out[n] = true
				}
			}
		}
	}
	return out
}

func arrIDParam(r *http.Request, keys ...string) int64 {
	for _, k := range keys {
		if n, err := strconv.ParseInt(r.URL.Query().Get(k), 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

func parseArrStored(s string) time.Time {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.000Z", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

// arrHistoryRecords builds every history line of one app, newest first,
// narrowed by eventType, movieId, seriesId, episodeId, downloadId and (for
// /history/since) date. The include* flags add the movie, show or episode.
func (s *Server) arrHistoryRecords(app arrApp, r *http.Request) ([]arrHistoryRecord, error) {
	items, err := s.QueueRepo.List()
	if err != nil {
		return nil, err
	}
	events := arrEventFilter(r)
	wantMovie := arrIDParam(r, "movieId", "movieid")
	wantSeries := arrIDParam(r, "seriesId", "seriesid")
	wantEpisode := arrIDParam(r, "episodeId", "episodeid")
	wantSeason := -1
	if v := r.URL.Query().Get("seasonNumber"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			wantSeason = n
		}
	}
	wantDownload := r.URL.Query().Get("downloadId")
	var since time.Time
	if v := r.URL.Query().Get("date"); v != "" {
		since = parseArrStored(v)
	}
	withMovie := queryBool(r, "includeMovie")
	withSeries := queryBool(r, "includeSeries")
	withEpisode := queryBool(r, "includeEpisode")

	var (
		mc     arrMovieContext
		sc     arrSeriesContext
		movies = map[int64]*arrMovie{}
		shows  = map[int64]*arrSeries{}
	)
	if withMovie {
		mc = s.newArrMovieContext()
	}
	if withSeries {
		sc = s.newArrSeriesContext()
	}
	out := []arrHistoryRecord{}
	for _, it := range items {
		switch {
		case app == arrRadarr && it.MovieID == 0, app == arrSonarr && it.SeriesID == 0, app == arrLidarr:
			continue
		}
		if wantMovie > 0 && it.MovieID != wantMovie || wantSeries > 0 && it.SeriesID != wantSeries {
			continue
		}
		if wantSeason >= 0 && it.Season != wantSeason {
			continue
		}
		downloadID := fmt.Sprintf("mediarium-%d", it.ID)
		if wantDownload != "" && !strings.EqualFold(wantDownload, downloadID) {
			continue
		}
		rel := parser.Parse(it.ReleaseTitle)
		q := arrQualityModelOf(app, quality.Classify(rel))
		added := parseArrStored(it.AddedAt)
		done := parseArrStored(it.CompletedAt)
		if done.IsZero() {
			done = added
		}
		protocol := "1"
		if it.Protocol == queue.ProtocolTorrent {
			protocol = "2"
		}
		kinds := []int{arrEventGrabbed}
		switch {
		case it.Status == queue.StatusCompleted && it.Error == "":
			kinds = append(kinds, arrEventImported)
		case it.Status == queue.StatusFailed:
			kinds = append(kinds, arrEventFailed)
		}
		eps := []library.Episode{{}}
		if app == arrSonarr {
			if found := s.queueEpisodes(it); len(found) > 0 {
				eps = found // a season pack is one line per episode, as in the queue
			}
		}
		for _, kind := range kinds {
			if len(events) > 0 && !events[kind] {
				continue
			}
			when := added
			data := map[string]any{"indexer": "", "releaseGroup": rel.Group, "size": strconv.FormatInt(it.SizeBytes, 10),
				"downloadClient": "Mediarium", "downloadClientName": "Mediarium", "protocol": protocol, "customFormatScore": "0"}
			if kind != arrEventGrabbed {
				when = done
				data = map[string]any{"downloadClient": "Mediarium", "downloadClientName": "Mediarium"}
				if kind == arrEventImported && it.DestPath != "" {
					data["importedPath"] = it.DestPath
				}
				if kind == arrEventFailed {
					data["message"] = it.Error
				}
			}
			if !since.IsZero() && !when.After(since) {
				continue
			}
			for j, ep := range eps {
				if wantEpisode > 0 && ep.ID != wantEpisode {
					continue
				}
				rec := arrHistoryRecord{
					ID: (it.ID*1000+int64(j))*10 + int64(kind), SourceTitle: it.ReleaseTitle, Languages: []arrLanguage{arrEnglish},
					Quality: q, CustomFormats: []any{}, Date: when.Format("2006-01-02T15:04:05Z"), DownloadID: downloadID,
					EventType: arrEventNames[kind], Data: data, when: when,
				}
				if app == arrRadarr {
					rec.MovieID = it.MovieID
					if withMovie {
						if movies[it.MovieID] == nil {
							if m, err := s.MovieRepo.Get(it.MovieID); err == nil {
								a := s.toArrMovie(m, mc)
								movies[it.MovieID] = &a
							}
						}
						rec.Movie = movies[it.MovieID]
					}
				} else {
					rec.SeriesID, rec.EpisodeID = it.SeriesID, ep.ID
					if withSeries {
						if shows[it.SeriesID] == nil {
							if sr, err := s.MovieRepo.GetSeries(it.SeriesID); err == nil {
								a := s.toArrSeries(sr, sc)
								shows[it.SeriesID] = &a
							}
						}
						rec.Series = shows[it.SeriesID]
					}
					if withEpisode && ep.ID > 0 {
						e := toArrEpisode(ep)
						rec.Episode = &e
					}
				}
				out = append(out, rec)
			}
		}
	}
	desc := !strings.EqualFold(r.URL.Query().Get("sortDirection"), "ascending")
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].when.Equal(out[j].when) {
			return (out[i].ID > out[j].ID) == desc
		}
		return out[i].when.After(out[j].when) == desc
	})
	return out, nil
}

// arrHistory answers GET /history with the paged envelope.
func (s *Server) arrHistory(app arrApp) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		recs, err := s.arrHistoryRecords(app, r)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		page, size := paging(r, 10)
		from, to := pageOf(len(recs), page, size)
		dir := "descending"
		if strings.EqualFold(r.URL.Query().Get("sortDirection"), "ascending") {
			dir = "ascending"
		}
		writeJSON(w, http.StatusOK, arrPage{Page: page, PageSize: size, SortKey: "date", SortDirection: dir,
			TotalRecords: len(recs), Records: recs[from:to]})
	}
}

// arrHistoryList answers /history/since, /history/movie and /history/series:
// the same lines as a plain list.
func (s *Server) arrHistoryList(app arrApp) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		recs, err := s.arrHistoryRecords(app, r)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, recs)
	}
}

// --- episode files ---

type arrEpisodeFile struct {
	ID                  int64           `json:"id"`
	SeriesID            int64           `json:"seriesId"`
	SeasonNumber        int             `json:"seasonNumber"`
	RelativePath        string          `json:"relativePath"`
	Path                string          `json:"path"`
	Size                int64           `json:"size"`
	DateAdded           string          `json:"dateAdded"`
	SceneName           string          `json:"sceneName,omitempty"`
	ReleaseGroup        string          `json:"releaseGroup,omitempty"`
	Languages           []arrLanguage   `json:"languages"`
	Quality             arrQualityModel `json:"quality"`
	CustomFormats       []any           `json:"customFormats"`
	CustomFormatScore   int             `json:"customFormatScore"`
	MediaInfo           any             `json:"mediaInfo"`
	QualityCutoffNotMet bool            `json:"qualityCutoffNotMet"`
}

// toArrEpisodeFile is the file of a downloaded episode (nil when it has none).
func (s *Server) toArrEpisodeFile(ep library.Episode) *arrEpisodeFile {
	if ep.Status != library.StatusDownloaded || ep.FilePath == "" {
		return nil
	}
	f := &arrEpisodeFile{
		ID: ep.ID, SeriesID: ep.SeriesID, SeasonNumber: ep.Season, RelativePath: filepath.Base(ep.FilePath), Path: ep.FilePath,
		DateAdded: "2000-01-01T00:00:00Z", Languages: []arrLanguage{arrEnglish}, CustomFormats: []any{}, MediaInfo: map[string]any{},
		Quality: arrQualityModelOf(arrSonarr, quality.Tier(ep.Quality)),
	}
	if fi, err := os.Stat(ep.FilePath); err == nil {
		f.Size = fi.Size()
		f.DateAdded = fi.ModTime().UTC().Format("2006-01-02T15:04:05Z")
	}
	if mi := s.arrMediaInfoFor(ep.FilePath); mi != nil {
		f.MediaInfo = mi
	}
	if ep.ReleaseTitle != "" {
		f.SceneName = ep.ReleaseTitle
		f.ReleaseGroup = parser.Parse(ep.ReleaseTitle).Group
	}
	return f
}

// arrEpisodeFiles answers GET /episodefile?seriesId= or ?episodeFileIds=.
func (s *Server) arrEpisodeFiles(w http.ResponseWriter, r *http.Request) {
	out := []*arrEpisodeFile{}
	want := map[int64]bool{}
	for _, key := range []string{"episodeFileIds", "episodefileids"} {
		for _, v := range r.URL.Query()[key] {
			for _, part := range strings.Split(v, ",") {
				if n, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64); err == nil && n > 0 {
					want[n] = true
				}
			}
		}
	}
	if sid := arrIDParam(r, "seriesId", "seriesid"); sid > 0 {
		eps, err := s.MovieRepo.ListEpisodes(sid)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, ep := range eps {
			if f := s.toArrEpisodeFile(ep); f != nil && (len(want) == 0 || want[f.ID]) {
				out = append(out, f)
			}
		}
	} else {
		for id := range want {
			if ep, err := s.MovieRepo.GetEpisodeByID(id); err == nil {
				if f := s.toArrEpisodeFile(ep); f != nil {
					out = append(out, f)
				}
			}
		}
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) arrGetEpisodeFile(w http.ResponseWriter, r *http.Request) {
	id, ok := arrPathID(w, r)
	if !ok {
		return
	}
	ep, err := s.MovieRepo.GetEpisodeByID(id)
	if err != nil {
		arrNotFound(w)
		return
	}
	f := s.toArrEpisodeFile(ep)
	if f == nil {
		arrNotFound(w)
		return
	}
	writeJSON(w, http.StatusOK, f)
}

func (s *Server) arrGetMovieFile(w http.ResponseWriter, r *http.Request) {
	id, ok := arrPathID(w, r)
	if !ok {
		return
	}
	m, err := s.MovieRepo.Get(id)
	if err != nil {
		arrNotFound(w)
		return
	}
	f := s.toArrMovie(m, s.newArrMovieContext()).MovieFile
	if f == nil {
		arrNotFound(w)
		return
	}
	writeJSON(w, http.StatusOK, f)
}

// --- parse ---

// arrParse answers GET /parse?title=: what the release name says, and the
// library title it belongs to when there is one.
func (s *Server) arrParse(app arrApp) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		title := r.URL.Query().Get("title")
		if strings.TrimSpace(title) == "" {
			arrBadRequest(w, "Title", "Give a release name in title.")
			return
		}
		rel := parser.Parse(title)
		q := arrQualityModelOf(app, quality.Classify(rel))
		langs := []arrLanguage{arrEnglish}
		clean := parseClean(rel.Title)
		if app == arrRadarr {
			info := map[string]any{
				"releaseTitle": title, "movieTitle": rel.Title, "movieTitles": []string{rel.Title}, "year": rel.Year, "quality": q,
				"languages": langs, "releaseGroup": rel.Group, "edition": rel.Edition, "simpleReleaseTitle": rel.Title,
				"imdbId": "", "tmdbId": 0, "hardcodedSubs": "", "movieTitleInfo": map[string]any{"title": rel.Title, "titleWithoutYear": rel.Title, "year": rel.Year},
			}
			var movie *arrMovie
			if movies, err := s.MovieRepo.List(); err == nil && clean != "" {
				c := s.newArrMovieContext()
				for _, m := range movies {
					if parseClean(m.Title) == clean && (rel.Year == 0 || m.Year == rel.Year) {
						a := s.toArrMovie(m, c)
						movie = &a
						break
					}
				}
			}
			writeJSON(w, http.StatusOK, map[string]any{"id": 0, "title": title, "parsedMovieInfo": info, "movie": movie})
			return
		}
		eps := rel.Episodes
		if len(eps) == 0 && rel.Episode > 0 {
			eps = []int{rel.Episode}
		}
		if eps == nil {
			eps = []int{}
		}
		abs := rel.Absolute
		if abs == nil {
			abs = []int{}
		}
		info := map[string]any{
			"releaseTitle": title, "seriesTitle": rel.Title, "seriesTitleInfo": map[string]any{"title": rel.Title, "titleWithoutYear": rel.Title, "year": rel.Year},
			"quality": q, "seasonNumber": rel.Season, "episodeNumbers": eps, "absoluteEpisodeNumbers": abs, "specialAbsoluteEpisodeNumbers": []int{},
			"languages": langs, "fullSeason": rel.Season > 0 && len(eps) == 0, "isPartialSeason": false, "isMultiSeason": rel.SeasonTo > rel.Season,
			"isSeasonExtra": false, "special": rel.Season == 0 && len(eps) > 0, "releaseGroup": rel.Group, "releaseHash": "", "seasonPart": 0,
			"releaseTokens": "", "isDaily": rel.AirDate != "", "isAbsoluteNumbering": len(abs) > 0, "isPossibleSpecialEpisode": false,
			"isPossibleSceneSeasonSpecial": false,
		}
		var (
			series   *arrSeries
			episodes = []arrEpisode{}
		)
		if list, err := s.MovieRepo.ListSeries(); err == nil && clean != "" {
			c := s.newArrSeriesContext()
			for _, sr := range list {
				if parseClean(sr.Title) != clean {
					continue
				}
				a := s.toArrSeries(sr, c)
				series = &a
				if all, err := s.MovieRepo.ListEpisodes(sr.ID); err == nil {
					for _, ep := range all {
						for _, n := range eps {
							if ep.Season == rel.Season && ep.Episode == n {
								episodes = append(episodes, toArrEpisode(ep))
							}
						}
					}
				}
				break
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": 0, "title": title, "parsedEpisodeInfo": info, "series": series, "episodes": episodes})
	}
}

// parseClean is a title for comparing: "&" and "and" are the same word.
func parseClean(title string) string {
	return arrCleanTitle(strings.ReplaceAll(title, "&", " and "))
}
