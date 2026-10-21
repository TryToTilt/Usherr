package api_test

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/queue"
)

// What nzb360's "recently downloaded" cards ask for: history as the paged
// envelope, filtered by event type, with the movie, show and episode when the
// app asks for them; the same lines as plain lists; the calendar with the
// episode file; and the file, parse and empty-list answers.
func TestArrHistoryAndFiles(t *testing.T) {
	server, base, admin, _, _ := familyServer(t)
	key := postJSON[map[string]any](t, admin, base+"/api/auth/api-keys", map[string]any{"name": "nzb360"}, http.StatusCreated)["key"].(string)
	radarr, sonarr := base+"/radarr/api/v3", base+"/sonarr/api/v3"

	movie, err := server.MovieRepo.Add(library.Movie{TMDBID: 701, Title: "Night Harbour", Year: 2021, Monitored: true, ReleaseDate: "2021-05-14"})
	if err != nil {
		t.Fatal(err)
	}
	show, err := server.MovieRepo.AddSeries(library.Series{TMDBID: 9001, Title: "Salt & Static", Year: 2019, Monitored: true}, []library.Episode{
		{Season: 1, Episode: 1, AirDate: "2019-02-01", Title: "Static"}, {Season: 1, Episode: 2, AirDate: "2019-02-08", Title: "Salt"},
	})
	if err != nil {
		t.Fatal(err)
	}
	eps, _ := server.MovieRepo.ListEpisodes(show.ID)
	file := filepath.Join(t.TempDir(), "S01E01.mkv")
	if err := os.WriteFile(file, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := server.MovieRepo.SetEpisodeStatus(eps[0].ID, library.StatusDownloaded, "WEBDL-1080p", file); err != nil {
		t.Fatal(err)
	}

	enqueue := func(it queue.Item, status queue.Status, errText string) int64 {
		it.NZBURL, it.Protocol = "http://127.0.0.1:1/x.nzb", queue.ProtocolUsenet
		id, err := server.QueueRepo.Enqueue(it)
		if err != nil {
			t.Fatal(err)
		}
		if err := server.QueueRepo.SetStatus(id, status, errText); err != nil {
			t.Fatal(err)
		}
		return id
	}
	enqueue(queue.Item{MovieID: movie.ID, ReleaseTitle: "Night.Harbour.2021.1080p.BluRay.x264-GRP", SizeBytes: 4000}, queue.StatusCompleted, "")
	enqueue(queue.Item{MovieID: movie.ID, ReleaseTitle: "Night.Harbour.2021.720p.WEB-DL.x264-BAD", SizeBytes: 2000}, queue.StatusFailed, "download failed")
	enqueue(queue.Item{SeriesID: show.ID, Season: 1, ReleaseTitle: "Salt.and.Static.S01.1080p.WEB-DL.x264-GRP", SizeBytes: 3000}, queue.StatusCompleted, "")

	// Radarr: the envelope, newest first, only imports when asked for 3.
	h := obj(arrCall(t, "GET", radarr+"/history?page=1&pageSize=20&sortKey=date&sortDirection=descending&eventType=3&includeMovie=true", key, nil, http.StatusOK))
	recs := list(h["records"])
	if h["page"] != float64(1) || h["pageSize"] != float64(20) || h["totalRecords"] != float64(1) || len(recs) != 1 {
		t.Fatalf("radarr history: %v", h)
	}
	r := obj(recs[0])
	for _, k := range []string{"id", "movieId", "sourceTitle", "quality", "customFormats", "languages", "date", "eventType", "data", "downloadId", "movie"} {
		if r[k] == nil {
			t.Errorf("radarr history record misses %q: %v", k, r)
		}
	}
	if r["eventType"] != "downloadFolderImported" || r["sourceTitle"] != "Night.Harbour.2021.1080p.BluRay.x264-GRP" || obj(r["movie"])["title"] != "Night Harbour" {
		t.Errorf("radarr history record: %v", r)
	}
	if obj(obj(r["quality"])["quality"])["name"] != "Bluray-1080p" {
		t.Errorf("quality: %v", r["quality"])
	}
	// Without a filter: 2 grabbed + 1 imported + 1 failed, paged.
	h = obj(arrCall(t, "GET", radarr+"/history?page=1&pageSize=3", key, nil, http.StatusOK))
	if h["totalRecords"] != float64(4) || len(list(h["records"])) != 3 {
		t.Fatalf("radarr history paging: %v", h)
	}
	for _, rec := range list(h["records"]) {
		if obj(rec)["movie"] != nil {
			t.Error("the movie came back without includeMovie")
		}
	}
	if f := obj(arrCall(t, "GET", radarr+"/history?eventType=4&eventType=1", key, nil, http.StatusOK)); f["totalRecords"] != float64(3) {
		t.Fatalf("grabbed and failed: %v", f)
	}
	if got := list(arrCall(t, "GET", fmt.Sprintf("%s/history/movie?movieId=%d&eventType=4", radarr, movie.ID), key, nil, http.StatusOK)); len(got) != 1 || obj(got[0])["eventType"] != "downloadFailed" {
		t.Fatalf("history/movie: %v", got)
	}
	if got := list(arrCall(t, "GET", radarr+"/history/since?date=2000-01-01T00:00:00Z&eventType=1", key, nil, http.StatusOK)); len(got) != 2 {
		t.Fatalf("history/since: %v", got)
	}
	if got := list(arrCall(t, "GET", radarr+"/history/since?date=2999-01-01T00:00:00Z", key, nil, http.StatusOK)); len(got) != 0 {
		t.Fatalf("history/since in the future: %v", got)
	}

	// Sonarr: the season pack is one line per episode.
	sh := obj(arrCall(t, "GET", sonarr+"/history?pageSize=50&eventType=3&includeSeries=true&includeEpisode=true", key, nil, http.StatusOK))
	srecs := list(sh["records"])
	if sh["totalRecords"] != float64(2) || len(srecs) != 2 {
		t.Fatalf("sonarr history: %v", sh)
	}
	s0 := obj(srecs[0])
	if s0["seriesId"] == nil || s0["episodeId"] == nil || obj(s0["series"])["title"] != "Salt & Static" || obj(s0["episode"])["seasonNumber"] != float64(1) {
		t.Errorf("sonarr history record: %v", s0)
	}
	if got := list(arrCall(t, "GET", fmt.Sprintf("%s/history/series?seriesId=%d&seasonNumber=1&eventType=1", sonarr, show.ID), key, nil, http.StatusOK)); len(got) != 2 {
		t.Fatalf("history/series: %v", got)
	}
	if got := list(arrCall(t, "GET", fmt.Sprintf("%s/history/since?date=2000-01-01&episodeId=%d", sonarr, eps[1].ID), key, nil, http.StatusOK)); len(got) != 2 {
		t.Fatalf("history of one episode: %v", got)
	}

	// Files.
	ef := list(arrCall(t, "GET", fmt.Sprintf("%s/episodefile?seriesId=%d", sonarr, show.ID), key, nil, http.StatusOK))
	if len(ef) != 1 || obj(ef[0])["relativePath"] != "S01E01.mkv" || obj(ef[0])["size"] != float64(5) || obj(ef[0])["seasonNumber"] != float64(1) {
		t.Fatalf("episodefile: %v", ef)
	}
	arrCall(t, "GET", fmt.Sprintf("%s/episodefile/%d", sonarr, eps[0].ID), key, nil, http.StatusOK)
	arrCall(t, "GET", fmt.Sprintf("%s/episodefile/%d", sonarr, eps[1].ID), key, nil, http.StatusNotFound)
	cal := list(arrCall(t, "GET", sonarr+"/calendar?start=2019-01-01&end=2019-12-31&includeSeries=true&includeEpisodeFile=true", key, nil, http.StatusOK))
	if len(cal) != 2 || obj(obj(cal[0])["episodeFile"])["relativePath"] != "S01E01.mkv" || obj(cal[1])["episodeFile"] != nil {
		t.Fatalf("sonarr calendar: %v", cal)
	}
	arrCall(t, "GET", fmt.Sprintf("%s/moviefile/%d", radarr, movie.ID), key, nil, http.StatusNotFound)

	// Parse, rename, manual import.
	p := obj(arrCall(t, "GET", radarr+"/parse?title=Night.Harbour.2021.1080p.BluRay.x264-GRP", key, nil, http.StatusOK))
	if obj(p["parsedMovieInfo"])["year"] != float64(2021) || obj(p["movie"])["tmdbId"] != float64(701) {
		t.Fatalf("radarr parse: %v", p)
	}
	sp := obj(arrCall(t, "GET", sonarr+"/parse?title=Salt.and.Static.S01E02.1080p.WEB-DL.x264-GRP", key, nil, http.StatusOK))
	info := obj(sp["parsedEpisodeInfo"])
	if info["seasonNumber"] != float64(1) || len(list(info["episodeNumbers"])) != 1 || len(list(sp["episodes"])) != 1 {
		t.Fatalf("sonarr parse: %v", sp)
	}
	arrCall(t, "GET", radarr+"/parse", key, nil, http.StatusBadRequest)
	for _, u := range []string{radarr + "/rename?movieId=1", sonarr + "/rename?seriesId=1", radarr + "/manualimport?folder=/x", sonarr + "/manualimport?downloadId=a"} {
		if got := arrCall(t, "GET", u, key, nil, http.StatusOK); got == nil || len(list(got)) != 0 {
			t.Errorf("%s: %v", u, got)
		}
	}
	if q := obj(arrCall(t, "GET", radarr+"/queue?page=1&pageSize=10&includeMovie=true", key, nil, http.StatusOK)); q["totalRecords"] != float64(0) || q["records"] == nil {
		t.Errorf("queue envelope: %v", q)
	}
}
