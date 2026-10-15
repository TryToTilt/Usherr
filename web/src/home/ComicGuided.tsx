// Home fork: the comic reader's guided view. The page glides so each panel
// fills the screen in reading order, then on to the next page, as a comic
// app's guided view does. The panels come from the server's AI panel
// detector (worked out once per page, a few pages ahead). A page with no
// panels (a splash page, a cover) is shown whole.
//
// Drawing is kept cheap so phones don't flicker: the page and its dimming
// are one layer that only moves (a transform the graphics chip does), and a
// turned page replaces the old one only once its picture is decoded.
import { forwardRef, useCallback, useEffect, useImperativeHandle, useLayoutEffect, useRef, useState } from 'react'
import { t } from '../i18n'
import { comicsApi, type ComicPanel } from './comicsApi'

export interface ComicGuidedHandle {
  next: () => void
  prev: () => void
  wholePage: () => void
}

interface Props {
  issueId: number
  page: number
  total: number
  onPage: (page: number) => void
  onPanel?: (index: number, count: number) => void // -1: the whole page
}

const MARGIN = 0.03 // a little of the gutter around each panel, as a share of the page

interface Shown {
  page: number
  src: string
  w: number // the picture's own size
  h: number
}

// pageWidth is how wide a page needs to be for panels to stay sharp when
// zoomed on this screen: about 1.6 times its narrow side, in steps, at most 2400.
function pageWidth(): number {
  const px = Math.min(window.innerWidth, window.innerHeight) * (window.devicePixelRatio || 1)
  return Math.min(2400, Math.max(1200, Math.ceil((px * 1.6) / 400) * 400))
}

const ComicGuided = forwardRef<ComicGuidedHandle, Props>(function ComicGuided({ issueId, page, total, onPage, onPanel }, ref) {
  const stage = useRef<HTMLDivElement>(null)
  const [view, setView] = useState({ w: 0, h: 0 })
  const [shown, setShown] = useState<Shown | null>(null)
  const [panels, setPanels] = useState<Record<number, ComicPanel[]>>({})
  const [idx, setIdx] = useState(-1)
  const entry = useRef<'first' | 'last'>('first') // where a newly turned page starts
  const [animate, setAnimate] = useState(false)
  const touch = useRef<{ x: number; y: number } | null>(null)
  const [width] = useState(pageWidth)
  // A page that didn't arrive: 'slow' after a while, 'failed' after a few tries.
  const [trouble, setTrouble] = useState<'' | 'slow' | 'failed'>('')
  const [attempt, setAttempt] = useState(0)

  useLayoutEffect(() => {
    const el = stage.current
    if (!el) return
    const ro = new ResizeObserver(() => setView({ w: el.clientWidth, h: el.clientHeight }))
    ro.observe(el)
    setView({ w: el.clientWidth, h: el.clientHeight })
    return () => ro.disconnect()
  }, [])

  // This page's panels (and the next page's, ahead of time).
  useEffect(() => {
    let live = true
    for (const n of [page, page + 1]) {
      if (n > total || panels[n]) continue
      comicsApi
        .panels(issueId, n)
        .then((r) => live && setPanels((m) => ({ ...m, [n]: r.panels ?? [] })))
        .catch(() => live && setPanels((m) => ({ ...m, [n]: [] })))
    }
    if (page < total) new Image().src = comicsApi.pageUrl(issueId, page + 1, width)
    return () => {
      live = false
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- fetched once per page
  }, [issueId, page, total])

  // The turned page is decoded first; the old one stays up until then. A
  // page that doesn't arrive is asked for again (twice), then says so.
  useEffect(() => {
    let live = true
    let tries = 0
    setTrouble('')
    const base = comicsApi.pageUrl(issueId, page, width)
    const slow = window.setTimeout(() => live && setTrouble((t) => t || 'slow'), 12000)
    const load = () => {
      const src = tries === 0 ? base : `${base}&try=${tries}-${attempt}`
      const im = new Image()
      const show = () => {
        if (!live || !im.naturalWidth) return
        window.clearTimeout(slow)
        setTrouble('')
        setAnimate(false)
        setShown({ page, src, w: im.naturalWidth, h: im.naturalHeight })
      }
      im.onload = () => void im.decode().then(show, show)
      im.onerror = () => {
        if (!live) return
        tries++
        if (tries < 3) window.setTimeout(load, 900 * tries)
        else {
          window.clearTimeout(slow)
          setTrouble('failed')
        }
      }
      im.src = src
    }
    load()
    return () => {
      live = false
      window.clearTimeout(slow)
    }
  }, [issueId, page, width, attempt])

  const list = shown ? panels[shown.page] : undefined
  // A new page starts on its first panel (or its last, going back), without gliding there.
  useEffect(() => {
    if (!list) return
    setIdx(list.length === 0 ? -1 : entry.current === 'last' ? list.length - 1 : 0)
    // eslint-disable-next-line react-hooks/exhaustive-deps -- when the shown page's panels arrive
  }, [list, shown?.page])
  useEffect(() => onPanel?.(idx, list?.length ?? 0), [idx, list, onPanel])

  const next = useCallback(() => {
    setAnimate(true)
    if (shown?.page === page && list && idx < list.length - 1) return setIdx(idx + 1)
    if (page < total) {
      entry.current = 'first'
      onPage(page + 1)
    }
  }, [shown, list, idx, page, total, onPage])
  const prev = useCallback(() => {
    setAnimate(true)
    if (shown?.page === page && list && idx > 0) return setIdx(idx - 1)
    if (page > 1) {
      entry.current = 'last'
      onPage(page - 1)
    }
  }, [shown, list, idx, page, onPage])
  useImperativeHandle(ref, () => ({ next, prev, wholePage: () => (setAnimate(true), setIdx(-1)) }), [next, prev])

  // Where the page sits: the panel (or the whole page) centred, as large as fits.
  const cur = list && idx >= 0 && idx < list.length ? list[idx] : null
  let layer: React.CSSProperties = { visibility: 'hidden' }
  if (shown && view.w > 0) {
    const p = cur ?? { x: 0, y: 0, w: 1, h: 1 }
    const m = cur ? MARGIN : 0
    const bx = Math.max(0, p.x - m) * shown.w
    const by = Math.max(0, p.y - m) * shown.h
    const bw = (Math.min(1, p.x + p.w + m) - Math.max(0, p.x - m)) * shown.w
    const bh = (Math.min(1, p.y + p.h + m) - Math.max(0, p.y - m)) * shown.h
    const s = Math.min(view.w / bw, view.h / bh)
    const tx = view.w / 2 - s * (bx + bw / 2)
    const ty = view.h / 2 - s * (by + bh / 2)
    layer = {
      width: shown.w,
      height: shown.h,
      transform: `translate3d(${tx}px, ${ty}px, 0) scale(${s})`,
      transition: animate ? 'transform 0.45s ease' : 'none',
    }
  }

  return (
    <div
      ref={stage}
      className="cx-guided"
      onClick={(e) => {
        const r = (e.currentTarget as HTMLElement).getBoundingClientRect()
        if (e.clientX - r.left < r.width / 3) prev()
        else next()
      }}
      onTouchStart={(e) => (touch.current = { x: e.touches[0].clientX, y: e.touches[0].clientY })}
      onTouchEnd={(e) => {
        const start = touch.current
        touch.current = null
        if (!start) return
        const dx = e.changedTouches[0].clientX - start.x
        const dy = e.changedTouches[0].clientY - start.y
        if (Math.abs(dx) > 50 && Math.abs(dx) > Math.abs(dy)) {
          e.preventDefault()
          if (dx < 0) next()
          else prev()
        }
      }}
    >
      {shown && (
        <div className="cx-guided-layer" style={layer}>
          <img src={shown.src} alt={t('Page {n}', { n: shown.page })} draggable={false} />
          {/* Everything but the panel is dimmed, so the next one doesn't distract. */}
          {cur && (
            <div
              className="cx-guided-mask"
              aria-hidden="true"
              style={{
                left: cur.x * shown.w,
                top: cur.y * shown.h,
                width: cur.w * shown.w,
                height: cur.h * shown.h,
                boxShadow: `0 0 0 ${Math.max(shown.w, shown.h) * 6}px rgba(8, 8, 10, 0.9)`,
              }}
            />
          )}
        </div>
      )}
      {trouble ? (
        <div className="hint cx-guided-msg">
          {trouble === 'failed' ? t('This page didn’t load.') : t('This page is taking a while to load…')}{' '}
          <button className="btn-sm" onClick={(e) => (e.stopPropagation(), setAttempt((a) => a + 1))}>
            {t('Try again')}
          </button>
        </div>
      ) : (
        (!shown || !list) && <p className="hint cx-guided-msg">{shown ? t('Finding the panels…') : t('Loading the page…')}</p>
      )}
    </div>
  )
})

export default ComicGuided
