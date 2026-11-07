package manga

import (
	"fmt"
	"testing"
)

// These names came from real indexer searches, one of which found that every
// publisher-style release was turned down as "another series". The publisher comes
// first in them. Names of authors are left out of what's kept here.

func officialTarget(names ...string) Target {
	return Target{Names: names, Licensed: 1, Sources: Sources{Official: true}}
}

func TestRealPublishedEditionsAreTaken(t *testing.T) {
	cases := []struct {
		name   string
		titles []string
		series string
		number string
	}{
		{"Chainsaw Man", []string{"VIZ.Media.Chainsaw.Man.Vol.01.2020.HYBRiD.MANGA.eBook-PNLS", "VIZ.Media.-.Chainsaw.Man.Vol.01.2020.HYBRID.MANGA.eBook-21A1"}, "Chainsaw Man", "1"},
		{"Chainsaw Man", []string{"VIZ.Media.Chainsaw.Man.Vol.21.2026.HYBRID.MANGA.eBook-21A1", "VIZ.Media.-.Chainsaw.Man.Vol.21.2026.HYBRID.MANGA.eBook-21A1"}, "Chainsaw Man", "21"},
		{"Kaiju No. 8", []string{"VIZ.Media-Kaiju.No.8.Vol.08.2023.HYBRID.MANGA"}, "Kaiju No. 8", "8"},
		{"Kaiju No. 8", []string{"VIZ.Media-Kaiju.No.8.Vol.10.2024.HYBRID.MANGA"}, "Kaiju No. 8", "10"},
		{"Mob Psycho 100", []string{"Dark.Horse-Mob.Psycho.100.Vol.16.2026.Retail.Comic"}, "Mob Psycho 100", "16"},
		{"Mob Psycho 100", []string{"Dark.Horse.-.Mob.Psycho.100.Vol.11.2023.Retail.Comic.eBook-BitBook"}, "Mob Psycho 100", "11"},
		{"Berserk", []string{"Dark.Horse.Berserk.Vol.43.2026.Retail.Comic.eBook-BitBook", "Berserk v43 [2026] [Digital] [XRA-Empire]"}, "Berserk", "43"},
		{"Berserk", []string{"Berserk v25 [2008] [Digital] [danke-Empire]"}, "Berserk", "25"},
		{"Vinland Saga", []string{"Kodansha-Vinland.Saga.Vol.09.2017.HYBRID.MANGA"}, "Vinland Saga", "9"},
		{"Vinland Saga", []string{"Kodansha-Vinland.Saga.Vol.15.2026.HYBRID.MANGA"}, "Vinland Saga", "15"},
		{"Yotsuba&!", []string{"Yen.Press-Yotsubaand.Vol.15.2022.Hybrid.Comic.eBook-BitBook", "Yen.Press-Yotsubaand.Vol.15.2022.Hybrid.Comic.eBook-BitBook"}, "Yotsuba&!", "15"},
		{"Yotsuba&!", []string{"Yotsuba&amp; v16 [2026] [Digital] [danke-Empire]", "Yotsuba&amp;! v16 (2026) (Digital) (danke-Empire)", "Yotsuba&! v16 (2026) (Digital) (danke-Empire) (cbz)"}, "Yotsuba&!", "16"},
		// Other publishers, in the same shape.
		{"Some Title", []string{"Seven.Seas-Some.Title.Vol.03.2024.Retail.Comic.eBook-BitBook", "Vertical.Comics.-.Some.Title.Vol.03.2024.Retail", "Square.Enix-Some.Title.Vol.03.2024.HYBRID.MANGA", "Tokyopop-Some.Title.Vol.03.2024.Retail", "Titan.Manga-Some.Title.Vol.03.2024.Retail", "One.Peace.Books-Some.Title.Vol.03.2024.Retail", "Denpa-Some.Title.Vol.03.2024.Retail"}, "Some Title", "3"},
	}
	for _, c := range cases {
		for _, title := range c.titles {
			v := Evaluate(Release{Title: title, SizeBytes: 300 << 20}, officialTarget(c.name), "")
			if !v.Confident() || v.Class != ClassOfficial || v.Number != c.number {
				t.Errorf("%q: %+v", title, v)
			}
			if n := ParseFor(title, []string{c.name}); n.Series != c.series || !n.Known {
				t.Errorf("%q series: %+v", title, n)
			}
		}
	}
}

func TestAPublisherIsntTakenForTheSeries(t *testing.T) {
	// Without knowing the series, what's after the publisher is the series.
	if n := Parse("Kodansha-Vinland.Saga.Vol.09.2017.HYBRID.MANGA"); n.Series != "Vinland Saga" || n.Number != "9" {
		t.Errorf("%+v", n)
	}
	// A series named like a publisher stays whole.
	if n := ParseFor("Vertical Limit v02", []string{"Vertical Limit"}); !n.Known || n.Number != "2" {
		t.Errorf("%+v", n)
	}
	if n := Parse("Titan Souls v02"); n.Series != "Titan Souls" {
		t.Errorf("%+v", n)
	}
	if n := Parse("Titan v02"); n.Series != "Titan" {
		t.Errorf("%+v", n)
	}
	// Publisher first, but another series.
	if v := Evaluate(Release{Title: "VIZ.Media.Another.Series.Vol.01.2020.HYBRID.MANGA"}, officialTarget("Chainsaw Man"), ""); v.Why != RejectSeries {
		t.Errorf("%+v", v)
	}
	// And the right series, the wrong volume.
	if v := Evaluate(Release{Title: "VIZ.Media.Chainsaw.Man.Vol.02.2020.HYBRiD.MANGA.eBook-PNLS"}, officialTarget("Chainsaw Man"), "3"); v.Why != RejectVolume {
		t.Errorf("%+v", v)
	}
}

func TestRealNamesInOtherLanguages(t *testing.T) {
	for title, names := range map[string]string{
		"Berserk Collection Vol 1-12 [PDF - ITA]":          "Berserk",
		"Monster [Manga][Full 36 tomos][CBR][Spanish]":     "Monster",
		"Les.ages.perdus.T03.2025.FRENCH.HYBRiD.COMiC.CBZ": "Les ages perdus",
	} {
		if v := Evaluate(Release{Title: title}, officialTarget(names), ""); v.Why != RejectLanguage {
			t.Errorf("%q: %+v", title, v)
		}
	}
}

func TestAnEbookIsJudgedOnItsName(t *testing.T) {
	// Novels and other books with the same word in them stay out.
	for _, title := range []string{"The Vagabond EPUB", "Vagabond.Clown.2014.RETAiL.EPUB.eBook-NODE", "Vagabond Princess: A Long Story EPUB"} {
		if v := Evaluate(Release{Title: title}, officialTarget("Vagabond"), ""); v.Why != RejectFormat {
			t.Errorf("%q: %+v", title, v)
		}
	}
	// A volume as an EPUB may be pictures: it waits for a person, who isn't told no.
	v := Evaluate(Release{Title: "Yen.Press-Yotsubaand.Vol.03.2022.RETAiL.EPUB.eBook-BitBook", SizeBytes: 300 << 20}, officialTarget("Yotsuba&!"), "")
	if v.Reject != "" || v.Confident() || v.Class != ClassOfficial {
		t.Errorf("EPUB volume: %+v", v)
	}
	// So does a PDF pack, named for what it holds.
	v = Evaluate(Release{Title: "Yotsuba&amp;!, Vol. 1-9 PDF"}, officialTarget("Yotsuba&!"), "")
	if v.Reject != "" || v.Kind != KindPack {
		t.Errorf("PDF pack: %+v", v)
	}
	// MOBI and the like can't be shown.
	if v := Evaluate(Release{Title: "Berserk Vol 3 AZW3"}, officialTarget("Berserk"), ""); v.Why != RejectFormat {
		t.Errorf("AZW3: %+v", v)
	}
}

func TestRealWholeRunsFollowTheSourceChoices(t *testing.T) {
	official := officialTarget("One Piece")
	fan := Target{Names: []string{"One Piece"}, Sources: Sources{Official: true, Fan: true}}
	run := "One Piece (Chapters 1-721) PirateBoy SilverRG"
	if v := Evaluate(Release{Title: run}, official, ""); v.Why != RejectFan {
		t.Errorf("with fan scans off: %+v", v)
	}
	if v := Evaluate(Release{Title: run}, fan, ""); v.Why != RejectChapter {
		t.Errorf("with fan scans on: %+v", v)
	}
	whole := "FullMetal Alchemist (Complete) PirateBoy SilverRG"
	if v := Evaluate(Release{Title: whole}, officialTarget("Fullmetal Alchemist"), ""); v.Why != RejectFan {
		t.Errorf("whole, fan scans off: %+v", v)
	}
	if v := Evaluate(Release{Title: whole}, Target{Names: []string{"Fullmetal Alchemist"}, Sources: Sources{Fan: true}}, ""); v.Reject != "" || v.Kind != KindWhole || v.Confident() {
		t.Errorf("whole, fan scans on: %+v", v)
	}
	// Nothing says who made these, so they wait for a person.
	for _, title := range []string{"Vagabond.Miyamoto.Musashi.Vol.1-27.MANGA-[M-S]", "Vagabond manga eng v 21-26 [as of 20.08.2007]", "Vagabond (Manga)"} {
		v := Evaluate(Release{Title: title}, officialTarget("Vagabond"), "")
		if v.Reject != "" || v.Confident() || v.Class != ClassUnknown {
			t.Errorf("%q: %+v", title, v)
		}
	}
	// Named fan scans follow the switch.
	if v := Evaluate(Release{Title: "Vagabond Volume 026 [scanlation.ch225-233] [Strays]"}, officialTarget("Vagabond"), ""); v.Why != RejectFan {
		t.Errorf("%+v", v)
	}
}

func TestANoteFromAnEarlierDownloadAsksFirst(t *testing.T) {
	title := "VIZ.Media.Chainsaw.Man.Vol.01.2020.HYBRiD.MANGA.eBook-PNLS"
	tgt := officialTarget("Chainsaw Man")
	tgt.Notes = map[string]string{title: "This download holds only AZW3 files."}
	v := Evaluate(Release{Title: title, SizeBytes: 100 << 20}, tgt, "")
	if v.Reject != "" || v.Confident() {
		t.Errorf("%+v", v)
	}
}

func TestSearchWordsMatchPublishedNames(t *testing.T) {
	got := VolumeQueries([]string{"Chainsaw Man"}, "1", false)
	want := []string{"Chainsaw Man Vol 01", "Chainsaw Man v001", "Chainsaw Man v01", "Chainsaw Man Vol 1", "Chainsaw Man Volume 1"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("%q", got)
	}
}

func TestBetterCopy(t *testing.T) {
	if !BetterCopy("V v01 (Digital) (danke-Empire).cbz", 10, "V v01 (scans).cbz", 900, 1) {
		t.Error("official beats fan whatever the size")
	}
	if !BetterCopy("V v01 big.cbz", 900, "V v01 small.cbz", 10, 1) {
		t.Error("the bigger file wins")
	}
	if BetterCopy("V v01 a.cbz", 10, "V v01 b.cbz", 10, 1) {
		t.Error("the same size keeps the first")
	}
	if FormatRank("a.cbz") <= FormatRank("a.epub") || FormatRank("a.epub") <= FormatRank("a.pdf") {
		t.Error("pages as pictures come first")
	}
}
