package api

// Usherr: the pre-show in Plex. Plex plays the same videos before every
// film (its "Cinema Trailers pre-roll video" setting, a Plex Pass feature):
// a list of files on the Plex computer. So Usherr puts a pre-show together
// (the theme's clips, the trailers downloaded, the cards that don't need the
// film), makes it videos in the virtual drive's Preshow folder, and points
// Plex's setting at them, fresh every half hour so it keeps changing. The
// rating card is left out: Plex doesn't say which film is next. A pre-roll
// Plex already has from elsewhere (NeXroll, by hand) is left alone.
//
// The household chooses the same pre-show as everywhere else, or Plex's
// own theme (keyPreshowPlexTheme), as Jellyfin's has.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/mediaservers"
)

const (
	keyPreshowPlexOn    = "home.preshow.plexOn"    // "1": before films in Plex
	keyPreshowPlexTheme = "home.preshow.plexTheme" // the theme; 0 the same as everywhere
	keyPreshowPlexSet   = "home.preshow.plexSet"   // the pre-roll Usherr last gave Plex

	plexPrerollPref  = "CinemaTrailersPrerollID"
	preshowPlexEvery = 30 * time.Minute
)

type preshowPlex struct {
	On      bool  `json:"on"`
	ThemeID int64 `json:"themeId"`
}

func (s *Server) preshowPlexNow() preshowPlex {
	on, _ := s.Settings.Get(keyPreshowPlexOn)
	th, _ := s.Settings.Get(keyPreshowPlexTheme)
	theme, _ := strconv.ParseInt(th, 10, 64)
	return preshowPlex{On: on == "1", ThemeID: theme}
}

// What the last build did, for the settings card.
var preshowPlexLast struct {
	sync.Mutex
	at     time.Time
	videos int
	theme  string
	note   string
}

func preshowPlexNote(videos int, theme, note string) {
	preshowPlexLast.Lock()
	preshowPlexLast.at, preshowPlexLast.videos, preshowPlexLast.theme, preshowPlexLast.note = time.Now(), videos, theme, note
	preshowPlexLast.Unlock()
}

var preshowPlexBusy sync.Mutex

// preshowPlexJob keeps Plex's pre-show fresh.
func (s *Server) preshowPlexJob(ctx context.Context) { s.preshowPlexBuild(ctx) }

// preshowPlexBuild puts a pre-show together for Plex and gives Plex its
// videos; with the pre-show off for Plex, it takes back the pre-roll it
// gave (and nothing else).
func (s *Server) preshowPlexBuild(ctx context.Context) {
	if !preshowPlexBusy.TryLock() {
		return
	}
	defer preshowPlexBusy.Unlock()
	set := s.preshowPlexNow()
	servers := s.plexServers()
	if !set.On || len(servers) == 0 || !s.driveOn() {
		setDrivePreshow(nil)
		s.preshowPlexGive(ctx, servers, "")
		switch {
		case !set.On:
			preshowPlexNote(0, "", "")
		case len(servers) == 0:
			preshowPlexNote(0, "", "Connect Plex first, under Settings, Media servers.")
		default:
			preshowPlexNote(0, "", "The virtual drive is off: Plex reads the pre-show from it (Settings, Extras, Virtual drive).")
		}
		return
	}
	film := preshowFilm{Kind: "movie", Files: true}
	plan := s.planPreshow(ctx, film, set.ThemeID, 0, time.Now())
	var files []driveLocal
	add := func(title, path string) {
		fi, err := os.Stat(path)
		if err != nil || fi.Size() == 0 || time.Since(fi.ModTime()) < preshowSettle {
			return // gone, or still being written
		}
		name := fmt.Sprintf("%02d %s%s", len(files)+1, cleanName(title), strings.ToLower(filepath.Ext(path)))
		files = append(files, driveLocal{name: name, path: path, size: fi.Size(), mod: fi.ModTime()})
	}
	for _, st := range plan.Steps {
		switch st.Kind {
		case "clip":
			id := strings.TrimSuffix(strings.TrimPrefix(st.URL, "/api/home/preshow/clips/"), "/file")
			if c, ok := s.preshowClipByID(ctx, id); ok {
				if p, ok := s.preshowClipFile(c); ok {
					add(st.Title, p)
				}
			}
		case "trailer":
			id := strings.TrimSuffix(strings.TrimPrefix(st.URL, "/api/home/preshow/trailers/"), "/file")
			if t, ok := s.trailerByID(ctx, id); ok && t.State == "ready" && t.File != "" {
				add("Coming soon - "+t.Title, filepath.Join(s.trailersDir(), filepath.Base(t.File)))
			}
		case "card":
			if st.Card == "rating" {
				continue // Plex doesn't say which film is next
			}
			p, err := s.preshowCardFile(ctx, preshowCardRemember(s.preshowCardFor(st, film)))
			if err != nil {
				slog.Info("preshow: a card for Plex wasn't made", "card", st.Card, "err", err)
				continue
			}
			name := preshowCardName[st.Card]
			if st.Card == "soon" {
				name += " - " + st.Title
			}
			add(name, p)
		}
	}
	setDrivePreshow(files)
	s.recordPlayed(ctx, plan) // so the next one picks others
	if len(files) == 0 {
		s.preshowPlexGive(ctx, servers, "")
		preshowPlexNote(0, plan.Theme.Name, "Nothing to show yet: add clips, or let trailers download.")
		return
	}
	// Plex is told once rclone's folder list has caught up.
	where := s.driveWhere()
	var paths []string
	for _, f := range files {
		paths = append(paths, driveServerFolder(where, "/Preshow/"+f.name))
	}
	select {
	case <-ctx.Done():
		return
	case <-time.After(driveScanAfter):
	}
	note := s.preshowPlexGive(ctx, servers, strings.Join(paths, ","))
	preshowPlexNote(len(files), plan.Theme.Name, note)
}

// preshowPlexGive sets Plex's pre-roll to value ("" takes Usherr's back):
// only where Plex has none, or the one Usherr gave last. It says why not,
// when it didn't.
func (s *Server) preshowPlexGive(ctx context.Context, servers []mediaservers.Server, value string) string {
	last, _ := s.Settings.Get(keyPreshowPlexSet)
	note := ""
	for _, srv := range servers {
		now, err := s.mediaClient.PlexPref(ctx, srv, plexPrerollPref)
		if err != nil {
			note = err.Error()
			continue
		}
		if now == value {
			continue
		}
		if now != "" && now != last {
			note = "Plex has a pre-roll of its own (from NeXroll, or set by hand): Usherr leaves it. Clear it in Plex (Settings, Extras) to use Usherr's."
			continue
		}
		if err := s.mediaClient.PlexSetPref(ctx, srv, plexPrerollPref, value); err != nil {
			note = err.Error()
			continue
		}
		slog.Info("preshow: Plex's pre-roll set", "server", srv.Name, "videos", strings.Count(value, ",")+btoi(value != ""))
	}
	if note == "" || value == "" {
		_ = s.Settings.Set(keyPreshowPlexSet, value, false)
	}
	return note
}

// ---- the API ----

type preshowPlexState struct {
	preshowPlex
	Plex    bool   `json:"plex"`    // a Plex server is connected
	Drive   bool   `json:"drive"`   // the virtual drive is on
	Videos  int    `json:"videos"`  // in the pre-show Plex has now
	Theme   string `json:"theme"`   // the theme it came from
	BuiltMs int64  `json:"builtMs"` // when
	Note    string `json:"note"`    // why not, or what to know
	Where   string `json:"where"`   // the folder Plex reads them from
}

func (s *Server) preshowPlexState() preshowPlexState {
	st := preshowPlexState{preshowPlex: s.preshowPlexNow(), Plex: len(s.plexServers()) > 0, Drive: s.driveOn()}
	preshowPlexLast.Lock()
	st.Videos, st.Theme, st.Note = preshowPlexLast.videos, preshowPlexLast.theme, preshowPlexLast.note
	if !preshowPlexLast.at.IsZero() {
		st.BuiltMs = preshowPlexLast.at.UnixMilli()
	}
	preshowPlexLast.Unlock()
	st.Where = driveServerFolder(s.driveWhere(), "/Preshow")
	return st
}

// GET /api/home/preshow/plex (admin)
func (s *Server) handlePreshowPlex(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.preshowPlexState())
}

// PUT /api/home/preshow/plex {on, themeId} (admin): saved, and the pre-show
// is put together again at once (in the background).
func (s *Server) handlePutPreshowPlex(w http.ResponseWriter, r *http.Request) {
	var b preshowPlex
	if err := decodeJSON(r, &b); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	if b.ThemeID < 0 {
		writeError(w, http.StatusBadRequest, errors.New("pick a theme, or the same as everywhere").Error())
		return
	}
	on := "0"
	if b.On {
		on = "1"
	}
	if s.Settings.Set(keyPreshowPlexOn, on, false) != nil || s.Settings.Set(keyPreshowPlexTheme, strconv.FormatInt(b.ThemeID, 10), false) != nil {
		writeError(w, http.StatusInternalServerError, "That couldn't be saved.")
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		s.preshowPlexBuild(ctx)
	}()
	writeJSON(w, http.StatusOK, s.preshowPlexState())
}
