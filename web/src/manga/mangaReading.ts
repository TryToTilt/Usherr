// Usherr: the arithmetic of the Manga reader: which pages show together, and where
// on the screen. Nothing here touches the page, so it can be tested on its own.
// Pages are 1-based and in reading order.

export type PageSize = [number, number] // width, height; 0, 0 when not known

export const isWide = (s?: PageSize) => !!s && s[1] > 0 && s[0] > s[1] * 1.1

// What shows together. One at a time, or two: the cover alone, then pairs. A wide
// page is a two-page spread already, so it's alone too, and pairing starts again
// after it.
export function spreadsOf(total: number, sizes: readonly PageSize[] | undefined, twoUp: boolean, shift = false): number[][] {
  const out: number[][] = []
  if (!twoUp) {
    for (let p = 1; p <= total; p++) out.push([p])
    return out
  }
  if (total >= 1) out.push([1])
  // Shifted, page 2 is alone too, for a volume whose spreads pair up the other way.
  if (shift && total >= 2) out.push([2])
  let p = shift ? 3 : 2
  while (p <= total) {
    if (!isWide(sizes?.[p - 1]) && p + 1 <= total && !isWide(sizes?.[p])) {
      out.push([p, p + 1])
      p += 2
    } else {
      out.push([p])
      p += 1
    }
  }
  return out
}

export function spreadIndexOf(spreads: readonly number[][], page: number): number {
  const i = spreads.findIndex((s) => s.includes(page))
  if (i >= 0) return i
  return page < 1 ? 0 : Math.max(0, spreads.length - 1)
}

// How tall a page is for its width: the middle one of the volume, leaving out wide
// pages, so one odd page doesn't change the shape of all the others.
export function pageRatio(sizes: readonly PageSize[] | undefined): number {
  const list = (sizes ?? [])
    .filter((s) => s[0] > 0 && s[1] > 0 && !isWide(s))
    .map((s) => s[1] / s[0])
    .sort((a, b) => a - b)
  return list.length ? list[Math.floor(list.length / 2)] : 1.45
}

export interface Box {
  x0: number
  y0: number
  x1: number
  y1: number
}

export interface Placed {
  n: number
  x: number // from the box's corner
  y: number
  w: number
  h: number
}

// fit puts a picture inside a room, whole. side says which way it leans when
// there's room left over: -1 left, 0 middle, 1 right.
function fit(size: PageSize | undefined, ratio: number, x: number, w: number, h: number, side: -1 | 0 | 1): Omit<Placed, 'n'> {
  const r = size && size[0] > 0 && size[1] > 0 ? size[1] / size[0] : ratio
  const pw = Math.min(w, h / r)
  const ph = pw * r
  const left = side < 0 ? x : side > 0 ? x + w - pw : x + (w - pw) / 2
  return { x: left, y: (h - ph) / 2, w: pw, h: ph }
}

// The page's box on the stage: the same for the whole volume, so nothing jumps as
// the pages turn. It holds one page or, with twoUp, two side by side.
export function pageBox(stageW: number, stageH: number, ratio: number, twoUp: boolean): Box {
  const across = twoUp ? 2 : 1
  const pw = Math.max(1, Math.min(stageW / across, stageH / ratio))
  const w = Math.round(pw * across)
  const h = Math.round(pw * ratio)
  const x0 = Math.round((stageW - w) / 2)
  const y0 = Math.round((stageH - h) / 2)
  return { x0, y0, x1: x0 + w, y1: y0 + h }
}

// Where a spread's pages go in the box. Right to left, the first page of a pair is
// on the right. The cover is alone on the side a cover opens on, a page left over
// on the side a first page has.
export function placeSpread(spread: readonly number[], index: number, box: Box, sizes: readonly PageSize[] | undefined, ratio: number, twoUp: boolean, rtl: boolean): Placed[] {
  const w = box.x1 - box.x0
  const h = box.y1 - box.y0
  const size = (n: number) => sizes?.[n - 1]
  if (!twoUp || (spread.length === 1 && isWide(size(spread[0])))) {
    return [{ n: spread[0], ...fit(size(spread[0]), ratio, 0, w, h, 0) }]
  }
  const half = w / 2
  if (spread.length === 1) {
    const onRight = index === 0 ? !rtl : rtl
    return [{ n: spread[0], ...fit(size(spread[0]), ratio, onRight ? half : 0, half, h, onRight ? -1 : 1) }]
  }
  const [first, second] = spread
  const left = rtl ? second : first
  const right = rtl ? first : second
  return [
    { n: left, ...fit(size(left), ratio, 0, half, h, 1) },
    { n: right, ...fit(size(right), ratio, half, half, h, -1) },
  ]
}

// How a page fits when it isn't shown whole: as wide as the screen, as tall as the
// screen, or at its own size. Too big for the screen, it scrolls.
export type PageFit = 'page' | 'width' | 'height' | 'original'

// Where a spread's pages go for a fit other than the whole page. The box is as big
// as the pages come out, in the middle of the stage when they're smaller than it.
export function placeFitted(spread: readonly number[], stageW: number, stageH: number, sizes: readonly PageSize[] | undefined, ratio: number, rtl: boolean, fit: Exclude<PageFit, 'page'>): { box: Box; items: Placed[] } {
  const known = (n: number): PageSize => {
    const s = sizes?.[n - 1]
    return s && s[0] > 0 && s[1] > 0 ? s : [1000, 1000 * ratio]
  }
  const order = rtl ? [...spread].reverse() : [...spread]
  const naturalW = order.reduce((sum, n) => sum + known(n)[0], 0)
  const naturalH = Math.max(...order.map((n) => known(n)[1]))
  const scale = fit === 'width' ? stageW / naturalW : fit === 'height' ? stageH / naturalH : 1
  const w = naturalW * scale
  const h = naturalH * scale
  const x0 = Math.max(0, Math.round((stageW - w) / 2))
  const y0 = Math.max(0, Math.round((stageH - h) / 2))
  let x = 0
  const items = order.map((n) => {
    const [pw, ph] = known(n)
    const item: Placed = { n, x, y: (h - ph * scale) / 2, w: pw * scale, h: ph * scale }
    x += pw * scale
    return item
  })
  return { box: { x0, y0, x1: x0 + Math.round(w), y1: y0 + Math.round(h) }, items }
}

export interface PanelBox {
  x: number
  y: number
  w: number
  h: number
}

// On a screen held upright, a two-page spread with no panels found would be tiny
// shown whole, so it's read a page at a time. Anywhere else it's shown whole.
export function panelsOrHalves(panels: readonly PanelBox[], size: PageSize | undefined, rtl: boolean, upright: boolean): PanelBox[] {
  if (panels.length > 0 || !upright || !isWide(size)) return [...panels]
  const left = { x: 0, y: 0, w: 0.5, h: 1 }
  const right = { x: 0.5, y: 0, w: 0.5, h: 1 }
  return rtl ? [right, left] : [left, right]
}

// How wide a copy of the pages to ask for: the screen's pixels, rounded up to what
// the server keeps, or 0 for the page as it is when the screen is big anyway.
export function pageWidthFor(pagePixels: number): number {
  if (pagePixels <= 0 || pagePixels > 1500) return 0
  return Math.max(600, Math.ceil(pagePixels / 100) * 100)
}

// Reading to the end of a spread counts as reaching its last page.
export function lastOf(spread: readonly number[]): number {
  return Math.max(...spread)
}
