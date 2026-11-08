// Usherr: Crews, the crew space between parties (internal/api/home_crews_http.go).
import { t } from '../i18n'

export type Has = 'yes' | 'getting' | 'no'
export type Can = 'all' | 'some' | 'none'

export interface CrewHouse {
  h: number
  name: string
  state: 'invited' | 'joined'
  people: string[]
  me: boolean
  online: boolean
}

export interface Crew {
  id: number
  name: string
  by?: string
  mine: boolean // this Usherr runs it
  state: 'invited' | 'joined'
  joined: boolean // you opted in
  myHid: number
  houses: CrewHouse[]
  canEdit: boolean
}

export interface CrewFilm {
  tmdb: number
  title: string
  year?: number
  poster?: string // a TMDB image path
}

export interface HouseHas {
  h: number
  has: Has
}

export interface Seat {
  h: number
  who: string
}

export interface ChatLine {
  id: number
  h: number
  house: string
  who: string
  text: string
  card?: CrewFilm & { note?: string; to?: number; toWho?: string }
  at: number
}

export interface ListEntry extends CrewFilm {
  by: string
  byH: number
  added: number
  have: HouseHas[]
  can: Can
  mine: boolean
}

export interface Choice extends CrewFilm {
  voters: Seat[]
  have: HouseHas[]
  can: Can
}

export interface CrewVote {
  vid: string
  hostH: number
  host: string
  closes: number
  at: number
  preshow: number
  club?: string
  state: 'open' | 'closed' | 'booked' | 'nobody'
  winner?: number
  choices: Choice[]
  myVote?: number
  showtimeId?: number
}

export interface Club {
  kid: string
  name: string
  weekday: number
  minute: number
  every: 1 | 2
  mode: 'pick' | 'vote'
  preshow: number
  by: string
  byH: number
  next: number
  rotation?: Seat[]
  turn: number
  pick?: CrewFilm
  streak: number
  nights: number
  in: boolean
  myTurn: boolean
  picker?: Seat
}

export interface ActivityLine {
  h: number
  house: string
  who: string
  kind: 'finished' | 'rated' | 'watched'
  title: string
  part?: string
  stars?: number
  at: number
}

export interface CrewSpace {
  crew: Crew
  on: boolean
  me: string
  sharing: boolean
  chat?: ChatLine[]
  list?: ListEntry[]
  votes?: CrewVote[]
  clubs?: Club[]
  activity?: ActivityLine[]
}

export interface CrewList {
  on: boolean
  crews: Crew[]
  peers: { id: number; name: string }[]
  sharing: boolean
}

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(`/api/home/crews${path}`, {
    method,
    credentials: 'include',
    cache: 'no-store',
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const data = (await res.json().catch(() => null)) as (T & { error?: string }) | null
  if (!res.ok) throw new Error(data?.error ?? t('Something went wrong (error {status}).', { status: res.status }))
  return data as T
}

export interface FilmIn {
  tmdbId: number
  title: string
  year?: number
  poster?: string
}

export const crewFilmIn = (f: CrewFilm): FilmIn => ({ tmdbId: f.tmdb, title: f.title, year: f.year, poster: f.poster })

export const crewsApi = {
  list: () => call<CrewList>('GET', ''),
  make: (name: string, peers: number[]) => call<Crew>('POST', '', { name, peers }),
  edit: (id: number, name: string, peers?: number[]) => call<Crew>('PUT', `/${id}`, { name, peers }),
  end: (id: number) => call<{ ok: boolean }>('DELETE', `/${id}`),
  space: (id: number) => call<CrewSpace>('GET', `/${id}`),
  join: (id: number) => call<Crew>('POST', `/${id}/join`),
  leave: (id: number) => call<{ ok: boolean; gone: boolean }>('POST', `/${id}/leave`),
  decline: (id: number) => call<{ ok: boolean }>('POST', `/${id}/decline`),
  chat: (id: number, text: string) => call<{ ok: boolean }>('POST', `/${id}/chat`, { text }),
  send: (id: number, film: FilmIn, to: number, toWho: string) => call<{ ok: boolean }>('POST', `/${id}/send`, { ...film, to, toWho }),
  addFilm: (id: number, film: FilmIn) => call<{ ok: boolean; sent: boolean }>('POST', `/${id}/list`, film),
  removeFilm: (id: number, tmdb: number) => call<{ ok: boolean }>('DELETE', `/${id}/list/${tmdb}`),
  openVote: (id: number, v: { films: number[]; closesMs: number; atMs: number; preshowMin: number; preshowTheme: number }) => call<{ ok: boolean; vid: string }>('POST', `/${id}/votes`, v),
  vote: (id: number, vid: string, tmdbId: number) => call<{ ok: boolean }>('PUT', `/${id}/votes/${vid}`, { tmdbId }),
  addClub: (id: number, c: { name: string; weekday: number; minute: number; every: number; mode: 'pick' | 'vote'; preshowMin: number }) => call<{ ok: boolean }>('POST', `/${id}/clubs`, c),
  club: (id: number, kid: string, op: 'join' | 'leave' | 'pick', film?: FilmIn) => call<{ ok: boolean }>('PUT', `/${id}/clubs/${kid}`, { op, ...film }),
  endClub: (id: number, kid: string) => call<{ ok: boolean }>('DELETE', `/${id}/clubs/${kid}`),
  sharing: (on: boolean) => call<{ sharing: boolean }>('PUT', '/sharing', { on }),
  wrapped: (id: number, year?: number) => call<CrewWrapped>('GET', `/${id}/wrapped${year ? `?year=${year}` : ''}`),
}

// Crew Wrapped (home_crew_wrapped.go): a crew's year in review.
export interface WrappedFilm {
  tmdbId?: number
  title: string
  poster?: string
  stars?: number // the average, out of 10
  low?: number
  high?: number
  count: number
}

export interface CrewWrapped {
  crew: string
  year: number
  years: number[]
  featured: number // the year the page offers (December and January), 0 otherwise
  empty: boolean
  parties: number
  minutes: number
  about: boolean
  houses: number
  streak: number
  weekdays: number[] // 0 = Sunday
  favourite?: WrappedFilm
  divisive?: WrappedFilm
  lowest?: WrappedFilm
  ratings: number
  starsFrom: '' | 'crew' | 'house'
  moment?: { tmdbId: number; title: string; pos: number; word: string; count: number; total: number }
  mostHa?: WrappedFilm
  mostWow?: WrappedFilm
  pickers: { who: string; house?: string; wins: number; you?: boolean }[]
  clubs: string[]
  nextYear: number
}

// wrappedFeatured is the year a crew's Wrapped is offered: this year from
// December 1, last year through January (0 the rest of the year).
export function wrappedFeatured(now = new Date()): number {
  if (now.getMonth() === 11) return now.getFullYear()
  if (now.getMonth() === 0) return now.getFullYear() - 1
  return 0
}

// posterOf is a crew film's poster address (TMDB's small size).
export const posterOf = (path?: string) => (path ? `https://image.tmdb.org/t/p/w185${path}` : '')

// crewTitle is "Alien (1979)".
export const crewTitle = (f: CrewFilm) => (f.year ? `${f.title} (${f.year})` : f.title)
