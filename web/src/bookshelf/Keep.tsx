import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, type Book } from '../api'
import Icon from '../components/Icon'
import { useConfirm } from '../components/ConfirmProvider'
import { withBase } from '../base'
import { t } from '../i18n'
import { keepBook, keptBooks, keptId, offlineSupport, sizeText, storageUsed, unkeep, type Kept, type KeptKind } from './offline'
import './keep.css'

// "Keep on this device": a button on each book, and the list of what is kept
// with the room it takes.

const KINDLE = new Set(['mobi', 'azw', 'azw3', 'prc'])
const COMIC = new Set(['cbz', 'cbr', 'cb7'])

// ebookKind is what kind of ebook can be kept, or '' for one that can't be
// read in Usherr.
export function ebookKind(format: string | undefined): KeptKind | '' {
  const f = (format ?? '').toLowerCase()
  if (f === 'epub') return 'epub'
  if (f === 'pdf') return 'pdf'
  if (KINDLE.has(f)) return 'kindle'
  if (COMIC.has(f)) return 'comic'
  return ''
}

// warmFor loads the parts of Usherr a kind of book needs to open, so they are
// stored with it.
export function warmFor(kind: KeptKind): () => Promise<unknown> {
  switch (kind) {
    case 'epub':
    case 'kindle':
      return () => Promise.all([import('./Reader'), import('epubjs')])
    case 'pdf':
      return () => Promise.all([import('./Reader'), import('pdfjs-dist'), import('pdfjs-dist/build/pdf.worker.min.mjs?url').then((m) => fetch(m.default))])
    case 'comic':
      return () => import('./Reader')
    case 'audiobook':
      return () => import('./Player')
    case 'issue':
      return () => Promise.all([import('./ComicIssue'), import('../home/ComicFlip')])
    case 'volume':
      return () => Promise.all([import('./MangaVolume'), import('./curl/imageCurl')])
  }
}

// useKept is the list of kept books, and a way to read it again.
export function useKept(): [Kept[], () => void] {
  const [list, setList] = useState<Kept[]>([])
  const load = useCallback(() => {
    void keptBooks().then(setList)
  }, [])
  useEffect(() => {
    load()
    window.addEventListener('usherr-books-kept', load)
    return () => window.removeEventListener('usherr-books-kept', load)
  }, [load])
  return [list, load]
}

function changed() {
  window.dispatchEvent(new Event('usherr-books-kept'))
}

// KeepButton keeps one book (or comic issue) on this device, or removes it.
export function KeepButton({
  kind,
  id,
  title,
  subtitle,
  cover,
  open,
  kept,
  count,
  compact,
}: {
  kind: KeptKind
  id: number
  title: string
  subtitle?: string
  cover?: string
  open: string
  kept: boolean
  count?: () => Promise<number> // pages or tracks, asked for when keeping
  compact?: boolean
}) {
  const [busy, setBusy] = useState<string>('')
  const [error, setError] = useState('')
  const support = offlineSupport()
  if (!support.ok) return null

  async function keep() {
    setError('')
    setBusy(t('Getting ready…'))
    try {
      const n = count ? await count() : 0
      await keepBook({ kind, id, title, subtitle, cover, open, count: n }, warmFor(kind), (done, total) => setBusy(t('Saving {done} of {total}', { done, total })))
      changed()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy('')
    }
  }

  async function remove() {
    setBusy(t('Removing…'))
    try {
      await unkeep(keptId(kind, id))
      changed()
    } finally {
      setBusy('')
    }
  }

  const label = kept ? t('Remove from this device') : t('Keep on this device')
  return (
    <>
      <button
        className={`btn-sm btn-with-icon keep-btn${kept ? ' kept' : ''}`}
        disabled={!!busy}
        title={kept ? t('Kept on this device: it opens with no connection. Click to remove it.') : t('Save it on this device to read with no connection')}
        aria-label={label}
        onClick={() => void (kept ? remove() : keep())}
      >
        <Icon name={kept ? 'check' : 'download'} size={14} />
        {busy ? busy : compact ? (kept ? t('On device') : t('Keep')) : label}
      </button>
      {error && <small className="error-text keep-error">{error}</small>}
    </>
  )
}

// bookCount asks how many pages a Books library comic has, or tracks an audiobook.
export function bookCount(b: Book, kind: KeptKind): (() => Promise<number>) | undefined {
  if (kind === 'audiobook') return () => api.bookTracks(b.id).then((r) => r.tracks.length)
  if (kind === 'comic')
    return () =>
      fetch(withBase(`/api/books/${b.id}/comic`), { credentials: 'include' })
        .then((r) => (r.ok ? (r.json() as Promise<{ pages: number }>) : Promise.reject(new Error(t("This comic couldn't be opened.")))))
        .then((r) => r.pages)
  return undefined
}

// OnDevice lists what is kept on this device, the room it takes, and a way to
// remove each one or all of them.
export function OnDevice({ kept }: { kept: Kept[] }) {
  const confirm = useConfirm()
  const [room, setRoom] = useState<{ usage: number; quota: number } | null>(null)
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    void storageUsed().then(setRoom)
  }, [kept])
  if (kept.length === 0) return null
  const total = kept.reduce((n, k) => n + k.bytes, 0)

  async function removeAll() {
    if (!(await confirm({ title: t('Remove every book from this device?'), body: <p>{t('They stay in Usherr. You need a connection to read them again.')}</p>, confirmLabel: t('Remove them') }))) return
    setBusy(true)
    try {
      for (const k of kept) await unkeep(k.id)
    } finally {
      setBusy(false)
      changed()
    }
  }

  return (
    <section className="bks-section bks-device">
      <h2>
        <Icon name="download" size={18} /> {t('On this device')}
      </h2>
      <p className="hint">
        {t('{n} kept · {size}', { n: kept.length, size: sizeText(total) })}
        {room && room.quota > 0 ? ` · ${t('{free} free for this site', { free: sizeText(Math.max(0, room.quota - room.usage)) })}` : ''}
      </p>
      <ul className="bks-device-list">
        {[...kept]
          .sort((a, b) => a.title.localeCompare(b.title))
          .map((k) => (
            <li key={k.id}>
              <Link to={k.open} className="bks-device-title">
                <Icon name={k.kind === 'audiobook' ? 'headphones' : 'book'} size={14} />
                <span>
                  <strong>{k.title}</strong>
                  {k.subtitle && <small> · {k.subtitle}</small>}
                </span>
              </Link>
              <small className="hint">{sizeText(k.bytes)}</small>
              <button
                className="btn-sm"
                disabled={busy}
                onClick={() => {
                  setBusy(true)
                  void unkeep(k.id).finally(() => {
                    setBusy(false)
                    changed()
                  })
                }}
              >
                {t('Remove from this device')}
              </button>
            </li>
          ))}
      </ul>
      <button className="btn-sm" disabled={busy} onClick={() => void removeAll()}>
        <Icon name="trash" size={14} /> {t('Remove all')}
      </button>
    </section>
  )
}

// useOnline follows whether this device has a connection.
export function useOnline(): boolean {
  const [on, setOn] = useState(() => navigator.onLine)
  useEffect(() => {
    const up = () => setOn(true)
    const downNow = () => setOn(false)
    window.addEventListener('online', up)
    window.addEventListener('offline', downNow)
    return () => {
      window.removeEventListener('online', up)
      window.removeEventListener('offline', downNow)
    }
  }, [])
  return on
}
