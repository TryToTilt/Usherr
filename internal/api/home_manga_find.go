package api

// Usherr: finding and following manga. Discover's shelves, what's related to and
// like a series, and someone's public AniList list all come from AniList, with no
// key and nothing saved until Add. A followed series' new volume goes on the
// calendar and sends a notification, and a light refresh between 2 and 9 keeps each
// series' details and status fresh.

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/manga"
	"github.com/rdborg/mediarium/internal/notify"
)

const (
	mangaRefreshMostPerNight = 300              // AniList is asked a second apart at most, and this is plenty
	mangaRefreshJobEvery     = 30 * time.Minute // it stops as soon as nothing is due
)

func (s *Server) registerMangaFindRoutes(member routeGroup, on func(http.HandlerFunc) http.HandlerFunc) {
	member.Need(auth.PermManga).HandleFunc("GET /api/manga/discover", on(s.handleMangaDiscover))
	member.Need(auth.PermManga).HandleFunc("GET /api/manga/anilist-list", on(s.handleMangaAniListList))
	member.HandleFunc("GET /api/manga/{id}/about", on(s.handleMangaAbout))
	member.HandleFunc("GET /api/manga/stats", on(s.handleMangaStats))
}

// mangaAniListProblem says in plain words why AniList couldn't answer.
func mangaAniListProblem(err error) string {
	if errors.Is(err, manga.ErrAniListBusy) {
		return "AniList asked Usherr to slow down. Try again in a minute."
	}
	return "AniList couldn't be reached right now. Your library is fine: try again in a few minutes."
}

// mangaFoundForPage dresses AniList's answers for the page: no novels, 18+ only for
// accounts allowed it, what's already in the library marked.
func (s *Server) mangaFoundForPage(r *http.Request, found []manga.Found) []manga.Found {
	have, _ := s.mangaRepo().LibraryIDs()
	adult := s.canSeeAdultManga(r)
	dex := s.dexOn()
	out := make([]manga.Found, 0, len(found))
	for _, f := range found {
		if f.IsNovel || (f.IsAdult && !adult) {
			continue
		}
		if !dex {
			f.MangaDexID = "" // only for reading online, which needs MangaDex on
		}
		f.InLibrary = have[f.AniListID]
		f.CoverURL = s.mangaImage(f.CoverURL)
		out = append(out, f)
	}
	return out
}

// GET /api/manga/discover?shelf=trending|popular|new&genre=&page=: a page of a shelf.
func (s *Server) handleMangaDiscover(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	shelf := q.Get("shelf")
	if shelf != manga.ShelfPopular && shelf != manga.ShelfNew {
		shelf = manga.ShelfTrending
	}
	genre := q.Get("genre")
	if !manga.ValidGenre(genre) {
		writeError(w, http.StatusBadRequest, "That isn't a genre.")
		return
	}
	page, _ := strconv.Atoi(q.Get("page"))
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	found, more, err := s.mangaMod().anilist.Discover(ctx, shelf, genre, page, time.Now())
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"results": []any{}, "genres": manga.Genres, "problem": mangaAniListProblem(err)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": s.mangaFoundForPage(r, found), "more": more, "genres": manga.Genres})
}

// GET /api/manga/anilist-list?user=: what a public AniList profile is reading and
// plans to read.
func (s *Server) handleMangaAniListList(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.URL.Query().Get("user"))
	if name == "" || len(name) > 40 {
		writeError(w, http.StatusBadRequest, "Type an AniList user name.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	found, err := s.mangaMod().anilist.UserList(ctx, name)
	switch {
	case errors.Is(err, manga.ErrNoSuchList):
		writeJSON(w, http.StatusOK, map[string]any{"results": []any{}, "problem": "AniList has no public manga list for " + name + ". Check the name, or the list may be private."})
	case err != nil:
		writeJSON(w, http.StatusOK, map[string]any{"results": []any{}, "problem": mangaAniListProblem(err)})
	default:
		writeJSON(w, http.StatusOK, map[string]any{"results": s.mangaFoundForPage(r, found)})
	}
}

// GET /api/manga/{id}/about: genres, score, related series and what's like this one.
// A series added by name, or AniList being away, just has none: the page does without.
func (s *Server) handleMangaAbout(w http.ResponseWriter, r *http.Request) {
	sr, ok := s.mangaFromPath(w, r)
	if !ok {
		return
	}
	out := map[string]any{"genres": []string{}, "related": []any{}, "similar": []any{}}
	if sr.AniListID > 0 {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		about, err := s.mangaMod().anilist.AboutSeries(ctx, sr.AniListID)
		if err != nil {
			slog.Info("manga: about", "series", sr.Title, "err", err)
		} else {
			out["genres"], out["score"], out["similar"] = about.Genres, about.Score, s.mangaFoundForPage(r, about.Similar)
			found := make([]manga.Found, len(about.Related))
			for i, rel := range about.Related {
				found[i] = rel.Found
				found[i].Relation = rel.Relation
			}
			out["related"] = s.mangaFoundForPage(r, found)
		}
	}
	w.Header().Set("Cache-Control", "private, max-age=3600")
	writeJSON(w, http.StatusOK, out)
}

// GET /api/manga/stats: what this person has read.
func (s *Server) handleMangaStats(w http.ResponseWriter, r *http.Request) {
	st, err := s.mangaRepo().ReadStatsFor(requestUserID(r), time.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "That couldn't be read.")
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// pacedAddLook runs the look that follows an Add. One press adds a series, so ten
// presses shouldn't be ten searches at once: they wait their turn, with the usual gap
// between them, and each has its full time once its turn comes.
func (s *Server) pacedAddLook(run func(context.Context)) {
	st := s.mangaMod()
	st.addLook.Lock()
	defer st.addLook.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	run(ctx)
	sleepOrDone(ctx, s.mangaGap())
}

// noteNewMangaVolumes tells about volumes a followed series just got: on the
// calendar, and one notification however many there are. An adult series isn't
// announced, since a calendar and a phone are seen by others.
func (s *Server) noteNewMangaVolumes(sr manga.Series, list []manga.Volume) {
	if !sr.MonitorNew || len(list) == 0 {
		return
	}
	if err := s.mangaRepo().NoteNewVolumes(list, time.Now()); err != nil {
		slog.Warn("manga: new volumes", "series", sr.Title, "err", err)
	}
	if !sr.IsAdult {
		s.notifyItem(notify.EventNewVolume, mangaItem(sr, manga.NumbersOf(list), "", ""))
	}
}

// mangaCalendarEntries are the new volumes of followed series, on the day each was
// found: AniList has no dates for volumes, so that is the day that's known.
func (s *Server) mangaCalendarEntries() []calendarEntryPayload {
	if !s.mangaEnabled() {
		return nil
	}
	now := time.Now()
	list, err := s.mangaRepo().NewVolumesBetween(now.Add(-calendarEpisodeLookback).Format("2006-01-02"), now.Add(calendarEpisodeLookahead).Format("2006-01-02"))
	if err != nil {
		return nil
	}
	out := make([]calendarEntryPayload, 0, len(list))
	for _, e := range list {
		status := "missing"
		if e.Status == manga.StatusDownloaded {
			status = "downloaded"
		} else if busy, _ := s.mangaRepo().ActiveForVolume(e.VolumeID); busy {
			status = "downloading"
		}
		out = append(out, calendarEntryPayload{Kind: "manga", ID: e.VolumeID, MangaSeriesID: e.SeriesID, Title: e.Series, Subtitle: "Volume " + e.Number, ReleaseDate: e.FoundOn, Status: status, PosterURL: s.mangaImage(e.CoverURL)})
	}
	return out
}

// mangaRefreshJob asks AniList about each series that's due, between 2 and 9, so
// its status and volume count stay fresh and a new volume is noticed. It's light:
// one question a series, spaced out, and it stops if AniList asks it to.
func (s *Server) mangaRefreshJob(ctx context.Context) {
	if !s.mangaEnabled() {
		return
	}
	now := time.Now()
	if now.Hour() < mangaScanFromHour || now.Hour() >= mangaScanToHour {
		return
	}
	s.refreshDueManga(ctx, now)
}

// refreshDueManga refreshes the series that are due, up to the night's most.
func (s *Server) refreshDueManga(ctx context.Context, now time.Time) {
	list, err := s.mangaRepo().ListSeries()
	if err != nil {
		return
	}
	asked := 0
	for _, sr := range list {
		if ctx.Err() != nil || asked >= mangaRefreshMostPerNight {
			return
		}
		if sr.AniListID <= 0 || !manga.RefreshDue(sr.Status, sr.CheckedAt, now) {
			continue
		}
		asked++
		rctx, cancel := context.WithTimeout(ctx, time.Minute)
		_, err := s.refreshMangaSeries(rctx, sr)
		cancel()
		if err != nil {
			slog.Info("manga: refresh", "series", sr.Title, "err", err)
			if errors.Is(err, manga.ErrAniListBusy) {
				return // it asked to slow down: not one more this run
			}
		}
	}
}
