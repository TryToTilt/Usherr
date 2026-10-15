package api

import (
	"reflect"
	"testing"

	"github.com/rdborg/mediarium/internal/auth"
)

func TestParseMoods(t *testing.T) {
	got := parseMoods("Here you go:\n1: 2 4\n2: 5, 7, 7, 1, 3\n3: 0\n9: 1\n", 3)
	want := map[int][]string{0: {"edge-of-seat", "dark"}, 1: {"laugh", "comfort", "feel-good"}, 2: {}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestParseBookGenres(t *testing.T) {
	got := parseNumbered("1: 1 7 3\n2: 18\n", 2, bookGenres, 2)
	want := map[int][]string{0: {"Fantasy", "Historical fiction"}, 1: {"Other nonfiction"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestViewerMayAskTheAI(t *testing.T) {
	v := &auth.User{Viewer: true}
	if ok, _ := readOnlyAllows(v, "POST", "POST /api/home/ai/ask"); !ok {
		t.Fatal("a viewer should be able to ask the AI")
	}
	if ok, _ := readOnlyAllows(v, "POST", "POST /api/home/ai/playlist"); ok {
		t.Fatal("a viewer must not make playlists")
	}
	if ok, _ := readOnlyAllows(v, "POST", "POST /api/movies"); ok {
		t.Fatal("a viewer must not add films")
	}
}
