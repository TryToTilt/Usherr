package api

// Home fork: the You page. One place for a person's own side of Mediarium:
// their taste in films and shows, their votes on everything, who they
// follow, their recipes and services. Their taste is private unless they
// share it with the household; then the household's sharers see what they
// lean towards and how much taste they have in common. Seeing other
// people's taste takes sharing your own.

import (
	"context"
	"database/sql"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/rdborg/mediarium/internal/auth"
)

type youTaste struct {
	Ready    bool       `json:"ready"`
	Liked    int        `json:"liked"`
	Disliked int        `json:"disliked"`
	Watched  int        `json:"watched"`
	Likes    []tasteTag `json:"likes"`
	Dislikes []tasteTag `json:"dislikes"`
}

func tasteSummary(p *tasteProfile, nFor, nAgainst int) youTaste {
	out := youTaste{Ready: p.ready}
	for _, sg := range p.signals {
		switch sg.via {
		case "liked":
			out.Liked++
		case "disliked":
			out.Disliked++
		case "watched":
			out.Watched++
		}
	}
	out.Likes, out.Dislikes = p.leanings(nFor, nAgainst)
	return out
}

func (s *Server) sharesTaste(ctx context.Context, uid int64) bool {
	var v int
	err := s.db.QueryRowContext(ctx, `SELECT share_taste FROM home_you WHERE user_id = ?`, uid).Scan(&v)
	return err == nil && v == 1
}

func (s *Server) countOf(ctx context.Context, q string, uid int64) int {
	var n int
	_ = s.db.QueryRowContext(ctx, q, uid).Scan(&n)
	return n
}

// GET /api/home/you: your taste, what you've voted on, and how much of the
// rest you've set up.
func (s *Server) handleYou(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := auth.UserFromContext(ctx)
	uid := userID(r)
	votes := map[string]map[string]int{}
	for _, v := range s.votesOf(ctx, uid, "", 0) {
		if votes[v.Kind] == nil {
			votes[v.Kind] = map[string]int{}
		}
		if v.Vote > 0 {
			votes[v.Kind]["liked"]++
		} else {
			votes[v.Kind]["disliked"]++
		}
	}
	services := 0
	var providers string
	if s.db.QueryRowContext(ctx, `SELECT providers FROM home_user_services WHERE user_id = ?`, uid).Scan(&providers) == nil {
		services = strings.Count(providers, ",") + 1
		if strings.TrimSpace(providers) == "[]" {
			services = 0
		}
	}
	name := ""
	if u != nil {
		name = strings.TrimSpace(u.FirstName)
		if name == "" {
			name = u.Username
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name":      name,
		"share":     s.sharesTaste(ctx, uid),
		"movie":     tasteSummary(s.myTaste(ctx, "movie"), 14, 8),
		"tv":        tasteSummary(s.myTaste(ctx, "tv"), 14, 8),
		"votes":     votes,
		"following": s.countOf(ctx, `SELECT COUNT(*) FROM home_follows WHERE user_id = ?`, uid),
		"recipes":   s.countOf(ctx, `SELECT COUNT(*) FROM home_recipes WHERE user_id = ?`, uid),
		"services":  services,
	})
}

// PUT /api/home/you/share {"share": true|false}: share your taste with the
// household, or stop.
func (s *Server) handleYouShare(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Share bool `json:"share"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	v := 0
	if in.Share {
		v = 1
	}
	if _, err := s.db.ExecContext(r.Context(), `INSERT INTO home_you (user_id, share_taste) VALUES (?, ?)
		ON CONFLICT (user_id) DO UPDATE SET share_taste = excluded.share_taste`, userID(r), v); err != nil {
		writeError(w, http.StatusInternalServerError, "That couldn't be saved.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"share": in.Share})
}

type youVote struct {
	votePayload
	To string `json:"to,omitempty"` // where it opens in Mediarium, when it can
}

// GET /api/home/you/votes: everything you've voted on, newest first, each
// with where it opens.
func (s *Server) handleYouVotes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	list := s.votesOf(ctx, userID(r), "", 0)
	out := make([]youVote, 0, len(list))
	local := func(q, ref string) int64 {
		var id int64
		if err := s.db.QueryRowContext(ctx, q, ref).Scan(&id); err != nil && err != sql.ErrNoRows {
			return 0
		}
		return id
	}
	for _, v := range list {
		to := ""
		switch v.Kind {
		case "movie":
			to = "/title/" + v.Ref
		case "tv":
			to = "/show/" + v.Ref
		case "album":
			to = "/music/release/" + v.Ref
		case "artist":
			if id := local(`SELECT id FROM artists WHERE mbid = ?`, v.Ref); id > 0 {
				to = "/music/artist/" + strconv.FormatInt(id, 10)
			}
		case "book":
			if id := local(`SELECT id FROM books WHERE ol_key = ?`, v.Ref); id > 0 {
				to = "/book/" + strconv.FormatInt(id, 10)
			} else if strings.HasPrefix(v.Ref, "OL") {
				to = "/books/work/" + v.Ref
			}
		case "comic":
			if id := local(`SELECT id FROM comic_series WHERE cv_id = ?`, v.Ref); id > 0 {
				to = "/comics/" + strconv.FormatInt(id, 10)
			}
		}
		out = append(out, youVote{v, to})
	}
	writeJSON(w, http.StatusOK, map[string]any{"votes": out})
}

type youPerson struct {
	Name     string     `json:"name"`
	InCommon int        `json:"inCommon"` // 0 to 100
	Both     []string   `json:"both"`     // what you both lean towards
	Likes    []tasteTag `json:"likes"`    // what they lean towards
}

// GET /api/home/you/household: the people sharing their taste, with what
// you have in common. Only for people who share theirs.
func (s *Server) handleYouHousehold(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := auth.UserFromContext(ctx)
	if me == nil || !s.sharesTaste(ctx, me.ID) {
		writeJSON(w, http.StatusOK, map[string]any{"share": false, "people": []youPerson{}})
		return
	}
	accounts, err := s.Auth.ListAccounts()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The household couldn't be read.")
		return
	}
	mine := map[string]*tasteProfile{"movie": s.myTaste(ctx, "movie"), "tv": s.myTaste(ctx, "tv")}
	people := []youPerson{}
	for i := range accounts {
		a := &accounts[i].User
		if a.ID == me.ID || a.Disabled || !s.sharesTaste(ctx, a.ID) {
			continue
		}
		p := youPerson{Name: strings.TrimSpace(a.FirstName), Both: []string{}}
		if p.Name == "" {
			p.Name = a.Username
		}
		var sims []float64
		both := map[string]float64{}
		for _, kind := range []string{"movie", "tv"} {
			theirs := s.tasteOfUser(ctx, a, kind)
			if !theirs.ready || !mine[kind].ready {
				continue
			}
			sims = append(sims, tasteCosine(mine[kind].w, theirs.w))
			for k, v := range mine[kind].w {
				if t := theirs.w[k]; v > 0 && t > 0 && k[0] != 'l' && k[0] != 'y' {
					both[k] += math.Sqrt(v * t)
				}
			}
			likes, _ := theirs.leanings(6, 0)
			p.Likes = append(p.Likes, likes...)
		}
		if len(sims) == 0 {
			continue // no taste yet
		}
		var sum float64
		for _, v := range sims {
			sum += v
		}
		p.InCommon = int(math.Round(100 * math.Max(0, sum/float64(len(sims)))))
		keys := make([]string, 0, len(both))
		for k := range both {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			return both[keys[i]] > both[keys[j]] || both[keys[i]] == both[keys[j]] && keys[i] < keys[j]
		})
		for _, k := range keys[:min(6, len(keys))] {
			p.Both = append(p.Both, k[2:])
		}
		people = append(people, p)
	}
	sort.Slice(people, func(i, j int) bool { return people[i].InCommon > people[j].InCommon })
	writeJSON(w, http.StatusOK, map[string]any{"share": true, "people": people})
}

// tasteCosine is how alike two tastes are, -1 to 1 (languages and decades
// left out: sharing English says little).
func tasteCosine(a, b map[string]float64) float64 {
	var dot, na, nb float64
	for k, v := range a {
		if k[0] == 'l' || k[0] == 'y' {
			continue
		}
		dot += v * b[k]
		na += v * v
	}
	for k, v := range b {
		if k[0] != 'l' && k[0] != 'y' {
			nb += v * v
		}
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / math.Sqrt(na*nb)
}
