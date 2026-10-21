package api

import "testing"

func TestChannelNum(t *testing.T) {
	for in, want := range map[string]int{"29": 29, "4.1": 4, "4": 4, "4.2": 42, "13.3": 133, "": 0, "x": 0} {
		if got := channelNum(in); got != want {
			t.Errorf("channelNum(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestChannelLabelAndCategory(t *testing.T) {
	if got := channelLabel(gnChannel{CallSign: "KFORDT2", AffiliateName: "ANTENNA TV"}); got != "Antenna TV" {
		t.Errorf("label = %q", got)
	}
	if got := channelLabel(gnChannel{CallSign: "ESPNHD", AffiliateName: ""}); got != "ESPN" {
		t.Errorf("label = %q", got)
	}
	if got := channelLabel(gnChannel{CallSign: "TBS", AffiliateName: "null"}); got != "TBS" {
		t.Errorf("label = %q", got)
	}
	for _, c := range []struct {
		in   gnChannel
		want string
	}{
		{gnChannel{CallSign: "CNNHD"}, "News"},
		{gnChannel{CallSign: "FNCHD", StationFilters: []string{"filter-news", "filter-talk"}}, "News"},
		{gnChannel{CallSign: "MSNOWHD"}, "News"},
		{gnChannel{CallSign: "ESPN2HD"}, "Sports"},
		{gnChannel{CallSign: "FS1HD"}, "Sports"},
		{gnChannel{CallSign: "HBOMOV"}, "Movies"},
		{gnChannel{CallSign: "TCMHD", StationFilters: []string{"filter-movie"}}, "Movies"},
		{gnChannel{CallSign: "HBODRMA"}, "Movies"},
		{gnChannel{CallSign: "HBOLATP"}, "Movies"},
		{gnChannel{CallSign: "ESPNEWS"}, "Sports"},
		{gnChannel{CallSign: "NFLRZN"}, "Sports"},
		{gnChannel{CallSign: "DISNHD"}, "Kids & Family"},
		{gnChannel{CallSign: "NIKHD", StationFilters: []string{"filter-movie"}}, "Kids & Family"},
		{gnChannel{CallSign: "MTVHD", StationFilters: []string{"filter-movie"}}, "Entertainment"},
		{gnChannel{CallSign: "TNTHD", StationFilters: []string{"filter-movie"}}, "Entertainment"},
		{gnChannel{CallSign: "KFORDT", AffiliateName: "NATIONAL BROADCASTING COMPANY", StationFilters: []string{"filter-news", "filter-talk"}}, "Local"},
		{gnChannel{CallSign: "KTBNDT", AffiliateName: "TRINITY BROADCASTING NETWORK", StationFilters: []string{"filter-news"}}, "Local"},
		{gnChannel{CallSign: "KFORDT2", AffiliateName: "THIS TV"}, "Movies"},
		{gnChannel{CallSign: "HGTVD"}, "Entertainment"},
		{gnChannel{CallSign: "FOO", StationFilters: []string{"filter-sports"}}, "Sports"},
	} {
		if got := categoryOf(c.in); got != c.want {
			t.Errorf("categoryOf(%+v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNumberLess(t *testing.T) {
	if !numberLess("4.1", "4.2") || !numberLess("4.2", "5") || !numberLess("9", "10") || numberLess("10", "9") {
		t.Error("channel numbers aren't in order")
	}
}
