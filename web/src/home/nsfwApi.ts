// Home fork: the NSFW module's API calls (internal/api/home_nsfw.go).
// Kept apart from ../api.ts so upstream changes there merge cleanly.
import { t } from '../i18n'

export type VideoStatus = 'missing' | 'downloading' | 'downloaded'

export interface VideoCredit {
  id: string
  name: string
  as?: string
  gender?: string // StashDB: MALE, FEMALE, TRANSGENDER_MALE, TRANSGENDER_FEMALE, INTERSEX, NON_BINARY
}

export interface Video {
  id: number
  stashId: string
  title: string
  studioStashId: string
  studioName: string
  releaseDate: string
  duration: number
  code: string
  details: string
  imageUrl: string
  performers: VideoCredit[]
  tags: string[]
  detailsAt: string
  wanted: boolean
  status: VideoStatus
  filePath: string
  quality: string
  addedBy: string
  addedAt: string
}

export interface LookedUpVideo extends Video {
  inLibrary: boolean
  libraryId?: number
}

export interface VideoPerformer {
  id: number
  stashId: string
  name: string
  disambiguation: string
  aliases: string[]
  imageUrl: string
  followed: boolean
  checkedAt: string
  videoCount?: number
}

export interface VideoStudio {
  id: number
  stashId: string
  name: string
  aliases: string[]
  parentName: string
  imageUrl: string
  followed: boolean
  checkedAt: string
}

export interface VideoCounts {
  nsfw: number
  wanted: number
  downloaded: number
  performers: number
  studios: number
}

export interface VideoSettings {
  enabled: boolean
  path: string
  endpoint: string
  keySet: boolean
}

export interface VideoRelease {
  title: string
  indexerName: string
  protocol: string
  downloadUrl: string
  sizeBytes: number
  publishDate?: string
  blocklisted?: boolean
  quality: string
  score: number
  matches: boolean
}

export interface WhisparrReport {
  dryRun: boolean
  nsfw: number
  nsfwAdded: number
  nsfwUpdated: number
  files: number
  filesFound: number
  filesMissing: string[]
  skippedNoStashId: number
  skippedMovies: number
  studiosFollowed: string[]
  performersFollowed: string[]
  performersKnown: number
}

export class VideoApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

async function call<T>(method: string, path: string, body?: unknown, signal?: AbortSignal): Promise<T> {
  let res: Response
  try {
    res = await fetch('/api/nsfw' + path, {
      method,
      credentials: 'include',
      cache: 'no-store',
      headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
      body: body !== undefined ? JSON.stringify(body) : undefined,
      signal,
    })
  } catch (e) {
    if (signal?.aborted) throw e
    throw new VideoApiError(0, t('Could not reach Mediarium. Check that it is running, then try again.'))
  }
  const text = await res.text()
  let data: unknown = null
  try {
    data = text ? JSON.parse(text) : null
  } catch {
    if (!res.ok) throw new VideoApiError(res.status, t('Something went wrong on the server (error {status}).', { status: res.status }))
  }
  if (!res.ok) {
    const msg = (data as { error?: string } | null)?.error ?? t('Something went wrong on the server (error {status}).', { status: res.status })
    throw new VideoApiError(res.status, msg)
  }
  return data as T
}

const q = (params: Record<string, string | number | undefined>) => {
  const s = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) if (v !== undefined && v !== '') s.set(k, String(v))
  const str = s.toString()
  return str ? '?' + str : ''
}

// imageSrc routes a StashDB image through Mediarium (see handleVideoImage).
// streamUrl plays a downloaded video's file in the browser (with seeking).
export function streamUrl(id: number): string {
  return `/api/nsfw/${id}/stream`
}

export function imageSrc(url: string): string {
  return url ? '/api/nsfw/image?u=' + encodeURIComponent(url) : ''
}

export const nsfwApi = {
  settings: () => call<VideoSettings>('GET', '/settings'),
  saveSettings: (body: { enabled?: boolean; path?: string; endpoint?: string; key?: string; removeKey?: boolean }) => call<VideoSettings>('PUT', '/settings', body),

  list: (filter: { status?: string; performer?: string; studio?: string } = {}) => call<{ counts: VideoCounts; nsfw: Video[] }>('GET', q(filter)),
  get: (id: number) => call<{ video: Video; downloading: boolean }>('GET', `/${id}`),
  add: (stashId: string, search: boolean) => call<Video>('POST', '', { stashId, search }),
  setWanted: (id: number, wanted: boolean) => call<Video>('PUT', `/${id}/wanted`, { wanted }),
  remove: (id: number) => call<null>('DELETE', `/${id}`),
  searchNow: (id: number) => call<{ grabbed: number; message: string }>('POST', `/${id}/search`),
  releases: (id: number, all: boolean) => call<VideoRelease[]>('GET', `/${id}/releases${all ? '?all=1' : ''}`),
  grab: (id: number, r: VideoRelease) => call<{ queueId: number }>('POST', `/${id}/grab`, { title: r.title, downloadUrl: r.downloadUrl, sizeBytes: r.sizeBytes, protocol: r.protocol }),

  lookupNsfw: (text: string, signal?: AbortSignal) => call<{ nsfw: LookedUpVideo[] }>('GET', '/lookup' + q({ kind: 'video', q: text }), undefined, signal),
  lookupPerformers: (text: string) => call<{ performers: VideoPerformer[] }>('GET', '/lookup' + q({ kind: 'performer', q: text })),
  lookupStudios: (text: string) => call<{ studios: VideoStudio[] }>('GET', '/lookup' + q({ kind: 'studio', q: text })),
  discover: (params: Record<string, string | number | undefined>, signal?: AbortSignal) =>
    call<{ nsfw?: LookedUpVideo[]; performers?: VideoPerformer[]; studios?: VideoStudio[]; total: number; page: number; more: boolean; filtered?: boolean }>('GET', '/discover' + q(params), undefined, signal),
  stashTags: (text: string, signal?: AbortSignal) => call<StashTag[]>('GET', '/tags' + q({ q: text }), undefined, signal),
  browse: (kind: 'performer' | 'studio', stashId: string, page: number) => call<{ count: number; nsfw: LookedUpVideo[] }>('GET', '/browse' + q({ kind, stashId, page })),

  performers: () => call<VideoPerformer[]>('GET', '/performers'),
  // follow answers how many videos "add all" added to the library.
  follow: (kind: 'performer' | 'studio', stashId: string, backfill: boolean, search = false) => call<{ added: number; searching: number }>('POST', kind === 'performer' ? '/performers' : '/studios', { stashId, backfill, search }),
  unfollow: (kind: 'performer' | 'studio', id: number) => call<null>('DELETE', `${kind === 'performer' ? '/performers' : '/studios'}/${id}`),
  studios: () => call<VideoStudio[]>('GET', '/studios'),
  checkNow: () => call<{ message: string }>('POST', '/check'),
  fillDetails: () => call<{ message: string }>('POST', '/fill-details'),
  searchStatus: () => call<SearchStatus>('GET', '/search-status'),
  runHeldSearches: () => call<SearchStatus>('POST', '/search-now'),
  bulk: (ids: number[], action: 'want' | 'unwant' | 'removeEmpty' | 'search') => call<{ changed: number; skipped: number }>('POST', '/bulk', { ids, action }),
  importWhisparr: (body: { url: string; apiKey: string; from: string; to: string; dryRun: boolean }) => call<WhisparrReport>('POST', '/import/whisparr', body),
}

export interface SearchStatus {
  running: boolean
  total: number
  done: number
  found: number
  waiting: number
  held?: boolean // the schedule holds the batch
  heldUntil?: string // …until then
}

export interface StashTag {
  id: string
  name: string
  aliases?: string[]
}
