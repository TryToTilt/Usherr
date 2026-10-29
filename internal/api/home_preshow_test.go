package api

import (
	"testing"
	"time"
)

// The pre-show's seasons, schedules and the blocks' conditions.
func TestPreshowHolidaysAndRules(t *testing.T) {
	loc := time.UTC
	at := func(s string) time.Time {
		d, _ := time.ParseInLocation("2006-01-02 15", s, loc)
		return d
	}
	for day, want := range map[string]string{
		"2026-10-28 20": "Halloween",
		"2026-11-26 20": "Thanksgiving", // the fourth Thursday
		"2026-11-27 20": "",
		"2026-12-25 20": "Christmas",
		"2026-12-30 20": "New Year",
		"2027-01-02 20": "New Year",
		"2026-04-05 20": "Easter", // Easter Sunday 2026
		"2026-08-10 20": "",
	} {
		if got := preshowHolidayNow(at(day)); got != want {
			t.Errorf("holiday on %s = %q, want %q", day, got, want)
		}
	}
	if !sameName("St Patricks Day", "St. Patrick's Day") || sameName("", "") {
		t.Error("folder names should match holidays loosely")
	}
	h := func(n int) *int { return &n }
	weekend := preshowRule{Kind: "weekly", Days: []int{5, 6}, FromHour: h(18)}
	if !weekend.matches(at("2026-10-30 20")) || weekend.matches(at("2026-10-30 12")) || weekend.matches(at("2026-10-28 20")) {
		t.Error("Friday and Saturday evenings")
	}
	winter := preshowRule{Kind: "dates", From: "12-15", To: "01-05"}
	if !winter.matches(at("2027-01-03 10")) || winter.matches(at("2026-12-10 10")) {
		t.Error("dates round New Year")
	}
	if msg := (preshowRule{Kind: "dates", From: "13-01", To: "01-05"}).check(); msg == "" {
		t.Error("a 13th month should be refused")
	}

	yes := true
	film := preshowFilm{Kind: "movie", Genres: []string{"Horror"}, Sound: "Dolby Atmos", Picture: []string{"4k", "hdr"}, Rated: "R"}
	cases := []struct {
		w    *preshowWhen
		want bool
	}{
		{nil, true},
		{&preshowWhen{Sound: []string{"atmos"}}, true},
		{&preshowWhen{Sound: []string{"dtsx"}}, false},
		{&preshowWhen{Genres: []string{"horror", "thriller"}}, true},
		{&preshowWhen{NotGenres: []string{"Horror"}}, false},
		{&preshowWhen{Picture: []string{"imax"}}, false},
		{&preshowWhen{Sound: []string{"dtsx"}, Picture: []string{"hdr"}}, false},
		{&preshowWhen{Sound: []string{"dtsx"}, Picture: []string{"hdr"}, EitherAV: true}, true},
		{&preshowWhen{Sound: []string{"dtsx"}, Picture: []string{"imax"}, EitherAV: true}, false},
		{&preshowWhen{Kids: &yes}, false},
		{&preshowWhen{Showtime: &yes}, false},
		{&preshowWhen{FromHour: h(22), ToHour: h(4)}, false},  // it's 20:00
		{&preshowWhen{Holidays: []string{"Halloween"}}, true}, // it's Halloween season
		{&preshowWhen{Holidays: []string{"any"}}, true},
		{&preshowWhen{Holidays: []string{"Christmas"}}, false},
	}
	for i, c := range cases {
		if got := c.w.fits(film, at("2026-10-28 20")); got != c.want {
			t.Errorf("case %d: fits = %v, want %v", i, got, c.want)
		}
	}
	if (&preshowWhen{Holidays: []string{"Halloween"}}).fits(film, at("2026-11-02 20")) {
		t.Error("Halloween's slot plays after Halloween")
	}
	if (preshowBlock{Type: "clips", Category: "x", When: &preshowWhen{Holidays: []string{"Arbor Day"}}}).check() == "" {
		t.Error("an unknown holiday should be refused")
	}
	if got := pictureOf("4K", "Dolby Vision", []string{"IMAX"}); len(got) != 4 {
		t.Errorf("picture words = %v", got)
	}
	if (preshowBlock{Type: "card", Card: "intermission"}).check() == "" || (preshowBlock{Type: "clips"}).check() == "" {
		t.Error("bad blocks should be refused")
	}
}
