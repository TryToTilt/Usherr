package configfile

import (
	"fmt"
	"strings"
	"testing"
)

// A file that still names the old priority levels by their words must not
// show a change for an indexer that already has the matching number.
func TestIndexerPriorityWordsMatchNumbers(t *testing.T) {
	const stored = `[{"id":1,"name":"NZB","kind":"newznab","baseUrl":"https://nzb.example","protocol":"usenet","enabled":true,"hasApiKey":true,"priority":%d}]`
	cases := []struct {
		name   string
		have   int
		want   string
		change string // what the plan must name; "" for no change at all
	}{
		{"preferred is 10", 10, "preferred", ""},
		{"normal is 25", 25, "normal", ""},
		{"last resort is 40", 40, "last resort", ""},
		{"a number as text", 12, `"12"`, ""},
		{"a plain number", 12, "12", ""},
		{"a different level is a change", 25, "preferred", "indexers[NZB].priority"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := fakeMediarium(t, map[string]string{"/api/indexers": fmt.Sprintf(stored, tc.have)})
			got, err := planText(t, c, "indexers:\n  - name: NZB\n    priority: "+tc.want+"\n")
			if err != nil {
				t.Fatal(err)
			}
			if tc.change == "" && got != "" {
				t.Fatalf("want no change, got %q", got)
			}
			if tc.change != "" && !strings.Contains(got, tc.change) {
				t.Fatalf("want a change naming %q, got %q", tc.change, got)
			}
		})
	}
}
