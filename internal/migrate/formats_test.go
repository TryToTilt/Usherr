package migrate

import (
	"context"
	"testing"

	"github.com/rdborg/mediarium/internal/quality"
)

// Usherr: Radarr's custom formats come over as they are, into Usherr's own
// format list, and no profile comes with them. A second import updates the
// copies instead of making more.
func TestImportCopiesCustomFormatsOnly(t *testing.T) {
	h := newHarness(t)
	no, yes := false, true
	opts := Options{Sources: Sources{Radarr: h.sources().Radarr}, Include: Include{Movies: &no, CustomFormats: &yes}}

	pv, err := h.im.Preview(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if cf := pv.Radarr.CustomFormats; cf == nil || cf.Formats != 3 {
		t.Fatalf("preview: %+v", pv.Radarr.CustomFormats)
	}
	before, err := quality.NewRepo(h.db).List()
	if err != nil {
		t.Fatal(err)
	}

	for run := 1; run <= 2; run++ {
		st, err := h.im.Run(context.Background(), opts)
		if err != nil {
			t.Fatal(err)
		}
		if st.Results.Movies.Added != 0 {
			t.Fatalf("movies were imported although left out: %+v", st.Results.Movies)
		}
		got := st.Results.CustomFormats.Added
		if run == 2 {
			got = st.Results.CustomFormats.Existing
		}
		if got != 3 {
			t.Fatalf("run %d: %+v", run, st.Results.CustomFormats)
		}
	}

	after, err := quality.NewRepo(h.db).List()
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Errorf("profiles changed: %d before, %d after", len(before), len(after))
	}
	formats, err := quality.NewFormatRepo(h.db).List()
	if err != nil {
		t.Fatal(err)
	}
	var dv *quality.CustomFormat
	for i := range formats {
		if formats[i].Name == "DV" {
			dv = &formats[i]
		}
	}
	if dv == nil {
		t.Fatalf("DV not saved: %v", formats)
	}
	// The conditions came over working: scored the way a profile would.
	dv.Compile()
	p := quality.Profile{Formats: []quality.ScoredFormat{{Format: dv, Score: 100}}}
	if score, _ := p.FormatScore("Movie.2020.1080p.BluRay.DV.x264-GRP"); score != 100 {
		t.Errorf("a DV release scores %d", score)
	}
	if score, _ := p.FormatScore("Movie.2020.1080p.BluRay.x264-GRP"); score != 0 {
		t.Errorf("a plain release scores %d", score)
	}
}
