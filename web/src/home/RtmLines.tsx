// Usherr: a made audiobook's lines (on the Read-to-Me tab and in the Read to
// me dialog). The lines that may be read badly first; any chapter's lines to
// hear one by one; a line read again (another take, or in other words) or the
// whole chapter, without making the rest of the book again. What it was made
// of is kept for 30 days; it can be let go of sooner to free the space.
import { useEffect, useRef, useState } from 'react'
import { useConfirm } from '../components/ConfirmProvider'
import Icon from '../components/Icon'
import { t } from '../i18n'
import { readToMeApi, type RtvLine, type RtvMade, type RtvMadeLines } from './readToMeApi'

const mb = (n: number) => t('{n} MB', { n: Math.max(1, Math.round(n / 1_000_000)) })

export default function RtmLines({ made, busy, onQueued }: { made: RtvMade; busy: boolean; onQueued: () => void }) {
  const confirm = useConfirm()
  const audio = useRef<HTMLAudioElement>(null)
  const [data, setData] = useState<RtvMadeLines | null>(null)
  const [error, setError] = useState('')
  const [wording, setWording] = useState<{ chapter: number; line: number; text: string } | null>(null)
  const load = (chapter?: number) =>
    readToMeApi
      .madeLines(made.id, chapter)
      .then(setData)
      .catch((e: Error) => setError(e.message))
  useEffect(() => {
    void load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [made.id, made.doneMs])

  const hear = (chapter: number, l: RtvLine) => {
    const a = audio.current
    if (!a) return
    a.src = readToMeApi.lineUrl(made.id, chapter, l.index)
    void a.play().catch(() => setError(t("That line couldn't be played.")))
  }
  const redo = (chapter: number, line: number, text?: string) => {
    setError('')
    readToMeApi
      .redo(made.id, { chapter, line, text })
      .then(() => {
        setWording(null)
        onQueued()
      })
      .catch((e: Error) => setError(e.message))
  }
  const free = async () => {
    const ok = await confirm({
      title: t('Let go of what this audiobook was made of?'),
      body: <p>{t('The audiobook stays as it is, but its lines can no longer be read again one by one (a remake still can). It frees {size}.', { size: mb(made.keptBytes ?? 0) })}</p>,
      confirmLabel: t('Let go of it'),
    })
    if (!ok) return
    readToMeApi
      .freeMakings(made.id)
      .then(onQueued)
      .catch((e: Error) => setError(e.message))
  }

  if (!data) return error ? <p className="error-text">{error}</p> : <p className="hint">{t('Getting the lines…')}</p>
  if (!data.kept) return <p className="hint">{t('What this audiobook was made of isn’t kept any more (it is kept for 30 days), so its lines can’t be read again one by one. Remake it instead.')}</p>
  const can = made.canChange && !busy
  const row = (chapter: number, l: RtvLine) => {
    const editing = wording && wording.chapter === chapter && wording.line === l.index
    return (
      <li key={`${chapter}:${l.index}`} className={l.flag ? 'rtm-line unknown' : 'rtm-line'}>
        <span className="rtm-row">
          <button type="button" className="icon-btn" onClick={() => hear(chapter, l)} aria-label={t('Hear this line')} title={t('Hear this line')}>
            <Icon name="play" size={14} />
          </button>
          <q>{l.text}</q>
        </span>
        {l.flag && <small className="error-text">{t('May be read badly: {why}', { why: l.flag })}</small>}
        {can && !editing && (
          <span className="rtm-made-actions">
            <button type="button" className="btn-sm btn-with-icon" onClick={() => redo(chapter, l.index)}>
              <Icon name="refresh" size={14} /> {t('Another take')}
            </button>
            <button type="button" className="btn-sm" onClick={() => setWording({ chapter, line: l.index, text: l.text })}>
              {t('Other words')}
            </button>
          </span>
        )}
        {editing && (
          <span className="rtm-say-foot">
            <input value={wording.text} onChange={(e) => setWording({ ...wording, text: e.target.value })} aria-label={t('The line in other words')} maxLength={2000} />
            <button type="button" className="btn-sm primary" disabled={!wording.text.trim()} onClick={() => redo(chapter, l.index, wording.text)}>
              {t('Read it like this')}
            </button>
            <button type="button" className="btn-sm" onClick={() => setWording(null)}>
              {t('Cancel')}
            </button>
          </span>
        )}
      </li>
    )
  }
  return (
    <div className="rtm-lines">
      {error && <p className="error-text">{error}</p>}
      {busy && <p className="hint">{t('It is being changed; the lines can be read again when that is done.')}</p>}
      {data.flagged.length > 0 && (
        <>
          <strong>{t('Lines to check ({n})', { n: data.flagged.length })}</strong>
          <small className="hint">{t('Chatterbox read these again and they still don’t fit their words. Hear each one; read it again if it is wrong.')}</small>
          <ul className="rtm-list">{data.flagged.map((l) => row(l.chapter, l))}</ul>
        </>
      )}
      <div className="rtm-say-foot">
        <select value={data.chapter} onChange={(e) => void load(Number(e.target.value))} aria-label={t('Chapter')}>
          {data.chapters.map((c, i) => (
            <option key={i} value={i}>
              {c.title || t('Part {n}', { n: i + 1 })} · {t('{n} lines', { n: c.lines })}
              {c.flagged ? ` · ${t('{n} to check', { n: c.flagged })}` : ''}
            </option>
          ))}
        </select>
        {can && (
          <button type="button" className="btn-sm btn-with-icon" onClick={() => redo(data.chapter, -1)}>
            <Icon name="refresh" size={14} /> {t('Read the chapter again')}
          </button>
        )}
      </div>
      <ul className="rtm-list">{data.lines.map((l) => row(data.chapter, l))}</ul>
      <audio ref={audio} controls className="rtm-audio" />
      <small className="hint">
        {t('What it was made of is kept for 30 days ({size}).', { size: mb(made.keptBytes ?? 0) })}{' '}
        {made.canChange && (
          <button type="button" className="btn-sm" onClick={() => void free()} disabled={busy}>
            {t('Let go of it now')}
          </button>
        )}
      </small>
    </div>
  )
}
