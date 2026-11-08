// Usherr: the reading options both picture readers share, the Manga reader and the
// Comics reader: the header, the progress bar (its style, side and size), the page
// number when the bar is hidden, tap and cursor hints, greyscale, dim pages and the
// background. They're remembered per series, on this device: a choice made in one
// series becomes the starting point for series not chosen for yet. Out of the box
// every reader looks and works as it always has. Nothing here touches the page, so
// it can be tested on its own.

export type ProgressStyle = 'hidden' | 'thin' | 'normal'
export type ProgressSide = 'bottom' | 'left' | 'right'
export type HintStyle = 'none' | 'overlay' | 'cursor'
export type ReaderBackground = 'reader' | 'theme' | 'white' | 'black'

export interface ReaderOptions {
  header: boolean // the bar along the top
  progress: ProgressStyle
  side: ProgressSide
  size: number // the bar's thickness, 1 (thinnest) to 4
  pageNumber: boolean // a small page number when the bar is hidden
  hints: HintStyle
  greyscale: boolean
  dim: boolean
  background: ReaderBackground
}

export const DEFAULT_OPTIONS: ReaderOptions = { header: true, progress: 'normal', side: 'bottom', size: 2, pageNumber: true, hints: 'overlay', greyscale: false, dim: false, background: 'reader' }

// The Comics reader never showed where to tap, so out of the box it still doesn't.
export const COMICS_OPTIONS: ReaderOptions = { ...DEFAULT_OPTIONS, hints: 'none' }

const PROGRESS: readonly string[] = ['hidden', 'thin', 'normal']
const SIDES: readonly string[] = ['bottom', 'left', 'right']
const HINTS: readonly string[] = ['none', 'overlay', 'cursor']
const BACKGROUNDS: readonly string[] = ['reader', 'theme', 'white', 'black']

// parseOptions reads a stored note, keeping only what makes sense; anything
// missing is the reader's own default.
export function parseOptions(raw: string | null, base: ReaderOptions = DEFAULT_OPTIONS): ReaderOptions {
  let p: Partial<Record<keyof ReaderOptions, unknown>> = {}
  try {
    const v = JSON.parse(raw ?? '{}') as unknown
    if (v && typeof v === 'object') p = v as typeof p
  } catch {
    // an old or broken note: start over
  }
  const pick = <T extends string>(v: unknown, list: readonly string[], fallback: T): T => (typeof v === 'string' && list.includes(v) ? (v as T) : fallback)
  const flag = (v: unknown, fallback: boolean) => (typeof v === 'boolean' ? v : fallback)
  const size = typeof p.size === 'number' && Number.isFinite(p.size) ? Math.min(4, Math.max(1, Math.round(p.size))) : base.size
  return {
    header: flag(p.header, base.header),
    progress: pick(p.progress, PROGRESS, base.progress),
    side: pick(p.side, SIDES, base.side),
    size,
    pageNumber: flag(p.pageNumber, base.pageNumber),
    hints: pick(p.hints, HINTS, base.hints),
    greyscale: flag(p.greyscale, base.greyscale),
    dim: flag(p.dim, base.dim),
    background: pick(p.background, BACKGROUNDS, base.background),
  }
}

const KEY = 'usherr.reader.options'

function read(key: string): string | null {
  try {
    return localStorage.getItem(key)
  } catch {
    return null
  }
}

// storedOptions are a series' options: its own, else the last ones chosen
// anywhere, else the reader's defaults. scope names the series, like "manga:12".
export function storedOptions(scope: string, base: ReaderOptions = DEFAULT_OPTIONS): ReaderOptions {
  const own = scope ? read(`${KEY}.${scope}`) : null
  return parseOptions(own ?? read(KEY), base)
}

export function saveOptions(scope: string, opts: ReaderOptions) {
  try {
    const v = JSON.stringify(opts)
    if (scope) localStorage.setItem(`${KEY}.${scope}`, v)
    localStorage.setItem(KEY, v)
  } catch {
    // no storage: it lasts until the reader closes
  }
}

// optionsFilter is the CSS filter the options add to the pages, or "".
export function optionsFilter(o: Pick<ReaderOptions, 'greyscale' | 'dim'>): string {
  const parts: string[] = []
  if (o.greyscale) parts.push('grayscale(1)')
  if (o.dim) parts.push('brightness(0.7)')
  return parts.join(' ')
}

// joinFilters puts two filters together, either of which may be missing.
export function joinFilters(...f: (string | undefined)[]): string | undefined {
  const out = f.filter((x) => !!x).join(' ')
  return out || undefined
}

// backgroundColor is the colour behind the pages; undefined keeps the reader's own.
export function backgroundColor(b: ReaderBackground): string | undefined {
  switch (b) {
    case 'theme':
      return 'var(--bg, #111)'
    case 'white':
      return '#ffffff'
    case 'black':
      return '#000000'
  }
  return undefined
}

// progressShown says what the bar shows: nothing, a line, or the bar you can drag.
// The page number appears in a corner only when there's no bar and it's wanted.
export function progressParts(o: Pick<ReaderOptions, 'progress' | 'pageNumber'>): { bar: boolean; slider: boolean; number: boolean } {
  return { bar: o.progress !== 'hidden', slider: o.progress === 'normal', number: o.progress === 'hidden' && o.pageNumber }
}

// ticksFor are where the page ticks go on the normal bar, as fractions; none for a
// book so long they'd be a smear.
export function ticksFor(total: number): number[] {
  if (total < 2 || total > 120) return []
  return Array.from({ length: total }, (_, i) => i / (total - 1))
}

// isHeaderKey says whether a key toggles the header.
export function isHeaderKey(key: string): boolean {
  return key === 'm' || key === 'M'
}
