package api

import (
	"context"
	"encoding/json"
	"image"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/manga"
)

func TestMangaChapterPanelsAndPrefs(t *testing.T) {
	noPanelModels(t)
	s, repo, root := mangaTestServer(t)
	user := mangaUser(t, s, "reader")
	sr := addSeries(t, repo, "Chapter Road", 0, 1)
	file := filepath.Join(root, "Chapter Road", "Chapters", "Chapter Road c0001.cbz")
	writePanelCBZ(t, file, [][]image.Rectangle{{image.Rect(420, 30, 770, 570), image.Rect(30, 30, 380, 570), image.Rect(30, 620, 770, 1170)}}, [][2]int{{800, 1200}})
	c, err := repo.EnsureChapter(sr.ID, "1", "file")
	if err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(file)
	if err := repo.SetChapterFile(c.ID, file, st.Size()); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetReaderPrefs(user, sr.ID, manga.ReaderPrefs{Layout: "panels", Shift: 1}); err != nil {
		t.Fatal(err)
	}

	r := httptest.NewRequest(http.MethodGet, "/?rtl=1", nil)
	r.SetPathValue("id", strconv.FormatInt(c.ID, 10))
	r.SetPathValue("n", "1")
	rec := httptest.NewRecorder()
	s.handleMangaChapterPagePanels(rec, withUser(r, user))
	var got pagePanels
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || len(got.Panels) != 3 || got.Panels[0].X < 0.4 || got.Panels[1].X > 0.4 {
		t.Fatalf("%v %s", err, rec.Body.String())
	}
	var kept int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM manga_chapter_panels WHERE chapter_id = ?`, c.ID).Scan(&kept)
	if kept == 0 {
		t.Error("nothing was kept")
	}

	// A chapter is read the way the series is: panels, and the shift.
	r = httptest.NewRequest(http.MethodGet, "/", nil)
	r.SetPathValue("id", strconv.FormatInt(c.ID, 10))
	rec = httptest.NewRecorder()
	s.handleMangaChapterPages(rec, withUser(r, user))
	var pages struct {
		Layout string `json:"layout"`
		Shift  bool   `json:"shift"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &pages)
	if pages.Layout != "panels" || !pages.Shift {
		t.Errorf("%s", rec.Body.String())
	}

	// Stats count chapters beside volumes.
	if err := repo.MarkChaptersRead(user, []int64{c.ID}, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	stats, err := repo.ReadStatsFor(user, time.Now())
	if err != nil || stats.Chapters != 1 || stats.Volumes != 0 || stats.Series != 1 || stats.ThisMonth != 1 {
		t.Errorf("%v %+v", err, stats)
	}
}

// Ten presses of Add are ten looks one after another, not ten at once.
func TestMangaLooksAfterAddTakeTurns(t *testing.T) {
	s, _, _ := mangaTestServer(t)
	var now, most, done atomic.Int32
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.pacedAddLook(func(context.Context) {
				if n := now.Add(1); n > most.Load() {
					most.Store(n)
				}
				time.Sleep(5 * time.Millisecond)
				now.Add(-1)
				done.Add(1)
			})
		}()
	}
	wg.Wait()
	if most.Load() != 1 || done.Load() != 10 {
		t.Errorf("at most %d at once, %d done", most.Load(), done.Load())
	}
}
