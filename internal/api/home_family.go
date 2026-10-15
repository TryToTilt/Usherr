package api

// Home fork: the family filter on Discover. For each title, the youngest age
// it suits, worked out once and kept (home_family_ages):
//  1. Common Sense Media's age, when MDBList has given it (home_scores);
//  2. else the US rating from TMDB (G, PG, PG-13, R; TV-Y … TV-MA);
//  3. else an estimate from the AI, a batch of titles at a time.
// Discover asks for the titles it's about to show; those not worked out yet
// in this call come back as pending, and it asks again.

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/ai"
)

const (
	familyAsk       = 80                  // titles one call may ask about
	familyLookups   = 24                  // TMDB ratings read in one call
	familyAIBatch   = 30                  // titles in one AI question
	familyAIDaily   = 600                 // AI-estimated titles a day, for everyone
	familyRetryNone = 30 * 24 * time.Hour // look again at titles nothing was known about
)

// ratingAge turns a US rating into the youngest age it suits; ok is false
// for no rating or "NR".
func ratingAge(r string) (int, bool) {
	switch strings.ToUpper(strings.TrimSpace(r)) {
	case "G", "TV-Y", "TV-G":
		return 0, true
	case "TV-Y7", "TV-Y7-FV":
		return 7, true
	case "PG":
		return 7, true // Common Sense puts most PG films at 6 to 8
	case "TV-PG":
		return 10, true
	case "PG-13":
		return 13, true
	case "TV-14":
		return 14, true
	case "R", "TV-MA":
		return 17, true
	case "NC-17":
		return 18, true
	}
	return 0, false
}

type familyItem struct {
	ID       int      `json:"id"`
	Title    string   `json:"title"`
	Year     int      `json:"year"`
	Genres   []string `json:"genres,omitempty"`
	Overview string   `json:"overview,omitempty"` // a little of it, for the AI
}

// horror says whether a title is listed as horror: whatever the AI thinks,
// that's no younger than 13.
func (it familyItem) horror() bool {
	for _, g := range it.Genres {
		if strings.EqualFold(g, "Horror") {
			return true
		}
	}
	return false
}

var (
	familyAIMu    sync.Mutex
	familyAIDay   string
	familyAIUsed  int
	familyAIBusy  sync.Mutex // one AI question at a time
	familyFillers sync.Map   // "kind:id" -> being worked out
)

func familyAIBudget(n int) bool {
	familyAIMu.Lock()
	defer familyAIMu.Unlock()
	today := time.Now().Format("2006-01-02")
	if familyAIDay != today {
		familyAIDay, familyAIUsed = today, 0
	}
	if familyAIUsed+n > familyAIDaily {
		return false
	}
	familyAIUsed += n
	return true
}

// familyAIRefund gives back allowance an AI question didn't use (it failed).
func familyAIRefund(n int) {
	familyAIMu.Lock()
	defer familyAIMu.Unlock()
	familyAIUsed = max(0, familyAIUsed-n)
}

func (s *Server) saveFamilyAge(ctx context.Context, kind string, id, age int, source, rating string) {
	_, _ = s.db.ExecContext(ctx, `INSERT OR REPLACE INTO home_family_ages (kind, tmdb_id, age, source, rating, fetched_at)
		VALUES (?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))`, kind, id, age, source, rating)
}

// keptFamilyAges reads the ages worked out before; titles nothing was known
// about are looked at again after a month.
func (s *Server) keptFamilyAges(ctx context.Context, kind string, ids []int) map[int]int {
	out := map[int]int{}
	if len(ids) == 0 {
		return out
	}
	args := []any{kind}
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT tmdb_id, age, source, fetched_at FROM home_family_ages WHERE kind = ? AND tmdb_id IN (`+strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")+`)`, args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id, age int
		var source, at string
		if rows.Scan(&id, &age, &source, &at) != nil {
			continue
		}
		if when, _ := time.Parse(time.RFC3339, at); source == "none" && time.Since(when) > familyRetryNone {
			continue
		}
		out[id] = age
	}
	return out
}

// POST /api/home/family {"kind":"movie","items":[{"id","title","year"}]}:
// each title's youngest suitable age (-1: not known), and the ones still
// being worked out.
func (s *Server) handleFamily(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Kind  string       `json:"kind"`
		Items []familyItem `json:"items"`
	}
	if err := decodeJSON(r, &in); err != nil || (in.Kind != "movie" && in.Kind != "tv") {
		writeError(w, http.StatusBadRequest, "Choose films or shows.")
		return
	}
	if len(in.Items) > familyAsk {
		in.Items = in.Items[:familyAsk]
	}
	ctx := r.Context()
	ids := make([]int, 0, len(in.Items))
	for _, it := range in.Items {
		ids = append(ids, it.ID)
	}
	ages := s.keptFamilyAges(ctx, in.Kind, ids)
	// Common Sense Media's age wins whenever MDBList has given it, even for a
	// title whose rating was read first.
	if csm, _ := s.cachedScores(ctx, in.Kind, ids); len(csm) > 0 {
		for id, sc := range csm {
			if sc.CommonSense > 0 {
				ages[id] = sc.CommonSense
			}
		}
	}
	var missing []familyItem
	for _, it := range in.Items {
		if _, ok := ages[it.ID]; !ok && it.ID > 0 {
			missing = append(missing, it)
		}
	}
	if len(missing) > 0 {
		wctx, cancel := context.WithTimeout(ctx, 12*time.Second)
		s.workOutFamily(wctx, in.Kind, missing)
		cancel()
		for id, age := range s.keptFamilyAges(ctx, in.Kind, idsOf(missing)) {
			ages[id] = age
		}
	}
	pending := []int{}
	out := map[string]int{}
	for _, id := range ids {
		if age, ok := ages[id]; ok {
			out[strconv.Itoa(id)] = age
		} else {
			pending = append(pending, id)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ages": out, "pending": pending})
}

func idsOf(list []familyItem) []int {
	out := make([]int, len(list))
	for i, it := range list {
		out[i] = it.ID
	}
	return out
}

// workOutFamily works out the ages of titles not known yet: Common Sense
// first, then the US rating, then the AI for what's left.
func (s *Server) workOutFamily(ctx context.Context, kind string, items []familyItem) {
	// Titles another request is already working out are left to it.
	var mine []familyItem
	for _, it := range items {
		k := kind + ":" + strconv.Itoa(it.ID)
		if _, busy := familyFillers.LoadOrStore(k, true); !busy {
			mine = append(mine, it)
		}
	}
	release := func(list []familyItem) {
		for _, it := range list {
			familyFillers.Delete(kind + ":" + strconv.Itoa(it.ID))
		}
	}
	// The AI's titles are let go when it has answered (in the background).
	var toAI []familyItem
	defer func() {
		aside := map[int]bool{}
		for _, it := range toAI {
			aside[it.ID] = true
		}
		var rest []familyItem
		for _, it := range mine {
			if !aside[it.ID] {
				rest = append(rest, it)
			}
		}
		release(rest)
	}()
	if len(mine) == 0 {
		return
	}

	csm, _ := s.cachedScores(ctx, kind, idsOf(mine))
	var unrated []familyItem
	var toRate []familyItem
	for _, it := range mine {
		if sc, ok := csm[it.ID]; ok && sc.CommonSense > 0 {
			s.saveFamilyAge(ctx, kind, it.ID, sc.CommonSense, "csm", "")
			continue
		}
		toRate = append(toRate, it)
	}
	if len(toRate) > familyLookups {
		toRate = toRate[:familyLookups] // the rest next time
	}
	if len(toRate) > 0 && s.TMDB().HasAPIKey() {
		var mu sync.Mutex
		var wg sync.WaitGroup
		sem := make(chan struct{}, 4)
		for _, it := range toRate {
			wg.Add(1)
			go func(it familyItem) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				var rating string
				var err error
				if kind == "movie" {
					rating, err = s.TMDB().MovieUSRating(ctx, it.ID)
				} else {
					rating, err = s.TMDB().ShowUSRating(ctx, it.ID)
				}
				if err != nil {
					return // try again another time
				}
				if age, ok := ratingAge(rating); ok {
					s.saveFamilyAge(ctx, kind, it.ID, age, "rating", rating)
					return
				}
				mu.Lock()
				unrated = append(unrated, it)
				mu.Unlock()
			}(it)
		}
		wg.Wait()
	}
	if len(unrated) == 0 {
		return
	}
	cfg, ok := s.aiConfig()
	if !ok { // no AI: nothing more to learn for now
		for _, it := range unrated {
			s.saveFamilyAge(ctx, kind, it.ID, -1, "none", "")
		}
		return
	}
	if !familyAIBudget(len(unrated)) {
		return // the day's AI allowance is spent: pending until tomorrow
	}
	// The AI can take a while (a local model has to wake up first), so it
	// answers in the background; Discover asks again until it has.
	toAI = unrated
	s.background(func() {
		defer release(unrated)
		familyAIBusy.Lock()
		defer familyAIBusy.Unlock()
		actx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		for start := 0; start < len(unrated); start += familyAIBatch {
			batch := unrated[start:min(start+familyAIBatch, len(unrated))]
			ages, err := s.askFamilyAges(actx, cfg, kind, batch)
			if err != nil {
				log.Printf("family: AI: %v", err)
				familyAIRefund(len(unrated) - start)
				return
			}
			for i, it := range batch {
				if age, ok := ages[i+1]; ok && age >= 0 && age <= 18 {
					if it.horror() {
						age = max(age, 13)
					}
					s.saveFamilyAge(actx, kind, it.ID, age, "ai", "")
				} else {
					s.saveFamilyAge(actx, kind, it.ID, -1, "none", "")
				}
			}
		}
	})
}

const familySystem = `You estimate the youngest age a film or show suits, the way Common Sense Media would: think of violence, scary scenes, sex, language and themes. You're given a numbered list of titles with their year, genres and a line about them. Judge by what they are, not by words in the title. Reply with a JSON object only, mapping each number to an age from 0 to 18, like {"1": 7, "2": 13}. Use -1 for a title you don't know. Don't add anything else.`

func (s *Server) askFamilyAges(ctx context.Context, cfg ai.Config, kind string, items []familyItem) (map[int]int, error) {
	what := "films"
	if kind == "tv" {
		what = "shows"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "These are %s:\n", what)
	for i, it := range items {
		fmt.Fprintf(&sb, "%d. %s", i+1, it.Title)
		if it.Year > 0 {
			fmt.Fprintf(&sb, " (%d)", it.Year)
		}
		if len(it.Genres) > 0 {
			fmt.Fprintf(&sb, " [%s]", strings.Join(it.Genres, ", "))
		}
		if o := strings.TrimSpace(it.Overview); o != "" {
			if r := []rune(o); len(r) > 220 {
				o = string(r[:220]) + "…"
			}
			fmt.Fprintf(&sb, ": %s", o)
		}
		sb.WriteString("\n")
	}
	raw, err := ai.Ask(ctx, cfg, ai.Request{System: familySystem, Prompt: sb.String(), JSON: true, MaxTokens: 500})
	if err != nil {
		return nil, err
	}
	var ans map[string]json.Number
	if err := json.Unmarshal([]byte(ai.JSONObject(raw)), &ans); err != nil {
		return nil, fmt.Errorf("couldn't read the answer: %w", err)
	}
	out := map[int]int{}
	for k, v := range ans {
		n, err1 := strconv.Atoi(strings.TrimSpace(k))
		age, err2 := v.Int64()
		if err1 == nil && err2 == nil {
			out[n] = int(age)
		}
	}
	return out, nil
}
