// Home fork: Rate 20. A quick deck of titles to thumb up or down, one at a
// time, so recommendations learn your taste fast. Half come from what the
// house has, half are well-known titles; nothing you've voted on yet.
// Keys: right arrow or L likes, left arrow or H passes on it, space skips.
import { useEffect, useRef, useState } from 'react'
import type { DiscoverMovie } from '../api'
import Icon from '../components/Icon'
import { t } from '../i18n'
import { setVote } from './useVotes'
import './news.css'

export default function RateDeck({ onClose }: { onClose: () => void }) {
  const [kind, setKind] = useState<'movie' | 'tv'>('movie')
  const [items, setItems] = useState<DiscoverMovie[] | null>(null)
  const [at, setAt] = useState(0)
  const [counts, setCounts] = useState({ up: 0, down: 0 })
  const [error, setError] = useState('')
  const [round, setRound] = useState(0) // "Another 20" fetches a new deck
  const box = useRef<HTMLDivElement>(null)

  useEffect(() => {
    setItems(null)
    setAt(0)
    setError('')
    fetch(`/api/home/votes/deck?kind=${kind}`, { credentials: 'include' })
      .then(async (r) => {
        const d = await r.json().catch(() => ({}))
        if (!r.ok) throw new Error((d as { error?: string }).error || r.statusText)
        return (d as { items: DiscoverMovie[] }).items ?? []
      })
      .then(setItems)
      .catch((e: Error) => setError(e.message))
  }, [kind, round])
  useEffect(() => box.current?.focus(), [])

  const cur = items?.[at]
  const answer = (vote: 1 | -1 | 0) => {
    if (!cur) return
    if (vote) {
      setVote(kind, cur.tmdbId, vote, {
        title: cur.title,
        year: cur.year,
        image: cur.posterUrl,
      })
      setCounts((c) => (vote === 1 ? { ...c, up: c.up + 1 } : { ...c, down: c.down + 1 }))
    }
    setAt((n) => n + 1)
  }
  const onKey = (e: React.KeyboardEvent) => {
    if (e.key === 'Escape') onClose()
    else if (e.key === 'ArrowRight' || e.key === 'l') answer(1)
    else if (e.key === 'ArrowLeft' || e.key === 'h') answer(-1)
    else if (e.key === ' ') {
      e.preventDefault()
      answer(0)
    }
  }

  const chip = (on: boolean) => `room-chip${on ? ' on' : ''}`
  const done = items !== null && !cur
  return (
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()} onKeyDown={onKey}>
      <div ref={box} tabIndex={-1} className="modal roulette" role="dialog" aria-modal="true" aria-label={t('Rate 20')}>
        <div className="roulette-head">
          <h2>
            <Icon name="thumb-up" size={18} /> {t('Rate 20')}
          </h2>
          <button className="icon-btn" onClick={onClose} aria-label={t('Close')}>
            <Icon name="x" size={16} />
          </button>
        </div>
        <div className="room-row" role="group" aria-label={t('Kind')}>
          <button type="button" className={chip(kind === 'movie')} aria-pressed={kind === 'movie'} onClick={() => setKind('movie')}>
            <Icon name="film" size={14} /> {t('Films')}
          </button>
          <button type="button" className={chip(kind === 'tv')} aria-pressed={kind === 'tv'} onClick={() => setKind('tv')}>
            <Icon name="tv" size={14} /> {t('Shows')}
          </button>
          <span className="hint rate-progress">
            {items && items.length > 0
              ? t('{n} of {total}', {
                  n: Math.min(at + 1, items.length),
                  total: items.length,
                })
              : ''}
          </span>
        </div>
        <p className="hint">{t('Seen it? Say if you liked it. Not seen it? Say if it looks like your kind of thing, or skip.')}</p>

        <div className="roulette-stage" aria-live="polite">
          {error ? (
            <p className="error-text">{error}</p>
          ) : items === null ? (
            <p className="hint">{t('Loading…')}</p>
          ) : done ? (
            <div className="roulette-empty">
              <Icon name="check" size={40} />
              <p>
                {t('Done: {up} liked, {down} not for you.', {
                  up: counts.up,
                  down: counts.down,
                })}
              </p>
              <p className="hint">{t('Recommendations use your votes from now on.')}</p>
            </div>
          ) : (
            cur && (
              <div className="rate-card" key={cur.tmdbId}>
                {cur.posterUrl ? (
                  <img className="roulette-face" src={cur.posterUrl} alt="" />
                ) : (
                  <div className="roulette-face rate-noposter">
                    <Icon name={kind === 'tv' ? 'tv' : 'film'} size={40} />
                  </div>
                )}
                <strong>{cur.title}</strong>
                {cur.year ? <span className="hint">{cur.year}</span> : null}
              </div>
            )
          )}
        </div>
        <div className="roulette-nav rate-nav">
          {done ? (
            <>
              <button
                className="btn-with-icon"
                onClick={() => {
                  setCounts({ up: 0, down: 0 })
                  setRound((n) => n + 1)
                }}
              >
                <Icon name="refresh" size={16} /> {t('Another 20')}
              </button>
              <button className="primary" onClick={onClose}>
                {t('Close')}
              </button>
            </>
          ) : (
            <>
              <button className="btn-with-icon" onClick={() => answer(-1)} disabled={!cur} title={t('Not for me')}>
                <Icon name="thumb-down" size={16} /> {t('Not for me')}
              </button>
              <button onClick={() => answer(0)} disabled={!cur}>
                {t('Skip')}
              </button>
              <button className="primary btn-with-icon" onClick={() => answer(1)} disabled={!cur} title={t('I like this')}>
                <Icon name="thumb-up" size={16} /> {t('Like')}
              </button>
            </>
          )}
        </div>
      </div>
    </div>
  )
}
