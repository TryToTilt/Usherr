package api

import (
	"testing"
	"time"
)

func TestAired(t *testing.T) {
	today := time.Now().Format("2006-01-02")
	tomorrow := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	if !aired("2020-01-01") || !aired(today) || aired(tomorrow) || aired("") {
		t.Fatal("aired: past and today yes; tomorrow and unknown no")
	}
}
