package api

import (
	"testing"

	"github.com/rdborg/mediarium/internal/indexers"
)

// Groups and indexers that keep failing go to the back; the rest keep
// their order; one bad try isn't enough.
func TestReliableFirst(t *testing.T) {
	s := newAuditServer(t)
	ctx := t.Context()
	list := []indexers.Result{
		{Title: "Film.2024.2160p.WEB-DL.DDP5.1.H.265-FLAKY", IndexerName: "Alpha"},
		{Title: "Film.2024.2160p.WEB-DL.DDP5.1.H.265-SOLID", IndexerName: "Alpha"},
		{Title: "Film.2024.1080p.WEB-DL.DDP5.1.H.264-OTHER", IndexerName: "Broken"},
		{Title: "Film.2024.1080p.WEB-DL.DDP5.1.H.264-FINE", IndexerName: "Alpha"},
	}
	titles := func(l []indexers.Result) []string {
		var out []string
		for _, r := range l {
			out = append(out, r.Title[len(r.Title)-5:])
		}
		return out
	}
	// One failure: nothing moves.
	s.noteStream(list[0].Title, "", false)
	if got := titles(s.reliableFirst(ctx, list)); got[0] != "FLAKY" {
		t.Fatalf("one failure moved it: %v", got)
	}
	// FLAKY keeps failing; the Broken indexer too; SOLID works.
	for i := 0; i < 3; i++ {
		s.noteStream(list[0].Title, "", false)
		s.noteStream("Show.S01E01.1080p.WEB-X", "Broken", false)
		s.noteStream(list[1].Title, "Alpha", true)
	}
	got := titles(s.reliableFirst(ctx, list))
	want := []string{"SOLID", "-FINE", "FLAKY", "OTHER"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order %v, want %v", got, want)
		}
	}
	// A group that fails sometimes but mostly works stays where it is.
	for i := 0; i < 3; i++ {
		s.noteStream(list[3].Title, "", false)
	}
	for i := 0; i < 10; i++ {
		s.noteStream(list[3].Title, "", true)
	}
	if got := titles(s.reliableFirst(ctx, list)); got[1] != "-FINE" {
		t.Fatalf("a mostly good group moved: %v", got)
	}
}
