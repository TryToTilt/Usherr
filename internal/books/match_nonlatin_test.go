package books

import "testing"

func TestMatchesNonLatinTitles(t *testing.T) {
	tests := []struct {
		name    string
		release string
		book    Book
		want    bool
	}{
		{"russian title", "Достоевский - Преступление и наказание [epub]", Book{Title: "Преступление и наказание", Author: "Фёдор Достоевский"}, true},
		{"different russian title", "Достоевский - Идиот [epub]", Book{Title: "Преступление и наказание", Author: "Фёдор Достоевский"}, false},
		{"japanese title", "村上春樹 ノルウェイの森 epub", Book{Title: "ノルウェイの森", Author: "村上春樹"}, true},
		{"japanese other title", "村上春樹 海辺のカフカ epub", Book{Title: "ノルウェイの森", Author: "村上春樹"}, false},
		{"accented latin still folds", "Le Pere Goriot Balzac epub", Book{Title: "Le Père Goriot", Author: "Honoré de Balzac"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Matches(tc.release, tc.book); got != tc.want {
				t.Errorf("Matches(%q, %+v) = %v, want %v", tc.release, tc.book, got, tc.want)
			}
		})
	}
}
