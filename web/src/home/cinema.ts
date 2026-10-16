// Usherr: films in cinemas (internal/api/home_cinema.go). One fetch, shared by
// the "In theaters now" rows and the film page.
import type { DiscoverMovie } from '../api'

export interface CinemaFilm extends DiscoverMovie {
  inLibrary?: boolean
  libraryId?: number
  status?: string
}

export interface Cinema {
  on: boolean
  region: string
  zip: string
  theatre: string
  source: 'tmdb' | 'amc'
  results: CinemaFilm[]
}

let pending: Promise<Cinema | null> | null = null
let at = 0

export function loadCinema(): Promise<Cinema | null> {
  if (pending && Date.now() - at < 10 * 60 * 1000) return pending
  at = Date.now()
  pending = fetch('/api/home/cinema', { credentials: 'include' })
    .then((r) => (r.ok ? (r.json() as Promise<Cinema>) : null))
    .catch(() => null)
  return pending
}

// whereElse opens a search for a film's showtimes near the household (Google
// lists every cinema showing it, with times and ticket links).
export function whereElseUrl(title: string, year: number | undefined, zip: string): string {
  const q = `${title}${year ? ` ${year}` : ''} showtimes${zip ? ` near ${zip}` : ''}`
  return `https://www.google.com/search?${new URLSearchParams({ q })}`
}

// theatreUrl searches for the film at the household's own theater.
export function theatreUrl(title: string, theatre: string): string {
  return `https://www.google.com/search?${new URLSearchParams({ q: `${title} showtimes ${theatre}` })}`
}
