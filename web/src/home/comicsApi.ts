// Home fork: the Comics module's API.
import { t } from '../i18n'
import type { ReadDirection, ReaderLayout } from './comicReading'

export interface ComicSeries {
  id: number
  cvId: number
  name: string
  startYear: number
  publisher: string
  issueCount: number
  description?: string
  imageUrl: string
  folder: string
  monitorNew: boolean
  checkedAt?: string
  have: number
  wanted: number
  inLibrary?: number // lookup results: the library id when already added
  type?: ComicSeriesType
  wantRead?: boolean // this person
}

export type ComicSeriesType = '' | 'comic' | 'manga' | 'webtoon' | 'collected'

export interface ComicIssue {
  id: number
  seriesId: number
  cvId: number
  number: string
  title: string
  coverDate: string
  storeDate: string
  imageUrl: string
  wanted: boolean
  status: 'missing' | 'downloading' | 'downloaded'
  filePath?: string
  fileSize?: number
  // This person: read, on their Want to Read, and where they left it.
  read?: boolean
  wantRead?: boolean
  page?: number
  pages?: number
}

// What the reader needs to open an issue.
export interface ComicNextIssue {
  id: number
  number: string
  title?: string
  have: boolean
}

export interface ComicPages {
  pages?: number
  pdf?: boolean
  title: string
  seriesId?: number
  direction?: ReadDirection
  layout?: ReaderLayout | ''
  next?: ComicNextIssue // the issue after this one
  nextHave?: ComicNextIssue // the next one with a file, when that isn't the same
}

// One entry in someone's reading views. No issueId: a whole series on Want to Read.
export interface ComicReadingItem {
  seriesId: number
  issueId?: number
  series: string
  number?: string
  title?: string
  imageUrl: string
  status?: string
  page?: number
  pages?: number
  next?: boolean
  at: number
}

export interface ComicReadingViews {
  continue: ComicReadingItem[]
  want: ComicReadingItem[]
  read: ComicReadingItem[]
}

// What choosing another ComicVine volume would do.
export interface ComicMatchPreview {
  series: ComicSeries
  files: number // files that stay with an issue of the same number
  noMatch: string[] // issue numbers with a file and no such number there
  added: number
  removed: number
}

export interface ComicSettings {
  enabled: boolean
  path: string
  keySet: boolean
}

export interface ComicRelease {
  title: string
  indexerName: string
  sizeBytes: number
  downloadUrl: string
  protocol: string
  matches: boolean
  blocklisted?: boolean
  publishDate?: string
  verdict?: ComicVerdict // home fork
}

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch('/api/comics' + path, {
    method,
    credentials: 'include',
    cache: 'no-store',
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const text = await res.text()
  const data = text ? JSON.parse(text) : null
  if (!res.ok) throw new Error(data?.error ?? t('Something went wrong (error {status}).', { status: res.status }))
  return data as T
}

// home fork: how a search judged a release (comics.Evaluate)
export interface ComicVerdict {
  reject?: string
  score: number
  confidence?: 'high' | 'medium' | 'low'
  notes?: string[]
}

export interface ComicReview {
  id: number
  issueId: number
  seriesId: number
  series: string
  number: string
  issueTitle: string
  coverDate: string
  title: string
  sizeBytes: number
  indexer: string
  score: number
  notes?: string[]
}

export interface ComicSearchLog {
  searched: boolean
  at?: string
  outcome?: 'grabbed' | 'review' | 'nothing'
  details?: {
    found: number
    accepted?: { title: string; confidence?: string; notes?: string[]; blocked?: boolean }[]
    rejected?: { title: string; reject?: string }[]
  }
}

// home fork: the week's new comics
export interface ComicWeekIssue {
  cvId: number
  volumeCvId: number
  series: string
  startYear?: number
  publisher?: string
  number: string
  title?: string
  storeDate: string
  imageUrl?: string
  seriesId?: number
  issueId?: number
  status?: 'missing' | 'downloading' | 'downloaded'
  wanted?: boolean
}

export interface ComicWeek {
  from: string
  to: string
  fetchedAt: string
  issues: ComicWeekIssue[]
}

// home fork: story arcs
export interface ComicArc {
  id: number
  cvId: number
  name: string
  publisher?: string
  description?: string
  imageUrl?: string
  total: number
  have: number
  wanted: number
  downloading: number
  busy?: string
}

// home fork: an arc your issues belong to, for "Story arcs for you".
export interface ComicArcForYou {
  cvId: number
  name: string
  imageUrl?: string
  libraryId?: number
  have: number
  series: number
  issues: { issueId: number; seriesId: number; have: boolean }[]
}

export interface ComicArcIssue {
  position: number
  cvId: number
  volumeCvId: number
  series: string
  number: string
  title?: string
  coverDate?: string
  storeDate?: string
  imageUrl?: string
  seriesId?: number
  issueId?: number
  status?: 'missing' | 'downloading' | 'downloaded' | ''
  wanted?: boolean
}

// home fork: library tools
export interface ComicTools {
  files: number
  cbr: number
  misnamed: number
  last: {
    tool?: 'check' | 'convert' | 'rename'
    running: boolean
    done: number
    total: number
    changed: number
    problems: { issueId: number; series: string; number: string; file: string; problem: string }[]
    started?: string
    finished?: string
  }
}

export const comicsApi = {
  tools: () => call<ComicTools>('GET', '/tools'),
  runTool: (tool: 'check' | 'convert' | 'rename') => call<{ message: string }>('POST', `/tools/run/${tool}`),
  arcs: () => call<ComicArc[]>('GET', '/arcs'),
  arcsForYou: () => call<ComicArcForYou[]>('GET', '/arcs/for-you'), // home fork
  arc: (id: number) => call<{ arc: ComicArc; issues: ComicArcIssue[] }>('GET', `/arcs/${id}`),
  lookupArcs: (q: string) => call<ComicArc[]>('GET', '/arcs/lookup?q=' + encodeURIComponent(q)),
  addArc: (cvId: number, want: boolean) => call<ComicArc>('POST', '/arcs', { cvId, want }),
  wantArc: (id: number) => call<{ message: string }>('POST', `/arcs/${id}/want`),
  refreshArc: (id: number) => call<ComicArc & { message?: string }>('POST', `/arcs/${id}/refresh`),
  removeArc: (id: number) => call<{ message: string }>('DELETE', `/arcs/${id}`),
  week: (date: string) => call<ComicWeek>('GET', '/week' + (date ? `?date=${date}` : '')),
  pull: (issueCvId: number, volumeCvId: number) => call<{ seriesId: number; issueId: number; message: string }>('POST', '/week/pull', { issueCvId, volumeCvId }),
  reviews: () => call<ComicReview[]>('GET', '/reviews'),
  acceptReview: (id: number) => call<{ message: string }>('POST', `/reviews/${id}/accept`),
  rejectReview: (id: number) => call<{ message: string }>('POST', `/reviews/${id}/reject`),
  searchLog: (issueId: number) => call<ComicSearchLog>('GET', `/issues/${issueId}/search-log`),
  enabled: () => call<{ enabled: boolean }>('GET', '/enabled'),
  settings: () => call<ComicSettings>('GET', '/settings'),
  saveSettings: (body: { enabled?: boolean; path?: string; key?: string; removeKey?: boolean }) => call<ComicSettings>('PUT', '/settings', body),
  list: () => call<ComicSeries[]>('GET', ''),
  lookup: (q: string) => call<ComicSeries[]>('GET', '/lookup?q=' + encodeURIComponent(q)),
  add: (cvId: number, want: 'all' | 'future' | 'none') => call<{ series: ComicSeries; issues: number }>('POST', '', { cvId, want }),
  get: (id: number) => call<{ series: ComicSeries; issues: ComicIssue[] }>('GET', `/${id}`),
  update: (id: number, body: { monitorNew?: boolean; type?: ComicSeriesType }) => call<ComicSeries>('PUT', `/${id}`, body),
  matchPreview: (id: number, cvId: number) => call<ComicMatchPreview>('POST', `/${id}/match/preview`, { cvId }),
  match: (id: number, cvId: number) => call<{ series: ComicSeries; found: number }>('POST', `/${id}/match`, { cvId }),
  reading: () => call<ComicReadingViews>('GET', '/reading'),
  markRead: (issueId: number, scope: 'issue' | 'upto' | 'series', read: boolean) => call<{ issueIds: number[] }>('PUT', '/reading/mark', { issueId, scope, read }),
  markIdsRead: (issueIds: number[]) => call<{ issueIds: number[] }>('PUT', '/reading/mark', { issueIds, read: true }),
  wantRead: (seriesId: number, issueId: number, want: boolean) => call<null>('PUT', '/reading/want', { seriesId, issueId, want }),
  readerPrefs: (seriesId: number, body: { direction?: ReadDirection; layout?: ReaderLayout }) => call<null>('PUT', '/reader-prefs', { seriesId, ...body }),
  remove: (id: number) => call<{ message: string }>('DELETE', `/${id}`),
  refresh: (id: number) => call<{ added: number; found?: number }>('POST', `/${id}/refresh`), // found: files already in its folder (home fork)
  searchSeries: (id: number) => call<{ message: string }>('POST', `/${id}/search`),
  setWanted: (ids: number[], wanted: boolean, search = false) => call<{ changed: number; searching: number }>('PUT', '/issues/wanted', { ids, wanted, search }),
  searchIssue: (id: number) => call<{ grabbed: number; message: string }>('POST', `/issues/${id}/search`),
  releases: (id: number, all: boolean, q = '') => call<ComicRelease[]>('GET', `/issues/${id}/releases?${new URLSearchParams({ ...(all ? { all: '1' } : {}), ...(q ? { q } : {}) }).toString()}`),
  grab: (id: number, r: ComicRelease) => call<{ queueId: number }>('POST', `/issues/${id}/grab`, { title: r.title, downloadUrl: r.downloadUrl, sizeBytes: r.sizeBytes, protocol: r.protocol }),
  fileUrl: (id: number) => `/api/comics/issues/${id}/file`,
  pages: (id: number) => call<ComicPages>('GET', `/issues/${id}/pages`),
  pageUrl: (id: number, n: number, w?: number) => `/api/comics/issues/${id}/pages/${n}${w ? `?w=${w}` : ''}`, // w: a copy that wide (home fork)
  // Guided view (home fork): a page's panels in reading order, as fractions of the page.
  panels: (id: number, n: number, rtl: boolean) => call<{ panels: ComicPanel[]; method: string }>('GET', `/issues/${id}/pages/${n}/panels?rtl=${rtl ? 1 : 0}`),
}

export interface ComicPanel {
  x: number
  y: number
  w: number
  h: number
}
