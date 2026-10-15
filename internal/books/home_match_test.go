package books

import (
	"slices"
	"testing"
)

func TestTitleVariants(t *testing.T) {
	cases := []struct {
		in, author, want string // want: must be among the variants
		first            string // when set: the first variant
	}{
		{"Stormlight Archive 01 - The Way of Kings", "Brandon Sanderson", "The Way of Kings", "The Way of Kings"},
		{"The Final Empire (1 of 3)", "Brandon Sanderson", "The Final Empire", ""},
		{"ReDawn (Skyward Flight", "Brandon Sanderson", "ReDawn", "ReDawn"},
		{"Shadows for Silence in the Forests of Hell_1", "", "Shadows for Silence in the Forests of Hell", ""},
		{"Edgedancer - From the Stormlight Archive", "", "Edgedancer", ""},
		{"Lisey’s Story", "Stephen King", "Lisey's Story", "Lisey's Story"},
		{"Dragons of the Hourglass Mage Margaret Weis &amp; Tracy Hickman", "Margaret Weis, Tracy Hickman", "Dragons of the Hourglass Mage", "Dragons of the Hourglass Mage"},
		{"Beyond the Veil, Book 5 The Grey Wolves Series", "Quinn Loftis", "Beyond the Veil", ""},
		{"D&D - Dragonlance - Warriors 06", "", "Warriors 06", ""},
	}
	for _, c := range cases {
		got := titleVariants(c.in, c.author)
		if !slices.Contains(got, c.want) {
			t.Errorf("%q: variants %q lack %q", c.in, got, c.want)
		}
		if c.first != "" && got[0] != c.first {
			t.Errorf("%q: first variant %q, want %q", c.in, got[0], c.first)
		}
	}
}

func TestAuthorFromTitle(t *testing.T) {
	a, title := AuthorFromTitle("War of the Twins - Margaret Weis, Tracy Hickman")
	if a != "Margaret Weis" || title != "War of the Twins" {
		t.Errorf("got %q / %q", a, title)
	}
	if a, _ := AuthorFromTitle("Edgedancer - From the Stormlight Archive"); a != "" {
		t.Errorf("tagline read as an author: %q", a)
	}
}

func TestPick(t *testing.T) {
	cands := []candidate{
		{Found: Found{Key: "sum", Title: "Summary of The Dragon Reborn", Author: "Quick Reads"}},
		{Found: Found{Key: "set", Title: "The Wheel of Time Box Set: The Dragon Reborn, The Great Hunt", Author: "Robert Jordan"}, authors: []string{"Robert Jordan"}},
		{Found: Found{Key: "long", Title: "The Dragon Reborn (The Wheel of Time, Book 3)", Author: "Robert Jordan"}, authors: []string{"Robert Jordan"}},
	}
	if f := pick("The Dragon Reborn", "Robert Jordan", cands); f == nil || f.Key != "long" {
		t.Errorf("shorter title on disk: got %+v", f)
	}
	co := []candidate{{Found: Found{Key: "gs", Title: "The Gathering Storm", Author: "Robert Jordan"}, authors: []string{"Robert Jordan", "Brandon Sanderson"}}}
	if f := pick("The Gathering Storm", "Brandon Sanderson", co); f == nil {
		t.Error("a co-author should match")
	}
	if f := pick("The Gathering Storm", "Stephen King", co); f != nil {
		t.Error("a different author matched")
	}
	one := []candidate{{Found: Found{Key: "big", Title: "Sunset and other stories from the dark tower years", Author: "Stephen King"}, authors: []string{"Stephen King"}}}
	if f := pick("Sunset", "Stephen King", one); f != nil {
		t.Error("a one-word title grabbed a much longer one")
	}
}

func TestPickRejectsLookAlikes(t *testing.T) {
	king := func(title string) []candidate {
		return []candidate{{Found: Found{Key: "x", Title: title, Author: "Stephen King"}, authors: []string{"Stephen King"}}}
	}
	if f := pick("Morality", "Stephen King", king("Law, Morality and Power")); f != nil {
		t.Error("one-word title matched a longer one")
	}
	if f := pick("Sunset", "Stephen King", king("Just After Sunset")); f != nil {
		t.Error("one-word title gained two words")
	}
	stone := []candidate{{Found: Found{Key: "g", Title: "German from the Ashes", Author: "Kyla Stone"}, authors: []string{"Kyla Stone"}}}
	if f := pick("From the Ashes", "Kyla Stone", stone); f != nil {
		t.Error("a translation matched")
	}
}
