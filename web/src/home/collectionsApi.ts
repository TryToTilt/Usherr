// Usherr: collections pushed to Jellyfin and Plex (home_collections.go) and
// badges on posters (home_badges.go): what Kometa did.
import { t } from '../i18n'

export type CollSource = 'list' | 'tmdb' | 'trending' | 'rule' | 'picked'
export type CollMedia = 'movie' | 'tv' | 'both'
export type CollPick = { kind: 'movie' | 'tv'; tmdbId: number; imdbId?: string; title: string; year?: number }
export type CollConfig = {
  listId?: number
  tmdbCollection?: number
  tmdbName?: string
  count?: number
  genres?: string[]
  yearFrom?: number
  yearTo?: number
  minRating?: number
  addedDays?: number
  picked?: CollPick[]
}
export type CollInput = {
  name: string
  source: CollSource
  config: CollConfig
  media: CollMedia
  servers: ('jellyfin' | 'plex')[]
  sortTitle: string
  poster: '' | 'upload' | 'made'
  visibility: ('library' | 'home' | 'shared')[]
  onlyLibrary: boolean
  removeMissing: boolean
  enabled: boolean
}
export type PushCollection = CollInput & {
  id: number
  hasUpload: boolean
  createdMs: number
  lastSyncMs?: number
  lastStatus?: 'ok' | 'error'
  lastMessage?: string
  lastFound: number
  lastMatched: number
  lastMissing: string[]
  targets: string[]
  running: boolean
}
export type CollAll = { collections: PushCollection[]; lists: { id: number; name: string }[]; hours: number; jellyfin: boolean; plex: boolean; tmdb: boolean }
export type CollPreview = { found: number; inLibrary: number; sample: string[]; missing: string[] }

export type BadgeCorner = 'top-left' | 'top-right' | 'bottom-left' | 'bottom-right'
export type BadgeSettings = {
  on: boolean
  libraries: string[]
  res: boolean
  hdr: boolean
  audio: boolean
  channels: boolean
  rating: boolean
  ratingFrom: 'server' | 'imdb'
  new: boolean
  newDays: number
  qualityAt: BadgeCorner
  ratingAt: BadgeCorner
  look: 'dark' | 'color'
  size: 'small' | 'medium' | 'large'
}
export type BadgeStatus = { running: boolean; lastMs?: number; drawn: number; restored: number; badged: number; failed: number; note?: string }
export type BadgeAll = { settings: BadgeSettings; libraries: string[]; samples: { copy: string; title: string; year?: number; library: string }[]; status: BadgeStatus; servers: boolean; note?: string }

async function answer<T>(res: Response): Promise<T> {
  if (res.status === 204) return undefined as T
  const data = await res.json().catch(() => null)
  if (!res.ok) throw new Error((data as { error?: string } | null)?.error ?? t('Something went wrong (error {status}).', { status: res.status }))
  return data as T
}

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(`/api/home${path}`, {
    method,
    credentials: 'include',
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  return answer<T>(res)
}

// A picture for <img>, from a POST that answers a JPEG.
async function picture(path: string, body: unknown, signal?: AbortSignal): Promise<string> {
  const res = await fetch(`/api/home${path}`, { method: 'POST', credentials: 'include', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body), signal })
  if (!res.ok) await answer(res)
  return URL.createObjectURL(await res.blob())
}

export const collApi = {
  all: () => call<CollAll>('GET', '/collections'),
  add: (c: CollInput) => call<PushCollection>('POST', '/collections', c),
  update: (id: number, c: CollInput) => call<PushCollection>('PUT', `/collections/${id}`, c),
  remove: (id: number, fromServers: boolean) => call<{ removed: boolean; problems: string[] | null }>('DELETE', `/collections/${id}${fromServers ? '?servers=1' : ''}`),
  sync: (id: number) => call<{ started: boolean }>('POST', `/collections/${id}/sync`),
  syncAll: () => call<{ started: number }>('POST', '/collections/sync'),
  preview: (c: CollInput) => call<CollPreview>('POST', '/collections/preview', c),
  titles: (q: string) => call<CollPick[]>('GET', `/collections/titles?q=${encodeURIComponent(q)}`),
  setHours: (hours: number) => call<{ hours: number }>('PUT', '/collections/settings', { hours }),
  posterUrl: (id: number, made = false) => `/api/home/collections/${id}/poster?${made ? 'made=1&' : ''}v=${Date.now()}`,
  uploadPoster: async (id: number, f: File) => {
    const form = new FormData()
    form.append('file', f)
    return answer<PushCollection>(await fetch(`/api/home/collections/${id}/poster`, { method: 'POST', credentials: 'include', body: form }))
  },
}

export const badgeApi = {
  all: () => call<BadgeAll>('GET', '/badges'),
  save: (b: BadgeSettings) => call<BadgeSettings>('PUT', '/badges', b),
  apply: () => call<{ started: boolean }>('POST', '/badges/apply'),
  preview: (settings: BadgeSettings, copy: string, signal?: AbortSignal) => picture('/badges/preview', { settings, copy }, signal),
}
