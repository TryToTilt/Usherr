package nsfw

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/rdborg/mediarium/internal/organizer"
)

func writeBytes(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// ---- fingerprints ----

func TestOSHashOnKnownBytes(t *testing.T) {
	dir := t.TempDir()
	hash := func(name string, data []byte) string {
		t.Helper()
		p := filepath.Join(dir, name)
		writeBytes(t, p, data)
		h, err := OSHash(p)
		if err != nil {
			t.Fatal(err)
		}
		return h
	}

	// Size 0x20000, first word 1, last word 2.
	two := make([]byte, 2*oshashChunk)
	binary.LittleEndian.PutUint64(two[0:], 1)
	binary.LittleEndian.PutUint64(two[len(two)-8:], 2)
	if got := hash("two", two); got != "0000000000020003" {
		t.Errorf("head + tail + size = %s", got)
	}

	// Bytes between the first and last 64 KiB don't count.
	big := make([]byte, 3*oshashChunk)
	binary.LittleEndian.PutUint64(big[0:], 1)
	binary.LittleEndian.PutUint64(big[len(big)-8:], 2)
	first := hash("big1", big)
	big[oshashChunk+100] = 0xff
	if second := hash("big2", big); first != second || first != "0000000000030003" {
		t.Errorf("the middle should not count: %s, %s", first, second)
	}

	// A file of exactly one chunk is its own head and tail.
	one := make([]byte, oshashChunk)
	binary.LittleEndian.PutUint64(one[0:], 5)
	if got := hash("one", one); got != "000000000001000a" {
		t.Errorf("one chunk = %s", got)
	}

	// Words are little-endian, and the sum wraps around.
	wrap := make([]byte, 2*oshashChunk)
	binary.LittleEndian.PutUint64(wrap[0:], ^uint64(0))
	if got := hash("wrap", wrap); got != "000000000001ffff" {
		t.Errorf("wrapped = %s", got)
	}
	order := make([]byte, 2*oshashChunk)
	order[1] = 1 // 0x0100 when read little-endian
	if got := hash("order", order); got != "0000000000020100" {
		t.Errorf("byte order = %s", got)
	}

	p := filepath.Join(dir, "small")
	writeBytes(t, p, []byte("too small"))
	if _, err := OSHash(p); err == nil {
		t.Error("a tiny file should be refused")
	}
}

func TestLengthAgreement(t *testing.T) {
	cases := []struct {
		file, stash int
		want        string
	}{
		{1800, 1800, LengthAgrees},
		{1810, 1800, LengthAgrees},  // 10 s exactly
		{1811, 1800, LengthDiffers}, // over 10 s, though under 5%
		{105, 100, LengthAgrees},    // 5% exactly
		{106, 100, LengthDiffers},   // over 5%, though under 10 s
		{94, 100, LengthDiffers},    // shorter counts too
		{0, 1800, LengthUnknown},    // the file's length couldn't be read
		{1800, 0, LengthUnknown},    // StashDB has none
		{-1, 1800, LengthUnknown},   // nonsense
		{7200, 7211, LengthDiffers}, // long films get no more slack than 10 s
		{7200, 7209, LengthAgrees},
	}
	for _, c := range cases {
		if got := LengthAgreement(c.file, c.stash); got != c.want {
			t.Errorf("LengthAgreement(%d, %d) = %s, want %s", c.file, c.stash, got, c.want)
		}
	}
}

func TestProposeMarksUnsure(t *testing.T) {
	one := Propose(1800, []FingerprintMatch{{Video: Video{StashID: "a", Duration: 1800}}})
	if len(one) != 1 || one[0].Unsure || one[0].Length != LengthAgrees || one[0].Matched != "OSHASH" {
		t.Fatalf("a single match of the right length is sure: %+v", one)
	}
	if off := Propose(1800, []FingerprintMatch{{Video: Video{StashID: "a", Duration: 1900}}}); !off[0].Unsure || off[0].Length != LengthDiffers {
		t.Fatalf("a length that disagrees is unsure: %+v", off)
	}
	if none := Propose(0, []FingerprintMatch{{Video: Video{StashID: "a", Duration: 1800}}}); !none[0].Unsure || none[0].Length != LengthUnknown {
		t.Fatalf("an unknown length is unsure: %+v", none)
	}
	// With no length on the video, the fingerprint's submitted length is used.
	if fp := Propose(1800, []FingerprintMatch{{Video: Video{StashID: "a"}, Duration: 1801}}); fp[0].Unsure || fp[0].StashLength != 1801 {
		t.Fatalf("the submitted length should do: %+v", fp)
	}
	// Two videos with one fingerprint: unsure, the agreeing one first, then most submissions.
	many := Propose(1800, []FingerprintMatch{
		{Video: Video{StashID: "wrong", Duration: 300}, Submissions: 50},
		{Video: Video{StashID: "few", Duration: 1800}, Submissions: 1},
		{Video: Video{StashID: "many", Duration: 1800}, Submissions: 9},
	})
	if many[0].Video.StashID != "many" || many[1].Video.StashID != "few" || many[2].Video.StashID != "wrong" {
		t.Fatalf("order: %+v", many)
	}
	for _, c := range many {
		if !c.Unsure {
			t.Fatalf("several videos for one fingerprint are all unsure: %+v", c)
		}
	}
}

func TestParseVideoProbe(t *testing.T) {
	sec, q, err := parseVideoProbe([]byte(`{"streams":[{"width":1920,"height":800}],"format":{"duration":"1799.6"}}`))
	if err != nil || sec != 1800 || q != "1080p" {
		t.Fatalf("got %d %q %v", sec, q, err)
	}
}

// fakeFingerprints answers findScenesBySceneFingerprints from a map of hash to
// video id, and records what each request asked.
type fakeFingerprints struct {
	mu       sync.Mutex
	matches  map[string]string
	requests [][]string
	times    []time.Time
	queries  []string
}

func (f *fakeFingerprints) server(t *testing.T) *StashBox {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Query     string `json:"query"`
			Variables struct {
				FP [][]struct {
					Hash      string `json:"hash"`
					Algorithm string `json:"algorithm"`
				} `json:"fp"`
			} `json:"variables"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("request: %v", err)
		}
		answer := make([][]map[string]any, 0, len(body.Variables.FP))
		var asked []string
		for _, list := range body.Variables.FP {
			if len(list) != 1 || list[0].Algorithm != "OSHASH" {
				t.Errorf("each file asks one OSHASH: %+v", list)
			}
			asked = append(asked, list[0].Hash)
			row := []map[string]any{}
			if id, ok := f.matches[list[0].Hash]; ok {
				row = append(row, map[string]any{"id": id, "title": "Placeholder " + id, "duration": 1800, "release_date": "2024-01-02",
					"studio": map[string]any{"id": "st", "name": "Example Studio", "aliases": []string{}, "images": []any{}}, "images": []any{}, "performers": []any{}, "tags": []any{},
					"fingerprints": []map[string]any{{"hash": list[0].Hash, "algorithm": "OSHASH", "duration": 1800, "submissions": 4}}})
			}
			answer = append(answer, row)
		}
		f.mu.Lock()
		f.requests = append(f.requests, asked)
		f.times = append(f.times, time.Now())
		f.queries = append(f.queries, body.Query)
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"findScenesBySceneFingerprints": answer}})
	}))
	t.Cleanup(srv.Close)
	return &StashBox{Endpoint: srv.URL, Key: func() string { return "k" }, HTTP: srv.Client()}
}

func TestFindByOSHashesKeepsOrderAndSkipsDeleted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"findScenesBySceneFingerprints":[
			[{"id":"gone","title":"Removed","deleted":true},{"id":"kept","title":"Kept","duration":60,"fingerprints":[
				{"hash":"other","algorithm":"OSHASH","duration":1,"submissions":1},
				{"hash":"aaaa","algorithm":"PHASH","duration":2,"submissions":2},
				{"hash":"aaaa","algorithm":"OSHASH","duration":61,"submissions":7}]}],
			[],
			[{"id":"third","title":"Third"}]]}}`))
	}))
	defer srv.Close()
	sb := &StashBox{Endpoint: srv.URL, Key: func() string { return "k" }, HTTP: srv.Client()}
	got, err := sb.FindByOSHashes(context.Background(), []string{"aaaa", "bbbb", "cccc"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || len(got[0]) != 1 || got[0][0].Video.StashID != "kept" || len(got[1]) != 0 || got[2][0].Video.StashID != "third" {
		t.Fatalf("mapping: %+v", got)
	}
	if got[0][0].Submissions != 7 || got[0][0].Duration != 61 {
		t.Fatalf("the OSHASH entry for this hash counts: %+v", got[0][0])
	}
	if _, err := sb.FindByOSHashes(context.Background(), []string{"only-two-expected", "x"}); err == nil {
		t.Fatal("an answer of the wrong length must not be mapped onto the wrong files")
	}
}

func makeVideos(t *testing.T, dir string, n int) []FoundFile {
	t.Helper()
	out := make([]FoundFile, 0, n)
	for i := 0; i < n; i++ {
		p := filepath.Join(dir, fmt.Sprintf("file-%03d.mp4", i))
		data := make([]byte, oshashChunk)
		binary.LittleEndian.PutUint64(data, uint64(i+1))
		writeBytes(t, p, data)
		st, _ := os.Stat(p)
		out = append(out, FoundFile{Path: p, Size: st.Size(), ModNs: st.ModTime().UnixNano()})
	}
	return out
}

func TestIdentifierBatchesPacesAndStores(t *testing.T) {
	r := testRepo(t)
	old := RequestGap
	RequestGap = 40 * time.Millisecond
	defer func() { RequestGap = old }()
	files := makeVideos(t, t.TempDir(), 60)
	matches := map[string]string{}
	for i, f := range files {
		h, err := OSHash(f.Path)
		if err != nil {
			t.Fatal(err)
		}
		if i%2 == 0 {
			matches[h] = fmt.Sprintf("video-%d", i)
		}
	}
	fake := &fakeFingerprints{matches: matches}
	steps := 0
	id := Identifier{Repo: r, Stash: fake.server(t), Step: func() { steps++ },
		Probe: func(context.Context, string) (int, string, error) { return 1800, "1080p", nil }}
	if err := id.Run(context.Background(), files); err != nil {
		t.Fatal(err)
	}
	if len(fake.requests) != 3 || len(fake.requests[0]) != IdentifyBatch || len(fake.requests[1]) != IdentifyBatch || len(fake.requests[2]) != 10 {
		t.Fatalf("batches: %d", len(fake.requests))
	}
	for i := 1; i < len(fake.times); i++ {
		if gap := fake.times[i].Sub(fake.times[i-1]); gap < RequestGap {
			t.Errorf("request %d came after only %v", i, gap)
		}
	}
	if !strings.Contains(fake.queries[0], "findScenesBySceneFingerprints(fingerprints: $fp)") {
		t.Errorf("query: %s", fake.queries[0])
	}
	if steps != 60 {
		t.Errorf("steps = %d", steps)
	}
	items, err := r.IdentifyItems()
	if err != nil || len(items) != 60 {
		t.Fatalf("stored %d, %v", len(items), err)
	}
	for _, it := range items {
		var n int
		if _, err := fmt.Sscanf(filepath.Base(it.Path), "file-%03d.mp4", &n); err != nil {
			t.Fatal(err)
		}
		if n%2 == 0 {
			if it.Status != IdentifyMatched || len(it.Candidates) != 1 || it.Candidates[0].Video.StashID != fmt.Sprintf("video-%d", n) {
				t.Errorf("%s was given %+v", it.Path, it)
			}
			if it.Candidates[0].Unsure || it.Quality != "1080p" || it.Duration != 1800 {
				t.Errorf("%s: %+v", it.Path, it)
			}
		} else if it.Status != IdentifyNone {
			t.Errorf("%s should have no match: %s", it.Path, it.Status)
		}
	}
	// Nothing is registered automatically.
	counts, err := r.Count()
	if err != nil || counts.Nsfw != 0 {
		t.Fatalf("nothing should be in the library yet: %+v, %v", counts, err)
	}
}

func TestIdentifierStopKeepsWhatItFound(t *testing.T) {
	r := testRepo(t)
	files := makeVideos(t, t.TempDir(), 30)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := 0
	fake := &fakeFingerprints{matches: map[string]string{}}
	id := Identifier{Repo: r, Stash: fake.server(t), Step: func() {
		if done++; done == IdentifyBatch {
			cancel()
		}
	}}
	if err := id.Run(ctx, files); err == nil {
		t.Fatal("a stopped run reports it")
	}
	if len(fake.requests) != 1 {
		t.Fatalf("no more questions after Stop: %d", len(fake.requests))
	}
	if items, _ := r.IdentifyItems(); len(items) != IdentifyBatch {
		t.Fatalf("kept %d", len(items))
	}
}

func TestScanFilesLeavesOutKnownLinksAndHidden(t *testing.T) {
	root := t.TempDir()
	mk := func(rel string) string {
		p := filepath.Join(root, rel)
		writeBytes(t, p, []byte("x"))
		return p
	}
	unknown := mk("Studio/unknown.mp4")
	mkv := mk("deep/er/second.MKV")
	known := mk("Studio/known.mp4")
	mk("Studio/notes.txt")
	mk("Studio/unknown.srt")
	mk(".hidden/skip.mp4")
	if err := os.Symlink(unknown, filepath.Join(root, "link.mp4")); err != nil {
		t.Skip("no links here")
	}
	got, err := ScanFiles(context.Background(), root, map[string]bool{known: true})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, f := range got {
		seen[f.Path] = true
	}
	if len(got) != 2 || !seen[unknown] || !seen[mkv] {
		t.Fatalf("got %+v", got)
	}
}

func identified(t *testing.T, r *Repo, path, stashID string, duration int) IdentifyItem {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	it := IdentifyItem{Path: path, Size: st.Size(), ModNs: st.ModTime().UnixNano(), Status: IdentifyMatched, Quality: "720p", Duration: duration,
		Candidates: Propose(duration, []FingerprintMatch{{Video: Video{StashID: stashID, Title: "Placeholder", StudioName: "Example Studio", Duration: duration, Wanted: true}}})}
	if err := r.addIdentifyItem(it); err != nil {
		t.Fatal(err)
	}
	items, err := r.IdentifyItems()
	if err != nil {
		t.Fatal(err)
	}
	for _, got := range items {
		if got.Path == path {
			return got
		}
	}
	t.Fatal("not stored")
	return it
}

func TestConfirmRegistersWhereItIsAndNeverWanted(t *testing.T) {
	r := testRepo(t)
	dir := t.TempDir()
	p := filepath.Join(dir, "some name.mp4")
	writeBytes(t, p, []byte("video"))
	it := identified(t, r, p, "scene-1", 1800)

	if blocked, err := r.RegisterFile(it, "not-a-candidate"); err != nil || blocked == "" {
		t.Fatalf("only a proposed video can be chosen: %q %v", blocked, err)
	}
	ok, err := r.ConfirmIdentify(it, "scene-1")
	if err != nil || !ok {
		t.Fatalf("confirm: %v %v", ok, err)
	}
	v, err := r.GetVideoByStashID("scene-1")
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != StatusDownloaded || v.FilePath != p || v.Quality != "720p" || v.Wanted || v.AddedBy != AddedImport {
		t.Fatalf("registered as %+v", v)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("the file must stay where it is: %v", err)
	}
	if wanted, _ := r.Wanted(); len(wanted) != 0 {
		t.Fatalf("nothing may become searchable: %+v", wanted)
	}
	// Twice: the row is no longer waiting.
	again, _ := r.IdentifyItems(it.ID)
	if again[0].Status != IdentifyRegistered {
		t.Fatalf("row = %+v", again[0])
	}
	if blocked, _ := r.RegisterFile(again[0], "scene-1"); blocked == "" {
		t.Fatal("a registered file can't be registered again")
	}
}

func TestConfirmIsBlockedSafely(t *testing.T) {
	r := testRepo(t)
	dir := t.TempDir()

	// The video already has another file.
	have := filepath.Join(dir, "have.mp4")
	writeBytes(t, have, []byte("first"))
	v, _, err := r.AddVideo(Video{StashID: "dup", Title: "T", StudioName: "S"})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.SetState(v.ID, StatusDownloaded, have, "1080p"); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(dir, "other.mp4")
	writeBytes(t, other, []byte("second"))
	it := identified(t, r, other, "dup", 60)
	if ok, err := r.ConfirmIdentify(it, "dup"); err != nil || ok {
		t.Fatalf("should be blocked: %v %v", ok, err)
	}
	got, _ := r.GetVideoByStashID("dup")
	if got.FilePath != have {
		t.Fatalf("the file it had must stay: %s", got.FilePath)
	}

	// The file changed since it was looked at.
	changed := filepath.Join(dir, "changed.mp4")
	writeBytes(t, changed, []byte("a"))
	it2 := identified(t, r, changed, "fresh", 60)
	writeBytes(t, changed, []byte("longer now"))
	if blocked, _ := r.RegisterFile(it2, "fresh"); blocked == "" {
		t.Fatal("a changed file must be refused")
	}
	if _, err := r.GetVideoByStashID("fresh"); err == nil {
		t.Fatal("nothing should have been added")
	}

	// The file is gone.
	gone := filepath.Join(dir, "gone.mp4")
	writeBytes(t, gone, []byte("a"))
	it3 := identified(t, r, gone, "fresh", 60)
	_ = os.Remove(gone)
	if blocked, _ := r.RegisterFile(it3, "fresh"); blocked == "" {
		t.Fatal("a missing file must be refused")
	}
}

func TestConfirmGivesAKnownFileItsID(t *testing.T) {
	r := testRepo(t)
	p := filepath.Join(t.TempDir(), "known.mp4")
	writeBytes(t, p, []byte("video"))
	res, err := r.db.Exec(`INSERT INTO nsfw_videos (stash_id, title, status, file_path) VALUES ('', 'No id yet', 'downloaded', ?)`, p)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	files, err := r.FilesWithoutStashID()
	if err != nil || len(files) != 1 || files[0].VideoID != id {
		t.Fatalf("files = %+v, %v", files, err)
	}
	it := identified(t, r, p, "adopted", 60)
	it.VideoID = id
	ok, err := r.ConfirmIdentify(it, "adopted")
	if err != nil || !ok {
		t.Fatalf("confirm: %v %v", ok, err)
	}
	v, err := r.GetVideo(id)
	if err != nil || v.StashID != "adopted" || v.FilePath != p || v.Status != StatusDownloaded {
		t.Fatalf("the same row should have the id: %+v %v", v, err)
	}
	if n, _ := r.Count(); n.Nsfw != 1 {
		t.Fatalf("no second row: %+v", n)
	}
}

// ---- favorites ----

func TestFavoritesReadsEveryPageOnlyWithTheFilter(t *testing.T) {
	old := RequestGap
	RequestGap = 0
	defer func() { RequestGap = old }()
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Query     string `json:"query"`
			Variables struct {
				Input map[string]any `json:"input"`
			} `json:"variables"`
		}
		_ = json.Unmarshal(raw, &body)
		queries = append(queries, body.Query)
		if body.Variables.Input["is_favorite"] != true {
			t.Errorf("no favorites filter: %v", body.Variables.Input)
		}
		page := int(body.Variables.Input["page"].(float64))
		if strings.Contains(body.Query, "queryPerformers") {
			list := []map[string]any{}
			for i := 0; i < favoritesPerPage; i++ {
				list = append(list, map[string]any{"id": fmt.Sprintf("p%d-%d", page, i), "name": "Placeholder"})
			}
			if page == 2 {
				list = list[:50]
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"queryPerformers": map[string]any{"count": 150, "performers": list}}})
			return
		}
		_, _ = w.Write([]byte(`{"data":{"queryStudios":{"count":1,"studios":[{"id":"s1","name":"Example Studio"}]}}}`))
	}))
	defer srv.Close()
	sb := &StashBox{Endpoint: srv.URL, Key: func() string { return "k" }, HTTP: srv.Client()}
	ps, err := sb.FavoritePerformers(context.Background())
	if err != nil || len(ps) != 150 {
		t.Fatalf("performers = %d, %v", len(ps), err)
	}
	ss, err := sb.FavoriteStudios(context.Background())
	if err != nil || len(ss) != 1 {
		t.Fatalf("studios = %d, %v", len(ss), err)
	}
	if len(queries) != 3 {
		t.Fatalf("requests = %d", len(queries))
	}
	for _, q := range queries {
		if strings.Contains(q, "mutation") || strings.Contains(q, "favoritePerformer") || strings.Contains(q, "favoriteStudio") {
			t.Fatalf("nothing may be written to StashDB: %s", q)
		}
	}
}

func TestFavoritesDiffOnlyAddsFollows(t *testing.T) {
	r := testRepo(t)
	followed, err := r.SavePerformer(Performer{StashID: "followed", Name: "Followed One"}, true)
	if err != nil {
		t.Fatal(err)
	}
	notFavorite, err := r.SavePerformer(Performer{StashID: "not-a-favorite", Name: "Other One"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.SavePerformer(Performer{StashID: "known", Name: "Known Only"}, false); err != nil {
		t.Fatal(err)
	}
	// Followed once and unfollowed by hand.
	left, err := r.SavePerformer(Performer{StashID: "unfollowed", Name: "Stopped Following"}, true)
	if err != nil {
		t.Fatal(err)
	}
	_ = r.PerformerChecked(left.ID)
	_ = r.SetPerformerFollowed(left.ID, false)

	side, err := r.DiffPerformerFavorites([]Performer{
		{StashID: "followed", Name: "Followed One"},
		{StashID: "known", Name: "Known Only"},
		{StashID: "unfollowed", Name: "Stopped Following"},
		{StashID: "new", Name: "New Favorite"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if side.Already != 1 || len(side.LeftAlone) != 1 || side.LeftAlone[0].StashID != "unfollowed" || len(side.ToFollow) != 2 || side.ToFollow[0].StashID != "known" || side.ToFollow[1].StashID != "new" {
		t.Fatalf("diff = %+v", side)
	}
	// Following them changes nobody else, and adds no video.
	for _, stash := range []string{"known", "new"} {
		if err := r.FollowPerformerFromNow(Performer{StashID: stash, Name: "Name"}); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := r.GetPerformer(notFavorite.ID); !got.Followed {
		t.Fatal("a follow that isn't a favorite must stay")
	}
	if got, _ := r.GetPerformer(followed.ID); !got.Followed {
		t.Fatal("a follow must stay")
	}
	if got, _ := r.GetPerformer(left.ID); got.Followed {
		t.Fatal("an unfollow by hand must stay")
	}
	list, _ := r.Performers(true)
	for _, p := range list {
		if (p.StashID == "known" || p.StashID == "new") && p.CheckedAt == "" {
			t.Fatalf("a new follow must count as checked, or the next check adds its whole catalogue: %+v", p)
		}
	}
	if n, _ := r.Count(); n.Nsfw != 0 || n.Wanted != 0 {
		t.Fatalf("no video may be added: %+v", n)
	}
}

func TestFavoriteStudioFollowsFromNow(t *testing.T) {
	r := testRepo(t)
	if err := r.FollowStudioFromNow(Studio{StashID: "s1", Name: "Example Studio"}); err != nil {
		t.Fatal(err)
	}
	list, err := r.Studios(true)
	if err != nil || len(list) != 1 || list[0].CheckedAt == "" {
		t.Fatalf("studios = %+v, %v", list, err)
	}
	side, err := r.DiffStudioFavorites([]Studio{{StashID: "s1", Name: "Example Studio"}, {StashID: "s2", Name: "Second Studio"}})
	if err != nil || side.Already != 1 || len(side.ToFollow) != 1 || side.ToFollow[0].StashID != "s2" {
		t.Fatalf("diff = %+v, %v", side, err)
	}
}

// ---- rename ----

func cleanName(s string) string { return organizer.Sanitize(s, organizer.SanitizeStrip, "") }

func TestNewFileStem(t *testing.T) {
	v := Video{StudioName: "Example Studio", ReleaseDate: "2024-03-15", Title: "Long Evening", Quality: "1080p"}
	if got := NewFileStem(v, cleanName); got != "Example Studio - 2024-03-15 - Long Evening [1080p]" {
		t.Errorf("stem = %q", got)
	}
	v.Quality, v.ReleaseDate = "", ""
	if got := NewFileStem(v, cleanName); got != "Example Studio - Long Evening" {
		t.Errorf("no date or quality = %q", got)
	}
	// Illegal characters go.
	if got := NewFileStem(Video{StudioName: `A/B:C`, Title: `What? "Yes"`, Quality: "720p"}, cleanName); strings.ContainsAny(got, `/\:?"`) {
		t.Errorf("stem = %q", got)
	}
	// A long title is cut to fit, at a character's edge, and the rest stays.
	long := Video{StudioName: "Example Studio", ReleaseDate: "2024-03-15", Title: strings.Repeat("título ", 80), Quality: "2160p"}
	got := NewFileStem(long, cleanName)
	if len(got) > maxStemBytes || !utf8.ValidString(got) || !strings.HasSuffix(got, " [2160p]") || !strings.HasPrefix(got, "Example Studio - 2024-03-15 - título") {
		t.Errorf("%d bytes: %q", len(got), got)
	}
}

// renameFixture is a library with one downloaded video.
func renameVideo(t *testing.T, r *Repo, stash, path string, tweak func(*Video)) Video {
	t.Helper()
	v := Video{StashID: stash, Title: "Long Evening", StudioName: "Example Studio", ReleaseDate: "2024-03-15"}
	if tweak != nil {
		tweak(&v)
	}
	saved, _, err := r.AddVideo(v)
	if err != nil {
		t.Fatal(err)
	}
	if path != "" {
		if err := r.SetState(saved.ID, StatusDownloaded, path, "1080p"); err != nil {
			t.Fatal(err)
		}
	}
	got, err := r.GetVideo(saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func checkNames(t *testing.T, r *Repo, root string) (RenamePlan, []RenameItem, int) {
	t.Helper()
	items, unchanged, err := r.PlanRename(context.Background(), root, cleanName, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	id, err := r.NewRenamePlan(root, unchanged, items)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := r.GetRenamePlan(id)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := r.RenameItems(id)
	if err != nil {
		t.Fatal(err)
	}
	return plan, stored, unchanged
}

func applyAll(t *testing.T, r *Repo, plan RenamePlan, items []RenameItem) {
	t.Helper()
	if err := r.MarkRenameApplied(plan.ID); err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.Status != ItemEligible {
			continue
		}
		if _, err := r.ApplyRename(context.Background(), plan, it); err != nil {
			t.Fatal(err)
		}
	}
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func TestRenamePlanNamesAndChangesNothing(t *testing.T) {
	r := testRepo(t)
	root := t.TempDir()
	old := filepath.Join(root, "incoming", "abc123.MP4")
	writeBytes(t, old, []byte("video"))
	v := renameVideo(t, r, "a", old, nil)

	_, items, unchanged := checkNames(t, r, root)
	if len(items) != 1 || unchanged != 0 {
		t.Fatalf("items = %+v", items)
	}
	want := filepath.Join(root, "Example Studio", "Example Studio - 2024-03-15 - Long Evening [1080p].mp4")
	if items[0].Status != ItemEligible || items[0].NewPath != want || items[0].OldPath != old || items[0].Outside {
		t.Fatalf("item = %+v", items[0])
	}
	if !exists(old) || exists(filepath.Dir(want)) {
		t.Fatal("a check must change nothing")
	}
	if got, _ := r.GetVideo(v.ID); got.FilePath != old {
		t.Fatal("a check must change nothing in the library")
	}
}

func TestRenamePlanLeavesAloneWithAReason(t *testing.T) {
	r := testRepo(t)
	root := t.TempDir()

	right := filepath.Join(root, "Example Studio", "Example Studio - 2024-03-15 - Named Right [1080p].mp4")
	writeBytes(t, right, []byte("right"))
	renameVideo(t, r, "right", right, func(v *Video) { v.Title = "Named Right" })

	gone := renameVideo(t, r, "gone", filepath.Join(root, "gone.mp4"), func(v *Video) { v.Title = "Gone" })

	noTitle := filepath.Join(root, "untitled.mp4")
	writeBytes(t, noTitle, []byte("x"))
	blank := renameVideo(t, r, "blank", noTitle, func(v *Video) { v.Title = "" })

	real := filepath.Join(root, "real.mp4")
	writeBytes(t, real, []byte("x"))
	link := filepath.Join(root, "link.mp4")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("no links here")
	}
	linked := renameVideo(t, r, "link", link, func(v *Video) { v.Title = "Linked" })

	notDone := renameVideo(t, r, "wanted", "", func(v *Video) { v.Title = "Not Downloaded" })

	_, items, unchanged := checkNames(t, r, root)
	if unchanged != 1 {
		t.Fatalf("unchanged = %d", unchanged)
	}
	by := map[int64]RenameItem{}
	for _, it := range items {
		by[it.VideoID] = it
	}
	for _, v := range []Video{gone, blank, linked} {
		if it := by[v.ID]; it.Status != ItemBlocked || it.Reason == "" {
			t.Errorf("%s: %+v", v.StashID, it)
		}
	}
	if _, listed := by[notDone.ID]; listed {
		t.Error("only downloaded videos are renamed")
	}
}

func TestRenameCollisionsAreBlocked(t *testing.T) {
	r := testRepo(t)
	root := t.TempDir()

	// A file is already at the new name.
	first := filepath.Join(root, "first.mp4")
	writeBytes(t, first, []byte("first"))
	target := filepath.Join(root, "Example Studio", "Example Studio - 2024-03-15 - Long Evening [1080p].mp4")
	writeBytes(t, target, []byte("someone else's file"))
	renameVideo(t, r, "first", first, nil)

	// Two videos would get one name, differing only in letter case.
	a := filepath.Join(root, "a.mp4")
	b := filepath.Join(root, "b.mp4")
	writeBytes(t, a, []byte("a"))
	writeBytes(t, b, []byte("b"))
	renameVideo(t, r, "a", a, func(v *Video) { v.Title = "Same Name" })
	renameVideo(t, r, "b", b, func(v *Video) { v.Title = "SAME name" })

	plan, items, _ := checkNames(t, r, root)
	if items[0].Status != ItemBlocked || !strings.Contains(items[0].Reason, "already there") {
		t.Errorf("taken name: %+v", items[0])
	}
	if items[1].Status != ItemEligible {
		t.Errorf("the first of two takes the name: %+v", items[1])
	}
	if items[2].Status != ItemBlocked || !strings.Contains(items[2].Reason, "same name") {
		t.Errorf("the second of two: %+v", items[2])
	}

	// A file that turns up at the new name after the check is not overwritten.
	writeBytes(t, items[1].NewPath, []byte("arrived later"))
	status, err := r.ApplyRename(context.Background(), plan, items[1])
	if err != nil || status != ItemBlocked {
		t.Fatalf("late collision: %s %v", status, err)
	}
	if readFile(t, items[1].NewPath) != "arrived later" || readFile(t, a) != "a" {
		t.Fatal("neither file may change")
	}
	if readFile(t, target) != "someone else's file" {
		t.Fatal("the file that was there must stay")
	}
}

func TestFindSidecarsBelongToTheirVideo(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"Night.mkv", "Night.part2.mkv", "Night.srt", "Night.en.srt", "Night.EN.FORCED.SUB", "Night.nfo", "Night.part2.srt", "Night.part2.en.srt", "Nightly.srt", "Night.jpg", "Night-poster.nfo"} {
		writeBytes(t, filepath.Join(dir, name), []byte("x"))
	}
	if err := os.Mkdir(filepath.Join(dir, "Night.d.srt"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := FindSidecars(filepath.Join(dir, "Night.mkv"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range got {
		names = append(names, filepath.Base(p))
	}
	if strings.Join(names, ",") != "Night.EN.FORCED.SUB,Night.en.srt,Night.nfo,Night.srt" {
		t.Fatalf("sidecars = %v", names)
	}
	second, _ := FindSidecars(filepath.Join(dir, "Night.part2.mkv"))
	if len(second) != 2 {
		t.Fatalf("second = %v", second)
	}
}

func TestApplyAndUndoRoundTrip(t *testing.T) {
	r := testRepo(t)
	root := t.TempDir()
	dir := filepath.Join(root, "old folder")
	old := filepath.Join(dir, "abc.mkv")
	srt := filepath.Join(dir, "abc.en.srt")
	nfo := filepath.Join(dir, "abc.nfo")
	unrelated := filepath.Join(dir, "abc2.srt")
	writeBytes(t, old, []byte("the video"))
	writeBytes(t, srt, []byte("subtitle"))
	writeBytes(t, nfo, []byte("<nfo/>"))
	writeBytes(t, unrelated, []byte("not its subtitle"))
	v := renameVideo(t, r, "a", old, nil)

	plan, items, _ := checkNames(t, r, root)
	if len(items) != 1 || len(items[0].Sidecars) != 2 {
		t.Fatalf("items = %+v", items)
	}
	newPath := items[0].NewPath
	newStem := strings.TrimSuffix(newPath, ".mkv")
	applyAll(t, r, plan, items)

	if readFile(t, newPath) != "the video" || readFile(t, newStem+".en.srt") != "subtitle" || readFile(t, newStem+".nfo") != "<nfo/>" {
		t.Fatal("the files should be under their new names")
	}
	if exists(old) || exists(srt) || exists(nfo) {
		t.Fatal("the old names should be gone")
	}
	if readFile(t, unrelated) != "not its subtitle" {
		t.Fatal("another file must stay")
	}
	got, _ := r.GetVideo(v.ID)
	if got.FilePath != newPath {
		t.Fatalf("file path = %s", got.FilePath)
	}
	stored, _ := r.RenameItems(plan.ID)
	if stored[0].Status != ItemApplied {
		t.Fatalf("status = %s", stored[0].Status)
	}

	res, paths, err := r.UndoRename(context.Background(), plan.ID, nil, nil)
	if err != nil || len(res) != 1 || !res[0].Done {
		t.Fatalf("undo: %+v %v", res, err)
	}
	if readFile(t, old) != "the video" || readFile(t, srt) != "subtitle" || readFile(t, nfo) != "<nfo/>" {
		t.Fatal("every file should be back")
	}
	if exists(newPath) || exists(filepath.Dir(newPath)) {
		t.Fatal("the new name and the folder made for it should be gone")
	}
	if !exists(dir) {
		t.Fatal("the old folder must stay")
	}
	got, _ = r.GetVideo(v.ID)
	if got.FilePath != old || got.Status != StatusDownloaded {
		t.Fatalf("file path = %s", got.FilePath)
	}
	if len(paths) == 0 {
		t.Fatal("the media servers should hear about it")
	}
	after, _ := r.GetRenamePlan(plan.ID)
	if after.Status != RenameUndone {
		t.Fatalf("plan = %+v", after)
	}
	// Doing it twice changes nothing.
	if res, _, err := r.UndoRename(context.Background(), plan.ID, nil, nil); err != nil || len(res) != 0 {
		t.Fatalf("second undo: %+v %v", res, err)
	}
}

func TestUndoKeepsAFolderWithOtherFiles(t *testing.T) {
	r := testRepo(t)
	root := t.TempDir()
	old := filepath.Join(root, "abc.mp4")
	writeBytes(t, old, []byte("v"))
	renameVideo(t, r, "a", old, nil)
	plan, items, _ := checkNames(t, r, root)
	applyAll(t, r, plan, items)
	extra := filepath.Join(filepath.Dir(items[0].NewPath), "someone else.txt")
	writeBytes(t, extra, []byte("keep me"))
	if res, _, err := r.UndoRename(context.Background(), plan.ID, nil, nil); err != nil || !res[0].Done {
		t.Fatalf("undo: %+v %v", res, err)
	}
	if readFile(t, extra) != "keep me" {
		t.Fatal("a folder with other files stays, with them in it")
	}
}

func TestUndoNeverRemovesAFileWhereAFolderWas(t *testing.T) {
	r := testRepo(t)
	root := t.TempDir()
	old := filepath.Join(root, "abc.mp4")
	writeBytes(t, old, []byte("v"))
	renameVideo(t, r, "a", old, nil)
	plan, items, _ := checkNames(t, r, root)
	applyAll(t, r, plan, items)
	if res, _, err := r.UndoRename(context.Background(), plan.ID, nil, nil); err != nil || !res[0].Done {
		t.Fatalf("undo: %+v %v", res, err)
	}
	// As if the folder was gone and a file took its name before a second try.
	made := filepath.Dir(items[0].NewPath)
	writeBytes(t, made, []byte("a file, not a folder"))
	if removeEmptyDir(made) {
		t.Fatal("a file isn't an empty folder")
	}
	if readFile(t, made) != "a file, not a folder" {
		t.Fatal("the file must stay")
	}
	if !removeEmptyDir(filepath.Join(root, "never there")) {
		t.Fatal("a folder that is gone counts as removed")
	}
}

func TestUndoNeverOverwritesOrLosesTrack(t *testing.T) {
	r := testRepo(t)
	root := t.TempDir()

	// Something else took the old name.
	takenOld := filepath.Join(root, "taken.mp4")
	writeBytes(t, takenOld, []byte("v1"))
	a := renameVideo(t, r, "a", takenOld, func(v *Video) { v.Title = "Taken" })
	// The file was changed after the rename.
	changedOld := filepath.Join(root, "changed.mp4")
	writeBytes(t, changedOld, []byte("v2"))
	b := renameVideo(t, r, "b", changedOld, func(v *Video) { v.Title = "Changed" })
	// The video has another file by now.
	otherOld := filepath.Join(root, "other.mp4")
	writeBytes(t, otherOld, []byte("v3"))
	c := renameVideo(t, r, "c", otherOld, func(v *Video) { v.Title = "Other" })

	plan, items, _ := checkNames(t, r, root)
	applyAll(t, r, plan, items)

	writeBytes(t, takenOld, []byte("newcomer"))
	writeBytes(t, items[1].NewPath, []byte("re-encoded, longer"))
	third := filepath.Join(root, "third.mp4")
	writeBytes(t, third, []byte("replacement"))
	if err := r.SetState(c.ID, StatusDownloaded, third, "1080p"); err != nil {
		t.Fatal(err)
	}

	res, _, err := r.UndoRename(context.Background(), plan.ID, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range res {
		if u.Done || u.Note == "" {
			t.Errorf("%+v should stay, with a reason", u)
		}
	}
	if readFile(t, takenOld) != "newcomer" || readFile(t, items[0].NewPath) != "v1" {
		t.Fatal("neither file may be overwritten")
	}
	if readFile(t, items[1].NewPath) != "re-encoded, longer" || exists(changedOld) {
		t.Fatal("a changed file stays")
	}
	if readFile(t, items[2].NewPath) != "v3" {
		t.Fatal("the file of a video with another file stays")
	}
	for _, chk := range []struct {
		id   int64
		want string
	}{{a.ID, items[0].NewPath}, {b.ID, items[1].NewPath}, {c.ID, third}} {
		if got, _ := r.GetVideo(chk.id); got.FilePath != chk.want {
			t.Errorf("video %d file = %s, want %s", chk.id, got.FilePath, chk.want)
		}
	}
	if after, _ := r.GetRenamePlan(plan.ID); after.Status != RenameApplied {
		t.Fatalf("undo stays on offer: %+v", after)
	}

	// With the way clear, undo finishes what it could not do.
	if err := os.Remove(takenOld); err != nil {
		t.Fatal(err)
	}
	res, _, err = r.UndoRename(context.Background(), plan.ID, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	done := 0
	for _, u := range res {
		if u.Done {
			done++
		}
	}
	if done != 1 || readFile(t, takenOld) != "v1" {
		t.Fatalf("second undo: %+v", res)
	}
	if got, _ := r.GetVideo(a.ID); got.FilePath != takenOld {
		t.Fatalf("file = %s", got.FilePath)
	}
}

func TestUndoAfterACrashHalfway(t *testing.T) {
	r := testRepo(t)
	root := t.TempDir()
	old := filepath.Join(root, "abc.mp4")
	srt := filepath.Join(root, "abc.srt")
	writeBytes(t, old, []byte("v"))
	writeBytes(t, srt, []byte("s"))
	v := renameVideo(t, r, "a", old, nil)
	plan, items, _ := checkNames(t, r, root)
	it := items[0]
	if err := r.MarkRenameApplied(plan.ID); err != nil {
		t.Fatal(err)
	}
	// As if Usherr stopped after moving the video, before the library was
	// changed, with the subtitle's step written but not taken.
	if err := r.setItemStatus(it.ID, ItemApplying, ""); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(old)
	if _, err := r.journal(plan.ID, it.ID, stepMove, true, old, it.NewPath, st.Size(), st.ModTime().UnixNano()); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(it.NewPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(old, it.NewPath); err != nil {
		t.Fatal(err)
	}
	if _, err := r.journal(plan.ID, it.ID, stepMove, false, srt, it.Sidecars[0].New, 1, 1); err != nil {
		t.Fatal(err)
	}

	res, _, err := r.UndoRename(context.Background(), plan.ID, nil, nil)
	if err != nil || len(res) != 1 || !res[0].Done {
		t.Fatalf("undo: %+v %v", res, err)
	}
	if readFile(t, old) != "v" || readFile(t, srt) != "s" || exists(it.NewPath) {
		t.Fatal("everything should be back")
	}
	if got, _ := r.GetVideo(v.ID); got.FilePath != old {
		t.Fatalf("file = %s", got.FilePath)
	}
}

func TestApplyIsStaleWhenTheFileChanged(t *testing.T) {
	r := testRepo(t)
	root := t.TempDir()
	old := filepath.Join(root, "abc.mp4")
	writeBytes(t, old, []byte("v"))
	renameVideo(t, r, "a", old, nil)
	plan, items, _ := checkNames(t, r, root)
	writeBytes(t, old, []byte("a new, longer download"))
	status, err := r.ApplyRename(context.Background(), plan, items[0])
	if err != nil || status != ItemStale {
		t.Fatalf("status = %s, %v", status, err)
	}
	if !exists(old) || exists(items[0].NewPath) {
		t.Fatal("a changed file isn't touched")
	}
}

func TestApplyLeavesARunningDownloadAlone(t *testing.T) {
	r := testRepo(t)
	root := t.TempDir()
	old := filepath.Join(root, "abc.mp4")
	writeBytes(t, old, []byte("v"))
	v := renameVideo(t, r, "a", old, nil)
	plan, items, _ := checkNames(t, r, root)
	if items[0].Status != ItemEligible {
		t.Fatalf("item = %+v", items[0])
	}
	if _, err := r.db.Exec(`INSERT INTO download_queue (id, release_title, nzb_url, status) VALUES (900, 'x', 'u', 'downloading')`); err != nil {
		t.Skip("queue table differs: ", err)
	}
	if err := r.LinkDownload(900, v.ID); err != nil {
		t.Fatal(err)
	}
	status, err := r.ApplyRename(context.Background(), plan, items[0])
	if err != nil || status != ItemBlocked || !exists(old) {
		t.Fatalf("status = %s, %v", status, err)
	}
}

func TestRenameFromOutsideTheFolder(t *testing.T) {
	r := testRepo(t)
	base := t.TempDir()
	root := filepath.Join(base, "nsfw")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(base, "whisparr", "movies", "abc.mp4")
	writeBytes(t, old, []byte("v"))
	renameVideo(t, r, "a", old, nil)
	plan, items, _ := checkNames(t, r, root)
	if !items[0].Outside || items[0].Status != ItemEligible {
		t.Fatalf("on the same drive it may move: %+v", items[0])
	}
	applyAll(t, r, plan, items)
	if !exists(items[0].NewPath) || exists(old) {
		t.Fatal("it should have moved")
	}
	if res, _, err := r.UndoRename(context.Background(), plan.ID, nil, nil); err != nil || !res[0].Done || !exists(old) {
		t.Fatalf("undo: %+v %v", res, err)
	}
}

func TestRenameFromAnotherDriveIsLeftAlone(t *testing.T) {
	const shm = "/dev/shm"
	if st, err := os.Stat(shm); err != nil || !st.IsDir() {
		t.Skip("no second file system here")
	}
	other, err := os.MkdirTemp(shm, "usherr-test-")
	if err != nil {
		t.Skip("can't write to a second file system")
	}
	defer os.RemoveAll(other)
	root := t.TempDir()
	if same, supported, err := organizer.SameFilesystem(other, root); err != nil || !supported || same {
		t.Skip("not a second file system")
	}
	r := testRepo(t)
	old := filepath.Join(other, "abc.mp4")
	writeBytes(t, old, []byte("v"))
	renameVideo(t, r, "a", old, nil)
	plan, items, _ := checkNames(t, r, root)
	if items[0].Status != ItemBlocked || !strings.Contains(items[0].Reason, "another drive") {
		t.Fatalf("item = %+v", items[0])
	}
	// Even if asked to, it refuses rather than copy.
	items[0].Status = ItemEligible
	if err := r.MarkRenameApplied(plan.ID); err != nil {
		t.Fatal(err)
	}
	items[0].NewPath = filepath.Join(root, "Example Studio", "name.mp4")
	status, err := r.ApplyRename(context.Background(), plan, items[0])
	if err != nil || status != ItemBlocked {
		t.Fatalf("status = %s, %v", status, err)
	}
	if !exists(old) || exists(items[0].NewPath) || exists(filepath.Dir(items[0].NewPath)) {
		t.Fatal("nothing may change, and no copy be made")
	}
}

func TestPlanSettlesWhenNothingWasRenamed(t *testing.T) {
	r := testRepo(t)
	root := t.TempDir()
	old := filepath.Join(root, "abc.mp4")
	writeBytes(t, old, []byte("v"))
	renameVideo(t, r, "a", old, nil)
	plan, items, _ := checkNames(t, r, root)
	writeBytes(t, old, []byte("changed since"))
	if err := r.MarkRenameApplied(plan.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ApplyRename(context.Background(), plan, items[0]); err != nil {
		t.Fatal(err)
	}
	if err := r.SettleRenamePlan(plan.ID); err != nil {
		t.Fatal(err)
	}
	if after, _ := r.GetRenamePlan(plan.ID); after.Status != RenamePreview {
		t.Fatalf("there is nothing to undo: %+v", after)
	}
}
