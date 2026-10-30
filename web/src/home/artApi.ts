// Usherr: Re-Posterr, artwork picking (home_artwork.go): the posters, backgrounds and
// logos Usherr picks from TMDB and fanart.tv and puts on Jellyfin's titles.
import { t } from '../i18n'

export type ArtType = 'poster' | 'backdrop' | 'logo'
export type ArtKind = 'movie' | 'tv' | 'collection'

export type ArtSettings = {
  on: boolean
  mode: 'ask' | 'fill' | 'auto'
  fanartKeySet: boolean
  textless: boolean
  from: number
  to: number
  minFreeGB: number
  folder: string
  tvdbKeySet: boolean
  posterizarr: string
  posterizarrOwns: boolean
}
export type ArtStatus = { running: boolean; lastMs?: number; looked: number; proposed: number; applied: number; note?: string; freeGB: number }
export type ArtAll = {
  settings: ArtSettings
  status: ArtStatus
  counts: Record<'titles' | 'looked' | 'missingPoster' | 'missingBackdrop' | 'missingLogo' | 'waiting' | 'done' | 'failed', number>
  jellyfin: boolean
  tmdb: boolean
  folder: string
}
export type ArtCand = { type: ArtType; source: 'tmdb' | 'fanart' | 'tvdb'; url: string; preview: string; width: number; height: number; lang: string; score: number }
// A change puts on a picture as it is, or makes one in the household's style
// (style set): a poster, or a show's season posters or title cards.
export type ArtChangeType = ArtType | 'seasons' | 'cards'
export type ArtChange = {
  id: number
  kind: ArtKind
  tmdbId: number
  title: string
  type: ArtChangeType
  source: string
  url: string
  preview: string
  width: number
  height: number
  lang: string
  why: 'missing' | 'small' | 'chosen' | 'styled'
  state: 'waiting' | 'done' | 'skipped' | 'undone' | 'failed'
  error?: string
  byHand: boolean
  style?: string
  createdMs: number
  doneMs?: number
  current?: string
}
export type ArtTitle = {
  candidates: Record<ArtType, ArtCand[]>
  current: string
  inJellyfin: boolean
  copies: number
  chosen: Record<ArtType, boolean>
  changes: ArtChange[]
}

export const ART_LABEL: Record<ArtChangeType, string> = { poster: t('Poster'), backdrop: t('Background'), logo: t('Logo'), seasons: t('Season posters'), cards: t('Title cards') }

// ---- Re-Posterr's own posters (home_artstyle.go) ----

export type StyleWhat = 'poster' | 'season' | 'card' | 'backdrop'
export type ArtLayerKind = 'fade' | 'band' | 'border' | 'overlay' | 'title'
export type ArtRes = '4k' | '1080' | '720' | 'sd'
export type ArtLayer = {
  kind: ArtLayerKind
  pos?: 'top' | 'middle' | 'bottom'
  color?: string
  opacity?: number
  size?: number
  font?: string
  caps?: boolean
  logo?: boolean
  sub?: boolean
  outline?: number
  shadow?: boolean
  maxLines?: number
  words?: string
  short?: boolean
  file?: string
  byRes?: Partial<Record<ArtRes, string>>
}
export type ArtStyle = { v: number; layers: ArtLayer[] }
export type ArtStyles = Record<StyleWhat, ArtStyle> & { libraries?: Record<string, ArtStyle> }
export type ArtStylesAll = { styles: ArtStyles; on: Partial<Record<StyleWhat, boolean>>; fonts: { id: string; name: string }[]; defaults: ArtStyles; libraries: string[] | null; overlays: string[] }
export type ArtSample = { kind: ArtKind; tmdbId: number; title: string; year: number }

// The picture a change in the style makes (the waiting list's side by side).
export const changeLook = (id: number) => `/api/home/art/changes/${id}/look`

// stylePreview draws the picture a style makes for a title (the saved style
// when none is given) and hands back a URL for an <img>.
export async function stylePreview(b: { what: StyleWhat; style?: ArtStyle; library?: string; kind: ArtKind; tmdbId: number; url?: string; words?: string; res?: ArtRes | '' }, signal?: AbortSignal): Promise<string> {
  const res = await fetch('/api/home/art/styles/preview', { method: 'POST', credentials: 'include', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(b), signal })
  if (!res.ok) {
    const data = await res.json().catch(() => null)
    throw new Error((data as { error?: string } | null)?.error ?? t('Something went wrong (error {status}).', { status: res.status }))
  }
  return URL.createObjectURL(await res.blob())
}

// sendFile sends a picture as a form (overlays, the household's own).
async function sendFile<T>(path: string, file: File): Promise<T> {
  const form = new FormData()
  form.append('file', file)
  const res = await fetch(`/api/home/art${path}`, { method: 'POST', credentials: 'include', body: form })
  const data = await res.json().catch(() => null)
  if (!res.ok) throw new Error((data as { error?: string } | null)?.error ?? t('Something went wrong (error {status}).', { status: res.status }))
  return data as T
}

// The picture Jellyfin has now (small), through Usherr.
export const currentArt = (item: string, type: ArtType) => `/api/home/art/current/${encodeURIComponent(item)}/${type}`

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(`/api/home/art${path}`, {
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

export const artApi = {
  all: () => call<ArtAll>('GET', ''),
  saveSettings: (s: ArtSettings & { fanartKey?: string; removeFanartKey?: boolean; tvdbKey?: string; removeTvdbKey?: boolean }) => call<ArtSettings>('PUT', '/settings', s),
  testFanart: () => call<{ pictures: number }>('POST', '/test-fanart'),
  testTvdb: () => call<{ pictures: number }>('POST', '/test-tvdb'),
  testPosterizarr: () => call<{ ok: boolean }>('POST', '/test-posterizarr'),
  addOverlay: (f: File) => sendFile<{ name: string; overlays: string[] }>('/overlays', f),
  deleteOverlay: (name: string) => call<{ overlays: string[] }>('DELETE', `/overlays/${encodeURIComponent(name)}`),
  upload: (f: File) => sendFile<{ url: string; preview: string }>('/upload', f),
  look: () => call<{ started: boolean }>('POST', '/look'),
  changes: (state: ArtChange['state'], limit = 60) => call<ArtChange[]>('GET', `/changes?state=${state}&limit=${limit}`),
  apply: (b: { ids?: number[]; all?: boolean }) => call<{ changes: number }>('POST', '/changes/apply', b),
  skip: (b: { ids?: number[]; all?: boolean }) => call<{ changes: number }>('POST', '/changes/skip', b),
  undo: (id: number) => call<{ undone: boolean }>('POST', `/changes/${id}/undo`),
  title: (kind: ArtKind, tmdbId: number) => call<ArtTitle>('GET', `/title?kind=${kind}&tmdbId=${tmdbId}`),
  choose: (b: { kind: ArtKind; tmdbId: number; type: ArtType; url: string }) => call<{ id: number }>('POST', '/title/choose', b),
  styles: () => call<ArtStylesAll>('GET', '/styles'),
  saveStyles: (b: { styles: ArtStyles; on: Partial<Record<StyleWhat, boolean>> }) => call<ArtStylesAll>('PUT', '/styles', b),
  samples: (q: string, kind?: ArtKind) => call<ArtSample[]>('GET', `/samples?q=${encodeURIComponent(q)}${kind ? `&kind=${kind}` : ''}`),
  titleStyle: (b: { kind: ArtKind; tmdbId: number; what: 'poster' | 'backdrop' | 'seasons' | 'cards'; url?: string; words?: string }) => call<{ id: number; background?: boolean }>('POST', '/title/style', b),
}

// A picture that won't load: its small preview gives way to the full picture,
// and when that fails too it's left out (onGone), not shown as an empty box.
export function artImgError(e: { currentTarget: HTMLImageElement }, full: string, onGone: () => void) {
  const img = e.currentTarget
  if (full && img.dataset.full !== '1' && img.src !== full) {
    img.dataset.full = '1'
    img.src = full
    return
  }
  onGone()
}

// "2000 × 3000 · English · fanart.tv"
export function candLine(c: { width: number; height: number; lang: string; source: string }): string {
  const parts: string[] = []
  if (c.width) parts.push(`${c.width} × ${c.height}`)
  parts.push(c.lang ? c.lang.toUpperCase() : t('No words'))
  parts.push(c.source === 'fanart' ? 'fanart.tv' : c.source === 'tvdb' ? 'TheTVDB' : 'TMDB')
  return parts.join(' · ')
}
