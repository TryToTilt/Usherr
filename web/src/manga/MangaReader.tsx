// Usherr: the Manga reader. Right to left with the book reader's page curl, top to
// bottom for manhwa, or panel by panel as the Comics reader does it, remembered per
// series. Two pages on a wide screen, one on a phone. It stays open from one volume
// to the next: each gets a fresh start.
import { useCallback, useEffect, useMemo, useRef, useState, type CSSProperties, type PointerEvent as ReactPointerEvent } from 'react'
import { createPortal, flushSync } from 'react-dom'
import type { ImageCurl, ImageLoader, ImageTurn, CurlPicture } from '../bookshelf/curl/imageCurl'
import type { CurlDrag, TurnDir } from '../bookshelf/curl/pageCurl'
import Marks from '../bookshelf/Marks'
import { pageOfMark } from '../bookshelf/marksApi'
import { saveMangaPlace } from '../bookshelf/progress'
import { animateTurn, reducedMotion, storedTurn } from '../bookshelf/turn'
import Icon from '../components/Icon'
import ComicGuided, { type ComicGuidedHandle, type GuidedSource } from '../home/ComicGuided'
import ComicWebtoon, { type ComicWebtoonHandle } from '../home/ComicWebtoon'
import { keyTurn, swipeTurn, tapTurn } from '../home/comicReading'
import '../home/comics.css'
import { t } from '../i18n'
import { mangaApi, type MangaLayout, type MangaPages, type ReadDirection } from './mangaApi'
import { lastOf, pageBox, pageRatio, pageWidthFor, panelsOrHalves, placeFitted, placeSpread, spreadIndexOf, spreadsOf, type PageFit, type PageSize } from './mangaReading'
import { trimPage, type TrimmedPage } from './pageTrim'
import type { PdfPages } from './pdfPages'
import { lookFilter, saveLook, storedLook, type ReaderLook } from './readerLook'
import { saveTurnEffect, storedTurnEffect, type TurnEffect } from './turnEffect'
import './manga.css'

const BACKGROUND = '#0b0b0d'
const BACKGROUND_DARK_PAGES = '#f4f4f2' // the dark-pages filter turns it into BACKGROUND
const HINT_KEY = 'usherr.manga.tap-hint'
const AWAKE_MINUTES = 15 // the screen may sleep when nothing has been read for this long
const LONG_PRESS_MS = 600
const READ_AHEAD = 4 // spreads fetched ahead of the one being read

// volumeId is a chapter's id when chapter is set: it reads the same, and the last
// page can go from one to the other.
export default function MangaReader({ volumeId, chapter = false, startPage = 0, onClose }: { volumeId: number; chapter?: boolean; startPage?: number; onClose: () => void }) {
  const [now, setNow] = useState({ id: volumeId, chapter, from: startPage })
  return <ReaderVolume key={`${now.chapter ? 'c' : 'v'}${now.id}`} volumeId={now.id} chapter={now.chapter} startPage={now.from} onClose={onClose} onNext={(id, isChapter) => setNow({ id, chapter: isChapter, from: 0 })} />
}

interface Press {
  id: number
  x: number
  y: number
  at: number
  drag: CurlDrag | null
  swipe: boolean
  moved: boolean
  turn: 1 | -1
}

function ReaderVolume({ volumeId, chapter, startPage, onClose, onNext }: { volumeId: number; chapter: boolean; startPage: number; onClose: () => void; onNext: (id: number, chapter: boolean) => void }) {
  const [info, setInfo] = useState<MangaPages | null>(null)
  const [error, setError] = useState('')
  const [page, setPage] = useState(1)
  const [direction, setDirection] = useState<ReadDirection>('rtl')
  const [layout, setLayout] = useState<MangaLayout>('')
  const [shift, setShift] = useState(false)
  const [effect, setEffect] = useState<TurnEffect>(storedTurnEffect)
  const [look, setLook] = useState<ReaderLook>(storedLook)
  const [optsOpen, setOptsOpen] = useState(false)
  const [moreOpen, setMoreOpen] = useState(false)
  const [marksOpen, setMarksOpen] = useState(false)
  const [hint, setHint] = useState(false)
  const [stageSize, setStageSize] = useState({ w: 0, h: 0 })
  const [widest, setWidest] = useState(0)
  const [curl, setCurl] = useState<ImageCurl | null>(null)
  const [pdfUrls, setPdfUrls] = useState<Record<number, string>>({})
  const [trims, setTrims] = useState<Record<string, TrimmedPage>>({})
  const [panelAt, setPanelAt] = useState({ index: -1, count: 0 })
  const [saving, setSaving] = useState<{ n: number; x: number; y: number } | null>(null)
  const pdf = useRef<PdfPages | null>(null)

  const stage = useRef<HTMLDivElement>(null)
  const sheet = useRef<HTMLDivElement>(null)
  const webtoon = useRef<ComicWebtoonHandle>(null)
  const guidedView = useRef<ComicGuidedHandle>(null)
  const loaderRef = useRef<ImageLoader | null>(null)
  const turning = useRef(false)
  const pending = useRef<1 | -1 | 0>(0)
  const press = useRef<Press | null>(null)
  const pressTimer = useRef<number | undefined>(undefined)
  const trimsAsked = useRef(new Set<string>())

  const total = info?.pages ?? 0
  const sizes = info?.sizes
  const isPdf = !!info?.pdf
  const scroll = layout === 'webtoon' && !isPdf // a PDF's pages are drawn a few at a time
  const guided = layout === 'panels' && !isPdf
  const flat = scroll || guided // the views that aren't pages laid out on the stage
  const rtl = direction === 'rtl' && !scroll
  const wideScreen = stageSize.w >= 900 && stageSize.w > stageSize.h
  const twoUp = !flat && (layout === 'double' || (layout === '' && wideScreen))
  const reduced = reducedMotion()
  const fit: PageFit = flat ? 'page' : look.fit
  const turnEffect: TurnEffect = reduced ? 'none' : fit === 'page' ? effect : effect === 'none' ? 'none' : 'slide' // the curl turns whole pages only
  const trimming = look.trim && !flat

  const spreads = useMemo(() => spreadsOf(total, sizes, twoUp, shift), [total, sizes, twoUp, shift])
  const index = spreadIndexOf(spreads, page)
  const ratio = useMemo(() => pageRatio(sizes), [sizes])
  const box = useMemo(() => pageBox(stageSize.w, stageSize.h, ratio, twoUp), [stageSize, ratio, twoUp])

  // The page as it comes from the server or the PDF, before any trimming.
  const rawUrl = useCallback((n: number) => (isPdf ? (pdfUrls[n] ?? '') : (chapter ? mangaApi.chapterPageUrl : mangaApi.pageUrl)(volumeId, n, widest > 0 ? widest : undefined)), [isPdf, pdfUrls, chapter, volumeId, widest])
  const pageUrl = useCallback((n: number) => (trimming ? (trims[rawUrl(n)]?.url ?? '') : rawUrl(n)), [trimming, trims, rawUrl])
  // A trimmed page has its own shape. What pairs with what stays as the scan has it.
  const liveSizes = useMemo<readonly PageSize[] | undefined>(() => {
    if (!trimming || !sizes) return sizes
    return sizes.map((s, i): PageSize => {
      const c = trims[rawUrl(i + 1)]
      return c && c.w > 0 ? [c.w, c.h] : s
    })
  }, [trimming, sizes, trims, rawUrl])

  const here = spreads[index] ?? [page]
  const fitted = useMemo(() => (fit !== 'page' && !flat && total > 0 && stageSize.w > 0 ? placeFitted(here, stageSize.w, stageSize.h, liveSizes, ratio, rtl, fit) : null), [fit, flat, total, stageSize, here, liveSizes, ratio, rtl])
  const sheetBox = fitted?.box ?? box

  // A copy of the pages about as wide as the screen shows them. Once asked for a
  // bigger one (the phone turned sideways, say) it stays that big.
  const across = twoUp ? 2 : 1
  const shownWidth = fitted ? Math.max(...fitted.items.map((p) => p.w)) : (box.x1 - box.x0) / across
  const wantWidth = pageWidthFor(shownWidth * Math.min(window.devicePixelRatio || 1, 2))
  useEffect(() => {
    if (wantWidth === 0 && stageSize.w > 0) setWidest(-1)
    else setWidest((w) => (w < 0 ? w : Math.max(w, wantWidth)))
  }, [wantWidth, stageSize.w])

  const pictureOf = useCallback(
    (i: number): CurlPicture | null => {
      const spread = spreads[i]
      if (!spread || !spread.every((n) => pageUrl(n))) return null
      const w = box.x1 - box.x0
      const h = box.y1 - box.y0
      return {
        key: `${spread.join('-')}@${w}x${h}${rtl ? 'r' : ''}${twoUp ? 'd' : ''}${trimming ? 't' : ''}${look.dark ? 'k' : ''}${widest}`,
        width: w,
        height: h,
        background: look.dark ? BACKGROUND_DARK_PAGES : BACKGROUND,
        items: placeSpread(spread, i, box, liveSizes, ratio, twoUp, rtl).map((p) => ({ url: pageUrl(p.n), x: p.x, y: p.y, w: p.w, h: p.h })),
      }
    },
    [spreads, box, liveSizes, ratio, twoUp, rtl, pageUrl, widest, trimming, look.dark],
  )

  // The panel-by-panel view, from the same code as the Comics reader. On a screen
  // held upright, a spread with no panels found is read a page at a time.
  const sizesNow = useRef(sizes)
  sizesNow.current = sizes
  const guidedSource = useMemo<GuidedSource>(
    () => ({
      pageUrl: (n, width) => (chapter ? mangaApi.chapterPageUrl : mangaApi.pageUrl)(volumeId, n, width),
      panels: (n, panelsRtl) => mangaApi.panels(volumeId, n, panelsRtl, chapter).then((r) => ({ panels: panelsOrHalves(r.panels ?? [], sizesNow.current?.[n - 1], panelsRtl, window.innerWidth < window.innerHeight) })),
    }),
    [volumeId, chapter],
  )
  const onPanel = useCallback((i: number, count: number) => setPanelAt({ index: i, count }), [])

  // ---- opening ----

  const closeNow = useRef(onClose)
  closeNow.current = onClose
  useEffect(() => {
    // Your place is kept for you, so it opens there on any device. A volume read to
    // the end has no place, and opens at the first page.
    const kept = fetch(`/api/home/position?item=manga:${chapter ? 'c' : 'v'}:${volumeId}`, { credentials: 'include' })
      .then((x) => (x.ok ? (x.json() as Promise<{ position?: number }>) : null))
      .catch(() => null)
    let gone = false
    Promise.all([chapter ? mangaApi.chapterPages(volumeId) : mangaApi.pages(volumeId), kept])
      .then(async ([answer, at]) => {
        // A PDF's pages are drawn here, from the file.
        let r = answer
        if (r.pdf) {
          const doc = await (await import('./pdfPages')).openPdf(chapter ? mangaApi.chapterFileUrl(volumeId) : mangaApi.fileUrl(volumeId))
          if (gone) {
            doc.destroy()
            return
          }
          pdf.current = doc
          r = { ...r, pages: doc.count, sizes: Array.from({ length: doc.count }, () => doc.size) }
        }
        if (gone) return
        const pages = r.pages ?? 0
        const from = startPage > 0 ? startPage : at?.position ? Math.round(at.position) : 1
        setDirection(r.direction === 'ltr' ? 'ltr' : 'rtl')
        setLayout(r.layout === 'single' || r.layout === 'double' || r.layout === 'webtoon' || r.layout === 'panels' ? r.layout : '')
        setShift(!!r.shift)
        setPage(pages ? Math.min(Math.max(1, from), pages) : 1)
        setInfo(r)
        if (r.layout !== 'webtoon') {
          try {
            if (!localStorage.getItem(HINT_KEY)) {
              localStorage.setItem(HINT_KEY, '1')
              setHint(true)
            }
          } catch {
            /* private mode */
          }
        }
      })
      .catch((e: Error) => setError(e.message))
    return () => {
      gone = true
      pdf.current?.destroy()
      pdf.current = null
    }
  }, [volumeId, chapter, startPage])

  useEffect(() => {
    if (!hint) return
    const tm = window.setTimeout(() => setHint(false), 7000)
    return () => window.clearTimeout(tm)
  }, [hint])

  // The stage's size, which everything is laid out by.
  useEffect(() => {
    const el = stage.current
    if (!el) return
    const measure = () => setStageSize((s) => (s.w === el.clientWidth && s.h === el.clientHeight ? s : { w: el.clientWidth, h: el.clientHeight }))
    measure()
    const ro = new ResizeObserver(measure)
    ro.observe(el)
    return () => ro.disconnect()
  }, [flat, total])

  // The curl's code and its canvas are there only while it's the choice.
  const curlOn = total > 0 && !flat && turnEffect === 'curl'
  useEffect(() => {
    if (!curlOn) return
    let gone = false
    let made: ImageCurl | null = null
    // Not loaded (offline, say): the page slides.
    import('../bookshelf/curl/imageCurl')
      .then(({ ImageCurl: Maker, ImageLoader: Loader }) => {
        if (gone || !stage.current) return
        loaderRef.current ??= new Loader()
        made = Maker.create(stage.current, loaderRef.current)
        setCurl(made)
      })
      .catch(() => undefined)
    return () => {
      gone = true
      made?.destroy()
      setCurl(null)
    }
  }, [curlOn])

  // A PDF's pages near you are drawn, and those far away let go.
  useEffect(() => {
    const doc = pdf.current
    if (!doc || total === 0) return
    const wanted = [index, index + 1, index - 1, index + 2].flatMap((i) => spreads[i] ?? [])
    const keep = [index - 2, index - 1, index, index + 1, index + 2, index + 3].flatMap((i) => spreads[i] ?? [])
    doc.keepOnly(keep)
    const width = widest > 0 ? widest : 1600
    let gone = false
    for (const n of wanted) {
      void doc
        .url(n, width)
        .then((u) => {
          if (!gone) setPdfUrls((m) => (m[n] === u ? m : { ...m, [n]: u }))
        })
        .catch(() => undefined)
    }
    return () => {
      gone = true
    }
  }, [index, spreads, total, widest])

  // Trimming the borders: the pages near you are cut, and those far away let go.
  useEffect(() => {
    if (!trimming || total === 0) return
    const wanted = [index, index + 1, index - 1, index + 2].flatMap((i) => spreads[i] ?? []).map(rawUrl)
    const keep = new Set([index - 3, index - 2, index - 1, index, index + 1, index + 2, index + 3].flatMap((i) => spreads[i] ?? []).map(rawUrl))
    for (const u of wanted) {
      if (!u || trimsAsked.current.has(u)) continue
      trimsAsked.current.add(u)
      void trimPage(u).then((r) => {
        if (trimsAsked.current.has(u)) setTrims((m) => ({ ...m, [u]: r }))
        else if (r.url !== u) URL.revokeObjectURL(r.url)
      })
    }
    setTrims((m) => {
      const far = Object.keys(m).filter((u) => !keep.has(u))
      if (far.length === 0) return m
      const next = { ...m }
      for (const u of far) {
        if (next[u].url !== u) URL.revokeObjectURL(next[u].url)
        delete next[u]
        trimsAsked.current.delete(u)
      }
      return next
    })
  }, [trimming, total, index, spreads, rawUrl])
  const trimsNow = useRef(trims)
  trimsNow.current = trims
  useEffect(
    () => () => {
      trimsAsked.current.clear()
      for (const [u, r] of Object.entries(trimsNow.current)) if (r.url !== u) URL.revokeObjectURL(r.url)
    },
    [],
  )

  // Pages either side are made ready, and what's far away is let go.
  useEffect(() => {
    if (total === 0 || flat) return
    const near = [index, index + 1, index - 1, index + 2].map(pictureOf).filter((p): p is CurlPicture => !!p)
    if (curl) {
      const tm = window.setTimeout(() => {
        curl.keepOnly(near.slice(0, 3).map((p) => p.key))
        void curl.prepare(near.slice(0, 3))
      }, 60)
      return () => window.clearTimeout(tm)
    }
    for (const p of near) for (const it of p.items) new Image().src = it.url
  }, [curl, index, pictureOf, flat, total])

  // And a few more ahead, so reading on a slow connection doesn't wait at each turn,
  // unless the device asks to save data.
  useEffect(() => {
    if (total === 0 || flat || isPdf) return
    if ((navigator as Navigator & { connection?: { saveData?: boolean } }).connection?.saveData) return
    const tm = window.setTimeout(() => {
      for (let i = index + 1; i <= index + READ_AHEAD; i++) for (const n of spreads[i] ?? []) new Image().src = rawUrl(n)
    }, 400)
    return () => window.clearTimeout(tm)
  }, [total, flat, isPdf, index, spreads, rawUrl])

  // A new page that doesn't fit starts at its beginning, on the side it reads from.
  useEffect(() => {
    const el = stage.current
    if (!el || fit === 'page') return
    el.scrollTop = 0
    el.scrollLeft = rtl ? el.scrollWidth : 0
  }, [fit, index, rtl, stageSize.w])

  // The screen stays on while reading, until nothing has been read for a while.
  useEffect(() => {
    const nav = navigator as Navigator & { wakeLock?: { request: (k: 'screen') => Promise<{ release: () => Promise<void> }> } }
    if (!nav.wakeLock || total === 0) return
    let gone = false
    let lock: { release: () => Promise<void> } | null = null
    const hold = () => {
      if (lock || document.visibilityState !== 'visible') return
      nav.wakeLock
        ?.request('screen')
        .then((l) => {
          if (gone) void l.release()
          else lock = l
        })
        .catch(() => undefined)
    }
    const onShow = () => {
      lock = null // the browser lets go when the page is hidden
      hold()
    }
    hold()
    document.addEventListener('visibilitychange', onShow)
    const tm = window.setTimeout(() => {
      void lock?.release()
      lock = null
      gone = true
    }, AWAKE_MINUTES * 60_000)
    return () => {
      gone = true
      window.clearTimeout(tm)
      document.removeEventListener('visibilitychange', onShow)
      void lock?.release()
    }
  }, [page, total])

  // ---- your place ----

  const pageNow = useRef({ page, total: 0 })
  useEffect(() => {
    if (!total) return
    pageNow.current = { page, total }
    const tm = window.setTimeout(() => saveMangaPlace(volumeId, page, total, false, chapter), 1200)
    return () => window.clearTimeout(tm)
  }, [page, total, volumeId, chapter])
  useEffect(
    () => () => {
      const at = pageNow.current
      if (at.total > 0) saveMangaPlace(volumeId, at.page, at.total, true, chapter)
    },
    [volumeId, chapter],
  )

  // ---- turning ----

  // What a turn needs, as it is now: gestures and timers outlive a render.
  const live = useRef({ index, spreads, rtl, twoUp, box, curl, turnEffect, pictureOf, scroll, guided })
  live.current = { index, spreads, rtl, twoUp, box, curl, turnEffect, pictureOf, scroll, guided }

  const dirOf = (d: 1 | -1): TurnDir => (d > 0 ? 'next' : 'prev')

  const imageTurn = useCallback((d: 1 | -1): ImageTurn | null => {
    const l = live.current
    const to = l.index + d
    const current = l.pictureOf(l.index)
    const target = l.pictureOf(to)
    if (!current || !target) return null
    return {
      page: l.box,
      dir: dirOf(d),
      rtl: l.rtl,
      spread: l.twoUp,
      current,
      target,
      speed: storedTurn().speed,
      commit: () => flushSync(() => setPage(lastOf(l.spreads[to]))),
    }
  }, [])

  // The other way of turning, and the one when the curl can't.
  const plainTurn = useCallback(async (d: 1 | -1) => {
    const l = live.current
    const to = l.spreads[l.index + d]
    if (!to) return
    const shown: TurnDir = (d > 0) !== l.rtl ? 'next' : 'prev' // right to left, the page goes the other way
    await animateTurn(sheet.current, l.turnEffect === 'slide' || l.turnEffect === 'curl' ? 'slide' : 'none', storedTurn().speed, shown, () => flushSync(() => setPage(lastOf(to))))
  }, [])

  const turn = useCallback(
    async (d: 1 | -1) => {
      const l = live.current
      if (l.scroll) {
        webtoon.current?.scroll(d * 0.85)
        return
      }
      if (l.guided) {
        if (d > 0) guidedView.current?.next()
        else guidedView.current?.prev()
        return
      }
      if (!l.spreads[l.index + d]) return
      if (turning.current) {
        l.curl?.hurry()
        pending.current = d
        return
      }
      turning.current = true
      try {
        const it = l.turnEffect === 'curl' ? imageTurn(d) : null
        const done = it && l.curl && !l.curl.dead ? (await l.curl.play(it)) === 'done' : false
        if (!done) await plainTurn(d)
      } finally {
        turning.current = false
      }
      const again = pending.current
      pending.current = 0
      if (again) void turn(again)
    },
    [imageTurn, plainTurn],
  )

  const jumpTo = (n: number) => {
    const to = Math.min(Math.max(1, n), total || 1)
    if (scroll) webtoon.current?.toPage(to)
    setHint(false)
    setPage(to)
  }

  const placed = useMemo(() => {
    if (total === 0 || flat || stageSize.w === 0) return []
    return fitted ? fitted.items : placeSpread(here, index, box, liveSizes, ratio, twoUp, rtl)
  }, [total, flat, stageSize.w, fitted, here, index, box, liveSizes, ratio, twoUp, rtl])

  // ---- fingers and the mouse ----

  // Holding on a page offers to save it. In two pages, the one under the finger.
  const offerSave = (x: number, y: number) => {
    const r = stage.current?.getBoundingClientRect()
    if (!r || placed.length === 0) return
    const inSheet = x - r.left - sheetBox.x0 + (stage.current?.scrollLeft ?? 0)
    const under = placed.find((p) => inSheet >= p.x && inSheet < p.x + p.w) ?? placed[0]
    setSaving({ n: under.n, x: Math.max(8, Math.min(x - 70, window.innerWidth - 170)), y: Math.max(8, y - 70) }) // clear of the finger
  }

  const onDown = (e: ReactPointerEvent<HTMLDivElement>) => {
    if (e.button !== 0 || !e.isPrimary) return
    press.current = { id: e.pointerId, x: e.clientX, y: e.clientY, at: performance.now(), drag: null, swipe: false, moved: false, turn: 1 }
    window.clearTimeout(pressTimer.current)
    pressTimer.current = window.setTimeout(() => {
      const p = press.current
      if (!p || p.moved || p.drag) return
      press.current = null
      offerSave(p.x, p.y)
    }, LONG_PRESS_MS)
  }

  const onMove = (e: ReactPointerEvent<HTMLDivElement>) => {
    const p = press.current
    if (!p || p.id !== e.pointerId) return
    if (p.drag) {
      p.drag.move(e.clientX, e.clientY)
      return
    }
    if (p.swipe) return
    const dx = e.clientX - p.x
    const dy = e.clientY - p.y
    if (Math.abs(dx) < 10 && Math.abs(dy) < 10) return
    p.moved = true
    window.clearTimeout(pressTimer.current)
    if (Math.abs(dx) < Math.abs(dy) * 1.2) {
      press.current = null // going up or down: not a turn
      return
    }
    // A page wider than the screen moves sideways with the finger instead.
    if (scrollsSideways) {
      press.current = null
      return
    }
    const l = live.current
    p.turn = swipeTurn(dx, l.rtl)
    if (l.turnEffect === 'curl' && l.curl && !l.curl.dead && !turning.current) {
      const it = imageTurn(p.turn)
      const drag = it && l.curl.drag(it, p.x, p.y, p.at)
      if (drag) {
        turning.current = true
        p.drag = drag
        e.currentTarget.setPointerCapture(e.pointerId)
        drag.move(e.clientX, e.clientY)
        return
      }
    }
    p.swipe = true
  }

  const onUp = (e: ReactPointerEvent<HTMLDivElement>) => {
    window.clearTimeout(pressTimer.current)
    const p = press.current
    if (!p || p.id !== e.pointerId) return
    press.current = null
    if (p.drag) {
      void p.drag.end().then(async (r) => {
        if (r.result === 'failed' && r.wanted) await plainTurn(p.turn)
        turning.current = false
      })
      return
    }
    if (p.swipe) {
      if (Math.abs(e.clientX - p.x) > 40) void turn(p.turn)
      return
    }
    if (!p.moved && e.type === 'pointerup') {
      const r = e.currentTarget.getBoundingClientRect()
      void turn(tapTurn((e.clientX - r.left) / r.width, live.current.rtl))
    }
  }

  // ---- the keys ----

  useEffect(() => {
    const key = (e: KeyboardEvent) => {
      // Typing a bookmark's note doesn't turn pages.
      if ((e.target as HTMLElement | null)?.closest?.('input:not([type="range"]), textarea, select')) return
      setHint(false)
      const d = keyTurn(e.key, live.current.rtl)
      if (d !== 0) void turn(d)
      else if (live.current.scroll && e.key === 'ArrowDown') webtoon.current?.scroll(0.2)
      else if (live.current.scroll && e.key === 'ArrowUp') webtoon.current?.scroll(-0.2)
      else if (e.key === 'Escape') closeNow.current()
      else return
      e.preventDefault()
    }
    window.addEventListener('keydown', key)
    document.body.style.overflow = 'hidden'
    return () => {
      window.removeEventListener('keydown', key)
      document.body.style.overflow = ''
    }
  }, [turn])

  // ---- your choices ----

  const remember = (body: { direction?: ReadDirection; layout?: MangaLayout; shift?: 1 | -1 }) => {
    if (info?.seriesId) mangaApi.readerPrefs(info.seriesId, body).catch(() => undefined)
  }
  const chooseDirection = (d: ReadDirection) => {
    setDirection(d)
    remember({ direction: d })
  }
  const chooseLayout = (v: MangaLayout) => {
    setLayout(v)
    setPanelAt({ index: -1, count: 0 })
    remember({ layout: v })
  }
  const togglePanels = () => {
    if (guided) chooseLayout(window.innerWidth >= 900 && window.innerWidth > window.innerHeight ? 'double' : 'single')
    else chooseLayout('panels')
  }
  const chooseEffect = (v: TurnEffect) => {
    setEffect(v)
    saveTurnEffect(v)
  }
  const chooseShift = (v: boolean) => {
    setShift(v)
    remember({ shift: v ? 1 : -1 })
  }
  const changeLook = (patch: Partial<ReaderLook>) => {
    const next = { ...look, ...patch }
    setLook(next)
    saveLook(next)
  }

  const counter = here.length === 2 ? `${here[0]}-${here[1]} / ${total}` : `${here[0]} / ${total}`
  const percent = total > 0 ? Math.round((lastOf(here) / total) * 100) : 0
  const atEnd = total > 0 && page >= total
  const nextUp = info?.next
  const nextHere = nextUp?.have ? nextUp : info?.nextHave
  const item = `manga:v:${volumeId}`
  const filter = lookFilter(look)
  const shownLayout = layout || (wideScreen ? 'double' : 'single')
  const canFullScreen = !!document.fullscreenEnabled
  const scrollsSideways = !!fitted && fitted.box.x1 - fitted.box.x0 > stageSize.w

  return createPortal(
    <div className="cx-reader mr-reader" role="dialog" aria-label={info?.title ?? t('Manga reader')} style={filter ? ({ '--mr-filter': filter } as CSSProperties) : undefined}>
      <div className="cx-reader-bar">
        <button className="btn-sm" onClick={onClose} aria-label={t('Close the reader')}>
          <Icon name="x" size={16} />
        </button>
        <span className="cx-reader-title">{info?.title}</span>
        <span className="cx-reader-count">
          {total ? `${scroll ? `${page} / ${total}` : counter} · ${percent}%` : ''}
          {guided && panelAt.count > 0 && panelAt.index >= 0 ? ` · ${t('panel {n} of {count}', { n: panelAt.index + 1, count: panelAt.count })}` : ''}
        </span>
        {total > 0 && !chapter && (
          <button
            className={`btn-sm${marksOpen ? ' primary' : ''}`}
            onClick={() => {
              setOptsOpen(false)
              setMarksOpen((v) => !v)
            }}
            title={t('Bookmarks')}
            aria-label={t('Bookmarks')}
            aria-pressed={marksOpen}
          >
            <Icon name="bookmark" size={14} />
          </button>
        )}
        {total > 0 && (
          <button
            className={`btn-sm${optsOpen ? ' primary' : ''}`}
            onClick={() => {
              setMarksOpen(false)
              setOptsOpen((v) => !v)
            }}
            title={t('How you read this series')}
            aria-label={t('How you read this series')}
            aria-pressed={optsOpen}
          >
            <Icon name="sliders" size={14} />
          </button>
        )}
        {total > 0 && canFullScreen && (
          <button className="btn-sm" onClick={() => (document.fullscreenElement ? void document.exitFullscreen() : void document.documentElement.requestFullscreen?.())} title={t('Full screen')} aria-label={t('Full screen')}>
            <Icon name="monitor" size={14} />
          </button>
        )}
        {total > 0 && !isPdf && (
          <button className={`btn-sm${guided ? ' primary' : ''}`} onClick={togglePanels} title={guided ? t('Back to whole pages') : t('Read panel by panel (guided view)')} aria-pressed={guided}>
            {t('Panels')}
          </button>
        )}
        {guided && panelAt.count > 0 && (
          <button className="btn-sm" onClick={() => guidedView.current?.wholePage()} title={t('See the whole page; the next tap goes on to the first panel')}>
            {t('Whole page')}
          </button>
        )}
      </div>
      {marksOpen && total > 0 && (
        <div className="cx-marks-panel" aria-label={t('Bookmarks')}>
          <Marks
            item={item}
            here={{ position: `page:${page}`, label: t('Page {n}', { n: page }), percent: Math.round((page / total) * 1000) / 10 }}
            onJump={(m) => {
              jumpTo(pageOfMark(m.position))
              setMarksOpen(false)
            }}
          />
        </div>
      )}
      {optsOpen && total > 0 && (
        <div className="cx-marks-panel cx-reader-opts" aria-label={t('How you read this series')}>
          <h3>{t('Reading direction')}</h3>
          <div className="seg" role="group" aria-label={t('Reading direction')}>
            <button className={direction === 'rtl' ? 'active' : ''} aria-pressed={direction === 'rtl'} disabled={scroll} onClick={() => chooseDirection('rtl')}>
              {t('Right to left')}
            </button>
            <button className={direction === 'ltr' ? 'active' : ''} aria-pressed={direction === 'ltr'} disabled={scroll} onClick={() => chooseDirection('ltr')}>
              {t('Left to right')}
            </button>
          </div>
          <h3>{t('How pages show')}</h3>
          <div className="seg cx-reader-layouts" role="group" aria-label={t('How pages show')}>
            {(
              [
                ['single', t('One page')],
                ['double', t('Two pages')],
                ['webtoon', t('Long scroll')],
                ['panels', t('Panels')],
              ] as const
            ).map(([id, label]) => (
              <button key={id} className={shownLayout === id ? 'active' : ''} aria-pressed={shownLayout === id} disabled={(id === 'webtoon' || id === 'panels') && isPdf} onClick={() => chooseLayout(id)}>
                {label}
              </button>
            ))}
          </div>
          <p className="hint">{t('Remembered for this series, just for you.')}</p>
          {twoUp && (
            <label className="mr-check">
              <input type="checkbox" checked={shift} onChange={(e) => chooseShift(e.target.checked)} />
              <span>
                {t('Shift the pairs by one page')}
                <small className="hint">{t('For when two-page pictures come out split across a turn.')}</small>
              </span>
            </label>
          )}
          {!flat && fit === 'page' && (
            <>
              <h3>{t('Page turn')}</h3>
              <div className="seg" role="group" aria-label={t('Page turn')}>
                {(
                  [
                    ['curl', t('Curl')],
                    ['slide', t('Slide')],
                    ['none', t('None')],
                  ] as const
                ).map(([id, label]) => (
                  <button key={id} className={effect === id ? 'active' : ''} aria-pressed={effect === id} disabled={reduced} onClick={() => chooseEffect(id)}>
                    {label}
                  </button>
                ))}
              </div>
              <p className="hint">{reduced ? t('Your device asks for less motion, so pages just turn.') : t('The same choice as the book reader, on this device.')}</p>
            </>
          )}
          <details className="mr-more" open={moreOpen} onToggle={(e) => setMoreOpen(e.currentTarget.open)}>
            <summary>{t('More for this device')}</summary>
            {!flat && (
              <>
                <h3>{t('Fit')}</h3>
                <div className="seg mr-fits" role="group" aria-label={t('Fit')}>
                  {(
                    [
                      ['page', t('Whole page')],
                      ['width', t('Width')],
                      ['height', t('Height')],
                      ['original', t('Actual size')],
                    ] as const
                  ).map(([id, label]) => (
                    <button key={id} className={look.fit === id ? 'active' : ''} aria-pressed={look.fit === id} onClick={() => changeLook({ fit: id })}>
                      {label}
                    </button>
                  ))}
                </div>
                <label className="mr-check">
                  <input type="checkbox" checked={look.trim} onChange={(e) => changeLook({ trim: e.target.checked })} />
                  <span>{t('Trim white borders')}</span>
                </label>
              </>
            )}
            <h3>{t('Brightness')}</h3>
            <input className="mr-bright" type="range" min={30} max={100} value={look.brightness} onChange={(e) => changeLook({ brightness: Number(e.target.value) })} aria-label={t('Brightness')} />
            <label className="mr-check">
              <input type="checkbox" checked={look.dark} onChange={(e) => changeLook({ dark: e.target.checked })} />
              <span>
                {t('Dark pages')}
                <small className="hint">{t('White paper turns black, for reading in the dark.')}</small>
              </span>
            </label>
          </details>
        </div>
      )}
      {error && <p className="error-text cx-reader-msg">{error}</p>}
      {!info && !error && <p className="hint cx-reader-msg">{t('Opening…')}</p>}
      {total > 0 && (
        <div className="mr-view" onPointerDownCapture={() => setHint(false)}>
          {scroll && <ComicWebtoon ref={webtoon} pageUrl={pageUrl} total={total} start={page} onPage={setPage} />}
          {guided && <ComicGuided ref={guidedView} key={direction} source={guidedSource} page={page} total={total} onPage={setPage} onPanel={onPanel} rtl={rtl} />}
          {!flat && (
            <div
              ref={stage}
              className={`mr-stage${fit === 'page' ? '' : ' mr-scroll'}`}
              style={scrollsSideways ? { touchAction: 'pan-x pan-y pinch-zoom' } : undefined}
              onPointerDown={onDown}
              onPointerMove={onMove}
              onPointerUp={onUp}
              onPointerCancel={onUp}
              onContextMenu={(e) => {
                e.preventDefault()
                press.current = null
                offerSave(e.clientX, e.clientY)
              }}
            >
              <div ref={sheet} className="mr-sheet" style={{ left: sheetBox.x0, top: sheetBox.y0, width: sheetBox.x1 - sheetBox.x0, height: sheetBox.y1 - sheetBox.y0 }}>
                {placed.map((p) =>
                  pageUrl(p.n) ? <img key={p.n} src={pageUrl(p.n)} alt={t('Page {n}', { n: p.n })} decoding="sync" draggable={false} style={{ left: p.x, top: p.y, width: p.w, height: p.h }} /> : null,
                )}
              </div>
            </div>
          )}
          {hint && !scroll && (
            <div className="mr-zones" aria-hidden="true">
              <div className={`mr-zone back ${rtl ? 'right' : 'left'}`}>{t('Tap here to go back')}</div>
              <div className={`mr-zone on ${rtl ? 'left' : 'right'}`}>
                {t('Tap here to go on')}
                {rtl && <small>{t('Right to left: this page turns the other way.')}</small>}
              </div>
            </div>
          )}
        </div>
      )}
      {saving && (
        <>
          {/* On a new press, not a click: lifting the finger that opened this makes a click too. */}
          <button className="mr-save-away" aria-label={t('Close')} onPointerDown={() => setSaving(null)} />
          <a className="btn-sm primary mr-save" style={{ left: saving.x, top: saving.y }} href={isPdf ? rawUrl(saving.n) : (chapter ? mangaApi.chapterPageUrl : mangaApi.pageUrl)(volumeId, saving.n)} download={`${info?.title ?? 'Manga'} - page ${saving.n}`} onClick={() => setSaving(null)}>
            {t('Save page {n}', { n: saving.n })}
          </a>
        </>
      )}
      {atEnd && (
        <div className="cx-reader-end mr-end">
          {nextUp && !nextUp.have && <span className="hint">{nextUp.chapter ? (nextUp.wanted ? t('Chapter {n} isn’t here yet. It’s wanted.', { n: nextUp.number }) : t('Chapter {n} isn’t here yet.', { n: nextUp.number })) : nextUp.wanted ? t('Volume {n} isn’t here yet. It’s wanted.', { n: nextUp.number }) : t('Volume {n} isn’t here yet.', { n: nextUp.number })}</span>}
          {nextHere ? (
            <button className="btn-sm primary btn-with-icon" onClick={() => onNext(nextHere.id, !!nextHere.chapter)}>
              {nextHere.chapter ? t('Next: Chapter {n}', { n: nextHere.number }) : t('Next: Volume {n}', { n: nextHere.number })} <Icon name="open" size={14} />
            </button>
          ) : (
            !nextUp && <span className="hint">{chapter ? t('That’s the last chapter here.') : t('That’s the last volume of this series.')}</span>
          )}
        </div>
      )}
      {total > 0 && <input className="cx-reader-slider" type="range" dir={rtl ? 'rtl' : 'ltr'} min={1} max={total} value={page} onChange={(e) => jumpTo(Number(e.target.value))} aria-label={t('Page')} />}
    </div>,
    document.body,
  )
}
