// Home fork: Catch Me Up. A spoiler-free recap of a show up to where you
// are: the last episode you finished in Jellyfin (or Emby), or one you
// choose. Written by the AI from TMDB's episode summaries. Shown only when
// AI is set up.
import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import Icon from '../components/Icon'
import { currentLanguage, t } from '../i18n'
import { useFocusTrap } from '../useFocusTrap'
import { ScrollText } from './aiShared'

interface Recap {
  show: string
  season: number
  episode: number
  episodeName: string
  from: '' | 'watched' | 'chosen'
  recap: string
  seasons: { season: number; episodes: number }[]
}

const code = (s: number, e: number) => `S${String(s).padStart(2, '0')}E${String(e).padStart(2, '0')}`

export default function CatchUp({ tmdbId, autoOpen, season, episode }: { tmdbId: number; autoOpen?: boolean; season?: number; episode?: number }) {
  const [on, setOn] = useState(false)
  const [open, setOpen] = useState(!!autoOpen)
  useEffect(() => {
    fetch('/api/home/ai/on', { credentials: 'include' })
      .then((r) => (r.ok ? r.json() : { on: false }))
      .then((d: { on: boolean }) => setOn(d.on))
      .catch(() => undefined)
  }, [])
  if (!on || !tmdbId) return null
  return (
    <>
      <button className="btn-with-icon" onClick={() => setOpen(true)} title={t('A spoiler-free recap up to where you are')}>
        <Icon name="sparkles" size={16} /> {t('Catch me up')}
      </button>
      {open && <CatchUpDialog tmdbId={tmdbId} season={season} episode={episode} onClose={() => setOpen(false)} />}
    </>
  )
}

function CatchUpDialog({ tmdbId, season: s0, episode: e0, onClose }: { tmdbId: number; season?: number; episode?: number; onClose: () => void }) {
  const [data, setData] = useState<Recap | null>(null)
  const [busy, setBusy] = useState(true)
  const [error, setError] = useState('')
  const [season, setSeason] = useState(0)
  const [episode, setEpisode] = useState(0)
  const box = useRef<HTMLDivElement>(null)
  useFocusTrap(box)
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  const load = async (s: number, e: number) => {
    setBusy(true)
    setError('')
    try {
      const r = await fetch('/api/home/ai/recap', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ tmdbId, season: s, episode: e, lang: currentLanguage() }),
      })
      const d = await r.json().catch(() => null)
      if (!r.ok) throw new Error(d?.error ?? t('Something went wrong (error {status}).', { status: r.status }))
      const rc = d as Recap
      setData(rc)
      const first = rc.seasons[0]
      setSeason(rc.season || first?.season || 1)
      setEpisode(rc.episode || 1)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  useEffect(() => {
    void load(s0 && e0 ? s0 : 0, s0 && e0 ? e0 : 0)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tmdbId])

  const episodes = data?.seasons.find((s) => s.season === season)?.episodes ?? 0
  const where = data && data.season > 0 ? code(data.season, data.episode) + (data.episodeName ? ` · ${data.episodeName}` : '') : ''

  return createPortal(
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div ref={box} tabIndex={-1} className="modal recap-dialog" role="dialog" aria-modal="true" aria-label={t('Catch me up')}>
        <div className="recap-head">
          <Icon name="sparkles" size={20} />
          <div>
            <h2>{data?.show ? t('Catch me up: {title}', { title: data.show }) : t('Catch me up')}</h2>
            {where && <p>{data?.from === 'watched' ? t('Up to {episode}, the last one you watched', { episode: where }) : t('Up to {episode}', { episode: where })}</p>}
          </div>
          <button className="icon-btn" onClick={onClose} aria-label={t('Close')}>
            <Icon name="x" size={18} />
          </button>
        </div>
        {busy && <p className="hint">{t('Writing your recap…')}</p>}
        {error && <p className="error-text">{error}</p>}
        {!busy && data && !data.recap && !error && <p className="hint">{t('Choose the last episode you watched, and the recap will stop there.')}</p>}
        {!busy && data?.recap && <ScrollText className="recap-text tall">{data.recap}</ScrollText>}
        {data && data.seasons.length > 0 && (
          <div className="recap-pick">
            <label>
              {t('Season')}{' '}
              <select
                value={season}
                onChange={(e) => {
                  setSeason(Number(e.target.value))
                  setEpisode(1)
                }}
              >
                {data.seasons.map((s) => (
                  <option key={s.season} value={s.season}>
                    {s.season}
                  </option>
                ))}
              </select>
            </label>
            <label>
              {t('Episode')}{' '}
              <select value={episode} onChange={(e) => setEpisode(Number(e.target.value))}>
                {Array.from({ length: episodes }, (_, i) => i + 1).map((n) => (
                  <option key={n} value={n}>
                    {n}
                  </option>
                ))}
              </select>
            </label>
            <button className="primary btn-sm" disabled={busy || (season === data.season && episode === data.episode)} onClick={() => void load(season, episode)}>
              {t('Recap up to here')}
            </button>
          </div>
        )}
        <p className="hint">{t('Written by AI from TMDB’s episode summaries. It stops at your episode.')}</p>
      </div>
    </div>,
    document.body,
  )
}

// Catch me up for a comic series: up to the last issue read to the end in
// this browser, or one you choose. From ComicVine's issue summaries.
export function ComicCatchUp({ seriesId, issues }: { seriesId: number; issues: { id: number; number: string }[] }) {
  const [on, setOn] = useState(false)
  const [open, setOpen] = useState(false)
  useEffect(() => {
    fetch('/api/home/ai/on', { credentials: 'include' })
      .then((r) => (r.ok ? r.json() : { on: false }))
      .then((d: { on: boolean }) => setOn(d.on))
      .catch(() => undefined)
  }, [])
  if (!on) return null
  // The furthest issue read to the end here (the reader marks it).
  let lastRead = ''
  let lastOrder = -1
  for (const i of issues) {
    let done = false
    try {
      done = localStorage.getItem(`comic-done-${i.id}`) === '1'
    } catch {
      // no storage
    }
    const n = parseFloat(i.number)
    if (done && Number.isFinite(n) && n > lastOrder) {
      lastOrder = n
      lastRead = i.number
    }
  }
  return (
    <>
      <button className="btn-sm" onClick={() => setOpen(true)} title={t('A spoiler-free recap up to where you are')}>
        <Icon name="sparkles" size={14} /> {t('Catch me up')}
      </button>
      {open && <ComicCatchUpDialog seriesId={seriesId} lastRead={lastRead} onClose={() => setOpen(false)} />}
    </>
  )
}

export function ComicCatchUpDialog({ seriesId, lastRead, onClose }: { seriesId: number; lastRead: string; onClose: () => void }) {
  const [data, setData] = useState<{ series: string; issues: string[]; upTo: string; recap: string } | null>(null)
  const [busy, setBusy] = useState(true)
  const [error, setError] = useState('')
  const [choice, setChoice] = useState(lastRead)
  const box = useRef<HTMLDivElement>(null)
  useFocusTrap(box)
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  const load = async (upTo: string) => {
    setBusy(true)
    setError('')
    try {
      const r = await fetch('/api/home/ai/comic-recap', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ seriesId, upTo, lang: currentLanguage() }),
      })
      const d = await r.json().catch(() => null)
      if (!r.ok) throw new Error(d?.error ?? t('Something went wrong (error {status}).', { status: r.status }))
      setData(d)
      if (!upTo && d.issues.length > 0) setChoice(d.issues[0])
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }
  useEffect(() => {
    void load(lastRead)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [seriesId])

  return createPortal(
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div ref={box} tabIndex={-1} className="modal recap-dialog" role="dialog" aria-modal="true" aria-label={t('Catch me up')}>
        <div className="recap-head">
          <Icon name="sparkles" size={20} />
          <div>
            <h2>{data?.series ? t('Catch me up: {title}', { title: data.series }) : t('Catch me up')}</h2>
            {data?.upTo && <p>{data.upTo === lastRead ? t('Up to issue #{n}, the last one you read here', { n: data.upTo }) : t('Up to issue #{n}', { n: data.upTo })}</p>}
          </div>
          <button className="icon-btn" onClick={onClose} aria-label={t('Close')}>
            <Icon name="x" size={18} />
          </button>
        </div>
        {busy && <p className="hint">{t('Writing your recap…')}</p>}
        {error && <p className="error-text">{error}</p>}
        {!busy && data && !data.recap && !error && <p className="hint">{t('Choose the last issue you read, and the recap will stop there.')}</p>}
        {!busy && data?.recap && <ScrollText className="recap-text tall">{data.recap}</ScrollText>}
        {data && data.issues.length > 0 && (
          <div className="recap-pick">
            <label>
              {t('Issue')}{' '}
              <select value={choice} onChange={(e) => setChoice(e.target.value)}>
                {data.issues.map((n) => (
                  <option key={n} value={n}>
                    #{n}
                  </option>
                ))}
              </select>
            </label>
            <button className="primary btn-sm" disabled={busy || !choice || choice === data.upTo} onClick={() => void load(choice)}>
              {t('Recap up to here')}
            </button>
          </div>
        )}
        <p className="hint">{t('Written by AI from ComicVine’s issue summaries. It stops at your issue.')}</p>
      </div>
    </div>,
    document.body,
  )
}
