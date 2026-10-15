package comics

import "testing"

func TestEvaluate(t *testing.T) {
	s := Series{Name: "Daredevil", StartYear: 2023}
	i := Issue{Number: "16", Title: "Introductory Rites, Part Sixteen", CoverDate: "2025-01-01", StoreDate: "2024-12-18"}
	mb := int64(1 << 20)
	cases := []struct {
		title      string
		size       int64
		cats       []int
		reject     bool
		confidence string
	}{
		{"Daredevil 016 (2024) (Digital) (Zone-Empire)", 40 * mb, []int{7030}, false, ConfidenceHigh},
		{"Daredevil 016 (2023) (Digital)", 40 * mb, nil, false, ConfidenceHigh},
		{"Daredevil 016", 40 * mb, nil, false, ConfidenceMedium},
		{"Daredevil 016 (2019) (Digital)", 40 * mb, nil, false, ConfidenceLow},
		{"Daredevil 016 (2024) (Digital)", 500 * 1024, nil, false, ConfidenceLow},
		{"Daredevil 017 (2024) (Digital)", 40 * mb, nil, true, ""},
		{"Daredevil Annual 016 (2024)", 40 * mb, nil, false, ConfidenceLow},
		{"Daredevil Annual 001 (2024)", 40 * mb, nil, true, ""},
		{"Daredevil 001-016 (2023-2024) (Digital)", 600 * mb, nil, false, ConfidenceLow},
		{"Daredevil 001-012 (2023-2024) (Digital)", 500 * mb, nil, true, ""},
		{"Daredevil 10-20 no 16.7z", 300 * mb, nil, true, ""},
		{"Daredevil 10-20 no 15.7z", 300 * mb, nil, false, ConfidenceLow},
		{"Daredevil Vol. 3 TPB (2024)", 200 * mb, nil, false, ConfidenceLow},
		{"Elektra Vol. 3 TPB (2024)", 200 * mb, nil, true, ""},
		{"Daredevil 016 Facsimile Edition (2024)", 40 * mb, nil, true, ""},
		{"Daredevil S01E16 1080p WEB-DL", 2000 * mb, nil, true, ""},
		{"Daredevil 016 (2024)", 40 * mb, []int{2040}, true, ""},
		{"Daredevil Black Armor 016 (2024)", 40 * mb, nil, true, ""},
	}
	for _, c := range cases {
		v := Evaluate(Release{Title: c.title, SizeBytes: c.size, Categories: c.cats}, s, i)
		if (v.Reject != "") != c.reject || (!c.reject && v.Confidence != c.confidence) {
			t.Errorf("%s: got %+v", c.title, v)
		}
	}
	a := Evaluate(Release{Title: "Daredevil 016 (2024) (Digital)", SizeBytes: 40 * mb}, s, i)
	b := Evaluate(Release{Title: "Daredevil 016 (2024)", SizeBytes: 40 * mb}, s, i)
	if a.Score <= b.Score {
		t.Errorf("digital should score higher: %d vs %d", a.Score, b.Score)
	}
}

func TestEvaluateMangaVolumes(t *testing.T) {
	s := Series{Name: "Vagabond", StartYear: 2008, Publisher: "Viz"}
	i := Issue{Number: "1", Title: "Invincible Under the Sun", CoverDate: "2008-09-01"}
	mb := int64(1 << 20)
	if v := Evaluate(Release{Title: "Vagabond Volume 023 [scanlation.ch198-206] [KO]", SizeBytes: 61 * mb}, s, i); v.Reject == "" {
		t.Errorf("volume 23 offered for #1: %+v", v)
	}
	if v := Evaluate(Release{Title: "Vagabond Volume 001 [scanlation.ch1-8] [KO]", SizeBytes: 60 * mb}, s, i); v.Reject != "" {
		t.Errorf("volume 1 turned down for #1: %+v", v)
	}
	d := Series{Name: "Daredevil", StartYear: 2023}
	if v := Evaluate(Release{Title: "Daredevil Vol. 3 TPB (2024)", SizeBytes: 200 * mb}, d, Issue{Number: "16"}); v.Reject != "" {
		t.Errorf("western TPB turned down: %+v", v)
	}
}

func TestNamedIssue(t *testing.T) {
	for title, want := range map[string]string{
		"Vagabond Volume 023 [scanlation.ch198-206] [KO]": "23",
		"Daredevil 022 (2013) (Digital) (Zone-Empire)":    "22",
		"Daredevil Vol. 3 TPB (2024)":                     "",
		"Saga 25-33 no 29.7z":                             "",
		"Daredevil #18 (2025)":                            "18",
	} {
		if got := NamedIssue(title, true); got != want {
			t.Errorf("%s: %q, want %q", title, got, want)
		}
	}
}

func TestWesternVolumeIsNotAnIssue(t *testing.T) {
	saga := Series{Name: "Saga", StartYear: 2012, Publisher: "Image"}
	v := Evaluate(Release{Title: "Saga Vol. 6 (2016)", SizeBytes: 135 << 20}, saga, Issue{Number: "31"})
	if v.Reject != "" || v.Confidence != ConfidenceLow {
		t.Errorf("Saga Vol. 6 for #31: %+v (want it on the review list)", v)
	}
	if got := NamedIssue("Saga Vol. 6 (2016)", false); got != "" {
		t.Errorf("Saga Vol. 6 named as issue %q", got)
	}
}
