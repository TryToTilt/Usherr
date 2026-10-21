package library_test

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/store"
)

// A show added before the country was stored (or just added) has no country
// until it is read; "none" is a different answer from "not read yet".
func TestSeriesOriginCountry(t *testing.T) {
	repo := newRepo(t)
	us, err := repo.AddSeries(library.Series{TMDBID: 34307, Title: "Shameless", Year: 2011, Monitored: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	uk, err := repo.AddSeries(library.Series{TMDBID: 2161, Title: "Shameless", Year: 2004, Monitored: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if us.OriginCountry != nil {
		t.Fatalf("a new show should have an unknown country, got %#v", us.OriginCountry)
	}

	gaps, err := repo.MissingOriginCountry(10)
	if err != nil || len(gaps) != 2 {
		t.Fatalf("gaps = %+v, err %v; want both shows", gaps, err)
	}

	if err := repo.SetSeriesOriginCountry(us.ID, []string{"us"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetSeriesOriginCountry(uk.ID, []string{}); err != nil { // TMDB names none
		t.Fatal(err)
	}
	got, _ := repo.GetSeries(us.ID)
	if !reflect.DeepEqual(got.OriginCountry, []string{"US"}) {
		t.Errorf("US show country = %#v", got.OriginCountry)
	}
	got, _ = repo.GetSeries(uk.ID)
	if got.OriginCountry == nil || len(got.OriginCountry) != 0 {
		t.Errorf("a show with no country should read back as an empty, non-nil list, got %#v", got.OriginCountry)
	}
	if gaps, _ := repo.MissingOriginCountry(10); len(gaps) != 0 {
		t.Errorf("shows with a known (even empty) country are not gaps: %+v", gaps)
	}

	// Several countries survive the round trip, in order, lower case is tidied.
	if err := repo.SetSeriesOriginCountry(us.ID, []string{"ca", " US "}); err != nil {
		t.Fatal(err)
	}
	got, _ = repo.GetSeries(us.ID)
	if !reflect.DeepEqual(got.OriginCountry, []string{"CA", "US"}) {
		t.Errorf("two countries came back as %#v", got.OriginCountry)
	}
}

// The migration adds the column as nullable, with no value filled in.
func TestOriginCountryColumnIsNullable(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO series (tmdb_id, title) VALUES (1, 'Old show')`); err != nil {
		t.Fatalf("insert without a country: %v", err)
	}
	var v sql.NullString
	if err := db.QueryRow(`SELECT origin_country FROM series WHERE tmdb_id = 1`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v.Valid {
		t.Errorf("origin_country should start as NULL, got %q", v.String)
	}
}
