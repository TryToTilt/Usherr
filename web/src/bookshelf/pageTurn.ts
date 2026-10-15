// Home fork: a page-turn effect for the ebook reader. EPUB pages are live,
// reflowing text, so this can't be a flip book like the comic reader.
// Instead, at each turn it lays a copy of the page you are on over the
// reader, moves to the next page underneath, then curls the copy away: your
// real page turns over, text and all. Installed by wrapping the rendition's
// next() and prev(), so the reader itself barely changes. Switched off with
// prefers-reduced-motion, or in the reader's settings (localStorage).
import type { Rendition } from 'epubjs'
import { t } from '../i18n'
import './pageTurn.css'

const keyOff = 'mediarium.reader.pageturn.off'
const keySpeed = 'mediarium.reader.pageturn.speed'

export type TurnSpeed = 'slow' | 'normal' | 'fast'
export const turnSpeeds: { id: TurnSpeed; label: string; ms: number }[] = [
  { id: 'slow', label: t('Slow'), ms: 950 },
  { id: 'normal', label: t('Normal'), ms: 560 },
  { id: 'fast', label: t('Fast'), ms: 300 },
]

export function turnSpeed(): TurnSpeed {
  try {
    const v = localStorage.getItem(keySpeed)
    if (v === 'slow' || v === 'fast') return v
  } catch {
    // no storage
  }
  return 'normal'
}

export function setTurnSpeed(v: TurnSpeed) {
  try {
    localStorage.setItem(keySpeed, v)
  } catch {
    // no storage
  }
}

function duration(): number {
  return turnSpeeds.find((s) => s.id === turnSpeed())?.ms ?? 560
}

export function pageTurnOn(): boolean {
  try {
    if (localStorage.getItem(keyOff) === '1') return false
  } catch {
    // no storage: on
  }
  return !(typeof window.matchMedia === 'function' && window.matchMedia('(prefers-reduced-motion: reduce)').matches)
}

export function setPageTurn(on: boolean) {
  try {
    if (on) localStorage.removeItem(keyOff)
    else localStorage.setItem(keyOff, '1')
  } catch {
    // no storage
  }
}

// snapshot copies what the reader shows right now into a layer over it.
// epub.js draws each chapter in an iframe inside .epub-container, which is
// scrolled sideways to the page shown; the copy rebuilds those iframes from
// their documents and scrolls the same way.
function snapshot(host: HTMLElement): HTMLElement | null {
  const container = host.querySelector<HTMLElement>('.epub-container')
  if (!container) return null
  const layer = document.createElement('div')
  layer.className = 'pt-layer'
  const hostBox = host.getBoundingClientRect()
  const box = container.getBoundingClientRect()
  Object.assign(layer.style, {
    left: `${box.left - hostBox.left}px`,
    top: `${box.top - hostBox.top}px`,
    width: `${box.width}px`,
    height: `${box.height}px`,
  })
  const copy = container.cloneNode(false) as HTMLElement
  copy.removeAttribute('id')
  copy.style.position = 'absolute'
  copy.style.inset = '0'
  copy.style.overflow = 'hidden'
  for (const view of Array.from(container.children) as HTMLElement[]) {
    const v = view.cloneNode(false) as HTMLElement
    const frame = view.querySelector('iframe')
    const doc = frame?.contentDocument
    if (frame && doc) {
      const f = document.createElement('iframe')
      f.setAttribute('style', frame.getAttribute('style') ?? '')
      f.width = frame.width
      f.height = frame.height
      f.style.width = `${frame.offsetWidth}px`
      f.style.height = `${frame.offsetHeight}px`
      f.style.border = '0'
      f.setAttribute('scrolling', 'no')
      f.setAttribute('aria-hidden', 'true')
      f.tabIndex = -1
      v.appendChild(f)
      copy.appendChild(v)
      // Written synchronously, so the copy is ready before the page moves.
      const html = '<!doctype html>' + doc.documentElement.outerHTML
      queueMicrotask(() => {
        const d = f.contentDocument
        if (!d) return
        d.open()
        d.write(html)
        d.close()
        d.documentElement.style.overflow = 'hidden'
      })
    } else {
      v.appendChild(view.cloneNode(true))
      copy.appendChild(v)
    }
  }
  layer.appendChild(copy)
  // The shading that sweeps across as the page lifts.
  const shade = document.createElement('div')
  shade.className = 'pt-shade'
  layer.appendChild(shade)
  host.appendChild(layer)
  copy.scrollLeft = container.scrollLeft
  copy.scrollTop = container.scrollTop
  // The iframes fill in a moment later; keep the scroll in step.
  requestAnimationFrame(() => {
    copy.scrollLeft = container.scrollLeft
    copy.scrollTop = container.scrollTop
  })
  return layer
}

// installPageTurn wraps rendition.next/prev with the page-turn effect.
export function installPageTurn(r: Rendition, host: HTMLElement) {
  const next = r.next.bind(r)
  const prev = r.prev.bind(r)
  let busy = false
  const turn = async (forward: boolean): Promise<void> => {
    const go = forward ? next : prev
    if (busy || !pageTurnOn() || !host.isConnected) return go()
    busy = true
    let layer: HTMLElement | null = null
    try {
      layer = snapshot(host)
      // Wait for the copy to be drawn (two frames) before moving underneath it.
      await new Promise((ok) => requestAnimationFrame(() => requestAnimationFrame(ok)))
      if (layer) {
        const scroll = layer.querySelector<HTMLElement>('.epub-container')
        const container = host.querySelector<HTMLElement>('.epub-container')
        if (scroll && container) scroll.scrollLeft = container.scrollLeft
      }
      await go()
      if (layer) {
        const ms = duration()
        layer.style.animationDuration = `${ms}ms`
        layer.style.setProperty('--pt-ms', `${ms}ms`)
        layer.classList.add(forward ? 'pt-forward' : 'pt-back')
        await new Promise((ok) => window.setTimeout(ok, ms + 40))
      }
    } catch {
      // the turn itself still happened (or failed on its own); just tidy up
    } finally {
      layer?.remove()
      busy = false
    }
  }
  r.next = () => turn(true)
  r.prev = () => turn(false)
}
