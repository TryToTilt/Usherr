// Usherr: the pre-show (home_preshow.go): clips, themes, schedules, and the
// plan for one film.
import { t } from '../i18n'

export type PreshowCard = 'welcome' | 'popcorn' | 'silence' | 'coming' | 'reel' | 'announce' | 'qr' | 'rating' | 'countdown' | 'feature' | 'soon'

export type PreshowWhen = {
  genres?: string[]
  notGenres?: string[]
  kinds?: string[]
  sound?: string[]
  picture?: string[]
  eitherAv?: boolean // the sound or the picture will do
  kids?: boolean
  fromHour?: number
  toHour?: number
  showtime?: boolean
  holidays?: string[] // only in one of these holidays' seasons ('any': any holiday's)
}

export type PreshowBlock = {
  type: 'card' | 'clips' | 'seasonal' | 'trailers'
  card?: PreshowCard
  category?: string
  clipId?: number
  count?: number
  inOrder?: boolean
  text?: string // card: an announcement's words, or a QR code's address
  when?: PreshowWhen
}

export type PreshowTheme = { id: number; name: string; blocks: PreshowBlock[] }

export type PreshowRule = { kind: 'holiday' | 'dates' | 'weekly'; holiday?: string; from?: string; to?: string; days?: number[]; fromHour?: number; toHour?: number }

export type PreshowSchedule = { id: number; name: string; themeId: number; rule: PreshowRule; priority: number; enabled: boolean }

export type PreshowClip = { id: number; path: string; category: string; title: string; seconds: number; width?: number; height?: number; size: number; enabled: boolean; fileUrl: string }

export type PreshowSettings = { folder: string; theater: string; themeId: number; trailerSeconds: number }

export type PreshowAll = {
  settings: PreshowSettings
  clips: PreshowClip[]
  categories: { name: string; clips: number }[]
  themes: PreshowTheme[]
  schedules: PreshowSchedule[]
  holidays: { name: string; from: string; to: string; now: boolean }[]
  holidayNow: string
  activeTheme: number
  cards: PreshowCard[]
}

export type PreshowStep = {
  kind: 'card' | 'clip' | 'trailer'
  card?: PreshowCard
  url?: string
  youtube?: string
  title?: string
  tmdbId?: number
  posterUrl?: string
  backdropUrl?: string
  releaseDate?: string
  category?: string
  season?: number // a show's new season
  why?: string // a trailer's: wanted, cinema or season
  reel?: PreshowReelItem[]
  seconds: number
}
export type PreshowReelItem = { title: string; posterUrl: string; releaseDate: string; season?: number; why?: string }

// Coming-soon trailers downloaded ahead (home_preshow_trailers.go).
export type PreshowTrailer = {
  id: number
  kind: 'movie' | 'tv'
  tmdbId: number
  season?: number
  title: string
  releaseDate: string
  why: 'wanted' | 'cinema' | 'season'
  posterUrl?: string
  youtube?: string
  size: number
  seconds: number
  state: 'waiting' | 'ready' | 'failed'
  error?: string
  keep: boolean
  fileUrl?: string
}
export type TrailerSettings = { on: boolean; folder: string; days: number; after: number; capGB: number; quality: 720 | 1080 | 1440 | 2160; card: boolean }
export type TrailersAll = { trailers: PreshowTrailer[]; settings: TrailerSettings; used: number; ytdlp: boolean; busy: boolean }

// soonLine says when a coming title comes (as home_preshow_cards.go's soonLine).
export function soonLine(date?: string, why?: string, season?: number): string {
  if (!date) return t('Coming soon')
  const d = new Date(date + 'T12:00:00')
  const now = new Date()
  const day = d.toLocaleDateString(undefined, d.getFullYear() === now.getFullYear() ? { month: 'long', day: 'numeric' } : { month: 'long', day: 'numeric', year: 'numeric' })
  const out = d.getTime() <= now.getTime()
  if (season && out) return t('Season {n} · now on', { n: season })
  if (season) return t('Season {n} · {day}', { n: season, day })
  if (why === 'cinema') return out ? t('Now in theaters') : t('In theaters {day}', { day })
  return out ? t('Out now') : t('Coming {day}', { day })
}

export type PreshowFilm = { kind: string; title?: string; tmdbId?: number; posterUrl?: string; backdropUrl?: string; rated?: string; genres?: string[]; sound?: string; picture?: string[]; showtime: boolean; atMs?: number }

export type PreshowPlan = { theme: PreshowTheme; steps: PreshowStep[]; seconds: number; film: PreshowFilm; theater?: string; holiday?: string }

export type PlanAsk = { kind?: 'movie' | 'episode'; id?: number; tmdbId?: number; release?: string; showtime?: number; theme?: number; budget?: number; dry?: boolean }

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(`/api/home/preshow${path}`, {
    method,
    credentials: 'include',
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  if (res.status === 204) return undefined as T
  const data = await res.json().catch(() => null)
  if (!res.ok) throw new Error((data as { error?: string } | null)?.error ?? t('Something went wrong (error {status}).', { status: res.status }))
  return data as T
}

// The pre-show in the Jellyfin app (the NeXroll Intros plugin, pointed at Usherr).
export type PreshowJF = {
  key: string
  address: string
  movies: boolean
  episodes: boolean
  themeId: number
  suggested: string
  server: string
  connected: boolean
  repo: boolean
  plugin: string // '', 'installing', 'installed' (restart), 'active', 'restart', or another of Jellyfin's words
  pluginVersion?: string
  configured: boolean
  watching: number
  lastAsk?: { atMs: number; title: string; steps: number; note?: string }
  problem?: string
}

export const preshowApi = {
  all: (scan = false) => call<PreshowAll>('GET', scan ? '?scan=1' : ''),
  plan: (a: PlanAsk) => {
    const q = new URLSearchParams()
    for (const [k, v] of Object.entries(a)) if (v !== undefined && v !== '' && v !== false) q.set(k, v === true ? '1' : String(v))
    return call<PreshowPlan>('GET', `/plan?${q}`)
  },
  themeNames: () => call<{ themes: { id: number; name: string }[]; active: number }>('GET', '/themes'),
  saveSettings: (s: PreshowSettings) => call<PreshowSettings>('PUT', '/settings', s),
  saveTheme: (th: Omit<PreshowTheme, 'id'> & { id?: number }) => (th.id ? call<{ id: number }>('PUT', `/themes/${th.id}`, th) : call<{ id: number }>('POST', '/themes', th)),
  deleteTheme: (id: number) => call<void>('DELETE', `/themes/${id}`),
  saveSchedule: (sc: Omit<PreshowSchedule, 'id'> & { id?: number }) => (sc.id ? call<{ id: number }>('PUT', `/schedules/${sc.id}`, sc) : call<{ id: number }>('POST', '/schedules', sc)),
  deleteSchedule: (id: number) => call<void>('DELETE', `/schedules/${id}`),
  saveClip: (id: number, c: { title?: string; enabled?: boolean; category?: string }) => call<{ id: number }>('PUT', `/clips/${id}`, c),
  deleteClip: (id: number) => call<void>('DELETE', `/clips/${id}`),
  uploadClip: async (file: File, category: string) => {
    const form = new FormData()
    form.append('category', category)
    form.append('file', file)
    const res = await fetch('/api/home/preshow/clips', { method: 'POST', credentials: 'include', body: form })
    if (!res.ok) throw new Error(((await res.json().catch(() => null)) as { error?: string } | null)?.error ?? t('The upload failed (error {status}).', { status: res.status }))
  },
  trailers: () => call<TrailersAll>('GET', '/trailers'),
  saveTrailerSettings: (s: TrailerSettings) => call<TrailerSettings>('PUT', '/trailers/settings', s),
  fetchTrailers: () => call<void>('POST', '/trailers/fetch'),
  saveTrailer: (id: number, b: { keep?: boolean; again?: boolean }) => call<PreshowTrailer>('PUT', `/trailers/${id}`, b),
  removeTrailer: (id: number) => call<void>('DELETE', `/trailers/${id}`),
  jellyfin: () => call<PreshowJF>('GET', '/jellyfin'),
  saveJellyfin: (j: Pick<PreshowJF, 'address' | 'movies' | 'episodes' | 'themeId'>) => call<PreshowJF>('PUT', '/jellyfin', j),
  jellyfinKey: () => call<PreshowJF>('POST', '/jellyfin/key'),
  jellyfinSetup: () => call<PreshowJF>('POST', '/jellyfin/setup'),
  jellyfinRestart: () => call<void>('POST', '/jellyfin/restart'),
  // The Marquee screens follow the pre-show playing here.
  now: (step: PreshowStep | null, film?: PreshowFilm) =>
    void fetch('/api/home/preshow/now', {
      method: 'POST',
      credentials: 'include',
      keepalive: !step,
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(
        step
          ? { kind: step.kind, card: step.card, title: step.title, posterUrl: step.posterUrl, backdropUrl: step.backdropUrl, releaseDate: step.releaseDate, filmTitle: film?.title, filmPoster: film?.posterUrl, seconds: step.seconds }
          : { end: true },
      ),
    }).catch(() => undefined),
}

export const CARD_LABEL: Record<PreshowCard, string> = {
  welcome: t('Welcome'),
  popcorn: t('Grab your popcorn'),
  silence: t('Please silence your phones'),
  coming: t('Coming attractions'),
  reel: t('Coming soon reel'),
  announce: t('An announcement'),
  qr: t('QR code for Usherr'),
  soon: t('Coming soon'),
  rating: t('The film’s rating'),
  countdown: t('Countdown'),
  feature: t('Feature presentation'),
}

// Whether this browser plays the pre-show before any film (not only a Showtime).
const BEFORE_KEY = 'usherr-preshow-before'
export function preshowBefore(): boolean {
  try {
    return localStorage.getItem(BEFORE_KEY) === '1'
  } catch {
    return false
  }
}
export function setPreshowBefore(on: boolean) {
  try {
    localStorage.setItem(BEFORE_KEY, on ? '1' : '0')
  } catch {
    // a blocked storage just means it isn't remembered
  }
}
