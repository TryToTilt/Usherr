package metadata

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestCinemaFormats(t *testing.T) {
	var d MovieDetail
	_ = json.Unmarshal([]byte(`{"release_dates": {"results": [
		{"iso_3166_1": "US", "release_dates": [{"type": 3, "note": "IMAX 70mm"}, {"type": 3, "note": "Dolby Cinema"}, {"type": 3, "note": ""}]},
		{"iso_3166_1": "GB", "release_dates": [{"type": 3, "note": "IMAX"}, {"type": 1, "note": "Premiere"}, {"type": 3, "note": "4DX"}]}
	]}}`), &d)
	got := d.CinemaFormats()
	want := []string{"IMAX 70mm", "Dolby Cinema", "70mm", "4DX"}
	if !slices.Equal(got, want) {
		t.Fatalf("formats: %v, want %v", got, want)
	}
	var none MovieDetail
	if f := none.CinemaFormats(); len(f) != 0 {
		t.Fatalf("no notes: %v", f)
	}
}
