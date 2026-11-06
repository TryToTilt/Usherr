// Usherr: Showtimes at home — a film booked for a time. The Marquee counts
// down to it and runs the pre-show; at showtime the film starts on the
// Showtime screen (/showtime), on the TV, or nowhere (watched elsewhere).
import { locale, plural, t } from '../i18n'

export type PlayOn = 'here' | 'tv' | 'none'
export interface Showtime {
  id: number
  tmdbId: number
  title: string
  year?: number
  posterUrl?: string
  runtime?: number // minutes
  atMs: number
  preshowMin: number
  playOn: PlayOn
  state: 'booked' | 'preshow' | 'playing' | 'done'
  mine: boolean
  libraryId?: number // the film's file in the library
  streamedFileId?: number // Usherr: its saved release in Streams
  // Usherr: shared as a watch party (Community), or a friend's joined by its
  // code (joined: host once found in their server; waiting: not found yet)
  party?: { going: string[] | null; cantMake: number; joined?: boolean; host?: string; waiting?: boolean; bot?: 'on' | 'off'; invite?: string; needLink?: boolean }
  // Usherr: linked screenings. Shared with linked Usherrs: how many are
  // invited, how many said they're coming, and how many people they bring.
  linked?: { invited: number; going: number; people: number }
}
export interface ShowtimeList {
  showtimes: Showtime[]
  nowMs: number // the server's clock
  tv: boolean // Play on TV is set up
  community?: boolean // Usherr: a showtime can be shared as a watch party
  linkOn?: boolean // Usherr: linked Usherrs can be invited
}

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(`/api/home/showtimes${path}`, {
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

export const listShowtimes = () => call<ShowtimeList>('GET', '')
export const bookShowtime = (b: { tmdbId: number; title: string; year?: number; posterUrl?: string; runtime?: number; atMs: number; preshowMin: number; playOn: PlayOn; preshowTheme?: number; share?: boolean }) =>
  call<{ id: number; shareError?: string }>('POST', '', b)
// Usherr: Community. Join a watch party by the code on its card in Discord:
// it books the same film for the same moment here.
export const joinShowtime = (code: string, playOn: PlayOn) => call<{ id: number; title: string; atMs: number; already?: boolean }>('POST', '/join', { code, playOn })
// Usherr: Community. Share a booked showtime as a watch party.
export const shareShowtime = async (id: number) => {
  const res = await fetch(`/api/home/showtimes/${id}/share`, { method: 'POST', credentials: 'include' })
  if (!res.ok) {
    const data = (await res.json().catch(() => null)) as { error?: string } | null
    throw new Error(data?.error ?? t('Something went wrong (error {status}).', { status: res.status }))
  }
}
export const moveShowtime = (id: number, b: { atMs: number; preshowMin: number; playOn: PlayOn }) => call<{ id: number }>('PUT', `/${id}`, b)
export const cancelShowtime = (id: number) => fetch(`/api/home/showtimes/${id}`, { method: 'DELETE', credentials: 'include' })

// Usherr: linked screenings, step 2 (internal/api/home_link_screenings.go).
export type ScreeningAnswer = 'going' | 'not' | 'maybe'
export type HasFilm = 'yes' | 'getting' | 'no'
// One linked Usherr in the host's invite picker.
export interface LinkedGuest {
  id: number // the link
  name: string
  invited: boolean
  answer?: ScreeningAnswer
  count?: number // how many from that household
  has?: HasFilm
  runtime?: number // minutes of its copy
  differentCut?: boolean
  seen: boolean // it has the screening as it is now
}
export interface LinkedAnswer {
  on: boolean // linking is on here
  peers: LinkedGuest[]
  runtime: number
}
// A screening a linked Usherr invited this one to.
export interface LinkedScreening {
  id: number
  host: string // the linked Usherr
  hostName?: string // the person hosting
  tmdbId: number
  title: string
  year?: number
  posterUrl?: string
  runtime?: number
  edition?: string
  atMs: number
  preshowMin: number
  mine?: ScreeningAnswer // this person's answer
  people: { name: string; answer: ScreeningAnswer }[] // this household
  roster: { name: string; answer?: ScreeningAnswer; count?: number }[] // who's coming, from the host
  has?: HasFilm
  myRuntime?: number
  differentCut?: boolean
}

// The summary on a booking shared with linked Usherrs.
export function linkedSummary(s: Showtime): string {
  const l = s.linked
  if (!l) return ''
  const invited = plural(l.invited, 'Shared with {n} linked Usherr', 'Shared with {n} linked Usherrs')
  if (l.going === 0) return `${invited} · ${t('nobody’s said I’m in yet')}`
  return `${invited} · ${plural(l.people, '{n} person coming', '{n} people coming')}`
}

export const linkedFor = (id: number) => call<LinkedAnswer>('GET', `/${id}/linked`)
export const inviteLinked = (id: number, peers: number[]) => call<LinkedAnswer>('PUT', `/${id}/linked`, { peers })

async function screeningsCall<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(`/api/home/screenings${path}`, {
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
export const listScreenings = () => screeningsCall<{ screenings: LinkedScreening[]; on: boolean }>('GET', '')
export const answerScreening = (id: number, answer: ScreeningAnswer) => screeningsCall<{ screenings: LinkedScreening[] }>('PUT', `/${id}/rsvp`, { answer })
export const checkScreening = (id: number) => screeningsCall<{ screenings: LinkedScreening[] }>('POST', `/${id}/check`)

// When a showtime is over: its film's length (two hours when unknown).
export const showtimeEnds = (s: Showtime) => s.atMs + (s.runtime && s.runtime > 0 ? s.runtime : 120) * 60_000

export function clockLabel(ms: number): string {
  return new Date(ms).toLocaleTimeString(locale(), { hour: 'numeric', minute: '2-digit' })
}

// "Tonight 8:00 PM", "Tomorrow 7:30 PM", "Sat 9:00 PM".
export function whenLabel(ms: number): string {
  const d = new Date(ms)
  const today = new Date()
  const days = Math.round((new Date(d.toDateString()).getTime() - new Date(today.toDateString()).getTime()) / 86_400_000)
  const time = clockLabel(ms)
  if (days === 0) return d.getHours() >= 17 ? t('Tonight {time}', { time }) : t('Today {time}', { time })
  if (days === 1) return t('Tomorrow {time}', { time })
  return `${d.toLocaleDateString(locale(), { weekday: 'short', month: 'short', day: 'numeric' })} ${time}`
}

// "in 1 h 05 min", "in 12 min", "now"; a day or more away, by the calendar:
// "tomorrow", "in 4 days" (not "in 117 h 59 min").
export function inLabel(ms: number, now: number): string {
  const m = Math.round((ms - now) / 60_000)
  if (m <= 0) return t('now')
  if (m < 60) return t('in {n} min', { n: m })
  if (m < 24 * 60) return t('in {h} h {m} min', { h: Math.floor(m / 60), m: String(m % 60).padStart(2, '0') })
  const days = Math.round((new Date(new Date(ms).toDateString()).getTime() - new Date(new Date(now).toDateString()).getTime()) / 86_400_000)
  if (days <= 1) return t('tomorrow')
  return plural(days, 'in {n} day', 'in {n} days')
}

export const PLAY_ON_LABEL: Record<PlayOn, () => string> = {
  here: () => t('On the Showtime screen'),
  tv: () => t('On the TV'),
  none: () => t('I’ll start it myself'),
}
