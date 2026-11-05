package api

// Usherr: poster badges (what Kometa's "overlays" did). Small badges drawn
// in the corners of the posters Jellyfin and Plex show: the picture size (4K,
// 1080p), HDR or Dolby Vision, the best sound (Atmos, DTS:X, TrueHD, DTS-HD
// MA) and its channels, the rating, and "New" for a title added lately.
//
// Off until switched on, and only in the libraries chosen. The badges are
// always drawn on the ORIGINAL poster, kept in the artwork folder
// (badges/originals) before the first upload, so drawing again never stacks
// badges, and switching them off (or taking a library out) puts the original
// back. A poster changed some other way (by hand in Jellyfin, by Re-Posterr)
// becomes the new original. A title is drawn again when what its badges say
// changes: a better file, a new rating, the look.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"log/slog"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"

	"github.com/rdborg/mediarium/internal/mediaservers"
)

const (
	keyBadges = "home.badges" // JSON: badgeSettings

	badgePosterW = 1000 // posters are drawn at most this wide
	badgeEvery   = time.Hour
)

// badgeMaxDraws is how many posters one pass sends at most; badgePause is
// the wait between two (tests make it 0).
var (
	badgeMaxDraws = 600
	badgePause    = 250 * time.Millisecond
	badgeMaxReads = 400 // Plex files whose tracks one pass reads
)

// ---- settings ----

type badgeSettings struct {
	On         bool     `json:"on"`
	Libraries  []string `json:"libraries"`
	Res        bool     `json:"res"`
	HDR        bool     `json:"hdr"`
	Audio      bool     `json:"audio"`
	Channels   bool     `json:"channels"`
	Rating     bool     `json:"rating"`
	RatingFrom string   `json:"ratingFrom"` // server (the media server's own, TMDB's by default) or imdb
	New        bool     `json:"new"`
	NewDays    int      `json:"newDays"`
	QualityAt  string   `json:"qualityAt"` // the corner of the picture and sound badges: top-left, top-right, bottom-left, bottom-right
	RatingAt   string   `json:"ratingAt"`  // the corner of the rating and New
	Look       string   `json:"look"`      // dark or color
	Size       string   `json:"size"`      // small, medium or large
}

func badgeDefaults() badgeSettings {
	return badgeSettings{Res: true, HDR: true, Audio: true, Rating: true, RatingFrom: "server", NewDays: 14, QualityAt: "top-left", RatingAt: "top-right", Look: "dark", Size: "medium", Libraries: []string{}}
}

var badgeCorners = map[string]bool{"top-left": true, "top-right": true, "bottom-left": true, "bottom-right": true}

// tidy puts every value in its range.
func (b badgeSettings) tidy() badgeSettings {
	d := badgeDefaults()
	if b.RatingFrom != "imdb" {
		b.RatingFrom = "server"
	}
	if b.NewDays < 1 || b.NewDays > 365 {
		b.NewDays = d.NewDays
	}
	if !badgeCorners[b.QualityAt] {
		b.QualityAt = d.QualityAt
	}
	if !badgeCorners[b.RatingAt] {
		b.RatingAt = d.RatingAt
	}
	if b.Look != "color" {
		b.Look = "dark"
	}
	if b.Size != "small" && b.Size != "large" {
		b.Size = "medium"
	}
	libs := []string{}
	seen := map[string]bool{}
	for _, l := range b.Libraries {
		l = strings.TrimSpace(l)
		if l != "" && !seen[l] && len(libs) < 100 {
			seen[l] = true
			libs = append(libs, trimRunes(l, 120))
		}
	}
	b.Libraries = libs
	return b
}

func (s *Server) badgeSettingsNow() badgeSettings {
	b := badgeDefaults()
	if raw, _ := s.Settings.Get(keyBadges); raw != "" {
		_ = json.Unmarshal([]byte(raw), &b)
	}
	return b.tidy()
}

func (b badgeSettings) inLibrary(lib string) bool {
	for _, l := range b.Libraries {
		if l == lib {
			return true
		}
	}
	return false
}

// ---- what a title's badges say ----

// badgeMedia is what a film's file holds, as its media server says.
type badgeMedia struct {
	Known  bool         `json:"known,omitempty"` // the tracks were read (not just Plex's listing)
	Width  int          `json:"w,omitempty"`
	Height int          `json:"h,omitempty"`
	Range  string       `json:"range,omitempty"` // dv, hdr10+, hdr, hlg or ""
	Audio  []badgeTrack `json:"audio,omitempty"`
}

type badgeTrack struct {
	Codec    string `json:"codec,omitempty"`
	Profile  string `json:"profile,omitempty"`
	Title    string `json:"title,omitempty"`
	Channels int    `json:"ch,omitempty"`
}

// posterBadge is one badge: its kind (res, hdr, audio, ch, rating, new) and
// its words.
type posterBadge struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// badgeRes is the picture size's badge: 4K, 1080p or 720p ("" smaller or
// unknown). A wide film is less tall than its size says, so the width
// counts.
func badgeRes(m badgeMedia) string {
	w, h := m.Width, m.Height
	switch {
	case w >= 3200 || h >= 2000:
		return "4K"
	case w >= 1800 || h >= 1000:
		return "1080p"
	case w >= 1200 || h >= 700:
		return "720p"
	}
	return ""
}

func badgeRange(r string) string {
	switch r {
	case "dv":
		return "Dolby Vision"
	case "hdr10+":
		return "HDR10+"
	case "hdr":
		return "HDR"
	case "hlg":
		return "HLG"
	}
	return ""
}

// badgeSound is a track's sound badge and its rank (higher is better): Atmos,
// DTS:X, TrueHD, DTS-HD MA; "" for the rest.
func badgeSound(t badgeTrack) (string, int) {
	all := strings.ToLower(t.Codec + " " + t.Profile + " " + t.Title)
	codec, prof := strings.ToLower(t.Codec), strings.ToLower(t.Profile)
	switch {
	case strings.Contains(all, "atmos"):
		return "Atmos", 4
	case strings.Contains(all, "dts:x") || strings.Contains(all, "dts-x") || strings.Contains(all, "dtsx") || strings.Contains(all, "dts x"):
		return "DTS:X", 3
	case codec == "truehd" || strings.Contains(all, "truehd"):
		return "TrueHD", 2
	case strings.Contains(codec, "dts") && (prof == "ma" || strings.Contains(prof, "hd ma") || strings.Contains(all, "dts-hd ma") || strings.Contains(all, "master audio")):
		return "DTS-HD MA", 1
	}
	return "", 0
}

// badgeChannels is the most channels a track has as a badge: 7.1 or 5.1.
func badgeChannels(m badgeMedia) string {
	most := 0
	for _, t := range m.Audio {
		most = max(most, t.Channels)
	}
	switch {
	case most >= 8:
		return "7.1"
	case most >= 6:
		return "5.1"
	}
	return ""
}

// badgeList is what a copy's badges say, in the order they're drawn.
// rating 0 is no rating; label names its source ("" for the server's own).
func badgeList(b badgeSettings, it libItem, rating float64, label string, now time.Time) []posterBadge {
	var out []posterBadge
	if it.Kind == "movie" {
		m := it.Media
		if b.Res {
			if r := badgeRes(m); r != "" {
				out = append(out, posterBadge{"res", r})
			}
		}
		if b.HDR {
			if r := badgeRange(m.Range); r != "" {
				out = append(out, posterBadge{"hdr", r})
			}
		}
		if b.Audio {
			best, rank := "", 0
			for _, t := range m.Audio {
				if s, r := badgeSound(t); r > rank {
					best, rank = s, r
				}
			}
			if best != "" {
				out = append(out, posterBadge{"audio", best})
			}
		}
		if b.Channels {
			if c := badgeChannels(m); c != "" {
				out = append(out, posterBadge{"ch", c})
			}
		}
	}
	if b.Rating && rating > 0 {
		text := strconv.FormatFloat(math.Round(rating*10)/10, 'f', 1, 64)
		if label != "" {
			text = label + " " + text
		}
		out = append(out, posterBadge{"rating", text})
	}
	if b.New && !it.Added.IsZero() && now.Sub(it.Added) < time.Duration(b.NewDays)*24*time.Hour && now.Sub(it.Added) >= 0 {
		out = append(out, posterBadge{"new", "New"})
	}
	return out
}

// badgeKey is the fingerprint of a drawing: the badges and the look. The
// same key is the same picture, so it isn't sent again.
func badgeKey(list []posterBadge, b badgeSettings) string {
	if len(list) == 0 {
		return ""
	}
	raw, _ := json.Marshal(struct {
		L          []posterBadge
		Q, R, K, S string
	}{list, b.QualityAt, b.RatingAt, b.Look, b.Size})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:8])
}

// ---- drawing ----

var badgeHeights = map[string]float64{"small": 58, "medium": 72, "large": 88} // on a 1000-wide poster

// badgeColors are a badge's background and words in the look.
func badgeColors(look, kind string) (bg, fg color.NRGBA) {
	white := color.NRGBA{255, 255, 255, 255}
	if look != "color" {
		return color.NRGBA{12, 12, 14, 200}, white
	}
	switch kind {
	case "res":
		return color.NRGBA{245, 197, 24, 240}, color.NRGBA{20, 20, 20, 255}
	case "hdr":
		return color.NRGBA{109, 40, 217, 235}, white
	case "audio":
		return color.NRGBA{29, 78, 216, 235}, white
	case "ch":
		return color.NRGBA{51, 65, 85, 235}, white
	case "rating":
		return color.NRGBA{20, 20, 20, 225}, color.NRGBA{245, 197, 24, 255}
	case "new":
		return color.NRGBA{22, 163, 74, 240}, white
	}
	return color.NRGBA{12, 12, 14, 200}, white
}

// badgeDraw draws the badges on a copy of the original poster (made at most
// 1000 wide). The original is never changed.
func badgeDraw(orig image.Image, list []posterBadge, b badgeSettings) *image.RGBA {
	ob := orig.Bounds()
	w, h := ob.Dx(), ob.Dy()
	if w > badgePosterW {
		h = max(h*badgePosterW/w, 1)
		w = badgePosterW
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	if w == ob.Dx() {
		draw.Draw(img, img.Bounds(), orig, ob.Min, draw.Src)
	} else {
		draw.CatmullRom.Scale(img, img.Bounds(), orig, ob, draw.Src, nil)
	}
	if len(list) == 0 {
		return img
	}
	scale := float64(w) / 1000
	bh := int(math.Round(badgeHeights[b.Size] * scale))
	if bh < 10 {
		bh = 10
	}
	f, err := artFont("bebas")
	if err != nil {
		return img
	}
	face := artFace(f, float64(bh)*0.74)
	if face == nil {
		return img
	}
	groups := map[string][]posterBadge{}
	var corners []string
	for _, bd := range list {
		at := b.QualityAt
		if bd.Kind == "rating" || bd.Kind == "new" {
			at = b.RatingAt
		}
		if _, ok := groups[at]; !ok {
			corners = append(corners, at)
		}
		groups[at] = append(groups[at], bd)
	}
	margin := int(float64(bh) * 0.42)
	gap := int(float64(bh) * 0.22)
	for _, at := range corners {
		top := strings.HasPrefix(at, "top")
		left := strings.HasSuffix(at, "left")
		y := margin
		if !top {
			n := len(groups[at])
			y = h - margin - n*bh - (n-1)*gap
		}
		for _, bd := range groups[at] {
			badgeOne(img, face, bd, b.Look, left, y, bh, margin)
			y += bh + gap
		}
	}
	return img
}

// badgeOne draws one badge, a rounded pill, at row y against the side.
func badgeOne(img *image.RGBA, face font.Face, bd posterBadge, look string, left bool, y, bh, margin int) {
	text := strings.ToUpper(bd.Text)
	pad := int(float64(bh) * 0.36)
	star := 0
	if bd.Kind == "rating" && !strings.Contains(text, " ") {
		star = int(float64(bh) * 0.62) // the server's own rating: a star, then the number
	}
	tw := font.MeasureString(face, text).Ceil()
	bw := tw + 2*pad
	if star > 0 {
		bw += star + pad/2
	}
	w := img.Rect.Dx()
	x := margin
	if !left {
		x = w - margin - bw
	}
	bg, fg := badgeColors(look, bd.Kind)
	badgeRound(img, image.Rect(x, y, x+bw, y+bh), float64(bh)*0.26, bg)
	tx := x + pad
	if star > 0 {
		gold := color.NRGBA{245, 197, 24, 255}
		badgeStar(img, float64(tx)+float64(star)/2, float64(y)+float64(bh)/2, float64(star)/2, gold)
		tx += star + pad/2
	}
	// Bebas is all capitals: the words sit in the middle by the height of an H.
	hb, _ := font.BoundString(face, "H")
	capH := (hb.Max.Y - hb.Min.Y).Ceil()
	base := y + (bh+capH)/2
	d := &font.Drawer{Dst: img, Src: image.NewUniform(fg), Face: face, Dot: fixed.P(tx, base)}
	d.DrawString(text)
}

// badgeRound fills a rounded rectangle, smooth at the edges.
func badgeRound(img *image.RGBA, r image.Rectangle, rad float64, c color.NRGBA) {
	w, h := float32(r.Dx()), float32(r.Dy())
	if w <= 0 || h <= 0 {
		return
	}
	k := float32(math.Min(rad, float64(min(w, h))/2))
	z := vector.NewRasterizer(r.Dx(), r.Dy())
	z.MoveTo(k, 0)
	z.LineTo(w-k, 0)
	z.QuadTo(w, 0, w, k)
	z.LineTo(w, h-k)
	z.QuadTo(w, h, w-k, h)
	z.LineTo(k, h)
	z.QuadTo(0, h, 0, h-k)
	z.LineTo(0, k)
	z.QuadTo(0, 0, k, 0)
	z.ClosePath()
	z.DrawOp = draw.Over
	z.Draw(img, r, image.NewUniform(c), image.Point{})
}

// badgeStar fills a five-pointed star centred at cx, cy.
func badgeStar(img *image.RGBA, cx, cy, rad float64, c color.NRGBA) {
	size := int(math.Ceil(rad*2)) + 2
	ox, oy := int(cx-rad)-1, int(cy-rad)-1
	z := vector.NewRasterizer(size, size)
	for i := 0; i < 10; i++ {
		rr := rad
		if i%2 == 1 {
			rr = rad * 0.45
		}
		a := -math.Pi/2 + float64(i)*math.Pi/5
		px, py := float32(cx-float64(ox)+rr*math.Cos(a)), float32(cy-float64(oy)+rr*math.Sin(a))
		if i == 0 {
			z.MoveTo(px, py)
		} else {
			z.LineTo(px, py)
		}
	}
	z.ClosePath()
	z.DrawOp = draw.Over
	z.Draw(img, image.Rect(ox, oy, ox+size, oy+size), image.NewUniform(c), image.Point{})
}

// ---- the originals and what was drawn ----

// badgeRow is what Usherr knows about one copy's poster.
type badgeRow struct {
	Copy  string
	Title string
	Base  string // the original's file name in badges/originals ("" none kept)
	Drawn string // the key of the badges on the server now ("" the original is there)
	Tag   string // the server's poster tag after Usherr's upload ("" not seen yet)
	Print string // Plex: the file the tracks were read from
	Media string // Plex: the tracks read (badgeMedia as JSON)
	Error string
}

func (s *Server) badgeDir() string { return filepath.Join(s.artDir(), "badges", "originals") }

func (s *Server) badgeRows(ctx context.Context) (map[string]*badgeRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT copy_id, title, base, drawn, tag, file_print, media, error FROM badge_posters`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*badgeRow{}
	for rows.Next() {
		var r badgeRow
		if rows.Scan(&r.Copy, &r.Title, &r.Base, &r.Drawn, &r.Tag, &r.Print, &r.Media, &r.Error) == nil {
			out[r.Copy] = &r
		}
	}
	return out, rows.Err()
}

func (s *Server) badgeSave(ctx context.Context, r *badgeRow) {
	_, err := s.db.ExecContext(ctx, `INSERT INTO badge_posters (copy_id, title, base, drawn, tag, file_print, media, error, updated_ms) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (copy_id) DO UPDATE SET title = excluded.title, base = excluded.base, drawn = excluded.drawn, tag = excluded.tag, file_print = excluded.file_print, media = excluded.media, error = excluded.error, updated_ms = excluded.updated_ms`,
		r.Copy, r.Title, r.Base, r.Drawn, r.Tag, r.Print, r.Media, r.Error, time.Now().UnixMilli())
	if err != nil {
		slog.Warn("poster badges: couldn't save", "err", err)
	}
}

func (s *Server) badgeForget(ctx context.Context, r *badgeRow) {
	if r.Base != "" {
		_ = os.Remove(filepath.Join(s.badgeDir(), filepath.Base(r.Base)))
	}
	_, _ = s.db.ExecContext(ctx, `DELETE FROM badge_posters WHERE copy_id = ?`, r.Copy)
}

// badgeKeepOriginal saves the poster the server has now as the copy's
// original.
func (s *Server) badgeKeepOriginal(ctx context.Context, srv mediaservers.Server, r *badgeRow) ([]byte, error) {
	data, _, err := s.artGetImage(ctx, srv, r.Copy, "poster", 0)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, errNoPoster
	}
	ext := ".jpg"
	if strings.Contains(http.DetectContentType(data), "png") {
		ext = ".png"
	}
	if err := os.MkdirAll(s.badgeDir(), 0o755); err != nil {
		return nil, fmt.Errorf("the folder for original posters couldn't be made: %w", err)
	}
	name := artSafe(r.Copy) + ext
	if r.Base != "" && r.Base != name {
		_ = os.Remove(filepath.Join(s.badgeDir(), filepath.Base(r.Base)))
	}
	if err := os.WriteFile(filepath.Join(s.badgeDir(), name), data, 0o644); err != nil {
		return nil, fmt.Errorf("the original poster couldn't be kept: %w", err)
	}
	r.Base = name
	return data, nil
}

var errNoPoster = errors.New("the title has no poster to put badges on")

func (s *Server) badgeOriginal(r *badgeRow) ([]byte, bool) {
	if r == nil || r.Base == "" {
		return nil, false
	}
	data, err := os.ReadFile(filepath.Join(s.badgeDir(), filepath.Base(r.Base)))
	return data, err == nil && len(data) > 0
}

// badgeUpload puts a poster on a copy without telling the badges about it
// (artSetImage would: it's for pictures that come from elsewhere).
func (s *Server) badgeUpload(ctx context.Context, srv mediaservers.Server, id string, data []byte) error {
	mime := http.DetectContentType(data)
	if !strings.HasPrefix(mime, "image/") {
		mime = "image/jpeg"
	}
	if ref, ok := parseArtPlex(id); ok {
		ps, ok := s.artPlexServer(ref.server)
		if !ok {
			return errors.New("the Plex server for this title isn't connected")
		}
		return s.mediaClient.PlexSetImage(ctx, ps, ref.key, ref.section, ref.kind, "poster", data, mime)
	}
	return jfSetImage(ctx, srv, id, "poster", data, mime)
}

// badgeTookPoster: Re-Posterr (or an undo) just put a poster on the copy.
// It is the new original: the next pass keeps it and draws on it.
func (s *Server) badgeTookPoster(id string) {
	_, _ = s.db.Exec(`UPDATE badge_posters SET drawn = '', tag = '' WHERE copy_id = ? AND drawn != ''`, id)
}

// badgeOriginalOf is the copy's original poster when the server shows it
// with badges, for Re-Posterr to keep instead of the badged one.
func (s *Server) badgeOriginalOf(id string) ([]byte, string, bool) {
	var r badgeRow
	if s.db.QueryRow(`SELECT copy_id, base, drawn FROM badge_posters WHERE copy_id = ?`, id).Scan(&r.Copy, &r.Base, &r.Drawn) != nil || r.Drawn == "" {
		return nil, "", false
	}
	data, ok := s.badgeOriginal(&r)
	if !ok {
		return nil, "", false
	}
	return data, http.DetectContentType(data), true
}

// ---- the pass ----

type badgeStatus struct {
	Running  bool   `json:"running"`
	LastMs   int64  `json:"lastMs,omitempty"`
	Drawn    int    `json:"drawn"`
	Restored int    `json:"restored"`
	Badged   int    `json:"badged"` // posters with badges on now
	Failed   int    `json:"failed"`
	Note     string `json:"note,omitempty"`
}

var badgeState struct {
	mu   sync.Mutex
	busy sync.Mutex
	st   badgeStatus
}

func badgeSetStatus(f func(*badgeStatus)) {
	badgeState.mu.Lock()
	f(&badgeState.st)
	badgeState.mu.Unlock()
}

func badgeStatusNow() badgeStatus {
	badgeState.mu.Lock()
	defer badgeState.mu.Unlock()
	return badgeState.st
}

// badgesJob is the hourly pass: it does nothing while badges are off and
// none are left to take off.
func (s *Server) badgesJob(ctx context.Context) {
	if !s.badgeSettingsNow().On {
		var n int
		if s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM badge_posters WHERE drawn != ''`).Scan(&n) != nil || n == 0 {
			return
		}
	}
	s.badgePass(ctx)
}

// badgeRatings are the ratings the badges show, by copy, and the label in
// front of them ("" for the server's own, drawn with a star).
func (s *Server) badgeRatings(ctx context.Context, b badgeSettings, items []libItem) (map[string]float64, string) {
	out := map[string]float64{}
	if b.RatingFrom != "imdb" {
		for _, it := range items {
			out[it.Copy] = it.Rating
		}
		return out, ""
	}
	byKind := map[string][]int{}
	for _, it := range items {
		if it.TMDBID > 0 {
			byKind[it.Kind] = append(byKind[it.Kind], it.TMDBID)
		}
	}
	got := map[string]map[int]titleScores{}
	for kind, ids := range byKind {
		got[kind] = map[int]titleScores{}
		for i := 0; i < len(ids); i += 500 {
			have, _ := s.cachedScores(ctx, kind, ids[i:min(i+500, len(ids))])
			for id, sc := range have {
				got[kind][id] = sc
			}
		}
	}
	for _, it := range items {
		out[it.Copy] = got[it.Kind][it.TMDBID].IMDb
	}
	return out, "IMDb"
}

// badgePass walks every copy: it draws what's due, and puts the original
// back where badges are no longer wanted.
func (s *Server) badgePass(ctx context.Context) {
	if !badgeState.busy.TryLock() {
		return
	}
	defer badgeState.busy.Unlock()
	badgeSetStatus(func(st *badgeStatus) { st.Running, st.Drawn, st.Restored, st.Failed, st.Note = true, 0, 0, 0, "" })
	defer badgeSetStatus(func(st *badgeStatus) { st.Running, st.LastMs = false, time.Now().UnixMilli() })

	b := s.badgeSettingsNow()
	srv, _ := s.jellyfinServer()
	items, err := s.libItems(ctx, true)
	if err != nil {
		badgeSetStatus(func(st *badgeStatus) { st.Note = "The media servers couldn't be read: " + err.Error() })
		return
	}
	rows, err := s.badgeRows(ctx)
	if err != nil {
		badgeSetStatus(func(st *badgeStatus) { st.Note = err.Error() })
		return
	}
	ratings, label := s.badgeRatings(ctx, b, items)
	now := time.Now()
	draws, reads := 0, 0
	for _, it := range items {
		if ctx.Err() != nil {
			return
		}
		r := rows[it.Copy]
		if !b.On || !b.inLibrary(it.Library) {
			if r != nil {
				if r.Drawn != "" {
					if err := s.badgeRestore(ctx, srv, r); err != nil {
						badgeSetStatus(func(st *badgeStatus) { st.Failed++ })
						continue
					}
					badgeSetStatus(func(st *badgeStatus) { st.Restored++ })
				}
				s.badgeForget(ctx, r)
			}
			continue
		}
		if r == nil {
			r = &badgeRow{Copy: it.Copy, Title: it.Title}
		}
		r.Title = it.Title
		changed := false
		// The server shows Usherr's drawing: a new tag means someone put
		// another poster on since; the first tag seen after an upload is ours.
		if r.Drawn != "" {
			switch {
			case r.Tag == "":
				r.Tag, changed = it.Poster, true
			case it.Poster != r.Tag:
				r.Drawn, r.Tag, changed = "", "", true
			}
		}
		// Plex's listing doesn't say if a film is HDR or Atmos: its tracks
		// are read once per file.
		if it.Server == "plex" && it.Kind == "movie" {
			if r.Print != it.FilePrint && it.FilePrint != "" && reads < badgeMaxReads {
				reads++
				if m, ok := s.badgePlexTracks(ctx, it); ok {
					raw, _ := json.Marshal(m)
					r.Media, r.Print, changed = string(raw), it.FilePrint, true
				}
			}
			if r.Print == it.FilePrint && r.Media != "" {
				var m badgeMedia
				if json.Unmarshal([]byte(r.Media), &m) == nil {
					it.Media = m
				}
			}
		}
		list := badgeList(b, it, ratings[it.Copy], label, now)
		key := badgeKey(list, b)
		if key == r.Drawn || draws >= badgeMaxDraws {
			if changed {
				s.badgeSave(ctx, r)
			}
			continue
		}
		draws++
		if err := s.badgePut(ctx, srv, r, list, key, b); err != nil {
			r.Error = err.Error()
			if !errors.Is(err, errNoPoster) {
				badgeSetStatus(func(st *badgeStatus) { st.Failed++ })
				slog.Info("poster badges: a poster couldn't be drawn", "title", it.Title, "err", err)
			}
		} else {
			r.Error = ""
			if key == "" {
				badgeSetStatus(func(st *badgeStatus) { st.Restored++ })
			} else {
				badgeSetStatus(func(st *badgeStatus) { st.Drawn++ })
			}
		}
		s.badgeSave(ctx, r)
		if badgePause > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(badgePause):
			}
		}
	}
	var n int
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM badge_posters WHERE drawn != ''`).Scan(&n)
	badgeSetStatus(func(st *badgeStatus) {
		st.Badged = n
		if draws >= badgeMaxDraws {
			st.Note = "More posters are left; the next pass carries on."
		}
	})
}

func (s *Server) badgePlexTracks(ctx context.Context, it libItem) (badgeMedia, bool) {
	ps, ok := s.artPlexServer(it.ServerID)
	if !ok {
		return badgeMedia{}, false
	}
	v, audio, err := s.mediaClient.PlexTracks(ctx, ps, it.ID)
	if err != nil {
		return badgeMedia{}, false
	}
	m := badgeMedia{Known: true, Width: v.Width, Height: v.Height, Range: plexRange(v)}
	if m.Width == 0 {
		m.Width, m.Height = it.Media.Width, it.Media.Height
	}
	for _, a := range audio {
		m.Audio = append(m.Audio, badgeTrack{Codec: a.Codec, Profile: a.Profile, Title: a.Title, Channels: a.Channels})
	}
	return m, true
}

// badgePut draws the badges on the copy's original and sends it; with no
// badges it sends the original back.
func (s *Server) badgePut(ctx context.Context, srv mediaservers.Server, r *badgeRow, list []posterBadge, key string, b badgeSettings) error {
	var orig []byte
	if r.Drawn != "" {
		// The server shows a drawing: the kept original is the one.
		var ok bool
		if orig, ok = s.badgeOriginal(r); !ok {
			return errors.New("the original poster isn't in the artwork folder any more")
		}
	} else {
		if key == "" {
			return nil // the original is there already
		}
		var err error
		if orig, err = s.badgeKeepOriginal(ctx, srv, r); err != nil {
			return err
		}
	}
	if key == "" {
		if err := s.badgeUpload(ctx, srv, r.Copy, orig); err != nil {
			return err
		}
		r.Drawn, r.Tag = "", ""
		return nil
	}
	img, _, err := image.Decode(bytes.NewReader(orig))
	if err != nil {
		return fmt.Errorf("the original poster couldn't be read: %w", err)
	}
	out, err := artJPEG(badgeDraw(img, list, b), 90)
	if err != nil {
		return err
	}
	if err := s.badgeUpload(ctx, srv, r.Copy, out); err != nil {
		return err
	}
	r.Drawn, r.Tag = key, ""
	return nil
}

// badgeRestore puts the original back.
func (s *Server) badgeRestore(ctx context.Context, srv mediaservers.Server, r *badgeRow) error {
	orig, ok := s.badgeOriginal(r)
	if !ok {
		return nil // nothing kept to put back
	}
	if err := s.badgeUpload(ctx, srv, r.Copy, orig); err != nil {
		return err
	}
	r.Drawn, r.Tag = "", ""
	return nil
}

func (s *Server) badgesSoon() {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Hour)
		defer cancel()
		s.badgePass(ctx)
	}()
}

// ---- the page ----

type badgeSample struct {
	Copy    string `json:"copy"`
	Title   string `json:"title"`
	Year    int    `json:"year,omitempty"`
	Library string `json:"library"`
}

// GET /api/home/badges (admin): the settings, the libraries to choose from,
// titles for the preview and how the last pass went.
func (s *Server) handleBadges(w http.ResponseWriter, r *http.Request) {
	b := s.badgeSettingsNow()
	libs := []string{}
	samples := []badgeSample{}
	items, err := s.libItems(r.Context(), false)
	note := ""
	if err != nil {
		note = err.Error()
	}
	seen := map[string]bool{}
	type scored struct {
		badgeSample
		n int
	}
	var cands []scored
	all := badgeDefaults()
	all.Channels, all.New = true, true
	for _, it := range items {
		if !seen[it.Library] {
			seen[it.Library] = true
			libs = append(libs, it.Library)
		}
		if it.Kind == "movie" && it.Poster != "" {
			cands = append(cands, scored{badgeSample{it.Copy, it.Title, it.Year, it.Library}, len(badgeList(all, it, it.Rating, "", time.Now()))})
		}
	}
	sort.Strings(libs)
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].n > cands[j].n })
	for i := 0; i < len(cands) && i < 40; i++ {
		samples = append(samples, cands[i].badgeSample)
	}
	sort.SliceStable(samples, func(i, j int) bool { return strings.ToLower(samples[i].Title) < strings.ToLower(samples[j].Title) })
	_, jf := s.jellyfinServer()
	st := badgeStatusNow()
	_ = s.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM badge_posters WHERE drawn != ''`).Scan(&st.Badged)
	writeJSON(w, http.StatusOK, map[string]any{"settings": b, "libraries": libs, "samples": samples, "status": st, "servers": jf || len(s.plexServers()) > 0, "note": note})
}

// PUT /api/home/badges (admin): save, and draw (or take off) right away.
func (s *Server) handlePutBadges(w http.ResponseWriter, r *http.Request) {
	var b badgeSettings
	if err := decodeJSON(r, &b); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that. Reload the page and try again.")
		return
	}
	b = b.tidy()
	raw, _ := json.Marshal(b)
	if err := s.Settings.Set(keyBadges, string(raw), false); err != nil {
		writeError(w, http.StatusInternalServerError, "The badges couldn't be saved.")
		return
	}
	s.badgesSoon()
	writeJSON(w, http.StatusOK, b)
}

// POST /api/home/badges/apply (admin): a pass now.
func (s *Server) handleBadgesApply(w http.ResponseWriter, r *http.Request) {
	if badgeStatusNow().Running {
		writeJSON(w, http.StatusOK, map[string]any{"started": false})
		return
	}
	s.badgesSoon()
	writeJSON(w, http.StatusOK, map[string]any{"started": true})
}

// POST /api/home/badges/preview (admin): one title's poster with the badges
// as the settings sent would draw them (a JPEG). Drawn on its original.
func (s *Server) handleBadgesPreview(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Settings badgeSettings `json:"settings"`
		Copy     string        `json:"copy"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that. Reload the page and try again.")
		return
	}
	b := body.Settings.tidy()
	items, err := s.libItems(r.Context(), false)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	var it *libItem
	for i := range items {
		if items[i].Copy == body.Copy {
			it = &items[i]
			break
		}
	}
	if it == nil {
		writeError(w, http.StatusNotFound, "That title isn't on your media servers any more.")
		return
	}
	srv, _ := s.jellyfinServer()
	ctx := r.Context()
	rows, _ := s.badgeRows(ctx)
	row := rows[it.Copy]
	var orig []byte
	if row != nil && row.Drawn != "" {
		orig, _ = s.badgeOriginal(row)
	}
	if orig == nil {
		if orig, _, err = s.artGetImage(ctx, srv, it.Copy, "poster", 0); err != nil || len(orig) == 0 {
			writeError(w, http.StatusBadGateway, "The title's poster couldn't be read.")
			return
		}
	}
	cp := *it
	if cp.Server == "plex" && cp.Kind == "movie" {
		if row != nil && row.Print == cp.FilePrint && row.Media != "" {
			_ = json.Unmarshal([]byte(row.Media), &cp.Media)
		} else if m, ok := s.badgePlexTracks(ctx, cp); ok {
			cp.Media = m
		}
	}
	ratings, label := s.badgeRatings(ctx, b, []libItem{cp})
	list := badgeList(b, cp, ratings[cp.Copy], label, time.Now())
	img, _, err := image.Decode(bytes.NewReader(orig))
	if err != nil {
		writeError(w, http.StatusBadGateway, "The title's poster couldn't be read.")
		return
	}
	out, err := artJPEG(badgeDraw(img, list, b), 85)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The preview couldn't be made.")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJPEG(w, out)
}
