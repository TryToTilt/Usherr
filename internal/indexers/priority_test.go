package indexers_test

import (
	"testing"

	"github.com/rdborg/mediarium/internal/indexers"
)

func TestParsePriority(t *testing.T) {
	cases := []struct {
		name   string
		in     any
		want   int
		wantOK bool
	}{
		{"number", float64(7), 7, true},
		{"lowest", float64(1), 1, true},
		{"highest number", float64(50), 50, true},
		{"yaml int", 12, 12, true},
		{"zero", float64(0), 0, false},
		{"too big", float64(51), 0, false},
		{"fraction", 2.5, 0, false},
		{"numeric string", " 30 ", 30, true},
		{"old word preferred", "Preferred", 10, true},
		{"old word normal", "normal", 25, true},
		{"old words last resort", "Last resort", 40, true},
		{"last_resort", "last_resort", 40, true},
		{"nonsense", "soon", 0, false},
		{"nil", nil, 0, false},
		{"bool", true, 0, false},
	}
	for _, tc := range cases {
		got, ok := indexers.ParsePriority(tc.in)
		if ok != tc.wantOK || (ok && got != tc.want) {
			t.Errorf("%s: got %d, %v; want %d, %v", tc.name, got, ok, tc.want, tc.wantOK)
		}
	}
}

func TestSetPriorityValidatesAndStores(t *testing.T) {
	repo := newTestRepo(t)
	made, err := repo.Create(indexers.Instance{Name: "A", DefinitionID: "newznab", BaseURL: "https://a.test", Enabled: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if made.Priority != indexers.PriorityDefault {
		t.Errorf("new indexer priority = %d, want 25", made.Priority)
	}
	got, err := repo.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Priority != 25 {
		t.Errorf("stored priority = %d, want 25", got.Priority)
	}
	for _, p := range []int{0, 51, -3} {
		if err := repo.SetPriority(made.ID, p); err == nil {
			t.Errorf("priority %d should be refused", p)
		}
	}
	if err := repo.SetPriority(made.ID, 3); err != nil {
		t.Fatal(err)
	}
	got, err = repo.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Priority != 3 {
		t.Errorf("priority = %d, want 3", got.Priority)
	}
	kept, err := repo.Create(indexers.Instance{Name: "B", DefinitionID: "newznab", BaseURL: "https://b.test", Enabled: true, Priority: 5}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if kept.Priority != 5 {
		t.Errorf("imported priority = %d, want 5", kept.Priority)
	}
}
