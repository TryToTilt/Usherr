// Home fork: "Stream now" (StreamNZB, through Mediarium). See
// internal/api/home_streaming.go.
import { t } from '../i18n'

export interface StreamStatus {
  ready: boolean
  qualities?: string[]
  problem?: string
  tv?: { name: string } | null // the living-room TV, when playing there is set up
}

export interface StreamOption {
  id: string
  name: string
  title: string
  quality?: string
  codec?: string
}

export interface StreamStart {
  id: string
  videoUrl: string
  title: string
  release: string
  codec?: string
  hdr?: string
  audio?: string
}

async function call<T>(method: string, path: string, body?: unknown, signal?: AbortSignal): Promise<T> {
  const res = await fetch(`/api/stream${path}`, {
    method,
    credentials: 'include',
    headers: body ? { 'Content-Type': 'application/json' } : undefined,
    body: body ? JSON.stringify(body) : undefined,
    signal,
  })
  const data = await res.json().catch(() => null)
  if (!res.ok) throw new Error(data?.error ?? t("Streaming didn't work (error {status}).", { status: res.status }))
  return data as T
}

export const streamApi = {
  status: () => call<StreamStatus>('GET', '/status'),
  options: (p: { kind: 'movie' | 'series'; tmdbId: number; season?: number; episode?: number; quality: string }, signal?: AbortSignal) =>
    call<{ profile: string; options: StreamOption[] }>(
      'GET',
      '/options?' + new URLSearchParams(Object.entries(p).filter(([, v]) => v !== undefined).map(([k, v]) => [k, String(v)])).toString(),
      undefined,
      signal,
    ),
  start: (b: { option?: string; downloadUrl?: string; releaseTitle?: string; title?: string }) => call<StreamStart>('POST', '/start', b),
  link: (id: string) => call<{ homeUrl: string }>('GET', `/${id}/link`),
  // Open a title in Stremio on the TV, or play a started stream there.
  tvTitle: (t: { kind: 'movie' | 'series'; tmdbId: number; season?: number; episode?: number }) => call<{ message: string }>('POST', '/tv', t),
  tvStream: (id: string) => call<{ message: string }>('POST', `/${id}/tv`),
  stop: (id: string) => {
    void fetch(`/api/stream/${id}/stop`, { method: 'POST', credentials: 'include', keepalive: true }).catch(() => undefined)
  },
}

// What a browser can't show without converting: plays at home on the TV,
// or in VLC.
export function browserTrouble(s: { codec?: string; hdr?: string; audio?: string; release?: string }): string {
  const caps = (() => {
    try {
      const ms = typeof MediaSource !== 'undefined' ? MediaSource : null
      const can = (t: string) => !!ms && ms.isTypeSupported(t)
      return { hevc: can('video/mp4; codecs="hvc1.2.4.L153.B0"') || can('video/mp4; codecs="hev1.2.4.L153.B0"'), dv: can('video/mp4; codecs="dvh1.05.06"') }
    } catch {
      return { hevc: false, dv: false }
    }
  })()
  const codec = (s.codec || '').toLowerCase()
  const hdr = (s.hdr || '').toLowerCase()
  const audio = (s.audio || '').toLowerCase()
  if ((codec === 'x265' || codec.includes('265') || codec.includes('hevc')) && !caps.hevc) return t('This browser can’t show HEVC (H.265) video.')
  if ((hdr === 'dv' || hdr.includes('dovi') || hdr.includes('vision')) && !caps.dv) return t('Dolby Vision may show with odd colours here.')
  if (audio.includes('truehd') || audio.includes('dts')) return t('The {audio} sound may be silent in a browser.', { audio: s.audio ?? '' })
  return ''
}

const keyQuality = 'mediarium.stream.quality'
const keyWhere = 'mediarium.stream.where'

export function rememberedWhere(): 'here' | 'tv' {
  try {
    return localStorage.getItem(keyWhere) === 'tv' ? 'tv' : 'here'
  } catch {
    return 'here'
  }
}

export function rememberWhere(w: 'here' | 'tv') {
  try {
    localStorage.setItem(keyWhere, w)
  } catch {
    // no storage
  }
}

export function rememberedQuality(available: string[]): string {
  try {
    const q = localStorage.getItem(keyQuality)
    if (q && available.includes(q)) return q
  } catch {
    // no storage: use the default
  }
  return available.includes('1080p') ? '1080p' : available[0] ?? ''
}

export function rememberQuality(q: string) {
  try {
    localStorage.setItem(keyQuality, q)
  } catch {
    // no storage: nothing to remember
  }
}

// Whether streaming is set up, asked once every couple of minutes, so a
// "Stream" button only shows where it can work (home fork: Settings > Extras).
let streamingOnAsk: { at: number; on: Promise<boolean> } | null = null
export function streamingOn(): Promise<boolean> {
  if (!streamingOnAsk || Date.now() - streamingOnAsk.at > 120_000) {
    streamingOnAsk = { at: Date.now(), on: streamApi.status().then((s) => s.ready, () => false) }
  }
  return streamingOnAsk.on
}
