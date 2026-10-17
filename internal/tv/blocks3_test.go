package tv

import (
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// byKind: shows 22 minutes, filler 30 seconds.
func byKind(it Item) (time.Duration, error) {
	if it.Kind == "filler" {
		return 30 * time.Second, nil
	}
	return 22 * time.Minute, nil
}

func fillerDir(t *testing.T, n int) string {
	dir := t.TempDir()
	for i := 0; i < n; i++ {
		if err := os.WriteFile(filepath.Join(dir, "ad"+string(rune('a'+i))+".mp4"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestBreaksSplitAShowAroundAds(t *testing.T) {
	lib := fakeLib{eps: map[int64][]Item{1: show(1, 5)}}
	ch := Channel{ID: 4, Name: "Ads", Kind: "library", Enabled: true, Rules: Rules{Series: []int64{1}, Breaks: 2, BreakMinutes: 2, Filler: Filler{Folder: fillerDir(t, 4)}}}
	st := State{}
	start := time.Date(2026, 10, 9, 18, 0, 0, 0, time.Local)
	list, err := Build(ch, lib, byKind, &st, start, start.Add(40*time.Minute), rand.New(rand.NewSource(1)))
	if err != nil {
		t.Fatal(err)
	}
	var parts []Program
	ads := 0
	for _, p := range list {
		if p.ItemID == 101 {
			parts = append(parts, p)
		}
		if p.Kind == "filler" && len(parts) > 0 && len(parts) < 3 {
			ads++
		}
	}
	if len(parts) != 3 {
		t.Fatalf("parts: %+v", parts)
	}
	if parts[0].Offset != 0 || parts[1].Offset <= 0 || parts[2].Offset <= parts[1].Offset {
		t.Errorf("offsets %v %v %v", parts[0].Offset, parts[1].Offset, parts[2].Offset)
	}
	if ads < 4 {
		t.Errorf("only %d ads in the breaks", ads)
	}
}

func TestFamilyHoursKeepOlderTitlesForTheEvening(t *testing.T) {
	kids := show(1, 30)
	grown := show(2, 30)
	for i := range grown {
		grown[i].Age = 17
	}
	lib := fakeLib{eps: map[int64][]Item{1: kids, 2: grown}}
	ch := Channel{ID: 5, Name: "Fam", Kind: "library", Enabled: true, Rules: Rules{Series: []int64{1, 2}, FamilyHours: true}}
	st := State{}
	start := time.Date(2026, 10, 9, 18, 0, 0, 0, time.Local)
	list, err := Build(ch, lib, byKind, &st, start, start.Add(6*time.Hour), rand.New(rand.NewSource(1)))
	if err != nil {
		t.Fatal(err)
	}
	late := false
	for _, p := range list {
		if p.SeriesID == 2 && familyTime(p.Start) {
			t.Errorf("%s at %s, in family hours", p.Title, p.Start.Format("15:04"))
		}
		if p.SeriesID == 2 && !familyTime(p.Start) {
			late = true
		}
	}
	if !late {
		t.Error("the older show should come on after 9 PM")
	}
}

func TestHolidayEpisodesInSeasonAndPremieresInPrimeTime(t *testing.T) {
	eps := show(1, 20)
	eps[7].Subtitle = "S01E08 · A Very Spooky Halloween"
	eps[15].AirDate = "2026-10-05"
	lib := fakeLib{eps: map[int64][]Item{1: eps}}
	ch := Channel{ID: 6, Name: "Season", Kind: "library", Enabled: true, Rules: Rules{Series: []int64{1}, Holidays: true, Premieres: true}}
	st := State{}
	start := time.Date(2026, 10, 9, 19, 0, 0, 0, time.Local)
	list, err := Build(ch, lib, byKind, &st, start, start.Add(3*time.Hour), rand.New(rand.NewSource(1)))
	if err != nil {
		t.Fatal(err)
	}
	var halloween, premiere bool
	for _, p := range list {
		if p.ItemID == eps[7].ID {
			halloween = true
		}
		if p.ItemID == eps[15].ID {
			premiere = DaypartOf(p.Start) == "prime" && len(p.Subtitle) >= 3 && p.Subtitle[:3] == "NEW"
		}
	}
	if !halloween {
		t.Error("the Halloween episode should be on in October")
	}
	if !premiere {
		t.Error("the new episode should premiere in prime time, marked NEW")
	}
	if e := easter(2026); e.Month() != time.April || e.Day() != 5 {
		t.Errorf("Easter 2026 is %v", e)
	}
}
