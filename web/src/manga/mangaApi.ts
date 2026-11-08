// Usherr: the Manga section's API.

export type MangaFormat = 'manga' | 'manhwa' | 'manhua' | 'oneshot'

export interface MangaSeries {
  id: number
  anilistId?: number
  title: string
  format: MangaFormat
  status: string
  startYear: number
  author: string
  publisher: string
  buyUrl?: string
  siteUrl?: string
  licensedEn: number // 1 known, -1 unknown
  isAdult?: boolean
  description?: string
  coverUrl?: string
  volumeCount: number
  monitorNew: boolean
  wantAll: boolean
  sourceMode: '' | 'official' | 'fan'
  folder?: string
  addedAt: string
  total: number
  have: number
  wanted: number
  downloading: number
  waiting: number
  following?: boolean
  chaptersHave?: number
}

export type MangaVolumeState = 'downloaded' | 'downloading' | 'waiting' | 'missing'

export interface MangaVolume {
  id: number
  seriesId: number
  number: string
  title?: string
  wanted: boolean
  status: string
  state: MangaVolumeState
  filePath?: string
  fileSize?: number
  sourceClass?: string
  reviews?: number
  read?: boolean
  page?: number
  pages?: number
}

export type ReadDirection = 'ltr' | 'rtl'
export type MangaLayout = '' | 'single' | 'double' | 'webtoon' | 'wide' | 'panels'

export interface MangaNextVolume {
  id: number
  number: string
  have: boolean
  wanted?: boolean
  chapter?: boolean
}

export interface MangaPages {
  pdf?: boolean
  pages?: number
  title: string
  seriesId: number
  number?: string
  format?: MangaFormat
  direction?: string
  layout?: string
  shift?: boolean
  sizes?: [number, number][]
  next?: MangaNextVolume
  nextHave?: MangaNextVolume
  chapter?: boolean
  group?: string
}

export interface MangaContinue {
  volumeId?: number
  chapterId?: number
  seriesId: number
  series: string
  number: string
  coverUrl?: string
  page: number
  pages: number
}

export interface MangaAlias {
  alias: string
  kind: string
}

export interface MangaReview {
  id: number
  seriesId: number
  series: string
  volumeId?: number
  number?: string
  kind: 'single' | 'pack' | 'copy'
  title: string
  sizeBytes: number
  protocol: string
  indexer: string
  score: number
  class: string
  notes: string[] | null
}

export interface MangaSummary {
  query?: string
  asked: number
  failed?: number
  skipped?: number
  noIndex?: boolean
  found: number
  rejected?: Record<string, number>
  doubtful?: number
  taken?: number
  number?: string
}

export interface MangaSearchEntry {
  volumeId?: number
  searchedAt: string
  outcome: string
  summary: MangaSummary
  nextAt?: string
  empties?: number
}

export interface MangaSearchState {
  last?: MangaSearchEntry
  number?: string
  nextLook?: string
}

export interface MangaSources {
  official: boolean
  fan: boolean
  nyaa: boolean
  mangadex?: boolean
}

export type MangaChapterState = 'downloaded' | 'downloading' | 'in_volume' | 'external' | 'unavailable' | 'missing'

export interface MangaChapter {
  id: number
  seriesId: number
  number: string
  volume?: string
  title?: string
  group?: string
  language?: string
  source: string
  pages?: number
  externalUrl?: string
  unavailable?: boolean
  publishedAt?: string
  wanted: boolean
  status: string
  state: MangaChapterState
  filePath?: string
  fileSize?: number
  setAside?: boolean
  problem?: string
  inVolumeId?: number
  inVolume?: string
  copies?: number
  read?: boolean
  page?: number
  pagesFor?: number
}

export interface MangaDexStats {
  listed: number
  readable: number
  external: number
  unavailable: number
  languages?: string[]
  licensedLink?: string
}

export interface MangaDexAway {
  kind: string
  until: string
}

export interface MangaChapterSummary {
  allowed: boolean
  follow: '' | 'new' | 'all'
  link: '' | 'linked' | 'unsure' | 'none'
  total: number
  have: number
  wanted: number
  stats: MangaDexStats
  empties?: number
  trouble?: string
  checkedAt?: string
  nextAt?: string
  tonight?: boolean
  away?: MangaDexAway
  firstHave?: number
  firstNumber?: string
}

export interface MangaChapterGroup {
  key: string
  name: string
  chapters: number
}

export interface MangaDexCandidate {
  id: string
  title: string
  year?: number
}

export interface MangaChapters {
  chapters: MangaChapter[]
  groups: MangaChapterGroup[]
  summary: MangaChapterSummary
  preferGroup: string
  dexId: string
  candidates?: MangaDexCandidate[]
}

export interface MangaDetail {
  series: MangaSeries
  volumes: MangaVolume[]
  aliases: MangaAlias[]
  reviews: MangaReview[]
  search: MangaSearchState
  sources: MangaSources
  problems: string[]
  reading: MangaContinue[]
  chapters: MangaChapterSummary
}

export interface MangaFound {
  anilistId: number
  title: string
  aliases: MangaAlias[] | null
  format: MangaFormat
  status: string
  startYear: number
  author: string
  publisher: string
  buyUrl?: string
  licensedEn: number
  isAdult?: boolean
  description?: string
  coverUrl?: string
  volumes: number
  inLibrary?: number
  genres?: string[]
  score?: number
  siteUrl?: string
  listStatus?: 'reading' | 'planning'
  relation?: string
  mangadexId?: string // AniList's link to MangaDex, when MangaDex is on
}

export interface MangaLookup {
  results: MangaFound[]
  hiddenAdult?: number
  hiddenNovels?: number
  problem?: string
}

export interface MangaDiscover {
  results: MangaFound[]
  more?: boolean
  genres?: string[]
  problem?: string
}

export interface MangaAbout {
  genres: string[]
  score?: number
  related: MangaFound[]
  similar: MangaFound[]
}

export interface MangaStats {
  volumes: number
  series: number
  chapters: number
  thisMonth: number
}

export interface MangaSourceSettings {
  official: boolean
  fan: boolean
  mangadex: boolean
  nyaa: boolean
}

export interface MangaNyaaState {
  indexer?: string
  own: boolean
  torrentsOn: boolean
  client: 'builtin' | 'debrid' | 'external'
  vpnRequired: boolean
  vpnConnected: boolean
  indexersThroughVpn: boolean
  seedRatio?: string
  seedHours?: string
}

export interface MangaSettings {
  folderProblem?: string
  sources: MangaSourceSettings
  path: string
  nyaa: MangaNyaaState
  chapters: { language: string; saver: boolean; tidy: boolean; away?: MangaDexAway }
}

// Bringing manga over from Comics.
export interface MangaMoveCandidate {
  id: number
  name: string
  publisher: string
  startYear: number
  type?: string
  issues: number
  files: number
  magazine: boolean
}

export interface MangaMoveFile {
  issueId?: number
  number: string
  from: string
  to: string
  size: number
  action: 'move' | 'stay'
  why?: string // why it stays: identical, copy, exists, unread
}

export interface MangaMovePlan {
  id: number
  comicSeriesId: number
  comicName: string
  title: string
  anilistId?: number
  mangaSeriesId?: number
  folder: string
  volumes: number
  files: MangaMoveFile[]
}

export interface MangaMoveJob {
  id: number
  comicSeriesId: number
  comicName: string
  title: string
  mangaSeriesId: number
  status: 'preview' | 'applying' | 'applied' | 'undoing' | 'undone' | 'stopped'
  done: number
  total: number
  moved: number
  stayed: number
  problem?: string
  undoLeft?: string[]
  at: string
}

export interface MangaMoveState {
  candidates: MangaMoveCandidate[]
  moves: MangaMoveJob[]
  folder: string
}

export interface MangaRelease {
  title: string
  downloadUrl: string
  sizeBytes: number
  protocol: string
  indexerName?: string
  indexer?: string
  blocklisted?: boolean
  verdict: { reject?: string; why?: string; score: number; confidence?: string; notes?: string[]; class?: string; kind?: string }
}

export interface MangaScanGroup {
  name: string
  seriesId?: number
  title?: string
  files: number
  claimed: number
  chapters: number
  packs: number
  unread: number
  doubles: number
}

export interface MangaScanResult {
  at?: string
  root: string
  problem?: string
  files: number
  claimed: number
  gone: number
  groups: MangaScanGroup[]
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

export const mangaApi = {
  list: () => call<{ series: MangaSeries[]; problems: string[]; sources: MangaSources }>('GET', '/api/manga'),
  get: (id: number | string) => call<MangaDetail>('GET', `/api/manga/${id}`),
  lookup: (q: string) => call<MangaLookup>('GET', `/api/manga/lookup?q=${encodeURIComponent(q)}`),
  add: (body: { anilistId?: number; title?: string; want: 'all' | 'new' | 'none'; allowFan?: boolean; chapters?: 'new' | 'all' }) => call<{ series: MangaSeries; volumes: number; found?: number }>('POST', '/api/manga', body),
  update: (id: number, body: { monitorNew?: boolean; sourceMode?: string }) => call<MangaSeries>('PUT', `/api/manga/${id}`, body),
  remove: (id: number) => call<{ message: string }>('DELETE', `/api/manga/${id}`),
  refresh: (id: number) => call<{ added: number; found: number }>('POST', `/api/manga/${id}/refresh`),
  search: (id: number) => call<{ message: string }>('POST', `/api/manga/${id}/search`),
  addVolume: (id: number, number: string) => call<MangaVolume>('POST', `/api/manga/${id}/volumes`, { number }),
  setWanted: (ids: number[], wanted: boolean) => call<{ changed: number }>('PUT', '/api/manga/volumes/wanted', { ids, wanted }),
  searchVolume: (id: number) => call<{ grabbed: boolean; summary: MangaSummary }>('POST', `/api/manga/volumes/${id}/search`),
  releases: (id: number) => call<{ releases: MangaRelease[]; summary: MangaSummary }>('GET', `/api/manga/volumes/${id}/releases`),
  grab: (id: number, r: MangaRelease) => call<{ queueId: number }>('POST', `/api/manga/volumes/${id}/grab`, { title: r.title, downloadUrl: r.downloadUrl, sizeBytes: r.sizeBytes, protocol: r.protocol }),
  reviews: () => call<MangaReview[]>('GET', '/api/manga/reviews'),
  acceptReview: (id: number) => call<{ message: string }>('POST', `/api/manga/reviews/${id}/accept`),
  rejectReview: (id: number) => call<{ message: string }>('POST', `/api/manga/reviews/${id}/reject`),
  settings: () => call<MangaSettings>('GET', '/api/manga/settings'),
  saveSources: (body: Partial<MangaSourceSettings> & { chapterLanguage?: string; chapterSaver?: boolean; tidyChapters?: boolean }) => call<MangaSettings>('PUT', '/api/manga/settings', body),
  scan: () => call<MangaScanResult>('POST', '/api/manga/scan'),
  lastScan: () => call<MangaScanResult>('GET', '/api/manga/scan'),
  continueRow: () => call<MangaContinue[]>('GET', '/api/manga/continue'),
  pages: (id: number) => call<MangaPages>('GET', `/api/manga/volumes/${id}/pages`),
  pageUrl: (id: number, n: number, w?: number) => `/api/manga/volumes/${id}/pages/${n}${w ? `?w=${w}` : ''}`,
  fileUrl: (id: number) => `/api/manga/volumes/${id}/file`,
  panels: (id: number, n: number, rtl: boolean, chapter = false) => call<{ panels: { x: number; y: number; w: number; h: number }[]; method: string }>('GET', `/api/manga/${chapter ? 'chapters' : 'volumes'}/${id}/pages/${n}/panels?rtl=${rtl ? 1 : 0}`),
  readerPrefs: (seriesId: number, body: { direction?: ReadDirection; layout?: MangaLayout; shift?: 1 | -1 }) => call<unknown>('PUT', `/api/manga/${seriesId}/reader`, body),
  discover: (shelf: string, genre: string, page: number) => call<MangaDiscover>('GET', `/api/manga/discover?shelf=${shelf}&genre=${encodeURIComponent(genre)}&page=${page}`),
  aniListList: (user: string) => call<MangaDiscover>('GET', `/api/manga/anilist-list?user=${encodeURIComponent(user)}`),
  about: (id: number | string) => call<MangaAbout>('GET', `/api/manga/${id}/about`),
  stats: () => call<MangaStats>('GET', '/api/manga/stats'),
  markRead: (volumeId: number, read: boolean, upTo = false) => call<{ changed: number }>('PUT', '/api/manga/reading/mark', { volumeId, read, upTo }),
  chapters: (id: number | string) => call<MangaChapters>('GET', `/api/manga/${id}/chapters`),
  setMangaDex: (id: number, body: { follow?: string; dexId?: string; preferGroup?: string }) => call<MangaChapterSummary>('PUT', `/api/manga/${id}/mangadex`, body),
  checkChapters: (id: number) => call<{ message: string }>('POST', `/api/manga/${id}/chapters/check`),
  getChapters: (id: number, body: { ids?: number[]; next?: number }) => call<{ queueId: number; chapters: number }>('POST', `/api/manga/${id}/chapters/get`, body),
  tidyChapters: (id: number, undo = false) => call<{ moved: number }>('POST', `/api/manga/${id}/chapters/tidy`, { undo }),
  setChaptersWanted: (ids: number[], wanted: boolean) => call<{ changed: number }>('PUT', '/api/manga/chapters/wanted', { ids, wanted }),
  chapterPages: (id: number) => call<MangaPages>('GET', `/api/manga/chapters/${id}/pages`),
  chapterPageUrl: (id: number, n: number, w?: number) => `/api/manga/chapters/${id}/pages/${n}${w ? `?w=${w}` : ''}`,
  chapterFileUrl: (id: number) => `/api/manga/chapters/${id}/file`,
  markChapterRead: (chapterId: number, read: boolean, upTo = false) => call<{ changed: number }>('PUT', '/api/manga/reading/chapter-mark', { chapterId, read, upTo }),
  moveState: () => call<MangaMoveState>('GET', '/api/manga/move'),
  movePreview: (body: { comicSeriesId: number; anilistId?: number; title?: string }) => call<MangaMovePlan>('POST', '/api/manga/move/preview', body),
  moveApply: (id: number) => call<{ message: string }>('POST', `/api/manga/move/${id}/apply`),
  moveUndo: (id: number) => call<{ message: string }>('POST', `/api/manga/move/${id}/undo`),
  moveEmpty: (ids: number[]) => call<{ moved: number; message: string }>('POST', '/api/manga/move/empty', { ids }),
}
