package api

import "testing"

func TestReleaseForDailyShows(t *testing.T) {
	cases := []struct {
		title, show string
		season, ep  int
		date        string
		want        bool
	}{
		{"The.Price.Is.Right.2026.10.16.720p.WEB.h264-DiRT", "The Price Is Right", 0, 0, "2026-10-16", true},
		{"The.Price.Is.Right.2026.10.15.720p.WEB.h264-DiRT", "The Price Is Right", 0, 0, "2026-10-16", false},
		{"The.Price.Is.Right.At.Night.2026.10.16.720p", "The Price Is Right", 0, 0, "2026-10-16", false},
		{"Jimmy.Kimmel.2026.10.16.Josh.Brolin.720p.HEVC.x265-MeGusta", "Jimmy Kimmel Live!", 0, 0, "2026-10-16", true},
		{"General.Hospital.S64E22.Wednesday.7.October.2026.WEB.H264.DME.mp4", "General Hospital", 64, 22, "2026-10-07", true},
		{"General Hospital September 23 2026 Wednesday", "General Hospital", 0, 0, "2026-09-23", true},
		{"Wheel of Fortune Episode #8425 (October 16, 2026)", "Wheel of Fortune", 0, 0, "2026-10-16", true},
		{"General.Hospital.S64E029.720p.HULU.WEB-DL", "General Hospital", 64, 29, "", true},
		{"SPY.x.FAMILY.S01E20.Investigate.the.General.Hospital", "General Hospital", 1, 20, "", false},
		{"Fire.Country.S05E02.1080p.WEB.h264-ETHEL", "Fire Country", 5, 2, "", true},
		{"Jimmy.Fallon.2026.10.16.John.Cusack.720p.WEB.H.264-JFF", "The Tonight Show Starring Jimmy Fallon", 0, 0, "2026-10-16", true},
		{"Seth.Meyers.2026.10.16.Florence.Pugh.720p.WEB.H.264-JFF", "Late Night with Seth Meyers", 0, 0, "2026-10-16", true},
		{"Jeopardy.2026.10.16.1080p.PCOK.WEB-DL.AAC2.0.H.264-MuTT", "Jeopardy!", 0, 0, "2026-10-16", true},
	}
	for _, c := range cases {
		if got := releaseFor(c.title, c.show, c.season, c.ep, c.date); got != c.want {
			t.Errorf("%s: %v, want %v", c.title, got, c.want)
		}
	}
}

func TestSearchName(t *testing.T) {
	for show, want := range map[string]string{
		"The Tonight Show Starring Jimmy Fallon": "jimmy fallon",
		"Late Night with Seth Meyers":            "seth meyers",
		"Jimmy Kimmel Live!":                     "jimmy kimmel",
		"The Price Is Right":                     "price is right",
		"Live with Kelly and Mark":               "live with kelly and mark",
	} {
		if got := searchName(show); got != want {
			t.Errorf("%s: %q, want %q", show, got, want)
		}
	}
}

func TestPureDV(t *testing.T) {
	for title, want := range map[string]bool{
		"Masters.of.the.Universe.2026.2160p.AMZN.WEB-DL.DV.H.265.DDP5.1.Atmos-FLUX":              true,
		"Movie.2026.2160p.WEB-DL.DoVi.H.265-X":                                                   true,
		"Movie.2026.2160p.WEB-DL.DV.HDR10.H.265-X":                                               false,
		"Hoppers.2026.Hybrid.2160p.UHD.Blu-ray.Remux.DV.HDR10P.HEVC.TrueHD.Atmos.7.1-CiNEPHiLES": false,
		"Movie.2026.2160p.WEB-DL.HDR.H.265-X":                                                    false,
		"Movie.2026.1080p.WEB-DL.H.264-DVSKY":                                                    false,
	} {
		if got := pureDV(title); got != want {
			t.Errorf("%s: %v, want %v", title, got, want)
		}
	}
}
