package manga

import (
	"strings"
	"testing"
	"time"
)

func TestDexQueryValues(t *testing.T) {
	v := DexQuery{
		Title: " tower ", IncludedTags: []string{"99999999-9999-9999-9999-999999999999", "nope"}, Status: []string{"ongoing", "lost"},
		Demographic: []string{"seinen"}, Original: []string{"ja", "Japanese"}, Year: 2024, Translated: "en", HasChapters: true,
		CreatedSince: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC), Limit: 500, Offset: 9990, Order: "relevance",
	}.Values()
	if v.Get("title") != "tower" || len(v["includedTags[]"]) != 1 || len(v["status[]"]) != 1 || len(v["originalLanguage[]"]) != 1 {
		t.Errorf("%v", v)
	}
	if v.Get("limit") != "24" || v.Get("offset") != "9976" {
		t.Errorf("paging stays inside MangaDex's 10000: %s %s", v.Get("limit"), v.Get("offset"))
	}
	if strings.Join(v["contentRating[]"], ",") != "safe,suggestive" {
		t.Errorf("ratings by default: %v", v["contentRating[]"])
	}
	if v.Get("order[relevance]") != "desc" || v.Get("createdAtSince") != "2026-10-07T12:00:00" || v.Get("availableTranslatedLanguage[]") != "en" || v.Get("hasAvailableChapters") != "true" {
		t.Errorf("%v", v)
	}
	// No words: relevance means nothing, so the most followed.
	if v := (DexQuery{Order: "relevance"}).Values(); v.Get("order[followedCount]") != "desc" {
		t.Errorf("%v", v)
	}
	if v := (DexQuery{Order: "bogus", Ascending: true, Ratings: []string{"safe", "x"}}).Values(); v.Get("order[followedCount]") != "asc" || len(v["contentRating[]"]) != 1 {
		t.Errorf("%v", v)
	}
}

func TestPickLists(t *testing.T) {
	seasonal, rec, self := PickLists([]DexList{{ID: "1", Name: "Seasonal: Summer 2026"}, {ID: "2", Name: "Staff Picks"}, {ID: "3", Name: "Self-Published"}, {ID: "4", Name: "Seasonal: Fall 2026"}, {ID: "5", Name: "Other"}})
	if seasonal.ID != "4" || rec.ID != "2" || self.ID != "3" {
		t.Errorf("%v %v %v", seasonal, rec, self)
	}
}

func TestDexTitleJSON(t *testing.T) {
	var j dexTitleJSON
	j.ID = "x"
	j.Attributes.Title = map[string]string{"ja-ro": "Tou"}
	j.Attributes.Description = map[string]string{"fr": "Une tour"}
	j.Attributes.Links = map[string]string{"al": "42", "engtl": "javascript:alert(1)"}
	got := j.title()
	if got.Title != "Tou" || got.Description != "Une tour" || got.AniListID != 42 || got.LicensedLink != "" {
		t.Errorf("%+v", got)
	}
	if ValidCoverFile("../../etc/passwd") || !ValidCoverFile("12345678-1234-1234-1234-123456789012.jpg") {
		t.Error("cover names")
	}
}
