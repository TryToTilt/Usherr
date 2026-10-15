// Home fork: the comics review list (after Pullbox's intervention queue).
// Search results that may be the issue, but not surely enough to grab by
// themselves, wait here: Get downloads one, Not this one blocklists it.
import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import Icon from '../components/Icon'
import { comicsApi, type ComicReview } from './comicsApi'
import { t, tx } from '../i18n'

function size(b?: number) {
  if (!b) return ''
  return b >= 1e9 ? `${(b / 1e9).toFixed(1)} GB` : `${Math.round(b / 1e6)} MB`
}

export default function ComicReviewList({ onCount }: { onCount?: (n: number) => void }) {
  const [list, setList] = useState<ComicReview[] | null>(null)
  const [error, setError] = useState('')
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(0)
  const load = useCallback(() => {
    comicsApi
      .reviews()
      .then((l) => {
        setList(l)
        onCount?.(new Set(l.map((r) => r.issueId)).size)
      })
      .catch((e: Error) => setError(e.message))
  }, [onCount])
  useEffect(load, [load])
  const act = (r: ComicReview, accept: boolean) => {
    setBusy(r.id)
    setError('')
    ;(accept ? comicsApi.acceptReview(r.id) : comicsApi.rejectReview(r.id))
      .then((res) => {
        setNote(res.message)
        load()
      })
      .catch((e: Error) => setError(e.message))
      .finally(() => setBusy(0))
  }
  if (!list) return error ? <p className="error-text">{error}</p> : <p className="hint">{t('Loading…')}</p>
  // One group per issue, best release first.
  const groups = new Map<number, ComicReview[]>()
  for (const r of list) groups.set(r.issueId, [...(groups.get(r.issueId) ?? []), r])
  return (
    <div className="cx-review">
      <p className="hint">
        {t("When a search finds releases it isn't sure about (a year that doesn't fit, a size that looks wrong) or a pack, collected edition (TPB) or annual that may hold the issue, it puts them here instead of guessing.")}{' '}
        {tx("{get} downloads one; {not} blocklists it so it isn't offered again.", { get: <strong>{t('Get')}</strong>, not: <strong>{t('Not this one')}</strong> })}
      </p>
      {note && <p className="notice notice-info cx-note">{note}</p>}
      {error && <p className="error-text">{error}</p>}
      {groups.size === 0 && <p className="hint">{t('Nothing to review.')}</p>}
      {[...groups.values()].map((rs) => (
        <section key={rs[0].issueId} className="card cx-review-issue">
          <h3>
            <Link to={`/comics/${rs[0].seriesId}`}>{rs[0].series}</Link> #{rs[0].number}
            {rs[0].issueTitle && <span className="hint"> · {rs[0].issueTitle}</span>}
            {rs[0].coverDate && <span className="hint"> · {rs[0].coverDate.slice(0, 7)}</span>}
          </h3>
          <ul className="cx-rel">
            {rs.map((r) => (
              <li key={r.id}>
                <div>
                  <span className="cx-rel-title">{r.title}</span>
                  <span className="hint">
                    {r.indexer} · {size(r.sizeBytes)} {r.notes?.length ? `· ${r.notes.join(' · ')}` : ''}
                  </span>
                </div>
                <div className="cx-review-actions">
                  <button className="btn-sm primary" disabled={busy === r.id} onClick={() => act(r, true)}>
                    <Icon name="download" size={14} /> {t('Get')}
                  </button>
                  <button className="btn-sm" disabled={busy === r.id} onClick={() => act(r, false)}>
                    {t('Not this one')}
                  </button>
                </div>
              </li>
            ))}
          </ul>
        </section>
      ))}
    </div>
  )
}
