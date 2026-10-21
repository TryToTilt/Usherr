package tv

import (
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A fixed day plays each slot at its time, the show filling it with
// "Up next" in the time left (no filler folder), and repeats the next day.
func TestFixedDayPlaysEachSlotAtItsTime(t *testing.T) {
	lib := fakeLib{eps: map[int64][]Item{1: show(1, 5), 2: show(2, 5)}}
	ch := Channel{ID: 4, Name: "NBC", Kind: "library", Enabled: true, Rules: Rules{Fixed: []FixedItem{
		{At: "19:00", Minutes: 60, SeriesID: 1, EpisodeID: 103, Exact: true, Was: "Line of Fire"},
		{At: "20:00", Minutes: 30, SeriesID: 2, Was: "Local news"},
	}}}
	probe := func(Item) (time.Duration, error) { return 22 * time.Minute, nil }
	st := State{}
	from := time.Date(2026, 10, 17, 18, 0, 0, 0, time.Local)
	list, err := Build(ch, lib, probe, &st, from, from.Add(27*time.Hour), rand.New(rand.NewSource(1)))
	if err != nil {
		t.Fatal(err)
	}
	at := func(h, m int, day int) *Program {
		want := time.Date(2026, 10, day, h, m, 0, 0, time.Local)
		for i := range list {
			if list[i].Start.Equal(want) {
				return &list[i]
			}
		}
		return nil
	}
	p := at(19, 0, 17)
	if p == nil || p.ItemID != 103 || p.End.Sub(p.Start) != 22*time.Minute {
		t.Fatalf("7 PM: %+v", p)
	}
	if c := at(19, 22, 17); c == nil || c.Kind != "card" || !c.End.Equal(time.Date(2026, 10, 17, 20, 0, 0, 0, time.Local)) {
		t.Fatalf("the rest of the hour: %+v", c)
	}
	if p := at(20, 0, 17); p == nil || p.SeriesID != 2 || p.Subtitle == "" {
		t.Fatalf("8 PM stand-in, marked: %+v", p)
	}
	if p := at(19, 0, 18); p == nil || p.ItemID != 103 {
		t.Fatalf("the next day repeats the exact episode: %+v", p)
	}
	if p := at(20, 0, 18); p == nil || p.SeriesID != 2 || p.ItemID == at(20, 0, 17).ItemID {
		t.Fatalf("the stand-in moves on to its next episode: %+v", p)
	}
}

// With ads, the time a show leaves over goes in breaks inside it.
func TestFixedDayBreaksAShowForAds(t *testing.T) {
	dir := t.TempDir()
	writeFiller(t, dir, "ad.mp4")
	lib := fakeLib{eps: map[int64][]Item{1: show(1, 2)}}
	ch := Channel{ID: 5, Name: "CBS", Kind: "library", Enabled: true, Rules: Rules{Filler: Filler{Folder: dir}, Fixed: []FixedItem{
		{At: "21:00", Minutes: 60, SeriesID: 1, EpisodeID: 101, Exact: true},
	}}}
	probe := func(it Item) (time.Duration, error) {
		if it.Kind == "filler" {
			return 30 * time.Second, nil
		}
		return 42 * time.Minute, nil
	}
	st := State{}
	from := time.Date(2026, 10, 17, 20, 59, 0, 0, time.Local)
	list, err := Build(ch, lib, probe, &st, from, from.Add(2*time.Hour), rand.New(rand.NewSource(1)))
	if err != nil {
		t.Fatal(err)
	}
	parts, ads := 0, 0
	var shown time.Duration
	for _, p := range list {
		if p.Start.Before(time.Date(2026, 10, 17, 21, 0, 0, 0, time.Local)) || !p.Start.Before(time.Date(2026, 10, 17, 22, 0, 0, 0, time.Local)) {
			continue
		}
		switch p.Kind {
		case "episode":
			parts++
			shown += p.End.Sub(p.Start)
		case "filler":
			ads++
		}
	}
	if parts != 4 || shown != 42*time.Minute || ads < 20 {
		t.Fatalf("parts %d, shown %v, ads %d", parts, shown, ads)
	}
}

func writeFiller(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A channel made while a slot is on joins that slot part-way.
func TestFixedDayJoinsTheSlotOnNow(t *testing.T) {
	lib := fakeLib{eps: map[int64][]Item{1: show(1, 3)}}
	ch := Channel{ID: 6, Name: "ABC", Kind: "library", Enabled: true, Rules: Rules{Fixed: []FixedItem{
		{At: "17:00", Minutes: 60, SeriesID: 1, EpisodeID: 102, Exact: true},
		{At: "18:00", Minutes: 30, SeriesID: 1, EpisodeID: 103, Exact: true},
	}}}
	probe := func(Item) (time.Duration, error) { return 40 * time.Minute, nil }
	st := State{}
	from := time.Date(2026, 10, 17, 17, 10, 0, 0, time.Local)
	list, err := Build(ch, lib, probe, &st, from, from.Add(2*time.Hour), rand.New(rand.NewSource(1)))
	if err != nil {
		t.Fatal(err)
	}
	if len(list) == 0 || !list[0].Start.Equal(from) || list[0].ItemID != 102 || list[0].Offset != 10*time.Minute {
		t.Fatalf("first: %+v", list[0])
	}
}
