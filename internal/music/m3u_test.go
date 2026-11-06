package music_test

import (
	"testing"

	"github.com/rdborg/mediarium/internal/music"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/unicode"
)

func TestParseM3UReadsExtinfPathsAndSkipsAddresses(t *testing.T) {
	src := "\ufeff#EXTM3U\r\n#PLAYLIST:Road Trip\r\n#EXTINF:215,Fixture Band - Opening\r\n01 - Opening.flac\r\n" +
		"\r\n# a comment\r\nhttp://radio.example/stream\r\n#EXTINF:-1 tvg-name=\"a,b\",Just A Title\r\nsub\\02 - Middle Song.flac\r\n" +
		"file:///C:/Music/Fixture%20Band/03%20-%20Closing%20Time.flac\r\n#EXTINF:12.6,Lonely\r\n"
	got := music.ParseM3U([]byte(src))
	if got.Name != "Road Trip" {
		t.Fatalf("name: %q", got.Name)
	}
	if len(got.Entries) != 3 {
		t.Fatalf("entries: %+v", got.Entries)
	}
	e := got.Entries[0]
	if e.Path != "01 - Opening.flac" || e.Artist != "Fixture Band" || e.Title != "Opening" || e.Duration != 215 || e.Line != 4 {
		t.Fatalf("first: %+v", e)
	}
	if e := got.Entries[1]; e.Path != `sub\02 - Middle Song.flac` || e.Title != "Just A Title" || e.Artist != "" || e.Duration != -1 {
		t.Fatalf("attributes with a quoted comma, negative length: %+v", e)
	}
	if e := got.Entries[2]; e.Path != "C:/Music/Fixture Band/03 - Closing Time.flac" || e.Duration != -1 || e.Title != "" {
		t.Fatalf("file address: %+v", e)
	}
	if d := got.Entries[2].Display(); d != "03 - Closing Time.flac" {
		t.Fatalf("display falls back to the file name: %q", d)
	}
}

func TestDecodeTextHandlesBOMsAndOldWindowsFiles(t *testing.T) {
	want := "Beyoncé - Café.flac"
	cp, _ := charmap.Windows1252.NewEncoder().String(want)
	le, _ := unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewEncoder().String(want)
	be, _ := unicode.UTF16(unicode.BigEndian, unicode.UseBOM).NewEncoder().String(want)
	for name, in := range map[string]string{
		"utf-8":        want,
		"utf-8 bom":    "\xEF\xBB\xBF" + want,
		"windows-1252": cp,
		"utf-16 le":    le,
		"utf-16 be":    be,
	} {
		if got := music.DecodeText([]byte(in)); got != want {
			t.Errorf("%s: %q", name, got)
		}
	}
	lines, _ := unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewEncoder().String("#EXTM3U\r\n" + want + "\r\n")
	got := music.ParseM3U([]byte(lines))
	if len(got.Entries) != 1 || got.Entries[0].Path != want {
		t.Fatalf("a UTF-16 playlist: %+v", got.Entries)
	}
}

func TestResolveM3UPath(t *testing.T) {
	for _, tc := range []struct{ dir, in, want string }{
		{"/music/Band/Album", "01.flac", "/music/Band/Album/01.flac"},
		{"/music/Band/Album", `CD1\01.flac`, "/music/Band/Album/CD1/01.flac"},
		{"/music/Band/Album", "../Other/02.flac", "/music/Band/Other/02.flac"},
		{"/music/Band/Album", "/music/x/03.flac", "/music/x/03.flac"},
		{"/music/Band/Album", `E:\Music\Band\04.flac`, "E:/Music/Band/04.flac"},
		{"/music/Band/Album", `\\nas\share\05.flac`, "/nas/share/05.flac"},
	} {
		if got := music.ResolveM3UPath(tc.dir, tc.in); got != tc.want {
			t.Errorf("%q in %q: %q, want %q", tc.in, tc.dir, got, tc.want)
		}
	}
	if !music.IsAbsM3UPath(`C:\x.flac`) || !music.IsAbsM3UPath("/x.flac") || music.IsAbsM3UPath("x.flac") {
		t.Fatal("IsAbsM3UPath")
	}
}

func TestM3UMatcherByPathTailAndTags(t *testing.T) {
	lib := []music.LibraryTrack{
		{ID: 1, Path: "/music/Fixture Band/First Album/01 - Opening.flac", Title: "Opening", Artist: "Fixture Band", LengthMs: 215000},
		{ID: 2, Path: "/music/Fixture Band/First Album/CD1/02 - Middle Song.flac", Title: "Middle Song", Artist: "Fixture Band", LengthMs: 180000},
		{ID: 3, Path: "/music/Fixture Band/Live/01 - Opening.flac", Title: "Opening", Artist: "Fixture Band", LengthMs: 260000},
		{ID: 4, Path: `/music/Other/Album/07 - Café.flac`, Title: "Café", Artist: "Other Act", LengthMs: 200000},
	}
	m := music.NewM3UMatcher(lib)
	dir := "/music/Fixture Band/First Album"
	for _, tc := range []struct {
		name string
		e    music.M3UEntry
		id   int64
		how  string
	}{
		{"relative path", music.M3UEntry{Path: "01 - Opening.flac", Duration: -1}, 1, music.MatchPath},
		{"case differs", music.M3UEntry{Path: "01 - OPENING.flac", Duration: -1}, 1, music.MatchPath},
		{"windows path of another computer", music.M3UEntry{Path: `D:\Old\Music\Other\Album\07 - Café.flac`, Duration: -1}, 4, music.MatchTail},
		{"file in a disc folder the playlist leaves out", music.M3UEntry{Path: "02 - Middle Song.flac", Duration: -1}, 2, music.MatchTail},
		{"same file name twice, the one under the playlist's folder", music.M3UEntry{Path: "elsewhere/01 - Opening.flac", Duration: -1}, 1, music.MatchTail},
		{"tags and length pick the live one", music.M3UEntry{Path: "gone.mp3", Artist: "fixture band", Title: "opening", Duration: 259}, 3, music.MatchTags},
		{"tags with accents folded", music.M3UEntry{Path: "gone.mp3", Artist: "Other Act", Title: "Cafe", Duration: -1}, 4, music.MatchTags},
		{"title alone needs a length", music.M3UEntry{Path: "gone.mp3", Title: "Middle Song", Duration: -1}, 0, ""},
		{"title alone with a length", music.M3UEntry{Path: "gone.mp3", Title: "Middle Song", Duration: 181}, 2, music.MatchTags},
		{"length too far off", music.M3UEntry{Path: "gone.mp3", Artist: "Other Act", Title: "Café", Duration: 100}, 0, ""},
		{"nothing like it", music.M3UEntry{Path: "nope.flac", Duration: -1}, 0, ""},
	} {
		id, how := m.Match(dir, tc.e)
		if id != tc.id || how != tc.how {
			t.Errorf("%s: got %d %q, want %d %q", tc.name, id, how, tc.id, tc.how)
		}
	}
}
