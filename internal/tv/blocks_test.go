package tv

import (
	"math/rand"
	"strings"
	"testing"
	"time"
)

func minutes(n int) Prober {
	return func(it Item) (time.Duration, error) { return time.Duration(n) * time.Minute, nil }
}

func TestGridStartsProgramsOnTheLinesAndFillsTheGaps(t *testing.T) {
	lib := fakeLib{eps: map[int64][]Item{1: show(1, 20)}}
	ch := Channel{ID: 1, Name: "Grid", Kind: "library", Enabled: true, Rules: Rules{Series: []int64{1}, Grid: 30}}
	st := State{}
	start := time.Date(2026, 10, 9, 18, 7, 0, 0, time.Local)
	list, err := Build(ch, lib, minutes(22), &st, start, start.Add(3*time.Hour), rand.New(rand.NewSource(1)))
	if err != nil {
		t.Fatal(err)
	}
	for i, p := range list {
		if i > 0 && !p.Start.Equal(list[i-1].End) {
			t.Fatalf("hole before %d: %v after %v", i, p.Start, list[i-1].End)
		}
		if p.Kind == "episode" && p.Start.Minute()%30 != 0 {
			t.Errorf("episode at %s, not on the grid", p.Start.Format("15:04"))
		}
	}
	if list[0].Kind != "card" || !list[1].Start.Equal(time.Date(2026, 10, 9, 18, 30, 0, 0, time.Local)) {
		t.Errorf("starts with %s until %s; want a card until 18:30", list[0].Kind, list[0].End.Format("15:04"))
	}
}

func TestDaypartsPlayTheirOwnLineUp(t *testing.T) {
	lib := fakeLib{eps: map[int64][]Item{1: show(1, 50), 2: show(2, 50)}}
	ch := Channel{ID: 2, Name: "Parts", Kind: "library", Enabled: true, Rules: Rules{Series: []int64{1}, Grid: 30, Dayparts: []Daypart{{Part: "prime", Series: []int64{2}}}}}
	st := State{}
	start := time.Date(2026, 10, 9, 17, 0, 0, 0, time.Local)
	list, err := Build(ch, lib, minutes(30), &st, start, start.Add(7*time.Hour), rand.New(rand.NewSource(1)))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range list {
		if p.Kind != "episode" {
			continue
		}
		prime := DaypartOf(p.Start) == "prime"
		if prime && p.SeriesID != 2 || !prime && p.SeriesID != 1 {
			t.Errorf("%s: show %d in %s", p.Start.Format("15:04"), p.SeriesID, DaypartOf(p.Start))
		}
	}
}

func TestSlotsPinAShowToItsTime(t *testing.T) {
	lib := fakeLib{eps: map[int64][]Item{1: show(1, 50), 9: show(9, 10)}}
	ch := Channel{ID: 3, Name: "Pins", Kind: "library", Enabled: true, Rules: Rules{Series: []int64{1}, Grid: 30, Slots: []Slot{{At: "19:00", SeriesID: 9, Count: 2, Name: "Must-see"}}}}
	st := State{}
	start := time.Date(2026, 10, 9, 17, 40, 0, 0, time.Local)
	list, err := Build(ch, lib, minutes(45), &st, start, start.Add(4*time.Hour), rand.New(rand.NewSource(1)))
	if err != nil {
		t.Fatal(err)
	}
	var pinned []Program
	for _, p := range list {
		if p.SeriesID == 9 {
			pinned = append(pinned, p)
		}
	}
	if len(pinned) != 2 || pinned[0].Start.Format("15:04") != "19:00" || !strings.HasPrefix(pinned[0].Subtitle, "Must-see") {
		t.Fatalf("pinned: %+v", pinned)
	}
	// Nothing runs into the slot.
	for _, p := range list {
		if p.Start.Before(pinned[0].Start) && p.End.After(pinned[0].Start) {
			t.Errorf("%s (%s–%s) runs into the slot", p.Title, p.Start.Format("15:04"), p.End.Format("15:04"))
		}
	}
	// The slot's episodes are the show's first two, in order.
	if pinned[0].ItemID != 901 || pinned[1].ItemID != 902 {
		t.Errorf("episodes %d, %d", pinned[0].ItemID, pinned[1].ItemID)
	}
}

func TestDaypartOf(t *testing.T) {
	at := func(h int) time.Time { return time.Date(2026, 1, 1, h, 30, 0, 0, time.Local) }
	for h, want := range map[int]string{0: "latenight", 1: "latenight", 2: "overnight", 6: "morning", 10: "midmorning", 13: "afternoon", 18: "evening", 21: "prime", 23: "latenight"} {
		if got := DaypartOf(at(h)); got != want {
			t.Errorf("%d:30 is %s, want %s", h, got, want)
		}
	}
}
