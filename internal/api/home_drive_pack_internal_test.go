package api

import (
	"context"
	"testing"
)

// A streamed file knows what it is, so its release can be searched for it: and
// how long its season is, the only way to place a file in an obfuscated pack.
func TestDriveWantDescribesTheFile(t *testing.T) {
	s, _, _ := driveTestServer(t)
	for _, q := range []string{
		`INSERT INTO streamed_files (id, title_id, season, episode, air_date, release, nzb, inner_file, size) VALUES (20, 3, 2, 3, '2026-10-16', 'Severance.S02.1080p', 'p.nzb', 'x.mkv', 10)`,
		`INSERT INTO streamed_files (id, title_id, season, episode) VALUES (21, 3, 2, 1), (22, 3, 2, 2), (23, 3, 3, 1)`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	w, err := s.driveWant(context.Background(), 20)
	if err != nil {
		t.Fatal(err)
	}
	if w.Season != 2 || w.Episode != 3 || w.AirDate != "2026-10-16" || w.Release != "Severance.S02.1080p" || w.InnerFile != "x.mkv" || w.SeasonEpisodes != 3 {
		t.Errorf("%+v", w)
	}
	film, err := s.driveWant(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if film.Episode != 0 || film.SeasonEpisodes != 0 || film.InnerFile != "Arrival.mkv" {
		t.Errorf("a film: %+v", film)
	}
}
