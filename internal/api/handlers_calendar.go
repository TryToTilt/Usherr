package api

import (
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/rdborg/mediarium/internal/books"
	"github.com/rdborg/mediarium/internal/metadata"
)

type calendarEntryPayload struct {
	Kind        string `json:"kind"` // "movie", "episode", "album" (music module) or "book" (ebooks and audiobooks)
	ID          int64  `json:"id"`
	MovieID     int64  `json:"movieId,omitempty"`
	TMDBID      int    `json:"tmdbId,omitempty"` // movies: the movie page is addressed by TMDB id
	SeriesID    int64  `json:"seriesId,omitempty"`
	AlbumID     int64  `json:"albumId,omitempty"`  // albums
	ArtistID    int64  `json:"artistId,omitempty"` // albums
	BookID      int64  `json:"bookId,omitempty"`   // books
	Season      int    `json:"season,omitempty"`
	Episode     int    `json:"episode,omitempty"`
	Title       string `json:"title"`
	Subtitle    string `json:"subtitle,omitempty"`
	ReleaseDate string `json:"releaseDate"`
	Status      string `json:"status"`
	PosterURL   string `json:"posterUrl,omitempty"` // the poster or cover the calendar shows next to the title
	// home fork: a book's wanted formats ("ebook", "audiobook"), and a comic
	// issue's series in the library.
	Formats       []string `json:"formats,omitempty"`
	ComicSeriesID int64    `json:"comicSeriesId,omitempty"`
	// BackdropURL and LogoURL are the wide still and the title logo of a movie or
	// show, for the Backdrops view. They appear once they have been fetched in
	// the background (see artwork.go). For an episode, TMDBID is the show's.
	BackdropURL string `json:"backdropUrl,omitempty"`
	LogoURL     string `json:"logoUrl,omitempty"`
	Anime       bool   `json:"anime,omitempty"` // Usherr: a streamed show that is anime
}

const (
	calendarEpisodeLookback  = 30 * 24 * time.Hour
	calendarEpisodeLookahead = 120 * 24 * time.Hour
)

// handleCalendar is the unified calendar (releases and
// episode airs in one list). Monitored movies with a known release date are
// included from 30 days back on, and episodes only within a window around
// today, so a long-running show's whole back catalogue and a big movie
// library's old releases don't drown out what's coming up.
func (s *Server) handleCalendar(w http.ResponseWriter, r *http.Request) {
	entries, err := s.calendarEntries()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Album and book releases, while their modules are on (the dashboard's
	// "coming up" list stays movies and episodes).
	more, err := s.moduleCalendarEntries()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	more = append(more, s.cinemaCalendarEntries(r.Context())...) // Usherr: cinema openings
	more = append(more, s.streamedCalendarEntries()...)          // Usherr: streamed shows' episodes
	if len(more) > 0 {
		entries = append(entries, more...)
		sort.SliceStable(entries, func(i, j int) bool { return entries[i].ReleaseDate < entries[j].ReleaseDate })
	}
	writeJSON(w, http.StatusOK, entries)
}

// moduleCalendarEntries are the album and book releases on the calendar.
func (s *Server) moduleCalendarEntries() ([]calendarEntryPayload, error) {
	albums, err := s.albumCalendarEntries()
	if err != nil {
		return nil, err
	}
	bookEntries, err := s.bookCalendarEntries()
	if err != nil {
		return nil, err
	}
	out := append(albums, bookEntries...)
	return append(out, s.comicCalendarEntries()...), nil // home fork: comics too
}

// bookCalendarEntries lists the library's books with a release date in the
// calendar's window (30 days back, 120 ahead), while ebooks or audiobooks are
// on. Release dates come from Hardcover (see books_series.go).
func (s *Server) bookCalendarEntries() ([]calendarEntryPayload, error) {
	if !s.booksEnabled() {
		return nil, nil
	}
	now := time.Now().UTC()
	from := now.Add(-calendarEpisodeLookback).Format("2006-01-02")
	to := now.Add(calendarEpisodeLookahead).Format("2006-01-02")
	list, err := s.BookRepo.ListReleasing(from, to)
	if err != nil {
		return nil, err
	}
	var out []calendarEntryPayload
	for _, b := range list {
		subtitle := bookFormatsLabel(b)
		if b.SeriesName != "" && b.SeriesPosition != "" {
			subtitle = fmt.Sprintf("Book %s of %s · %s", b.SeriesPosition, b.SeriesName, subtitle)
		}
		title := b.Title
		if b.Author != "" {
			title = b.Author + " — " + b.Title
		}
		out = append(out, calendarEntryPayload{Kind: "book", ID: b.ID, BookID: b.ID, Title: title, Subtitle: subtitle, ReleaseDate: b.ReleaseDate, Status: bookCalendarStatus(b), PosterURL: b.CoverPath("M"), Formats: bookFormats(b)})
	}
	return out, nil
}

// bookFormatsLabel says which formats of a book are wanted.
func bookFormatsLabel(b books.Book) string {
	switch {
	case b.WantEbook && b.WantAudiobook:
		return "Ebook and audiobook"
	case b.WantAudiobook:
		return "Audiobook"
	default:
		return "Ebook"
	}
}

// bookCalendarStatus is "downloaded" when every wanted format is there,
// "downloading" while one is on its way, else "missing".
func bookCalendarStatus(b books.Book) string {
	all, busy := true, false
	for _, f := range []books.Format{books.Ebook, books.Audiobook} {
		if !b.Wants(f) {
			continue
		}
		switch b.Status(f) {
		case books.StatusDownloaded:
		case books.StatusDownloading:
			busy, all = true, false
		default:
			all = false
		}
	}
	switch {
	case all:
		return "downloaded"
	case busy:
		return "downloading"
	}
	return "missing"
}

// albumCalendarEntries lists the monitored albums that
// come out (or came out) within the same window as episodes: 30 days back,
// 120 ahead. Only albums with a full release date can be placed on a day;
// nothing is listed while the music module is off.
func (s *Server) albumCalendarEntries() ([]calendarEntryPayload, error) {
	if !s.musicEnabled() {
		return nil, nil
	}
	now := time.Now().UTC()
	from := now.Add(-calendarEpisodeLookback).Format("2006-01-02")
	to := now.Add(calendarEpisodeLookahead).Format("2006-01-02")
	albums, err := s.MusicRepo.ListMonitoredReleasing(from, to)
	if err != nil {
		return nil, err
	}
	var out []calendarEntryPayload
	for _, al := range albums {
		name := al.ArtistName
		subtitle := "Album"
		switch al.Type {
		case "ep":
			subtitle = "EP"
		case "single":
			subtitle = "Single"
		}
		out = append(out, calendarEntryPayload{
			Kind: "album", ID: al.ID, AlbumID: al.ID, ArtistID: al.ArtistID, Title: name + " — " + al.Title, Subtitle: subtitle,
			ReleaseDate: al.ReleaseDate, Status: string(al.Status), PosterURL: albumCoverURL(al.ID),
		})
	}
	return out, nil
}

// calendarEntries builds the merged, date-ordered movie-release and
// episode-air list shared by the calendar page and the dashboard.
func (s *Server) calendarEntries() ([]calendarEntryPayload, error) {
	return s.calendarEntriesSince("")
}

// calendarEntriesSince is calendarEntries with everything before the date
// since ("YYYY-MM-DD", or "" for no limit) left out. The dashboard only wants
// what is still to come, and asking the database for just that is much
// cheaper than building the whole list and cutting it down.
func (s *Server) calendarEntriesSince(since string) ([]calendarEntryPayload, error) {
	now := time.Now().UTC()
	from := now.Add(-calendarEpisodeLookback).Format("2006-01-02")
	to := now.Add(calendarEpisodeLookahead).Format("2006-01-02")
	if since > from {
		from = since
	}
	// Movies start at the same date as episodes: a library of thousands of
	// films would otherwise put every old release in each calendar answer.
	// Releases further ahead are all kept.
	movies, err := s.MovieRepo.CalendarMovies(from)
	if err != nil {
		return nil, err
	}

	entries := make([]calendarEntryPayload, 0, len(movies)) // never nil: a nil slice encodes as JSON null, which the frontend would confuse with "still loading"
	for _, m := range movies {
		entries = append(entries, calendarEntryPayload{
			Kind: "movie", ID: m.ID, MovieID: m.ID, TMDBID: m.TMDBID, Title: m.Title, ReleaseDate: m.ReleaseDate, Status: string(m.Status),
			PosterURL: metadata.PosterURL(m.PosterPath),
		})
	}

	eps, err := s.MovieRepo.CalendarEpisodes(from, to)
	if err != nil {
		return nil, err
	}
	for _, ep := range eps {
		label := fmt.Sprintf("S%02dE%02d", ep.Season, ep.Episode)
		if ep.Title != "" {
			label += " · " + ep.Title
		}
		entries = append(entries, calendarEntryPayload{
			Kind: "episode", ID: ep.ID, SeriesID: ep.SeriesID, Season: ep.Season, Episode: ep.Episode,
			Title: ep.SeriesName, Subtitle: label, ReleaseDate: ep.AirDate, Status: string(ep.Status),
			PosterURL: metadata.PosterURL(ep.PosterPath), TMDBID: ep.SeriesTMDB,
		})
	}
	s.attachArt(entries)

	sort.SliceStable(entries, func(i, j int) bool { return entries[i].ReleaseDate < entries[j].ReleaseDate })
	return entries, nil
}
