// What the ebook reader does inside a chapter's iframe, beyond epub.js: its own style
// sheet for the settings, waiting for the text to settle, and redrawing highlights
// when it has.
//
// Safari (all iPhone browsers) gives a sandboxed iframe no events at all, not even to
// listeners the app itself added, because the book's frame may not run scripts. The
// app can still read and change the frame from outside, so what can't be heard is
// looked at instead: watchPage reads the selection and the frame's address a few
// times a second, and a frame that hears nothing has its links turned into addresses
// that can be seen changing.
import type { Rendition } from 'epubjs'
import { FLOW_MARK, pageCss, type Prefs } from './readerPrefs'

export interface PageContents {
  document: Document
  window: Window
  sectionIndex: number
  cfiFromRange: (range: Range) => string
}

interface Internals {
  manager?: {
    clear: () => void
    updateLayout: () => void
    container?: HTMLElement
    views: { forEach: (fn: (view: { pane?: { render: () => void } }) => void) => void }
  }
  getContents: () => PageContents[]
}

const inner = (r: Rendition) => r as unknown as Internals
const STYLE_ID = 'rdr-settings'
const sleep = (ms: number) => new Promise((resolve) => window.setTimeout(resolve, ms))

function setSheet(doc: Document, css: string) {
  let sheet: Element | null = doc.getElementById(STYLE_ID)
  if (!sheet) {
    sheet = doc.createElementNS(doc.documentElement.namespaceURI ?? 'http://www.w3.org/1999/xhtml', 'style')
    sheet.id = STYLE_ID
    // Last, so that it follows the book's own.
    ;(doc.head ?? doc.documentElement).appendChild(sheet)
  }
  if (sheet.textContent !== css) sheet.textContent = css
}

// Running text, as the book itself aligns it; asked before our own sheet can answer.
function markFlow(doc: Document) {
  for (const el of Array.from(doc.querySelectorAll('p, li, blockquote, dd'))) {
    const align = doc.defaultView?.getComputedStyle(el).textAlign ?? ''
    if (align === 'start' || align === 'left' || align === 'justify' || align === 'auto' || align === '-webkit-auto') el.setAttribute(FLOW_MARK, '')
  }
}

// installSettings: every chapter of this rendition follows the settings.
export function installSettings(r: Rendition, prefs: () => Prefs) {
  r.hooks.content.register((contents: PageContents) => {
    markFlow(contents.document)
    setSheet(contents.document, pageCss(prefs()))
    void settled(r)
  })
}

export function applySettingsSheet(r: Rendition, p: Prefs) {
  for (const c of inner(r).getContents()) setSheet(c.document, pageCss(p))
}

// epub.js draws a highlight once, where its words are then, and the words move after:
// the text size and fonts arrive a moment later.
export function redrawMarks(r: Rendition) {
  inner(r).manager?.views.forEach((v) => {
    try {
      v.pane?.render()
    } catch {
      // a chapter on its way out
    }
  })
}

// settled waits until the chapter on screen has stopped changing: its fonts are in,
// and epub.js, which checks only every 350 ms, has made its frame as wide as the
// columns. Highlights are put back in their place then.
export async function settled(r: Rendition): Promise<void> {
  const frames = () => Array.from(inner(r).manager?.container?.querySelectorAll('iframe') ?? [])
  await Promise.all(frames().map((f) => f.contentDocument?.fonts?.ready.catch(() => undefined)))
  let last = ''
  let same = 0
  for (let i = 0; i < 14 && same < 3; i++) {
    await sleep(130)
    const now = frames()
      .map((f) => `${f.offsetWidth}/${f.contentDocument?.body?.scrollWidth}/${f.contentDocument?.fonts?.status}`)
      .join()
    same = now === last ? same + 1 : 0
    last = now
  }
  redrawMarks(r)
}

// A new margin needs new columns, which epub.js only makes for a chapter it opens anew.
export function relayout(r: Rendition) {
  const m = inner(r).manager
  m?.clear()
  m?.updateLayout()
}

export interface WatchOptions {
  rendition: Rendition
  onSelected: (cfi: string, contents: PageContents) => void
  onLink: (link: HTMLAnchorElement, contents: PageContents) => boolean // false: not for the reader
}

// A link that goes somewhere else in the book. Not one to the web.
const inBook = (a: Element) => {
  const href = a.getAttribute('href') ?? ''
  return !!href && !/^[a-z][a-z\d+.-]*:/i.test(href)
}

export function watchPage(o: WatchOptions): { tell: (cfi: string) => void; stop: () => void } {
  // Links of the frames that hear nothing, by their made-up address.
  const links = new WeakMap<Document, HTMLAnchorElement[]>()
  let told = ''
  let seen = ''

  o.rendition.hooks.content.register((contents: PageContents) => {
    const doc = contents.document
    let hears = false
    const probe = () => {
      hears = true
    }
    doc.addEventListener('rdr-probe', probe)
    doc.dispatchEvent(new Event('rdr-probe'))
    doc.removeEventListener('rdr-probe', probe)
    if (hears) {
      // Before epub.js's own handler on the link, which would turn to the place.
      doc.addEventListener(
        'click',
        (e) => {
          const a = (e.target as Element | null)?.closest?.('a[href]') as HTMLAnchorElement | null
          if (a && inBook(a) && o.onLink(a, contents)) {
            e.preventDefault()
            e.stopPropagation()
          }
        },
        true,
      )
      return
    }
    const list = Array.from(doc.querySelectorAll<HTMLAnchorElement>('a[href]')).filter(inBook)
    links.set(doc, list)
    // The real address stays beside it, and a tap can only be seen in the frame's address.
    // In full: a book's frame has a <base>, and a bare "#" would leave the frame for it.
    const here = doc.URL.split('#')[0]
    list.forEach((a, i) => {
      a.setAttribute('data-rdr-href', a.getAttribute('href') ?? '')
      a.setAttribute('href', `${here}#rdr-link-${i}`)
    })
  })

  const tick = () => {
    if (document.visibilityState === 'hidden') return
    let selected = false
    for (const c of inner(o.rendition).getContents()) {
      const sel = c.window.getSelection()
      if (sel && !sel.isCollapsed && sel.rangeCount > 0) {
        selected = true
        let cfi = ''
        try {
          cfi = c.cfiFromRange(sel.getRangeAt(0))
        } catch {
          continue
        }
        // Twice the same: the finger has let go of the handles, or rests on them.
        if (cfi === seen && cfi !== told) {
          told = cfi
          o.onSelected(cfi, c)
        }
        seen = cfi
      }
      const m = /^#rdr-link-(\d+)$/.exec(c.window.location.hash)
      if (m) {
        const a = links.get(c.document)?.[Number(m[1])]
        // Back to no address, so the same link can be seen being tapped again.
        c.window.location.hash = ''
        if (a) o.onLink(a, c)
      }
    }
    if (!selected) {
      told = ''
      seen = ''
    }
  }
  const timer = window.setInterval(tick, 300)
  return {
    // epub.js heard the selection itself: no need to say it again.
    tell: (cfi) => {
      told = cfi
      seen = cfi
    },
    stop: () => window.clearInterval(timer),
  }
}
