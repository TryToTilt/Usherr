// Usherr Broadcast: the remote's pure logic, kept free of the page so it can be
// tested on its own (spatial.test.ts). remote.ts reads the page and calls these.

export type Dir = 'up' | 'down' | 'left' | 'right'

export interface Rect {
  left: number
  top: number
  right: number
  bottom: number
}

/** The gap between two ranges on a line; 0 when they overlap. */
function gap(a0: number, a1: number, b0: number, b1: number): number {
  if (b1 < a0) return a0 - b1
  if (b0 > a1) return b0 - a1
  return 0
}

const EDGE = 2 // a couple of pixels of overlap still counts as "past"

/** Is `c` in direction `dir` from `from`, by its edges (strict) or, failing that, by its centre (loose)? */
export function inDirection(from: Rect, c: Rect, dir: Dir, loose = false): boolean {
  if (loose) {
    const fx = (from.left + from.right) / 2
    const fy = (from.top + from.bottom) / 2
    const cx = (c.left + c.right) / 2
    const cy = (c.top + c.bottom) / 2
    if (dir === 'right') return cx > fx + 1
    if (dir === 'left') return cx < fx - 1
    if (dir === 'down') return cy > fy + 1
    return cy < fy - 1
  }
  if (dir === 'right') return c.left >= from.right - EDGE
  if (dir === 'left') return c.right <= from.left + EDGE
  if (dir === 'down') return c.top >= from.bottom - EDGE
  return c.bottom <= from.top + EDGE
}

/**
 * How far `c` is from `from` going `dir`: lower is better. The distance along
 * the way counts once, being off to the side counts twice plus a fixed step, so
 * something in the same row (or column) wins over something nearer but in another one; a small
 * share of the centres' offset breaks ties (the item straight below wins).
 */
export function score(from: Rect, c: Rect, dir: Dir): number {
  const horizontal = dir === 'left' || dir === 'right'
  let along: number
  if (dir === 'right') along = Math.max(0, c.left - from.right)
  else if (dir === 'left') along = Math.max(0, from.left - c.right)
  else if (dir === 'down') along = Math.max(0, c.top - from.bottom)
  else along = Math.max(0, from.top - c.bottom)
  const side = horizontal ? gap(from.top, from.bottom, c.top, c.bottom) : gap(from.left, from.right, c.left, c.right)
  const centre = horizontal
    ? Math.abs((from.top + from.bottom) / 2 - (c.top + c.bottom) / 2)
    : Math.abs((from.left + from.right) / 2 - (c.left + c.right) / 2)
  // not in the row (column) at all: a fixed step on top, bigger sideways, where rows are what a remote follows
  const outside = side > 0 ? (horizontal ? 300 : 150) : 0
  return along + side * 2 + outside + centre * 0.05
}

/**
 * The index of the candidate to move to from `from` going `dir`, or -1 when
 * there is none that way. Candidates past the edge come first; only when there
 * are none is a candidate whose centre lies that way taken (nested or
 * overlapping things).
 */
export function pickNext(from: Rect, candidates: readonly Rect[], dir: Dir): number {
  // Usherr Broadcast: Left stays in the row, as on a TV: nothing beside it to
  // the left is the left edge, where the menu opens. Right tries the row first.
  if (dir === 'left' || dir === 'right') {
    const sameRow = candidates.map((c) => (c.bottom > from.top + EDGE && c.top < from.bottom - EDGE ? c : null))
    const i = pickAmong(from, sameRow, dir)
    if (i >= 0 || dir === 'left') return i
  }
  return pickAmong(from, candidates, dir)
}

function pickAmong(from: Rect, candidates: readonly (Rect | null)[], dir: Dir): number {
  for (const loose of [false, true]) {
    let best = -1
    let bestScore = Infinity
    candidates.forEach((c, i) => {
      if (!c || !inDirection(from, c, dir, loose)) return
      const s = score(from, c, dir)
      if (s < bestScore) {
        bestScore = s
        best = i
      }
    })
    if (best >= 0) return best
  }
  return -1
}

/** Where to start when nothing has focus: the top-most, then left-most, of the ones in sight. */
export function pickFirst(candidates: readonly Rect[], view: Rect): number {
  let best = -1
  let bestKey = Infinity
  candidates.forEach((c, i) => {
    const visible = c.bottom > view.top && c.top < view.bottom && c.right > view.left && c.left < view.right
    if (!visible) return
    // rows a few pixels apart count as one row
    const key = Math.round(Math.max(c.top, view.top) / 16) * 100000 + c.left
    if (key < bestKey) {
      bestKey = key
      best = i
    }
  })
  return best
}

// ---- Keys ------------------------------------------------------------------

/** What a key press means to the remote. */
export type Intent =
  | { kind: 'move'; dir: Dir }
  | { kind: 'ok' }
  | { kind: 'back'; native: boolean } // native: the browser goes back on its own unless stopped
  | { kind: 'escape' }
  | { kind: 'options' }
  | { kind: 'media'; action: 'playpause' | 'play' | 'pause' | 'fastforward' | 'rewind' | 'next' | 'previous' | 'stop' }
  | { kind: 'none' }

/** The parts of a key event the remote looks at. */
export interface KeyLike {
  key: string
  keyCode?: number
  altKey?: boolean
  ctrlKey?: boolean
  metaKey?: boolean
  shiftKey?: boolean
}

const ARROWS: Record<string, Dir> = { ArrowUp: 'up', ArrowDown: 'down', ArrowLeft: 'left', ArrowRight: 'right', Up: 'up', Down: 'down', Left: 'left', Right: 'right' }
const MEDIA: Record<string, Extract<Intent, { kind: 'media' }>['action']> = {
  MediaPlayPause: 'playpause',
  MediaPlay: 'play',
  MediaPause: 'pause',
  MediaFastForward: 'fastforward',
  MediaRewind: 'rewind',
  MediaTrackNext: 'next',
  MediaTrackPrevious: 'previous',
  MediaStop: 'stop',
}

/** Reads a key press. Modified presses (Ctrl, Alt, Cmd) are the browser's and the page's, never the remote's. */
export function keyIntent(e: KeyLike): Intent {
  if (e.ctrlKey || e.altKey || e.metaKey) return { kind: 'none' }
  const dir = ARROWS[e.key]
  if (dir) return e.shiftKey ? { kind: 'none' } : { kind: 'move', dir }
  if (e.key === 'Enter') return { kind: 'ok' }
  if (e.key === 'Escape' || e.key === 'Esc') return { kind: 'escape' }
  if (e.key === 'Backspace') return { kind: 'back', native: false }
  // a remote's own Back: Android's GoBack, a keyboard's BrowserBack, webOS (461) and Tizen (10009)
  if (e.key === 'GoBack' || e.key === 'BrowserBack' || e.keyCode === 461 || e.keyCode === 10009) return { kind: 'back', native: true }
  if (e.key === 'ContextMenu' || e.key === 'm' || e.key === 'M') return { kind: 'options' }
  const media = MEDIA[e.key]
  if (media) return { kind: 'media', action: media }
  return { kind: 'none' }
}

/** Input types that take typing; the remote never takes keys from these. */
const TYPED = new Set(['', 'text', 'search', 'email', 'url', 'password', 'tel', 'number', 'date', 'time', 'datetime-local', 'month', 'week'])

/** What the focused thing is, as far as the remote cares. */
export type FieldKind = 'typing' | 'slider' | 'select' | 'radio' | 'other'

export function fieldKind(tag: string, type: string | null, editable: boolean): FieldKind {
  if (editable) return 'typing'
  const t = tag.toUpperCase()
  if (t === 'TEXTAREA') return 'typing'
  if (t === 'SELECT') return 'select'
  if (t === 'INPUT') {
    const ty = (type ?? '').toLowerCase()
    if (ty === 'range') return 'slider'
    if (ty === 'radio') return 'radio'
    if (TYPED.has(ty)) return 'typing'
  }
  return 'other'
}

/**
 * Whether the remote should move focus for this arrow, given what has focus.
 * Typing keeps every arrow (the caret is the user's), except that on a TV
 * Up and Down leave a one-line field (nothing else would get the remote out).
 * A slider keeps its arrows only while grabbed; radios keep their own.
 */
export function remoteTakesArrow(kind: FieldKind, dir: Dir, opts: { grabbed: boolean; tv: boolean; multiline: boolean }): boolean {
  if (kind === 'typing') return opts.tv && !opts.multiline && (dir === 'up' || dir === 'down')
  if (kind === 'slider') return !opts.grabbed
  if (kind === 'radio') return false
  return true
}
