// Usherr: the arithmetic of the Comics reader's reading directions and spreads.
// Nothing here touches the page, so it can be tested on its own. Pages are
// 1-based and in reading order; "forward" is on towards the end of the issue.

export type ReadDirection = 'ltr' | 'rtl'
export type ReaderLayout = 'flip' | 'slide' | 'double' | 'webtoon' | 'panels'

const layouts: readonly string[] = ['flip', 'slide', 'double', 'webtoon', 'panels']

export function isLayout(v: unknown): v is ReaderLayout {
  return typeof v === 'string' && layouts.includes(v)
}

// The arrow keys go the way the comic reads; Space and Page Down are always
// forward, Page Up always back. 0: not a key that turns the page.
export function keyTurn(key: string, rtl: boolean): -1 | 0 | 1 {
  if (key === ' ' || key === 'PageDown') return 1
  if (key === 'PageUp') return -1
  if (key === 'ArrowRight') return rtl ? -1 : 1
  if (key === 'ArrowLeft') return rtl ? 1 : -1
  return 0
}

// A tap: the third of the page behind you goes back, the rest goes forward.
// at is how far across the page it was, 0 to 1 from the left.
export function tapTurn(at: number, rtl: boolean): -1 | 1 {
  return (rtl ? at > 2 / 3 : at < 1 / 3) ? -1 : 1
}

// A swipe pulls the next page in from the side it's on. dx is how far the
// finger moved, left being negative.
export function swipeTurn(dx: number, rtl: boolean): -1 | 1 {
  return dx < 0 === rtl ? -1 : 1
}

// ---- two pages at a time: the cover alone, then pairs (2-3, 4-5, ...) ----

export function spreadOf(page: number, total: number): number[] {
  if (page <= 1) return [1]
  const first = page % 2 === 0 ? page : page - 1
  return first + 1 <= total ? [first, first + 1] : [first]
}

export function spreadEnd(page: number, total: number): number {
  const s = spreadOf(page, total)
  return s[s.length - 1]
}

// The spread after or before this one, as its last page: that way a spread that
// holds the last page counts as reaching it.
export function stepSpread(page: number, d: number, total: number): number {
  const s = spreadOf(page, total)
  const to = d > 0 ? s[s.length - 1] + 1 : s[0] - 1
  if (to < 1) return s[s.length - 1]
  if (to > total) return s[s.length - 1]
  return spreadEnd(to, total)
}

// ---- the page-turn book ----
//
// The page-turn library only reads left to right, and shows the cover alone,
// then pairs. Right to left, the book is the pages in reverse: it opens at its
// back, and reading forward turns the pages the other way, as a manga does.
// With an odd number of pages a blank goes at the very back, or the cover would
// be paired with page 2.

export function bookPad(total: number, rtl: boolean, twoUp: boolean): number {
  return rtl && twoUp && total % 2 === 1 ? 1 : 0
}

export function pageToBook(page: number, total: number, rtl: boolean, pad: number): number {
  const p = Math.max(1, Math.min(page, total))
  return rtl ? pad + total - p : p - 1
}

// The book reports the first of the leaves showing. This returns the furthest
// page showing in reading order, so a spread that holds the last page reaches it.
export function bookToPage(index: number, total: number, rtl: boolean, pad: number, twoUp: boolean): number {
  if (rtl) return index < pad ? total : Math.min(total, pad + total - index)
  if (twoUp && index % 2 === 1 && index + 1 < total) return index + 2
  return index + 1
}

// The book's leaves, front to back: a page number, or 0 for the blank.
export function bookLeaves(total: number, rtl: boolean, pad: number): number[] {
  const pages = Array.from({ length: total }, (_, i) => i + 1)
  if (!rtl) return pages
  return [...Array.from({ length: pad }, () => 0), ...pages.reverse()]
}
