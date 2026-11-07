// The page curl itself, knowing nothing of what is on the pages: give it two
// page pictures (any canvas or image) and it turns one into the other over the
// reader, by finger or on its own, in either reading direction. What the reader
// shows moves only once a turn is decided (commit), under the canvas, so a turn
// given up changes nothing.
//
//   const curl = PageCurl.create(stageElement)       // null: no WebGL, turn another way
//   const current = curl.texture(canvasOrImage)      // sized like the page, opaque
//   await curl.play({ page, dir: 'next', rtl: true, pages: { current, target }, commit })
//   const drag = curl.drag(turn, x, y); drag.move(x, y); await drag.end()
//
// page is the page's box in stage pixels. rtl mirrors everything, so "next" lifts
// from the left edge. spread: page holds two facing pages; the leaf is hinged at the
// middle and its back is the target's facing page.
import { CurlGl, type PageTexture } from './curlGl'
import { finishes, foldAt, shapeFor, type Rect, type TurnShape } from './curlMath'

export type { PageTexture, Rect }
export type TurnDir = 'next' | 'prev'
export type TurnSpeed = 'fast' | 'normal' | 'slow'

export interface CurlPages {
  current: PageTexture // the page on screen
  target: PageTexture // the page being turned to
}

export interface CurlTurn {
  page: Rect
  dir: TurnDir
  rtl?: boolean
  spread?: boolean
  // The pictures, or a way to get them. null: no page that way; rejected: can't be had.
  pages: CurlPages | (() => Promise<CurlPages | null>)
  paper?: string // the paper's colour, "#rrggbb", for the sheet's plain back
  dark?: boolean
  speed?: TurnSpeed
  commit: () => void | Promise<void> // show the target for real; the canvas is still on top
}

export interface DragEnd {
  // 'done' turned, 'back' fell back or nothing to turn to, 'failed' the curl couldn't run.
  result: 'done' | 'back' | 'failed'
  wanted: boolean // the gesture did ask for a turn
}

export interface CurlDrag {
  move: (x: number, y: number) => void
  end: () => Promise<DragEnd>
}

const CURL_MS: Record<TurnSpeed, number> = { fast: 460, normal: 720, slow: 1100 }
const SPRING: Record<TurnSpeed, number> = { fast: 0.016, normal: 0.012, slow: 0.008 } // per millisecond

const raf = () => new Promise<number>((resolve) => requestAnimationFrame(resolve))
const easeTurn = (t: number) => (t < 0.5 ? 2 * t * t : 1 - (-2 * t + 2) ** 2 / 2)

function parseColor(hex: string): [number, number, number] {
  const m = /^#?([\da-f]{2})([\da-f]{2})([\da-f]{2})/i.exec(hex)
  return m ? [parseInt(m[1], 16) / 255, parseInt(m[2], 16) / 255, parseInt(m[3], 16) / 255] : [1, 1, 1]
}

// The sides of the sheet: "next" starts from the current page, "prev" is the same
// motion played backwards from the target.
const sides = (dir: TurnDir, p: CurlPages) => (dir === 'next' ? { front: p.current, other: p.target } : { front: p.target, other: p.current })

export class PageCurl {
  private canvas: HTMLCanvasElement
  private gl: CurlGl
  private stage: HTMLElement
  private hurrying = false
  private detail = 1 // how much of the screen's resolution the moving page gets
  private slow = 0
  private frames = 0
  busy = false
  dead = false

  static create(stage: HTMLElement): PageCurl | null {
    const canvas = document.createElement('canvas')
    canvas.className = 'rdr-curl'
    canvas.setAttribute('aria-hidden', 'true')
    Object.assign(canvas.style, { position: 'absolute', inset: '0', width: '100%', height: '100%', zIndex: '5', pointerEvents: 'none', display: 'none' })
    const gl = CurlGl.create(canvas)
    return gl ? new PageCurl(stage, canvas, gl) : null
  }

  private constructor(stage: HTMLElement, canvas: HTMLCanvasElement, gl: CurlGl) {
    this.stage = stage
    this.canvas = canvas
    this.gl = gl
    stage.appendChild(canvas)
    canvas.addEventListener('webglcontextlost', () => {
      this.dead = true
      canvas.style.display = 'none'
    })
  }

  // The screen's sharpness pictures should be made at.
  static get scale() {
    return Math.min(window.devicePixelRatio || 1, 2)
  }

  // texture makes a page picture usable. It throws on a canvas the browser
  // considers tainted.
  texture(source: TexImageSource): PageTexture {
    return this.gl.upload(source)
  }

  free(tex: PageTexture) {
    this.gl.free(tex)
  }

  // hurry: another turn is waiting, finish this one sooner.
  hurry() {
    this.hurrying = true
  }

  destroy() {
    this.dead = true
    this.canvas.remove()
    this.gl.gl.getExtension('WEBGL_lose_context')?.loseContext()
  }

  private sizeCanvas() {
    const k = PageCurl.scale * this.detail
    const w = Math.round(this.stage.clientWidth * k)
    const h = Math.round(this.stage.clientHeight * k)
    if (this.canvas.width !== w || this.canvas.height !== h) {
      this.canvas.width = w
      this.canvas.height = h
    }
    return k
  }

  private drawFrame(turn: CurlTurn, shape: TurnShape, p: CurlPages, d: number, dy: number) {
    const k = this.sizeCanvas()
    const f = foldAt(shape, d, dy)
    const sc = (x: Rect): Rect => ({ x0: x.x0 * k, y0: x.y0 * k, x1: x.x1 * k, y1: x.y1 * k })
    const paper = parseColor(turn.paper ?? '#ffffff')
    const lighten = turn.dark ? 0.075 : 0.3
    const lift = Math.min(1, (d / shape.dMax) * 5) * Math.min(1, ((shape.dMax - d) / shape.dMax) * 5)
    this.gl.draw({
      page: sc(turn.page),
      leaf: sc(shape.leaf),
      fold: { ax: f.ax * k, ay: f.ay * k, nx: f.nx, ny: f.ny, r: f.r * k },
      spread: !!turn.spread,
      mirror: !!turn.rtl,
      ...sides(turn.dir, p),
      paper,
      back: [paper[0] + (1 - paper[0]) * lighten, paper[1] + (1 - paper[1]) * lighten, paper[2] + (1 - paper[2]) * lighten],
      dark: !!turn.dark,
      lift,
      scale: k,
    })
    if (this.canvas.style.display === 'none') {
      this.canvas.style.transition = 'none'
      this.canvas.style.opacity = '1'
      this.canvas.style.display = 'block'
    }
  }

  // watch lowers the detail of the moving page rather than letting it stutter.
  private watch(dt: number) {
    this.frames++
    if (this.frames < 6) return // the first frames of a turn say little
    this.slow = dt > 26 && dt < 120 ? this.slow + 1 : Math.max(0, this.slow - 1)
    if (this.slow >= 10 && this.detail > 0.7) {
      this.detail = Math.max(0.7, this.detail - 0.15)
      this.slow = 0
    }
  }

  // A new turn: it gets a chance at more detail again.
  private startTurn() {
    this.busy = true
    this.hurrying = false
    this.frames = 0
    this.detail = Math.min(1, this.detail + 0.15)
  }

  private async hide() {
    const c = this.canvas
    // Whatever slight difference there is between picture and live page goes unseen.
    c.style.transition = 'opacity 90ms linear'
    c.style.opacity = '0'
    await new Promise((resolve) => window.setTimeout(resolve, 100))
    c.style.display = 'none'
    c.style.transition = 'none'
    c.style.opacity = '1'
  }

  private async commit(turn: CurlTurn) {
    await turn.commit()
    await raf()
    await raf()
  }

  private static async pagesOf(turn: CurlTurn): Promise<CurlPages | null> {
    return typeof turn.pages === 'function' ? turn.pages() : turn.pages
  }

  // play runs a whole turn on its own: 'done', 'none' (no page that way) or
  // 'failed' (turn another way; nothing has moved).
  async play(turn: CurlTurn): Promise<'done' | 'none' | 'failed'> {
    if (this.dead || this.busy) return 'failed'
    this.startTurn()
    let committed = false
    try {
      const p = await PageCurl.pagesOf(turn)
      if (!p) return 'none'
      const shape = shapeFor(turn.page, !!turn.spread, turn.page.y0 + (turn.page.y1 - turn.page.y0) * 0.9)
      const ms = CURL_MS[turn.speed ?? 'normal']
      let t = 0
      let last = performance.now()
      for (;;) {
        const now = await raf()
        const dt = Math.min(60, now - last)
        last = now
        this.watch(dt)
        if (this.dead) return 'failed'
        t = Math.min(1, t + (dt * (this.hurrying ? 2 : 1)) / ms)
        const e = easeTurn(t)
        this.drawFrame(turn, shape, p, (turn.dir === 'next' ? e : 1 - e) * shape.dMax, 0)
        if (t >= 1) break
      }
      committed = true
      await this.commit(turn)
      await this.hide()
      return 'done'
    } catch {
      this.canvas.style.display = 'none'
      return committed ? 'done' : 'failed'
    } finally {
      this.busy = false
    }
  }

  // drag follows a finger pressed at (x, y), viewport coordinates, or returns null
  // when it's busy with another turn.
  drag(turn: CurlTurn, x: number, y: number, pressedAt = performance.now()): CurlDrag | null {
    if (this.dead || this.busy) return null
    const { page } = turn
    const stage = this.stage.getBoundingClientRect()
    const toX = (v: number) => {
      const c = v - stage.left
      return turn.rtl ? page.x0 + page.x1 - c : c
    }
    const gx = Math.max(page.x0 + 1, Math.min(page.x1 - 1, toX(x)))
    const gy = y - stage.top
    const shape = shapeFor(page, !!turn.spread, gy)
    const reach = turn.spread ? 2 * (shape.leaf.x1 - shape.leaf.x0) : page.x1 - page.x0
    const forward = turn.dir === 'next'
    const span = Math.max(40, forward ? gx - page.x0 : page.x1 - gx)

    this.startTurn()
    let target = forward ? 0 : shape.dMax // where the finger wants the sheet
    let d = target
    let dyTarget = 0
    let dy = 0
    let vel = 0 // along the turn, px/ms
    let frac = 0 // how far the finger has come
    let released = false
    let turnOn = false // the release's verdict
    const moves: { t: number; x: number }[] = [{ t: pressedAt, x: gx }]
    let settle: (v: DragEnd) => void = () => undefined
    const finished = new Promise<DragEnd>((resolve) => (settle = resolve))
    let onRelease: () => void = () => undefined
    const letGo = new Promise<void>((resolve) => (onRelease = resolve))

    // How fast the finger was going as it let go; held still first, not at all.
    const fingerSpeed = () => {
      const lastMove = moves[moves.length - 1]
      if (!lastMove || performance.now() - lastMove.t > 200) return 0
      // Quick moves may reach us as one, in a slow frame: then against the one before.
      const near = moves.slice(-4).find((m) => lastMove.t - m.t < 200)
      const a = !near || near === lastMove ? (moves[moves.length - 2] ?? lastMove) : near
      const dx = lastMove.t > a.t ? (lastMove.x - a.x) / (lastMove.t - a.t) : 0
      return forward ? -dx : dx // positive: going on with the turn
    }

    const run = async () => {
      let p: CurlPages | null
      try {
        p = await PageCurl.pagesOf(turn)
      } catch {
        this.busy = false
        await letGo // the finger says whether a turn was asked for
        settle({ result: 'failed', wanted: turnOn })
        return
      }
      if (!p) {
        this.busy = false
        await letGo
        settle({ result: 'back', wanted: false })
        return
      }
      let last = performance.now()
      try {
        for (;;) {
          const now = await raf()
          const dt = Math.min(48, now - last)
          last = now
          this.watch(dt)
          if (this.dead) {
            // The screen took the drawing away, mid-turn: nothing has moved, and the
            // finger says whether the page should still turn, the plain way.
            this.canvas.style.display = 'none'
            await letGo
            settle({ result: 'failed', wanted: turnOn })
            return
          }
          if (!released) {
            // A touch of lag gives the sheet some weight.
            const before = d
            const k = 1 - Math.exp(-dt / 30)
            d += (target - d) * k
            dy += (dyTarget - dy) * k
            vel = dt > 0 ? (d - before) / dt : 0
          } else {
            // dMax is where "next" has turned and "prev" has fallen back.
            const end = turnOn === forward ? shape.dMax : 0
            const aim = end === 0 ? -shape.dMax * 0.14 : shape.dMax * 1.14
            const w = SPRING[turn.speed ?? 'normal'] * (this.hurrying ? 1.6 : 1)
            for (let left = dt; left > 0; left -= 4) {
              const h = Math.min(4, left)
              vel += (w * w * (aim - d) - 2 * w * vel) * h
              d += vel * h
            }
            dy *= Math.exp(-dt / 180)
            if (end === 0 ? d <= 0 : d >= shape.dMax) {
              this.drawFrame(turn, shape, p, end, dy)
              break
            }
          }
          this.drawFrame(turn, shape, p, Math.max(0, Math.min(shape.dMax, d)), dy)
        }
        if (turnOn) await this.commit(turn)
        await this.hide()
        settle({ result: turnOn ? 'done' : 'back', wanted: turnOn })
      } catch {
        this.canvas.style.display = 'none'
        settle({ result: 'failed', wanted: turnOn })
      } finally {
        this.busy = false
      }
    }
    void run()

    return {
      move: (mx, my) => {
        const fx = toX(mx)
        moves.push({ t: performance.now(), x: fx })
        if (moves.length > 12) moves.shift()
        frac = Math.max(0, Math.min(1, (forward ? gx - fx : fx - gx) / span))
        target = Math.max(0, Math.min(shape.dMax, forward ? frac * reach : shape.dMax * (1 - frac)))
        dyTarget = my - stage.top - gy
      },
      end: () => {
        turnOn = finishes(frac, fingerSpeed())
        released = true
        onRelease()
        return finished
      },
    }
  }
}
