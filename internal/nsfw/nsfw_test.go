package nsfw

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/store"
)

func testRepo(t *testing.T) *Repo {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return NewRepo(db)
}

func TestRepoNsfwKeepStateOnRefresh(t *testing.T) {
	r := testRepo(t)
	s := video("abc", "Northlight", "2023-11-02", "First", "Jane Doe")
	s.Performers[0].StashID = "p1"
	s.Wanted = true
	got, added, err := r.AddVideo(s)
	if err != nil || !added || got.ID == 0 || got.Status != StatusMissing {
		t.Fatalf("add: %+v added=%v err=%v", got, added, err)
	}
	if err := r.SetState(got.ID, StatusDownloaded, "/x/a.mp4", "1080p"); err != nil {
		t.Fatal(err)
	}
	s.Title = "Renamed"
	again, added, err := r.AddVideo(s)
	if err != nil || added {
		t.Fatalf("a second add must refresh, not add: added=%v err=%v", added, err)
	}
	if again.Title != "Renamed" || again.Status != StatusDownloaded || again.FilePath != "/x/a.mp4" {
		t.Fatalf("refresh lost the file or kept old details: %+v", again)
	}
	with, err := r.NsfwWithPerformer("p1")
	if err != nil || len(with) != 1 {
		t.Fatalf("videos with performer: %v %v", with, err)
	}
	wanted, _ := r.Wanted()
	if len(wanted) != 0 {
		t.Fatalf("a downloaded video isn't wanted any more: %v", wanted)
	}
}

func TestRepoFollowAndDownloads(t *testing.T) {
	r := testRepo(t)
	p, err := r.SavePerformer(Performer{StashID: "p1", Name: "Jane Doe"}, true)
	if err != nil || !p.Followed {
		t.Fatalf("follow: %+v %v", p, err)
	}
	// Refreshing details later must not unfollow.
	p, _ = r.SavePerformer(Performer{StashID: "p1", Name: "Jane D."}, false)
	if !p.Followed || p.Name != "Jane D." {
		t.Fatalf("refresh unfollowed or kept the old name: %+v", p)
	}
	st, _ := r.SaveStudio(Studio{StashID: "s1", Name: "Northlight"}, false)
	if st.Followed {
		t.Fatal("not followed unless asked")
	}
	followed, _ := r.Performers(true)
	if len(followed) != 1 {
		t.Fatalf("followed performers = %v", followed)
	}

	sc, _, _ := r.AddVideo(video("abc", "Northlight", "2023-11-02", "T"))
	res, err := r.db.Exec(`INSERT INTO download_queue (release_title, nzb_url, status) VALUES ('x', 'http://x', 'queued')`)
	if err != nil {
		t.Fatal(err)
	}
	qid, _ := res.LastInsertId()
	if err := r.LinkDownload(qid, sc.ID); err != nil {
		t.Fatal(err)
	}
	if !r.IsVideoDownload(qid) {
		t.Fatal("the download is a video download")
	}
	got, err := r.VideoForDownload(qid)
	if err != nil || got.ID != sc.ID {
		t.Fatalf("video for download: %+v %v", got, err)
	}
	if busy, _ := r.HasActiveDownload(sc.ID); !busy {
		t.Fatal("a queued download is active")
	}
	if _, err := r.VideoForDownload(qid + 100); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an unlinked download: %v", err)
	}
	c, _ := r.Count()
	if c.Nsfw != 1 || c.Performers != 1 || c.Studios != 0 {
		t.Fatalf("counts = %+v", c)
	}
}

func TestStashBoxSendsKeyAndReadsNsfw(t *testing.T) {
	var gotKey string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("ApiKey")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		if gotKey != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"data":{"queryScenes":{"count":1,"scenes":[{"id":"s1","title":"T","release_date":"2024-03-15T00:00:00Z","duration":1800,
			"studio":{"id":"st","name":"Northlight","aliases":["NORTHLIGHT"],"images":[]},
			"images":[{"url":"small","width":100,"height":50},{"url":"big","width":1000,"height":500}],
			"performers":[{"as":"Janie","performer":{"id":"p1","name":"Jane Doe","aliases":[]}}]}]}}}`))
	}))
	defer srv.Close()

	key := "secret"
	c := NewStashBox("test", func() string { return key })
	c.Endpoint = srv.URL
	page, err := c.NsfwOf(context.Background(), KindPerformer, "p1", 1, 10, "2024-01-01")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Nsfw) != 1 {
		t.Fatalf("page = %+v", page)
	}
	s := page.Nsfw[0]
	if s.ReleaseDate != "2024-03-15" || s.ImageURL != "big" || s.StudioName != "Northlight" || s.Performers[0].As != "Janie" {
		t.Fatalf("video = %+v", s)
	}
	in := gotBody["variables"].(map[string]any)["in"].(map[string]any)
	if _, ok := in["performers"]; !ok || in["date"] == nil {
		t.Fatalf("the query must filter by performer and date: %v", in)
	}

	key = "wrong"
	if _, err := c.NsfwOf(context.Background(), KindStudio, "st", 1, 10, ""); !errors.Is(err, ErrKeyRefused) {
		t.Fatalf("a refused key: %v", err)
	}
	key = " "
	if err := c.Test(context.Background()); !errors.Is(err, ErrNoKey) {
		t.Fatalf("no key: %v", err)
	}
	if !strings.Contains(ErrNoKey.Error(), "Settings") {
		t.Fatal("the no-key message should say where to add it")
	}
}

func TestBulkWantAndRemoveEmptyKeepsFiles(t *testing.T) {
	r := testRepo(t)
	empty, _, _ := r.AddVideo(video("e", "S", "2024-01-01", "Empty"))
	withFile, _, _ := r.AddVideo(video("f", "S", "2024-01-02", "Has file"))
	_ = r.SetState(withFile.ID, StatusDownloaded, "/data/nsfw/S/f.mp4", "1080p")
	busy, _, _ := r.AddVideo(video("b", "S", "2024-01-03", "Downloading"))
	res, err := r.db.Exec(`INSERT INTO download_queue (release_title, nzb_url, status) VALUES ('x', 'http://x', 'downloading')`)
	if err != nil {
		t.Fatal(err)
	}
	qid, _ := res.LastInsertId()
	_ = r.LinkDownload(qid, busy.ID)

	got, err := r.SetWantedMany([]int64{empty.ID, withFile.ID}, false)
	if err != nil || got.Changed != 2 {
		t.Fatalf("unwant = %+v %v", got, err)
	}
	got, err = r.RemoveEmpty([]int64{empty.ID, withFile.ID, busy.ID})
	if err != nil || got.Changed != 1 || got.Skipped != 2 {
		t.Fatalf("remove empty = %+v %v", got, err)
	}
	if _, err := r.GetVideo(empty.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("the empty video should be gone")
	}
	if _, err := r.GetVideo(withFile.ID); err != nil {
		t.Fatal("a video with a file must never be removed")
	}
	if _, err := r.GetVideo(busy.ID); err != nil {
		t.Fatal("a video that is downloading must not be removed")
	}
}

func TestTagsAndGendersSurviveAnImport(t *testing.T) {
	r := testRepo(t)
	imported := video("t1", "S", "2024-01-01", "T", "Jane")
	if _, _, err := r.AddVideo(imported); err != nil {
		t.Fatal(err)
	}
	if todo, _ := r.MissingDetails(10); len(todo) != 1 {
		t.Fatalf("an imported video has no details yet: %v", todo)
	}
	fromStash := video("t1", "S", "2024-01-01", "T", "Jane", "John")
	fromStash.Performers[0].Gender, fromStash.Performers[1].Gender = "FEMALE", "MALE"
	fromStash.Tags = []string{"POV", "Creampie"}
	fromStash.DetailsAt = now()
	if _, _, err := r.AddVideo(fromStash); err != nil {
		t.Fatal(err)
	}
	// Importing again (no StashDB details) must keep tags and genders.
	if _, _, err := r.AddVideo(imported); err != nil {
		t.Fatal(err)
	}
	got, _ := r.GetVideoByStashID("t1")
	if len(got.Tags) != 2 || len(got.Performers) != 2 || got.Performers[1].Gender != "MALE" || got.DetailsAt == "" {
		t.Fatalf("details lost: %+v", got)
	}
	if todo, _ := r.MissingDetails(10); len(todo) != 0 {
		t.Fatalf("details were read: %v", todo)
	}
}
