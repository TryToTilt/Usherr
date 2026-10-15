package api

// Home fork: the calendar's comics (issues of the series in the library, by
// their on-sale date) and the formats a book is wanted in.

import (
	"time"

	"github.com/rdborg/mediarium/internal/books"
)

// bookFormats is "ebook" and/or "audiobook", as wanted.
func bookFormats(b books.Book) []string {
	var f []string
	if b.WantEbook {
		f = append(f, "ebook")
	}
	if b.WantAudiobook {
		f = append(f, "audiobook")
	}
	return f
}

// comicCalendarEntries are the issues of library comic series on sale in the
// calendar's window (30 days back, 120 ahead).
func (s *Server) comicCalendarEntries() []calendarEntryPayload {
	if !s.comicsEnabled() {
		return nil
	}
	now := time.Now()
	from := now.Add(-calendarEpisodeLookback).Format("2006-01-02")
	to := now.Add(calendarEpisodeLookahead).Format("2006-01-02")
	rows, err := s.db.Query(`SELECT i.id, i.series_id, i.number, i.title, i.store_date, i.status, i.image_url, s.name
		FROM comic_issues i JOIN comic_series s ON s.id = i.series_id
		WHERE i.store_date >= ? AND i.store_date <= ? ORDER BY i.store_date`, from, to)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []calendarEntryPayload
	for rows.Next() {
		var id, series int64
		var number, title, date, status, image, name string
		if rows.Scan(&id, &series, &number, &title, &date, &status, &image, &name) != nil {
			continue
		}
		sub := "#" + number
		if title != "" {
			sub += " · " + title
		}
		st := "missing"
		switch status {
		case "downloaded", "downloading":
			st = status
		}
		out = append(out, calendarEntryPayload{Kind: "comic", ID: id, ComicSeriesID: series, Title: name, Subtitle: sub, ReleaseDate: date, Status: st, PosterURL: s.comicImage(image)})
	}
	return out
}
