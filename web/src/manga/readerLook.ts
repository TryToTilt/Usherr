// Usherr: how the Manga reader looks on this device: the fit, trimming white borders,
// brightness and dark pages. It belongs to the screen and the room, not the series.
import type { PageFit } from './mangaReading'

export interface ReaderLook {
  fit: PageFit
  trim: boolean
  brightness: number // 30 to 100
  dark: boolean
}

const KEY = 'usherr.manga.look'
const FITS: readonly string[] = ['page', 'width', 'height', 'original']

export function parseLook(raw: string | null): ReaderLook {
  let p: Partial<ReaderLook> = {}
  try {
    p = JSON.parse(raw ?? '{}') as Partial<ReaderLook>
  } catch {
    // an old or broken note: start over
  }
  const brightness = typeof p.brightness === 'number' ? Math.min(100, Math.max(30, p.brightness)) : 100
  return { fit: FITS.includes(p.fit as string) ? (p.fit as PageFit) : 'page', trim: p.trim === true, brightness, dark: p.dark === true }
}

export function storedLook(): ReaderLook {
  try {
    return parseLook(localStorage.getItem(KEY))
  } catch {
    return parseLook(null)
  }
}

export function saveLook(look: ReaderLook) {
  try {
    localStorage.setItem(KEY, JSON.stringify(look))
  } catch {
    // no storage: it lasts until the reader closes
  }
}

// The CSS filter for the pages, or undefined when there's nothing to change. Dark
// pages turn white paper black and leave colours near what they were.
export function lookFilter(look: Pick<ReaderLook, 'brightness' | 'dark'>): string | undefined {
  const parts: string[] = []
  if (look.dark) parts.push('invert(1)', 'hue-rotate(180deg)')
  if (look.brightness < 100) parts.push(`brightness(${look.brightness / 100})`)
  return parts.length ? parts.join(' ') : undefined
}
