// Usherr: fixing a series matched to the wrong ComicVine volume: search again,
// see what choosing another volume would do, then choose it. Or say what kind of
// series it is. No file moves: issues keep their files by number, and the Rename
// tool in Comics settings renames them when you run it.
import { useState, type FormEvent } from 'react'
import Icon from '../components/Icon'
import { plural, t } from '../i18n'
import { comicsApi, type ComicMatchPreview, type ComicSeries, type ComicSeriesType } from './comicsApi'

export default function ComicFixMatch({ series, onClose, onChanged }: { series: ComicSeries; onClose: () => void; onChanged: (note: string) => void }) {
  const [q, setQ] = useState(series.name)
  const [found, setFound] = useState<ComicSeries[] | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [choice, setChoice] = useState<{ cvId: number; preview: ComicMatchPreview } | null>(null)
  const [type, setType] = useState<ComicSeriesType>(series.type ?? '')

  // ComicVine counts each of these against the hourly limit, so a search goes
  // only when asked for.
  const work = <T,>(p: Promise<T>, then: (v: T) => void) => {
    setBusy(true)
    setError('')
    p.then(then)
      .catch((e: Error) => setError(e.message))
      .finally(() => setBusy(false))
  }
  const search = (e: FormEvent) => {
    e.preventDefault()
    const term = q.trim()
    if (term.length < 2) return
    setChoice(null)
    work(comicsApi.lookup(term), setFound)
  }
  const saveType = (v: ComicSeriesType) => {
    setType(v)
    work(comicsApi.update(series.id, { type: v }), () => onChanged(t('Saved what kind of series it is.')))
  }
  const choose = () => {
    if (!choice) return
    work(comicsApi.match(series.id, choice.cvId), (r) => {
      onChanged(r.found > 0 ? t('Now matched to {name}. {n} more files in its folder were recognised.', { name: r.series.name, n: r.found }) : t('Now matched to {name}.', { name: r.series.name }))
      onClose()
    })
  }

  return (
    <div className="cx-modal" role="dialog" aria-label={t('Fix match for {name}', { name: series.name })} onClick={onClose}>
      <div className="cx-modal-box card" onClick={(e) => e.stopPropagation()}>
        <header>
          <h2>{t('Fix match')}</h2>
          <button className="btn-sm" onClick={onClose} aria-label={t('Close')}>
            <Icon name="x" size={14} />
          </button>
        </header>
        <label className="cx-field">
          <span>{t('What kind of series it is')}</span>
          <select value={type} disabled={busy} onChange={(e) => saveType(e.target.value as ComicSeriesType)}>
            <option value="">{t('Not set (guess from the publisher)')}</option>
            <option value="comic">{t('Comic')}</option>
            <option value="manga">{t('Manga (reads right to left, volumes count as issues)')}</option>
            <option value="webtoon">{t('Webtoon (opens as one long scroll)')}</option>
            <option value="collected">{t('Collected editions (each volume counts as an issue)')}</option>
          </select>
        </label>
        <p className="hint">
          {t('Matched to ComicVine volume {id}: {name}.', { id: String(series.cvId), name: `${series.name}${series.startYear ? ` (${series.startYear})` : ''}` })} {t('If that’s the wrong one, search for the right one. Your files stay where they are.')}
        </p>
        <form className="cx-own-search" onSubmit={search}>
          <input value={q} onChange={(e) => setQ(e.target.value)} aria-label={t('Search ComicVine')} />
          <button className="btn-sm" type="submit" disabled={busy || q.trim().length < 2}>
            <Icon name="search" size={14} /> {t('Search')}
          </button>
        </form>
        {busy && <p className="hint">{t('Asking ComicVine…')}</p>}
        {error && <p className="error-text">{error}</p>}
        {choice ? (
          <div className="cx-match-preview">
            <h3>
              {choice.preview.series.name} {choice.preview.series.startYear ? `(${choice.preview.series.startYear})` : ''}
            </h3>
            <ul>
              <li>{plural(choice.preview.files, '{n} of your files stays with the issue of the same number.', '{n} of your files stay with the issue of the same number.')}</li>
              {choice.preview.noMatch.length > 0 && <li>{t('Not on that volume, kept as they are: #{numbers}', { numbers: choice.preview.noMatch.join(', #') })}</li>}
              {choice.preview.added > 0 && <li>{plural(choice.preview.added, '{n} new issue is added.', '{n} new issues are added.')}</li>}
              {choice.preview.removed > 0 && <li>{plural(choice.preview.removed, '{n} issue you have no file for goes away.', '{n} issues you have no file for go away.')}</li>}
              <li>{t('What people have read, their places and bookmarks stay with the issues.')}</li>
            </ul>
            <div className="cx-actions">
              <button className="btn-sm primary" disabled={busy} onClick={choose}>
                {t('Use this volume')}
              </button>
              <button className="btn-sm" disabled={busy} onClick={() => setChoice(null)}>
                {t('Back')}
              </button>
            </div>
          </div>
        ) : (
          <>
            {found && found.length === 0 && !busy && <p className="hint">{t('ComicVine has nothing by that name.')}</p>}
            <ul className="cx-results">
              {(found ?? []).map((s) => (
                <li key={s.cvId}>
                  {s.imageUrl ? <img src={s.imageUrl} alt="" loading="lazy" /> : <div className="cx-noimg small"><Icon name="book" size={20} /></div>}
                  <div className="cx-result-main">
                    <strong>
                      {s.name} {s.startYear ? `(${s.startYear})` : ''}
                    </strong>
                    <span className="hint">
                      {s.publisher} · {plural(s.issueCount, '{n} issue', '{n} issues')}
                    </span>
                    {s.description && <p className="cx-desc">{s.description}</p>}
                  </div>
                  {s.cvId === series.cvId ? (
                    <span className="hint">{t('This one now')}</span>
                  ) : s.inLibrary ? (
                    <span className="hint">{t('Already in your library')}</span>
                  ) : (
                    <button className="btn-sm" disabled={busy} onClick={() => work(comicsApi.matchPreview(series.id, s.cvId), (preview) => setChoice({ cvId: s.cvId, preview }))}>
                      {t('Choose')}
                    </button>
                  )}
                </li>
              ))}
            </ul>
          </>
        )}
      </div>
    </div>
  )
}
