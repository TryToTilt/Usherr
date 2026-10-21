package music

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A FLAC file that is only its header followed by zeros (an interrupted
// copy) reads as endless empty metadata blocks. Its tags are given up on
// once the read limit is reached, instead of walking the whole file.
func TestFileTagReaderGivesUpOnAZeroFilledFLAC(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "02 Joga.flac")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("fLaC")); err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(TagReadLimit + 8<<20); err != nil {
		t.Fatal(err)
	}
	f.Close()

	start := time.Now()
	_, ok, err := FileTagReader{}.ReadTags(p)
	if ok || !errors.Is(err, ErrTagReadLimit) {
		t.Fatalf("want ErrTagReadLimit, got ok=%v err=%v", ok, err)
	}
	// The reader itself gives up after TagReadTimeout (5 s), so a loaded
	// machine can legitimately take that long; the limit only has to catch a
	// loop that never ends.
	if took := time.Since(start); took > 15*time.Second*slowdown {
		t.Errorf("giving up took %v", took)
	}
	// The file is still identified, by its name.
	if info := IdentifyFile(p, FileTagReader{}); info.Track != 2 || info.Title != "Joga" {
		t.Errorf("by name: %+v", info)
	}
}

func TestFileTagReaderStopsAtTheDeadline(t *testing.T) {
	dir := t.TempDir()
	p := writeFixture(t, dir, "a.flac", buildFLAC(flacSpec{sampleRate: 44100, channels: 2, bps: 16, seconds: 1, audioBytes: 10,
		comments: []string{"TITLE=Lucky"}}))
	old := TagReadTimeout
	defer func() { TagReadTimeout = old }()
	TagReadTimeout = -time.Second
	if _, _, err := (FileTagReader{}).ReadTags(p); !errors.Is(err, ErrTagReadLimit) {
		t.Fatalf("want ErrTagReadLimit past the deadline, got %v", err)
	}
	TagReadTimeout = old
	if info, ok, err := (FileTagReader{}).ReadTags(p); err != nil || !ok || info.Title != "Lucky" {
		t.Fatalf("within the limits: %+v ok=%v err=%v", info, ok, err)
	}
}

func TestFileTagReaderReadsFLACAndMP3(t *testing.T) {
	dir := t.TempDir()
	flac := writeFixture(t, dir, "a.flac", buildFLAC(flacSpec{
		sampleRate: 44100, channels: 2, bps: 16, seconds: 10, audioBytes: 100, picture: true,
		comments: []string{"TITLE=Paranoid Android", "ARTIST=Radiohead", "ALBUMARTIST=Radiohead", "ALBUM=OK Computer", "TRACKNUMBER=2", "DISCNUMBER=1", "DATE=1997"},
	}))
	mp3 := writeFixture(t, dir, "b.mp3", buildMP3CBR(14, 5, id3v2([][2]string{
		{"TIT2", "Lucky"}, {"TPE1", "Radiohead"}, {"TPE2", "Radiohead"}, {"TALB", "OK Computer"}, {"TRCK", "10/12"}, {"TPOS", "2/2"}, {"TYER", "1997"},
	}), false))

	before := map[string][]byte{}
	for _, p := range []string{flac, mp3} {
		before[p], _ = os.ReadFile(p)
	}

	got, ok, err := FileTagReader{}.ReadTags(flac)
	want := FileInfo{Path: flac, Disc: 1, Track: 2, Title: "Paranoid Android", Artist: "Radiohead", AlbumArtist: "Radiohead", Album: "OK Computer", Year: 1997, HasPicture: true, FileType: "FLAC"}
	if err != nil || !ok || got != want {
		t.Fatalf("flac: %+v ok=%v err=%v\nwant %+v", got, ok, err, want)
	}
	got, ok, err = FileTagReader{}.ReadTags(mp3)
	want = FileInfo{Path: mp3, Disc: 2, Track: 10, Title: "Lucky", Artist: "Radiohead", AlbumArtist: "Radiohead", Album: "OK Computer", Year: 1997, FileType: "MP3"}
	if err != nil || !ok || got != want {
		t.Fatalf("mp3: %+v ok=%v err=%v\nwant %+v", got, ok, err, want)
	}

	// Reading never changes a file.
	for p, b := range before {
		if now, _ := os.ReadFile(p); string(now) != string(b) {
			t.Errorf("%s was modified by reading its tags", p)
		}
	}
}

func TestFileTagReaderWithoutTagsOrWithDamagedFiles(t *testing.T) {
	dir := t.TempDir()
	plain := writeFixture(t, dir, "plain.flac", buildFLAC(flacSpec{sampleRate: 44100, channels: 2, bps: 16, seconds: 1, audioBytes: 10}))
	info, ok, err := FileTagReader{}.ReadTags(plain)
	if err != nil || (ok && info.Title != "") {
		t.Fatalf("a tagless FLAC has nothing to report: %+v ok=%v err=%v", info, ok, err)
	}
	junk := writeFixture(t, dir, "junk.mp3", []byte("no tags anywhere in here, just text padding "))
	if _, ok, _ := (FileTagReader{}).ReadTags(junk); ok {
		t.Fatal("junk has no tags")
	}
	if _, _, err := (FileTagReader{}).ReadTags(filepath.Join(dir, "missing.mp3")); err == nil {
		t.Fatal("a missing file is an error")
	}
}

func TestIdentifyFileUsesTagsOverTheFileName(t *testing.T) {
	dir := t.TempDir()
	p := writeFixture(t, dir, "random name.flac", buildFLAC(flacSpec{
		sampleRate: 44100, channels: 2, bps: 16, seconds: 1, audioBytes: 10,
		comments: []string{"TITLE=Airbag", "TRACKNUMBER=1", "ALBUM=OK Computer", "ARTIST=Radiohead"},
	}))
	got := IdentifyFile(p, FileTagReader{})
	if got.Track != 1 || got.Title != "Airbag" || got.Album != "OK Computer" || got.Artist != "Radiohead" {
		t.Fatalf("%+v", got)
	}
	// Without a reader the same file is only what its name says.
	if got := IdentifyFile(p, nil); got.Track != 0 || got.Album != "" || got.Title != "random name" {
		t.Fatalf("%+v", got)
	}
}

func TestConsensusOf(t *testing.T) {
	tr := func(albumArtist, artist, album string, year int) FileInfo {
		return FileInfo{AlbumArtist: albumArtist, Artist: artist, Album: album, Year: year}
	}
	cases := []struct {
		name  string
		files []FileInfo
		want  TagConsensus
	}{
		{"agreeing files", []FileInfo{tr("Radiohead", "", "OK Computer", 1997), tr("Radiohead", "", "OK Computer", 1997)}, TagConsensus{"Radiohead", "OK Computer", 1997}},
		{"album artist beats track artist", []FileInfo{tr("Radiohead", "Thom Yorke", "OK Computer", 0), tr("Radiohead", "Jonny Greenwood", "OK Computer", 0)}, TagConsensus{"Radiohead", "OK Computer", 0}},
		{"no album artist: the track artist", []FileInfo{tr("", "Radiohead", "Kid A", 2000), tr("", "Radiohead", "Kid A", 2000), tr("", "radiohead", "Kid A", 2000)}, TagConsensus{"Radiohead", "Kid A", 2000}},
		{"a compilation has no artist", []FileInfo{tr("Various Artists", "A", "Hits", 2001), tr("Various Artists", "B", "Hits", 2001)}, TagConsensus{"", "Hits", 2001}},
		{"a stray file does not decide", []FileInfo{tr("Radiohead", "", "OK Computer", 1997), tr("Radiohead", "", "OK Computer", 1997), tr("Other", "", "Elsewhere", 2010)}, TagConsensus{"Radiohead", "OK Computer", 1997}},
		{"no majority", []FileInfo{tr("A", "", "X", 0), tr("B", "", "Y", 0)}, TagConsensus{}},
		{"untagged files", []FileInfo{{}, {}}, TagConsensus{}},
		{"no files", nil, TagConsensus{}},
	}
	for _, tc := range cases {
		if got := ConsensusOf(tc.files); got != tc.want {
			t.Errorf("%s: %+v, want %+v", tc.name, got, tc.want)
		}
	}
}
