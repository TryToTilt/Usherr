// Usherr: a chapter's text as Read-to-Me reads it, put right before it is read
// (a word the ebook has wrong, a footnote, a line that shouldn't be read). The
// ebook itself is never changed.
import { useEffect, useState } from 'react'
import { t } from '../i18n'
import { readToMeApi } from './readToMeApi'

export default function RtmTextEdit({ bookId, chapter, onDone }: { bookId: number; chapter: number; onDone: (edited: boolean | null) => void }) {
  const [text, setText] = useState<string | null>(null)
  const [edited, setEdited] = useState(false)
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)
  useEffect(() => {
    readToMeApi
      .text(bookId, chapter)
      .then((d) => {
        setText(d.text)
        setEdited(d.edited)
      })
      .catch((e: Error) => setError(e.message))
  }, [bookId, chapter])
  const run = (p: Promise<{ edited: boolean }>) => {
    setSaving(true)
    setError('')
    p.then((d) => onDone(d.edited))
      .catch((e: Error) => setError(e.message))
      .finally(() => setSaving(false))
  }
  if (text === null) return error ? <p className="error-text">{error}</p> : <p className="hint">{t('Getting the text…')}</p>
  return (
    <div className="rtm-edit">
      <small className="hint">{t('Change the words to be read; a blank line starts a new paragraph, and *** a scene break. The ebook isn’t changed.')}</small>
      <textarea value={text} onChange={(e) => setText(e.target.value)} rows={12} aria-label={t('The chapter’s text')} />
      {error && <p className="error-text">{error}</p>}
      <div className="rtm-say-foot">
        <button type="button" className="btn-sm primary" disabled={saving || !text.trim()} onClick={() => run(readToMeApi.saveText(bookId, chapter, text))}>
          {t('Save')}
        </button>
        {edited && (
          <button type="button" className="btn-sm" disabled={saving} onClick={() => run(readToMeApi.resetText(bookId, chapter))}>
            {t('Back to the ebook’s text')}
          </button>
        )}
        <button type="button" className="btn-sm" onClick={() => onDone(null)}>
          {t('Cancel')}
        </button>
      </div>
    </div>
  )
}
