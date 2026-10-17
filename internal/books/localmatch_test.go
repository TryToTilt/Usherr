package books

import (
	"strings"
	"testing"
)

func TestSpellNumber(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{0, "zero"}, {7, "seven"}, {22, "twenty two"}, {40, "forty"}, {451, "four hundred fifty one"},
		{1984, "nineteen eighty four"}, {1900, "nineteen hundred"}, {1905, "nineteen oh five"},
		{2001, "two thousand one"}, {2000, "two thousand"}, {10000, ""},
	}
	for _, tc := range cases {
		if got := strings.Join(spellNumber(tc.n), " "); got != tc.want {
			t.Errorf("spellNumber(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

func TestSameTitleNumbersAndApostrophes(t *testing.T) {
	cases := []struct {
		local, catalogue string
		exact, want      bool
	}{
		{"1984", "Nineteen Eighty-Four", true, true},
		{"Nineteen Eighty-Four", "1984", true, true},
		{"Catch 22", "Catch-Twenty-Two", true, true},
		{"1984", "1985", true, false},
		{"1984", "Animal Farm / Nineteen Eighty-Four", true, false},
		// Open Library often has the curly apostrophe.
		{"L'Étranger", "L’étranger", true, true},
		{"L'Etranger", "L’étranger", true, true},
		{"L'Étranger", "L'Etranger. Albert Camus", true, false},
	}
	for _, tc := range cases {
		if got := SameTitle(tc.local, tc.catalogue, tc.exact); got != tc.want {
			t.Errorf("SameTitle(%q, %q, %v) = %v, want %v", tc.local, tc.catalogue, tc.exact, got, tc.want)
		}
	}
}

func TestBestLocalMatch(t *testing.T) {
	orwell := []Found{
		{Key: "OL14905934W", Title: "SparkNotes for 1984 by George Orwell", Author: "SparkNotes"},
		{Key: "OL1168083W", Title: "Nineteen Eighty-Four", Author: "George Orwell"},
	}
	camus := []Found{
		{Key: "OL9990587W", Title: "L'Etranger. Albert Camus", Author: "Michel Mougenot"},
		{Key: "OL1230613W", Title: "L’étranger", Author: "Albert Camus"},
	}
	cases := []struct {
		name, title, author string
		found               []Found
		want                string
	}{
		{"a number title", "1984", "George Orwell", orwell, "OL1168083W"},
		{"accents and a curly apostrophe", "L'Étranger", "Albert Camus", camus, "OL1230613W"},
		{"title and author the other way round", "George Orwell", "1984", orwell, "OL1168083W"},
		{"the author has to match", "1984", "Somebody Else", orwell, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			i := BestLocalMatch(tc.title, tc.author, tc.found)
			got := ""
			if i >= 0 {
				got = tc.found[i].Key
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRankByTitleAndAuthor(t *testing.T) {
	found := []Found{
		{Key: "a", Title: "1984 George Orwell", Author: "Shafqat Mushtaq"},
		{Key: "b", Title: "Julia", Author: "Sandra Newman"},
		{Key: "c", Title: "Animal Farm", Author: "George Orwell"},
		{Key: "d", Title: "Nineteen Eighty-Four", Author: "George Orwell"},
	}
	RankByTitleAndAuthor(found, "1984", "George Orwell")
	var got []string
	for _, f := range found {
		got = append(got, f.Key)
	}
	if strings.Join(got, "") != "dcab" {
		t.Fatalf("order: %v", got)
	}
}
