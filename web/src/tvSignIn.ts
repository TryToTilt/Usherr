// Usherr: Sign in on a TV, Finish on your phone and the on-screen keyboard.
// The pure parts, kept apart so they can be tested without a browser.

// The letters a code is made of (internal/api/home_tv_signin.go): no 0 O, 1 I L or U.
export const TV_CODE_ALPHABET = 'ABCDEFGHJKMNPQRSTVWXYZ23456789'
export const TV_CODE_LENGTH = 8

/** "ABCDEFGH" as it is shown: "ABCD-EFGH". */
export function formatTVCode(code: string): string {
  const c = code.toUpperCase()
  return c.length === TV_CODE_LENGTH ? `${c.slice(0, 4)}-${c.slice(4)}` : c
}

/** What someone typed ("abcd efgh") as a code, or '' while it isn't one yet. */
export function normalizeTVCode(typed: string): string {
  const c = typed.toUpperCase().replace(/[\s-]/g, '')
  if (c.length !== TV_CODE_LENGTH) return ''
  for (const ch of c) if (!TV_CODE_ALPHABET.includes(ch)) return ''
  return c
}

/** Whole seconds until expiresAt (never below 0). */
export function secondsLeft(expiresAt: string, now: number): number {
  const end = Date.parse(expiresAt)
  if (Number.isNaN(end)) return 0
  return Math.max(0, Math.ceil((end - now) / 1000))
}

/** 299 → "4:59". */
export function clockText(seconds: number): string {
  const s = Math.max(0, Math.floor(seconds))
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`
}

/**
 * A path of this app a phone may be sent to: starts with one "/", no address,
 * no backslash, no "..". The server checks it again (with its own list of
 * pages); this only keeps the button from offering what would be refused.
 */
export function isAppPath(path: string): boolean {
  if (!path.startsWith('/') || path.startsWith('//') || path.length > 512) return false
  if (/[\s\\"<>`]/.test(path) || /[^\x21-\x7e]/.test(path)) return false
  const bare = path.split(/[?#]/)[0]
  return !bare.split('/').some((seg) => seg === '..' || seg === '.')
}

// ---- The on-screen keyboard ----

/** The letter rows, top to bottom. The last row holds the action keys (see OnScreenKeyboard). */
export const KEY_ROWS: string[][] = [
  ['a', 'b', 'c', 'd', 'e', 'f', 'g'],
  ['h', 'i', 'j', 'k', 'l', 'm', 'n'],
  ['o', 'p', 'q', 'r', 's', 't', 'u'],
  ['v', 'w', 'x', 'y', 'z', '-', "'"],
  ['1', '2', '3', '4', '5', '6', '7'],
  ['8', '9', '0', '.', '&', ':', '!'],
]

/**
 * Where an arrow key goes in a grid whose rows have these lengths. Up and down
 * keep the column (or the nearest one in a shorter row); the edges stop it.
 * Returns null at an edge, so the caller can let focus leave the keyboard.
 */
export function gridMove(rowLengths: number[], row: number, col: number, key: string): [number, number] | null {
  const clampCol = (r: number) => Math.min(col, rowLengths[r] - 1)
  switch (key) {
    case 'ArrowLeft':
      return col > 0 ? [row, col - 1] : null
    case 'ArrowRight':
      return col < rowLengths[row] - 1 ? [row, col + 1] : null
    case 'ArrowUp':
      return row > 0 ? [row - 1, clampCol(row - 1)] : null
    case 'ArrowDown':
      return row < rowLengths.length - 1 ? [row + 1, clampCol(row + 1)] : null
  }
  return null
}

export const RECENT_SEARCHES_KEY = 'usherr-recent-searches'
const RECENT_MAX = 8

/** list with q at the front (once, case ignored), at most eight. Blank q changes nothing. */
export function addRecent(list: string[], q: string): string[] {
  const v = q.trim()
  if (!v) return list
  return [v, ...list.filter((x) => x.toLowerCase() !== v.toLowerCase())].slice(0, RECENT_MAX)
}

/** The recent searches kept in this browser ([] when storage is blocked or the value is damaged). */
export function readRecent(): string[] {
  try {
    const v: unknown = JSON.parse(localStorage.getItem(RECENT_SEARCHES_KEY) ?? '[]')
    return Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string').slice(0, RECENT_MAX) : []
  } catch {
    return []
  }
}

/** Remembers a search in this browser. */
export function rememberSearch(q: string): string[] {
  const next = addRecent(readRecent(), q)
  try {
    localStorage.setItem(RECENT_SEARCHES_KEY, JSON.stringify(next))
  } catch {
    // not kept: storage blocked
  }
  return next
}
