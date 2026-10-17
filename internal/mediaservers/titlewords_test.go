package mediaservers

import (
	"reflect"
	"testing"
)

func TestTitleWordsKeepsEveryScript(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"The Hobbit", []string{"hobbit"}},
		{"Преступление и наказание", []string{"преступление", "и", "наказание"}},
		{"ノルウェイの森", []string{"ノルウェイの森"}},
		{"Le Père Goriot", []string{"le", "père", "goriot"}},
	}
	for _, tc := range tests {
		if got := titleWords(tc.in); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("titleWords(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
	if !sameWords("Преступление и наказание", "преступление, и наказание!") {
		t.Error("the same Russian title in different case and punctuation should match")
	}
	if sameWords("ノルウェイの森", "海辺のカフカ") {
		t.Error("two different Japanese titles should not match")
	}
}
