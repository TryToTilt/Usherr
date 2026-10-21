package api

// Usherr: a film's cinema formats from IMDb's technical specifications — how
// it was shot (65mm or 35mm film, digital) and how cinemas showed it (IMAX,
// IMAX 70mm, Dolby Cinema, 70mm) — for the Marquee's badges. TMDB's release
// notes are the fallback: they name a format for some films only.
//
// IMDb has no open API: this asks the service IMDb's own pages use, once
// per film, one film at a time, in the background, and remembers the answer.
// IMDb allows its data for limited, personal, non-commercial use; the Marquee
// setting "Cinema formats from IMDb" turns it off.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

type imdbSpecs struct {
	Processes []string
	Printed   []string
	Negatives []string
	Colors    []string
}

type imdbSpecsEntry struct {
	done    bool
	formats []string
	at      time.Time
	retry   time.Time // a failed ask is tried again after this
}

var (
	imdbSpecsMu    sync.Mutex
	imdbSpecsCache = map[string]*imdbSpecsEntry{}
	imdbSpecsQueue = make(chan string, 500)
	imdbSpecsOnce  sync.Once
	imdbIDRe       = regexp.MustCompile(`^tt\d{5,10}$`)
	imdbClient     = &http.Client{Timeout: 20 * time.Second}
	imdbGraphQL    = "https://caching.graphql.imdb.com/"
)

// imdbFormats is what IMDb says about a film's formats, once it has been
// asked; the first ask queues it and answers false.
func imdbFormats(imdbID string) ([]string, bool) {
	if !imdbIDRe.MatchString(imdbID) {
		return nil, false
	}
	imdbSpecsOnce.Do(func() { go imdbSpecsWorker() })
	imdbSpecsMu.Lock()
	defer imdbSpecsMu.Unlock()
	e, ok := imdbSpecsCache[imdbID]
	if ok && (e.done || time.Now().Before(e.retry)) {
		return e.formats, e.done
	}
	if !ok {
		e = &imdbSpecsEntry{}
		imdbSpecsCache[imdbID] = e
	}
	e.retry = time.Now().Add(time.Hour) // queued: not queued again for an hour
	select {
	case imdbSpecsQueue <- imdbID:
	default: // a long queue: asked again later
	}
	return nil, false
}

// imdbSpecsWorker asks IMDb for one film at a time, gently.
func imdbSpecsWorker() {
	for id := range imdbSpecsQueue {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		specs, err := fetchIMDbSpecs(ctx, id)
		cancel()
		imdbSpecsMu.Lock()
		e := imdbSpecsCache[id]
		if err != nil {
			slog.Debug("imdb: technical specs", "imdbId", id, "err", err)
		} else if e != nil {
			e.done, e.formats, e.at = true, imdbFormatLabels(specs), time.Now()
		}
		imdbSpecsMu.Unlock()
		time.Sleep(1500 * time.Millisecond)
	}
}

const imdbSpecsQuery = `query ($id: ID!) { title(id: $id) { technicalSpecifications {
  processes { items { process } } printedFormats { items { printedFormat } }
  negativeFormats { items { negativeFormat } } colorations { items { text } } } } }`

func fetchIMDbSpecs(ctx context.Context, id string) (imdbSpecs, error) {
	body, _ := json.Marshal(map[string]any{"query": imdbSpecsQuery, "variables": map[string]string{"id": id}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, imdbGraphQL, bytes.NewReader(body))
	if err != nil {
		return imdbSpecs{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Language", "en-US")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0 Safari/537.36")
	req.Header.Set("x-imdb-client-name", "imdb-web-next")
	resp, err := imdbClient.Do(req)
	if err != nil {
		return imdbSpecs{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return imdbSpecs{}, fmt.Errorf("imdb answered %d", resp.StatusCode)
	}
	var out struct {
		Data struct {
			Title *struct {
				Tech *struct {
					Processes struct {
						Items []struct {
							Process string `json:"process"`
						} `json:"items"`
					} `json:"processes"`
					Printed struct {
						Items []struct {
							Format string `json:"printedFormat"`
						} `json:"items"`
					} `json:"printedFormats"`
					Negatives struct {
						Items []struct {
							Format string `json:"negativeFormat"`
						} `json:"items"`
					} `json:"negativeFormats"`
					Colors struct {
						Items []struct {
							Text string `json:"text"`
						} `json:"items"`
					} `json:"colorations"`
				} `json:"technicalSpecifications"`
			} `json:"title"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return imdbSpecs{}, err
	}
	var sp imdbSpecs
	if t := out.Data.Title; t != nil && t.Tech != nil {
		for _, x := range t.Tech.Processes.Items {
			sp.Processes = append(sp.Processes, x.Process)
		}
		for _, x := range t.Tech.Printed.Items {
			sp.Printed = append(sp.Printed, x.Format)
		}
		for _, x := range t.Tech.Negatives.Items {
			sp.Negatives = append(sp.Negatives, x.Format)
		}
		for _, x := range t.Tech.Colors.Items {
			sp.Colors = append(sp.Colors, x.Text)
		}
	}
	return sp, nil
}

// imdbFormatLabels turns IMDb's specs into badges: how cinemas showed the
// film (IMAX 70mm, IMAX, Dolby Cinema, 70mm), how it was shot (65mm film,
// 35mm film, 16mm film, Digital) and Black & white.
func imdbFormatLabels(sp imdbSpecs) []string {
	has := func(list []string, words ...string) bool {
		for _, s := range list {
			l := strings.ToLower(s)
			for _, w := range words {
				if strings.Contains(l, w) {
					return true
				}
			}
		}
		return false
	}
	var out []string
	imax := has(sp.Processes, "imax") || has(sp.Printed, "imax")
	print70 := has(sp.Printed, "70 mm", "70mm")
	if imax && print70 {
		out = append(out, "IMAX 70mm")
	} else if imax {
		out = append(out, "IMAX")
	}
	if has(sp.Processes, "dolby vision") || has(sp.Printed, "dolby vision") {
		out = append(out, "Dolby Cinema")
	}
	if print70 {
		out = append(out, "70mm")
	}
	film := false
	for _, f := range []struct{ word, label string }{{"65 mm", "65mm film"}, {"70 mm", "65mm film"}, {"35 mm", "35mm film"}, {"16 mm", "16mm film"}} {
		if has(sp.Negatives, f.word) && !slices.Contains(out, f.label) {
			out = append(out, f.label)
			film = true
		}
	}
	if !film && len(sp.Negatives) > 0 {
		out = append(out, "Digital")
	}
	if len(sp.Colors) > 0 && has(sp.Colors, "black and white") && !has(sp.Colors, "color") {
		out = append(out, "Black & white")
	}
	return out
}
