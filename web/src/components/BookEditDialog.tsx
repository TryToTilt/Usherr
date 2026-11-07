import { createPortal } from 'react-dom'
import { useEffect, useRef, useState } from 'react'
import { api, type Book, type BookFileDetails, type BookFound } from '../api'
import { useFocusTrap } from '../useFocusTrap'
import { t } from '../i18n'
import { useConfirm } from './ConfirmProvider'
import Icon from './Icon'
import { useToast } from './Toast'

interface Fields {
  title: string
  author: string
  seriesName: string
  seriesPosition: string
  year: string
  description: string
}

const fieldsOf = (b: Book): Fields => ({
  title: b.title,
  author: b.author,
  seriesName: b.seriesName ?? '',
  seriesPosition: b.seriesPosition ?? '',
  year: b.year ? String(b.year) : '',
  description: b.description ?? '',
})

// What the book's file says, as the same fields ('' = it doesn't say).
const fileFieldsOf = (d: BookFileDetails): Fields => ({
  title: d.title ?? '',
  author: d.author ?? '',
  seriesName: d.seriesName ?? '',
  seriesPosition: d.seriesPosition ?? '',
  year: d.year ? String(d.year) : '',
  description: d.description ?? '',
})

const errorText = (e: unknown) => (e instanceof Error ? e.message : String(e))

// Correct a book that was matched wrongly: edit the details by hand, take them
// from the book's own file, or match it again against the search sources. The
// files stay where they are.
export default function BookEditDialog({ book, onClose, onChanged, onChangeCover }: { book: Book; onClose: () => void; onChanged: () => void; onChangeCover: () => void }) {
  const toast = useToast()
  const confirm = useConfirm()
  const box = useRef<HTMLDivElement>(null)
  useFocusTrap(box)
  const [fields, setFields] = useState<Fields>(() => fieldsOf(book))
  const [fromFile, setFromFile] = useState<Fields | null>(null)
  const [query, setQuery] = useState(`${book.title} ${book.author}`.trim())
  const [results, setResults] = useState<BookFound[] | null>(null)
  const [searching, setSearching] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && !busy && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [busy, onClose])

  useEffect(() => {
    api
      .bookFileDetails(book.id)
      .then((a) => setFromFile(a.found && a.details ? fileFieldsOf(a.details) : null))
      .catch(() => setFromFile(null))
  }, [book.id])

  const set = (k: keyof Fields, v: string) => setFields((f) => ({ ...f, [k]: v }))

  async function save() {
    const year = fields.year.trim() === '' ? 0 : Number(fields.year)
    if (!Number.isInteger(year) || year < 0) {
      setError(t('Enter the year as a number, such as 1937, or leave it empty.'))
      return
    }
    setBusy(true)
    setError('')
    try {
      await api.editBook(book.id, { ...fields, year })
      toast.success(t('Details saved.'))
      onChanged()
      onClose()
    } catch (e) {
      setError(errorText(e))
    } finally {
      setBusy(false)
    }
  }

  async function search() {
    if (query.trim().length < 2) return
    setSearching(true)
    setError('')
    try {
      setResults(await api.searchBooks(query.trim()))
    } catch (e) {
      setError(errorText(e))
    } finally {
      setSearching(false)
    }
  }

  async function rematch(f: BookFound) {
    const yes = await confirm({
      title: t('Make this book {title}?', { title: f.title }),
      body: <p>{t('Its title, author, year, description and cover are replaced with that one, and its series is looked up again. Your files and reading places stay as they are.')}</p>,
      confirmLabel: t('Match it'),
    })
    if (!yes) return
    setBusy(true)
    setError('')
    try {
      await api.rematchBook(book.id, f)
      toast.success(t('Matched again.'))
      onChanged()
      onClose()
    } catch (e) {
      setError(errorText(e))
    } finally {
      setBusy(false)
    }
  }

  const labels: Record<keyof Fields, string> = {
    title: t('Title'),
    author: t('Author'),
    seriesName: t('Series'),
    seriesPosition: t('Number in the series'),
    year: t('Year'),
    description: t('Description'),
  }
  const different = fromFile ? (Object.keys(labels) as (keyof Fields)[]).filter((k) => fromFile[k] && fromFile[k] !== fields[k]) : []

  return createPortal(
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && !busy && onClose()}>
      <div ref={box} tabIndex={-1} className="modal" role="dialog" aria-modal="true" aria-label={t('Edit details')}>
        <div className="modal-head">
          <div style={{ flex: 1, minWidth: 0 }}>
            <h2 style={{ margin: '0 0 4px' }}>{t('Edit details')}</h2>
            <p style={{ margin: 0, color: 'var(--text-dim)', fontSize: '0.88rem' }}>{t('Only the details change. The files stay where they are, under the names they have.')}</p>
          </div>
          <button className="icon-btn" onClick={onClose} disabled={busy} aria-label={t('Close')}>
            <Icon name="x" />
          </button>
        </div>

        <div className="modal-body grid-form">
          <label>
            {labels.title}
            <input value={fields.title} onChange={(e) => set('title', e.target.value)} disabled={busy} />
          </label>
          <label>
            {labels.author}
            <input value={fields.author} onChange={(e) => set('author', e.target.value)} disabled={busy} />
          </label>
          <label>
            {labels.seriesName}
            <input value={fields.seriesName} onChange={(e) => set('seriesName', e.target.value)} disabled={busy} />
          </label>
          <label>
            {labels.seriesPosition}
            <input value={fields.seriesPosition} inputMode="decimal" onChange={(e) => set('seriesPosition', e.target.value)} disabled={busy} />
          </label>
          <label>
            {labels.year}
            <input value={fields.year} inputMode="numeric" onChange={(e) => set('year', e.target.value)} disabled={busy} />
          </label>
          <label>
            {labels.description}
            <textarea rows={5} style={{ fontFamily: 'inherit' }} value={fields.description} onChange={(e) => set('description', e.target.value)} disabled={busy} />
          </label>
          <div>
            <button className="btn-with-icon" onClick={onChangeCover} disabled={busy}>
              <Icon name="image" size={16} /> {t('Change cover')}
            </button>
          </div>

          {different.length > 0 && fromFile && (
            <div>
              <h3 style={{ margin: '8px 0 4px', fontSize: '1rem' }}>{t("What the book's file says")}</h3>
              {different.map((k) => (
                <p key={k} style={{ display: 'flex', gap: 8, alignItems: 'baseline', margin: '4px 0' }}>
                  <span style={{ flex: 1, minWidth: 0, overflowWrap: 'anywhere' }}>
                    <strong>{labels[k]}:</strong> {fromFile[k].length > 160 ? `${fromFile[k].slice(0, 160)}…` : fromFile[k]}
                  </span>
                  <button disabled={busy} onClick={() => set(k, fromFile[k])}>
                    {t('Use this')}
                  </button>
                </p>
              ))}
            </div>
          )}

          <div>
            <h3 style={{ margin: '8px 0 4px', fontSize: '1rem' }}>{t('Match it again')}</h3>
            <p className="field-hint" style={{ marginTop: 0 }}>
              {t('Wrong book altogether? Search Open Library, Hardcover and Google Books, and pick the right one.')}
            </p>
            <div style={{ display: 'flex', gap: 8 }}>
              <input style={{ flex: 1, minWidth: 0 }} value={query} aria-label={t('Search for the book')} onChange={(e) => setQuery(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && void search()} disabled={busy} />
              <button className="btn-with-icon" onClick={() => void search()} disabled={busy || searching}>
                <Icon name="search" size={16} /> {searching ? t('Searching…') : t('Search')}
              </button>
            </div>
            {results && results.length === 0 && <p className="hint">{t('Nothing found. Try fewer words.')}</p>}
            {results && results.length > 0 && (
              <ul style={{ listStyle: 'none', margin: '8px 0 0', padding: 0, maxHeight: 240, overflowY: 'auto' }}>
                {results.map((f) => (
                  <li key={f.key} style={{ display: 'flex', gap: 8, alignItems: 'center', padding: '4px 0' }}>
                    {f.coverUrl ? <img src={f.coverUrl} alt="" width={32} height={48} style={{ objectFit: 'cover', borderRadius: 3 }} /> : <span style={{ width: 32 }} />}
                    <span style={{ flex: 1, minWidth: 0, overflowWrap: 'anywhere' }}>
                      {[f.title, f.author, f.year].filter(Boolean).join(' · ')}
                      {f.libraryId && f.libraryId !== book.id ? <span className="hint"> {t('(already in your library)')}</span> : null}
                    </span>
                    <button disabled={busy || (!!f.libraryId && f.libraryId !== book.id) || f.key === book.olKey} onClick={() => void rematch(f)}>
                      {t('Pick')}
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </div>

          {error && <p className="error-text">{error}</p>}
        </div>

        <div className="modal-foot">
          <button onClick={onClose} disabled={busy}>
            {t('Cancel')}
          </button>
          <button className="primary" onClick={() => void save()} disabled={busy}>
            {t('Save')}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  )
}
