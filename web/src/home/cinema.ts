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
  logos?: Record<string, boolean> // services with a picture of the household's own on their button
  tickets?: TicketService
  ticketsUrl?: string
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

// Usherr: where Tickets goes. A ticket service (US), a link of the
// household's own ({title} and {zip} filled in), or none. Fandango is the
// default in the US. Links only: nothing is read from these sites.
export type TicketService = '' | 'fandango' | 'atom' | 'cinemark' | 'moviefone' | 'showtimes' | 'custom' | 'off'

const q = (params: Record<string, string>) => new URLSearchParams(params).toString()
export const TICKET_SITES: Record<Exclude<TicketService, '' | 'custom' | 'off'>, { name: string; film: (title: string) => string }> = {
  fandango: { name: 'Fandango', film: (title) => `https://www.fandango.com/search?${q({ q: title, mode: 'all' })}` },
  atom: { name: 'Atom Tickets', film: (title) => `https://www.atomtickets.com/search?${q({ query: title })}` },
  cinemark: { name: 'Cinemark', film: (title) => `https://www.cinemark.com/search?${q({ query: title })}` },
  moviefone: { name: 'Moviefone', film: (title) => `https://www.moviefone.com/search/?${q({ q: title })}` },
  showtimes: { name: 'Showtimes.com', film: (title) => `https://www.showtimes.com/search?${q({ query: title })}` },
}

function serviceOf(c: Pick<Cinema, 'region' | 'tickets'>): TicketService {
  if (c.tickets) return c.tickets
  return c.region.toUpperCase() === 'US' ? 'fandango' : 'off'
}

// ticketsFor is the Tickets link for a film, with the service's name, or
// nothing when none is set.
export function ticketsFor(c: Pick<Cinema, 'region' | 'zip' | 'tickets' | 'ticketsUrl'>, title: string): { service: TicketService; name: string; url: string } | null {
  const service = serviceOf(c)
  if (service === 'off' || service === '') return null
  if (service === 'custom') {
    const tpl = (c.ticketsUrl ?? '').trim()
    if (!/^https?:\/\//.test(tpl)) return null
    return { service, name: '', url: tpl.replaceAll('{title}', encodeURIComponent(title)).replaceAll('{zip}', encodeURIComponent(c.zip.trim())) }
  }
  const site = TICKET_SITES[service]
  return { service, name: site.name, url: site.film(title) }
}

// nearUrl is every showtime near the household's zip, on Fandango (the one
// service with a page for that), for a US zip code.
export function nearUrl(c: Pick<Cinema, 'region' | 'zip' | 'tickets'>): string | null {
  if (serviceOf(c) !== 'fandango' || !/^\d{5}$/.test(c.zip.trim())) return null
  return `https://www.fandango.com/${encodeURIComponent(c.zip.trim())}_movietimes`
}
