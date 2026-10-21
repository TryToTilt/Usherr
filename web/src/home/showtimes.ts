// Usherr: Showtimes at home — a film booked for a time. The Marquee counts
// down to it and runs the pre-show; at showtime the film starts on the
// Showtime screen (/showtime), on the TV, or nowhere (watched elsewhere).
import { locale, t } from '../i18n'

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
}
export interface ShowtimeList {
  showtimes: Showtime[]
  nowMs: number // the server's clock
  tv: boolean // Play on TV is set up
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
export const bookShowtime = (b: { tmdbId: number; title: string; year?: number; posterUrl?: string; runtime?: number; atMs: number; preshowMin: number; playOn: PlayOn }) =>
  call<{ id: number }>('POST', '', b)
export const moveShowtime = (id: number, b: { atMs: number; preshowMin: number; playOn: PlayOn }) => call<{ id: number }>('PUT', `/${id}`, b)
export const cancelShowtime = (id: number) => fetch(`/api/home/showtimes/${id}`, { method: 'DELETE', credentials: 'include' })

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

// "in 1 h 05 min", "in 12 min", "now".
export function inLabel(ms: number, now: number): string {
  const m = Math.round((ms - now) / 60_000)
  if (m <= 0) return t('now')
  if (m < 60) return t('in {n} min', { n: m })
  return t('in {h} h {m} min', { h: Math.floor(m / 60), m: String(m % 60).padStart(2, '0') })
}

export const PLAY_ON_LABEL: Record<PlayOn, () => string> = {
  here: () => t('On the Showtime screen'),
  tv: () => t('On the TV'),
  none: () => t('I’ll start it myself'),
}
