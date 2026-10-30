package api

// Usherr: Re-Posterr, artwork picking (M4, after Posterizarr). For each film, show and
// collection Jellyfin has, Usherr picks the poster, background and logo from
// TMDB and fanart.tv (your language first, textless backgrounds, the best
// liked, big enough) and puts them on the Jellyfin items: the library's and
// the streamed copy alike. Only pictures change, never a file.
//
// Nothing changes without the household's say: in "ask" mode (the default)
// the picks wait on the Re-Posterr page (Settings) to be approved; "fill" puts in only
// what's missing; "auto" also replaces pictures that are too small. A
// picture replaced is kept first (in the artwork folder), so every change can
// be put back, and a picture chosen by hand is never replaced by a pass.
//
// The pass runs at night, a title every few seconds, and stops when someone
// starts playing on Jellyfin or its metadata disk runs low.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/image/draw"

	"github.com/rdborg/mediarium/internal/mediaservers"
	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/internal/netguard"
)

const (
	keyArtOn       = "home.art.on"        // "0": no nightly pass (on by default)
	keyArtMode     = "home.art.mode"      // ask (default), fill or auto
	keyArtFanart   = "home.art.fanartKey" // encrypted; a fanart.tv key
	keyArtFanartAs = "home.art.fanartAs"  // how fanart.tv takes the key: "api" or "both"
	keyArtTextless = "home.art.textless"  // "0": backgrounds with words are as good (textless first by default)
	keyArtFrom     = "home.art.from"      // the night's first hour (1)
	keyArtTo       = "home.art.to"        // the hour it stops (7)
	keyArtMinFree  = "home.art.minFreeGB" // stop when Jellyfin's metadata disk has less (20)
	keyArtFolder   = "home.art.folder"    // where replaced pictures are kept

	artPause      = 3 * time.Second // between titles in a pass
	artPerRun     = 900             // titles looked at in one hour's run at most
	artIndexFresh = 30 * time.Minute

	// Sizes sent to Jellyfin: as sharp as a TV tile wants, no bigger.
	artPosterW   = 1000
	artBackdropW = 1920
	artLogoW     = 800
	// Smaller than this is "too small" (replaced in auto mode, proposed in ask).
	artSmallPosterW   = 600
	artSmallBackdropW = 1280
)

var artTypes = []string{"poster", "backdrop", "logo"}

// jfImageType is Jellyfin's name for each kind of picture.
var jfImageType = map[string]string{"poster": "Primary", "backdrop": "Backdrop", "logo": "Logo"}

type artSettings struct {
	On        bool   `json:"on"`
	Mode      string `json:"mode"`
	FanartSet bool   `json:"fanartKeySet"`
	Textless  bool   `json:"textless"`
	From      int    `json:"from"`
	To        int    `json:"to"`
	MinFreeGB int    `json:"minFreeGB"`
	Folder    string `json:"folder"`
	// TheTVDB as a source too; the full Posterizarr app (when it owns the
	// posters, Re-Posterr's style stands aside).
	TVDBSet     bool   `json:"tvdbKeySet"`
	Posterizarr string `json:"posterizarr"`
	PzOwns      bool   `json:"posterizarrOwns"`
}

func (s *Server) artSettingsNow() artSettings {
	get := func(k string) string { v, _ := s.Settings.Get(k); return strings.TrimSpace(v) }
	num := func(k string, def, lo, hi int) int {
		n, err := strconv.Atoi(get(k))
		if err != nil || n < lo || n > hi {
			return def
		}
		return n
	}
	mode := get(keyArtMode)
	if mode != "fill" && mode != "auto" {
		mode = "ask"
	}
	return artSettings{On: get(keyArtOn) != "0", Mode: mode, FanartSet: get(keyArtFanart) != "", Textless: get(keyArtTextless) != "0",
		From: num(keyArtFrom, 1, 0, 23), To: num(keyArtTo, 7, 0, 23), MinFreeGB: num(keyArtMinFree, 20, 1, 1000), Folder: get(keyArtFolder),
		TVDBSet: get(keyArtTVDB) != "", Posterizarr: get(keyArtPosterizarr), PzOwns: get(keyArtPzOwns) == "1"}
}

// artDir is where replaced pictures are kept: the setting, else
// /data/artwork when it's mapped, else the config folder.
func (s *Server) artDir() string {
	if v := s.artSettingsNow().Folder; v != "" {
		return v
	}
	if st, err := os.Stat("/data/artwork"); err == nil && st.IsDir() {
		return "/data/artwork"
	}
	return filepath.Join(s.cfg.ConfigDir, "artwork")
}

// inNight says whether the hour is in the pass's hours (from..to, past
// midnight too).
func (a artSettings) inNight(t time.Time) bool {
	h := t.Hour()
	if a.From == a.To {
		return true
	}
	if a.From < a.To {
		return h >= a.From && h < a.To
	}
	return h >= a.From || h < a.To
}

// ---- the candidates ----

type artCand struct {
	Type    string  `json:"type"`
	Source  string  `json:"source"` // tmdb or fanart
	URL     string  `json:"url"`
	Preview string  `json:"preview"`
	Width   int     `json:"width"`
	Height  int     `json:"height"`
	Lang    string  `json:"lang"` // "" for none (textless)
	Score   float64 `json:"score"`
}

// artCandidates gathers every poster, background and logo TMDB and
// fanart.tv have for the title, best first per type.
func (s *Server) artCandidates(ctx context.Context, kind string, tmdbID, tvdbID int) (map[string][]artCand, error) {
	out := map[string][]artCand{}
	var img *metadata.Images
	var err error
	if kind == "collection" {
		img, err = s.displayTMDB().CollectionImages(ctx, tmdbID)
	} else {
		img, err = s.displayTMDB().GetImages(ctx, kind, tmdbID)
	}
	if err != nil && !s.artSettingsNow().FanartSet {
		return nil, err
	}
	if img != nil {
		add := func(typ string, list []metadata.Image, previewSize string) {
			for _, im := range list {
				if !metadata.ValidImagePath(im.FilePath) || strings.HasSuffix(strings.ToLower(im.FilePath), ".svg") {
					continue
				}
				out[typ] = append(out[typ], artCand{Type: typ, Source: "tmdb", URL: metadata.OriginalImageURL(im.FilePath), Preview: metadata.ImageURL(previewSize, im.FilePath),
					Width: im.Width, Height: im.Height, Lang: strings.ToLower(im.Language), Score: im.VoteAverage + float64(min(im.VoteCount, 50))/25})
			}
		}
		add("poster", img.Posters, "w342")
		add("backdrop", img.Backdrops, "w780")
		add("logo", img.Logos, "w500")
	}
	// fanart.tv and TheTVDB get a few seconds each, and one that doesn't
	// answer is left out a while, so a title opens without them.
	fctx, fcancel := context.WithTimeout(ctx, artSideWait)
	defer fcancel()
	if artSideDown("fanart") {
		slog.Debug("artwork: fanart.tv left out a while (it didn't answer)")
	} else if fa, ferr := s.fanartImages(fctx, kind, tmdbID, tvdbID); ferr == nil {
		for typ, list := range fa {
			out[typ] = append(out[typ], list...)
		}
	} else if !errors.Is(ferr, errNoFanart) {
		slog.Info("artwork: fanart.tv", "kind", kind, "tmdb", tmdbID, "err", ferr)
		artSideFailed("fanart", fctx)
	}
	if kind == "tv" && !artSideDown("tvdb") { // Usherr: TheTVDB too, with a key
		tctx, tcancel := context.WithTimeout(ctx, artSideWait)
		defer tcancel()
		if tv, terr := s.tvdbImages(tctx, tvdbID); terr == nil {
			for typ, list := range tv {
				out[typ] = append(out[typ], list...)
			}
		} else if !errors.Is(terr, errNoTVDB) {
			slog.Info("artwork: TheTVDB", "tvdb", tvdbID, "err", terr)
			artSideFailed("tvdb", tctx)
		}
	}
	set := s.artSettingsNow()
	lang := baseLang(s.infoLanguage())
	for typ := range out {
		artRank(out[typ], typ, lang, set.Textless)
	}
	return out, nil
}

// artSideWait is how long fanart.tv or TheTVDB gets for a title.
const artSideWait = 8 * time.Second

var artSides sync.Map // "fanart"/"tvdb" -> time.Time: left out until then

// artSideFailed: a source that ran out of time is left out five minutes.
func artSideFailed(name string, ctx context.Context) {
	if ctx.Err() != nil {
		artSides.Store(name, time.Now().Add(5*time.Minute))
	}
}

func artSideDown(name string) bool {
	until, ok := artSides.Load(name)
	return ok && time.Now().Before(until.(time.Time))
}

func baseLang(tag string) string {
	if i := strings.IndexAny(tag, "-_"); i > 0 {
		tag = tag[:i]
	}
	if tag == "" {
		return "en"
	}
	return strings.ToLower(tag)
}

// artRank sorts a type's candidates best first: the right language (for a
// background, none at all when textless is wanted), big enough, then the
// best liked.
func artRank(list []artCand, typ, lang string, textless bool) {
	langRank := func(c artCand) int {
		l := c.Lang
		if typ == "backdrop" && textless {
			switch l {
			case "":
				return 0
			case lang:
				return 1
			case "en":
				return 2
			}
			return 3
		}
		switch l {
		case lang:
			return 0
		case "en":
			return 1
		case "":
			return 2
		}
		return 3
	}
	small := func(c artCand) bool {
		switch typ {
		case "poster":
			return c.Width > 0 && c.Width < artSmallPosterW
		case "backdrop":
			return c.Width > 0 && c.Width < artSmallBackdropW
		}
		return false
	}
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if ra, rb := langRank(a), langRank(b); ra != rb {
			return ra < rb
		}
		if sa, sb := small(a), small(b); sa != sb {
			return !sa
		}
		return a.Score > b.Score
	})
}

// ---- fanart.tv ----

var errNoFanart = errors.New("no fanart.tv key")

// fanartAnswers keeps fanart.tv's answers a while, so opening a title and
// then choosing a picture asks it once.
var fanartAnswers = newTTLBytes(300, 30*time.Minute)

// fanartBase is fanart.tv's API (tests point it at a fake).
var fanartBase = "https://webservice.fanart.tv/v3.2"

type fanartImage struct {
	URL    string `json:"url"`
	Lang   string `json:"lang"`
	Likes  string `json:"likes"`
	Width  string `json:"width"`
	Height string `json:"height"`
}

// fanartImages is fanart.tv's art for a film (by TMDB id) or a show (by
// TheTVDB id); none for a collection.
func (s *Server) fanartImages(ctx context.Context, kind string, tmdbID, tvdbID int) (map[string][]artCand, error) {
	key, _ := s.Settings.Get(keyArtFanart)
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, errNoFanart
	}
	var path string
	var fields map[string]string
	switch kind {
	case "movie":
		path = "/movies/" + strconv.Itoa(tmdbID)
		fields = map[string]string{"movieposter": "poster", "moviebackground": "backdrop", "hdmovielogo": "logo"}
	case "tv":
		if tvdbID <= 0 {
			return nil, errNoFanart
		}
		path = "/tv/" + strconv.Itoa(tvdbID)
		fields = map[string]string{"tvposter": "poster", "showbackground": "backdrop", "hdtvlogo": "logo"}
	default:
		return nil, errNoFanart
	}
	raw, cached := fanartAnswers.get(path)
	if !cached {
		var status int
		var err error
		raw, status, err = s.fanartGet(ctx, path, key)
		if err != nil {
			return nil, err
		}
		if status == http.StatusNotFound {
			raw = []byte(`{}`)
		} else if status != http.StatusOK {
			return nil, fmt.Errorf("fanart.tv answered %d", status)
		}
		fanartAnswers.put(path, raw)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("fanart.tv's answer couldn't be read: %w", err)
	}
	out := map[string][]artCand{}
	for field, typ := range fields {
		var list []fanartImage
		if json.Unmarshal(body[field], &list) != nil {
			continue
		}
		for _, f := range list {
			if !strings.HasPrefix(f.URL, "https://") {
				continue
			}
			lang := strings.ToLower(f.Lang)
			if lang == "00" {
				lang = ""
			}
			likes, _ := strconv.Atoi(f.Likes)
			w, _ := strconv.Atoi(f.Width)
			h, _ := strconv.Atoi(f.Height)
			out[typ] = append(out[typ], artCand{Type: typ, Source: "fanart", URL: f.URL, Preview: fanartPreview(f.URL), Width: w, Height: h, Lang: lang, Score: 5 + float64(min(likes, 50))/5})
		}
	}
	return out, nil
}

// fanartPreview is fanart.tv's small copy of a picture, fetched through
// Usherr (handleArtPreview): fanart.tv doesn't have one for every picture,
// and its pictures don't always show in a page from another site.
func fanartPreview(u string) string {
	return "/api/home/art/preview?u=" + url.QueryEscape(u)
}

// fanartGet asks fanart.tv, with the key as it takes it: as the API key, or
// (a personal key) as both the API and the client key.
func (s *Server) fanartGet(ctx context.Context, path, key string) ([]byte, int, error) {
	as, _ := s.Settings.Get(keyArtFanartAs)
	try := func(both bool) ([]byte, int, error) {
		q := url.Values{"api_key": {key}}
		if both {
			q.Set("client_key", key)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fanartBase+path+"?"+q.Encode(), nil)
		if err != nil {
			return nil, 0, err
		}
		resp, err := netguard.Client(20 * time.Second).Do(req)
		if err != nil {
			return nil, 0, fmt.Errorf("couldn't reach fanart.tv: %w", netguard.CleanError(err))
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		return b, resp.StatusCode, err
	}
	b, st, err := try(as == "both")
	if err == nil && (st == http.StatusUnauthorized || st == http.StatusForbidden) {
		other := as != "both"
		if b2, st2, err2 := try(other); err2 == nil && st2/100 == 2 || st2 == http.StatusNotFound {
			v := "api"
			if other {
				v = "both"
			}
			_ = s.Settings.Set(keyArtFanartAs, v, false)
			return b2, st2, nil
		}
	}
	return b, st, err
}

// ---- Jellyfin: the titles and their pictures ----

type artJFItem struct {
	ID          string
	Type        string // Movie, Series or BoxSet
	Name        string
	Year        int
	TMDBID      int
	TVDBID      int
	HasPoster   bool
	HasBackdrop bool
	HasLogo     bool
}

func (it artJFItem) kind() string {
	switch it.Type {
	case "Series":
		return "tv"
	case "BoxSet":
		return "collection"
	}
	return "movie"
}

// artIndex is every film, show and collection Jellyfin has, kept a while.
var artIndex struct {
	mu    sync.Mutex
	at    time.Time
	items []artJFItem
}

func (s *Server) artJFItems(ctx context.Context, srv mediaservers.Server, fresh bool) ([]artJFItem, error) {
	artIndex.mu.Lock()
	defer artIndex.mu.Unlock()
	if !fresh && time.Since(artIndex.at) < artIndexFresh && artIndex.items != nil {
		return artIndex.items, nil
	}
	var all []artJFItem
	for start := 0; ; start += 1000 {
		var page struct {
			Items []struct {
				ID                string `json:"Id"`
				Type              string
				Name              string
				ProductionYear    int
				ProviderIds       map[string]string
				ImageTags         map[string]string
				BackdropImageTags []string
			}
			TotalRecordCount int
		}
		q := fmt.Sprintf("/Items?IncludeItemTypes=Movie,Series,BoxSet&Recursive=true&Fields=ProviderIds&EnableImageTypes=Primary,Backdrop,Logo&ImageTypeLimit=1&StartIndex=%d&Limit=1000", start)
		if err := jfJSON(ctx, srv, http.MethodGet, q, nil, &page); err != nil {
			return nil, err
		}
		for _, it := range page.Items {
			tm, _ := strconv.Atoi(it.ProviderIds["Tmdb"])
			tv, _ := strconv.Atoi(it.ProviderIds["Tvdb"])
			_, p := it.ImageTags["Primary"]
			_, l := it.ImageTags["Logo"]
			all = append(all, artJFItem{ID: it.ID, Type: it.Type, Name: it.Name, Year: it.ProductionYear, TMDBID: tm, TVDBID: tv, HasPoster: p, HasBackdrop: len(it.BackdropImageTags) > 0, HasLogo: l})
		}
		if len(page.Items) < 1000 || start+1000 >= page.TotalRecordCount {
			break
		}
	}
	artIndex.at, artIndex.items = time.Now(), all
	return all, nil
}

// artSync writes Jellyfin's titles into art_titles: one row per film, show
// or collection (its copies together), with what they lack.
func (s *Server) artSync(ctx context.Context, items []artJFItem) error {
	type agg struct {
		it      artJFItem
		ids     []string
		missing map[string]bool
	}
	by := map[string]*agg{}
	var order []string
	for _, it := range items {
		if it.TMDBID <= 0 {
			continue
		}
		k := it.kind() + ":" + strconv.Itoa(it.TMDBID)
		a := by[k]
		if a == nil {
			a = &agg{it: it, missing: map[string]bool{}}
			by[k] = a
			order = append(order, k)
		}
		a.ids = append(a.ids, it.ID)
		if a.it.TVDBID == 0 {
			a.it.TVDBID = it.TVDBID
		}
		if !it.HasPoster {
			a.missing["poster"] = true
		}
		if !it.HasBackdrop {
			a.missing["backdrop"] = true
		}
		if !it.HasLogo {
			a.missing["logo"] = true
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Titles Jellyfin no longer has keep their history, without items.
	if _, err := tx.ExecContext(ctx, `UPDATE art_titles SET jf_ids = ''`); err != nil {
		return err
	}
	for _, k := range order {
		a := by[k]
		var miss []string
		for _, t := range artTypes {
			if a.missing[t] {
				miss = append(miss, t)
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO art_titles (kind, tmdb_id, tvdb_id, title, year, jf_ids, missing) VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (kind, tmdb_id) DO UPDATE SET tvdb_id = excluded.tvdb_id, title = excluded.title, year = excluded.year, jf_ids = excluded.jf_ids, missing = excluded.missing`,
			a.it.kind(), a.it.TMDBID, a.it.TVDBID, a.it.Name, a.it.Year, strings.Join(a.ids, ","), strings.Join(miss, ",")); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// jfImageWidth is the width of an item's picture of the type (0: none or
// unknown).
func jfImageWidth(ctx context.Context, srv mediaservers.Server, itemID, typ string) int {
	var infos []struct {
		ImageType  string
		ImageIndex *int
		Width      int
	}
	if jfJSON(ctx, srv, http.MethodGet, "/Items/"+url.PathEscape(itemID)+"/Images", nil, &infos) != nil {
		return 0
	}
	for _, in := range infos {
		if in.ImageType == jfImageType[typ] && (in.ImageIndex == nil || *in.ImageIndex == 0) {
			return in.Width
		}
	}
	return 0
}

// jfFreeGB is how much room Jellyfin's metadata folder has (-1: unknown).
func jfFreeGB(ctx context.Context, srv mediaservers.Server) float64 {
	var st map[string]json.RawMessage
	if jfJSON(ctx, srv, http.MethodGet, "/System/Info/Storage", nil, &st) != nil {
		return -1
	}
	for _, k := range []string{"InternalMetadataFolder", "ProgramDataFolder"} {
		var f struct{ FreeSpace int64 }
		if json.Unmarshal(st[k], &f) == nil && f.FreeSpace > 0 {
			return float64(f.FreeSpace) / (1 << 30)
		}
	}
	return -1
}

// ---- putting a picture on ----

// artFetch downloads a picture (TMDB's or fanart.tv's) and makes it the size
// Jellyfin gets: a JPEG for posters and backgrounds, a PNG for logos.
func artFetch(ctx context.Context, u, typ string) ([]byte, string, error) {
	if !artHostOK(u) {
		return nil, "", errors.New("that picture isn't from TMDB, fanart.tv or TheTVDB")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := artHTTP().Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("download the picture: %w", netguard.CleanError(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("download the picture: status %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxMetaImage+1))
	if err != nil || len(raw) > maxMetaImage {
		return nil, "", errors.New("the picture couldn't be downloaded whole")
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, "", fmt.Errorf("the picture couldn't be read: %w", err)
	}
	maxW := map[string]int{"poster": artPosterW, "backdrop": artBackdropW, "logo": artLogoW}[typ]
	if b := img.Bounds(); b.Dx() > maxW {
		h := b.Dy() * maxW / b.Dx()
		dst := image.NewRGBA(image.Rect(0, 0, maxW, max(h, 1)))
		draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
		img = dst
	}
	var buf bytes.Buffer
	if typ == "logo" {
		if err := png.Encode(&buf, img); err != nil {
			return nil, "", err
		}
		return buf.Bytes(), "image/png", nil
	}
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 88}); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), "image/jpeg", nil
}

// artHTTP and artTestHost let tests serve pictures from a fake.
var artHTTP = func() *http.Client { return netguard.Client(40 * time.Second) }
var artTestHost = func(string) bool { return false }

// jfSetImage puts the picture on the item as its first of the type
// (replacing it). Jellyfin takes it base64-encoded.
func jfSetImage(ctx context.Context, srv mediaservers.Server, itemID, typ string, data []byte, mime string) error {
	body := base64.StdEncoding.EncodeToString(data)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(srv.BaseURL, "/")+"/Items/"+url.PathEscape(itemID)+"/Images/"+jfImageType[typ]+"/0", strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", mime)
	// As jfRequest: Jellyfin 12 wants the token in Authorization for an upload.
	req.Header.Set("X-Emby-Token", srv.Token)
	req.Header.Set("Authorization", fmt.Sprintf(`MediaBrowser Token=%q, Client="Mediarium", Device="Mediarium player", DeviceId="mediarium-player", Version="1"`, srv.Token))
	resp, err := jfHTTP.Do(req)
	if err != nil {
		return fmt.Errorf("couldn't reach %s: %w", serverLabel(srv), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%s answered %d to the picture", serverLabel(srv), resp.StatusCode)
	}
	return nil
}

// jfGetImage is the item's picture of the type as Jellyfin has it (nil: none).
func jfGetImage(ctx context.Context, srv mediaservers.Server, itemID, typ string, maxWidth int) ([]byte, string, error) {
	q := ""
	if maxWidth > 0 {
		q = "?maxWidth=" + strconv.Itoa(maxWidth)
	}
	resp, err := jfRequest(ctx, srv, http.MethodGet, "/Items/"+url.PathEscape(itemID)+"/Images/"+jfImageType[typ]+"/0"+q, nil)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, "", nil
	}
	if resp.StatusCode/100 != 2 {
		return nil, "", fmt.Errorf("%s answered %d", serverLabel(srv), resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxMetaImage))
	return b, resp.Header.Get("Content-Type"), err
}

type artChange struct {
	ID      int64  `json:"id"`
	Kind    string `json:"kind"`
	TMDBID  int    `json:"tmdbId"`
	Title   string `json:"title"`
	Type    string `json:"type"`
	Source  string `json:"source"`
	URL     string `json:"url"`
	Preview string `json:"preview"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Lang    string `json:"lang"`
	Why     string `json:"why"`
	State   string `json:"state"`
	Error   string `json:"error,omitempty"`
	ByHand  bool   `json:"byHand"`
	// Style: made in the household's style (its fingerprint); "" for a
	// picture put on as it is.
	Style   string `json:"style,omitempty"`
	Words   string `json:"words,omitempty"` // the household's own words on it
	Created int64  `json:"createdMs"`
	Done    int64  `json:"doneMs,omitempty"`
	JFIDs   string `json:"-"`
	Backups string `json:"-"`
	// The Jellyfin item to show the current picture of (the first copy).
	Current string `json:"current,omitempty"`
}

const artChangeCols = `c.id, c.kind, c.tmdb_id, c.title, c.type, c.source, c.url, c.preview, c.width, c.height, c.lang, c.why, c.state, c.error, c.by_hand, c.style, c.words, c.created_ms, c.done_ms, COALESCE(t.jf_ids, ''), c.backups`

func (s *Server) artChanges(ctx context.Context, where string, args ...any) ([]artChange, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+artChangeCols+` FROM art_changes c LEFT JOIN art_titles t ON t.kind = c.kind AND t.tmdb_id = c.tmdb_id `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []artChange{}
	for rows.Next() {
		var c artChange
		var byHand int
		if err := rows.Scan(&c.ID, &c.Kind, &c.TMDBID, &c.Title, &c.Type, &c.Source, &c.URL, &c.Preview, &c.Width, &c.Height, &c.Lang, &c.Why, &c.State, &c.Error, &byHand, &c.Style, &c.Words, &c.Created, &c.Done, &c.JFIDs, &c.Backups); err != nil {
			continue
		}
		c.ByHand = byHand == 1
		if ids := strings.Split(c.JFIDs, ","); ids[0] != "" {
			c.Current = ids[0]
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// artApply makes the change on every Jellyfin item of the title, keeping
// each picture it replaces first.
func (s *Server) artApply(ctx context.Context, srv mediaservers.Server, c artChange) error {
	if c.Style != "" || c.Type == "seasons" || c.Type == "cards" {
		return s.artApplyStyled(ctx, srv, c)
	}
	ids := strings.Split(c.JFIDs, ",")
	if c.JFIDs == "" {
		return errors.New("Jellyfin doesn't have this title (yet)")
	}
	data, mime, err := artFetch(ctx, c.URL, c.Type)
	if err != nil {
		return err
	}
	backups := map[string]string{}
	_ = json.Unmarshal([]byte(c.Backups), &backups)
	for _, id := range ids {
		if err := s.artKeep(ctx, srv, id, c.Type, backups); err != nil {
			s.artSaveBackups(ctx, c.ID, backups)
			return err
		}
		if err := jfSetImage(ctx, srv, id, c.Type, data, mime); err != nil {
			s.artSaveBackups(ctx, c.ID, backups)
			return err
		}
	}
	s.artSaveBackups(ctx, c.ID, backups)
	if c.Type == "poster" {
		s.artSavePoster(c.ID, data) // Usherr's pages show it too
	}
	return nil
}

// artKeep keeps the item's picture of the type before it's replaced (once
// per change), noting it in backups ("" when there was none).
func (s *Server) artKeep(ctx context.Context, srv mediaservers.Server, id, typ string, backups map[string]string) error {
	if _, kept := backups[id]; kept {
		return nil
	}
	old, oldMime, err := jfGetImage(ctx, srv, id, typ, 0)
	if err != nil {
		return nil // as before: Jellyfin didn't say; the picture still goes on
	}
	if len(old) == 0 {
		backups[id] = "" // there was none
		return nil
	}
	ext := ".jpg"
	if strings.Contains(oldMime, "png") {
		ext = ".png"
	}
	dir := filepath.Join(s.artDir(), "replaced")
	name := fmt.Sprintf("%s-%s-%d%s", artSafe(id), typ, time.Now().UnixMilli(), ext)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("the folder for replaced pictures couldn't be made: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), old, 0o644); err != nil {
		return fmt.Errorf("the picture it replaces couldn't be kept: %w", err)
	}
	backups[id] = name
	return nil
}

func (s *Server) artSaveBackups(ctx context.Context, id int64, b map[string]string) {
	raw, _ := json.Marshal(b)
	_, _ = s.db.ExecContext(ctx, `UPDATE art_changes SET backups = ? WHERE id = ?`, string(raw), id)
}

func artSafe(id string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' {
			return r
		}
		return '_'
	}, id)
}

// artUndo puts back what the change replaced (or takes the picture off when
// there was none).
func (s *Server) artUndo(ctx context.Context, srv mediaservers.Server, c artChange) error {
	backups := map[string]string{}
	_ = json.Unmarshal([]byte(c.Backups), &backups)
	typ := c.Type
	if typ == "seasons" || typ == "cards" {
		typ = "poster" // a season's or an episode's own picture
	}
	for id, name := range backups {
		if strings.HasPrefix(id, "+") {
			continue // a note that the picture was made
		}
		if name == "" {
			resp, err := jfRequest(ctx, srv, http.MethodDelete, "/Items/"+url.PathEscape(id)+"/Images/"+jfImageType[typ]+"/0", nil)
			if err != nil {
				return err
			}
			resp.Body.Close()
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.artDir(), "replaced", filepath.Base(name)))
		if err != nil {
			return fmt.Errorf("the picture it replaced isn't there any more: %w", err)
		}
		mime := "image/jpeg"
		if strings.HasSuffix(name, ".png") {
			mime = "image/png"
		}
		if err := jfSetImage(ctx, srv, id, typ, data, mime); err != nil {
			return err
		}
	}
	return nil
}

// ---- the pass ----

type artStatus struct {
	Running  bool    `json:"running"`
	LastMs   int64   `json:"lastMs,omitempty"`
	Looked   int     `json:"looked"`
	Proposed int     `json:"proposed"`
	Applied  int     `json:"applied"`
	Note     string  `json:"note,omitempty"`
	FreeGB   float64 `json:"freeGB"`
}

var artState struct {
	mu   sync.Mutex
	busy sync.Mutex
	st   artStatus
}

func artSetStatus(f func(*artStatus)) {
	artState.mu.Lock()
	f(&artState.st)
	artState.mu.Unlock()
}

func artStatusNow() artStatus {
	artState.mu.Lock()
	defer artState.mu.Unlock()
	return artState.st
}

// artJob is the hourly look: at night, when nobody is watching.
func (s *Server) artJob(ctx context.Context) {
	s.artPosterSwaps(ctx) // the library's poster paths may have changed
	set := s.artSettingsNow()
	if !set.On || !set.inNight(time.Now()) {
		return
	}
	s.artPass(ctx, artPerRun, false)
}

// artPass looks at up to n titles, those not looked at for longest first.
// It stops when someone starts playing, the night ends (unless now), or
// Jellyfin's disk runs low.
func (s *Server) artPass(ctx context.Context, n int, now bool) {
	if !artState.busy.TryLock() {
		return
	}
	defer artState.busy.Unlock()
	srv, ok := s.jellyfinServer()
	if !ok || !s.TMDB().HasAPIKey() {
		artSetStatus(func(a *artStatus) { a.Note = "Jellyfin and a TMDB key are needed." })
		return
	}
	set := s.artSettingsNow()
	artSetStatus(func(a *artStatus) { *a = artStatus{Running: true, FreeGB: a.FreeGB} })
	defer artSetStatus(func(a *artStatus) { a.Running, a.LastMs = false, time.Now().UnixMilli() })
	note := func(msg string) { artSetStatus(func(a *artStatus) { a.Note = msg }) }

	if msg := s.artCanWrite(ctx, srv, set); msg != "" && set.Mode != "ask" {
		note(msg)
		return
	}
	if playing, err := s.mediaClient.NowPlaying(ctx, srv); err == nil && len(playing) > 0 {
		note("Waiting: someone is watching on Jellyfin.")
		return
	}
	items, err := s.artJFItems(ctx, srv, true)
	if err != nil {
		note(err.Error())
		return
	}
	if err := s.artSync(ctx, items); err != nil {
		note("The titles couldn't be saved: " + err.Error())
		return
	}
	type row struct {
		kind           string
		tmdb, tvdb     int
		title, jf, mis string
	}
	rows, err := s.db.QueryContext(ctx, `SELECT kind, tmdb_id, tvdb_id, title, jf_ids, missing FROM art_titles WHERE jf_ids != '' ORDER BY checked_ms, missing = '' LIMIT ?`, n)
	if err != nil {
		note(err.Error())
		return
	}
	var todo []row
	for rows.Next() {
		var r row
		if rows.Scan(&r.kind, &r.tmdb, &r.tvdb, &r.title, &r.jf, &r.mis) == nil {
			todo = append(todo, r)
		}
	}
	rows.Close()
	for i, r := range todo {
		if ctx.Err() != nil {
			return
		}
		if i > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(artPause):
			}
			if !now && !s.artSettingsNow().inNight(time.Now()) {
				note("Stopped for the day; it carries on tonight.")
				return
			}
			if i%20 == 0 {
				if playing, err := s.mediaClient.NowPlaying(ctx, srv); err == nil && len(playing) > 0 {
					note("Paused: someone started watching on Jellyfin.")
					return
				}
				if msg := s.artCanWrite(ctx, srv, set); msg != "" && set.Mode != "ask" {
					note(msg)
					return
				}
			}
		}
		proposed, applied, err := s.artLookAt(ctx, srv, set, r.kind, r.tmdb, r.tvdb, r.title, r.jf, r.mis, set.Mode != "fill")
		errText := ""
		if err != nil {
			errText = trimRunes(err.Error(), 300)
		}
		_, _ = s.db.ExecContext(ctx, `UPDATE art_titles SET checked_ms = ?, error = ? WHERE kind = ? AND tmdb_id = ?`, time.Now().UnixMilli(), errText, r.kind, r.tmdb)
		artSetStatus(func(a *artStatus) { a.Looked++; a.Proposed += proposed; a.Applied += applied })
	}
	if len(todo) == 0 {
		note("Nothing to look at: Jellyfin has no films or shows with a TMDB id.")
	}
}

// artCanWrite says why pictures can't go to Jellyfin now ("" when they can).
func (s *Server) artCanWrite(ctx context.Context, srv mediaservers.Server, set artSettings) string {
	free := jfFreeGB(ctx, srv)
	artSetStatus(func(a *artStatus) { a.FreeGB = free })
	if free < 0 {
		return "Stopped: Jellyfin doesn't say how much room its metadata disk has, so nothing is sent in bulk."
	}
	if free < float64(set.MinFreeGB) {
		return fmt.Sprintf("Stopped: Jellyfin's metadata disk has %.0f GB free, under the %d GB kept free.", free, set.MinFreeGB)
	}
	return ""
}

// artLookAt finds the title's best pictures for what it lacks (and, with
// small, for pictures that are too small) and proposes or makes the change.
func (s *Server) artLookAt(ctx context.Context, srv mediaservers.Server, set artSettings, kind string, tmdbID, tvdbID int, title, jfIDs, missing string, small bool) (proposed, applied int, err error) {
	ids := strings.Split(jfIDs, ",")
	styles, styleOn := s.artStylesNow()
	if set.PzOwns {
		styleOn = map[string]bool{} // Posterizarr makes them
	}
	var needs []string
	why := map[string]string{}
	for _, typ := range artTypes {
		if typ == "poster" && styleOn["poster"] || typ == "backdrop" && styleOn["backdrop"] {
			continue // made in the style (below)
		}
		if s.artLocked(ctx, kind, tmdbID, typ) || s.artPending(ctx, kind, tmdbID, typ) {
			continue
		}
		if strings.Contains(","+missing+",", ","+typ+",") {
			needs, why[typ] = append(needs, typ), "missing"
			continue
		}
		if small && typ != "logo" {
			w := jfImageWidth(ctx, srv, ids[0], typ)
			limit := artSmallPosterW
			if typ == "backdrop" {
				limit = artSmallBackdropW
			}
			if w > 0 && w < limit {
				needs, why[typ] = append(needs, typ), "small"
			}
		}
	}
	var cands map[string][]artCand
	if len(needs) > 0 {
		if cands, err = s.artCandidates(ctx, kind, tmdbID, tvdbID); err != nil {
			return 0, 0, err
		}
	}
	for _, typ := range needs {
		list := cands[typ]
		if len(list) == 0 {
			continue
		}
		best := list[0]
		if why[typ] == "small" && best.Width > 0 && best.Width <= jfImageWidth(ctx, srv, ids[0], typ) {
			continue // nothing bigger
		}
		res, err := s.db.ExecContext(ctx, `INSERT INTO art_changes (kind, tmdb_id, title, type, source, url, preview, width, height, lang, why, created_ms) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			kind, tmdbID, title, typ, best.Source, best.URL, best.Preview, best.Width, best.Height, best.Lang, why[typ], time.Now().UnixMilli())
		if err != nil {
			return proposed, applied, err
		}
		if set.Mode == "ask" {
			proposed++
			continue
		}
		id, _ := res.LastInsertId()
		if s.artMake(ctx, srv, id) == nil {
			applied++
		}
	}
	// Pictures in the household's style: the poster, and a show's season
	// posters and title cards, each once per style (a new style is offered
	// again).
	for _, what := range []string{"poster", "backdrop", "seasons", "cards"} {
		if !styleOn[artStyleFor[what]] || (what == "seasons" || what == "cards") && kind != "tv" || s.artLocked(ctx, kind, tmdbID, what) {
			continue
		}
		print := styles.printOf(what)
		var last int64
		var state string
		if s.db.QueryRowContext(ctx, `SELECT id, state FROM art_changes WHERE kind = ? AND tmdb_id = ? AND type = ? AND style = ? AND by_hand = 0 ORDER BY id DESC LIMIT 1`, kind, tmdbID, what, print).Scan(&last, &state) == nil {
			if state == "waiting" && set.Mode != "ask" && s.artMake(ctx, srv, last) == nil { // one that was paused
				applied++
			}
			continue
		}
		if what != "cards" { // made from art without words: is there any?
			if cands == nil {
				if cands, err = s.artCandidates(ctx, kind, tmdbID, tvdbID); err != nil {
					return proposed, applied, err
				}
			}
			found := false
			for _, c := range append(append([]artCand{}, cands["poster"]...), cands["backdrop"]...) {
				found = found || c.Lang == ""
			}
			if !found {
				continue
			}
		}
		res, err := s.db.ExecContext(ctx, `INSERT INTO art_changes (kind, tmdb_id, title, type, source, url, why, style, created_ms) VALUES (?, ?, ?, ?, 'usherr', '', 'styled', ?, ?)`,
			kind, tmdbID, title, what, print, time.Now().UnixMilli())
		if err != nil {
			return proposed, applied, err
		}
		if set.Mode == "ask" {
			proposed++
			continue
		}
		id, _ := res.LastInsertId()
		if s.artMake(ctx, srv, id) == nil {
			applied++
		}
	}
	return proposed, applied, nil
}

// artLocked: a picture chosen by hand stays.
func (s *Server) artLocked(ctx context.Context, kind string, tmdbID int, typ string) bool {
	var n int
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM art_changes WHERE kind = ? AND tmdb_id = ? AND type = ? AND by_hand = 1 AND state = 'done'`, kind, tmdbID, typ).Scan(&n)
	return n > 0
}

// artPending: a change waiting, or one skipped, isn't proposed again.
func (s *Server) artPending(ctx context.Context, kind string, tmdbID int, typ string) bool {
	var n int
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM art_changes WHERE kind = ? AND tmdb_id = ? AND type = ? AND by_hand = 0 AND state IN ('waiting', 'skipped', 'undone')`, kind, tmdbID, typ).Scan(&n)
	return n > 0
}

// artMake makes a change (by its id) and notes how it went.
func (s *Server) artMake(ctx context.Context, srv mediaservers.Server, id int64) error {
	list, err := s.artChanges(ctx, `WHERE c.id = ?`, id)
	if err != nil || len(list) == 0 {
		return errors.New("that change isn't there")
	}
	c := list[0]
	err = s.artApply(ctx, srv, c)
	if errors.Is(err, errArtPaused) {
		return err // still waiting: the next look carries on where it stopped
	}
	if err != nil {
		_, _ = s.db.ExecContext(ctx, `UPDATE art_changes SET state = 'failed', error = ? WHERE id = ?`, trimRunes(err.Error(), 300), id)
		return err
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE art_changes SET state = 'done', error = '', done_ms = ? WHERE id = ?`, time.Now().UnixMilli(), id)
	if c.Type == "poster" {
		s.artSwapsSoon()
	}
	// An earlier change of the same picture is superseded (its backups stay
	// with it, so the oldest picture can still come back from there).
	_, _ = s.db.ExecContext(ctx, `UPDATE art_changes SET by_hand = 0 WHERE kind = ? AND tmdb_id = ? AND type = ? AND id != ? AND state = 'done'`, c.Kind, c.TMDBID, c.Type, id)
	if c.Why == "missing" || c.Why == "chosen" {
		_, _ = s.db.ExecContext(ctx, `UPDATE art_titles SET missing = TRIM(REPLACE(','||missing||',', ','||?||',', ','), ',') WHERE kind = ? AND tmdb_id = ?`, c.Type, c.Kind, c.TMDBID)
	}
	return nil
}

// artForFilm looks at one film straight away (a Showtime was booked), so
// its art is ready before the night.
func (s *Server) artForFilm(tmdbID int) {
	set := s.artSettingsNow()
	srv, ok := s.jellyfinServer()
	if !set.On || !ok || tmdbID <= 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	items, err := s.artJFItems(ctx, srv, false)
	if err != nil || s.artSync(ctx, items) != nil {
		return
	}
	var tvdb int
	var title, jf, mis string
	if s.db.QueryRowContext(ctx, `SELECT tvdb_id, title, jf_ids, missing FROM art_titles WHERE kind = 'movie' AND tmdb_id = ?`, tmdbID).Scan(&tvdb, &title, &jf, &mis) != nil || jf == "" {
		return
	}
	if set.Mode != "ask" && s.artCanWrite(ctx, srv, set) != "" {
		return
	}
	_, _, _ = s.artLookAt(ctx, srv, set, "movie", tmdbID, tvdb, title, jf, mis, set.Mode != "fill")
	_, _ = s.db.ExecContext(ctx, `UPDATE art_titles SET checked_ms = ? WHERE kind = 'movie' AND tmdb_id = ?`, time.Now().UnixMilli(), tmdbID)
}

// ---- the API ----

// GET /api/home/art (admin): the settings, where things stand, the counts.
func (s *Server) handleArt(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	counts := map[string]int{}
	for k, q := range map[string]string{
		"titles":          `SELECT COUNT(*) FROM art_titles WHERE jf_ids != ''`,
		"looked":          `SELECT COUNT(*) FROM art_titles WHERE jf_ids != '' AND checked_ms > 0`,
		"missingPoster":   `SELECT COUNT(*) FROM art_titles WHERE jf_ids != '' AND ','||missing||',' LIKE '%,poster,%'`,
		"missingBackdrop": `SELECT COUNT(*) FROM art_titles WHERE jf_ids != '' AND ','||missing||',' LIKE '%,backdrop,%'`,
		"missingLogo":     `SELECT COUNT(*) FROM art_titles WHERE jf_ids != '' AND ','||missing||',' LIKE '%,logo,%'`,
		"waiting":         `SELECT COUNT(*) FROM art_changes WHERE state = 'waiting'`,
		"done":            `SELECT COUNT(*) FROM art_changes WHERE state = 'done'`,
		"failed":          `SELECT COUNT(*) FROM art_changes WHERE state = 'failed'`,
	} {
		var n int
		_ = s.db.QueryRowContext(ctx, q).Scan(&n)
		counts[k] = n
	}
	_, jf := s.jellyfinServer()
	writeJSON(w, http.StatusOK, map[string]any{"settings": s.artSettingsNow(), "status": artStatusNow(), "counts": counts, "jellyfin": jf, "tmdb": s.TMDB().HasAPIKey(), "folder": s.artDir()})
}

// PUT /api/home/art/settings (admin)
func (s *Server) handlePutArtSettings(w http.ResponseWriter, r *http.Request) {
	var b struct {
		artSettings
		FanartKey    *string `json:"fanartKey"`
		RemoveFanart bool    `json:"removeFanartKey"`
		TVDBKey      *string `json:"tvdbKey"`
		RemoveTVDB   bool    `json:"removeTvdbKey"`
	}
	if err := decodeJSON(r, &b); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	if b.Mode != "ask" && b.Mode != "fill" && b.Mode != "auto" || b.From < 0 || b.From > 23 || b.To < 0 || b.To > 23 || b.MinFreeGB < 1 || b.MinFreeGB > 1000 {
		writeError(w, http.StatusBadRequest, "Those settings aren't right.")
		return
	}
	bit := func(v bool) string {
		if v {
			return "1"
		}
		return "0"
	}
	for k, v := range map[string]string{keyArtOn: bit(b.On), keyArtMode: b.Mode, keyArtTextless: bit(b.Textless), keyArtFrom: strconv.Itoa(b.From), keyArtTo: strconv.Itoa(b.To),
		keyArtMinFree: strconv.Itoa(b.MinFreeGB), keyArtFolder: strings.TrimSpace(b.Folder), keyArtPosterizarr: strings.TrimSpace(b.Posterizarr), keyArtPzOwns: bit(b.PzOwns)} {
		if err := s.Settings.Set(k, v, false); err != nil {
			writeError(w, http.StatusInternalServerError, "The settings couldn't be saved.")
			return
		}
	}
	switch {
	case b.RemoveFanart:
		_ = s.Settings.Set(keyArtFanart, "", false)
	case b.FanartKey != nil && strings.TrimSpace(*b.FanartKey) != "":
		_ = s.Settings.Set(keyArtFanart, strings.TrimSpace(*b.FanartKey), true)
		_ = s.Settings.Set(keyArtFanartAs, "", false)
		fanartAnswers.clear()
	}
	switch {
	case b.RemoveTVDB:
		_ = s.Settings.Set(keyArtTVDB, "", false)
		tvdbAnswers.clear()
	case b.TVDBKey != nil && strings.TrimSpace(*b.TVDBKey) != "":
		_ = s.Settings.Set(keyArtTVDB, strings.TrimSpace(*b.TVDBKey), true)
		tvdbAnswers.clear()
	}
	writeJSON(w, http.StatusOK, s.artSettingsNow())
}

// POST /api/home/art/test-fanart (admin): asks fanart.tv for a well-known
// film's art with the key.
func (s *Server) handleArtTestFanart(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	got, err := s.fanartImages(ctx, "movie", 603, 0) // The Matrix
	if errors.Is(err, errNoFanart) {
		writeError(w, http.StatusBadRequest, "Put in a fanart.tv key first.")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	n := 0
	for _, l := range got {
		n += len(l)
	}
	if n == 0 {
		writeError(w, http.StatusBadGateway, "fanart.tv answered, but with no pictures: the key may not be right.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"pictures": n})
}

// POST /api/home/art/look (admin): a pass now (as at night; it still stops
// when someone starts watching).
func (s *Server) handleArtLook(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.jellyfinServer(); !ok {
		writeError(w, http.StatusConflict, connectServerFirst)
		return
	}
	if artStatusNow().Running {
		writeError(w, http.StatusConflict, "A look is going on already.")
		return
	}
	go s.artPass(context.Background(), artPerRun, true)
	writeJSON(w, http.StatusAccepted, map[string]bool{"started": true})
}

// GET /api/home/art/changes?state=waiting&limit=60 (admin)
func (s *Server) handleArtChanges(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	if state == "" {
		state = "waiting"
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 60
	}
	order := `ORDER BY c.id`
	if state != "waiting" {
		order = `ORDER BY c.done_ms DESC, c.id DESC`
	}
	list, err := s.artChanges(r.Context(), `WHERE c.state = ? `+order+` LIMIT ?`, state, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The changes couldn't be read.")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// POST /api/home/art/changes/apply {ids, all} (admin): makes the approved
// changes, in the background, a picture a second.
func (s *Server) handleArtApply(w http.ResponseWriter, r *http.Request) {
	ids, ok := s.artPickIDs(w, r)
	if !ok {
		return
	}
	srv, okJF := s.jellyfinServer()
	if !okJF {
		writeError(w, http.StatusConflict, connectServerFirst)
		return
	}
	if msg := s.artCanWrite(r.Context(), srv, s.artSettingsNow()); msg != "" && len(ids) > 20 {
		writeError(w, http.StatusConflict, msg)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
		defer cancel()
		for i, id := range ids {
			if i > 0 {
				time.Sleep(time.Second)
			}
			if err := s.artMake(ctx, srv, id); err != nil {
				slog.Info("artwork: change not made", "id", id, "err", err)
			}
		}
	}()
	writeJSON(w, http.StatusAccepted, map[string]int{"changes": len(ids)})
}

// POST /api/home/art/changes/skip {ids, all} (admin): not wanted; not
// proposed again.
func (s *Server) handleArtSkip(w http.ResponseWriter, r *http.Request) {
	ids, ok := s.artPickIDs(w, r)
	if !ok {
		return
	}
	for _, id := range ids {
		_, _ = s.db.ExecContext(r.Context(), `UPDATE art_changes SET state = 'skipped', done_ms = ? WHERE id = ? AND state = 'waiting'`, time.Now().UnixMilli(), id)
	}
	writeJSON(w, http.StatusOK, map[string]int{"changes": len(ids)})
}

// artPickIDs reads {ids: [...]} or {all: true} (every waiting change).
func (s *Server) artPickIDs(w http.ResponseWriter, r *http.Request) ([]int64, bool) {
	var b struct {
		IDs []int64 `json:"ids"`
		All bool    `json:"all"`
	}
	if err := decodeJSON(r, &b); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return nil, false
	}
	if b.All {
		rows, err := s.db.QueryContext(r.Context(), `SELECT id FROM art_changes WHERE state = 'waiting' ORDER BY id`)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "The changes couldn't be read.")
			return nil, false
		}
		defer rows.Close()
		b.IDs = nil
		for rows.Next() {
			var id int64
			if rows.Scan(&id) == nil {
				b.IDs = append(b.IDs, id)
			}
		}
	}
	return b.IDs, true
}

// POST /api/home/art/changes/{id}/undo (admin): puts the old picture back.
func (s *Server) handleArtUndo(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	list, err := s.artChanges(r.Context(), `WHERE c.id = ? AND c.state = 'done'`, id)
	if err != nil || len(list) == 0 {
		writeError(w, http.StatusNotFound, "That change isn't there, or wasn't made.")
		return
	}
	srv, ok := s.jellyfinServer()
	if !ok {
		writeError(w, http.StatusConflict, connectServerFirst)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	if err := s.artUndo(ctx, srv, list[0]); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE art_changes SET state = 'undone', by_hand = 0, done_ms = ? WHERE id = ?`, time.Now().UnixMilli(), id)
	if list[0].Type == "poster" {
		s.artSwapsSoon()
	}
	writeJSON(w, http.StatusOK, map[string]bool{"undone": true})
}

// GET /api/home/art/title?kind=movie&tmdbId= (admin): every candidate, and
// what the title has now.
func (s *Server) handleArtTitle(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	tmdbID, _ := strconv.Atoi(r.URL.Query().Get("tmdbId"))
	if kind != "movie" && kind != "tv" && kind != "collection" || tmdbID <= 0 {
		writeError(w, http.StatusBadRequest, "Which title?")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	srv, ok := s.jellyfinServer()
	var jf string
	tvdb := 0
	// The titles the nightly look keeps say which Jellyfin items are this
	// one; Jellyfin's whole list is read only for a title not among them.
	look := func() {
		_ = s.db.QueryRowContext(ctx, `SELECT jf_ids, tvdb_id FROM art_titles WHERE kind = ? AND tmdb_id = ?`, kind, tmdbID).Scan(&jf, &tvdb)
	}
	if ok {
		if look(); jf == "" {
			if items, err := s.artJFItems(ctx, srv, false); err == nil {
				_ = s.artSync(ctx, items)
			}
			look()
		}
	}
	cands, err := s.artCandidates(ctx, kind, tmdbID, tvdb)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	for typ, l := range cands {
		if len(l) > 40 {
			cands[typ] = l[:40]
		}
	}
	chosen := map[string]bool{}
	for _, typ := range artTypes {
		chosen[typ] = s.artLocked(context.WithoutCancel(ctx), kind, tmdbID, typ)
		if cands[typ] == nil {
			cands[typ] = []artCand{}
		}
	}
	current := ""
	if jf != "" {
		current = strings.Split(jf, ",")[0]
	}
	dctx, dcancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
	defer dcancel()
	history, _ := s.artChanges(dctx, `WHERE c.kind = ? AND c.tmdb_id = ? AND c.state IN ('done', 'undone', 'waiting', 'failed') ORDER BY c.id DESC LIMIT 20`, kind, tmdbID)
	if history == nil {
		history = []artChange{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"candidates": cands, "current": current, "inJellyfin": jf != "", "copies": len(strings.Split(jf, ",")) * btoi(jf != ""), "chosen": chosen, "changes": history})
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// POST /api/home/art/title/choose {kind, tmdbId, type, url} (admin): a
// picture chosen by hand, put on at once and kept.
func (s *Server) handleArtChoose(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Kind   string `json:"kind"`
		TMDBID int    `json:"tmdbId"`
		Type   string `json:"type"`
		URL    string `json:"url"`
	}
	if err := decodeJSON(r, &b); err != nil || jfImageType[b.Type] == "" || b.TMDBID <= 0 || (b.Kind != "movie" && b.Kind != "tv" && b.Kind != "collection") {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	srv, ok := s.jellyfinServer()
	if !ok {
		writeError(w, http.StatusConflict, connectServerFirst)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	var title, jf string
	if s.db.QueryRowContext(ctx, `SELECT title, jf_ids FROM art_titles WHERE kind = ? AND tmdb_id = ?`, b.Kind, b.TMDBID).Scan(&title, &jf) != nil || jf == "" {
		writeError(w, http.StatusConflict, "Jellyfin doesn't have this title yet.")
		return
	}
	// The picture must be one of the title's own candidates.
	var tvdb int
	_ = s.db.QueryRowContext(ctx, `SELECT tvdb_id FROM art_titles WHERE kind = ? AND tmdb_id = ?`, b.Kind, b.TMDBID).Scan(&tvdb)
	cands, err := s.artCandidates(ctx, b.Kind, b.TMDBID, tvdb)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	var pick *artCand
	for i := range cands[b.Type] {
		if cands[b.Type][i].URL == b.URL {
			pick = &cands[b.Type][i]
		}
	}
	if pick == nil {
		writeError(w, http.StatusBadRequest, "That picture isn't one of this title's.")
		return
	}
	// A waiting proposal for the same picture gives way.
	_, _ = s.db.ExecContext(ctx, `UPDATE art_changes SET state = 'skipped', done_ms = ? WHERE kind = ? AND tmdb_id = ? AND type = ? AND state = 'waiting'`, time.Now().UnixMilli(), b.Kind, b.TMDBID, b.Type)
	res, err := s.db.ExecContext(ctx, `INSERT INTO art_changes (kind, tmdb_id, title, type, source, url, preview, width, height, lang, why, by_hand, created_ms) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'chosen', 1, ?)`,
		b.Kind, b.TMDBID, title, b.Type, pick.Source, pick.URL, pick.Preview, pick.Width, pick.Height, pick.Lang, time.Now().UnixMilli())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The choice couldn't be saved.")
		return
	}
	id, _ := res.LastInsertId()
	if err := s.artMake(ctx, srv, id); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

// artPreviews keeps the small pictures fetched for the side-by-side a while.
var artPreviews = newTTLBytes(400, 30*time.Minute)

// GET /api/home/art/preview?u=<fanart.tv picture> (admin): fanart.tv's
// small copy, or the picture itself made small when there's none.
func (s *Server) handleArtPreview(w http.ResponseWriter, r *http.Request) {
	u := r.URL.Query().Get("u")
	if !strings.HasPrefix(u, "https://assets.fanart.tv/fanart/") && !artTestHost(u) {
		http.NotFound(w, r)
		return
	}
	if b, ok := artPreviews.get(u); ok {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Cache-Control", "private, max-age=3600")
		_, _ = w.Write(b)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	b, err := artSmall(ctx, strings.Replace(u, "/fanart/", "/preview/", 1))
	if err != nil {
		b, err = artSmall(ctx, u)
	}
	if err != nil {
		http.NotFound(w, r)
		return
	}
	artPreviews.put(u, b)
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	_, _ = w.Write(b)
}

// artSmall downloads a picture and makes it 400 pixels wide at most (a JPEG,
// on dark grey where it's see-through, as a logo is).
func artSmall(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := artHTTP().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	img, _, err := image.Decode(io.LimitReader(resp.Body, maxMetaImage))
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	w := min(b.Dx(), 400)
	h := max(b.Dy()*w/max(b.Dx(), 1), 1)
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.RGBA{0x33, 0x33, 0x33, 0xff}), image.Point{}, draw.Src)
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Over, nil)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 82}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// GET /api/home/art/current/{item}/{type} (admin): the picture Jellyfin has
// now, small, for the side-by-side.
func (s *Server) handleArtCurrent(w http.ResponseWriter, r *http.Request) {
	typ := r.PathValue("type")
	item := r.PathValue("item")
	if jfImageType[typ] == "" || item == "" || len(item) > 64 || strings.ContainsAny(item, "/?&#") {
		http.NotFound(w, r)
		return
	}
	srv, ok := s.jellyfinServer()
	if !ok {
		http.NotFound(w, r)
		return
	}
	data, mime, err := jfGetImage(r.Context(), srv, item, typ, 500)
	if err != nil || len(data) == 0 {
		http.NotFound(w, r)
		return
	}
	if mime == "" {
		mime = "image/jpeg"
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "private, max-age=60")
	_, _ = w.Write(data)
}

// ttlBytes is a small cache of bytes by key, each kept a while; the oldest
// go when it's full.
type ttlBytes struct {
	mu   sync.Mutex
	max  int
	ttl  time.Duration
	m    map[string]ttlEntry
	keys []string
}

type ttlEntry struct {
	b  []byte
	at time.Time
}

func newTTLBytes(n int, ttl time.Duration) *ttlBytes {
	return &ttlBytes{max: n, ttl: ttl, m: map[string]ttlEntry{}}
}

func (c *ttlBytes) get(k string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[k]
	if !ok || time.Since(e.at) > c.ttl {
		return nil, false
	}
	return e.b, true
}

func (c *ttlBytes) put(k string, b []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.m[k]; !ok {
		c.keys = append(c.keys, k)
	}
	c.m[k] = ttlEntry{b: b, at: time.Now()}
	for len(c.keys) > c.max {
		delete(c.m, c.keys[0])
		c.keys = c.keys[1:]
	}
}

func (c *ttlBytes) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m, c.keys = map[string]ttlEntry{}, nil
}
