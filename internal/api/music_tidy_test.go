package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/rdborg/mediarium/internal/music"
	"github.com/rdborg/mediarium/internal/musictidy"
)

const tidyFirstAlbumGroup = "aaaaaaaa-0000-4000-8000-000000000001"

// tidyAudio writes a tone as long as the fixture's track, tagged as given.
func tidyAudio(t *testing.T, path string, seconds int, meta ...string) []byte {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	args := []string{"-hide_banner", "-loglevel", "error", "-y", "-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=440:sample_rate=8000:duration=%d", seconds)}
	for _, m := range meta {
		args = append(args, "-metadata", m)
	}
	args = append(args, path)
	if out, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		t.Fatalf("make %s: %v: %s", path, err, out)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// tidyFirstAlbum lays out the fixture's "First Album" in a folder, with
// untidy names but tags that say what each file is.
func tidyFirstAlbum(t *testing.T, folder string, ext string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	for _, tr := range []struct {
		name, title string
		secs        int
	}{{"aaa", "Opening", 180}, {"bbb", "Middle Song", 200}, {"ccc", "Closing Time", 240}} {
		p := filepath.Join(folder, tr.name+ext)
		out[p] = tidyAudio(t, p, tr.secs, "title="+tr.title, "album=First Album", "artist=Fixture Band")
	}
	return out
}

func tidyWait(t *testing.T, e *musicEnv) map[string]any {
	t.Helper()
	waitBackground(t, e.server)
	over := getJSON[map[string]any](t, e.client, e.base+"/api/music/tidy")
	job, ok := over["job"].(map[string]any)
	if !ok {
		t.Fatalf("no job: %v", over)
	}
	if job["phase"] != "done" {
		t.Fatalf("job: %v", job)
	}
	return job
}

// tidyPlan runs a check and returns its plan.
func tidyPlan(t *testing.T, e *musicEnv) (int64, []map[string]any) {
	t.Helper()
	postJSON[map[string]any](t, e.client, e.base+"/api/music/tidy/check", nil, http.StatusAccepted)
	job := tidyWait(t, e)
	id := int64(job["planId"].(float64))
	plan := getJSON[map[string]any](t, e.client, fmt.Sprintf("%s/api/music/tidy/plans/%d", e.base, id))
	var albums []map[string]any
	for _, a := range plan["albums"].([]any) {
		albums = append(albums, a.(map[string]any))
	}
	return id, albums
}

func tidyAlbumIn(t *testing.T, albums []map[string]any, folderPart string) map[string]any {
	t.Helper()
	for _, a := range albums {
		if strings.Contains(a["oldFolder"].(string), folderPart) {
			return a
		}
	}
	t.Fatalf("no album in a folder like %q: %v", folderPart, albums)
	return nil
}

func tidyNeedTools(t *testing.T) {
	t.Helper()
	if !musictidy.Available() {
		t.Skip("ffmpeg and ffprobe aren't on PATH")
	}
}

func TestTidyCheckApplyAndUndoOfALibraryAlbum(t *testing.T) {
	tidyNeedTools(t)
	e := newMusicEnv(t)
	folder := filepath.Join(e.musicRoot, "Fixture Band", "First Album")
	originals := tidyFirstAlbum(t, folder, ".flac")
	if err := os.WriteFile(filepath.Join(folder, "aaa.lrc"), []byte("[00:01.00]la"), 0o644); err != nil {
		t.Fatal(err)
	}
	postJSON[map[string]any](t, e.client, e.base+"/api/music/import/scan", nil, http.StatusAccepted)
	waitBackground(t, e.server)
	albums, err := e.server.MusicRepo.ListAllAlbums()
	if err != nil {
		t.Fatal(err)
	}
	var album music.Album
	for _, a := range albums {
		if a.MBID == tidyFirstAlbumGroup {
			album = a
		}
	}
	if album.Path != folder {
		t.Fatalf("the album wasn't registered at %s: %+v", folder, album)
	}

	// The check changes nothing and says what it would do.
	planID, rows := tidyPlan(t, e)
	for p, want := range originals {
		if got, err := os.ReadFile(p); err != nil || !bytes.Equal(got, want) {
			t.Fatalf("the check touched %s: %v", p, err)
		}
	}
	row := tidyAlbumIn(t, rows, "First Album")
	if row["status"] != "eligible" || row["newFolder"] != filepath.Join("Fixture Band", "First Album (1999)") {
		t.Fatalf("row: %v", row)
	}
	if d := row["detail"].(map[string]any); d["source"] != "library" || d["quality"] == "" {
		t.Fatalf("detail: %v", d)
	}
	items := row["items"].([]any)
	if len(items) != 3 {
		t.Fatalf("items: %v", items)
	}
	first := items[0].(map[string]any)
	if first["newName"] != filepath.Join("Fixture Band", "First Album (1999)", "01 - Opening.flac") {
		t.Fatalf("first: %v", first)
	}
	if !strings.Contains(fmt.Sprint(first["changes"]), "mb_releasegroupid") {
		t.Fatalf("changes: %v", first["changes"])
	}

	// Apply: new names, tags and library paths; originals kept. The fake
	// cover isn't a real picture, which never holds up the rest.
	postJSON[map[string]any](t, e.client, fmt.Sprintf("%s/api/music/tidy/plans/%d/apply", e.base, planID), map[string]any{"albums": []any{row["id"]}}, http.StatusAccepted)
	tidyWait(t, e)
	newFolder := filepath.Join(e.musicRoot, "Fixture Band", "First Album (1999)")
	tidied := filepath.Join(newFolder, "01 - Opening.flac")
	probe, err := musictidy.ProbeFile(context.Background(), tidied)
	if err != nil {
		t.Fatal(err)
	}
	tags := musictidy.ReadTags(probe.Tags)
	if tags[musictidy.TagTitle] != "Opening" || tags[musictidy.TagTrack] != "1" || tags[musictidy.TagTrackTotal] != "3" ||
		tags[musictidy.TagReleaseGroupID] != tidyFirstAlbumGroup || tags[musictidy.TagReleaseID] != "rel-first" || tags[musictidy.TagRecordingID] != "r1" ||
		tags[musictidy.TagAlbumArtist] != "Fixture Band" || tags[musictidy.TagDate] != "1999-03-01" {
		t.Errorf("tags: %v", tags)
	}
	if _, err := os.Stat(filepath.Join(newFolder, "01 - Opening.lrc")); err != nil {
		t.Errorf("the lyrics went along: %v", err)
	}
	updated, err := e.server.MusicRepo.GetAlbum(album.ID)
	if err != nil || updated.Path != newFolder {
		t.Errorf("album path: %q %v", updated.Path, err)
	}
	tracks, err := e.server.MusicRepo.ListTracks(album.ID)
	if err != nil || len(tracks) != 3 || tracks[0].FilePath != tidied {
		t.Errorf("tracks: %+v %v", tracks, err)
	}
	over := getJSON[map[string]any](t, e.client, e.base+"/api/music/tidy")
	if over["recycleBytes"].(float64) == 0 || over["hasKey"] != false {
		t.Errorf("overview: %v", over)
	}
	var pl []map[string]any
	data, _ := json.Marshal(over["plans"])
	_ = json.Unmarshal(data, &pl)
	if len(pl) != 1 || pl[0]["status"] != "applied" || pl[0]["applied"] != float64(1) {
		t.Errorf("plans: %v", pl)
	}

	// A second check finds no names or tags left to change (only the cover,
	// as the fake one couldn't be embedded).
	again, rows2 := tidyPlan(t, e)
	for _, r := range rows2 {
		for _, it := range r["items"].([]any) {
			item := it.(map[string]any)
			if item["changes"] != nil || item["oldName"] != item["newName"] || item["cover"] != true {
				t.Errorf("second check still wants: %v", item)
			}
		}
	}
	if again == planID {
		t.Errorf("a new check is a new plan")
	}

	// Undo puts every original back byte for byte, and the library's paths.
	postJSON[map[string]any](t, e.client, fmt.Sprintf("%s/api/music/tidy/plans/%d/undo", e.base, planID), nil, http.StatusAccepted)
	job := tidyWait(t, e)
	if job["kind"] != "undo" {
		t.Errorf("job: %v", job)
	}
	for p, want := range originals {
		if got, err := os.ReadFile(p); err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s not back: %v", p, err)
		}
	}
	if _, err := os.Stat(filepath.Join(folder, "aaa.lrc")); err != nil {
		t.Errorf("lyrics not back: %v", err)
	}
	if _, err := os.Stat(newFolder); !os.IsNotExist(err) {
		t.Errorf("the tidy folder should be gone: %v", err)
	}
	updated, _ = e.server.MusicRepo.GetAlbum(album.ID)
	if updated.Path != folder {
		t.Errorf("album path after undo: %q", updated.Path)
	}
	tracks, _ = e.server.MusicRepo.ListTracks(album.ID)
	if tracks[0].FilePath != filepath.Join(folder, "aaa.flac") {
		t.Errorf("track path after undo: %q", tracks[0].FilePath)
	}
}

// fakeTidyAcoustID answers every lookup as the fixture's First Album.
func fakeTidyAcoustID(t *testing.T, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits != nil {
			hits.Add(1)
		}
		_ = r.ParseForm()
		if r.Form.Get("client") != "the-key" {
			t.Errorf("client: %q", r.Form.Get("client"))
		}
		fmt.Fprintf(w, `{"status":"ok","results":[{"score":0.95,"recordings":[{"id":"r1","releasegroups":[{"id":%q,"title":"First Album","type":"Album"}]}]}]}`, tidyFirstAlbumGroup)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestTidyRecognisesAnUnknownFolderBySound(t *testing.T) {
	tidyNeedTools(t)
	e := newMusicEnv(t)
	var hits atomic.Int32
	e.server.TestSetAcoustID(fakeTidyAcoustID(t, &hits).URL)
	postJSONMethod[map[string]any](t, e.client, http.MethodPut, e.base+"/api/music/tidy/acoustid", map[string]any{"key": "the-key"}, http.StatusOK)
	got := getJSON[map[string]any](t, e.client, e.base+"/api/music/tidy/acoustid")
	if got["hasKey"] != true || got["key"] != nil {
		t.Fatalf("key answer: %v", got)
	}
	cleared := postJSONMethod[map[string]any](t, e.client, http.MethodPut, e.base+"/api/music/tidy/acoustid", map[string]any{"key": ""}, http.StatusOK)
	if cleared["hasKey"] != false {
		t.Fatalf("an empty key removes it: %v", cleared)
	}
	postJSONMethod[map[string]any](t, e.client, http.MethodPut, e.base+"/api/music/tidy/acoustid", map[string]any{"key": "the-key"}, http.StatusOK)
	tidyFirstAlbum(t, filepath.Join(e.musicRoot, "misc", "old rip"), ".mp3")

	_, rows := tidyPlan(t, e)
	row := tidyAlbumIn(t, rows, "old rip")
	if row["status"] != "eligible" || row["newFolder"] != filepath.Join("Fixture Band", "First Album (1999)") || row["artist"] != "Fixture Band" {
		t.Fatalf("row: %v", row)
	}
	if d := row["detail"].(map[string]any); d["source"] != "fingerprint" || d["score"] != 0.95 {
		t.Fatalf("detail: %v", d)
	}
	if n := hits.Load(); n == 0 || n > 3 {
		t.Fatalf("%d lookups for one album: at most three samples", n)
	}
}

func TestTidyFailsOpenWithoutAcoustID(t *testing.T) {
	tidyNeedTools(t)
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, e *musicEnv)
	}{
		{"no key", func(t *testing.T, e *musicEnv) {}},
		{"server down", func(t *testing.T, e *musicEnv) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
			t.Cleanup(srv.Close)
			e.server.TestSetAcoustID(srv.URL)
			postJSONMethod[map[string]any](t, e.client, http.MethodPut, e.base+"/api/music/tidy/acoustid", map[string]any{"key": "the-key"}, http.StatusOK)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newMusicEnv(t)
			tc.setup(t, e)
			// Names that can be read find the album all the same.
			named := tidyFirstAlbum(t, filepath.Join(e.musicRoot, "Fixture Band", "First Album"), ".mp3")
			// Names that say nothing leave a folder alone, not the job failed.
			lost := filepath.Join(e.musicRoot, "misc", "lost")
			tidyAudio(t, filepath.Join(lost, "x.mp3"), 5)
			_, rows := tidyPlan(t, e)
			row := tidyAlbumIn(t, rows, "First Album")
			if row["status"] != "eligible" || row["detail"].(map[string]any)["source"] != "names" {
				t.Fatalf("named: %v", row)
			}
			left := tidyAlbumIn(t, rows, "lost")
			if left["status"] != "blocked" || !strings.Contains(left["reason"].(string), "wasn't recognised") {
				t.Fatalf("lost: %v", left)
			}
			for p, want := range named {
				if got, err := os.ReadFile(p); err != nil || !bytes.Equal(got, want) {
					t.Fatalf("the check touched %s: %v", p, err)
				}
			}
		})
	}
}

func TestTidyLeavesAlbumsWithUnmatchedFilesAlone(t *testing.T) {
	tidyNeedTools(t)
	e := newMusicEnv(t)
	folder := filepath.Join(e.musicRoot, "Fixture Band", "First Album")
	tidyFirstAlbum(t, folder, ".mp3")
	postJSON[map[string]any](t, e.client, e.base+"/api/music/import/scan", nil, http.StatusAccepted)
	waitBackground(t, e.server)
	// A fourth file nobody can place: wrong length, a title of its own.
	tidyAudio(t, filepath.Join(folder, "zzz.mp3"), 5, "title=Secret Bonus")
	_, rows := tidyPlan(t, e)
	row := tidyAlbumIn(t, rows, "First Album")
	if row["status"] != "blocked" || !strings.Contains(row["reason"].(string), "couldn't be matched") {
		t.Fatalf("row: %v", row)
	}
	if body, _ := json.Marshal(row["items"]); string(body) != "[]" {
		t.Fatalf("items: %s", body)
	}
}

func TestTidyReportsDuplicatesAndKeepsTheBest(t *testing.T) {
	tidyNeedTools(t)
	e := newMusicEnv(t)
	e.server.TestSetAcoustID(fakeTidyAcoustID(t, nil).URL)
	postJSONMethod[map[string]any](t, e.client, http.MethodPut, e.base+"/api/music/tidy/acoustid", map[string]any{"key": "the-key"}, http.StatusOK)
	lossy := tidyFirstAlbum(t, filepath.Join(e.musicRoot, "misc", "mp3 copy"), ".mp3")
	tidyFirstAlbum(t, filepath.Join(e.musicRoot, "misc", "flac copy"), ".flac")
	planID, rows := tidyPlan(t, e)
	worse, better := tidyAlbumIn(t, rows, "mp3 copy"), tidyAlbumIn(t, rows, "flac copy")
	if worse["status"] != "blocked" || !strings.Contains(worse["reason"].(string), "flac copy") {
		t.Fatalf("worse: %v", worse)
	}
	if better["status"] != "eligible" {
		t.Fatalf("better: %v", better)
	}
	copies := better["detail"].(map[string]any)["copies"].([]any)
	if len(copies) != 1 || !strings.Contains(copies[0].(map[string]any)["folder"].(string), "mp3 copy") || copies[0].(map[string]any)["better"] != false {
		t.Fatalf("copies: %v", copies)
	}
	// Choosing the blocked one is refused, and nothing is ever deleted.
	if status, _ := tidyPost(t, e, fmt.Sprintf("%s/api/music/tidy/plans/%d/apply", e.base, planID), map[string]any{"albums": []any{worse["id"]}}); status != http.StatusBadRequest {
		t.Fatalf("apply of a blocked album: %d", status)
	}
	for p := range lossy {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("copy touched: %v", err)
		}
	}
}

func tidyPost(t *testing.T, e *musicEnv, url string, body any) (int, map[string]any) {
	t.Helper()
	data, _ := json.Marshal(body)
	resp, err := e.client.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestTidyEmptyRecycleNowAndTheDailyJob(t *testing.T) {
	tidyNeedTools(t)
	e := newMusicEnv(t)
	folder := filepath.Join(e.musicRoot, "Fixture Band", "First Album")
	tidyFirstAlbum(t, folder, ".mp3")
	postJSON[map[string]any](t, e.client, e.base+"/api/music/import/scan", nil, http.StatusAccepted)
	waitBackground(t, e.server)
	planID, rows := tidyPlan(t, e)
	row := tidyAlbumIn(t, rows, "First Album")
	postJSON[map[string]any](t, e.client, fmt.Sprintf("%s/api/music/tidy/plans/%d/apply", e.base, planID), map[string]any{"albums": []any{row["id"]}}, http.StatusAccepted)
	tidyWait(t, e)
	over := getJSON[map[string]any](t, e.client, e.base+"/api/music/tidy")
	kept := over["recycleBytes"].(float64)
	if kept == 0 {
		t.Fatalf("overview: %v", over)
	}
	// Today's originals stay through the daily job.
	e.server.RunTidyRecycleJob()
	if musictidy.RecycleSize(e.musicRoot) == 0 {
		t.Fatal("the daily job removed originals younger than 30 days")
	}
	out := postJSON[map[string]any](t, e.client, e.base+"/api/music/tidy/recycle/empty", nil, http.StatusOK)
	if out["freedBytes"].(float64) != kept || out["recycleBytes"].(float64) != 0 {
		t.Fatalf("empty: %v", out)
	}
	if _, err := os.Stat(filepath.Join(e.musicRoot, musictidy.RecycleFolder)); !os.IsNotExist(err) {
		t.Fatalf("recycle folder: %v", err)
	}
}
