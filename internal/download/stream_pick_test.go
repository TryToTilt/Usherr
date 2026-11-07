package download

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"
)

func cand(name string, size int64) videoCand { return videoCand{name: name, size: size} }

func episodes(n int) []videoCand {
	var out []videoCand
	for i := 1; i <= n; i++ {
		out = append(out, cand(fmt.Sprintf("Show.S01E%02d.1080p.WEB.mkv", i), 1<<30))
	}
	return out
}

func TestPickVideo(t *testing.T) {
	ep := func(e int) StreamWant { return StreamWant{Season: 1, Episode: e} }
	pack := func(e, count int) StreamWant {
		return StreamWant{Season: 1, Episode: e, Release: "Show.S01.1080p.WEB-GROUP", SeasonEpisodes: count}
	}
	scrambled := func(names ...string) []videoCand {
		var out []videoCand
		for i, n := range names {
			out = append(out, videoCand{name: n, size: 1 << 30, order: i})
		}
		return out
	}
	counted := func(places ...int) []videoCand {
		var out []videoCand
		for i, p := range places {
			out = append(out, videoCand{name: fmt.Sprintf("x%02d.mkv", 9-i), size: 1 << 30, order: i, post: p})
		}
		return out
	}
	for _, c := range []struct {
		name  string
		cands []videoCand
		want  StreamWant
		pick  string // "" means refused
		err   error
	}{
		{"a film: the biggest", []videoCand{cand("Film.Sample.mkv", 50), cand("Film.2024.mkv", 500), cand("extra.mkv", 100)}, StreamWant{}, "Film.2024.mkv", nil},
		{"a pack, by name", episodes(10), ep(3), "Show.S01E03.1080p.WEB.mkv", nil},
		{"never the biggest when it's another episode", append(episodes(3), cand("Show.S01E09.REMUX.mkv", 9<<30)), ep(2), "Show.S01E02.1080p.WEB.mkv", nil},
		{"a double episode counts for both", []videoCand{cand("Show.S01E01E02.mkv", 2<<30), cand("Show.S01E03.mkv", 1<<30)}, ep(2), "Show.S01E01E02.mkv", nil},
		{"the wrong season isn't it", []videoCand{cand("Show.S02E03.mkv", 1<<30)}, ep(3), "", ErrNotInRelease},
		{"a pack without the episode", episodes(5), ep(9), "", ErrNotInRelease},
		{"two copies: the bigger", []videoCand{cand("Show.S01E03.720p.mkv", 1<<30), cand("Show.S01E03.1080p.mkv", 2<<30)}, ep(3), "Show.S01E03.1080p.mkv", nil},
		{"a sample isn't it", []videoCand{cand("Show.S01E03.sample.mkv", 50)}, ep(3), "", errors.New("")},
		{"a kept name wins", episodes(4), StreamWant{Season: 1, Episode: 2, InnerFile: "Show.S01E02.1080p.WEB.mkv"}, "Show.S01E02.1080p.WEB.mkv", nil},
		{"a kept name that says another episode doesn't", episodes(4), StreamWant{Season: 1, Episode: 2, InnerFile: "Show.S01E04.1080p.WEB.mkv"}, "Show.S01E02.1080p.WEB.mkv", nil},
		{"a kept scrambled name", scrambled("aaa.mkv", "bbb.mkv"), StreamWant{Season: 1, Episode: 2, InnerFile: "BBB.mkv"}, "bbb.mkv", nil},
		{"a daily show by date", []videoCand{cand("Show.2026.10.16.Guest.mkv", 1<<30), cand("Show.2026.10.15.Guest.mkv", 1<<30)}, StreamWant{AirDate: "2026-10-16"}, "Show.2026.10.16.Guest.mkv", nil},
		{"another date isn't it", []videoCand{cand("Show.2026.10.15.mkv", 1<<30)}, StreamWant{AirDate: "2026-10-16"}, "", ErrNotInRelease},
		{"named episodes and something unnamed", []videoCand{cand("Show.S01E01.mkv", 1<<30), cand("Extra.Feature.mkv", 1<<30)}, ep(5), "", ErrNotInRelease},

		{"one file, scrambled, in a release of that episode", scrambled("a1b2c3.mkv"), StreamWant{Season: 1, Episode: 3, Release: "Show.S01E03.1080p"}, "a1b2c3.mkv", nil},
		{"one file, scrambled, no title to go by", scrambled("a1b2c3.mkv"), ep(3), "a1b2c3.mkv", nil},
		{"one file, scrambled, in a pack: not sure", scrambled("a1b2c3.mkv"), pack(3, 10), "", ErrNotInRelease},
		{"scrambled with extras, in a release of that episode", []videoCand{{name: "a.mkv", size: 1 << 30}, {name: "b.mkv", size: 1 << 20}}, StreamWant{Season: 1, Episode: 3, Release: "Show.S01E03"}, "a.mkv", nil},

		{"scrambled pack, posts number themselves", counted(3, 1, 2), pack(2, 3), "x07.mkv", nil},
		{"scrambled pack in shuffled NZB order", scrambled("zz.mkv", "aa.mkv", "mm.mkv"), pack(2, 3), "aa.mkv", nil},
		{"scrambled pack sorted by its names says nothing", scrambled("aa.mkv", "mm.mkv", "zz.mkv"), pack(2, 3), "", ErrNotInRelease},
		{"names that count up", scrambled("001.mkv", "002.mkv", "003.mkv"), pack(3, 3), "003.mkv", nil},
		{"a count that doesn't fit the season", scrambled("zz.mkv", "aa.mkv", "mm.mkv"), pack(2, 10), "", ErrNotInRelease},
		{"count unknown", scrambled("zz.mkv", "aa.mkv", "mm.mkv"), pack(2, 0), "", ErrNotInRelease},
		{"not a pack", scrambled("zz.mkv", "aa.mkv", "mm.mkv"), StreamWant{Season: 1, Episode: 2, Release: "Show.Some.Name", SeasonEpisodes: 3}, "", ErrNotInRelease},
		{"another season's pack", scrambled("zz.mkv", "aa.mkv", "mm.mkv"), StreamWant{Season: 1, Episode: 2, Release: "Show.S02.1080p", SeasonEpisodes: 3}, "", ErrNotInRelease},
		{"sizes that aren't a season", []videoCand{{name: "zz.mkv", size: 4 << 30}, {name: "aa.mkv", size: 1 << 30}, {name: "mm.mkv", size: 1 << 30}}, pack(2, 3), "", ErrNotInRelease},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := pickVideo(c.cands, c.want, 0, false)
			if c.pick == "" {
				if err == nil {
					t.Fatalf("picked %s", got.name)
				}
				if c.err != nil && c.err.Error() != "" && !errors.Is(err, c.err) {
					t.Fatalf("err %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.name != c.pick {
				t.Fatalf("picked %s, want %s", got.name, c.pick)
			}
		})
	}
}

// What the file says at its start settles it when its name says nothing, and
// overrules a place in the order.
func TestPickVideoByWhatTheFileSays(t *testing.T) {
	cands := []videoCand{
		{name: "zz.mkv", size: 1 << 30, order: 0, head: []byte("\x1a\x45\xdf\xa3 title Show.S01E03.1080p")},
		{name: "aa.mkv", size: 1 << 30, order: 1, head: []byte("\x1a\x45\xdf\xa3 title Show.S01E02.1080p")},
	}
	got, err := pickVideo(cands, StreamWant{Season: 1, Episode: 2, Release: "Show.S01.1080p", SeasonEpisodes: 2}, 0, false)
	if err != nil || got.name != "aa.mkv" {
		t.Fatalf("%v %v", got, err)
	}
}

// A file whose first article is gone could have been the one wanted: say that,
// not "it isn't in this release".
func TestPickVideoWithAFileThatWontOpen(t *testing.T) {
	if _, err := pickVideo(episodes(3), StreamWant{Season: 1, Episode: 9}, 1<<30, false); !errors.Is(err, ErrArticleMissing) {
		t.Errorf("a pack missing one: %v", err)
	}
	// The one wanted is there: another's trouble doesn't matter.
	if got, err := pickVideo(episodes(3), StreamWant{Season: 1, Episode: 2}, 1<<30, false); err != nil || got.name != "Show.S01E02.1080p.WEB.mkv" {
		t.Errorf("%v %v", got, err)
	}
	// A film whose real video won't open: the sample left isn't it.
	if _, err := pickVideo([]videoCand{cand("sample.mkv", 50<<20)}, StreamWant{}, 4<<30, false); !errors.Is(err, ErrArticleMissing) {
		t.Errorf("a film: %v", err)
	}
}

func packFixture(t *testing.T, posts map[string][]byte, subjectOf func(name string) string) (*fakeNNTPServer, *NZB) {
	t.Helper()
	bodies := map[string][]byte{}
	var nzb NZB
	i := 0
	for name, data := range posts {
		id := fmt.Sprintf("p%d@pack", i)
		i++
		bodies[id] = buildMultipartYenc(name, data, 1, len(data))
		nzb.Files = append(nzb.Files, NZBFile{Subject: subjectOf(name), Segments: []NZBSegment{{Number: 1, Bytes: int64(len(data)), MessageID: id}}})
	}
	return newFakeNNTPServer(t, bodies), &nzb
}

func openPack(t *testing.T, srv *fakeNNTPServer, nzb *NZB, want StreamWant) (*StreamFile, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return OpenStreamFor(ctx, []ClientConfig{{Host: srv.addr, Port: srv.port, Connections: 8}}, nzb, want)
}

func video(fill byte, n int) []byte {
	b := bytes.Repeat([]byte{fill}, n)
	copy(b, []byte{0x1a, 0x45, 0xdf, 0xa3})
	return b
}

func TestOpenStreamPlainPack(t *testing.T) {
	posts := map[string][]byte{
		"Show.S01E01.mkv": video(1, 4000),
		"Show.S01E02.mkv": video(2, 3000),
		"Show.S01E03.mkv": video(3, 5000),
	}
	srv, nzb := packFixture(t, posts, func(n string) string { return `"` + n + `" yEnc (1/1)` })
	f, err := openPack(t, srv, nzb, StreamWant{Season: 1, Episode: 2})
	if err != nil {
		t.Fatal(err)
	}
	if f.Name != "Show.S01E02.mkv" || f.Size != 3000 {
		t.Fatalf("%s %d", f.Name, f.Size)
	}
	got, _ := io.ReadAll(io.NewSectionReader(f, 0, f.Size))
	if !bytes.Equal(got, posts["Show.S01E02.mkv"]) {
		t.Errorf("wrong bytes")
	}
	// Only that post was opened: the others aren't needed.
	if srv.totalHits() != 1 {
		t.Errorf("%d articles read", srv.totalHits())
	}
	if _, err := openPack(t, srv, nzb, StreamWant{Season: 1, Episode: 7}); !errors.Is(err, ErrNotInRelease) {
		t.Errorf("not in it: %v", err)
	}
	if f, err := openPack(t, srv, nzb, StreamWant{}); err != nil || f.Name != "Show.S01E03.mkv" {
		t.Errorf("nothing asked for: the biggest: %v %v", f, err)
	}
}

// The yEnc name is the real one when the post's subject is scrambled.
func TestOpenStreamPackWithScrambledSubjects(t *testing.T) {
	posts := map[string][]byte{
		"Show.S01E01.mkv": video(1, 4000),
		"Show.S01E02.mkv": video(2, 3000),
	}
	srv, nzb := packFixture(t, posts, func(string) string { return `abcdef yEnc (1/1)` })
	f, err := openPack(t, srv, nzb, StreamWant{Season: 1, Episode: 2})
	if err != nil || f.Name != "Show.S01E02.mkv" {
		t.Fatalf("%v %v", f, err)
	}
}

// One bad article in another episode doesn't cost this one.
func TestOpenStreamPackOtherEpisodeMissing(t *testing.T) {
	posts := map[string][]byte{"Show.S01E01.mkv": video(1, 4000), "Show.S01E02.mkv": video(2, 3000)}
	srv, nzb := packFixture(t, posts, func(string) string { return `abcdef yEnc (1/1)` })
	for _, f := range nzb.Files {
		if b, _ := srv.body(f.Segments[0].MessageID); bytes.Contains(b, []byte("Show.S01E01")) {
			srv.setBody(f.Segments[0].MessageID, nil)
		}
	}
	quickAskAgain(t)
	f, err := openPack(t, srv, nzb, StreamWant{Season: 1, Episode: 2})
	if err != nil || f.Name != "Show.S01E02.mkv" {
		t.Fatalf("%v %v", f, err)
	}
}

// rar4File is a stored RAR4 file header and its data.
func rar4File(name string, data []byte, unp int, splitAfter, splitBefore bool) []byte {
	flags := uint16(0x8000)
	if splitAfter {
		flags |= 0x0002
	}
	if splitBefore {
		flags |= 0x0001
	}
	h := make([]byte, 32+len(name))
	h[2] = 0x74
	binary.LittleEndian.PutUint16(h[3:], flags)
	binary.LittleEndian.PutUint16(h[5:], uint16(len(h)))
	binary.LittleEndian.PutUint32(h[7:], uint32(len(data)))
	binary.LittleEndian.PutUint32(h[11:], uint32(unp))
	h[25] = 0x30
	binary.LittleEndian.PutUint16(h[26:], uint16(len(name)))
	copy(h[32:], name)
	return append(h, data...)
}

func rar4Start() []byte {
	b := append([]byte(nil), rar4Sig...)
	return append(b, 0, 0, 0x73, 0x01, 0x00, 13, 0, 0, 0, 0, 0, 0, 0)
}

var rar4End = []byte{0, 0, 0x7b, 0, 0x40, 7, 0}

// A pack in one RAR set: many files in a volume, and one running over into the
// next. Each episode's bytes come out whole.
func TestOpenStreamMultiFileRAR(t *testing.T) {
	e1, e2, e3 := video(1, 3000), video(2, 5000), video(3, 2000)
	vol1 := append(rar4Start(), rar4File("Pack/Show.S01E01.mkv", e1, len(e1), false, false)...)
	vol1 = append(vol1, rar4File("Pack/Show.S01E02.mkv", e2[:2000], len(e2), true, false)...)
	vol1 = append(vol1, rar4End...)
	vol2 := append(rar4Start(), rar4File("Pack/Show.S01E02.mkv", e2[2000:], len(e2), false, true)...)
	vol2 = append(vol2, rar4File("Pack/Show.S01E03.mkv", e3, len(e3), false, false)...)
	vol2 = append(vol2, rar4End...)
	srv, nzb := packFixture(t, map[string][]byte{"pack.part01.rar": vol1, "pack.part02.rar": vol2},
		func(n string) string { return `"` + n + `" yEnc (1/1)` })
	for e, want := range map[int][]byte{1: e1, 2: e2, 3: e3} {
		f, err := openPack(t, srv, nzb, StreamWant{Season: 1, Episode: e})
		if err != nil {
			t.Fatalf("episode %d: %v", e, err)
		}
		if f.Size != int64(len(want)) {
			t.Fatalf("episode %d: %d bytes", e, f.Size)
		}
		got, err := io.ReadAll(io.NewSectionReader(f, 0, f.Size))
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("episode %d: wrong bytes (%v)", e, err)
		}
	}
	if _, err := openPack(t, srv, nzb, StreamWant{Season: 1, Episode: 4}); !errors.Is(err, ErrNotInRelease) {
		t.Errorf("not there: %v", err)
	}
}

// One RAR set per episode: only that episode's volumes are opened.
func TestOpenStreamRARSetPerEpisode(t *testing.T) {
	e1, e2 := video(1, 3000), video(2, 5000)
	set := func(name string, data []byte) []byte {
		v := append(rar4Start(), rar4File(name, data, len(data), false, false)...)
		return append(v, rar4End...)
	}
	srv, nzb := packFixture(t, map[string][]byte{
		"show.s01e01.rar": set("Show.S01E01.mkv", e1),
		"show.s01e02.rar": set("Show.S01E02.mkv", e2),
	}, func(n string) string { return `"` + n + `" yEnc (1/1)` })
	f, err := openPack(t, srv, nzb, StreamWant{Season: 1, Episode: 2})
	if err != nil || f.Name != "Show.S01E02.mkv" {
		t.Fatalf("%v %v", f, err)
	}
	if srv.totalHits() != 1 {
		t.Errorf("%d articles read", srv.totalHits())
	}
}

func TestParseRARHeadersRefuseNonsense(t *testing.T) {
	// A file's size that doesn't fit its volume never makes a video.
	bad := append(rar4Start(), rar4File("Show.S01E01.mkv", []byte("short"), 5, false, false)...)
	binary.LittleEndian.PutUint32(bad[len(rar4Start())+7:], 1<<30)
	binary.LittleEndian.PutUint32(bad[len(rar4Start())+11:], 1<<30)
	srv, nzb := packFixture(t, map[string][]byte{"a.rar": bad}, func(n string) string { return `"` + n + `" yEnc (1/1)` })
	if _, err := openPack(t, srv, nzb, StreamWant{Season: 1, Episode: 1}); err == nil {
		t.Error("opened")
	}
}
