package tv

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// DBLibrary reads shows, episodes and movies straight from Mediarium's
// library tables. Only items with a file are offered.
type DBLibrary struct{ DB *sql.DB }

func (l DBLibrary) Episodes(seriesID int64) ([]Item, error) {
	// Usherr: with the air date (premieres) and the age the show suits (family hours).
	rows, err := l.DB.Query(`SELECT e.id, s.title, e.season, e.episode, e.title, e.file_path, COALESCE(e.air_date, ''),
			COALESCE((SELECT age FROM home_family_ages fa WHERE fa.kind = 'tv' AND fa.tmdb_id = s.tmdb_id), 0), COALESCE(s.genres, '')
		FROM episodes e JOIN series s ON s.id = e.series_id
		WHERE e.series_id = ? AND e.season > 0 AND e.file_path <> ''
		ORDER BY e.season, e.episode`, seriesID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Item
	seen := map[string]bool{} // a multi-episode file plays once
	for rows.Next() {
		var id int64
		var show, title, path, aired, genres string
		var season, episode, age int
		if err := rows.Scan(&id, &show, &season, &episode, &title, &path, &aired, &age, &genres); err != nil {
			return nil, err
		}
		if seen[path] {
			continue
		}
		seen[path] = true
		sub := fmt.Sprintf("S%02dE%02d", season, episode)
		if strings.TrimSpace(title) != "" {
			sub += " · " + title
		}
		out = append(out, Item{Kind: "episode", ID: id, SeriesID: seriesID, Title: show, Subtitle: sub, Path: path, AirDate: aired, Age: ageOr(age, genres)})
	}
	return out, rows.Err()
}

func matches(genresJSON string, year int, genres []string, decades []int) bool {
	if len(genres) > 0 {
		var g []string
		_ = json.Unmarshal([]byte(genresJSON), &g)
		ok := false
		for _, want := range genres {
			for _, have := range g {
				if strings.EqualFold(want, have) {
					ok = true
				}
			}
		}
		if !ok {
			return false
		}
	}
	if len(decades) > 0 {
		ok := false
		for _, d := range decades {
			if year >= d && year < d+10 {
				ok = true
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

func (l DBLibrary) SeriesMatching(genres []string, decades []int) ([]int64, error) {
	rows, err := l.DB.Query(`SELECT id, COALESCE(genres, ''), year FROM series ORDER BY title`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		var g string
		var year int
		if err := rows.Scan(&id, &g, &year); err != nil {
			return nil, err
		}
		if matches(g, year, genres, decades) {
			out = append(out, id)
		}
	}
	return out, rows.Err()
}

func (l DBLibrary) Movies(ids []int64, all bool, genres []string, decades []int) ([]Item, error) {
	want := map[int64]bool{}
	for _, id := range ids {
		want[id] = true
	}
	if len(want) == 0 && !all {
		return nil, nil
	}
	rows, err := l.DB.Query(`SELECT id, title, year, COALESCE(genres, ''), file_path,
			COALESCE((SELECT age FROM home_family_ages fa WHERE fa.kind = 'movie' AND fa.tmdb_id = movies.tmdb_id), 0)
		FROM movies WHERE file_path <> '' ORDER BY title`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Item
	for rows.Next() {
		var id int64
		var title, g, path string
		var year, age int
		if err := rows.Scan(&id, &title, &year, &g, &path, &age); err != nil {
			return nil, err
		}
		if want[id] || (all && matches(g, year, genres, decades)) {
			sub := ""
			if year > 0 {
				sub = strconv.Itoa(year)
			}
			out = append(out, Item{Kind: "movie", ID: id, Title: title, Subtitle: sub, Path: path, Age: ageOr(age, g)})
		}
	}
	return out, rows.Err()
}

// FFProbe measures a file with ffprobe, remembering the answer.
func FFProbe(repo *Repo) Prober {
	return func(it Item) (time.Duration, error) {
		fi, err := os.Stat(it.Path)
		if err != nil {
			return 0, err
		}
		if secs, ok := repo.Duration(it.Path, fi.Size()); ok {
			return time.Duration(secs * float64(time.Second)), nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-show_entries", "format=duration",
			"-of", "default=noprint_wrappers=1:nokey=1", it.Path).Output()
		if err != nil {
			return 0, fmt.Errorf("ffprobe %s: %w", it.Path, err)
		}
		secs, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
		if err != nil || secs <= 0 {
			return 0, fmt.Errorf("ffprobe %s: no duration", it.Path)
		}
		repo.SetDuration(it.Path, fi.Size(), secs)
		return time.Duration(secs * float64(time.Second)), nil
	}
}

// Horizon is how far ahead schedules are laid out.
const Horizon = 72 * time.Hour

// Extend tops a channel's schedule up to now+Horizon, carrying on from where
// it ends (or from now, for a new or long-idle channel).
func Extend(repo *Repo, lib Library, probe Prober, ch Channel, now time.Time) (int, error) {
	if !ch.Enabled || ch.Kind != "library" {
		return 0, nil
	}
	from, err := repo.LastEnd(ch.ID)
	if err != nil {
		return 0, err
	}
	if from.Before(now) {
		from = now.Truncate(time.Minute)
	}
	until := now.Add(Horizon)
	if !from.Before(until) {
		return 0, nil
	}
	st, err := repo.State(ch.ID)
	if err != nil {
		return 0, err
	}
	rnd := rand.New(rand.NewSource(now.UnixNano() ^ ch.ID))
	list, err := Build(ch, lib, probe, &st, from, until, rnd)
	if len(list) > 0 {
		if aerr := repo.Append(list); aerr != nil {
			return 0, aerr
		}
		if serr := repo.SaveState(ch.ID, st); serr != nil {
			return len(list), serr
		}
	}
	return len(list), err
}

// ageOr is a title's age, or, when not worked out yet, 17 for horror (left
// out of family hours) and 0 (not known: allowed) for the rest.
func ageOr(age int, genresJSON string) int {
	if age > 0 {
		return age
	}
	if strings.Contains(strings.ToLower(genresJSON), "horror") {
		return 17
	}
	return 0
}
