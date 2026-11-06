package musictidy

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/store"
)

func needFFmpeg(t *testing.T) {
	t.Helper()
	if !Available() {
		t.Skip("ffmpeg and ffprobe aren't on PATH")
	}
}

// makeAudio writes a tiny tone in the format of the file's extension.
func makeAudio(t *testing.T, path string, seconds string, meta ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	args := []string{"-hide_banner", "-loglevel", "error", "-y", "-f", "lavfi", "-i", "sine=frequency=440:duration=" + seconds}
	for _, m := range meta {
		args = append(args, "-metadata", m)
	}
	args = append(args, path)
	if out, err := exec.Command(FFmpeg, args...).CombinedOutput(); err != nil {
		t.Fatalf("make %s: %v: %s", path, err, out)
	}
}

func makeCover(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "cover-test.jpg")
	if out, err := exec.Command(FFmpeg, "-hide_banner", "-loglevel", "error", "-y", "-f", "lavfi", "-i", "color=red:s=32x32", "-frames:v", "1", path).CombinedOutput(); err != nil {
		t.Fatalf("make cover: %v: %s", err, out)
	}
	return path
}

func newDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func testTarget() Target {
	return Target{
		Artist: "Robin", ArtistID: "artist-1", Album: "Long Night", Year: 2001, Date: "2001-03-04",
		ReleaseGroupID: "rg-1", ReleaseID: "rel-1",
		Tracks: []TargetTrack{
			{Disc: 1, Position: 1, Title: "First Song", TrackID: "t-1", RecordingID: "rec-1", Artist: "Robin", ArtistIDs: []string{"artist-1"}},
			{Disc: 1, Position: 2, Title: "Second Song", TrackID: "t-2", RecordingID: "rec-2", Artist: "Robin feat. Sam", ArtistIDs: []string{"artist-1", "artist-2"}},
		},
	}
}

// source reads a file on disk the way the check does.
func source(t *testing.T, path string, track int) SourceFile {
	t.Helper()
	st, link, err := StateOf(path)
	if err != nil {
		t.Fatal(err)
	}
	p, err := ProbeFile(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	return SourceFile{Path: path, Size: st.Size, ModNs: st.ModNs, Tags: ReadTags(p.Tags), Raw: p.Tags, Pictures: p.Pictures, Link: link, Track: track}
}

type fixture struct {
	root, folder string
	store        *Store
	engine       *Engine
	plan         int64
	db           *sql.DB
	originals    map[string][]byte
}

// newFixture lays out an untidy album of two files: one FLAC, one MP3.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	needFFmpeg(t)
	root := filepath.Join(t.TempDir(), "music")
	folder := filepath.Join(root, "robin stuff", "long night")
	a := filepath.Join(folder, "track one.flac")
	b := filepath.Join(folder, "CD1", "b.mp3")
	makeAudio(t, a, "2", "TITLE=wrong name", "ARTIST=Robin", "COMMENT=keep me")
	makeAudio(t, b, "2", "title=Second Song", "comment=keep me too")
	if err := os.WriteFile(filepath.Join(folder, "cover.jpg"), []byte("picture"), 0o644); err != nil {
		t.Fatal(err)
	}
	db := newDB(t)
	s := NewStore(db)
	plan, err := s.NewPlan(root)
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{root: root, folder: folder, store: s, plan: plan, db: db, originals: map[string][]byte{},
		engine: &Engine{Store: s, DB: db, Root: root}}
	for _, p := range []string{a, b} {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		f.originals[p] = data
	}
	return f
}

func (f *fixture) row(t *testing.T, o Options) AlbumRow {
	t.Helper()
	files := []SourceFile{
		source(t, filepath.Join(f.folder, "track one.flac"), 0),
		source(t, filepath.Join(f.folder, "CD1", "b.mp3"), 1),
	}
	p := PlanAlbum(f.root, f.folder, testTarget(), files, o)
	if p.Blocked != "" {
		t.Fatalf("blocked: %s", p.Blocked)
	}
	id, err := f.store.AddAlbum(f.plan, 0, "Robin", "Long Night", p, StatusEligible, "", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := f.store.Albums(f.plan, id)
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows: %v %v", rows, err)
	}
	return rows[0]
}

func TestPlanRenamesAndDiffsTags(t *testing.T) {
	f := newFixture(t)
	row := f.row(t, Options{CoverAvailable: true})
	want := filepath.Join(f.root, "Robin", "Long Night (2001)")
	if row.NewFolder != want {
		t.Fatalf("new folder %s", row.NewFolder)
	}
	if len(row.Items) != 2 || row.Items[0].NewPath != filepath.Join(want, "01 - First Song.flac") || row.Items[1].NewPath != filepath.Join(want, "02 - Second Song.mp3") {
		t.Fatalf("items: %+v", row.Items)
	}
	changes := map[string]Change{}
	for _, c := range row.Items[0].Plan.Changes {
		changes[c.Tag] = c
	}
	if c := changes[TagTitle]; c.Old != "wrong name" || c.New != "First Song" {
		t.Errorf("title change: %+v", c)
	}
	if _, same := changes[TagArtist]; same {
		t.Errorf("the artist already fits: %+v", changes[TagArtist])
	}
	if changes[TagRecordingID].New != "rec-1" || changes[TagReleaseGroupID].New != "rg-1" {
		t.Errorf("ids: %+v", changes)
	}
	if !row.Items[0].Plan.Cover || !row.Items[1].Plan.Cover {
		t.Errorf("cover wanted in files with none: %+v", row.Items)
	}
	for _, c := range row.Items[1].Plan.Changes {
		if c.Tag == TagRecordingID {
			t.Errorf("ffmpeg can't write the recording id into an MP3: %+v", c)
		}
		if c.Tag == TagTitle {
			t.Errorf("the MP3's title already fits: %+v", c)
		}
	}
}

func TestPlanOfATidyFolderChangesNothing(t *testing.T) {
	f := newFixture(t)
	row := f.row(t, Options{})
	if out := f.engine.ApplyAlbum(context.Background(), row, ""); out.Status != StatusApplied {
		t.Fatalf("apply: %+v", out)
	}
	files := []SourceFile{
		source(t, filepath.Join(row.NewFolder, "01 - First Song.flac"), 0),
		source(t, filepath.Join(row.NewFolder, "02 - Second Song.mp3"), 1),
	}
	p := PlanAlbum(f.root, row.NewFolder, testTarget(), files, Options{})
	if !p.Unchanged() {
		t.Fatalf("second look still wants changes: %+v", p)
	}
}

func TestPlanBlocksWhatIsUnsure(t *testing.T) {
	f := newFixture(t)
	a := source(t, filepath.Join(f.folder, "track one.flac"), 0)
	b := source(t, filepath.Join(f.folder, "CD1", "b.mp3"), -1)
	if p := PlanAlbum(f.root, f.folder, testTarget(), []SourceFile{a, b}, Options{}); !strings.Contains(p.Blocked, "1 of 2 files") {
		t.Errorf("unmatched: %q", p.Blocked)
	}
	b.Track = 1
	b.Link = true
	if p := PlanAlbum(f.root, f.folder, testTarget(), []SourceFile{a, b}, Options{}); !strings.Contains(p.Blocked, "links") {
		t.Errorf("link: %q", p.Blocked)
	}
	b.Link = false
	b.Raw = map[string]string{"lyrics-eng": "la la"}
	if p := PlanAlbum(f.root, f.folder, testTarget(), []SourceFile{a, b}, Options{}); !strings.Contains(p.Blocked, "lyrics") {
		t.Errorf("lyrics: %q", p.Blocked)
	}
	b.Raw = nil
	if p := PlanAlbum(f.root, f.folder, testTarget(), []SourceFile{a, a}, Options{}); !strings.Contains(p.Blocked, "same name") {
		t.Errorf("same name: %q", p.Blocked)
	}
	// The new folder is someone else's.
	if err := os.MkdirAll(filepath.Join(f.root, "Robin", "Long Night (2001)"), 0o755); err != nil {
		t.Fatal(err)
	}
	if p := PlanAlbum(f.root, f.folder, testTarget(), []SourceFile{a, b}, Options{}); !strings.Contains(p.Blocked, "already a folder") {
		t.Errorf("folder taken: %q", p.Blocked)
	}
}

func TestApplyAndUndoRoundTrip(t *testing.T) {
	f := newFixture(t)
	row := f.row(t, Options{CoverAvailable: true})
	cover := makeCover(t, t.TempDir())
	out := f.engine.ApplyAlbum(context.Background(), row, cover)
	if out.Status != StatusApplied || out.Reason != "" {
		t.Fatalf("apply: %+v", out)
	}

	// The new files have the new names, tags and cover, and kept the rest.
	flac := filepath.Join(row.NewFolder, "01 - First Song.flac")
	p, err := ProbeFile(context.Background(), flac)
	if err != nil {
		t.Fatal(err)
	}
	tags := ReadTags(p.Tags)
	if tags[TagTitle] != "First Song" || tags[TagAlbumArtist] != "Robin" || tags[TagTrack] != "1" || tags[TagTrackTotal] != "2" ||
		tags[TagDate] != "2001-03-04" || tags[TagRecordingID] != "rec-1" || tags[TagReleaseGroupID] != "rg-1" {
		t.Errorf("flac tags: %+v", tags)
	}
	if p.Tags["comment"] != "keep me" {
		t.Errorf("other tags kept: %+v", p.Tags)
	}
	if _, stamped := p.Tags["encoder"]; stamped {
		t.Errorf("ffmpeg's encoder tag left in: %+v", p.Tags)
	}
	if p.Pictures != 1 {
		t.Errorf("cover: %d", p.Pictures)
	}
	mp3 := filepath.Join(row.NewFolder, "02 - Second Song.mp3")
	p, err = ProbeFile(context.Background(), mp3)
	if err != nil {
		t.Fatal(err)
	}
	tags = ReadTags(p.Tags)
	if tags[TagArtist] != "Robin feat. Sam" || tags[TagTrack] != "2" || tags[TagTrackTotal] != "2" || tags[TagReleaseID] != "rel-1" || p.Tags["comment"] != "keep me too" || p.Pictures != 1 {
		t.Errorf("mp3 tags: %+v, pictures %d", p.Tags, p.Pictures)
	}

	// The folder went along with what else was in it.
	if _, err := os.Stat(filepath.Join(row.NewFolder, "cover.jpg")); err != nil {
		t.Errorf("the rest of the folder: %v", err)
	}
	if _, err := os.Stat(f.folder); !os.IsNotExist(err) {
		t.Errorf("old folder still there: %v", err)
	}
	// The originals are in the recycle folder, unchanged.
	rel, _ := filepath.Rel(f.root, filepath.Join(f.folder, "track one.flac"))
	kept, err := os.ReadFile(filepath.Join(RecycleDir(f.root, f.plan), rel))
	if err != nil || !bytes.Equal(kept, f.originals[filepath.Join(f.folder, "track one.flac")]) {
		t.Errorf("kept original: %v", err)
	}
	if RecycleSize(f.root) == 0 {
		t.Error("recycle size")
	}

	// Undo: every original back, byte for byte, and nothing else left.
	res, err := f.engine.Undo(context.Background(), f.plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || len(res[0].Problems) != 0 {
		t.Fatalf("undo: %+v", res)
	}
	for path, want := range f.originals {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s not back: %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(f.root, "Robin")); !os.IsNotExist(err) {
		t.Errorf("the new artist folder should be gone: %v", err)
	}
	if _, err := os.Stat(filepath.Join(f.folder, "cover.jpg")); err != nil {
		t.Errorf("the rest of the folder came back: %v", err)
	}
	if n := audioFiles(t, f.root); n != 2 {
		t.Errorf("%d audio files outside the recycle folder", n)
	}
}

func audioFiles(t *testing.T, root string) int {
	t.Helper()
	n := 0
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && !strings.Contains(p, RecycleFolder) && (strings.HasSuffix(p, ".flac") || strings.HasSuffix(p, ".mp3")) {
			n++
		}
		return nil
	})
	return n
}

func TestApplySwapsNamesInPlace(t *testing.T) {
	needFFmpeg(t)
	root := filepath.Join(t.TempDir(), "music")
	folder := filepath.Join(root, "Robin", "Long Night (2001)")
	a := filepath.Join(folder, "01 - Second Song.flac")
	b := filepath.Join(folder, "02 - First Song.flac")
	makeAudio(t, a, "1", "TITLE=Second Song")
	makeAudio(t, b, "1", "TITLE=First Song")
	db := newDB(t)
	s := NewStore(db)
	plan, _ := s.NewPlan(root)
	files := []SourceFile{source(t, a, 1), source(t, b, 0)}
	p := PlanAlbum(root, folder, testTarget(), files, Options{})
	if p.Blocked != "" || p.NewFolder != folder {
		t.Fatalf("plan: %+v", p)
	}
	id, _ := s.AddAlbum(plan, 0, "Robin", "Long Night", p, StatusEligible, "", nil)
	rows, _ := s.Albums(plan, id)
	e := &Engine{Store: s, DB: db, Root: root}
	if out := e.ApplyAlbum(context.Background(), rows[0], ""); out.Status != StatusApplied {
		t.Fatalf("apply: %+v", out)
	}
	pr, err := ProbeFile(context.Background(), filepath.Join(folder, "01 - First Song.flac"))
	if err != nil || ReadTags(pr.Tags)[TagTitle] != "First Song" {
		t.Fatalf("swap: %+v %v", pr.Tags, err)
	}
}

func TestApplyMarksChangedFilesStale(t *testing.T) {
	f := newFixture(t)
	row := f.row(t, Options{})
	path := filepath.Join(f.folder, "track one.flac")
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	out := f.engine.ApplyAlbum(context.Background(), row, "")
	if out.Status != StatusStale {
		t.Fatalf("apply: %+v", out)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("nothing should have moved: %v", err)
	}
	rows, _ := f.store.Albums(f.plan, row.ID)
	if rows[0].Status != StatusStale {
		t.Errorf("stored status %s", rows[0].Status)
	}
}

func TestApplyCollisionBlocksAndPutsBack(t *testing.T) {
	f := newFixture(t)
	row := f.row(t, Options{})
	// Something took the new folder's name after the check.
	if err := os.MkdirAll(row.NewFolder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(row.NewFolder, "01 - First Song.flac"), []byte("someone else's"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := f.engine.ApplyAlbum(context.Background(), row, "")
	if out.Status != StatusBlocked {
		t.Fatalf("apply: %+v", out)
	}
	for path, want := range f.originals {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s changed: %v", path, err)
		}
	}
	got, err := os.ReadFile(filepath.Join(row.NewFolder, "01 - First Song.flac"))
	if err != nil || string(got) != "someone else's" {
		t.Errorf("the other file was touched: %q %v", got, err)
	}
}

func TestApplyCollisionInPlaceIsTakenBack(t *testing.T) {
	needFFmpeg(t)
	root := filepath.Join(t.TempDir(), "music")
	folder := filepath.Join(root, "Robin", "Long Night (2001)")
	a := filepath.Join(folder, "track a.flac")
	b := filepath.Join(folder, "track b.flac")
	makeAudio(t, a, "1", "TITLE=First Song")
	makeAudio(t, b, "1", "TITLE=Second Song")
	origA, _ := os.ReadFile(a)
	db := newDB(t)
	s := NewStore(db)
	plan, _ := s.NewPlan(root)
	p := PlanAlbum(root, folder, testTarget(), []SourceFile{source(t, a, 0), source(t, b, 1)}, Options{})
	if p.Blocked != "" {
		t.Fatal(p.Blocked)
	}
	id, _ := s.AddAlbum(plan, 0, "Robin", "Long Night", p, StatusEligible, "", nil)
	rows, _ := s.Albums(plan, id)
	// The second track's new name got taken after the check.
	blocker := filepath.Join(folder, "02 - Second Song.flac")
	if err := os.WriteFile(blocker, []byte("in the way"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := &Engine{Store: s, DB: db, Root: root}
	out := e.ApplyAlbum(context.Background(), rows[0], "")
	if out.Status != StatusBlocked {
		t.Fatalf("apply: %+v", out)
	}
	got, err := os.ReadFile(a)
	if err != nil || !bytes.Equal(got, origA) {
		t.Errorf("first track not put back: %v", err)
	}
	if data, _ := os.ReadFile(blocker); string(data) != "in the way" {
		t.Errorf("the other file was touched: %q", data)
	}
	if entries, _ := filepath.Glob(filepath.Join(folder, ".usherr-tidy-*")); len(entries) != 0 {
		t.Errorf("temporary files left: %v", entries)
	}
}

func TestUndoLeavesAChangedFile(t *testing.T) {
	f := newFixture(t)
	row := f.row(t, Options{})
	if out := f.engine.ApplyAlbum(context.Background(), row, ""); out.Status != StatusApplied {
		t.Fatalf("apply: %+v", out)
	}
	mine := filepath.Join(row.NewFolder, "01 - First Song.flac")
	later := time.Now().Add(time.Hour)
	if err := os.WriteFile(mine, []byte("my own edit"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(mine, later, later); err != nil {
		t.Fatal(err)
	}
	res, err := f.engine.Undo(context.Background(), f.plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || len(res[0].Problems) == 0 {
		t.Fatalf("undo should report the changed file: %+v", res)
	}
	// The changed file is kept (moved back with the folder), the original is back too.
	moved := filepath.Join(f.folder, "01 - First Song.flac")
	if data, err := os.ReadFile(moved); err != nil || string(data) != "my own edit" {
		t.Errorf("changed file: %q %v", data, err)
	}
	orig := filepath.Join(f.folder, "track one.flac")
	if got, err := os.ReadFile(orig); err != nil || !bytes.Equal(got, f.originals[orig]) {
		t.Errorf("original not back: %v", err)
	}
}

func TestPurgeAndUndoAfterIt(t *testing.T) {
	f := newFixture(t)
	row := f.row(t, Options{})
	if out := f.engine.ApplyAlbum(context.Background(), row, ""); out.Status != StatusApplied {
		t.Fatalf("apply: %+v", out)
	}
	before := RecycleSize(f.root)
	if before == 0 {
		t.Fatal("nothing kept")
	}
	// Not old enough yet.
	if freed, err := f.engine.Purge(time.Now().Add(-RecycleKeep)); err != nil || freed != 0 || RecycleSize(f.root) != before {
		t.Fatalf("purge too early: %d %v", freed, err)
	}
	// Thirty days later.
	if _, err := f.db.Exec(`UPDATE music_tidy_plans SET applied_at = '2020-01-02T03:04:05.000Z' WHERE id = ?`, f.plan); err != nil {
		t.Fatal(err)
	}
	freed, err := f.engine.Purge(time.Now().Add(-RecycleKeep))
	if err != nil || freed != before {
		t.Fatalf("purge: %d %v", freed, err)
	}
	if RecycleSize(f.root) != 0 {
		t.Error("still kept")
	}
	if _, err := os.Stat(filepath.Join(f.root, RecycleFolder)); !os.IsNotExist(err) {
		t.Errorf("empty recycle folder left: %v", err)
	}
	// Undo now reports it and the tidied files stay: nothing is lost.
	res, err := f.engine.Undo(context.Background(), f.plan)
	if err != nil || len(res) != 1 || len(res[0].Problems) == 0 {
		t.Fatalf("undo: %+v %v", res, err)
	}
	if !strings.Contains(strings.Join(res[0].Problems, " "), "no longer kept") {
		t.Errorf("problems: %v", res[0].Problems)
	}
	if n := audioFiles(t, f.root); n != 2 {
		t.Errorf("%d audio files, want the 2 tidied ones", n)
	}
}

func TestRewriteFailureLeavesTheOriginal(t *testing.T) {
	needFFmpeg(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "a.flac")
	makeAudio(t, src, "1")
	orig, _ := os.ReadFile(src)
	dst := TempName(src)
	// A cover that isn't a picture makes ffmpeg fail.
	bad := filepath.Join(dir, "bad.jpg")
	if err := os.WriteFile(bad, []byte("not a picture"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Rewrite(context.Background(), src, dst, map[string]string{TagTitle: "X"}, bad); err == nil {
		t.Fatal("expected a failure")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Errorf("the failed copy was left: %v", err)
	}
	if got, _ := os.ReadFile(src); !bytes.Equal(got, orig) {
		t.Error("the original changed")
	}
}

func TestRewriteKeepsTheFilesPermissions(t *testing.T) {
	needFFmpeg(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "a.flac")
	makeAudio(t, src, "1")
	if err := os.Chmod(src, 0o664); err != nil {
		t.Fatal(err)
	}
	dst := TempName(src)
	if err := Rewrite(context.Background(), src, dst, map[string]string{TagTitle: "X"}, ""); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dst)
	if err != nil || info.Mode().Perm() != 0o664 {
		t.Fatalf("mode %v, err %v", info.Mode().Perm(), err)
	}
}

func TestMoveNeverReplaces(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "sub", "b")
	if err := os.WriteFile(a, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Move(a, b); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a, []byte("again"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Move(a, b); err == nil {
		t.Fatal("replaced a file")
	}
	if got, _ := os.ReadFile(b); string(got) != "a" {
		t.Fatalf("b holds %q", got)
	}
}
