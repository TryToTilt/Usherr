package libimport

import "testing"

func TestNormalizeKeepsEveryScript(t *testing.T) {
	cases := []struct {
		name, a, b string
		same       bool
	}{
		{"accents fold", "Amélie", "amelie", true},
		{"punctuation ignored", "Spider-Man: Homecoming", "spider man homecoming", true},
		{"greek accents fold", "Ο Πατέρας", "ο πατερας", true},
		{"japanese titles differ", "千と千尋の神隠し", "もののけ姫", false},
		{"chinese titles differ", "霸王别姬", "活着", false},
		{"russian titles differ", "Брат", "Сталкер", false},
		{"greek titles differ", "Ζ", "Αθήνα", false},
		{"dakuten kept", "が", "か", false},
		{"symbols only differ", "???", "!!!", false},
		{"same japanese same key", "千と千尋の神隠し", "千と千尋の神隠し", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ka, kb := normalize(c.a), normalize(c.b)
			if ka == "" || kb == "" {
				t.Fatalf("empty key for %q or %q", c.a, c.b)
			}
			if (ka == kb) != c.same {
				t.Fatalf("normalize(%q)=%q normalize(%q)=%q, same=%v want %v", c.a, ka, c.b, kb, ka == kb, c.same)
			}
		})
	}
}

func TestGroupKeyKeepsNonLatinTitlesApart(t *testing.T) {
	if groupKey("千と千尋の神隠し", 2001) == groupKey("もののけ姫", 2001) {
		t.Fatal("two Japanese films of one year share a group key")
	}
	if groupKey("Брат", 0) == groupKey("Сталкер", 0) {
		t.Fatal("two Russian films with no year share a group key")
	}
}

func TestDecideNonLatinTitle(t *testing.T) {
	cands := []Candidate{{TMDBID: 1, Title: "もののけ姫", Year: 1997}}
	if st, _ := Decide("千と千尋の神隠し", 2001, cands); st == MatchMatched {
		t.Fatalf("a different non-Latin title was matched: %v", st)
	}
	cands = []Candidate{{TMDBID: 2, Title: "千と千尋の神隠し", Year: 2001}}
	if st, _ := Decide("千と千尋の神隠し", 2001, cands); st != MatchMatched {
		t.Fatalf("same non-Latin title not matched: %v", st)
	}
}
