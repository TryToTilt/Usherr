package comics

import "testing"

func TestParseName(t *testing.T) {
	cases := []struct {
		in     string
		series string
		number string
		year   int
		coll   bool
	}{
		{"Batman 001 (2016) (Digital) (Zone-Empire).cbr", "Batman", "001", 2016, false},
		{"Batman.001.2016.Digital.cbz", "Batman", "001", 2016, false},
		{"Batman #12 (2016)", "Batman", "12", 2016, false},
		{"Groo the Wanderer (1982) Volume 01 Issue 001.cbz", "Groo the Wanderer", "001", 1982, false},
		{"The Amazing Spider-Man v2 036 (2002) (Digital)", "The Amazing Spider-Man", "036", 2002, false},
		{"Saga 054.5 (2018)", "Saga", "054.5", 2018, false},
		{"Batman Vol. 3 TPB (2017)", "Batman", "", 2017, true},
		{"Daredevil - Born Again (1986) (TPB)", "Daredevil - Born Again", "", 1986, true},
		{"Spider-Man 2099 001 (1992)", "Spider-Man 2099", "001", 1992, false},
	}
	for _, c := range cases {
		n := ParseName(c.in)
		if n.Series != c.series || n.Number != c.number || n.Year != c.year || n.Collection != c.coll {
			t.Errorf("%q: got %+v", c.in, n)
		}
	}
}

func TestMatches(t *testing.T) {
	s := Series{Name: "The Amazing Spider-Man", StartYear: 1999}
	i := Issue{Number: "36", CoverDate: "2002-01-01"}
	if !Matches(ParseName("Amazing Spider-Man 036 (2002) (Digital)"), s, i) {
		t.Error("should match without 'The' and with padding")
	}
	if Matches(ParseName("Amazing Spider-Man 036 (2018)"), s, i) {
		t.Error("a release from another year matched")
	}
	if Matches(ParseName("Amazing Spider-Man 037 (2002)"), s, i) {
		t.Error("another issue matched")
	}
	if Matches(ParseName("Amazing Spider-Man Vol. 3 TPB (2002)"), s, i) {
		t.Error("a collection matched an issue")
	}
}

func TestSortKeyAndNumbers(t *testing.T) {
	if !(SortKey("1") < SortKey("1.1") && SortKey("1.1") < SortKey("2") && SortKey("2") < SortKey("10") && SortKey("½") < SortKey("1")) {
		t.Error("sort order wrong")
	}
	if NormalNumber("001") != "1" || NormalNumber("12.50") != "12.5" || NormalNumber("#7") != "7" {
		t.Error("normal numbers wrong")
	}
	s := Series{Name: "Groo the Wanderer", StartYear: 1982}
	if FileName(s, Issue{Number: "1"}) != "Groo the Wanderer (1982) Volume 01 Issue 001" {
		t.Errorf("file name %q", FileName(s, Issue{Number: "1"}))
	}
	if q := SearchQueries(s, Issue{Number: "7"}); q[0] != "Groo the Wanderer 007" || q[1] != "Groo the Wanderer 7" {
		t.Errorf("queries %q", q)
	}
}

func TestMatchesTheRightVolumesIssue(t *testing.T) {
	s := Series{Name: "Daredevil", StartYear: 2022}
	i := Issue{Number: "1", CoverDate: "2022-09-01", StoreDate: "2022-07-13"}
	good := "Daredevil 001 (2022) (Digital) (Zone-Empire)"
	for _, bad := range []string{
		"Daredevil 001 (2023) (Digital-HD) (Shan-Empire)",       // a later volume's #1... one year off is allowed
		"Daredevil 001 [2026] [Digital] [Shan-Empire]",          // square brackets
		"Daredevil.No.1.Facsimile.Edition.001.2025.c2c.digital", // a reprint, year after the number
	} {
		n := ParseName(bad)
		if Matches(n, s, i) && !IsReprint(bad) && YearDistance(n, i) == 0 {
			t.Errorf("%q ranked as the issue itself", bad)
		}
	}
	if ParseName("Daredevil 001 [2026] [Digital] [Shan-Empire]").Year != 2026 {
		t.Error("year in square brackets not read")
	}
	if ParseName("Daredevil.No.1.Facsimile.Edition.001.2025.c2c.digital").Year != 2025 {
		t.Error("year after the number not read")
	}
	if Matches(ParseName("Daredevil 001 [2026] [Digital]"), s, i) {
		t.Error("a 2026 #1 matched a 2022 issue")
	}
	if Matches(ParseName("Daredevil 001 (2023) (Digital-HD) (Shan-Empire)"), s, i) {
		t.Error("a 2023 #1 matched the 2022 issue")
	}
	later := Issue{Number: "11", CoverDate: "2023-07-01", StoreDate: "2023-05-24"}
	if !Matches(ParseName("Daredevil 011 (2022) (Digital)"), s, later) || !Matches(ParseName("Daredevil 011 (2023) (Digital)"), s, later) {
		t.Error("a later issue labelled with the series' year or its own year should match")
	}
	if !IsReprint("Daredevil.No.1.Facsimile.Edition") {
		t.Error("facsimile not seen as a reprint")
	}
	n := ParseName(good)
	if !Matches(n, s, i) || YearDistance(n, i) != 0 {
		t.Errorf("the real release: %+v", n)
	}
}
