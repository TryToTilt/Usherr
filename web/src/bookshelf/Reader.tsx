import { useCallback, useEffect, useRef, useState } from 'react'
import { Link, useParams, useSearchParams } from 'react-router-dom'
import type { Book as EpubBook, NavItem, Rendition } from 'epubjs'
import { api, type Book } from '../api'
import Icon from '../components/Icon'
import { useDocumentTitle } from '../documentTitle'
import { t } from '../i18n'
import ComicReader from './ComicReader'
import PdfReader from './PdfReader'
import Marks from './Marks'
import { MARK_COLORS, marksApi, type MarkColor, type ReadingMark } from './marksApi'
import { saveProgress } from './progress'
import { hrefOf, isNoteLink, noteText, resolveHref } from './footnotes'
import { applySettingsSheet, installSettings, redrawMarks, relayout, settled, watchPage, type PageContents } from './readerPage'
import { effectOf, FONTS, gapFor, layoutOf, loadPrefs, PREFS_KEY, THEMES, type Effect, type Font, type Justify, type LineSpacing, type Margin, type Prefs, type Reading, type Theme } from './readerPrefs'
import { animateTurn, type TurnSpeed } from './turn'
import type { EpubCurl } from './curl/epubCurl'
import { attachTurnGestures } from './turnGestures'

// The reader: one book, a page at a time. EPUBs are drawn here (epub.js),
// and so are MOBI and AZW3 books, as an EPUB copy the server makes; PDFs are
// drawn by pdf.js (PdfReader.tsx).
export default function Reader() {
  const { id } = useParams()
  const bookId = Number(id)
  // ?at=<bookmark id>: opened from a bookmark on the book's page.
  const at = Number(useSearchParams()[0].get('at')) || 0
  const [book, setBook] = useState<Book | null>(null)
  const [error, setError] = useState('')
  useDocumentTitle(book ? t('{title} · Reading', { title: book.title }) : t('Reading'))

  useEffect(() => {
    api
      .getBook(bookId)
      .then(setBook)
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
  }, [bookId])

  if (error) return <ReaderMessage title={t("Couldn't open this book")} text={error} />
  if (!book) return <div className="bks-loading">{t('Opening…')}</div>
  if (book.ebook.status !== 'downloaded') return <ReaderMessage title={book.title} text={t('There is no ebook for this book yet. Once Mediarium has downloaded it, it opens here.')} />
  const format = (book.ebook.format ?? '').toLowerCase()
  if (format === 'pdf') return <PdfReader book={book} at={at} />
  if (COMIC.has(format)) return <ComicReader book={book} at={at} />
  // Kindle books open as an EPUB copy Mediarium makes for the reader.
  if (KINDLE.has(format)) return <EpubReader book={book} src={`${api.bookFileUrl(book.id)}?as=epub`} at={at} />
  if (format !== 'epub') {
    return (
      <ReaderMessage
        title={book.title}
        text={
          format
            ? t("This ebook is a {format} file, which browsers can't show. Download it for your e-reader, or look for an EPUB version in Mediarium.", { format: format.toUpperCase() })
            : t("This ebook is in a format browsers can't show. Download it for your e-reader, or look for an EPUB version in Mediarium.")
        }
        action={
          <a className="primary btn-with-icon" href={api.bookFileUrl(book.id, true)}>
            <Icon name="download" size={16} /> {t('Download')}
          </a>
        }
      />
    )
  }
  return <EpubReader book={book} src={api.bookFileUrl(book.id)} at={at} />
}

const KINDLE = new Set(['mobi', 'azw', 'azw3', 'prc'])
const COMIC = new Set(['cbz', 'cbr', 'cb7'])

function ReaderMessage({ title, text, action }: { title: string; text: string; action?: React.ReactNode }) {
  return (
    <div className="bks-message">
      <Icon name="book" size={44} />
      <h1>{title}</h1>
      <p>{text}</p>
      <div className="row-actions">
        {action}
        <Link className="btn-with-icon" to="/bookshelf">
          <Icon name="chevron-left" size={16} /> {t('Back to your books')}
        </Link>
      </div>
    </div>
  )
}

// The colours a highlight is drawn in (SVG fill over the words).
const HIGHLIGHT_FILL: Record<MarkColor, string> = { '': '#f5d76e', yellow: '#f5d76e', green: '#8fd694', blue: '#8cc4f2', pink: '#f2a6c8' }

function drawHighlight(r: Rendition, m: ReadingMark, onClick: () => void) {
  try {
    r.annotations.highlight(m.position, { id: m.id }, onClick, 'rdr-hl', { fill: HIGHLIGHT_FILL[m.color], 'fill-opacity': '0.4', 'mix-blend-mode': 'multiply' })
  } catch {
    // a highlight whose place is no longer in the book (a new edition) is only listed
  }
}

function EpubReader({ book, src, at }: { book: Book; src: string; at: number }) {
  const viewer = useRef<HTMLDivElement>(null)
  const epub = useRef<EpubBook | null>(null)
  const rendition = useRef<Rendition | null>(null)
  const [prefs, setPrefs] = useState<Prefs>(loadPrefs)
  const prefsRef = useRef(prefs)
  prefsRef.current = prefs
  const reading = prefs.reading
  const turning = useRef(false)
  const queued = useRef<'next' | 'prev' | null>(null)
  const stage = useRef<HTMLElement>(null)
  const curl = useRef<EpubCurl | null>(null)
  const bookData = useRef<ArrayBuffer | null>(null)
  const [toc, setToc] = useState<NavItem[]>([])
  const [panel, setPanel] = useState<'' | 'toc' | 'settings' | 'marks'>('')
  const [marks, setMarks] = useState<ReadingMark[] | undefined>(undefined)
  const marksRef = useRef(marks)
  marksRef.current = marks
  // A footnote being read, over the page.
  const [note, setNote] = useState<string | null>(null)
  const drawn = useRef(new Set<string>())
  // Words picked in the page, to highlight.
  const [pick, setPick] = useState<{ cfi: string; text: string; clear: () => void } | null>(null)
  const [pickNote, setPickNote] = useState<string | null>(null)
  const [pickError, setPickError] = useState('')
  const [percent, setPercent] = useState(0)
  const [located, setLocated] = useState(false) // page percentages are ready
  const [chapter, setChapter] = useState('')
  const [error, setError] = useState('')
  const [ready, setReady] = useState(false)
  const last = useRef<{ cfi: string; percent: number } | null>(null)
  const saveTimer = useRef<number | undefined>(undefined)
  // Nothing is saved until the book has opened where it was left: a book
  // drawn too early (a hidden window) reports the start, which must never
  // replace the real place.
  const armed = useRef(false)
  // Pages <-> Scroll opens the book anew, where the reading is now.
  const resumeAt = useRef<{ src: string; cfi: string } | null>(null)
  // The window changed size: the page that was being read. The window changing back
  // finds the same page, however many times the phone's bars come and go.
  const fitAnchor = useRef<string | null>(null)

  const save = useCallback(
    (keepalive = false) => {
      const at = last.current
      if (!at || !armed.current) return
      saveProgress(book.id, { format: 'ebook', position: at.cfi, percent: at.percent, finished: at.percent >= 99.5 }, keepalive)
    },
    [book.id],
  )

  // Turn a page: it curls over (curl/), or slides. With "none" it just turns. A
  // page asked for while one is still turning follows straight after it.
  const turn = useCallback(async (dir: 'next' | 'prev') => {
    const r = rendition.current
    if (!r) return
    fitAnchor.current = null
    if (turning.current) {
      queued.current = dir
      curl.current?.hurry()
      return
    }
    turning.current = true
    try {
      const effect = prefsRef.current.reading === 'scroll' ? 'none' : effectOf(prefsRef.current)
      const curled = effect === 'curl' && !!curl.current && (await curl.current.play(dir))
      if (!curled) await animateTurn(viewer.current, effect === 'none' ? 'none' : 'slide', prefsRef.current.speed, dir, () => (dir === 'next' ? r.next() : r.prev()))
    } finally {
      turning.current = false
      const then = queued.current
      queued.current = null
      if (then) void turnRef.current(then)
    }
  }, [])
  const turnRef = useRef(turn)
  turnRef.current = turn

  // The text was laid out anew by a setting: the same words stay on the screen. Settings
  // tapped quickly one after another all keep the place the first one started from.
  const keeping = useRef<{ cfi: string; run: number } | null>(null)
  const keepPlace = useCallback(async (newColumns: boolean) => {
    const r = rendition.current
    const cfi = keeping.current?.cfi ?? fitAnchor.current ?? last.current?.cfi
    if (!r || !cfi) return
    const run = (keeping.current?.run ?? 0) + 1
    keeping.current = { cfi, run }
    turning.current = true
    try {
      if (newColumns) {
        relayout(r)
        await r.display(cfi)
      }
      await settled(r)
      if (keeping.current?.run !== run) return
      await r.display(cfi)
      redrawMarks(r)
    } finally {
      if (keeping.current?.run === run) {
        keeping.current = null
        turning.current = false
        const then = queued.current
        queued.current = null
        if (then) void turnRef.current(then)
      }
    }
  }, [])
  // The side a page is on: the next one is on the left in a right-to-left book.
  const isRtl = () => (rendition.current as unknown as { manager?: { settings?: { direction?: string } } } | null)?.manager?.settings?.direction === 'rtl'
  const turnSide = (side: 'left' | 'right') => void turn((side === 'right') !== isRtl() ? 'next' : 'prev')
  // Going somewhere on purpose: that is the page being read now.
  const goTo = (where?: string) => {
    fitAnchor.current = null
    return rendition.current?.display(where as string)
  }

  // Open the book once.
  useEffect(() => {
    let cancelled = false
    let cleanup = () => {}
    let cleanupGestures = () => {}
    let stopWatching = () => {}
    let hereHref = ''
    let opened = false
    armed.current = false
    fitAnchor.current = null
    drawn.current.clear()
    setReady(false)
    const resume = resumeAt.current?.src === src ? resumeAt.current.cfi : undefined
    resumeAt.current = null
    void (async () => {
      try {
        const [{ default: ePub }, data, saved] = await Promise.all([
          import('epubjs'),
          resume && bookData.current
            ? bookData.current
            : fetch(src, { credentials: 'include' }).then(async (r) => {
                if (r.status === 422) throw new Error(((await r.json().catch(() => null)) as { error?: string } | null)?.error ?? t("This book couldn't be opened here."))
                if (!r.ok) throw new Error(r.status === 404 ? t("Couldn't find the book's file. It may have been moved or deleted.") : t("The book didn't load (error {status}).", { status: String(r.status) }))
                return r.arrayBuffer()
              }),
          api.bookProgress(book.id, 'ebook').catch(() => null),
        ])
        const list = await marksApi.list(`book:${book.id}`).catch(() => [] as ReadingMark[])
        if (cancelled || !viewer.current) return
        setMarks(list)
        await hasRoom(viewer.current)
        if (cancelled || !viewer.current) return
        bookData.current = data
        const b = ePub(data)
        epub.current = b
        const r = b.renderTo(viewer.current, renderOptions())
        rendition.current = r
        // Scrolling, a finger on the text moves the text.
        if (reading === 'pages') {
          cleanupGestures = attachTurnGestures({
            rendition: r,
            stage: stage.current!,
            rtl: isRtl,
            onTurn: (dir) => void turnRef.current(dir),
            beginDrag: (dir, x, y, at) => {
              if (effectOf(prefsRef.current) !== 'curl' || turning.current) return null
              const drag = curl.current?.drag(dir, x, y, at)
              if (!drag) return null
              fitAnchor.current = null
              turning.current = true
              return {
                move: drag.move,
                end: () =>
                  drag.end().finally(() => {
                    turning.current = false
                  }),
              }
            },
          })
        }
        for (const [name, th] of Object.entries(THEMES)) registerTheme(r, name, th)
        installSettings(r, () => prefsRef.current)
        applyPrefs(r, loadPrefs())
        const picked = (cfiRange: string, contents: { window: Window }) => {
          let text = ''
          try {
            text = (r.getRange(cfiRange) as unknown as Range).toString().trim()
          } catch {
            text = ''
          }
          if (!text) return
          setPickError('')
          setPickNote(null)
          setPick({ cfi: cfiRange, text: text.slice(0, 4000), clear: () => contents.window.getSelection()?.removeAllRanges() })
        }
        const watcher = watchPage({ rendition: r, onSelected: picked, onLink: (a, contents) => openNote(b, a, contents) })
        stopWatching = watcher.stop
        const tocRef = { current: [] as NavItem[] }
        r.on('relocated', (loc: { start: { cfi: string; percentage: number; href: string } }) => {
          const cfi = loc.start.cfi
          const pct = b.locations.length() > 0 ? b.locations.percentageFromCfi(cfi) * 100 : loc.start.percentage * 100
          setPercent(pct)
          last.current = { cfi, percent: Math.round(pct * 10) / 10 }
          hereHref = loc.start.href
          const item = findChapter(tocRef.current, hereHref)
          if (item) setChapter(item.label.trim())
          window.clearTimeout(saveTimer.current)
          if (armed.current && document.visibilityState === 'visible') saveTimer.current = window.setTimeout(() => save(), 1500)
          curl.current?.schedule()
        })
        // The window changed: new columns are made and the page is found again. Scrolled,
        // the place is wherever the reader has scrolled to, so there is no page to hold.
        r.on('resized', () => {
          curl.current?.schedule(300)
          if (!opened) return
          if (reading === 'pages') fitAnchor.current ??= last.current?.cfi ?? null
          void keepPlace(false)
        })
        r.on('keyup', onKey)
        // Words picked with the mouse or a finger can be highlighted.
        r.on('selected', (cfiRange: string, contents: { window: Window }) => {
          watcher.tell(cfiRange)
          picked(cfiRange, contents)
        })
        for (const m of list) {
          if (m.kind === 'highlight') {
            drawHighlight(r, m, () => setPanel('marks'))
            drawn.current.add(m.position)
          }
        }
        b.loaded.navigation.then((nav) => {
          tocRef.current = nav.toc
          setToc(nav.toc)
          const item = findChapter(nav.toc, hereHref)
          if (item) setChapter(item.label.trim())
        })
        // Usherr: a book read to the end opens at the start again.
        const kept = saved?.finished ? null : saved
        // Opened from a bookmark: there, not where the reading was left.
        const fromMark = !resume && at ? list.find((m) => m.id === at)?.position : undefined
        const start = resume ?? fromMark ?? (kept?.position && kept.position.startsWith('epubcfi') ? kept.position : undefined)
        // A place that came from an e-reader (a Kobo) is only a percentage, not an
        // EPUB location: the book opens at the start and jumps there once the page
        // numbers are worked out, unless the reader has already turned a page.
        const jumpTo = !start && kept && kept.percent > 0 && kept.percent < 100 ? kept.percent : 0
        await r.display(start)
        // The words were found where they were before the settings and the book's fonts
        // took hold, which can be a page earlier than where they end up.
        await settled(r)
        if (cancelled) return
        if (start) await r.display(start)
        const firstCfi = (r.currentLocation() as unknown as { start?: { cfi: string } } | undefined)?.start?.cfi
        if (cancelled) return
        opened = true
        setReady(true)
        window.setTimeout(() => {
          if (!cancelled) armed.current = true
        }, 1000)
        // Page percentages: worked out once per book and kept in this browser.
        const key = `mediarium.reader.loc.${book.id}`
        let cached: string | null = null
        try {
          cached = localStorage.getItem(key)
        } catch {
          cached = null
        }
        if (cached) {
          b.locations.load(cached)
        } else {
          await b.locations.generate(1600)
          try {
            localStorage.setItem(key, b.locations.save())
          } catch {
            // a full storage only means working it out again next time
          }
        }
        if (cancelled) return
        setLocated(true)
        if (jumpTo > 0) {
          const now = (r.currentLocation() as unknown as { start?: { cfi: string } } | undefined)?.start?.cfi
          const target = b.locations.cfiFromPercentage(jumpTo / 100)
          if (target && now === firstCfi) {
            // Not saved: it is still the place the e-reader gave, to the page.
            armed.current = false
            await r.display(target)
            window.setTimeout(() => {
              if (!cancelled) armed.current = true
            }, 1200)
          }
        }
        const cur = r.currentLocation() as unknown as { start?: { cfi: string } }
        if (cur?.start?.cfi) setPercent(b.locations.percentageFromCfi(cur.start.cfi) * 100)
      } catch (e) {
        if (!cancelled) setError(e instanceof Error ? e.message : String(e))
      }
    })()
    function onKey(e: KeyboardEvent) {
      // The arrows move the place slider and Space presses a button: those keys belong to the control.
      if ((e.target as HTMLElement | null)?.closest?.('input, select, textarea, button')) return
      if (e.key === 'ArrowRight') void turnRef.current(isRtl() ? 'prev' : 'next')
      if (e.key === 'ArrowLeft') void turnRef.current(isRtl() ? 'next' : 'prev')
      if (e.key === 'PageDown' || e.key === ' ') void turnRef.current('next')
      if (e.key === 'PageUp') void turnRef.current('prev')
    }
    document.addEventListener('keyup', onKey)
    const onHide = () => {
      if (document.visibilityState === 'hidden') save(true)
    }
    document.addEventListener('visibilitychange', onHide)
    window.addEventListener('pagehide', onHide)
    cleanup = () => {
      document.removeEventListener('keyup', onKey)
      document.removeEventListener('visibilitychange', onHide)
      window.removeEventListener('pagehide', onHide)
    }
    return () => {
      cancelled = true
      cleanup()
      cleanupGestures()
      stopWatching()
      window.clearTimeout(saveTimer.current)
      save(true)
      if (opened && last.current) resumeAt.current = { src, cfi: last.current.cfi }
      epub.current?.destroy()
      epub.current = null
      rendition.current = null
    }
  }, [book.id, save, src, at, reading, keepPlace])

  // Whatever changes the room for the page (the phone's bars, a rotation, the reader's own
  // bars), epub.js makes new columns; on its own it only looks when the window says so.
  useEffect(() => {
    const el = viewer.current
    if (!ready || !el) return
    let tm = 0
    const ro = new ResizeObserver(() => {
      window.clearTimeout(tm)
      tm = window.setTimeout(() => {
        if (el.offsetWidth > 0 && el.offsetHeight > 0) (rendition.current as unknown as { resize: () => void } | null)?.resize()
      }, 120)
    })
    ro.observe(el)
    return () => {
      ro.disconnect()
      window.clearTimeout(tm)
    }
  }, [ready])

  const laidOut = useRef({ layout: layoutOf(prefs), margin: prefs.margin })
  useEffect(() => {
    try {
      localStorage.setItem(PREFS_KEY, JSON.stringify(prefs))
    } catch {
      // the reader still works; the choice just isn't remembered
    }
    const r = rendition.current
    if (r) applyPrefs(r, prefs)
    const was = laidOut.current
    laidOut.current = { layout: layoutOf(prefs), margin: prefs.margin }
    // Words moved to other pages: the place follows, and the pictures after it.
    if (r && was.layout !== laidOut.current.layout) void keepPlace(was.margin !== prefs.margin)
    curl.current?.invalidate()
  }, [prefs, keepPlace])

  useEffect(() => {
    if (note === null) return
    const onEsc = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setNote(null)
    }
    document.addEventListener('keydown', onEsc)
    return () => document.removeEventListener('keydown', onEsc)
  }, [note])

  // A highlight added or removed is part of the pictures of the page.
  useEffect(() => {
    curl.current?.invalidate()
  }, [marks])

  // The curl's code and its canvas are there only while the curl is the choice.
  const curlOn = ready && reading === 'pages' && effectOf(prefs) === 'curl'
  useEffect(() => {
    if (!curlOn) return
    let gone = false
    // Not loaded (offline, say): the page slides.
    const loaded = Promise.all([import('./curl/epubCurl'), import('epubjs')]).catch(() => null)
    void loaded.then((mods) => {
      if (!mods) return
      const [{ EpubCurl }, { default: ePub }] = mods
      const r = rendition.current
      const data = bookData.current
      if (gone || !r || !stage.current || !viewer.current || !data) return
      if (!epub.current) return
      curl.current = EpubCurl.create({
        book: epub.current,
        rendition: r,
        stage: stage.current,
        viewer: viewer.current,
        theme: () => ({ bg: THEMES[prefsRef.current.theme].bg, dark: THEMES[prefsRef.current.theme].dark }),
        speed: () => prefsRef.current.speed,
        openCopy: (host) => {
          const copy = ePub(data)
          return { book: copy, rendition: copy.renderTo(host, renderOptions()) }
        },
        setUpCopy: (c) => {
          for (const [name, th] of Object.entries(THEMES)) registerTheme(c, name, th)
          installSettings(c, () => prefsRef.current)
          applyPrefs(c, prefsRef.current)
          for (const m of marksRef.current ?? []) if (m.kind === 'highlight') drawHighlight(c, m, () => undefined)
        },
        settled,
      })
    })
    return () => {
      gone = true
      curl.current?.destroy()
      curl.current = null
    }
  }, [curlOn])

  const theme = THEMES[prefs.theme]
  if (error)
    return (
      <ReaderMessage
        title={book.title}
        text={error}
        action={
          <a className="primary btn-with-icon" href={api.bookFileUrl(book.id, true)}>
            <Icon name="download" size={16} /> {t('Download')}
          </a>
        }
      />
    )
  return (
    <div className={`rdr rdr-epub theme-${prefs.theme}${reading === 'scroll' ? ' rdr-scroll' : ''}`} style={{ ['--rdr-bg' as string]: theme.bg, ['--rdr-fg' as string]: theme.fg }}>
      <header className="rdr-top">
        <Link className="icon-btn" to="/bookshelf" title={t('Back to your books')} aria-label={t('Back to your books')}>
          <Icon name="chevron-left" size={20} />
        </Link>
        <span className="rdr-title">
          <strong>{book.title}</strong>
          {/* Always there: a bar that grows when the chapter is known makes the page shorter, and moves every word after the place was found. */}
          <small>{chapter || '\u00a0'}</small>
        </span>
        <button className="icon-btn" onClick={() => void goTo()} disabled={!ready} title={t('Start over')} aria-label={t('Start over')}>
          <Icon name="skip-prev" size={18} />
        </button>
        <button className={`icon-btn${panel === 'toc' ? ' active' : ''}`} onClick={() => setPanel(panel === 'toc' ? '' : 'toc')} title={t('Contents')} aria-label={t('Contents')}>
          <Icon name="list" size={18} />
        </button>
        <button className={`icon-btn${panel === 'marks' ? ' active' : ''}`} onClick={() => setPanel(panel === 'marks' ? '' : 'marks')} disabled={!ready} title={t('Bookmarks')} aria-label={t('Bookmarks')}>
          <Icon name="bookmark" size={18} />
        </button>
        <button className={`icon-btn${panel === 'settings' ? ' active' : ''}`} onClick={() => setPanel(panel === 'settings' ? '' : 'settings')} title={t('Text and colours')} aria-label={t('Text and colours')}>
          <span className="rdr-aa">Aa</span>
        </button>
        <button
          className="icon-btn"
          onClick={() => (document.fullscreenElement ? void document.exitFullscreen() : void document.documentElement.requestFullscreen?.())}
          title={t('Full screen')}
          aria-label={t('Full screen')}
        >
          <Icon name="monitor" size={18} />
        </button>
      </header>

      {panel === 'toc' && (
        <nav className="rdr-panel rdr-toc" aria-label={t('Contents')}>
          {toc.length === 0 && <p className="hint">{t('This book has no table of contents.')}</p>}
          <TocList
            items={toc}
            onPick={(href) => {
              void goTo(href)
              setPanel('')
            }}
          />
        </nav>
      )}
      {panel === 'marks' && (
        <div className="rdr-panel" aria-label={t('Bookmarks')}>
          <Marks
            item={`book:${book.id}`}
            list={marks}
            here={last.current ? { position: last.current.cfi, label: chapter || t('{n}% in', { n: Math.round(percent) }), percent: last.current.percent } : null}
            onChange={(next) => {
              setMarks(next)
              // A highlight removed in the list leaves the page too.
              const keep = new Set(next.filter((m) => m.kind === 'highlight').map((m) => m.position))
              for (const cfi of [...drawn.current]) {
                if (!keep.has(cfi)) {
                  rendition.current?.annotations.remove(cfi, 'highlight')
                  drawn.current.delete(cfi)
                }
              }
            }}
            onJump={(m) => {
              void goTo(m.position)
              setPanel('')
            }}
          />
        </div>
      )}
      {pick && (
        <div className="rdr-pick" role="dialog" aria-label={t('Highlight')}>
          <span>{t('Highlight')}</span>
          {MARK_COLORS.map((c) => (
            <button key={c} className={`rdr-pick-dot ${c}`} onClick={() => void highlight(c)} aria-label={t('Highlight in {colour}', { colour: c })} title={t('Highlight')} />
          ))}
          {pickNote === null ? (
            <button className="btn-sm" onClick={() => setPickNote('')}>
              {t('Add a note')}
            </button>
          ) : (
            <>
              <textarea value={pickNote} onChange={(e) => setPickNote(e.target.value)} rows={2} maxLength={10000} aria-label={t('Note')} autoFocus />
              <button className="btn-sm primary" onClick={() => void highlight('yellow')}>
                {t('Save')}
              </button>
            </>
          )}
          <button className="btn-sm" onClick={() => closePick()}>
            {t('Cancel')}
          </button>
          {pickError && <p className="error-text" style={{ flexBasis: '100%', margin: 0 }}>{pickError}</p>}
        </div>
      )}
      {panel === 'settings' && (
        <div className="rdr-panel rdr-settings">
          <div className="rdr-row">
            <span>{t('Text size')}</span>
            <button className="btn-sm" onClick={() => setPrefs((p) => ({ ...p, size: Math.max(70, p.size - 10) }))} aria-label={t('Smaller text')}>
              A−
            </button>
            <span className="rdr-size">{prefs.size}%</span>
            <button className="btn-sm" onClick={() => setPrefs((p) => ({ ...p, size: Math.min(200, p.size + 10) }))} aria-label={t('Bigger text')}>
              A+
            </button>
          </div>
          <div className="rdr-row">
            <span>{t('Page')}</span>
            {(Object.keys(THEMES) as Theme[]).map((th) => (
              <button key={th} className={`rdr-swatch${prefs.theme === th ? ' active' : ''}`} style={{ background: THEMES[th].bg, color: THEMES[th].fg }} onClick={() => setPrefs((p) => ({ ...p, theme: th }))}>
                {th === 'light' ? t('Light') : th === 'sepia' ? t('Sepia') : th === 'dark' ? t('Dark') : t('Black')}
              </button>
            ))}
          </div>
          <div className="rdr-row">
            <span>{t('Reading')}</span>
            {(['pages', 'scroll'] as Reading[]).map((x) => (
              <button key={x} className={`btn-sm${reading === x ? ' primary' : ''}`} onClick={() => setPrefs((p) => ({ ...p, reading: x }))}>
                {x === 'pages' ? t('Pages') : t('Scroll')}
              </button>
            ))}
          </div>
          {reading === 'pages' && (
            <div className="rdr-row">
              <span>{t('Page turn')}</span>
              {(['curl', 'slide', 'none'] as Effect[]).map((x) => (
                <button key={x} className={`btn-sm${effectOf(prefs) === x ? ' primary' : ''}`} onClick={() => setPrefs((p) => ({ ...p, effect: x, turn: x === 'curl' ? (p.turn === 'none' ? 'slide' : p.turn) : x }))}>
                  {x === 'curl' ? t('Curl') : x === 'slide' ? t('Slide') : t('None')}
                </button>
              ))}
            </div>
          )}
          {reading === 'pages' && effectOf(prefs) !== 'none' && (
            <div className="rdr-row">
              <span>{t('Turn speed')}</span>
              {(['fast', 'normal', 'slow'] as TurnSpeed[]).map((x) => (
                <button key={x} className={`btn-sm${prefs.speed === x ? ' primary' : ''}`} onClick={() => setPrefs((p) => ({ ...p, speed: x }))}>
                  {x === 'fast' ? t('Fast') : x === 'normal' ? t('Normal') : t('Slow')}
                </button>
              ))}
            </div>
          )}
          <div className="rdr-row">
            <span>{t('Font')}</span>
            {FONT_CHOICES.map(([f, label]) => (
              <button key={f} className={`btn-sm${prefs.font === f ? ' primary' : ''}`} style={{ fontFamily: FONTS[f] || undefined }} onClick={() => setPrefs((p) => ({ ...p, font: f }))}>
                {t(label)}
              </button>
            ))}
          </div>
          <div className="rdr-row">
            <span>{t('Line spacing')}</span>
            {LINE_CHOICES.map(([x, label]) => (
              <button key={x} className={`btn-sm${prefs.line === x ? ' primary' : ''}`} onClick={() => setPrefs((p) => ({ ...p, line: x }))}>
                {t(label)}
              </button>
            ))}
          </div>
          <div className="rdr-row">
            <span>{t('Margins')}</span>
            {MARGIN_CHOICES.map(([x, label]) => (
              <button key={x} className={`btn-sm${prefs.margin === x ? ' primary' : ''}`} onClick={() => setPrefs((p) => ({ ...p, margin: x }))}>
                {t(label)}
              </button>
            ))}
          </div>
          <div className="rdr-row">
            <span>{t('Justify')}</span>
            {JUSTIFY_CHOICES.map(([x, label]) => (
              <button key={x} className={`btn-sm${prefs.justify === x ? ' primary' : ''}`} onClick={() => setPrefs((p) => ({ ...p, justify: x }))}>
                {t(label)}
              </button>
            ))}
          </div>
        </div>
      )}

      {note !== null && (
        <>
          <div className="rdr-note-back" onClick={() => setNote(null)} />
          <div className="rdr-note" role="dialog" aria-label={t('Footnote')}>
            <p>{note}</p>
            <button className="btn-sm" autoFocus onClick={() => setNote(null)}>
              {t('Close')}
            </button>
          </div>
        </>
      )}

      <main className="rdr-page" ref={stage}>
        <button className="rdr-turn prev" onClick={() => turnSide('left')} aria-label={t('Previous page')}>
          <Icon name="chevron-left" size={26} />
        </button>
        <div className="rdr-viewer" ref={viewer} style={ready ? undefined : { visibility: 'hidden' }}>
          <div className="rdr-grip left" />
          <div className="rdr-grip right" />
        </div>
        <button className="rdr-turn next" onClick={() => turnSide('right')} aria-label={t('Next page')}>
          <Icon name="chevron-right" size={26} />
        </button>
        {!ready && <div className="rdr-opening">{t('Opening the book…')}</div>}
      </main>

      <footer className="rdr-foot">
        <input
          type="range"
          min={0}
          max={100}
          step={0.1}
          value={percent}
          disabled={!located}
          title={located ? t('Go to a place in the book') : t('Working out the pages…')}
          aria-label={t('Place in the book')}
          onChange={(e) => setPercent(Number(e.target.value))}
          onMouseUp={(e) => jump(Number((e.target as HTMLInputElement).value))}
          onTouchEnd={(e) => jump(Number((e.target as HTMLInputElement).value))}
          onKeyUp={(e) => {
            // Only the keys that move the slider; Tab in and out must not jump.
            if (['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown', 'Home', 'End', 'PageUp', 'PageDown'].includes(e.key)) jump(Number((e.target as HTMLInputElement).value))
          }}
        />
        <span className="rdr-pct">{located ? `${Math.round(percent)}%` : '…'}</span>
      </footer>
    </div>
  )

  function closePick() {
    pick?.clear()
    setPick(null)
    setPickNote(null)
  }

  async function highlight(color: MarkColor) {
    const r = rendition.current
    if (!pick || !r) return
    try {
      const m = await marksApi.add(`book:${book.id}`, { kind: 'highlight', position: pick.cfi, excerpt: pick.text, note: pickNote ?? '', color, label: chapter, percent: last.current?.percent ?? 0 })
      setMarks((list) => [...(list ?? []), m].sort((a, b) => a.percent - b.percent || a.id - b.id))
      drawHighlight(r, m, () => setPanel('marks'))
      drawn.current.add(m.position)
      closePick()
    } catch (e) {
      setPickError(e instanceof Error ? e.message : String(e))
    }
  }

  // Both copies of the book share this. The gap is asked for each time the page is
  // measured, so a margin follows the window; epub.js copies the getter as it is.
  function renderOptions() {
    const scroll = prefsRef.current.reading === 'scroll'
    return {
      width: '100%',
      height: '100%',
      ...(scroll ? { manager: 'continuous', flow: 'scrolled', spread: 'none' } : { flow: 'paginated', spread: 'auto' }),
      allowScriptedContent: false,
      get gap() {
        return gapFor(viewer.current?.clientWidth ?? 0, prefsRef.current.margin)
      },
    } as unknown as Parameters<EpubBook['renderTo']>[1]
  }

  // A tap on a link: true if it was a footnote, now open in a card; the page hasn't moved.
  // In a frame that hears no events every link comes here, and any other is followed.
  function openNote(b: EpubBook, a: HTMLAnchorElement, contents: PageContents): boolean {
    const r = rendition.current
    const section = b.spine.get(contents.sectionIndex) as unknown as { href: string } | undefined
    if (!r || !section) return false
    const href = hrefOf(a)
    const place = resolveHref(href, section.href)
    const there = `${place.path}${place.id ? `#${place.id}` : ''}`
    let isNote = false
    try {
      isNote = isNoteLink(a)
    } catch {
      isNote = false
    }
    if (!isNote) {
      // epub.js's own handler follows it, where it can hear the tap.
      if (a.hasAttribute('data-rdr-href')) void goTo(there)
      return false
    }
    void (async () => {
      let target: Element | null = null
      try {
        if (place.path === section.href) target = contents.document.getElementById(place.id)
        else {
          const other = b.spine.get(place.path) as unknown as { load: (request: unknown) => Promise<unknown>; document?: Document } | undefined
          await other?.load(b.load.bind(b))
          target = other?.document?.getElementById(place.id) ?? null
        }
      } catch {
        target = null
      }
      const text = target ? noteText(target) : ''
      // Not readable as a note: as any other link.
      if (text) setNote(text)
      else void goTo(there)
    })()
    return true
  }

  function jump(p: number) {
    const b = epub.current
    if (!b || b.locations.length() === 0) return
    void goTo(b.locations.cfiFromPercentage(p / 100))
  }
}

// hasRoom waits until el has a size: a book laid out in a hidden window has
// no pages to turn.
function hasRoom(el: HTMLElement): Promise<void> {
  if (el.offsetWidth > 0 && el.offsetHeight > 0) return Promise.resolve()
  return new Promise((resolve) => {
    const ro = new ResizeObserver(() => {
      if (el.offsetWidth > 0 && el.offsetHeight > 0) {
        ro.disconnect()
        resolve()
      }
    })
    ro.observe(el)
  })
}

const FONT_CHOICES: [Font, string][] = [
  ['original', 'As published'],
  ['serif', 'Book'],
  ['classic', 'Classic'],
  ['sans', 'Plain'],
  ['clear', 'Clear'],
  ['mono', 'Typewriter'],
]
const LINE_CHOICES: [LineSpacing, string][] = [
  ['book', 'As published'],
  ['tight', 'Tight'],
  ['normal', 'Normal'],
  ['wide', 'Wide'],
]
const MARGIN_CHOICES: [Margin, string][] = [
  ['narrow', 'Narrow'],
  ['normal', 'Normal'],
  ['wide', 'Wide'],
]
const JUSTIFY_CHOICES: [Justify, string][] = [
  ['book', 'As published'],
  ['on', 'On'],
  ['off', 'Off'],
]

function registerTheme(r: Rendition, name: string, th: { bg: string; fg: string; link: string }) {
  r.themes.register(name, { body: { background: `${th.bg} !important`, color: `${th.fg} !important` }, a: { color: `${th.link} !important` } })
}

function applyPrefs(r: Rendition, p: Prefs) {
  r.themes.select(p.theme)
  r.themes.fontSize(`${p.size}%`)
  if (FONTS[p.font]) r.themes.font(FONTS[p.font])
  else (r.themes as unknown as { removeOverride: (name: string) => void }).removeOverride('font-family')
  applySettingsSheet(r, p)
}

function findChapter(items: NavItem[], href: string): NavItem | undefined {
  const base = href.split('#')[0]
  for (const it of items) {
    if (it.href.split('#')[0] === base) return it
    const sub = findChapter(it.subitems ?? [], href)
    if (sub) return sub
  }
  return undefined
}

function TocList({ items, onPick }: { items: NavItem[]; onPick: (href: string) => void }) {
  return (
    <ul>
      {items.map((it) => (
        <li key={it.id || it.href}>
          <button onClick={() => onPick(it.href)}>{it.label.trim()}</button>
          {it.subitems && it.subitems.length > 0 && <TocList items={it.subitems} onPick={onPick} />}
        </li>
      ))}
    </ul>
  )
}
