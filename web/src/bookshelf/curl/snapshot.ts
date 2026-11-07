// A picture of a chapter, for the page curl. An EPUB chapter is live text in an
// iframe, which can't be drawn on a curved surface, but the browser can draw the
// same document as an SVG picture (foreignObject), with its own layout engine,
// so the text lands where the live text is. An SVG picture may load nothing from
// outside itself, so fonts and images go into it as data. One picture holds the
// whole chapter; each page is a crop of it.

export interface ChapterPicture {
  img: HTMLImageElement
  width: number // CSS pixels, the whole laid-out chapter
  height: number
  scale: number // picture pixels per CSS pixel
}

const XHTML = 'http://www.w3.org/1999/xhtml'
// A chapter that takes more than this to write out is left to the plain turn.
const MAX_BYTES = 48 * 1024 * 1024
// Pictures bigger than this are shrunk to about the page's size first.
const SHRINK_OVER = 512 * 1024

const asDataUrl = (blob: Blob) =>
  new Promise<string>((resolve, reject) => {
    const r = new FileReader()
    r.onload = () => resolve(String(r.result))
    r.onerror = () => reject(r.error ?? new Error('read'))
    r.readAsDataURL(blob)
  })

// How to get a file of the book by its address. Not fetch: the app's security rules
// (connect-src) don't let a page fetch a blob: address.
export type FileReaderFn = (url: string) => Promise<Blob>

// What each address inside the book turned into; a book's fonts and pictures don't change.
class Inliner {
  private seen = new Map<string, Promise<string>>()
  bytes = 0

  private read: FileReaderFn

  constructor(read: FileReaderFn) {
    this.read = read
  }

  data(url: string): Promise<string> {
    if (url.startsWith('data:')) return Promise.resolve(url)
    const known = this.seen.get(url)
    if (known) return known
    const p = this.read(url).then((b) => this.count(b).then(asDataUrl))
    this.seen.set(url, p)
    return p
  }

  private async count(b: Blob) {
    this.bytes += b.size
    if (this.bytes > MAX_BYTES) throw new Error('chapter too big for a picture')
    return b
  }

  // image is data(), for a picture: a big one is shrunk to what a page can show.
  image(url: string, maxW: number, maxH: number): Promise<string> {
    const key = `img:${url}`
    const known = this.seen.get(key)
    if (known) return known
    const p = (async () => {
      if (url.startsWith('data:')) return url
      const blob = await this.read(url)
      if (blob.size <= SHRINK_OVER || blob.type.includes('svg')) return this.count(blob).then(asDataUrl)
      const bmp = await createImageBitmap(blob)
      const k = Math.min(1, maxW / bmp.width, maxH / bmp.height)
      const c = document.createElement('canvas')
      c.width = Math.max(1, Math.round(bmp.width * k))
      c.height = Math.max(1, Math.round(bmp.height * k))
      c.getContext('2d')!.drawImage(bmp, 0, 0, c.width, c.height)
      bmp.close()
      const out = c.toDataURL(blob.type === 'image/jpeg' ? 'image/jpeg' : 'image/webp', 0.86)
      this.bytes += out.length
      return out
    })()
    this.seen.set(key, p)
    return p
  }
}

const familyOf = (s: string) => s.replace(/^['"]|['"]$/g, '').toLowerCase()

async function replaceUrls(css: string, base: string, inline: Inliner): Promise<string> {
  const found = [...css.matchAll(/url\(\s*(['"]?)([^'")]+)\1\s*\)/g)]
  if (found.length === 0) return css
  const out = new Map<string, string>()
  await Promise.all(
    found.map(async (m) => {
      const raw = m[2]
      if (raw.startsWith('#') || out.has(raw)) return
      out.set(raw, '')
      try {
        out.set(raw, await inline.data(new URL(raw, base).href))
      } catch {
        out.set(raw, '') // a missing file: as the live page, without it
      }
    }),
  )
  return css.replace(/url\(\s*(['"]?)([^'")]+)\1\s*\)/g, (whole, _q, raw: string) => {
    const v = out.get(raw)
    return v === undefined ? whole : v ? `url("${v}")` : 'none'
  })
}

function ruleText(rule: CSSRule, used: Set<string>, parts: string[]) {
  if (rule.type === CSSRule.IMPORT_RULE) {
    const sheet = (rule as CSSImportRule).styleSheet
    if (sheet) for (const r of Array.from(sheet.cssRules)) ruleText(r, used, parts)
    return
  }
  if (rule.type === CSSRule.FONT_FACE_RULE) {
    // Only the fonts this chapter really uses: a big font is a big picture.
    const family = familyOf((rule as CSSFontFaceRule).style.getPropertyValue('font-family'))
    if (!used.has(family)) return
  }
  parts.push(rule.cssText)
}

async function pageStyles(doc: Document, inline: Inliner): Promise<string> {
  const used = new Set<string>()
  doc.fonts.forEach((f) => {
    if (f.status === 'loaded') used.add(familyOf(f.family))
  })
  const chunks: string[] = []
  for (const sheet of Array.from(doc.styleSheets)) {
    const parts: string[] = []
    for (const r of Array.from(sheet.cssRules)) ruleText(r, used, parts)
    chunks.push(await replaceUrls(parts.join('\n'), sheet.href ?? doc.baseURI, inline))
  }
  // :root in a picture is the SVG, not the page's own root.
  return chunks.join('\n').replace(/:root\b/g, 'html')
}

async function inlineImages(src: HTMLElement, copy: HTMLElement, width: number, height: number, inline: Inliner) {
  const imgs = Array.from(src.querySelectorAll('img'))
  const out = Array.from(copy.querySelectorAll('img'))
  await Promise.all(
    imgs.map(async (img, i) => {
      const url = img.currentSrc || img.src
      const to = out[i]
      if (!url) return
      // Whatever becomes of the picture, its box stays as it is on the page.
      const box = img.getBoundingClientRect()
      if (box.width > 0 && box.height > 0) {
        to.style.setProperty('width', `${box.width}px`, 'important')
        to.style.setProperty('height', `${box.height}px`, 'important')
      }
      try {
        to.setAttribute('src', await inline.image(url, Math.ceil(width * 2), Math.ceil(height * 2)))
      } catch {
        to.removeAttribute('src')
      }
      to.removeAttribute('srcset')
    }),
  )
  const svgImgs = Array.from(copy.querySelectorAll('image'))
  await Promise.all(
    svgImgs.map(async (im) => {
      const url = im.getAttribute('href') ?? im.getAttributeNS('http://www.w3.org/1999/xlink', 'href')
      if (!url) return
      try {
        const data = await inline.image(new URL(url, src.ownerDocument.baseURI).href, Math.ceil(width * 2), Math.ceil(height * 2))
        im.setAttribute('href', data)
        im.setAttributeNS('http://www.w3.org/1999/xlink', 'xlink:href', data)
      } catch {
        im.remove()
      }
    }),
  )
}

// Highlights are drawn by epub.js in an SVG laid over the iframe.
function overlayOf(frame: HTMLIFrameElement): string {
  const pane = frame.parentElement?.querySelector<SVGSVGElement>(':scope > svg')
  if (!pane || pane.childElementCount === 0) return ''
  const a = pane.getBoundingClientRect()
  const b = frame.getBoundingClientRect()
  const copy = pane.cloneNode(true) as SVGSVGElement
  copy.setAttribute('xmlns', 'http://www.w3.org/2000/svg')
  copy.setAttribute('x', String(a.left - b.left))
  copy.setAttribute('y', String(a.top - b.top))
  copy.setAttribute('width', String(a.width))
  copy.setAttribute('height', String(a.height))
  return new XMLSerializer().serializeToString(copy)
}

async function loadImage(src: string): Promise<HTMLImageElement> {
  const img = new Image()
  await new Promise<void>((resolve, reject) => {
    img.onload = () => resolve()
    img.onerror = () => reject(new Error('picture'))
    img.src = src
  })
  await img.decode().catch(() => undefined)
  return img
}

const isBlink = () => /(Chrome|Chromium|HeadlessChrome)\//.test(navigator.userAgent)

export async function pictureOfChapter(frame: HTMLIFrameElement, scale: number, read: FileReaderFn): Promise<ChapterPicture> {
  const doc = frame.contentDocument
  if (!doc?.body) throw new Error('chapter not there')
  await doc.fonts.ready
  await Promise.all(Array.from(doc.images).map((i) => i.decode().catch(() => undefined)))
  const width = frame.offsetWidth
  const height = frame.offsetHeight
  const view = frame.parentElement?.parentElement
  const pageW = view?.clientWidth || width
  const inline = new Inliner(read)
  const css = await pageStyles(doc, inline)

  const body = doc.body.cloneNode(true) as HTMLElement
  for (const el of Array.from(body.querySelectorAll('script, iframe, object, embed, video, audio, link'))) el.remove()
  await inlineImages(doc.body, body, pageW, height, inline)
  // In the iframe these overflow out of the window; here nothing should clip.
  body.style.setProperty('overflow', 'visible', 'important')
  const root = doc.documentElement.cloneNode(false) as HTMLElement
  // Chrome lays a page out in screen pixels, rounding as it goes, and a picture thinks
  // its screen is 1x. Small roundings add up to a line more or less on a page, so the
  // picture is laid out at the screen's own zoom to come out the same.
  const zoom = isBlink() ? window.devicePixelRatio || 1 : 1
  if (zoom !== 1) root.style.setProperty('zoom', String(zoom))
  root.style.setProperty('overflow', 'visible', 'important')
  root.style.setProperty('width', `${width}px`)
  root.style.setProperty('height', `${height}px`)
  const head = doc.createElementNS(XHTML, 'head')
  const style = doc.createElementNS(XHTML, 'style')
  // The sheet of paper is drawn underneath; the body alone covers only one column. And
  // epub.js's limit on a book's own SVGs mustn't catch the SVG this picture is.
  // A picture is laid out as if on a plain 1x screen; a sharp screen measures letters
  // more finely, and lines would break differently without this.
  const fine = window.devicePixelRatio >= 1.5 ? '\n* { text-rendering: geometricPrecision !important; }' : ''
  style.textContent = `${css}\nhtml, body { background: transparent !important; }
svg:root { max-width: none !important; max-height: none !important; }${fine}`
  head.appendChild(style)
  root.appendChild(head)
  root.appendChild(body)
  const text = new XMLSerializer().serializeToString(root)

  // A picture's own size stays in CSS pixels: an SVG is redrawn sharp at any scale, and
  // Safari miscounts a source rectangle on one whose size differs from its viewBox.
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="${height}" viewBox="0 0 ${width} ${height}"><foreignObject x="0" y="0" width="${width * zoom}" height="${height * zoom}" transform="scale(${1 / zoom})">${text}</foreignObject>${overlayOf(frame)}</svg>`
  const src = `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`
  let img = await loadImage(src)
  // Safari leaves out the pictures inside a picture until it has loaded them once.
  if (!isBlink()) {
    const warm = document.createElement('canvas').getContext('2d')
    for (let i = 0; i < 3; i++) {
      warm?.drawImage(img, 0, 0, 300, 150)
      await new Promise((resolve) => window.setTimeout(resolve, 100))
      img = await loadImage(src)
    }
  }
  return { img, width, height, scale }
}

// drawPage paints one page of the picture onto a canvas of the page's own size.
export function drawPage(pic: ChapterPicture, cropX: number, pageW: number, pageH: number, paper: string, into: HTMLCanvasElement) {
  const w = Math.round(pageW * pic.scale)
  const h = Math.round(pageH * pic.scale)
  if (into.width !== w || into.height !== h) {
    into.width = w
    into.height = h
  }
  const ctx = into.getContext('2d', { alpha: false })
  if (!ctx) throw new Error('no canvas')
  ctx.fillStyle = paper
  ctx.fillRect(0, 0, w, h)
  ctx.drawImage(pic.img, cropX, 0, pageW, pageH, 0, 0, w, h)
}
