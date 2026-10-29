// Home fork: Tonight, from the top bar (Usherr: the button beside the dice). One tap asks the AI for three things
// to watch tonight, from what you're in the middle of, the next episodes of
// your shows and films you have but haven't seen (from Jellyfin or Emby).
// Nothing is asked until you tap, and the answer is kept for the evening.
import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { Link } from 'react-router-dom'
import Icon from '../components/Icon'
import { currentLanguage, t } from '../i18n'

interface Pick {
  kind: 'movie' | 'tv'
  tmdbId: number
  title: string
  year?: number
  posterUrl?: string
  label: 'Continue' | 'Next' | 'Unwatched' | 'New show'
  episode?: string
  why: string
  to: string
}

interface Answer {
  note: string
  items: Pick[]
}

const KEY = 'tonight-answer'

function labelOf(p: Pick): string {
  switch (p.label) {
    case 'Continue':
      return t('Pick up where you left off')
    case 'Next':
      return t('Next episode')
    case 'New show':
      return t('A show you haven’t started')
    default:
      return t('A film you haven’t seen')
  }
}

export default function Tonight({ autoAsk, onPick }: { autoAsk?: boolean; onPick?: () => void } = {}) {
  const [on, setOn] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [answer, setAnswer] = useState<Answer | null>(() => {
    try {
      const v = sessionStorage.getItem(KEY)
      return v ? (JSON.parse(v) as Answer) : null
    } catch {
      return null
    }
  })
  useEffect(() => {
    fetch('/api/home/ai/on', { credentials: 'include' })
      .then((r) => (r.ok ? r.json() : { on: false }))
      .then((d: { on: boolean }) => setOn(d.on))
      .catch(() => undefined)
  }, [])
  const ask = async (again: boolean) => {
    setBusy(true)
    setError('')
    try {
      const r = await fetch('/api/home/ai/tonight', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ lang: currentLanguage(), again }),
      })
      const d = await r.json().catch(() => null)
      if (!r.ok) throw new Error(d?.error ?? t('Something went wrong (error {status}).', { status: r.status }))
      setAnswer(d as Answer)
      try {
        sessionStorage.setItem(KEY, JSON.stringify(d))
      } catch {
        // remembering is optional
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }
  // Opened from the AI bar: ask straight away (an answer from this evening is reused).
  useEffect(() => {
    if (autoAsk && on && !answer) void ask(false)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [autoAsk, on])
  if (!on) return null

  return (
    // Usherr: just the button, on its own line (Roulette is the dice in the top bar).
    <section className={`card tonight-card${answer || error ? '' : ' tonight-idle'}`} aria-label={t('Tonight')}>
      <div className="tonight-head tonight-solo">
        {answer?.note && <p className="hint">{answer.note}</p>}
        <button className={`btn-with-icon tonight-ask${answer ? '' : ' primary'}`} onClick={() => void ask(!!answer)} disabled={busy}>
          <Icon name={answer ? 'shuffle' : 'sparkles'} size={14} /> {busy ? t('Thinking…') : answer ? t('Something else') : t('What should I watch tonight?')}
        </button>
      </div>
      {error && <p className="error-text">{error}</p>}
      {answer && answer.items.length > 0 && (
        <div className="tonight-list">
          {answer.items.map((p) => (
            <Link key={`${p.kind}-${p.tmdbId}`} to={p.to} className="tonight-item" onClick={onPick}>
              {p.posterUrl ? <img src={p.posterUrl} alt="" loading="lazy" /> : <span className="tonight-noposter" />}
              <span className="tonight-body">
                <small>{labelOf(p)}</small>
                <b>
                  {p.title} {p.year ? <span className="hint">({p.year})</span> : null}
                </b>
                {p.episode && <span className="tonight-ep">{p.episode}</span>}
                <span className="tonight-why">{p.why}</span>
              </span>
            </Link>
          ))}
        </div>
      )}
    </section>
  )
}

// Usherr: the top bar's button beside the dice. It opens Tonight in a window,
// asking straight away (or showing this evening's answer again). Hidden when
// the AI is off.
export function TonightButton() {
  const [on, setOn] = useState(false)
  const [open, setOpen] = useState(false)
  const box = useRef<HTMLDivElement>(null)
  useEffect(() => {
    fetch('/api/home/ai/on', { credentials: 'include' })
      .then((r) => (r.ok ? r.json() : { on: false }))
      .then((d: { on: boolean }) => setOn(d.on))
      .catch(() => undefined)
  }, [])
  useEffect(() => {
    if (open) box.current?.focus()
  }, [open])
  if (!on) return null
  const close = () => setOpen(false)
  return (
    <>
      <button type="button" className="icon-btn top-tonight" onClick={() => setOpen(true)} title={t('What should I watch tonight?')}>
        <Icon name="sparkles" size={19} />
        <span className="top-tonight-label">{t("Tonight's picks")}</span>
      </button>
      {open &&
        createPortal(
          <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && close()} onKeyDown={(e) => e.key === 'Escape' && close()}>
            <div ref={box} tabIndex={-1} className="modal roulette tonight-modal" role="dialog" aria-modal="true" aria-label={t('Tonight')}>
              <div className="roulette-head">
                <h2>
                  <Icon name="sparkles" size={18} /> {t('Tonight')}
                </h2>
                <button className="icon-btn" onClick={close} aria-label={t('Close')}>
                  <Icon name="x" size={16} />
                </button>
              </div>
              <Tonight autoAsk onPick={close} />
            </div>
          </div>,
          document.body,
        )}
    </>
  )
}
