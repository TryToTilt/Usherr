package api

import "testing"

func TestRatingAge(t *testing.T) {
	for r, want := range map[string]int{"G": 0, "PG": 7, "PG-13": 13, "R": 17, "NC-17": 18, "TV-Y": 0, "TV-Y7": 7, "TV-G": 0, "TV-PG": 10, "TV-14": 14, "TV-MA": 17, " pg-13 ": 13} {
		if got, ok := ratingAge(r); !ok || got != want {
			t.Fatalf("ratingAge(%q) = %d, %v; want %d", r, got, ok, want)
		}
	}
	for _, r := range []string{"", "NR", "Unrated", "12A"} {
		if _, ok := ratingAge(r); ok {
			t.Fatalf("ratingAge(%q) should be unknown", r)
		}
	}
}

func TestFamilyAIBudget(t *testing.T) {
	familyAIDay, familyAIUsed = "", 0
	if !familyAIBudget(familyAIDaily) {
		t.Fatal("a full day's allowance should be allowed once")
	}
	if familyAIBudget(1) {
		t.Fatal("past the day's allowance")
	}
	familyAIDay, familyAIUsed = "", 0
}

func TestHorrorFloor(t *testing.T) {
	if !(familyItem{Genres: []string{"Thriller", "horror"}}).horror() || (familyItem{Genres: []string{"Family"}}).horror() {
		t.Fatal("horror is told by its genre")
	}
}
