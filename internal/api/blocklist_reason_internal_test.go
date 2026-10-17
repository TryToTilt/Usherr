package api

import (
	"testing"

	"github.com/rdborg/mediarium/internal/blocklist"
	"github.com/rdborg/mediarium/internal/queue"
)

// "Blocklist & search again" says it was a person, and keeps why the release
// was bad instead of replacing it.
func TestPersonBlocklistReasonKeepsTheRealReason(t *testing.T) {
	s := newAuditServer(t)
	const auto = "Nosferatu.1922.1080p.BluRay.x264-OLDTiMERS"
	if err := s.Blocklist.Add(blocklist.Entry{ReleaseTitle: auto, Reason: "couldn't unpack the download: archive is password protected"}); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		item queue.Item
		want string
	}{
		{"already blocklisted by itself", queue.Item{ReleaseTitle: auto, Error: "Couldn't unpack the download."},
			"Blocklisted by you: couldn't unpack the download: archive is password protected"},
		{"not blocklisted yet, failed", queue.Item{ReleaseTitle: "Other.Release-GRP", Error: "The download could not be finished."},
			"Blocklisted by you: The download could not be finished."},
		{"nothing to go on", queue.Item{ReleaseTitle: "Third.Release-GRP"}, "Blocklisted by you"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.personBlocklistReason(tt.item)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("reason = %q, want %q", got, tt.want)
			}
		})
	}

	// Blocklisting it by hand a second time does not say "by you" twice.
	first, _ := s.personBlocklistReason(queue.Item{ReleaseTitle: auto})
	if err := s.Blocklist.Add(blocklist.Entry{ReleaseTitle: auto, Reason: first}); err != nil {
		t.Fatal(err)
	}
	if again, _ := s.personBlocklistReason(queue.Item{ReleaseTitle: auto}); again != first {
		t.Fatalf("second time: %q, want %q", again, first)
	}
}
