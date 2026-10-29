// Usherr: Read-to-Me's pronunciation list. A word, and how the voice should
// say it ("Hermione" → "her-MY-oh-nee"), for one book (bookId) or for every
// book (bookId 0). Saved as a whole.
import { useEffect, useState } from 'react'
import Icon from '../components/Icon'
import { t } from '../i18n'
import { readToMeApi, type RtvSay } from './readToMeApi'

export default function SayList({ bookId = 0 }: { bookId?: number }) {
  const [rows, setRows] = useState<RtvSay[] | null>(null)
  const [saved, setSaved] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    readToMeApi
      .say(bookId)
      .then((d) => {
        const list = (bookId ? d.book : d.all) ?? []
        setRows(list)
        setSaved(JSON.stringify(list))
      })
      .catch((e: Error) => setError(e.message))
  }, [bookId])
  if (!rows) return error ? <p className="error-text">{error}</p> : null
  const set = (i: number, patch: Partial<RtvSay>) => setRows(rows.map((r, j) => (j === i ? { ...r, ...patch } : r)))
  const changed = JSON.stringify(rows) !== saved
  const save = () => {
    setBusy(true)
    setError('')
    readToMeApi
      .saveSay(bookId, rows)
      .then((d) => {
        setRows(d.words)
        setSaved(JSON.stringify(d.words))
      })
      .catch((e: Error) => setError(e.message))
      .finally(() => setBusy(false))
  }
  return (
    <div className="rtm-say">
      {rows.length === 0 && <p className="hint">{bookId ? t('No words for this book yet.') : t('No words yet.')}</p>}
      {rows.map((r, i) => (
        <div key={i} className="rtm-say-row">
          <input value={r.word} onChange={(e) => set(i, { word: e.target.value })} placeholder={t('Word, as written')} aria-label={t('Word, as written')} maxLength={60} />
          <span aria-hidden="true">→</span>
          <input value={r.say} onChange={(e) => set(i, { say: e.target.value })} placeholder={t('How to say it')} aria-label={t('How to say it')} maxLength={120} />
          <button type="button" className="icon-btn" onClick={() => setRows(rows.filter((_, j) => j !== i))} aria-label={t('Remove {word}', { word: r.word })}>
            <Icon name="x" size={14} />
          </button>
        </div>
      ))}
      {error && <p className="error-text">{error}</p>}
      <div className="rtm-say-foot">
        <button type="button" className="btn-sm btn-with-icon" onClick={() => setRows([...rows, { word: '', say: '' }])}>
          <Icon name="plus" size={14} /> {t('Add a word')}
        </button>
        <button type="button" className="btn-sm primary" onClick={save} disabled={!changed || busy}>
          {busy ? t('Saving…') : t('Save')}
        </button>
      </div>
    </div>
  )
}
