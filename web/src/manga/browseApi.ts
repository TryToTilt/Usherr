// Usherr: MangaDex in Manga > Discover, and reading its chapters online in Usherr's
// own reader (internal/api/home_manga_browse*.go). Everything goes through Usherr:
// the browser never talks to MangaDex.
import type { MangaPages } from './mangaApi'

export interface DexTitle {
  id: string
  title: string
  altTitles?: string[]
  description?: string
  year?: number
  status?: string
  contentRating?: string
  demographic?: string
  originalLanguage?: string
  tags?: string[]
  authors?: string[]
  anilistId?: number
  languages?: string[]
  lastChapter?: string
  licensedLink?: string
  follows?: number
  score?: number
  coverUrl?: string
  inLibrary?: number
  canRead: boolean
}

export interface DexGroup {
  ID: string
  Name: string
}

export interface DexUpdate {
  chapterId: string
  mangaId: string
  number?: string
  volume?: string
  title?: string
  language: string
  groups?: DexGroup[]
  readableAt?: string
  external?: boolean
  comments?: number
  manga?: DexTitle
}

export interface DexListRow {
  name: string
  titles: DexTitle[]
}

export interface OfficialVolume {
  seriesId: number
  volumeId: number
  title: string
  number: string
  coverUrl?: string
  at: string
  have: boolean
}

export interface DexHub {
  dexOn: boolean
  adultAllowed: boolean
  language: string
  official: OfficialVolume[]
  mangadex?: {
    popular: DexTitle[]
    latest: DexUpdate[]
    recent: DexTitle[]
    seasonal?: DexListRow | null
    recommended?: DexListRow | null
    selfPublished?: DexListRow | null
    problem?: string
  }
}

export interface DexTag {
  id: string
  name: string
  group: string
}

export interface DexChapterRow {
  id: string
  number: string
  volume?: string
  title?: string
  group?: string
  pages?: number
  publishedAt?: string
  externalUrl?: string
  copies?: number
}

export interface DexReading {
  mangaId: string
  chapterId: string
  series: string
  chapterTitle?: string
  volume?: string
  groups: string[]
  canDownload: boolean
  next?: { id: string; number: string }
  prev?: { id: string; number: string }
  chapters?: DexChapterRow[]
}

export type DexPages = MangaPages & { dex?: DexReading }

export interface DexContinue {
  dexChapterId: string
  dexMangaId: string
  series: string
  number: string
  coverUrl?: string
  page: number
  pages: number
}

export interface DexSearch {
  title?: string
  tags?: string[]
  notTags?: string[]
  status?: string[]
  demo?: string[]
  lang?: string[]
  year?: string
  order?: string
  asc?: boolean
  readable?: boolean
  adult?: boolean
  offset?: number
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

// searchQuery turns a search into the address's query: only what's chosen.
export function searchQuery(s: DexSearch): string {
  const q = new URLSearchParams()
  if (s.title?.trim()) q.set('title', s.title.trim())
  for (const v of s.tags ?? []) q.append('tag', v)
  for (const v of s.notTags ?? []) q.append('notTag', v)
  for (const v of s.status ?? []) q.append('status', v)
  for (const v of s.demo ?? []) q.append('demo', v)
  for (const v of s.lang ?? []) q.append('lang', v)
  if (s.year && /^\d{4}$/.test(s.year)) q.set('year', s.year)
  if (s.order) q.set('order', s.order)
  if (s.asc) q.set('asc', '1')
  if (s.readable) q.set('readable', '1')
  if (s.adult) q.set('adult', '1')
  if (s.offset) q.set('offset', String(s.offset))
  return q.toString()
}

const adultQ = (adult?: boolean) => (adult ? '?adult=1' : '')

export const browseApi = {
  hub: (adult?: boolean) => call<DexHub>('GET', `/api/manga/dex/hub${adultQ(adult)}`),
  search: (s: DexSearch) => call<{ results: DexTitle[]; total?: number; more?: boolean; problem?: string }>('GET', `/api/manga/dex/search?${searchQuery(s)}`),
  tags: () => call<{ tags: DexTag[]; problem?: string }>('GET', '/api/manga/dex/tags'),
  random: (adult?: boolean) => call<DexTitle>('GET', `/api/manga/dex/random${adultQ(adult)}`),
  title: (id: string, adult?: boolean) => call<{ title: DexTitle; chapters: DexChapterRow[]; language: string; problem?: string }>('GET', `/api/manga/dex/titles/${id}${adultQ(adult)}`),
  continueRow: () => call<DexContinue[]>('GET', '/api/manga/dex/continue'),
  pages: (chapterId: string) => call<DexPages>('GET', `/api/manga/dex/chapters/${chapterId}/pages`),
  pageUrl: (chapterId: string, n: number) => `/api/manga/dex/chapters/${chapterId}/pages/${n}`,
  panels: (chapterId: string, n: number, rtl: boolean) => call<{ panels: { x: number; y: number; w: number; h: number }[]; method: string }>('GET', `/api/manga/dex/chapters/${chapterId}/pages/${n}/panels?rtl=${rtl ? 1 : 0}`),
  download: (chapterId: string) => call<{ queueId?: number; chapterId: number; message: string }>('POST', `/api/manga/dex/chapters/${chapterId}/download`),
}

// dexItem is a chapter read online's key for your place.
export const dexItem = (chapterId: string) => `manga:d:${chapterId}`

// saveDexPlace keeps your place in a chapter read online, as the other readers do.
export function saveDexPlace(chapterId: string, page: number, total: number, keepalive = false) {
  void fetch('/api/home/position', {
    method: 'PUT',
    credentials: 'include',
    keepalive,
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ item: dexItem(chapterId), position: page, duration: total }),
  }).catch(() => undefined)
}

// How someone reads a MangaDex series that isn't in the library: on this device.
const PREFS_KEY = 'usherr.manga.dex-reader.'

export function storedDexPrefs(mangaId: string): { direction?: string; layout?: string } {
  try {
    return JSON.parse(localStorage.getItem(PREFS_KEY + mangaId) ?? '{}') as { direction?: string; layout?: string }
  } catch {
    return {}
  }
}

export function saveDexPrefs(mangaId: string, p: { direction?: string; layout?: string }) {
  try {
    localStorage.setItem(PREFS_KEY + mangaId, JSON.stringify({ ...storedDexPrefs(mangaId), ...p }))
  } catch {
    // no storage: it lasts until the reader closes
  }
}
