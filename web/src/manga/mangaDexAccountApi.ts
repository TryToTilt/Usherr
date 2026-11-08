// Usherr: your own MangaDex account in Manga: the sign-in (saved, never shown
// again), and your follows, reading status, lists and shelves.

export type MangaDexReadingStatus = '' | 'reading' | 'on_hold' | 'plan_to_read' | 'dropped' | 're_reading' | 'completed'

export interface MangaDexSyncStats {
  follows: number
  added: number
  outside: number
  waiting?: number
  readPushed: number
  readPulled: number
  note?: string
  unfinished?: boolean
}

export interface MangaDexAccount {
  connected: boolean
  state?: '' | 'ok' | 'refused'
  problem?: string
  connectedAt?: string
  lightAt?: string
  fullAt?: string
  stats?: MangaDexSyncStats
  chaptersOn: boolean
  away?: { kind: string; until: string }
}

export interface MangaDexSeries {
  connected: boolean
  refused?: boolean
  linked: boolean
  dexId?: string
  following: boolean
  status: MangaDexReadingStatus
  lists: { id: string; name: string; in: boolean }[]
  changed?: boolean
}

export interface MangaDexShelfItem {
  dexId: string
  title: string
  cover?: string
  seriesId?: number
  number?: string
  group?: string
  at?: string
  reason?: string
  chapterId?: string
}

export interface MangaDexShelves {
  connected: boolean
  state?: string
  problem?: string
  newChapters?: MangaDexShelfItem[]
  recent?: MangaDexShelfItem[]
  lists?: { id: string; name: string; items: MangaDexShelfItem[]; more?: number }[]
  outside?: MangaDexShelfItem[]
}

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const r = await fetch(path, {
    method,
    credentials: 'include',
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const d = (await r.json().catch(() => ({}))) as { error?: string; message?: string }
  if (!r.ok) throw new Error(d.error || d.message || `Request failed (${r.status})`)
  return d as T
}

export const mangaDexAccountApi = {
  account: () => call<MangaDexAccount>('GET', '/api/manga/md/account'),
  connect: (body: { clientId: string; clientSecret: string; username: string; password: string }) => call<MangaDexAccount>('PUT', '/api/manga/md/account', body),
  disconnect: () => call<{ message: string }>('DELETE', '/api/manga/md/account'),
  syncNow: () => call<{ message: string }>('POST', '/api/manga/md/sync'),
  shelves: () => call<MangaDexShelves>('GET', '/api/manga/md/shelves'),
  series: (id: number) => call<MangaDexSeries>('GET', `/api/manga/${id}/md`),
  follow: (id: number, following: boolean) => call<{ md: MangaDexSeries; note: string }>('PUT', `/api/manga/${id}/md/follow`, { following }),
  status: (id: number, status: MangaDexReadingStatus) => call<{ md: MangaDexSeries; note: string }>('PUT', `/api/manga/${id}/md/status`, { status }),
  inList: (id: number, list: string, inList: boolean) => call<{ md: MangaDexSeries }>('PUT', `/api/manga/${id}/md/lists/${list}`, { in: inList }),
}

// Links out to MangaDex, which is credited wherever its data shows.
export const mangaDexTitleUrl = (dexId: string) => `https://mangadex.org/title/${dexId}`
export const mangaDexChapterUrl = (chapterId: string) => `https://mangadex.org/chapter/${chapterId}`
