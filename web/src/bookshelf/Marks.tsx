import { useEffect, useState } from 'react'
import Icon from '../components/Icon'
import { t } from '../i18n'
import { marksApi, type ReadingMark } from './marksApi'
import './marks.css'

export interface MarkHere {
  position: string
  label: string
  percent: number
}

// Marks: the bookmarks and highlights in one book or comic, newest place
// last, each one a button that goes back there. With here, the place being
// read now can be bookmarked (or the bookmark there removed). Notes are
// written and changed in place.
export default function Marks({
  item,
  here,
  onJump,
  onChange,
  list: given,
}: {
  item: string
  here?: MarkHere | null
  onJump: (m: ReadingMark) => void
  onChange?: (list: ReadingMark[]) => void
  list?: ReadingMark[]
}) {
  const [list, setList] = useState<ReadingMark[] | null>(given ?? null)
  const [error, setError] = useState('')
  const [editing, setEditing] = useState<number | null>(null)
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (given) {
      setList(given)
      return
    }
    marksApi
      .list(item)
      .then(setList)
      .catch((e: Error) => setError(e.message))
  }, [item, given])

  const changed = (next: ReadingMark[]) => {
    setList(next)
    onChange?.(next)
  }
  const atHere = here ? list?.find((m) => m.kind === 'bookmark' && m.position === here.position) : undefined

  async function toggleHere() {
    if (!here || !list) return
    setBusy(true)
    setError('')
    try {
      if (atHere) {
        await marksApi.remove(atHere.id)
        changed(list.filter((m) => m.id !== atHere.id))
      } else {
        const m = await marksApi.add(item, { kind: 'bookmark', position: here.position, label: here.label, percent: here.percent })
        changed([...list.filter((x) => x.id !== m.id), m].sort((a, b) => a.percent - b.percent || a.id - b.id))
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  async function saveNote(m: ReadingMark) {
    setBusy(true)
    try {
      const saved = await marksApi.update(m.id, { note })
      changed((list ?? []).map((x) => (x.id === saved.id ? saved : x)))
      setEditing(null)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  async function remove(m: ReadingMark) {
    setBusy(true)
    try {
      await marksApi.remove(m.id)
      changed((list ?? []).filter((x) => x.id !== m.id))
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="mk">
      {here && (
        <button className={`btn-with-icon${atHere ? '' : ' primary'}`} disabled={busy || !list} onClick={() => void toggleHere()}>
          <Icon name="bookmark" size={16} /> {atHere ? t('Remove this bookmark') : t('Bookmark this page')}
        </button>
      )}
      {error && <p className="error-text">{error}</p>}
      {list === null && !error && <p className="hint">{t('Loading…')}</p>}
      {list && list.length === 0 && <p className="hint">{t('No bookmarks yet.')}</p>}
      {list && list.length > 0 && (
        <ul className="mk-list">
          {list.map((m) => (
            <li key={m.id} className={`mk-item kind-${m.kind}${m.color ? ` mk-${m.color}` : ''}`}>
              <button className="mk-go" onClick={() => onJump(m)} title={t('Go there')}>
                <Icon name={m.kind === 'highlight' ? 'edit' : 'bookmark'} size={14} />
                <span className="mk-text">
                  {m.excerpt ? <q>{m.excerpt}</q> : <strong>{m.label || t('Bookmark')}</strong>}
                  <small>{[m.excerpt ? m.label : '', m.percent > 0 ? `${Math.round(m.percent)}%` : ''].filter(Boolean).join(' · ')}</small>
                </span>
              </button>
              {editing === m.id ? (
                <div className="mk-note-edit">
                  <textarea value={note} onChange={(e) => setNote(e.target.value)} rows={3} maxLength={10000} aria-label={t('Note')} autoFocus />
                  <div className="row-actions">
                    <button className="btn-sm primary" disabled={busy} onClick={() => void saveNote(m)}>
                      {t('Save')}
                    </button>
                    <button className="btn-sm" onClick={() => setEditing(null)}>
                      {t('Cancel')}
                    </button>
                  </div>
                </div>
              ) : (
                m.note && <p className="mk-note">{m.note}</p>
              )}
              {editing !== m.id && (
                <div className="mk-tools">
                  <button
                    className="btn-sm"
                    onClick={() => {
                      setNote(m.note)
                      setEditing(m.id)
                    }}
                  >
                    {m.note ? t('Edit note') : t('Add a note')}
                  </button>
                  <button className="btn-sm" disabled={busy} onClick={() => void remove(m)} aria-label={t('Remove')} title={t('Remove')}>
                    <Icon name="trash" size={13} />
                  </button>
                </div>
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
