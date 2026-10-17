package api

// Home fork: bring a Whisparr (v3) library over into NSFW. Read-only on
// Whisparr's side (plain GETs). Files are registered where they are; nothing
// is moved, renamed or deleted.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/nsfw"
)

type whisparrImage struct {
	CoverType string `json:"coverType"`
	RemoteURL string `json:"remoteUrl"`
}

type whisparrVideo struct {
	Title               string          `json:"title"`
	Code                string          `json:"code"`
	Overview            string          `json:"overview"`
	ReleaseDate         string          `json:"releaseDate"`
	Runtime             int             `json:"runtime"` // minutes
	StudioTitle         string          `json:"studioTitle"`
	StudioForeignID     string          `json:"studioForeignId"`
	Path                string          `json:"path"`
	HasFile             bool            `json:"hasFile"`
	Monitored           bool            `json:"monitored"`
	StashID             string          `json:"stashId"`
	ItemType            string          `json:"itemType"`
	Images              []whisparrImage `json:"images"`
	PerformerForeignIDs []string        `json:"performerForeignIds"`
	PerformerNames      []string        `json:"performerNames"`
	MovieFile           *struct {
		RelativePath string `json:"relativePath"`
		Quality      struct {
			Quality struct {
				Resolution int `json:"resolution"`
			} `json:"quality"`
		} `json:"quality"`
	} `json:"movieFile"`
}

type whisparrStudio struct {
	Title     string          `json:"title"`
	ForeignID string          `json:"foreignId"`
	Monitored bool            `json:"monitored"`
	Aliases   []string        `json:"aliases"`
	Network   string          `json:"network"`
	Images    []whisparrImage `json:"images"`
}

type whisparrPerformer struct {
	Name      string          `json:"name"`
	ForeignID string          `json:"foreignId"`
	Monitored bool            `json:"monitored"`
	Aliases   []string        `json:"aliases"`
	Images    []whisparrImage `json:"images"`
}

// whisparrReport is what an import found (dryRun) or did.
type whisparrReport struct {
	DryRun             bool     `json:"dryRun"`
	Nsfw               int      `json:"nsfw"`
	NsfwAdded          int      `json:"nsfwAdded"`
	NsfwUpdated        int      `json:"nsfwUpdated"`
	Files              int      `json:"files"`
	FilesFound         int      `json:"filesFound"`
	FilesMissing       []string `json:"filesMissing"`
	SkippedNoStashID   int      `json:"skippedNoStashId"`
	SkippedMovies      int      `json:"skippedMovies"`
	StudiosFollowed    []string `json:"studiosFollowed"`
	PerformersFollowed []string `json:"performersFollowed"`
	PerformersKnown    int      `json:"performersKnown"`
}

func whisparrGet(ctx context.Context, base, key, p string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/api/v3/"+p, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Api-Key", key)
	resp, err := (&http.Client{Timeout: 2 * time.Minute}).Do(req)
	if err != nil {
		return fmt.Errorf("couldn't reach Whisparr at %s", base)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return errors.New("Whisparr didn't accept the API key (Whisparr: Settings > General > API Key)")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Whisparr answered %d for %s", resp.StatusCode, p)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 256<<20))
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

func coverOf(imgs []whisparrImage) string {
	for _, want := range []string{"screenshot", "fanart", "poster", "headshot", "logo"} {
		for _, im := range imgs {
			if strings.EqualFold(im.CoverType, want) && strings.HasPrefix(im.RemoteURL, "http") {
				return im.RemoteURL
			}
		}
	}
	for _, im := range imgs {
		if strings.HasPrefix(im.RemoteURL, "http") {
			return im.RemoteURL
		}
	}
	return ""
}

// mapWhisparrPath turns Whisparr's path into Mediarium's, swapping the
// start: with from "/movies" and to "/data/nsfw", "/movies/a/b.mp4"
// becomes "/data/nsfw/a/b.mp4".
func mapWhisparrPath(p, from, to string) string {
	from = strings.TrimRight(from, "/")
	if from == "" || (p != from && !strings.HasPrefix(p, from+"/")) {
		return p
	}
	return path.Join(to, strings.TrimPrefix(p, from))
}

func (s *Server) importWhisparr(ctx context.Context, base, key, from, to string, dryRun bool) (whisparrReport, error) {
	rep := whisparrReport{DryRun: dryRun, FilesMissing: []string{}, StudiosFollowed: []string{}, PerformersFollowed: []string{}}
	var list []whisparrVideo
	if err := whisparrGet(ctx, base, key, "movie", &list); err != nil {
		return rep, err
	}
	var studios []whisparrStudio
	if err := whisparrGet(ctx, base, key, "studio", &studios); err != nil {
		return rep, err
	}
	var performers []whisparrPerformer
	if err := whisparrGet(ctx, base, key, "performer", &performers); err != nil {
		return rep, err
	}
	repo := s.videoRepo()
	aliases := map[string][]string{}
	for _, st := range studios {
		aliases[st.ForeignID] = st.Aliases
	}

	for _, w := range list {
		if w.ItemType != "" && w.ItemType != "scene" {
			rep.SkippedMovies++
			continue
		}
		if w.StashID == "" {
			rep.SkippedNoStashID++
			continue
		}
		rep.Nsfw++
		sc := nsfw.Video{
			StashID: w.StashID, Title: w.Title, Code: w.Code, Details: w.Overview, Duration: w.Runtime * 60,
			StudioStashID: w.StudioForeignID, StudioName: w.StudioTitle, StudioAliases: aliases[w.StudioForeignID],
			ImageURL: coverOf(w.Images), Wanted: w.Monitored, AddedBy: nsfw.AddedImport, Performers: []nsfw.Credit{},
		}
		if len(w.ReleaseDate) >= 10 {
			sc.ReleaseDate = w.ReleaseDate[:10]
		}
		for i, id := range w.PerformerForeignIDs {
			name := ""
			if i < len(w.PerformerNames) {
				name = w.PerformerNames[i]
			}
			sc.Performers = append(sc.Performers, nsfw.Credit{StashID: id, Name: name})
		}
		file, quality := "", ""
		if w.HasFile && w.MovieFile != nil && w.MovieFile.RelativePath != "" {
			rep.Files++
			file = mapWhisparrPath(path.Join(w.Path, w.MovieFile.RelativePath), from, to)
			if _, err := os.Stat(file); err == nil {
				rep.FilesFound++
			} else {
				if len(rep.FilesMissing) < 20 {
					rep.FilesMissing = append(rep.FilesMissing, file)
				}
				file = ""
			}
			if r := w.MovieFile.Quality.Quality.Resolution; r > 0 {
				quality = fmt.Sprintf("%dp", r)
			}
		}
		if dryRun {
			if _, err := repo.GetVideoByStashID(sc.StashID); err == nil {
				rep.NsfwUpdated++
			} else {
				rep.NsfwAdded++
			}
			continue
		}
		saved, added, err := repo.AddVideo(sc)
		if err != nil {
			return rep, err
		}
		if added {
			rep.NsfwAdded++
		} else {
			rep.NsfwUpdated++
		}
		if file != "" && saved.Status != nsfw.StatusDownloading {
			_ = repo.SetState(saved.ID, nsfw.StatusDownloaded, file, quality)
		}
	}

	for _, st := range studios {
		if !st.Monitored || st.ForeignID == "" {
			continue
		}
		rep.StudiosFollowed = append(rep.StudiosFollowed, st.Title)
		if !dryRun {
			saved, err := repo.SaveStudio(nsfw.Studio{StashID: st.ForeignID, Name: st.Title, Aliases: st.Aliases, ParentName: st.Network, ImageURL: coverOf(st.Images)}, true)
			if err == nil {
				// Whisparr already brought the back catalogue: look only for new ones from here.
				_ = repo.StudioChecked(saved.ID)
			}
		}
	}
	for _, p := range performers {
		if p.ForeignID == "" {
			continue
		}
		if p.Monitored {
			rep.PerformersFollowed = append(rep.PerformersFollowed, p.Name)
		} else {
			rep.PerformersKnown++
		}
		if !dryRun {
			saved, err := repo.SavePerformer(nsfw.Performer{StashID: p.ForeignID, Name: p.Name, Aliases: p.Aliases, ImageURL: coverOf(p.Images)}, p.Monitored)
			if err == nil && p.Monitored {
				_ = repo.PerformerChecked(saved.ID)
			}
		}
	}
	return rep, nil
}

// POST /api/nsfw/import/whisparr {"url","apiKey","from","to","dryRun"}
func (s *Server) handleImportWhisparr(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL    string `json:"url"`
		APIKey string `json:"apiKey"`
		From   string `json:"from"`
		To     string `json:"to"`
		DryRun bool   `json:"dryRun"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	u, err := url.Parse(strings.TrimSpace(req.URL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		writeError(w, http.StatusBadRequest, "Whisparr's address must look like http://host.docker.internal:6969.")
		return
	}
	key := strings.TrimSpace(req.APIKey)
	if key == "" || strings.ContainsAny(key, " \r\n\t") {
		writeError(w, http.StatusBadRequest, "Paste Whisparr's API key (Whisparr: Settings > General > API Key).")
		return
	}
	to := strings.TrimSpace(req.To)
	if to == "" {
		to = s.nsfwRoot()
	}
	if to == "" {
		writeError(w, http.StatusBadRequest, "Choose the NSFW folder first.")
		return
	}
	from := strings.TrimSpace(req.From)
	if from == "" {
		from = "/movies"
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	rep, err := s.importWhisparr(ctx, u.String(), key, from, to, req.DryRun)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rep)
}
