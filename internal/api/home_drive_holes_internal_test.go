package api

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/download"
)

// holeyReader stands in for a release a read found too many missing articles in.
type holeyReader struct {
	*bytes.Reader
	verdict download.HoleVerdict
	asked   chan struct{}
}

func (h *holeyReader) ReadAt([]byte, int64) (int, error) { return 0, download.ErrTooManyHoles }

func (h *holeyReader) ConfirmHoles(context.Context) download.HoleVerdict {
	defer close(h.asked)
	return h.verdict
}

func TestDriveKeepsAFileUntilItsHolesAreConfirmed(t *testing.T) {
	const film = "/dav/Movies/Arrival%20(2016)%20%5Btmdbid-329865%5D/Arrival%20(2016).mkv"
	for _, c := range []struct {
		name    string
		verdict download.HoleVerdict
		want    string
	}{
		{"gone", download.HolesGone, "dead"},
		{"back", download.HolesBearable, "ok"},
		{"couldn't ask", download.HolesUnknown, "ok"},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, ts, _ := driveTestServer(t)
			h := &holeyReader{verdict: c.verdict, asked: make(chan struct{})}
			old := driveReader
			driveReader = func(*Server, context.Context, int64) (io.ReaderAt, error) { return h, nil }
			t.Cleanup(func() {
				driveReader = old
				driveOpenMu.Lock()
				delete(driveHoleChecked, 10)
				driveOpenMu.Unlock()
			})
			driveDo(t, s, ts, http.MethodGet, film, "", map[string]string{"Range": "bytes=100-199"})
			select {
			case <-h.asked:
			case <-time.After(5 * time.Second):
				t.Fatal("the servers weren't asked")
			}
			var health string
			for range 200 { // the verdict lands just after the question is answered
				_ = s.db.QueryRow(`SELECT health FROM streamed_files WHERE id = 10`).Scan(&health)
				if health == c.want {
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
			if health != c.want {
				t.Errorf("health %q, want %q", health, c.want)
			}
		})
	}
}

// A plain missing-article answer from a stand-in, with nobody to ask, changes
// nothing: one read can't take a title off the drive.
func TestDriveOneMissingReadChangesNothing(t *testing.T) {
	s, ts, _ := driveTestServer(t)
	old := driveReader
	driveReader = func(*Server, context.Context, int64) (io.ReaderAt, error) {
		return missingReader{}, nil
	}
	t.Cleanup(func() { driveReader = old })
	driveDo(t, s, ts, http.MethodGet, "/dav/Movies/Arrival%20(2016)%20%5Btmdbid-329865%5D/Arrival%20(2016).mkv", "", map[string]string{"Range": "bytes=100-199"})
	if got := healthOf(t, s, 10); got != "ok" {
		t.Errorf("health %q", got)
	}
}

type missingReader struct{}

func (missingReader) ReadAt([]byte, int64) (int, error) {
	return 0, fmt.Errorf("read: %w", download.ErrArticleMissing)
}

// A release that won't open leaves the drive only if it still won't a while later.
func TestDriveOpenMissedNeedsTwoStrikesApart(t *testing.T) {
	s, _, _ := driveTestServer(t)
	t.Cleanup(func() {
		driveOpenMu.Lock()
		delete(driveOpenStrikes, 10)
		driveOpenMu.Unlock()
	})
	s.driveOpenMissed(10)
	s.driveOpenMissed(10) // a player asking again straight away
	if got := healthOf(t, s, 10); got != "ok" {
		t.Fatalf("after two quick tries: %q", got)
	}
	driveOpenMu.Lock()
	driveOpenStrikes[10] = time.Now().Add(-driveStrikeGap - time.Second)
	driveOpenMu.Unlock()
	s.driveOpenMissed(10)
	if got := healthOf(t, s, 10); got != "dead" {
		t.Errorf("still missing later: %q", got)
	}
}
