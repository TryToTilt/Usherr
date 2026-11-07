// What a finger or a mouse does to a page of the EPUB reader. Dragging on text
// has to keep selecting it, so a page is pulled by its edges: a strip along each
// outer edge (.rdr-grip, over the iframe) and the arrows beside the page. Anywhere
// else belongs to the text, except that a sideways finger drag in the middle turns
// too, because a touch screen picks words by holding still (350 ms) and not by
// dragging. That part only works where the browser passes on events from a book's
// sandboxed iframe (Chrome, Firefox); Safari doesn't, which is why the strips are
// outside the iframe. A tap turns: on a strip, or on the page's outer third by
// finger. While words are selected nothing turns, so their handles can be dragged.
import type { Rendition } from 'epubjs'
import type { CurlDrag, TurnDir } from './curl/pageCurl'

export interface GestureOptions {
  rendition: Rendition
  stage: HTMLElement
  rtl: () => boolean
  onTurn: (dir: TurnDir) => void
  // A drag that follows the finger, if the turn can do that right now.
  beginDrag: (dir: TurnDir, x: number, y: number, pressedAt: number) => CurlDrag | null
}

interface Press {
  id: number
  x: number
  y: number
  at: number
  touch: boolean
  side: '' | 'left' | 'right'
  capture: (id: number) => void
  drag: CurlDrag | null
  swipe: boolean
  noTap: boolean // a link, or an arrow that does its own tap
  timer: number
}

const TOUCH_TAP = 0.3 // of a page's width

interface Internals {
  manager?: { container: HTMLElement }
}

export function attachTurnGestures(o: GestureOptions): () => void {
  let press: Press | null = null
  let swallowClick = false

  const pageBox = () => (o.rendition as unknown as Internals).manager?.container.getBoundingClientRect()
  const dirOf = (side: 'left' | 'right'): TurnDir => ((side === 'right') !== o.rtl() ? 'next' : 'prev')
  const swallow = () => {
    swallowClick = true
    window.setTimeout(() => (swallowClick = false), 400)
  }

  const selectedText = () => {
    const frames = (o.rendition as unknown as Internals).manager?.container.querySelectorAll('iframe')
    return Array.from(frames ?? []).some((f) => {
      const sel = f.contentWindow?.getSelection()
      return !!sel && !sel.isCollapsed
    })
  }

  const down = (e: PointerEvent, x: number, y: number, side: '' | 'left' | 'right', noTap: boolean, capture: (id: number) => void) => {
    if (e.button !== 0 || !e.isPrimary) return
    press = null
    if (selectedText()) return
    const p: Press = { id: e.pointerId, x, y, at: performance.now(), touch: e.pointerType !== 'mouse', side, capture, drag: null, swipe: false, noTap, timer: 0 }
    press = p
    if (p.touch && !side) {
      p.timer = window.setTimeout(() => {
        if (press === p && !p.drag && !p.swipe) press = null
      }, 350)
    }
  }

  const move = (e: PointerEvent, x: number, y: number) => {
    const p = press
    if (!p || e.pointerId !== p.id) return
    if (p.drag) {
      p.drag.move(x, y)
      return
    }
    if (p.swipe) return
    const dx = x - p.x
    const dy = y - p.y
    if (Math.hypot(dx, dy) < 7) return
    window.clearTimeout(p.timer)
    // A mouse pressed on the text is selecting. A finger going up or down isn't turning.
    if ((!p.touch && !p.side) || Math.abs(dx) < Math.abs(dy) * (p.side ? 0.5 : 1.2)) {
      press = null
      return
    }
    // From an edge, only inwards.
    if ((p.side === 'right' && dx > 0) || (p.side === 'left' && dx < 0)) {
      press = null
      return
    }
    p.drag = o.beginDrag(dirOf(dx < 0 ? 'right' : 'left'), p.x, p.y, p.at)
    if (!p.drag) p.swipe = true
    p.capture(p.id)
    p.drag?.move(x, y)
  }

  const up = (e: PointerEvent, x: number, y: number, cancelled: boolean) => {
    // The last moves may arrive only with the release.
    if (!cancelled) move(e, x, y)
    const p = press
    if (!p || e.pointerId !== p.id) return
    press = null
    window.clearTimeout(p.timer)
    if (p.drag) {
      swallow()
      void p.drag.end().then(({ result, wanted }) => {
        if (result === 'failed' && wanted) o.onTurn(dirOf(x < p.x ? 'right' : 'left'))
      })
      return
    }
    if (cancelled) return
    if (p.swipe) {
      swallow()
      if (Math.abs(x - p.x) > 50) o.onTurn(dirOf(x < p.x ? 'right' : 'left'))
      return
    }
    if (p.noTap || Math.hypot(x - p.x, y - p.y) >= 7 || performance.now() - p.at > 350) return
    const box = pageBox()
    if (!box) return
    const zone = p.touch ? box.width * TOUCH_TAP : 0
    const side = p.side || (x > box.right - zone ? 'right' : x < box.left + zone ? 'left' : '')
    if (!side) return
    // A strip lies over the text, and a footnote mark is often at the end of a line. A
    // click from here works where the book's frame hears nothing too: the link still goes.
    const link = linkAt(x, y)
    if (link) link.click()
    else if (!overHighlight(x, y)) o.onTurn(dirOf(side))
  }

  const linkAt = (x: number, y: number) => {
    for (const f of Array.from((o.rendition as unknown as Internals).manager?.container.querySelectorAll('iframe') ?? [])) {
      const b = f.getBoundingClientRect()
      const a = f.contentDocument?.elementFromPoint(x - b.left, y - b.top)?.closest<HTMLElement>('a[href]')
      if (a) return a
    }
    return null
  }

  // A tap on a highlight opens it, it doesn't turn the page.
  const overHighlight = (x: number, y: number) => {
    const marks = (o.rendition as unknown as Internals).manager?.container.querySelectorAll('.rdr-hl rect')
    for (const r of Array.from(marks ?? [])) {
      const b = r.getBoundingClientRect()
      if (x >= b.left && x <= b.right && y >= b.top && y <= b.bottom) return true
    }
    return false
  }

  // Each chapter's iframe, as epub.js opens it.
  o.rendition.hooks.content.register((contents: { document: Document; window: Window }) => {
    const doc = contents.document
    const at = (e: PointerEvent) => {
      const f = contents.window.frameElement?.getBoundingClientRect()
      return { x: e.clientX + (f?.left ?? 0), y: e.clientY + (f?.top ?? 0) }
    }
    doc.documentElement.style.touchAction = 'pan-y pinch-zoom'
    const capture = (id: number) => {
      try {
        doc.documentElement.setPointerCapture(id)
      } catch {
        // the finger has already gone
      }
    }
    doc.addEventListener('pointerdown', (e) => {
      if (e.pointerType === 'mouse') return
      const p = at(e)
      down(e, p.x, p.y, '', !!(e.target as Element | null)?.closest?.('a[href]'), capture)
    })
    doc.addEventListener('pointermove', (e) => {
      const p = at(e)
      move(e, p.x, p.y)
    })
    doc.addEventListener('pointerup', (e) => {
      const p = at(e)
      up(e, p.x, p.y, false)
    })
    doc.addEventListener('pointercancel', (e) => {
      const p = at(e)
      up(e, p.x, p.y, true)
    })
    // No selecting while the page is held.
    doc.addEventListener('selectstart', (e) => {
      if (press?.drag) e.preventDefault()
    })
  })

  // The strips and the arrows beside the page. An arrow's own click does its tap.
  const onDown = (e: PointerEvent) => {
    const grip = (e.target as Element | null)?.closest?.('.rdr-grip, .rdr-turn')
    if (!grip) return
    const side = grip.classList.contains('left') || grip.classList.contains('prev') ? 'left' : 'right'
    down(e, e.clientX, e.clientY, side, grip.classList.contains('rdr-turn'), (id) => {
      try {
        grip.setPointerCapture(id)
      } catch {
        // the finger has already gone
      }
    })
  }
  const onMove = (e: PointerEvent) => move(e, e.clientX, e.clientY)
  const onUp = (e: PointerEvent) => up(e, e.clientX, e.clientY, false)
  const onCancel = (e: PointerEvent) => up(e, e.clientX, e.clientY, true)
  const onClick = (e: MouseEvent) => {
    if (swallowClick) {
      swallowClick = false
      e.stopPropagation()
      e.preventDefault()
    }
  }
  o.stage.addEventListener('pointerdown', onDown)
  o.stage.addEventListener('pointermove', onMove)
  o.stage.addEventListener('pointerup', onUp)
  o.stage.addEventListener('pointercancel', onCancel)
  o.stage.addEventListener('click', onClick, true)
  return () => {
    o.stage.removeEventListener('pointerdown', onDown)
    o.stage.removeEventListener('pointermove', onMove)
    o.stage.removeEventListener('pointerup', onUp)
    o.stage.removeEventListener('pointercancel', onCancel)
    o.stage.removeEventListener('click', onClick, true)
  }
}
