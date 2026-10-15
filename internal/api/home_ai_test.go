package api

import (
	"image"
	"image/color"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/internal/queue"
)

func TestNamedIn(t *testing.T) {
	for _, c := range []struct {
		q, name string
		want    bool
	}{
		{"every film directed by Denis Villeneuve", "Denis Villeneuve", true},
		{"the whole alien series", "Alien", true},
		{"", "Charlie Cox", false},
		{"a 90s heist movie", "Steven Soderbergh", false},
	} {
		if got := namedIn(c.q, c.name); got != c.want {
			t.Errorf("namedIn(%q, %q) = %v", c.q, c.name, got)
		}
	}
}

func TestNormTitle(t *testing.T) {
	if normTitle("The Lord of the Rings: The Return of the King") != normTitle("lord of the rings the return of the king") {
		t.Fatal("titles should match without case, punctuation or a leading The")
	}
}

func TestWantsSeries(t *testing.T) {
	for q, want := range map[string]bool{
		"the whole Alien movie series":    true,
		"all the lord of the rings films": true,
		"a 90s heist movie":               false,
	} {
		if got := wantsSeries.MatchString(q); got != want {
			t.Errorf("wantsSeries(%q) = %v", q, got)
		}
	}
}

func TestSameTitle(t *testing.T) {
	for _, c := range []struct {
		asked, name string
		want        bool
	}{
		{"Iron Fist", "Marvel's Iron Fist", true},
		{"Iron Fist", "Iron Fists", false},
		{"Alien", "Alien: Covenant", true},
		{"The Thomas Crown Affair", "Thomas Crown Affair", true},
		{"Heat", "Heatwave", false},
	} {
		if got := sameTitle(c.asked, c.name); got != c.want {
			t.Errorf("sameTitle(%q, %q) = %v", c.asked, c.name, got)
		}
	}
}

func TestRecapPromptStopsAtTheEpisode(t *testing.T) {
	eps := []metadata.EpisodeInfo{
		{Season: 1, Episode: 1, Name: "One", Overview: "first"},
		{Season: 1, Episode: 2, Name: "Two", Overview: "second"},
		{Season: 2, Episode: 1, Name: "Three", Overview: "LATER"},
		{Season: 0, Episode: 1, Name: "Special", Overview: "SPECIAL"},
	}
	p := recapPrompt("Show", "", eps, 1, 2, "de")
	if !strings.Contains(p, "S1E2 Two: second") || strings.Contains(p, "LATER") || strings.Contains(p, "SPECIAL") {
		t.Fatalf("prompt = %q", p)
	}
	if !strings.Contains(p, "German") {
		t.Fatalf("language missing: %q", p)
	}
}

func TestRoomSampleKeepsSizeAndNoRepeats(t *testing.T) {
	var list []roomTitle
	for i := 1; i <= 400; i++ {
		list = append(list, roomTitle{TMDB: i, Rating: float64(i % 10)})
	}
	got := roomSample(list, 150, rand.New(rand.NewPCG(1, 2)))
	seen := map[int]bool{}
	for _, x := range got {
		if seen[x.TMDB] {
			t.Fatalf("repeat %d", x.TMDB)
		}
		seen[x.TMDB] = true
	}
	if len(got) != 150 {
		t.Fatalf("len = %d", len(got))
	}
}

func TestRoomPickIDPrefersTheName(t *testing.T) {
	sample := []roomTitle{{TMDB: 1, Name: "Luca"}, {TMDB: 2, Name: "The Lego Movie"}}
	for _, c := range []struct {
		n     int
		title string
		want  int
	}{
		{2, "Luca", 1},
		{1, "Luca", 1},
		{2, "", 2},
		{2, "Not Listed", 2},
		{9, "Not Listed", 0},
		{9, "", 0},
		{9, "Luca (2021) - Animation, Comedy", 1},
		{9, "Luca (2021)", 1},
	} {
		if got := roomPickID(sample, c.n, c.title); got != c.want {
			t.Errorf("roomPickID(%d, %q) = %d, want %d", c.n, c.title, got, c.want)
		}
	}
}

func TestLeanAway(t *testing.T) {
	got := leanAway(map[string]int{"Documentary": 5, "Reality": 3, "Drama": 2, "Comedy": 3}, 10)
	if len(got) != 3 || got[0].Genre != "Documentary" || got[1].Genre != "Comedy" || got[2].Genre != "Reality" {
		t.Fatalf("got %+v", got)
	}
	if got := leanAway(map[string]int{"Documentary": 3}, 20); len(got) != 0 {
		t.Fatalf("3 of 20 shouldn't count: %+v", got)
	}
}

func TestIssueOrder(t *testing.T) {
	for in, want := range map[string]float64{"12": 12, "12.1": 12.1, "½": 0.5, "Annual 1": -1, "": -1} {
		if got := issueOrder(in); got != want {
			t.Errorf("issueOrder(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestPickNewsTakesTurns(t *testing.T) {
	rows := []newsRow{{ID: 1, Source: "A"}, {ID: 2, Source: "A"}, {ID: 3, Source: "A"}, {ID: 4, Source: "B"}, {ID: 5, Source: "C"}}
	got := pickNews(rows, 4)
	ids := []int64{}
	for _, r := range got {
		ids = append(ids, r.ID)
	}
	if len(ids) != 4 || ids[0] != 1 || ids[1] != 4 || ids[2] != 5 || ids[3] != 2 {
		t.Fatalf("got %v", ids)
	}
}

func TestNewsPlace(t *testing.T) {
	lib := newsLibrary{movies: map[int]bool{603: true}, shows: map[int]int64{1399: 7}, comics: map[string]int64{"saga": 40}, artists: map[string]int64{}, books: map[string]int64{}}
	if o := lib.place(newsMention{Title: "The Matrix", Type: "movie", TMDBID: 603}); !o.Have || o.Add || o.To != "/title/603" {
		t.Fatalf("movie %+v", o)
	}
	if o := lib.place(newsMention{Title: "X", Type: "tv", TMDBID: 5}); o.Have || !o.Add || o.To != "/show/5" {
		t.Fatalf("show %+v", o)
	}
	if o := lib.place(newsMention{Title: "Saga", Type: "comic"}); !o.Have || o.To != "/comics/40" {
		t.Fatalf("comic %+v", o)
	}
	if o := lib.place(newsMention{Title: "Blue Ruin", Type: "album", Creator: "Band"}); o.Have || o.To != "/search?q=Band+Blue+Ruin" {
		t.Fatalf("album %+v", o)
	}
}

func TestAlbumNumber(t *testing.T) {
	albums := []aiAlbum{{ID: 1, Artist: "Miles Davis", Title: "Kind of Blue"}, {ID: 2, Artist: "Adele", Title: "25"}}
	for _, c := range []struct {
		v    any
		want int
	}{{float64(2), 2}, {"1", 1}, {"2. Adele - 25", 2}, {"Miles Davis - Kind of Blue", 1}, {"Kind of Blue", 1}, {"Nope", 0}} {
		if got := albumNumber(albums, c.v); got != c.want {
			t.Errorf("albumNumber(%v) = %d, want %d", c.v, got, c.want)
		}
	}
}

func TestAskFixups(t *testing.T) {
	r := askFixups(askReply{Action: "recap"}, "catch me up on Yellowstone", "tv")
	if r.Title != "Yellowstone" {
		t.Fatalf("title %q", r.Title)
	}
	r = askFixups(askReply{Action: "add"}, "Add The Matrix please", "movie")
	if r.Title != "The Matrix" {
		t.Fatalf("title %q", r.Title)
	}
	r = askFixups(askReply{Action: "vibe"}, "what's Severance like?", "tv")
	if r.Title != "Severance" {
		t.Fatalf("title %q", r.Title)
	}
	if r := askFixups(askReply{Action: "titles", Query: "x"}, "a cozy mystery set in Scotland", "ebook"); r.Action != "books" || r.Query != "a cozy mystery set in Scotland" {
		t.Fatalf("books %+v", r)
	}
	if r := askFixups(askReply{Action: "titles", Answer: "Michael Mann."}, "Who directed Heat?", "movie"); r.Action != "answer" {
		t.Fatalf("answer %+v", r)
	}
	if r := askFixups(askReply{Action: "titles"}, "a 90s heist movie", "movie"); r.Action != "titles" {
		t.Fatalf("titles %+v", r)
	}
}

func TestShrinkToKeepsShape(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 400, 600))
	for y := 0; y < 600; y++ {
		for x := 0; x < 400; x++ {
			if x < 200 {
				src.Set(x, y, color.White)
			}
		}
	}
	d := shrinkTo(src, 100)
	if d.Bounds().Dx() != 100 || d.Bounds().Dy() != 150 {
		t.Fatalf("size %v", d.Bounds())
	}
	if r, _, _, _ := d.At(10, 10).RGBA(); r>>8 != 255 {
		t.Fatalf("left should stay white, got %d", r>>8)
	}
	if r, _, _, _ := d.At(90, 10).RGBA(); r>>8 != 0 {
		t.Fatalf("right should stay black, got %d", r>>8)
	}
}

func TestSpecsOf(t *testing.T) {
	s := &Server{}
	sp := s.specsOf(queue.Item{MovieID: 1, ReleaseTitle: "Dune.Part.Two.2024.2160p.UHD.BluRay.REMUX.DV.HDR10.HEVC.TrueHD.Atmos.7.1-FRAMESTOR"})
	if sp == nil || sp.Resolution == "" || sp.Codec == "" || sp.HDR == "" || sp.Audio == "" || sp.Group == "" {
		t.Fatalf("specs %+v", sp)
	}
	if sp := s.specsOf(queue.Item{AlbumID: 3, ReleaseTitle: "Artist - Album (2020) [FLAC]"}); sp != nil {
		t.Fatalf("music shouldn't get specs: %+v", sp)
	}
}
