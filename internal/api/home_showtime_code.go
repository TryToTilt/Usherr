package api

// Usherr: Community, step 4. A showtime code: a watch party's film, start
// time and pre-show length in 13 letters and numbers (HXK4-7QPM-2RD9C), on
// the party's card in Discord. Pasted into another Usherr (Showtime, Join a
// watch party), it books the same film for the same moment there, with no
// connection between the two Usherrs. Only the pre-show's length crosses
// over; each Usherr plays its own clips and trailers in it. The last character is a check, so a
// mistyped code is refused rather than booking the wrong film.

import (
	"errors"
	"net/http"
	"strings"
	"time"
)

// Crockford's base 32: no I, L, O or U, so nothing is mistaken for 1 or 0.
const codeAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

const (
	codeTMDBBits    = 24 // films up to 16 million
	codeMinuteBits  = 30 // minutes since 1970, enough for two thousand years
	codePreshowBits = 6  // up to 63 minutes
	codeDataChars   = (codeTMDBBits + codeMinuteBits + codePreshowBits) / 5
)

type showtimeCode struct {
	TMDBID     int
	AtMs       int64 // to the minute
	PreshowMin int
}

func codeCheck(digits []int) int {
	sum := 0
	for i, d := range digits {
		sum += d * (i + 1)
	}
	return sum % 32
}

// encodeShowtimeCode makes the code for a showtime (its start to the minute).
func encodeShowtimeCode(c showtimeCode) string {
	n := uint64(c.TMDBID)&(1<<codeTMDBBits-1)<<(codeMinuteBits+codePreshowBits) |
		uint64(c.AtMs/60_000)&(1<<codeMinuteBits-1)<<codePreshowBits |
		uint64(min(max(c.PreshowMin, 0), 63))
	digits := make([]int, codeDataChars)
	for i := codeDataChars - 1; i >= 0; i-- {
		digits[i] = int(n & 31)
		n >>= 5
	}
	digits = append(digits, codeCheck(digits))
	var b strings.Builder
	for i, d := range digits {
		if i == 4 || i == 8 {
			b.WriteByte('-')
		}
		b.WriteByte(codeAlphabet[d])
	}
	return b.String()
}

var errBadCode = errors.New("not a showtime code")

// findShowtimeCode reads a code from what was pasted: the code alone, with or
// without dashes, or a whole message with the code in it. Letters people mix
// up are read as what they look like (O as 0, I and L as 1).
func findShowtimeCode(s string) (showtimeCode, error) {
	for _, word := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == '\n' || r == '\t' || r == '`' || r == '*' || r == ',' || r == '.' || r == ':' || r == '(' || r == ')'
	}) {
		if c, err := decodeShowtimeCode(word); err == nil {
			return c, nil
		}
	}
	return showtimeCode{}, errBadCode
}

func decodeShowtimeCode(s string) (showtimeCode, error) {
	var digits []int
	for _, r := range strings.ToUpper(s) {
		switch r {
		case '-', ' ':
			continue
		case 'O':
			r = '0'
		case 'I', 'L':
			r = '1'
		}
		i := strings.IndexRune(codeAlphabet, r)
		if i < 0 {
			return showtimeCode{}, errBadCode
		}
		digits = append(digits, i)
	}
	if len(digits) != codeDataChars+1 || codeCheck(digits[:codeDataChars]) != digits[codeDataChars] {
		return showtimeCode{}, errBadCode
	}
	var n uint64
	for _, d := range digits[:codeDataChars] {
		n = n<<5 | uint64(d)
	}
	c := showtimeCode{
		TMDBID:     int(n >> (codeMinuteBits + codePreshowBits) & (1<<codeTMDBBits - 1)),
		AtMs:       int64(n>>codePreshowBits&(1<<codeMinuteBits-1)) * 60_000,
		PreshowMin: int(n & (1<<codePreshowBits - 1)),
	}
	if c.TMDBID <= 0 || c.AtMs <= 0 || c.PreshowMin > 60 {
		return showtimeCode{}, errBadCode
	}
	return c, nil
}

// codeFor is a showtime's code.
func codeFor(st showtime) string {
	return encodeShowtimeCode(showtimeCode{TMDBID: st.TMDBID, AtMs: st.AtMs, PreshowMin: st.PreshowMin})
}

// POST /api/home/showtimes/join {"code", "playOn"} (member): book the
// showtime a code is for, in this Usherr. Joining one already booked here
// gives that booking back.
func (s *Server) handleJoinShowtime(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code   string `json:"code"`
		PlayOn string `json:"playOn"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	c, err := findShowtimeCode(body.Code)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a showtime code. Copy it from the watch party's card in Discord: three groups of letters and numbers, like HXK4-7QPM-2RD9C.")
		return
	}
	if c.AtMs < time.Now().Add(-time.Minute).UnixMilli() {
		writeError(w, http.StatusBadRequest, "That watch party has already started. Start the film yourself to catch up.")
		return
	}
	var have struct {
		id    int64
		title string
	}
	if s.db.QueryRowContext(r.Context(), `SELECT id, title FROM showtimes WHERE tmdb_id = ? AND at_ms = ? AND state <> 'done'`, c.TMDBID, c.AtMs).Scan(&have.id, &have.title) == nil {
		writeJSON(w, http.StatusOK, map[string]any{"id": have.id, "title": have.title, "atMs": c.AtMs, "already": true})
		return
	}
	pre := c.PreshowMin
	b := showtimeBody{TMDBID: c.TMDBID, AtMs: c.AtMs, PreshowMin: &pre, PlayOn: body.PlayOn}
	s.fillFilm(r.Context(), &b)
	if strings.TrimSpace(b.Title) == "" {
		writeError(w, http.StatusBadRequest, "Usherr couldn't look the film up. A TMDB key is needed (Settings, Info, lists and subtitles, Movie and show info).")
		return
	}
	if msg := b.check(); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if b.PlayOn == "tv" && !s.tvReady() {
		b.PlayOn = "here"
	}
	id, err := s.saveShowtime(r.Context(), requestUserID(r), b, pre)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The showtime couldn't be saved.")
		return
	}
	s.markJoined(r.Context(), id, encodeShowtimeCode(c)) // a ticket stub here too (home_community_guest.go)
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "title": b.Title, "atMs": c.AtMs})
}
