// The page curl for pages that are pictures (manga, comics). It sits on PageCurl,
// which knows nothing of what is on a page: this makes the page pictures. A
// picture is drawn into a canvas the size of the page's box, the way the live page
// shows it, so nothing shifts when the curl hands back to the page underneath.
//
// Three things matter on a phone, where a scan is big and memory is small:
//  - pictures are made at the screen's sharpness, however big the scan is;
//  - only the few textures around the current page are kept (keepOnly);
//  - neighbours are made ahead, between turns (prepare), so a turn starts at once.
import { PageCurl, type CurlDrag, type PageTexture, type Rect, type TurnDir, type TurnSpeed } from './pageCurl'

const MAX_TEXTURE = 4096 // every WebGL device of the last decade has at least this
const KEEP_IMAGES = 10

export interface CurlPicture {
  key: string // the same key means the same picture
  width: number // the box, in CSS pixels
  height: number
  background: string // what shows round the pages; opaque
  items: { url: string; x: number; y: number; w: number; h: number }[] // from the box's corner
}

export interface ImageTurn {
  page: Rect // the page's box, in stage pixels
  dir: TurnDir
  rtl: boolean
  spread: boolean
  current: CurlPicture
  target: CurlPicture
  speed?: TurnSpeed
  commit: () => void | Promise<void>
}

// ImageLoader keeps the last few pages decoded, so a turn needn't wait for them.
export class ImageLoader {
  private images = new Map<string, Promise<HTMLImageElement>>()

  load(url: string): Promise<HTMLImageElement> {
    const had = this.images.get(url)
    if (had) {
      // Most recently used goes last.
      this.images.delete(url)
      this.images.set(url, had)
      return had
    }
    const img = new Image()
    img.decoding = 'async'
    const loaded = new Promise<HTMLImageElement>((resolve, reject) => {
      img.onload = () => {
        // decode() is what stops the turn stuttering on a big page.
        void (img.decode ? img.decode().catch(() => undefined) : Promise.resolve()).then(() => resolve(img))
      }
      img.onerror = () => {
        this.images.delete(url)
        reject(new Error('page'))
      }
    })
    img.src = url
    loaded.catch(() => undefined)
    this.images.set(url, loaded)
    while (this.images.size > KEEP_IMAGES) {
      const oldest = this.images.keys().next().value
      if (oldest === undefined) break
      this.images.delete(oldest)
    }
    return loaded
  }
}

export class ImageCurl {
  readonly loader: ImageLoader
  private curl: PageCurl
  private textures = new Map<string, Promise<PageTexture>>()
  private scratch = document.createElement('canvas')

  static create(stage: HTMLElement, loader = new ImageLoader()): ImageCurl | null {
    const curl = PageCurl.create(stage)
    return curl ? new ImageCurl(curl, loader) : null
  }

  private constructor(curl: PageCurl, loader: ImageLoader) {
    this.curl = curl
    this.loader = loader
  }

  get busy() {
    return this.curl.busy
  }

  get dead() {
    return this.curl.dead
  }

  hurry() {
    this.curl.hurry()
  }

  destroy() {
    this.textures.clear()
    this.scratch.width = 0
    this.scratch.height = 0
    this.curl.destroy()
  }

  // The sharpness pictures are made at: the screen's, within what a texture can be.
  static sharpness(width: number, height: number): number {
    return Math.min(PageCurl.scale, MAX_TEXTURE / Math.max(width, height, 1))
  }

  private async make(pic: CurlPicture): Promise<PageTexture> {
    const images = await Promise.all(pic.items.map((i) => this.loader.load(i.url)))
    const k = ImageCurl.sharpness(pic.width, pic.height)
    const c = this.scratch
    c.width = Math.max(1, Math.round(pic.width * k))
    c.height = Math.max(1, Math.round(pic.height * k))
    const ctx = c.getContext('2d', { alpha: false })
    if (!ctx) throw new Error('no canvas')
    ctx.fillStyle = pic.background
    ctx.fillRect(0, 0, c.width, c.height)
    ctx.imageSmoothingEnabled = true
    ctx.imageSmoothingQuality = 'high'
    pic.items.forEach((it, n) => ctx.drawImage(images[n], it.x * k, it.y * k, it.w * k, it.h * k))
    return this.curl.texture(c)
  }

  private textureOf(pic: CurlPicture): Promise<PageTexture> {
    let t = this.textures.get(pic.key)
    if (!t) {
      t = this.make(pic)
      this.textures.set(pic.key, t)
      t.catch(() => this.textures.delete(pic.key)) // another try next time
    }
    return t
  }

  // prepare makes pictures ahead of need, one at a time.
  async prepare(pics: readonly CurlPicture[]) {
    for (const pic of pics) {
      if (this.curl.dead) return
      await this.textureOf(pic).catch(() => undefined)
      await new Promise((resolve) => window.setTimeout(resolve, 0))
    }
  }

  // keepOnly frees every texture but these. Not while a turn is using them.
  keepOnly(keys: readonly string[]) {
    if (this.curl.busy) return
    for (const [key, t] of this.textures) {
      if (keys.includes(key)) continue
      this.textures.delete(key)
      void t.then((tex) => this.curl.free(tex)).catch(() => undefined)
    }
  }

  private turn(t: ImageTurn) {
    return {
      page: t.page,
      dir: t.dir,
      rtl: t.rtl,
      spread: t.spread,
      speed: t.speed,
      // White paper: the sheet's back is plain paper, whatever the pages are.
      paper: '#ffffff',
      dark: false,
      pages: async () => ({ current: await this.textureOf(t.current), target: await this.textureOf(t.target) }),
      commit: t.commit,
    }
  }

  play(t: ImageTurn) {
    return this.curl.play(this.turn(t))
  }

  drag(t: ImageTurn, x: number, y: number, pressedAt?: number): CurlDrag | null {
    return this.curl.drag(this.turn(t), x, y, pressedAt)
  }
}
