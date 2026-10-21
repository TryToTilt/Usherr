package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

func TestIMDbFormatLabels(t *testing.T) {
	cases := []struct {
		name string
		sp   imdbSpecs
		want []string
	}{
		{"Oppenheimer", imdbSpecs{Processes: []string{"IMAX", "Panavision Super 70", "Super 35"}, Printed: []string{"35 mm", "70 mm", "DCP Digital Cinema Package"}, Negatives: []string{"35 mm", "65 mm"}, Colors: []string{"Color", "Black and White"}},
			[]string{"IMAX 70mm", "70mm", "65mm film", "35mm film"}},
		{"a digital IMAX and Dolby Cinema film", imdbSpecs{Processes: []string{"Digital Intermediate (4K)", "IMAX", "Dolby Vision"}, Printed: []string{"D-Cinema"}, Negatives: []string{"Codex", "ARRIRAW (4.5K)"}},
			[]string{"IMAX", "Dolby Cinema", "Digital"}},
		{"black and white on film", imdbSpecs{Negatives: []string{"35 mm"}, Colors: []string{"Black and White"}}, []string{"35mm film", "Black & white"}},
		{"nothing known", imdbSpecs{}, nil},
	}
	for _, c := range cases {
		if got := imdbFormatLabels(c.sp); !slices.Equal(got, c.want) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

func TestFetchIMDbSpecs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(b), `"id":"tt15398776"`) {
			http.Error(w, "wrong id", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"data":{"title":{"technicalSpecifications":{"processes":{"items":[{"process":"IMAX"}]},"printedFormats":{"items":[{"printedFormat":"70 mm"}]},"negativeFormats":{"items":[{"negativeFormat":"65 mm"}]},"colorations":{"items":[{"text":"Color"}]}}}}}`))
	}))
	defer srv.Close()
	old := imdbGraphQL
	imdbGraphQL = srv.URL
	defer func() { imdbGraphQL = old }()
	sp, err := fetchIMDbSpecs(context.Background(), "tt15398776")
	if err != nil {
		t.Fatal(err)
	}
	if got := imdbFormatLabels(sp); !slices.Equal(got, []string{"IMAX 70mm", "70mm", "65mm film"}) {
		t.Fatalf("labels: %v", got)
	}
	if _, err := fetchIMDbSpecs(context.Background(), "tt0000001"); err == nil {
		t.Fatal("a refused ask should be an error")
	}
	if _, ok := imdbFormats("not-an-id"); ok {
		t.Fatal("a bad id is never asked")
	}
}
