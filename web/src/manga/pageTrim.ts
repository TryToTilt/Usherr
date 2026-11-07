// Usherr: trimming the blank borders off a scanned page, here in the browser. A small
// copy is looked at to find where the page's content is, and only a page that really
// has borders is cut and made again. The same picture then serves the page and the
// curl, as a PDF's drawn pages do.

const WHITE = 225
const BLACK = 30
const NEARLY_ALL = 0.985 // specks of dust don't stop a line being blank
const MOST_FROM_A_SIDE = 0.35
const LEAST_LEFT = 0.4 // a mostly blank page is meant to be: it stays whole
const NOT_WORTH_IT = 0.015

export interface Crop {
  x: number
  y: number
  w: number
  h: number
}

// trimBox finds the content of a picture, from its RGBA pixels. A line is blank when
// it's nearly all white or nearly all black. Null: nothing worth cutting.
export function trimBox(data: ArrayLike<number>, w: number, h: number): Crop | null {
  if (w < 8 || h < 8) return null
  const shade = (x: number, y: number) => {
    const i = (y * w + x) * 4
    return (data[i] * 299 + data[i + 1] * 587 + data[i + 2] * 114) / 1000
  }
  const blank = (count: number, at: (k: number) => number) => {
    let white = 0
    let black = 0
    for (let k = 0; k < count; k++) {
      const v = at(k)
      if (v >= WHITE) white++
      else if (v <= BLACK) black++
    }
    return white >= count * NEARLY_ALL || black >= count * NEARLY_ALL
  }
  const row = (y: number) => blank(w, (x) => shade(x, y))
  const col = (x: number) => blank(h, (y) => shade(x, y))
  let top = 0
  while (top < h * MOST_FROM_A_SIDE && row(top)) top++
  let bottom = h
  while (h - bottom < h * MOST_FROM_A_SIDE && row(bottom - 1)) bottom--
  let left = 0
  while (left < w * MOST_FROM_A_SIDE && col(left)) left++
  let right = w
  while (w - right < w * MOST_FROM_A_SIDE && col(right - 1)) right--
  const cw = right - left
  const ch = bottom - top
  if (cw * ch < w * h * LEAST_LEFT) return null
  if (Math.max(left, w - right) < w * NOT_WORTH_IT && Math.max(top, h - bottom) < h * NOT_WORTH_IT) return null
  return { x: left, y: top, w: cw, h: ch }
}

export interface TrimmedPage {
  url: string // the original when nothing was cut
  w: number // the cut picture's size; 0 when nothing was cut
  h: number
}

// trimPage answers for one page. Anything going wrong leaves the page as it was.
export async function trimPage(url: string): Promise<TrimmedPage> {
  const whole: TrimmedPage = { url, w: 0, h: 0 }
  try {
    const r = await fetch(url, { credentials: 'include' })
    if (!r.ok) return whole
    const bmp = await createImageBitmap(await r.blob())
    const scale = Math.min(1, 300 / Math.max(bmp.width, bmp.height))
    const small = document.createElement('canvas')
    small.width = Math.max(1, Math.round(bmp.width * scale))
    small.height = Math.max(1, Math.round(bmp.height * scale))
    const sctx = small.getContext('2d', { willReadFrequently: true })
    if (!sctx) return whole
    sctx.drawImage(bmp, 0, 0, small.width, small.height)
    const box = trimBox(sctx.getImageData(0, 0, small.width, small.height).data, small.width, small.height)
    if (!box) {
      bmp.close()
      return whole
    }
    const k = 1 / scale
    const cut = document.createElement('canvas')
    cut.width = Math.round(box.w * k)
    cut.height = Math.round(box.h * k)
    const ctx = cut.getContext('2d')
    if (!ctx) return whole
    ctx.drawImage(bmp, -box.x * k, -box.y * k)
    bmp.close()
    const blob = await new Promise<Blob | null>((done) => cut.toBlob(done, 'image/jpeg', 0.92))
    return blob ? { url: URL.createObjectURL(blob), w: cut.width, h: cut.height } : whole
  } catch {
    return whole
  }
}
