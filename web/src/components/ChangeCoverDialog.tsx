import { createPortal } from 'react-dom'
import { useEffect, useRef, useState } from 'react'
import { api, type Book } from '../api'
import { withBase } from '../base'
import { coverFileProblem, coverLinkProblem, needsConverting } from '../bookCover'
import Icon from './Icon'
import { useToast } from './Toast'
import { useFocusTrap } from '../useFocusTrap'
import { t } from '../i18n'

type Way = 'file' | 'link' | 'editions'

interface Edition {
  coverId: number
  url: string
}

// A WebP picture is turned into a JPEG here, since the server reads JPEG and PNG.
async function webpToJpeg(file: File): Promise<Blob> {
  const bitmap = await createImageBitmap(file)
  const canvas = document.createElement('canvas')
  canvas.width = bitmap.width
  canvas.height = bitmap.height
  const ctx = canvas.getContext('2d')
  if (!ctx) throw new Error(t("This browser couldn't read that picture. Save it as a JPEG or PNG and try again."))
  ctx.fillStyle = '#fff'
  ctx.fillRect(0, 0, canvas.width, canvas.height)
  ctx.drawImage(bitmap, 0, 0)
  bitmap.close()
  const blob = await new Promise<Blob | null>((done) => canvas.toBlob(done, 'image/jpeg', 0.92))
  if (!blob) throw new Error(t("This browser couldn't read that picture. Save it as a JPEG or PNG and try again."))
  return blob
}

// Change a book's cover: upload a picture, paste a link to one, or pick the
// cover of another edition when Open Library has several. "Remove my cover"
// takes the choice away and brings back the automatic cover.
export default function ChangeCoverDialog({ book, onClose, onChanged }: { book: Book; onClose: () => void; onChanged: () => void }) {
  const toast = useToast()
  const box = useRef<HTMLDivElement>(null)
  useFocusTrap(box)
  const [way, setWay] = useState<Way>('file')
  const [file, setFile] = useState<File | null>(null)
  const [preview, setPreview] = useState('')
  const [link, setLink] = useState('')
  const [editions, setEditions] = useState<Edition[] | null>(null)
  const [current, setCurrent] = useState(0)
  const [editionError, setEditionError] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && !busy && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [busy, onClose])

  // A small preview of the chosen file, given back when another is chosen or the dialog closes.
  const previewUrl = useRef('')
  useEffect(
    () => () => {
      if (previewUrl.current) URL.revokeObjectURL(previewUrl.current)
    },
    [],
  )
  function pickFile(f: File | null) {
    if (previewUrl.current) URL.revokeObjectURL(previewUrl.current)
    previewUrl.current = f ? URL.createObjectURL(f) : ''
    setFile(f)
    setPreview(previewUrl.current)
    setError('')
  }

  // The other editions' covers are asked for the first time that tab is opened.
  useEffect(() => {
    if (way !== 'editions' || editions !== null) return
    let live = true
    api
      .bookCoverEditions(book.id)
      .then((r) => {
        if (!live) return
        setEditions(r.editions)
        setCurrent(r.current)
      })
      .catch((e) => {
        if (!live) return
        setEditions([])
        setEditionError(e instanceof Error ? e.message : String(e))
      })
    return () => {
      live = false
    }
  }, [way, editions, book.id])

  function done(message: string) {
    toast.success(message)
    onChanged()
    onClose()
  }

  async function run(work: () => Promise<unknown>, message: string) {
    setBusy(true)
    setError('')
    try {
      await work()
      done(message)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
      setBusy(false)
    }
  }

  function save() {
    if (way === 'file') {
      const why = coverFileProblem(file)
      if (why || !file) {
        setError(why)
        return
      }
      void run(async () => {
        if (needsConverting(file)) await api.uploadBookCover(book.id, await webpToJpeg(file), file.name.replace(/\.[^.]*$/, '') + '.jpg')
        else await api.uploadBookCover(book.id, file, file.name)
      }, t('Cover changed.'))
    } else if (way === 'link') {
      const why = coverLinkProblem(link)
      if (why) {
        setError(why)
        return
      }
      void run(() => api.setBookCoverLink(book.id, link.trim()), t('Cover changed.'))
    }
  }

  const choose = (w: Way) => {
    setWay(w)
    setError('')
  }

  return createPortal(
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && !busy && onClose()}>
      <div ref={box} tabIndex={-1} className="modal change-cover" role="dialog" aria-modal="true" aria-label={t('Change cover')}>
        <div className="modal-head">
          <div style={{ flex: 1, minWidth: 0 }}>
            <h2 style={{ margin: '0 0 4px' }}>{t('Change cover')}</h2>
            <p style={{ margin: 0, color: 'var(--text-dim)', fontSize: '0.88rem' }}>
              {t('Give {title} a cover of your own. It stays until you remove it.', { title: book.title })}
            </p>
          </div>
          <button className="icon-btn" onClick={onClose} disabled={busy} aria-label={t('Close')}>
            <Icon name="x" />
          </button>
        </div>

        <div className="modal-body grid-form">
          <div className="seg" role="radiogroup" aria-label={t('Where the cover comes from')}>
            <button role="radio" aria-checked={way === 'file'} className={way === 'file' ? 'active' : ''} onClick={() => choose('file')} disabled={busy}>
              {t('Upload a picture')}
            </button>
            <button role="radio" aria-checked={way === 'link'} className={way === 'link' ? 'active' : ''} onClick={() => choose('link')} disabled={busy}>
              {t('Paste a link')}
            </button>
            <button role="radio" aria-checked={way === 'editions'} className={way === 'editions' ? 'active' : ''} onClick={() => choose('editions')} disabled={busy}>
              {t('Other editions')}
            </button>
          </div>

          {way === 'file' && (
            <>
              <label>
                {t('JPEG, PNG or WebP picture, up to 8 MB')}
                <input type="file" accept="image/jpeg,image/png,image/webp" onChange={(e) => pickFile(e.target.files?.[0] ?? null)} disabled={busy} />
              </label>
              {preview && (
                <div className="change-cover-preview">
                  <img src={preview} alt={t('The picture you chose')} />
                </div>
              )}
            </>
          )}

          {way === 'link' && (
            <label>
              {t('Link to a picture')}
              <input type="url" value={link} onChange={(e) => { setLink(e.target.value); setError('') }} placeholder={t('https://…')} disabled={busy} spellCheck={false} autoComplete="off" onKeyDown={(e) => e.key === 'Enter' && save()} />
              <span className="field-hint">{t('Mediarium downloads the picture once and keeps its own copy. Links to places on your own network are not allowed.')}</span>
            </label>
          )}

          {way === 'editions' && (
            <>
              {editions === null && <div className="skeleton" style={{ height: 120 }} />}
              {editions !== null && editionError && <p className="error-text">{editionError}</p>}
              {editions !== null && !editionError && editions.length === 0 && <p className="field-hint">{t('Open Library has no covers for this book. Upload a picture or paste a link instead.')}</p>}
              {editions !== null && editions.length > 0 && (
                <>
                  <p className="field-hint">{t('Open Library has these covers for the book. Choose the one you want.')}</p>
                  <div className="change-cover-editions">
                    {editions.map((ed, i) => (
                      <button
                        key={ed.coverId}
                        className={`change-cover-pick${ed.coverId === current ? ' current' : ''}`}
                        onClick={() => void run(() => api.pickBookCover(book.id, ed.coverId), t('Cover changed.'))}
                        disabled={busy}
                        aria-label={t('Use cover {n} of {total}', { n: i + 1, total: editions.length })}
                        aria-pressed={ed.coverId === current}
                      >
                        <img src={withBase(ed.url)} alt="" loading="lazy" />
                      </button>
                    ))}
                  </div>
                </>
              )}
            </>
          )}
          {error && <p className="error-text">{error}</p>}
        </div>

        <div className="modal-foot">
          {book.coverChosen && (
            <button className="btn-with-icon danger-ghost" onClick={() => void run(() => api.removeBookCover(book.id), t('Cover removed.'))} disabled={busy} style={{ marginRight: 'auto' }}>
              <Icon name="trash" size={16} /> {t('Remove my cover')}
            </button>
          )}
          <button onClick={onClose} disabled={busy}>
            {t('Cancel')}
          </button>
          {way !== 'editions' && (
            <button className="primary btn-with-icon" onClick={save} disabled={busy}>
              <Icon name="image" size={16} /> {busy ? t('Saving…') : t('Use this picture')}
            </button>
          )}
        </div>
      </div>
    </div>,
    document.body,
  )
}
