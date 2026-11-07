// The page curl (pageCurl.ts) for the EPUB reader: keeps pictures of the pages
// either side of the one being read ready as textures (see snapshot.ts), so a
// turn starts at once, and moves the book with epub.js when a turn is decided, so
// reading place, bookmarks and selection stay epub.js's own business. Loaded only
// when the curl is switched on.
import type { Book as EpubBook, Rendition } from 'epubjs'
import type { CurlDrag, CurlPages, CurlTurn, PageTexture, Rect, TurnDir, TurnSpeed } from './pageCurl'
import { PageCurl } from './pageCurl'
import { drawPage, pictureOfChapter, type ChapterPicture, type FileReaderFn } from './snapshot'

export type { CurlDrag, TurnDir }

export interface CurlOptions {
  book: EpubBook
  rendition: Rendition
  stage: HTMLElement // what the canvas covers
  viewer: HTMLElement // what the book is drawn in
  theme: () => { bg: string; dark: boolean }
  speed: () => TurnSpeed
  // A second copy of the book, for a look at the next chapter without moving this one.
  openCopy: (host: HTMLElement) => { book: EpubBook; rendition: Rendition }
  setUpCopy: (r: Rendition) => void
  // Resolves when a rendition's chapter has stopped changing, highlights in their place.
  settled: (r: Rendition) => Promise<void>
}

interface Section {
  index: number
  href: string
  next: () => Section | null | undefined
  prev: () => Section | null | undefined
}

interface Internals {
  manager?: {
    container: HTMLElement
    layout: { name: string; delta: number; divisor: number; gap: number }
    settings: { axis?: string; direction?: string }
    views: { first: () => { iframe?: HTMLIFrameElement; section: Section } | undefined }
  }
}

interface Geometry {
  key: string
  page: Rect // CSS pixels, in the stage
  w: number
  h: number
  crop: number
  delta: number
  total: number
  spread: boolean
  rtl: boolean
  section: Section
  frame: HTMLIFrameElement
}

// A book's files by the blob: addresses epub.js gave them, out of the book itself.
const filesOf =
  (book: EpubBook): FileReaderFn =>
  async (url) => {
    const archive = book.archive as unknown as { urlCache: Record<string, string>; getBlob: (path: string) => Promise<Blob> }
    const path = Object.keys(archive.urlCache).find((k) => archive.urlCache[k] === url)
    if (!path) throw new Error('not in the book')
    return archive.getBlob(path)
  }

const raf = () => new Promise<number>((resolve) => requestAnimationFrame(resolve))

export class EpubCurl {
  private curl: PageCurl
  private scratch = document.createElement('canvas')
  private opts: CurlOptions
  private gen = 0
  private geoKey = ''
  private pictures = new Map<number, Promise<ChapterPicture>>()
  private textures = new Map<string, PageTexture>()
  private prepared: Promise<void> | null = null
  private timer = 0
  private resizes: ResizeObserver
  private broken = false // this chapter couldn't be pictured: plain turns until it changes
  private copy: { book: EpubBook; rendition: Rendition; host: HTMLElement } | null = null
  private copyQueue: Promise<unknown> = Promise.resolve()

  // create returns null where the curl can't run; the reader then slides.
  static create(opts: CurlOptions): EpubCurl | null {
    const curl = PageCurl.create(opts.stage)
    return curl ? new EpubCurl(opts, curl) : null
  }

  private constructor(opts: CurlOptions, curl: PageCurl) {
    this.opts = opts
    this.curl = curl
    this.schedule()
    this.resizes = new ResizeObserver(() => this.schedule(250))
    this.resizes.observe(opts.viewer)
  }

  destroy() {
    window.clearTimeout(this.timer)
    this.resizes.disconnect()
    this.curl.destroy()
    this.dropCopy()
  }

  // invalidate: the look or the layout of the page changed (any setting, a highlight).
  invalidate() {
    this.gen++
    this.broken = false
    this.pictures.clear()
    this.freeTextures(new Set())
    this.prepared = null
    this.dropCopy() // its columns and its highlights are out of date; the next look opens it anew
    this.schedule(350) // the reader lays the text out again first
  }

  // schedule: the reader settled on a page; get the pages either side ready.
  schedule(ms = 140) {
    window.clearTimeout(this.timer)
    this.timer = window.setTimeout(() => {
      if (this.curl.busy) return this.schedule()
      this.prepared = null
      void this.prepare()
    }, ms)
  }

  hurry() {
    this.curl.hurry()
  }

  private geometry(): Geometry | null {
    const m = (this.opts.rendition as unknown as Internals).manager
    const view = m?.views.first()
    const frame = view?.iframe
    if (!m || !view || !frame?.contentDocument) return null
    // Fixed-layout books and vertical text keep the plain turn.
    if (m.layout.name === 'pre-paginated' || m.settings.axis === 'vertical') return null
    const box = m.container.getBoundingClientRect()
    const stage = this.opts.stage.getBoundingClientRect()
    const w = m.container.clientWidth
    const h = m.container.clientHeight
    if (w < 40 || h < 40) return null
    const x0 = Math.round(box.left - stage.left)
    const y0 = Math.round(box.top - stage.top)
    const spread = m.layout.divisor === 2
    return {
      // With the size the text is laid out at: the reader's bars settling can change it late.
      key: `${w}x${h}@${x0},${y0}|${spread}|${m.layout.delta}|${m.layout.gap}|${window.devicePixelRatio}|${frame.contentDocument.body?.style.width}x${frame.contentDocument.body?.style.height}`,
      page: { x0, y0, x1: x0 + w, y1: y0 + h },
      w,
      h,
      crop: Math.round(box.left - frame.getBoundingClientRect().left),
      delta: m.layout.delta,
      total: frame.offsetWidth,
      spread,
      rtl: m.settings.direction === 'rtl',
      section: view.section,
      frame,
    }
  }

  // The chapter's picture: from the live page, or from the copy for a chapter not open.
  private picture(g: Geometry, section: Section): Promise<ChapterPicture> {
    let p = this.pictures.get(section.index)
    if (!p) {
      p = section.index === g.section.index ? this.opts.settled(this.opts.rendition).then(() => pictureOfChapter(g.frame, PageCurl.scale, filesOf(this.opts.book))) : this.viaCopy(section)
      this.pictures.set(section.index, p)
      p.catch(() => this.pictures.delete(section.index))
    }
    return p
  }

  private viaCopy(section: Section): Promise<ChapterPicture> {
    const job = this.copyQueue.then(async () => {
      if (!this.copy) {
        const host = document.createElement('div')
        const stage = this.opts.stage.getBoundingClientRect()
        const v = this.opts.viewer.getBoundingClientRect()
        Object.assign(host.style, { position: 'absolute', left: `${v.left - stage.left}px`, top: `${v.top - stage.top}px`, width: `${v.width}px`, height: `${v.height}px`, visibility: 'hidden', pointerEvents: 'none', overflow: 'hidden', zIndex: '-1' })
        host.setAttribute('aria-hidden', 'true')
        this.opts.stage.appendChild(host)
        const { book, rendition } = this.opts.openCopy(host)
        this.opts.setUpCopy(rendition)
        this.copy = { book, rendition, host }
      }
      await this.copy.rendition.display(section.href)
      await this.opts.settled(this.copy.rendition)
      const view = (this.copy.rendition as unknown as Internals).manager?.views.first()
      if (!view?.iframe) throw new Error('chapter not there')
      return pictureOfChapter(view.iframe, PageCurl.scale, filesOf(this.copy.book))
    })
    this.copyQueue = job.catch(() => undefined)
    return job
  }

  private dropCopy() {
    if (!this.copy) return
    const { book, host } = this.copy
    this.copy = null
    try {
      book.destroy()
    } catch {
      // already gone
    }
    host.remove()
  }

  private freeTextures(keep: Set<PageTexture>) {
    for (const [id, t] of this.textures) {
      if (!keep.has(t)) {
        this.curl.free(t)
        this.textures.delete(id)
      }
    }
  }

  private async texture(g: Geometry, section: Section, where: number | 'first' | 'last'): Promise<PageTexture | null> {
    if (typeof where === 'number') {
      const known = this.textures.get(`${section.index}@${where}`)
      if (known) return known
    }
    const gen = this.gen
    const pic = await this.picture(g, section)
    if (gen !== this.gen || this.curl.dead) return null
    // The first page of a right-to-left chapter is its rightmost.
    const lastCrop = Math.max(0, Math.floor((pic.width - g.w) / g.delta + 0.01) * g.delta)
    const crop = where === 'first' ? (g.rtl ? lastCrop : 0) : where === 'last' ? (g.rtl ? 0 : lastCrop) : where
    const id = `${section.index}@${crop}`
    const known = this.textures.get(id)
    if (known) return known
    drawPage(pic, crop, g.w, g.h, this.opts.theme().bg, this.scratch)
    const tex = this.curl.texture(this.scratch)
    this.textures.set(id, tex)
    return tex
  }

  // What the page in a direction is: this chapter at another crop, or the next one along.
  private neighbour(g: Geometry, dir: TurnDir): { section: Section; where: number | 'first' | 'last' } | null {
    const onward = (dir === 'next') !== g.rtl ? g.crop + g.delta : g.crop - g.delta
    if (onward >= -1 && onward + g.w <= g.total + 1) return { section: g.section, where: Math.max(0, onward) }
    const section = dir === 'next' ? g.section.next() : g.section.prev()
    return section ? { section, where: dir === 'next' ? 'first' : 'last' } : null
  }

  private prepare(): Promise<void> {
    if (this.prepared) return this.prepared
    this.prepared = this.doPrepare().catch(() => {
      this.broken = true
    })
    return this.prepared
  }

  // The text was laid out again: old pictures no longer match it.
  private layoutIs(g: Geometry) {
    if (g.key === this.geoKey) return
    if (this.geoKey) {
      this.gen++
      this.pictures.clear()
      this.freeTextures(new Set())
      this.dropCopy()
    }
    this.geoKey = g.key
  }

  private async doPrepare() {
    if (this.curl.dead) return
    const seen = this.geometry()
    if (!seen) return
    this.layoutIs(seen)
    // The chapter's frame may still grow as it settles, so its picture comes first.
    await this.picture(seen, seen.section)
    const g = this.geometry()
    if (!g) return
    this.layoutIs(g)
    this.broken = false
    const keep = new Set<PageTexture>()
    const cur = await this.texture(g, g.section, g.crop)
    if (cur) keep.add(cur)
    for (const dir of ['next', 'prev'] as const) {
      const n = this.neighbour(g, dir)
      if (!n) continue
      await raf() // leave room for a frame between the pages
      const tex = await this.texture(g, n.section, n.where)
      if (tex) keep.add(tex)
    }
    if (!this.curl.busy) this.freeTextures(keep)
    // Only this chapter and its neighbours stay.
    const wanted = new Set([g.section.index, g.section.next()?.index, g.section.prev()?.index])
    for (const k of this.pictures.keys()) if (!wanted.has(k)) this.pictures.delete(k)
  }

  // The two pages of a turn, waited for if need be, up to a point.
  private async pagesFor(g: Geometry, dir: TurnDir, waitMs: number): Promise<CurlPages | null> {
    const n = this.neighbour(g, dir)
    if (!n) return null
    const get = async () => {
      const current = await this.texture(g, g.section, g.crop)
      const target = await this.texture(g, n.section, n.where)
      if (!current || !target) throw new Error('page not ready')
      return { current, target }
    }
    let timer = 0
    const late = new Promise<never>((_, reject) => {
      timer = window.setTimeout(() => reject(new Error('page not ready')), waitMs)
    })
    try {
      return await Promise.race([get(), late])
    } finally {
      window.clearTimeout(timer)
    }
  }

  private turnFor(g: Geometry, dir: TurnDir, waitMs: number): CurlTurn {
    const theme = this.opts.theme()
    return {
      page: g.page,
      dir,
      rtl: g.rtl,
      spread: g.spread,
      pages: () => this.pagesFor(g, dir, waitMs),
      paper: theme.bg,
      dark: theme.dark,
      speed: this.opts.speed(),
      commit: async () => {
        await (dir === 'next' ? this.opts.rendition.next() : this.opts.rendition.prev())
      },
    }
  }

  private current(): Geometry | null {
    if (this.curl.dead || this.curl.busy || this.broken) return null
    const g = this.geometry()
    if (g) this.layoutIs(g) // settled without a word from the reader: the pictures follow
    return g
  }

  // play runs a whole turn on its own. false: it couldn't, turn the plain way.
  async play(dir: TurnDir): Promise<boolean> {
    const g = this.current()
    if (!g) return false
    const result = await this.curl.play(this.turnFor(g, dir, 900))
    this.schedule(60)
    return result !== 'failed'
  }

  // drag follows a finger from (x, y), viewport coordinates, or returns null when a
  // drag can't be followed.
  drag(dir: TurnDir, x: number, y: number, pressedAt?: number): CurlDrag | null {
    const g = this.current()
    if (!g) return null
    const drag = this.curl.drag(this.turnFor(g, dir, 1500), x, y, pressedAt)
    if (!drag) return null
    return {
      move: drag.move,
      end: async () => {
        const end = await drag.end()
        this.schedule(60)
        return end
      },
    }
  }
}
