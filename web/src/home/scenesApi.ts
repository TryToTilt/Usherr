// Home fork: the Scenes module's API calls (internal/api/home_scenes.go).
// Kept apart from ../api.ts so upstream changes there merge cleanly.
import { t } from '../i18n'

export type SceneStatus = 'missing' | 'downloading' | 'downloaded'

export interface SceneCredit {
  id: string
  name: string
  as?: string
  gender?: string // StashDB: MALE, FEMALE, TRANSGENDER_MALE, TRANSGENDER_FEMALE, INTERSEX, NON_BINARY
}

export interface Scene {
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
  performers: SceneCredit[]
  tags: string[]
  detailsAt: string
  wanted: boolean
  status: SceneStatus
  filePath: string
  quality: string
  addedBy: string
  addedAt: string
}

export interface LookedUpScene extends Scene {
  inLibrary: boolean
  libraryId?: number
}

export interface ScenePerformer {
  id: number
  stashId: string
  name: string
  disambiguation: string
  aliases: string[]
  imageUrl: string
  followed: boolean
  checkedAt: string
  sceneCount?: number
}

export interface SceneStudio {
  id: number
  stashId: string
  name: string
  aliases: string[]
  parentName: string
  imageUrl: string
  followed: boolean
  checkedAt: string
}

export interface SceneCounts {
  scenes: number
  wanted: number
  downloaded: number
  performers: number
  studios: number
}

export interface SceneSettings {
  enabled: boolean
  path: string
  endpoint: string
  keySet: boolean
}

export interface SceneRelease {
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
  scenes: number
  scenesAdded: number
  scenesUpdated: number
  files: number
  filesFound: number
  filesMissing: string[]
  skippedNoStashId: number
  skippedMovies: number
  studiosFollowed: string[]
  performersFollowed: string[]
  performersKnown: number
}

export class SceneApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

async function call<T>(method: string, path: string, body?: unknown, signal?: AbortSignal): Promise<T> {
  let res: Response
  try {
    res = await fetch('/api/scenes' + path, {
      method,
      credentials: 'include',
      cache: 'no-store',
      headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
      body: body !== undefined ? JSON.stringify(body) : undefined,
      signal,
    })
  } catch (e) {
    if (signal?.aborted) throw e
    throw new SceneApiError(0, t('Could not reach Mediarium. Check that it is running, then try again.'))
  }
  const text = await res.text()
  let data: unknown = null
  try {
    data = text ? JSON.parse(text) : null
  } catch {
    if (!res.ok) throw new SceneApiError(res.status, t('Something went wrong on the server (error {status}).', { status: res.status }))
  }
  if (!res.ok) {
    const msg = (data as { error?: string } | null)?.error ?? t('Something went wrong on the server (error {status}).', { status: res.status })
    throw new SceneApiError(res.status, msg)
  }
  return data as T
}

const q = (params: Record<string, string | number | undefined>) => {
  const s = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) if (v !== undefined && v !== '') s.set(k, String(v))
  const str = s.toString()
  return str ? '?' + str : ''
}

// imageSrc routes a StashDB image through Mediarium (see handleSceneImage).
// streamUrl plays a downloaded scene's file in the browser (with seeking).
export function streamUrl(id: number): string {
  return `/api/scenes/${id}/stream`
}

export function imageSrc(url: string): string {
  return url ? '/api/scenes/image?u=' + encodeURIComponent(url) : ''
}

export const scenesApi = {
  settings: () => call<SceneSettings>('GET', '/settings'),
  saveSettings: (body: { enabled?: boolean; path?: string; endpoint?: string; key?: string; removeKey?: boolean }) => call<SceneSettings>('PUT', '/settings', body),

  list: (filter: { status?: string; performer?: string; studio?: string } = {}) => call<{ counts: SceneCounts; scenes: Scene[] }>('GET', q(filter)),
  get: (id: number) => call<{ scene: Scene; downloading: boolean }>('GET', `/${id}`),
  add: (stashId: string, search: boolean) => call<Scene>('POST', '', { stashId, search }),
  setWanted: (id: number, wanted: boolean) => call<Scene>('PUT', `/${id}/wanted`, { wanted }),
  remove: (id: number) => call<null>('DELETE', `/${id}`),
  searchNow: (id: number) => call<{ grabbed: number; message: string }>('POST', `/${id}/search`),
  releases: (id: number, all: boolean) => call<SceneRelease[]>('GET', `/${id}/releases${all ? '?all=1' : ''}`),
  grab: (id: number, r: SceneRelease) => call<{ queueId: number }>('POST', `/${id}/grab`, { title: r.title, downloadUrl: r.downloadUrl, sizeBytes: r.sizeBytes, protocol: r.protocol }),

  lookupScenes: (text: string, signal?: AbortSignal) => call<{ scenes: LookedUpScene[] }>('GET', '/lookup' + q({ kind: 'scene', q: text }), undefined, signal),
  lookupPerformers: (text: string) => call<{ performers: ScenePerformer[] }>('GET', '/lookup' + q({ kind: 'performer', q: text })),
  lookupStudios: (text: string) => call<{ studios: SceneStudio[] }>('GET', '/lookup' + q({ kind: 'studio', q: text })),
  discover: (params: Record<string, string | number | undefined>, signal?: AbortSignal) =>
    call<{ scenes?: LookedUpScene[]; performers?: ScenePerformer[]; studios?: SceneStudio[]; total: number; page: number; more: boolean; filtered?: boolean }>('GET', '/discover' + q(params), undefined, signal),
  stashTags: (text: string, signal?: AbortSignal) => call<StashTag[]>('GET', '/tags' + q({ q: text }), undefined, signal),
  browse: (kind: 'performer' | 'studio', stashId: string, page: number) => call<{ count: number; scenes: LookedUpScene[] }>('GET', '/browse' + q({ kind, stashId, page })),

  performers: () => call<ScenePerformer[]>('GET', '/performers'),
  // follow answers how many scenes "add all" added to the library.
  follow: (kind: 'performer' | 'studio', stashId: string, backfill: boolean, search = false) => call<{ added: number; searching: number }>('POST', kind === 'performer' ? '/performers' : '/studios', { stashId, backfill, search }),
  unfollow: (kind: 'performer' | 'studio', id: number) => call<null>('DELETE', `${kind === 'performer' ? '/performers' : '/studios'}/${id}`),
  studios: () => call<SceneStudio[]>('GET', '/studios'),
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
