package manga

import "testing"

func TestParseMangaNames(t *testing.T) {
	cases := []struct {
		title  string
		names  []string
		kind   Kind
		number string
		lo, hi float64
		known  bool
	}{
		{"Chainsaw Man v01 (2020) (Digital) (1r0n)", []string{"Chainsaw Man"}, KindVolume, "1", 0, 0, true},
		{"Chainsaw Man Vol. 1 (2020) (Digital)", []string{"Chainsaw Man"}, KindVolume, "1", 0, 0, true},
		{"Chainsaw Man Volume 012", []string{"Chainsaw Man"}, KindVolume, "12", 0, 0, true},
		{"Chainsaw Man 023 [Group]", []string{"Chainsaw Man"}, KindBare, "23", 0, 0, true},
		{"Chainsaw Man c001 (2020)", []string{"Chainsaw Man"}, KindChapter, "", 0, 0, true},
		{"Chainsaw Man ch.123", []string{"Chainsaw Man"}, KindChapter, "", 0, 0, true},
		{"Chainsaw Man Chapter 123", []string{"Chainsaw Man"}, KindChapter, "", 0, 0, true},
		{"Chainsaw Man v03 c17", []string{"Chainsaw Man"}, KindChapter, "3", 0, 0, true},
		{"Mob Psycho 100 v01", []string{"Mob Psycho 100"}, KindVolume, "1", 0, 0, true},
		{"Mob.Psycho.100.v01.Digital.1r0n", []string{"Mob Psycho 100"}, KindVolume, "1", 0, 0, true},
		{"Kaiju No. 8 v01", []string{"Kaiju No. 8"}, KindVolume, "1", 0, 0, true},
		{"Kaiju No 8 Vol. 3 (2022)", []string{"Kaiju No. 8"}, KindVolume, "3", 0, 0, true},
		{"Shingeki no Kyojin v01", []string{"Attack on Titan", "Shingeki no Kyojin"}, KindVolume, "1", 0, 0, true},
		{"Spy x Family v01", []string{"Spy × Family"}, KindVolume, "1", 0, 0, true},
		{"SpyxFamily v02", []string{"Spy × Family"}, KindVolume, "2", 0, 0, true},
		{"One Piece v01-v50", []string{"One Piece"}, KindPack, "", 1, 50, true},
		{"Berserk v01-40", []string{"Berserk"}, KindPack, "", 1, 40, true},
		{"Vagabond manga eng v 21-26", []string{"Vagabond"}, KindPack, "", 21, 26, true},
		{"Vagabond.Miyamoto.Musashi.Vol.1-27.MANGA-[M-S]", []string{"Vagabond"}, KindPack, "", 1, 27, true},
		{"Fire Force v12.5", []string{"Fire Force"}, KindVolume, "12.5", 0, 0, true},
		{"Fire.Force.v12.5.Digital", []string{"Fire Force"}, KindVolume, "12.5", 0, 0, true},
		{"Vagabond Volume 026 [scanlation.ch225-233] [Strays]", []string{"Vagabond"}, KindVolume, "26", 0, 0, true},
		{"Pokémon Adventures v05", []string{"Pokemon Adventures"}, KindVolume, "5", 0, 0, true},
		{"The Promised Neverland v02", []string{"Promised Neverland"}, KindVolume, "2", 0, 0, true},
		{"Promised Neverland v02", []string{"The Promised Neverland"}, KindVolume, "2", 0, 0, true},
		{"Yotsuba&! v15", []string{"Yotsuba&!"}, KindVolume, "15", 0, 0, true},
		{"Series Name Complete Collection", []string{"Series Name"}, KindWhole, "", 0, 0, true},
		{"Series Name [v01-v05] [Group]", []string{"Series Name"}, KindPack, "", 1, 5, true},
		{"Daredevil 001-016", []string{"Daredevil"}, KindRange, "", 1, 16, true},
		{"Another Series v01", []string{"Chainsaw Man"}, KindVolume, "1", 0, 0, false},
	}
	for _, c := range cases {
		n := ParseFor(c.title, c.names)
		if n.Kind != c.kind || n.Number != c.number || n.Lo != c.lo || n.Hi != c.hi || n.Known != c.known {
			t.Errorf("%q: got %+v", c.title, n)
		}
	}
}

func TestParseWithoutKnowingTheSeries(t *testing.T) {
	cases := []struct {
		name, series string
		kind         Kind
		number       string
	}{
		{"Kaiju No. 8 v01.cbz", "Kaiju No. 8", KindVolume, "1"},
		{"Mob Psycho 100 Vol. 3 (2018).cbz", "Mob Psycho 100", KindVolume, "3"},
		{"Lanternfish Harbor 007.cbr", "Lanternfish Harbor", KindBare, "7"},
		{"Some Title 100 005.cbz", "Some Title 100", KindBare, "5"},
		{"Some_Title_v12.5.cbz", "Some Title", KindVolume, "12.5"},
	}
	for _, c := range cases {
		n := Parse(c.name)
		if n.Series != c.series || n.Kind != c.kind || n.Number != c.number {
			t.Errorf("%q: got %+v", c.name, n)
		}
	}
}

func TestEvaluateVolumes(t *testing.T) {
	mb := int64(1 << 20)
	official := Sources{Official: true}
	both := Sources{Official: true, Fan: true}
	cases := []struct {
		title      string
		names      []string
		licensed   int
		sources    Sources
		want       string
		why        string // the reject code, "" when accepted
		confidence string
	}{
		{"Chainsaw Man v01 (2020) (Digital) (1r0n)", []string{"Chainsaw Man"}, 1, official, "1", "", ConfidenceHigh},
		{"Chainsaw Man v01 (2020) (Digital) (1r0n)", []string{"Chainsaw Man"}, -1, official, "1", "", ConfidenceHigh}, // the group says it
		{"Chainsaw Man Vol. 1 (2020) (Digital)", []string{"Chainsaw Man"}, 1, official, "1", "", ConfidenceHigh},
		{"Chainsaw Man Vol. 1 (2020) (Digital)", []string{"Chainsaw Man"}, -1, official, "1", "", ConfidenceLow}, // "(Digital)" alone proves nothing
		{"Chainsaw Man v01", []string{"Chainsaw Man"}, 1, official, "1", "", ConfidenceLow},
		{"Mob Psycho 100 v01 (Digital) (Viz)", []string{"Mob Psycho 100"}, -1, official, "1", "", ConfidenceHigh},
		{"Kaiju No. 8 v01 (Digital) (Viz)", []string{"Kaiju No. 8"}, -1, official, "1", "", ConfidenceHigh},
		{"Kaiju No. 8: Relax v01 (Digital) (Viz)", []string{"Kaiju No. 8"}, -1, official, "1", RejectSeries, ""},
		{"Shingeki no Kyojin v01 (Digital) (Kodansha)", []string{"Attack on Titan", "Shingeki no Kyojin"}, -1, official, "1", "", ConfidenceHigh},
		{"Spy x Family v01 (Digital) (Viz)", []string{"Spy × Family"}, -1, official, "1", "", ConfidenceHigh},
		{"Chainsaw Man 023 [Group]", []string{"Chainsaw Man"}, 1, official, "23", "", ConfidenceLow},
		{"Chainsaw Man c001 (2020)", []string{"Chainsaw Man"}, 1, official, "1", RejectChapter, ""},
		{"Chainsaw Man v02 (Digital) (Viz)", []string{"Chainsaw Man"}, 1, official, "1", RejectVolume, ""},
		{"One Piece v01-v50", []string{"One Piece"}, 1, official, "10", "", ConfidenceLow},
		{"One Piece v01-v50", []string{"One Piece"}, 1, official, "60", RejectVolume, ""},
		{"Berserk v01-40", []string{"Berserk"}, 1, official, "10", "", ConfidenceLow},
		{"Fire Force v12.5 (Digital) (Kodansha)", []string{"Fire Force"}, 1, official, "12.5", "", ConfidenceHigh},
		{"Fire Force v12.5 (Digital) (Kodansha)", []string{"Fire Force"}, 1, official, "12", RejectVolume, ""},
		{"One Piece v001 (Japanese) (raw)", []string{"One Piece"}, 1, official, "1", RejectLanguage, ""},
		{"One.Piece.v001.RAW", []string{"One Piece"}, 1, official, "1", RejectLanguage, ""},
		{"Vagabond - T25 (Digital)", []string{"Vagabond"}, 1, official, "25", RejectLanguage, ""},
		{"Raw Hero v01 (Digital) (Yen Press)", []string{"Raw Hero"}, 1, official, "1", "", ConfidenceHigh},
		{"Naruto v72 (2015) (Digital) epub", []string{"Naruto"}, 1, official, "72", "", ConfidenceLow}, // may be pictures: it waits for a person
		{"Vagabond Volume 026 [scanlation.ch225-233] [Strays]", []string{"Vagabond"}, 1, official, "26", RejectFan, ""},
		{"Vagabond Volume 026 [scanlation.ch225-233] [Strays]", []string{"Vagabond"}, 1, both, "26", "", ConfidenceMedium},
		{"Vagabond VizBig v02 (Digital) (Viz)", []string{"Vagabond"}, 1, official, "2", "", ConfidenceLow},
		{"Attack on Titan Omnibus v01 (Digital) (Kodansha)", []string{"Attack on Titan"}, 1, official, "1", "", ConfidenceLow},
		{"Daredevil 016 1080p WEB-DL", []string{"Daredevil"}, 1, official, "16", RejectFormat, ""},
	}
	for _, c := range cases {
		v := Evaluate(Release{Title: c.title, SizeBytes: 150 * mb}, Target{Names: c.names, Licensed: c.licensed, Sources: c.sources}, c.want)
		if v.Why != c.why || v.Confidence != c.confidence {
			t.Errorf("%q for %s: got %+v", c.title, c.want, v)
		}
	}
}

func TestEvaluateOfficialBeatsFan(t *testing.T) {
	target := Target{Names: []string{"Vagabond"}, Licensed: 1, Sources: Sources{Official: true, Fan: true}}
	off := Evaluate(Release{Title: "Vagabond v26 (Digital) (Viz)"}, target, "26")
	fan := Evaluate(Release{Title: "Vagabond v26 [scanlation] [Strays]"}, target, "26")
	if off.Class != ClassOfficial || fan.Class != ClassFan || off.Score <= fan.Score {
		t.Fatalf("official %+v, fan %+v", off, fan)
	}
}

func TestEvaluateSourceSwitches(t *testing.T) {
	onlyFan := Target{Names: []string{"Vagabond"}, Licensed: 1, Sources: Sources{Fan: true}}
	if v := Evaluate(Release{Title: "Vagabond v26 (Digital) (Viz)"}, onlyFan, "26"); v.Why != RejectOfficial {
		t.Fatalf("official with official off: %+v", v)
	}
	target := Target{Names: []string{"Vagabond"}, Licensed: 1, Sources: Sources{Official: true}}
	if v := Evaluate(Release{Title: "Vagabond v26 (Digital) (Viz)", Indexer: "Nyaa"}, target, "26"); v.Why != RejectNyaa {
		t.Fatalf("Nyaa with Nyaa off: %+v", v)
	}
	target.Sources.Nyaa = true
	if v := Evaluate(Release{Title: "Vagabond v26 (Digital) (Viz)", Indexer: "Nyaa"}, target, "26"); !v.Confident() {
		t.Fatalf("Nyaa with Nyaa on: %+v", v)
	}
	// A size that's off, and an unknown class, never go by themselves.
	if v := Evaluate(Release{Title: "Vagabond v26 (Digital) (Viz)", SizeBytes: 1 << 20}, target, "26"); v.Confident() {
		t.Fatalf("a 1 MB volume went by itself: %+v", v)
	}
	if v := Evaluate(Release{Title: "Vagabond v26"}, target, "26"); v.Confident() {
		t.Fatalf("an unknown release went by itself: %+v", v)
	}
}

func TestVolumeQueries(t *testing.T) {
	got := VolumeQueries([]string{"Spy × Family", "スパイファミリー", "Spy Family Alt", "One More", "Too Many"}, "023", false)
	want := []string{"Spy x Family Vol 23", "Spy x Family v023", "Spy x Family v23", "Spy x Family Volume 23",
		"Spy Family Alt Vol 23", "Spy Family Alt v23", "One More Vol 23", "One More v23"}
	if len(got) != len(want) {
		t.Fatalf("got %q", got)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got %q", got)
		}
	}
	half := VolumeQueries([]string{"Fire Force"}, "12.5", true)
	if half[0] != "Fire Force Vol 12.5" || half[1] != "Fire Force v012.5" || half[len(half)-1] != "Fire Force 012.5" {
		t.Fatalf("got %q", half)
	}
}

func TestFileName(t *testing.T) {
	for in, want := range map[string]string{"1": "S v01", "12.5": "S v12.5", "100": "S v100", "007": "S v07"} {
		if got := FileName("S", in); got != want {
			t.Errorf("%s: %s", in, got)
		}
	}
}
