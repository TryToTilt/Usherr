package api

// Usherr: a channel's look (tv.Rules.Look), worked out for what is on now,
// so older shows can look like old TV and newer ones play clean.

import (
	"context"
	"database/sql"
	"regexp"
	"strconv"

	"github.com/rdborg/mediarium/internal/tv"
)

// lookOldBefore is the year from which a show plays clean under "age".
const lookOldBefore = 2000

// yearIn finds a year (or a decade, "1980s") in a filler video's path.
var yearIn = regexp.MustCompile(`(?:^|\D)(19[2-9]\d|20[0-4]\d)(?:\D|$)`)

// tvLook is "vintage", "hd" or "" (the viewer's own setting) for p on ch.
func (s *Server) tvLook(ctx context.Context, ch tv.Channel, p tv.Program) string {
	switch ch.Rules.Look {
	case "vintage", "hd":
		return ch.Rules.Look
	case "age":
		y := s.programYear(ctx, p)
		switch {
		case y <= 0:
			return ""
		case y < lookOldBefore:
			return "vintage"
		default:
			return "hd"
		}
	}
	return ""
}

// programYear is the year p first aired or came out, or 0 when unknown.
func (s *Server) programYear(ctx context.Context, p tv.Program) int {
	var date sql.NullString
	switch p.Kind {
	case "movie":
		_ = s.db.QueryRowContext(ctx, `SELECT release_date FROM movies WHERE id = ?`, p.ItemID).Scan(&date)
	case "episode":
		_ = s.db.QueryRowContext(ctx, `SELECT air_date FROM episodes WHERE id = ?`, p.ItemID).Scan(&date)
	case "stream":
		if id, season, ep, ok := tv.ParseStreamPath(p.FilePath); ok && s.TMDB() != nil {
			if eps, err := s.TMDB().GetSeason(ctx, int(id), season); err == nil {
				for _, e := range eps {
					if e.Episode == ep {
						date.String = e.AirDate
					}
				}
			}
		}
	case "filler":
		if m := yearIn.FindAllStringSubmatch(p.FilePath, -1); len(m) > 0 {
			date.String = m[len(m)-1][1]
		}
	}
	if len(date.String) < 4 {
		return 0
	}
	y, _ := strconv.Atoi(date.String[:4])
	return y
}
