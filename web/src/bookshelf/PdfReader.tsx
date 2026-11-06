import { useCallback, useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import type { PDFDocumentProxy, RenderTask } from 'pdfjs-dist'
import { api, type Book } from '../api'
import Icon from '../components/Icon'
import { t } from '../i18n'
import Marks from './Marks'
import { marksApi } from './marksApi'
import { saveProgress } from './progress'

type Fit = 'page' | 'width'

// pdf.js is big, so it loads only when a PDF is opened. Its worker is part of
// Usherr's own build (no outside address).
async function loadPdfjs() {
  const [pdfjs, worker] = await Promise.all([import('pdfjs-dist'), import('pdfjs-dist/build/pdf.worker.min.mjs?url')])
  pdfjs.GlobalWorkerOptions.workerSrc = worker.default
  return pdfjs
}

// pageOf reads a saved place: "page:12". Older saves said only "pdf".
export function pageOf(position: string | undefined): number {
  const m = /^page:(\d+)$/.exec(position ?? '')
  return m ? Math.max(1, Number(m[1])) : 1
}

// The PDF reader: one page at a time, drawn by pdf.js, with the place kept
// like any other book.
export default function PdfReader({ book, at = 0 }: { book: Book; at?: number }) {
  const holder = useRef<HTMLDivElement>(null)
  const canvas = useRef<HTMLCanvasElement>(null)
  const doc = useRef<PDFDocumentProxy | null>(null)
  const task = useRef<RenderTask | null>(null)
  const [pages, setPages] = useState(0)
  const [page, setPage] = useState(1)
  const [fit, setFit] = useState<Fit>(() => (window.innerWidth < window.innerHeight ? 'width' : 'page'))
  const [error, setError] = useState('')
  const [panel, setPanel] = useState(false)
  const armed = useRef(false)
  const pageNow = useRef({ page: 1, pages: 0 })

  const save = useCallback(
    (keepalive = false) => {
      const { page: p, pages: n } = pageNow.current
      if (!armed.current || n === 0) return
      saveProgress(book.id, { format: 'ebook', position: `page:${p}`, percent: Math.round((p / n) * 1000) / 10, finished: p >= n }, keepalive)
    },
    [book.id],
  )

  // Open the file once.
  useEffect(() => {
    let cancelled = false
    void (async () => {
      try {
        const [pdfjs, data, saved] = await Promise.all([
          loadPdfjs(),
          fetch(api.bookFileUrl(book.id), { credentials: 'include' }).then((r) => {
            if (!r.ok) throw new Error(r.status === 404 ? t("Couldn't find the book's file. It may have been moved or deleted.") : t("The book didn't load (error {status}).", { status: String(r.status) }))
            return r.arrayBuffer()
          }),
          api.bookProgress(book.id, 'ebook').catch(() => null),
        ])
        // Opened from a bookmark on the book's page: that page.
        const mark = at ? (await marksApi.list(`book:${book.id}`).catch(() => [])).find((m) => m.id === at) : undefined
        if (cancelled) return
        const d = await pdfjs.getDocument({ data: new Uint8Array(data) }).promise
        if (cancelled) {
          void d.destroy()
          return
        }
        doc.current = d
        const start = mark ? Math.min(pageOf(mark.position), d.numPages) : saved && !saved.finished ? Math.min(pageOf(saved.position), d.numPages) : 1
        pageNow.current = { page: start, pages: d.numPages }
        setPages(d.numPages)
        setPage(start)
        window.setTimeout(() => {
          armed.current = true
        }, 800)
      } catch (e) {
        if (!cancelled) setError(e instanceof Error ? e.message : String(e))
      }
    })()
    const onHide = () => {
      if (document.visibilityState === 'hidden') save(true)
    }
    document.addEventListener('visibilitychange', onHide)
    window.addEventListener('pagehide', onHide)
    return () => {
      cancelled = true
      document.removeEventListener('visibilitychange', onHide)
      window.removeEventListener('pagehide', onHide)
      save(true)
      task.current?.cancel()
      void doc.current?.destroy()
      doc.current = null
    }
  }, [book.id, save, at])

  // Draw the page, sharp on high-density screens, again when the window changes size.
  const draw = useCallback(async () => {
    const d = doc.current
    const c = canvas.current
    const box = holder.current
    if (!d || !c || !box) return
    task.current?.cancel()
    const pg = await d.getPage(pageNow.current.page)
    const base = pg.getViewport({ scale: 1 })
    const room = { w: Math.max(box.clientWidth - 16, 100), h: Math.max(box.clientHeight - 16, 100) }
    const scale = fit === 'width' ? room.w / base.width : Math.min(room.w / base.width, room.h / base.height)
    const dpr = Math.min(window.devicePixelRatio || 1, 3)
    const vp = pg.getViewport({ scale: scale * dpr })
    c.width = Math.floor(vp.width)
    c.height = Math.floor(vp.height)
    c.style.width = `${Math.floor(vp.width / dpr)}px`
    c.style.height = `${Math.floor(vp.height / dpr)}px`
    const job = pg.render({ canvas: c, viewport: vp })
    task.current = job
    try {
      await job.promise
    } catch {
      // a newer drawing took over
    }
  }, [fit])

  useEffect(() => {
    if (!pages) return
    pageNow.current = { page, pages }
    void draw()
    holder.current?.scrollTo({ top: 0 })
    const tm = window.setTimeout(() => save(), 1500)
    return () => window.clearTimeout(tm)
  }, [page, pages, draw, save])

  useEffect(() => {
    const box = holder.current
    if (!box || !pages) return
    let tm = 0
    const ro = new ResizeObserver(() => {
      window.clearTimeout(tm)
      tm = window.setTimeout(() => void draw(), 150)
    })
    ro.observe(box)
    return () => {
      ro.disconnect()
      window.clearTimeout(tm)
    }
  }, [draw, pages])

  const go = useCallback((d: number) => setPage((p) => Math.min(Math.max(1, p + d), pageNow.current.pages || 1)), [])
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.target as HTMLElement | null)?.closest?.('input, select, textarea, button')) return
      if (e.key === 'ArrowRight' || e.key === 'PageDown') go(1)
      else if (e.key === 'ArrowLeft' || e.key === 'PageUp') go(-1)
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [go])

  if (error)
    return (
      <div className="bks-message">
        <Icon name="book" size={44} />
        <h1>{book.title}</h1>
        <p>{error}</p>
        <div className="row-actions">
          <a className="primary btn-with-icon" href={api.bookFileUrl(book.id, true)}>
            <Icon name="download" size={16} /> {t('Download')}
          </a>
          <Link className="btn-with-icon" to="/bookshelf">
            <Icon name="chevron-left" size={16} /> {t('Back to your books')}
          </Link>
        </div>
      </div>
    )

  return (
    <div className="rdr rdr-pdf">
      <header className="rdr-top">
        <Link className="icon-btn" to="/bookshelf" title={t('Back to your books')} aria-label={t('Back to your books')}>
          <Icon name="chevron-left" size={20} />
        </Link>
        <span className="rdr-title">
          <strong>{book.title}</strong>
          {pages > 0 && <small>{t('Page {n} of {count}', { n: page, count: pages })}</small>}
        </span>
        <button className="icon-btn" onClick={() => setFit((f) => (f === 'page' ? 'width' : 'page'))} disabled={!pages} title={fit === 'page' ? t('Fit width') : t('Fit page')} aria-label={fit === 'page' ? t('Fit width') : t('Fit page')}>
          <Icon name="monitor" size={18} />
        </button>
        <button className={`icon-btn${panel ? ' active' : ''}`} onClick={() => setPanel((p) => !p)} disabled={!pages} title={t('Bookmarks')} aria-label={t('Bookmarks')}>
          <Icon name="bookmark" size={18} />
        </button>
        <a className="icon-btn" href={api.bookFileUrl(book.id, true)} title={t('Download')} aria-label={t('Download')}>
          <Icon name="download" size={18} />
        </a>
      </header>
      {panel && (
        <div className="rdr-panel" aria-label={t('Bookmarks')}>
          <Marks
            item={`book:${book.id}`}
            here={{ position: `page:${page}`, label: t('Page {n}', { n: page }), percent: pages ? Math.round((page / pages) * 1000) / 10 : 0 }}
            onJump={(m) => {
              setPage(Math.min(pageOf(m.position), pages || 1))
              setPanel(false)
            }}
          />
        </div>
      )}
      <main className="rdr-page">
        <button className="rdr-turn prev" onClick={() => go(-1)} disabled={page <= 1} aria-label={t('Previous page')}>
          <Icon name="chevron-left" size={26} />
        </button>
        <div className={`rdr-pdf-view fit-${fit}`} ref={holder}>
          <canvas ref={canvas} />
          {!pages && <div className="rdr-opening">{t('Opening the book…')}</div>}
        </div>
        <button className="rdr-turn next" onClick={() => go(1)} disabled={page >= pages} aria-label={t('Next page')}>
          <Icon name="chevron-right" size={26} />
        </button>
      </main>
      <footer className="rdr-foot">
        <input type="range" min={1} max={Math.max(pages, 1)} value={page} disabled={!pages} aria-label={t('Page')} onChange={(e) => setPage(Number(e.target.value))} />
        <span className="rdr-pct">{pages ? `${page} / ${pages}` : '…'}</span>
      </footer>
    </div>
  )
}
