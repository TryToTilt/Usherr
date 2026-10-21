// Usherr: what the Marquee is sent (GET /api/home/marquee), its settings, and
// small helpers every look shares.
import { locale, t } from '../i18n'
import type { CalendarEntry } from '../api'
import type { Showtime } from './showtimes'

export interface Playing {
  title: string
  series?: string
  season?: number
  episode?: number
  kind: 'movie' | 'episode' | 'other'
  year?: number
  progress: number
  paused: boolean
  device?: string
  playMethod?: string // "DirectPlay", "DirectStream" or "Transcode" (Jellyfin)
  runtime?: number // minutes
  posterUrl?: string
  backdropUrl?: string
  // What the picture and sound are, and the info panel's details (phase 3).
  resolution?: string
  hdr?: string
  audio?: string
  channels?: string
  aspect?: string // Usherr: "2.39:1", "1.85:1", "16:9"
  formats?: string[] // Usherr: "IMAX", "Dolby Cinema", "70mm"
  overview?: string
  genres?: string[]
  rated?: string
  rating?: number
  cast?: Person[]
  director?: Person
}
export interface Person {
  id: number
  name: string
  role?: string
  photo?: string
}
export interface Spotlight extends Person {
  known: 'Directing' | 'Acting'
  bio?: string
  films: { tmdbId: number; title: string; year?: number; posterUrl: string; have: boolean }[]
}
export interface Tonight {
  number: number
  name: string
  logo?: string
  shows: { title: string; subtitle?: string; start: string; end: string; posterUrl?: string }[]
}
export interface MediaItem {
  id: number
  place: 'slide' | 'preshow'
  kind: 'image' | 'video' | 'web' | 'text'
  title: string
  body?: string
  url?: string
  fileUrl?: string
  size?: number
  seconds: number
  enabled: boolean
  sort: number
}
// ownLabel names the settings a screen keeps of its own ("look, text size"),
// from their JSON names (Usherr).
const OWN_LABELS: Record<string, () => string> = {
  style: () => t('look'),
  theme: () => t('theme'),
  palette: () => t('colours'),
  font: () => t('font'),
  frame: () => t('frame'),
  textScale: () => t('text size'),
  posterScale: () => t('poster size'),
  scenes: () => t('scenes'),
  trailerFit: () => t('trailer fill'),
  trailerSound: () => t('trailer sound'),
  badges: () => t('badges'),
  footer: () => t('footer'),
  quietOn: () => t('quiet hours'),
  quietFrom: () => t('quiet hours'),
  quietTo: () => t('quiet hours'),
}
export function ownLabel(keys: string[] | undefined): string {
  const names = [...new Set((keys ?? []).map((k) => OWN_LABELS[k]?.()).filter(Boolean) as string[])]
  const other = (keys ?? []).filter((k) => !OWN_LABELS[k]).length
  if (other) names.push(other === 1 ? t('1 other setting') : t('{n} other settings', { n: other }))
  return names.join(', ')
}

export interface Feature {
  tmdbId: number
  title: string
  year?: number
  tagline?: string
  runtime?: number
  rating?: number
  rated?: string
  genres?: string[]
  posterUrl?: string
  backdropUrl?: string
  trailer?: string
  reason: 'new' | 'theaters' | 'coming'
  formats?: string[] // Usherr: how cinemas showed it ("IMAX", "Dolby Cinema", "70mm")
  releaseDate?: string
  overview?: string
  cast?: Person[]
  director?: Person
  directorFilms?: string[]
  budget?: number
  revenue?: number
  studio?: string
  country?: string
  collection?: string
  originalTitle?: string
  themeUrl?: string
}
export interface Arrival {
  kind: string
  title: string
  year?: number
  posterUrl?: string
  at: string
  tmdbId?: number
  seriesId?: number
}
export type SceneKind = 'playing' | 'new' | 'trailer' | 'theaters-film' | 'arrivals' | 'coming-film' | 'theaters' | 'coming' | 'tvnow' | 'tonight' | 'spotlight' | 'trivia' | 'slides' | 'preshow'
export interface MarqueeSettings {
  scenes: { kind: SceneKind; on: boolean; seconds: number }[]
  showIcon: boolean
  showName: boolean
  showClock: boolean
  signMode: 'auto' | 'custom'
  signLine1: string
  signLine2: string
  textScale: number
  posterScale: number
  scrollSpeed: 'slow' | 'normal' | 'fast'
  trailerSound: boolean
  trailerFit?: 'letterbox' | 'poster' | 'fill'
  noImdb?: boolean // Usherr: cinema formats from TMDB's notes only
  fromJellyfin: boolean
  fromStreamNZB: boolean
  hideUsherr?: boolean
  profiles: string[]
  quietOn: boolean
  quietFrom: string
  quietTo: string
  quietMode: 'clock' | 'black'
  palette: string
  // The look (phase 2).
  style: MarqueeStyle
  theme: MarqueeTheme
  accent: string
  background: string
  corners: 'square' | 'soft' | 'round'
  font: MarqueeFont
  frame: 'bulbs' | 'gold' | 'neon' | 'none'
  bulbSize: number
  framing: 'fill' | 'letterbox' | 'matted'
  transition: MarqueeTransition
  transitionMs: number
  kenBurns: boolean
  grain: boolean
  vintage: boolean
  ambilight: boolean
  footer: 'details' | 'title' | 'tagline' | 'none'
  wallLayout: 'grid' | 'hero'
  wallColumns: number
  wallSwap: number
  playingFirst: boolean
  nudge: boolean
  nightDimOn: boolean
  nightFrom: string
  nightTo: string
  nightLevel: number
  // What it shows (phase 3).
  badges: 'on' | 'off'
  infoPanel: 'off' | 'always' | 'held'
  qr: 'off' | 'trailer' | 'usherr'
  preShowOnStart: boolean
  preShowBuiltins: PreShowPiece[]
  preShowTrailers: number
  themeMusic: boolean
  themeVolume: number
}
export type PreShowPiece = 'popcorn' | 'silence' | 'coming' | 'countdown' | 'feature'
export type MarqueeStyle = 'usherr' | 'cinema' | 'wall' | 'screensaver' | 'board' | 'posterr' | 'onesheet'
export type MarqueeTheme = 'usherr' | 'deco' | 'noir' | 'neon' | 'ivory' | 'midnight' | 'drivein' | 'velvet'
export type MarqueeFont = 'classic' | 'deco' | 'neon' | 'poster' | 'roman' | 'retro' | 'bold' | 'condensed' | 'elegant' | 'modern'
export type MarqueeTransition = 'fade' | 'slide' | 'push' | 'zoom' | 'flip' | 'rotate' | 'blur' | 'cut' | 'random'

export interface Data {
  nowPlaying: Playing[]
  features: Feature[]
  arrivals: Arrival[]
  theaters: Feature[]
  coming: CalendarEntry[]
  theatre?: string
  settings: MarqueeSettings
  tvNow?: { number: number; name: string; logo?: string; title: string; subtitle?: string; progress: number; next?: string; nextAt?: string }[] // Usherr: Live TV
  tonight?: Tonight[]
  spotlights?: Spotlight[]
  media?: MediaItem[]
  themes?: Record<string, string>
  showtimes?: Showtime[] // Usherr: films booked to start soon, for the countdown
}

export const hours = (min?: number) => (min ? `${Math.floor(min / 60)}h ${min % 60}m` : '')

export function dayLabel(date: string): string {
  const d = new Date(`${date}T00:00:00`)
  const today = new Date()
  today.setHours(0, 0, 0, 0)
  const diff = Math.round((d.getTime() - today.getTime()) / 86400000)
  if (diff === 0) return t('Today')
  if (diff === 1) return t('Tomorrow')
  return d.toLocaleDateString(locale(), { weekday: 'long', month: 'short', day: 'numeric' })
}

export function typeLabel(e: CalendarEntry): string {
  switch (e.kind) {
    case 'episode':
      return e.subtitle?.split(' · ')[0] ?? t('Episode')
    case 'album':
      return t('Album')
    case 'book':
      return t('Book')
    case 'comic':
      return t('Comic')
    default:
      return e.status === 'theaters' ? t('In theaters') : t('Movie')
  }
}

// Quiet hours (and the night dimming): "23:30" to "14:00" runs over midnight.
export function inQuiet(now: Date, from: string, to: string): boolean {
  const m = now.getHours() * 60 + now.getMinutes()
  const [fh, fm] = from.split(':').map(Number)
  const [th, tm] = to.split(':').map(Number)
  const a = fh * 60 + fm
  const b = th * 60 + tm
  return a <= b ? m >= a && m < b : m >= a || m < b
}

