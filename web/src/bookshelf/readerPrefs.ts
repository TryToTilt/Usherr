// What a person can set in the ebook reader (Aa), and what each setting does to the
// page. Everything here must reach the curl's picture of a page as well: a setting
// is either a rule in the page's own style sheet (pageCss), which the picture copies,
// or inline on the book's body, which it copies too.
import { reducedMotion, type Turn, type TurnSpeed } from './turn.ts'

export type Theme = 'light' | 'sepia' | 'dark' | 'black'
export type Font = 'original' | 'serif' | 'classic' | 'sans' | 'clear' | 'mono'
export type LineSpacing = 'book' | 'tight' | 'normal' | 'wide'
export type Margin = 'narrow' | 'normal' | 'wide'
export type Justify = 'book' | 'on' | 'off'
// How the ebook reader turns a page. Curl needs a browser that can draw it; where
// it can't, the page slides.
export type Effect = 'curl' | 'slide' | 'none'
// Whole pages that fit the window, or one long column to scroll. Only Scroll ever
// moves up and down.
export type Reading = 'pages' | 'scroll'

export interface Prefs {
  size: number // percent
  theme: Theme
  font: Font
  line: LineSpacing
  margin: Margin
  justify: Justify
  turn: Turn // how a page turn looks; the comic reader reads this one too
  speed: TurnSpeed // how long it takes
  effect?: Effect // the page turn chosen here; not chosen, it follows defaultEffect
  reading: Reading
}

export const PREFS_KEY = 'mediarium.reader'

export const THEMES: Record<Theme, { bg: string; fg: string; link: string; dark: boolean }> = {
  light: { bg: '#fbfaf7', fg: '#1d1c1a', link: '#1f6feb', dark: false },
  sepia: { bg: '#f4ecd8', fg: '#3b2f22', link: '#8a4b14', dark: false },
  dark: { bg: '#15171b', fg: '#d9dde3', link: '#7fb4ff', dark: true },
  // For OLED screens: true black, and a text a little softer than white.
  black: { bg: '#000000', fg: '#c9ccd1', link: '#7fb4ff', dark: true },
}

// Only fonts a device already has: nothing here adds to what the app downloads, and
// a picture of a page can draw them too. 'original' leaves the book's own.
export const FONTS: Record<Font, string> = {
  original: '',
  serif: 'Georgia, "Iowan Old Style", "Palatino Linotype", serif',
  classic: '"Palatino Linotype", Palatino, "Book Antiqua", "Iowan Old Style", serif',
  sans: 'system-ui, -apple-system, "Segoe UI", Roboto, sans-serif',
  clear: 'Verdana, Geneva, "DejaVu Sans", Tahoma, sans-serif',
  mono: 'ui-monospace, "SF Mono", Menlo, Consolas, "DejaVu Sans Mono", monospace',
}

// A scrolled chapter has no columns, so no gap to make its margins from.
const SCROLL_MARGIN: Record<Margin, string> = { narrow: '4%', normal: '8%', wide: '16%' }

const LINE_HEIGHT: Record<LineSpacing, number> = { book: 0, tight: 1.3, normal: 1.55, wide: 1.85 }

const oneOf = <T extends string>(v: unknown, all: readonly T[], otherwise: T): T => (all.includes(v as T) ? (v as T) : otherwise)

// Curl, unless the system asks for less motion or the old setting said no animation.
function defaultEffect(turn: Turn): Effect {
  if (reducedMotion() || turn === 'none') return 'none'
  try {
    if (localStorage.getItem('mediarium.reader.pageturn.off') === '1') return 'none'
  } catch {
    // no storage
  }
  return 'curl'
}
export const effectOf = (p: Prefs): Effect => p.effect ?? defaultEffect(p.turn)

// readPrefs makes sense of whatever was saved. Fade is no longer a page turn here:
// someone who chose it gets the other quiet one, Slide. The saved "turn" stays as it
// is, because the comic reader still offers Fade.
export function readPrefs(saved: unknown): Prefs {
  const p = (saved && typeof saved === 'object' ? saved : {}) as Record<string, unknown>
  const turn = oneOf<Turn>(p.turn, ['none', 'fade', 'slide'], 'slide')
  const effect: Effect | undefined = p.effect === 'curl' || p.effect === 'slide' || p.effect === 'none' ? p.effect : p.effect === 'fade' || turn === 'fade' ? 'slide' : undefined
  return {
    size: Math.min(200, Math.max(70, Number(p.size) || 105)),
    theme: oneOf<Theme>(p.theme, ['light', 'sepia', 'dark', 'black'], 'light'),
    // Readers before "As published" had serif unless they chose otherwise.
    font: oneOf<Font>(p.font, ['original', 'serif', 'classic', 'sans', 'clear', 'mono'], 'serif'),
    line: oneOf<LineSpacing>(p.line, ['book', 'tight', 'normal', 'wide'], 'book'),
    margin: oneOf<Margin>(p.margin, ['narrow', 'normal', 'wide'], 'normal'),
    justify: oneOf<Justify>(p.justify, ['book', 'on', 'off'], 'book'),
    turn,
    speed: p.speed === 'fast' || p.speed === 'slow' ? p.speed : 'normal',
    effect,
    reading: p.reading === 'scroll' ? 'scroll' : 'pages',
  }
}

export function loadPrefs(): Prefs {
  try {
    return readPrefs(JSON.parse(localStorage.getItem(PREFS_KEY) ?? '{}'))
  } catch {
    return readPrefs({})
  }
}

// The gap epub.js keeps between columns, half of it each side of a page. undefined:
// its own choice, a twelfth of the width.
export function gapFor(width: number, margin: Margin): number | undefined {
  if (margin === 'normal') return undefined
  const g = Math.floor(width / (margin === 'narrow' ? 24 : 6))
  return g - (g % 2)
}

// The settings that move words from one page to another.
export const layoutOf = (p: Prefs) => [p.size, p.font, p.line, p.margin, p.justify].join('|')

// Marks the running text that may be aligned: a centred line or a right-aligned
// signature in a book stays as it is.
export const FLOW_MARK = 'data-rdr-flow'

// The blocks that hold running text. Headings and code keep the book's own look.
const TEXT = 'p, li, blockquote, dd, dt, td, th, div'

// pageCss is the page's own style sheet for the settings a book's CSS could otherwise
// beat, as its rules are usually on its paragraphs rather than the body.
export function pageCss(p: Prefs): string {
  const rules: string[] = []
  if (p.reading === 'scroll') rules.push(`body { padding: 20px ${SCROLL_MARGIN[p.margin]} !important; }`)
  if (FONTS[p.font]) rules.push(`${TEXT} { font-family: ${FONTS[p.font]} !important; }`)
  if (LINE_HEIGHT[p.line]) rules.push(`body, ${TEXT} { line-height: ${LINE_HEIGHT[p.line]} !important; }`)
  if (p.justify !== 'book') rules.push(`[${FLOW_MARK}] { text-align: ${p.justify === 'on' ? 'justify' : 'start'} !important; }`)
  return rules.join('\n')
}
