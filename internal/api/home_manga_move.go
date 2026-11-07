package api

// Usherr: bringing manga over from Comics, in the way of the music tidy: look first
// (a preview of every new file name, nothing changed), then apply, then undo if you
// like. Only an administrator pressing the button starts it. Files are moved and
// never deleted or overwritten: a duplicate stays where it is, and says so. The comic
// series is hidden rather than deleted, so an undo gets it back as it was.

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/comics"
	"github.com/rdborg/mediarium/internal/manga"
)

func (s *Server) registerMangaMoveRoutes(admin routeGroup, on func(http.HandlerFunc) http.HandlerFunc) {
	admin.HandleFunc("GET /api/manga/move", on(s.handleMangaMoveState))
	admin.HandleFunc("POST /api/manga/move/preview", on(s.handleMangaMovePreview))
	admin.HandleFunc("POST /api/manga/move/empty", on(s.handleMangaMoveEmpty))
	admin.HandleFunc("POST /api/manga/move/{id}/apply", on(s.handleMangaMoveApply))
	admin.HandleFunc("POST /api/manga/move/{id}/undo", on(s.handleMangaMoveUndo))
}

// mangaMoveCandidate is a comic series that looks like manga.
type mangaMoveCandidate struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Publisher string `json:"publisher"`
	StartYear int    `json:"startYear"`
	Type      string `json:"type,omitempty"`
	Issues    int    `json:"issues"`
	Files     int    `json:"files"`
	Magazine  bool   `json:"magazine"`
}

// GET /api/manga/move: comic series that look like manga, and the moves so far.
func (s *Server) handleMangaMoveState(w http.ResponseWriter, r *http.Request) {
	repo := s.mangaRepo()
	out := []mangaMoveCandidate{}
	list, err := s.comicRepo().ListSeries()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The comics couldn't be read.")
		return
	}
	for _, sr := range list {
		if sr.Type != comics.TypeManga && sr.Type != comics.TypeWebtoon && !(sr.Type == "" && comics.PublisherManga(sr)) {
			continue
		}
		issues := sr.IssueCount
		if all, err := s.comicRepo().Issues(sr.ID); err == nil {
			issues = len(all)
		}
		out = append(out, mangaMoveCandidate{ID: sr.ID, Name: sr.Name, Publisher: sr.Publisher, StartYear: sr.StartYear, Type: sr.Type, Issues: issues, Files: sr.Have, Magazine: manga.LooksLikeMagazine(sr.Name, issues)})
	}
	moves, err := repo.Moves()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The moves couldn't be read.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"candidates": out, "moves": moves, "folder": s.mangaRoot()})
}

// planMangaMove works out what bringing a comic series over would do. It reads
// files, and changes nothing.
func (s *Server) planMangaMove(sr comics.Series, title string, anilistID, into int64, intoFolder string) (manga.MovePlan, error) {
	plan := manga.MovePlan{ComicSeriesID: sr.ID, ComicName: sr.Name, Title: title, AniListID: anilistID, MangaSeriesID: into, Files: []manga.MoveFile{}, Issues: []manga.MoveIssue{}}
	dest := manga.Series{Title: title, Folder: intoFolder}
	folderOf := func(number, file string) (string, error) { return s.mangaDest(dest, number, file) }
	if p, err := folderOf("1", "x.cbz"); err == nil {
		plan.Folder = filepath.Dir(p)
	} else {
		return plan, err
	}
	issues, err := s.comicRepo().Issues(sr.ID)
	if err != nil {
		return plan, err
	}
	have := map[string]manga.Volume{}
	if into > 0 {
		if list, err := s.mangaRepo().Volumes(into); err == nil {
			for _, v := range list {
				have[v.Number] = v
			}
		}
	}
	claimed := map[string]string{} // volume to the file that moves for it
	numbers := map[string]bool{}
	owned := map[string]bool{}
	names := []string{title, sr.Name}
	consider := func(issueID int64, number, file string) {
		f := manga.MoveFile{IssueID: issueID, Number: number, From: file, Action: "stay"}
		if st, err := os.Stat(file); err == nil {
			f.Size = st.Size()
		} else {
			f.Why = manga.StayChanged
			plan.Files = append(plan.Files, f)
			return
		}
		if number == "" {
			f.Why = manga.StayUnread
			plan.Files = append(plan.Files, f)
			return
		}
		to, err := folderOf(number, file)
		if err != nil {
			f.Why = manga.StayUnread
			plan.Files = append(plan.Files, f)
			return
		}
		f.To = to
		switch first, twice := claimed[number]; {
		case twice && manga.SameFile(first, file):
			f.Why = manga.StayIdentical
		case twice:
			f.Why = manga.StayCopy
		case have[number].Status == manga.StatusDownloaded:
			f.Why = manga.StayExists
		default:
			if _, err := os.Lstat(to); err == nil {
				f.Why = manga.StayExists
			} else {
				f.Action = "move"
				claimed[number] = file
			}
		}
		plan.Files = append(plan.Files, f)
	}
	for _, is := range issues {
		number := ""
		if manga.PlainNumber(is.Number) {
			number = manga.NormalNumber(is.Number)
		}
		if is.FilePath == "" || is.Status != comics.StatusDownloaded {
			if number != "" {
				plan.Issues = append(plan.Issues, manga.MoveIssue{IssueID: is.ID, Number: number, Wanted: is.Wanted})
				numbers[number] = true
			}
			continue
		}
		owned[is.FilePath] = true
		if number == "" {
			if n := manga.ParseFor(filepath.Base(is.FilePath), names); n.Kind == manga.KindVolume {
				number = n.Number
			}
		}
		if number != "" {
			numbers[number] = true
		}
		consider(is.ID, number, is.FilePath)
	}
	// Files in the series' folder that no issue points at: the duplicates, mostly.
	root := filepath.Clean(s.comicsRoot())
	if sr.Folder != "" && filepath.Clean(sr.Folder) != root && filepath.Clean(sr.Folder) != "." {
		for _, file := range mangaFiles(sr.Folder) {
			if owned[file] {
				continue
			}
			number := ""
			if n := manga.ParseFor(filepath.Base(file), names); n.Kind == manga.KindVolume || n.Kind == manga.KindBare {
				number = n.Number
			}
			if number != "" {
				numbers[number] = true
			}
			consider(0, number, file)
		}
	}
	plan.Volumes = len(numbers)
	return plan, nil
}

// POST /api/manga/move/preview {"comicSeriesId", "anilistId" or "title"}: every new
// file name, for you to look at. Nothing changes.
func (s *Server) handleMangaMovePreview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ComicSeriesID int64  `json:"comicSeriesId"`
		AniListID     int64  `json:"anilistId"`
		Title         string `json:"title"`
	}
	if err := decodeJSON(r, &req); err != nil || req.ComicSeriesID <= 0 {
		writeError(w, http.StatusBadRequest, "Choose a comic series to bring over.")
		return
	}
	sr, err := s.comicRepo().GetSeries(req.ComicSeriesID)
	if err != nil {
		writeError(w, http.StatusNotFound, "That comic series isn't there any more.")
		return
	}
	repo := s.mangaRepo()
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = sr.Name
	}
	var into int64
	folder := ""
	if req.AniListID > 0 {
		if have, err := repo.SeriesByAniList(req.AniListID); err == nil {
			into, title, folder = have.ID, have.Title, have.Folder
		} else {
			ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
			f, err := s.mangaMod().anilist.Get(ctx, req.AniListID)
			cancel()
			if err != nil {
				writeUpstreamError(w, "read the series from AniList", err)
				return
			}
			title = f.AsSeries().Title
		}
	}
	plan, err := s.planMangaMove(sr, title, req.AniListID, into, folder)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err := repo.SaveMovePlan(&plan); err != nil {
		writeError(w, http.StatusInternalServerError, "The preview couldn't be kept.")
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

func (s *Server) mangaMoveFromPath(w http.ResponseWriter, r *http.Request) (manga.Move, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err == nil {
		if m, err := s.mangaRepo().GetMove(id); err == nil {
			return m, true
		}
	}
	writeError(w, http.StatusNotFound, "That move isn't on the list.")
	return manga.Move{}, false
}

// POST /api/manga/move/{id}/apply: do what the preview showed. It runs by itself,
// and the page follows it; moving between drives can take a while.
func (s *Server) handleMangaMoveApply(w http.ResponseWriter, r *http.Request) {
	m, ok := s.mangaMoveFromPath(w, r)
	if !ok {
		return
	}
	if m.Status != manga.MovePreview {
		writeError(w, http.StatusConflict, "That move has been done already. Look at the preview again.")
		return
	}
	st := s.mangaMod()
	if !st.move.TryLock() {
		writeError(w, http.StatusConflict, "Another move is running. Try again when it's done.")
		return
	}
	m.Status = manga.MoveApplying
	if err := s.mangaRepo().SaveMove(m); err != nil {
		st.move.Unlock()
		writeError(w, http.StatusInternalServerError, "The move couldn't start.")
		return
	}
	s.background(func() {
		defer st.move.Unlock()
		if err := s.applyMangaMove(context.Background(), &m); err != nil {
			slog.Warn("manga: move stopped", "series", m.ComicName, "err", err)
		}
	})
	writeJSON(w, http.StatusAccepted, map[string]string{"message": "Moving " + m.ComicName + " now."})
}

// applyMangaMove does a plan. After each step what's been done is written down.
func (s *Server) applyMangaMove(ctx context.Context, m *manga.Move) (err error) {
	repo := s.mangaRepo()
	creps := s.comicRepo()
	save := func() { _ = repo.SaveMove(*m) }
	defer func() {
		if err != nil {
			m.Status, m.Problem = manga.MoveStopped, err.Error()
		} else {
			m.Status = manga.MoveApplied
		}
		save()
	}()
	cs, err := creps.GetSeries(m.ComicSeriesID)
	if err != nil {
		return errors.New("that comic series isn't there any more")
	}
	m.Journal.MonitorWas = cs.MonitorNew
	sr, err := s.mangaSeriesForMove(ctx, m, cs)
	if err != nil {
		return err
	}
	m.MangaSeriesID = sr.ID
	save()
	byIssue := map[int64]comics.Issue{}
	if list, err := creps.Issues(cs.ID); err == nil {
		for _, is := range list {
			byIssue[is.ID] = is
		}
	}
	allMoved := true
	carry := func(issueID, volumeID int64) { _ = repo.CarryReading(issueID, volumeID) }
	for i := range m.Plan.Files {
		f := &m.Plan.Files[i]
		if f.Action != "move" {
			if f.IssueID > 0 {
				allMoved = false
			}
			m.Done++
			continue
		}
		if st, serr := os.Stat(f.From); serr != nil || st.Size() != f.Size {
			f.Action, f.Why = "stay", manga.StayChanged
			if f.IssueID > 0 {
				allMoved = false
			}
			m.Done++
			save()
			continue
		}
		is := byIssue[f.IssueID]
		v, isNew, verr := repo.EnsureVolume(sr.ID, f.Number, is.Wanted)
		if verr != nil {
			return verr
		}
		if merr := manga.MoveNoOverwrite(f.From, f.To); merr != nil {
			if errors.Is(merr, manga.ErrFileThere) {
				f.Action, f.Why = "stay", manga.StayExists
				if f.IssueID > 0 {
					allMoved = false
				}
				if isNew {
					_ = repo.DeleteVolume(v.ID)
				}
				m.Done++
				save()
				continue
			}
			return merr
		}
		// Written down straight away: the file has moved.
		m.Journal.Steps = append(m.Journal.Steps, manga.MoveStep{IssueID: f.IssueID, VolumeID: v.ID, NewVolume: isNew, From: f.From, To: f.To, Size: f.Size, IssueWanted: is.Wanted, IssueStatus: is.Status, IssueSize: is.FileSize})
		save()
		if err := repo.SetVolumeFile(v.ID, f.To, f.Size, string(manga.ClassOwn)); err != nil {
			return err
		}
		_ = repo.SetReleaseSize(v.ID, f.Size)
		if sr.Folder == "" {
			sr.Folder = filepath.Dir(f.To)
			_ = repo.SetFolder(sr.ID, sr.Folder)
		}
		if f.IssueID > 0 {
			if err := creps.SetState(f.IssueID, comics.StatusMissing, "", 0); err != nil {
				return err
			}
			if _, err := creps.SetWanted([]int64{f.IssueID}, false); err != nil {
				return err
			}
			carry(f.IssueID, v.ID)
		}
		m.Done++
		save()
	}
	for _, is := range m.Plan.Issues {
		v, isNew, verr := repo.EnsureVolume(sr.ID, is.Number, is.Wanted)
		if verr != nil {
			return verr
		}
		m.Journal.Steps = append(m.Journal.Steps, manga.MoveStep{IssueID: is.IssueID, VolumeID: v.ID, NewVolume: isNew, IssueWanted: is.Wanted, IssueStatus: comics.StatusMissing})
		if _, err := creps.SetWanted([]int64{is.IssueID}, false); err != nil {
			return err
		}
		carry(is.IssueID, v.ID)
		m.Done++
	}
	_ = repo.CarryReaderPrefs(cs.ID, sr.ID)
	if allMoved {
		if err := creps.HideMovedToManga(cs.ID, sr.ID); err != nil {
			return err
		}
		m.Journal.Hidden = true
	}
	return nil
}

// mangaSeriesForMove is the manga series a move goes into: one in the library, or
// a new one, from AniList when it was matched.
func (s *Server) mangaSeriesForMove(ctx context.Context, m *manga.Move, cs comics.Series) (manga.Series, error) {
	repo := s.mangaRepo()
	if m.Plan.MangaSeriesID > 0 {
		return repo.GetSeries(m.Plan.MangaSeriesID)
	}
	info := manga.Series{Title: m.Plan.Title, LicensedEN: -1, StartYear: cs.StartYear, Publisher: cs.Publisher, Description: cs.Description, MonitorNew: cs.MonitorNew}
	if cs.Type == comics.TypeWebtoon {
		info.Format = "manhwa"
	}
	var aliases []manga.Alias
	if m.Plan.AniListID > 0 {
		actx, cancel := context.WithTimeout(ctx, time.Minute)
		f, err := s.mangaMod().anilist.Get(actx, m.Plan.AniListID)
		cancel()
		if err != nil {
			return manga.Series{}, errors.New("AniList couldn't be reached, so nothing was moved. Try again in a few minutes")
		}
		info, aliases = f.AsSeries(), f.Aliases
		info.MonitorNew = cs.MonitorNew
		info.CheckedAt = time.Now().UTC().Format(time.RFC3339)
	}
	sr, err := repo.AddSeries(info, aliases)
	if err != nil {
		return manga.Series{}, err
	}
	m.MadeSeries = true
	return sr, nil
}

// POST /api/manga/move/{id}/undo: put it all back. A file that's been changed or
// moved since, or whose old name has been taken, is left and listed.
func (s *Server) handleMangaMoveUndo(w http.ResponseWriter, r *http.Request) {
	m, ok := s.mangaMoveFromPath(w, r)
	if !ok {
		return
	}
	if m.Status != manga.MoveApplied && m.Status != manga.MoveStopped {
		writeError(w, http.StatusConflict, "There's nothing to undo in that move.")
		return
	}
	st := s.mangaMod()
	if !st.move.TryLock() {
		writeError(w, http.StatusConflict, "A move is running. Try again when it's done.")
		return
	}
	m.Status = manga.MoveUndoing
	_ = s.mangaRepo().SaveMove(m)
	s.background(func() {
		defer st.move.Unlock()
		s.undoMangaMove(&m)
	})
	writeJSON(w, http.StatusAccepted, map[string]string{"message": "Putting " + m.ComicName + " back."})
}

func (s *Server) undoMangaMove(m *manga.Move) {
	repo := s.mangaRepo()
	creps := s.comicRepo()
	m.Journal.Left = nil
	kept := false // something stays in Manga, so the series does too
	for i := len(m.Journal.Steps) - 1; i >= 0; i-- {
		step := m.Journal.Steps[i]
		if step.To != "" {
			there, err := os.Stat(step.To)
			if err != nil || there.Size() != step.Size {
				m.Journal.Left = append(m.Journal.Left, step.To)
				kept = true
				continue
			}
			if err := manga.MoveNoOverwrite(step.To, step.From); err != nil {
				m.Journal.Left = append(m.Journal.Left, step.To)
				kept = true
				continue
			}
		}
		if step.IssueID > 0 {
			_ = repo.UncarryReading(step.IssueID, step.VolumeID)
			if step.To != "" {
				_ = creps.SetState(step.IssueID, step.IssueStatus, step.From, step.IssueSize)
			}
			_, _ = creps.SetWanted([]int64{step.IssueID}, step.IssueWanted)
		}
		switch {
		case step.NewVolume:
			_ = repo.DeleteVolume(step.VolumeID)
		case step.To != "":
			_ = repo.ClearVolumeFile(step.VolumeID)
		}
	}
	_ = creps.HideMovedToManga(m.ComicSeriesID, 0)
	_ = creps.SetMonitorNew(m.ComicSeriesID, m.Journal.MonitorWas)
	if m.MadeSeries && !kept {
		_ = repo.DeleteSeries(m.MangaSeriesID)
	}
	m.Journal.Steps = nil
	m.Journal.Hidden = false
	m.Status = manga.MoveUndone
	m.Done = 0
	_ = repo.SaveMove(*m)
}

// POST /api/manga/move/empty {"ids":[...]}: comic series with no files, brought over
// with one tick. There's no file to rename, so there's nothing to preview, and an
// undo is there all the same. AniList gives the details when its title is an exact
// match; otherwise the series keeps the name it had.
func (s *Server) handleMangaMoveEmpty(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []int64 `json:"ids"`
	}
	if err := decodeJSON(r, &req); err != nil || len(req.IDs) == 0 || len(req.IDs) > 200 {
		writeError(w, http.StatusBadRequest, "Tick the series to bring over.")
		return
	}
	st := s.mangaMod()
	if !st.move.TryLock() {
		writeError(w, http.StatusConflict, "A move is running. Try again when it's done.")
		return
	}
	defer st.move.Unlock()
	repo := s.mangaRepo()
	moved := 0
	for _, id := range req.IDs {
		sr, err := s.comicRepo().GetSeries(id)
		if err != nil || sr.Have > 0 {
			if err == nil {
				slog.Info("manga: a series with files isn't moved with a tick", "series", sr.Name)
			}
			continue
		}
		if list, err := s.comicRepo().Issues(id); err != nil || anyFile(list) {
			continue
		}
		title, anilistID := s.exactAniList(r.Context(), sr.Name)
		into, folder := int64(0), ""
		if anilistID > 0 {
			if have, err := repo.SeriesByAniList(anilistID); err == nil {
				into, title, folder = have.ID, have.Title, have.Folder
			}
		}
		plan, err := s.planMangaMove(sr, title, anilistID, into, folder)
		if err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		if err := repo.SaveMovePlan(&plan); err != nil {
			writeError(w, http.StatusInternalServerError, "The move couldn't be kept.")
			return
		}
		m, err := repo.GetMove(plan.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "The move couldn't be kept.")
			return
		}
		m.Status = manga.MoveApplying
		if err := s.applyMangaMove(r.Context(), &m); err == nil {
			moved++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"moved": moved, "message": "Brought over."})
}

func anyFile(list []comics.Issue) bool {
	for _, is := range list {
		if is.FilePath != "" {
			return true
		}
	}
	return false
}

// exactAniList is a name's AniList match, only when a title is the very same. It
// says nothing when AniList can't be reached.
func (s *Server) exactAniList(ctx context.Context, name string) (string, int64) {
	actx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	found, err := s.mangaMod().anilist.Search(actx, name)
	if err != nil {
		return name, 0
	}
	key := manga.Key(name)
	for _, f := range found {
		if f.IsNovel {
			continue
		}
		if manga.Key(f.Title) == key {
			return f.Title, f.AniListID
		}
		for _, a := range f.Aliases {
			if manga.Key(a.Alias) == key {
				return f.Title, f.AniListID
			}
		}
	}
	return name, 0
}
