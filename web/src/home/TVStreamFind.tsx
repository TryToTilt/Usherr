// Usherr: finding a show to stream on a channel (it plays through StreamNZB),
// wherever a channel is built: the line-up, a part of the day, a set time.
import { useEffect, useState } from 'react'
import { api, type TitleResult } from '../api'
import Icon from '../components/Icon'
import { t } from '../i18n'

export interface StreamPick {
  tmdbId: number
  title: string
  year?: number
}

// useStreamSearch looks shows up on TMDB as q is typed: in the library or not,
// any show can be streamed.
export function useStreamSearch(q: string): TitleResult[] {
  const [found, setFound] = useState<TitleResult[]>([])
  useEffect(() => {
    const s = q.trim()
    if (s.length < 2) {
      setFound([])
      return
    }
    const ac = new AbortController()
    const tm = window.setTimeout(() => {
      api
        .discoverSearch(s, ac.signal)
        .then((list) => setFound(list.filter((r) => r.kind === 'tv').slice(0, 6)))
        .catch(() => undefined)
    }, 350)
    return () => {
      window.clearTimeout(tm)
      ac.abort()
    }
  }, [q])
  return found
}

// StreamFind is a search box with the shows found, each one to pick.
export function StreamFind({ picked = [], onPick, placeholder, autoFocus }: { picked?: StreamPick[]; onPick: (s: StreamPick) => void; placeholder?: string; autoFocus?: boolean }) {
  const [q, setQ] = useState('')
  const found = useStreamSearch(q).filter((r) => !picked.some((p) => p.tmdbId === r.tmdbId))
  return (
    <div className="tv-streamfind">
      <input type="search" value={q} onChange={(e) => setQ(e.target.value)} placeholder={placeholder ?? t('Find a show to stream')} autoFocus={autoFocus} />
      {found.length > 0 && (
        <div className="tv-chips">
          {found.map((r) => (
            <button
              key={r.tmdbId}
              type="button"
              className="btn-sm"
              title={t('Plays through StreamNZB')}
              onClick={() => {
                onPick({ tmdbId: r.tmdbId, title: r.title, year: r.year || undefined })
                setQ('')
              }}
            >
              <Icon name="plus" size={12} /> {r.title} {r.year ? `(${r.year})` : ''} <span className="tv-tag">{t('stream')}</span>
            </button>
          ))}
        </div>
      )}
      {q.trim().length >= 2 && found.length === 0 && <small className="hint">{t('Nothing found to stream yet.')}</small>}
    </div>
  )
}

// StreamChips are the streamed shows picked, each with a button to drop it.
export function StreamChips({ list, onDrop }: { list: StreamPick[]; onDrop: (s: StreamPick) => void }) {
  if (list.length === 0) return null
  return (
    <div className="tv-chips">
      {list.map((s) => (
        <button key={s.tmdbId} type="button" className="btn-sm" onClick={() => onDrop(s)} title={t('Stop streaming this show here')}>
          {s.title} <span className="tv-tag">{t('stream')}</span> <Icon name="x" size={12} />
        </button>
      ))}
    </div>
  )
}
