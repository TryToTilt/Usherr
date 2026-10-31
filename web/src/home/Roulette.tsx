// Home fork: Roulette. Spin for one title to watch tonight: from what the
// people watching have and haven't seen, or from well-rated titles the house
// doesn't have. Keeps to a mood, a running time and the family filter. A reel
// of posters spins and slows, then lands on the pick.
import { useEffect, useRef, useState } from 'react'
import type { DiscoverMovie } from '../api'
import AddDialog, { type AddTarget } from '../components/AddDialog'
import { DiscoverCards } from '../components/DiscoverRail'
import Icon from '../components/Icon'
import { t } from '../i18n'
import { useOwned } from '../useOwned'
import { moods } from './RoomPick'
import { useFamilyLevel } from './useFamily'
import './news.css'

interface Person {
  id: string
  name: string
}
interface Spin {
  pick: DiscoverMovie | null
  reel: DiscoverMovie[]
  runtime?: number
  pool: number
  note?: string
}

const MINUTES = [0, 90, 120, 150]

export default function Roulette({ onClose }: { onClose: () => void }) {
  const owned = useOwned()
  const [adding, setAdding] = useState<AddTarget | null>(null)
  const [people, setPeople] = useState<Person[]>([])
  const [room, setRoom] = useState<string[]>([])
  const [kind, setKind] = useState<'movie' | 'tv'>('movie')
  const [source, setSource] = useState<'here' | 'anywhere'>('here')
  const [mood, setMood] = useState('')
  const [minutes, setMinutes] = useState(0)
  const [family] = useFamilyLevel()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [spin, setSpin] = useState<Spin | null>(null)
  const [face, setFace] = useState<DiscoverMovie | null>(null) // what the reel shows now
  const [landed, setLanded] = useState(false)
  const timer = useRef<number | undefined>(undefined)
  const box = useRef<HTMLDivElement>(null)

  useEffect(() => {
    box.current?.focus()
    fetch('/api/home/discover/people?room=1', { credentials: 'include' })
      .then((r) => (r.ok ? r.json() : null))
      .then((d: { people: Person[]; me: string } | null) => {
        if (!d) return
        setPeople(d.people)
        if (d.me) setRoom([d.me])
      })
      .catch(() => undefined)
    return () => window.clearTimeout(timer.current)
  }, [])

  const toggle = (id: string) => setRoom((cur) => (cur.includes(id) ? cur.filter((x) => x !== id) : [...cur, id]))

  // The reel: posters go by fast, then slower, then the pick.
  const play = (s: Spin) => {
    const frames = [...s.reel, ...s.reel, ...s.reel].filter((m) => m.posterUrl).slice(0, 14)
    let k = 0
    setLanded(false)
    const step = () => {
      if (k >= frames.length || !s.pick) {
        setFace(s.pick)
        setLanded(true)
        return
      }
      setFace(frames[k])
      k++
      timer.current = window.setTimeout(step, 60 + k * k * 4)
    }
    step()
  }

  const go = () => {
    setBusy(true)
    setError('')
    setSpin(null)
    setFace(null)
    const q = new URLSearchParams({ kind, source, mood })
    if (source === 'here') q.set('who', room.join(','))
    if (minutes && kind === 'movie') q.set('minutes', String(minutes))
    if (family !== 'off') q.set('age', family)
    fetch(`/api/home/roulette?${q}`, { credentials: 'include' })
      .then(async (r) => {
        const d = await r.json().catch(() => ({}))
        if (!r.ok) throw new Error((d as { error?: string }).error || r.statusText)
        return d as Spin
      })
      .then((s) => {
        setSpin(s)
        if (s.pick) play(s)
      })
      .catch((e: Error) => setError(e.message))
      .finally(() => setBusy(false))
  }

  const chip = (on: boolean) => `room-chip${on ? ' on' : ''}`
  return (
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()} onKeyDown={(e) => e.key === 'Escape' && onClose()}>
      <div ref={box} tabIndex={-1} className="modal roulette" data-walk="roulette-box" role="dialog" aria-modal="true" aria-label={t('Roulette')}>
        <div className="roulette-head">
          <h2>
            <Icon name="shuffle" size={18} /> {t('Roulette')}
          </h2>
          <button className="icon-btn" onClick={onClose} aria-label={t('Close')}>
            <Icon name="x" size={16} />
          </button>
        </div>
        <div className="room-pick">
          <div className="room-row" role="group" aria-label={t('From')}>
            <button type="button" className={chip(source === 'here')} aria-pressed={source === 'here'} onClick={() => setSource('here')}>
              {t('Already here')}
            </button>
            <button type="button" className={chip(source === 'anywhere')} aria-pressed={source === 'anywhere'} onClick={() => setSource('anywhere')}>
              {t('Something new')}
            </button>
            <span className="room-sep" />
            <button type="button" className={chip(kind === 'movie')} aria-pressed={kind === 'movie'} onClick={() => setKind('movie')}>
              <Icon name="film" size={14} /> {t('A film')}
            </button>
            <button type="button" className={chip(kind === 'tv')} aria-pressed={kind === 'tv'} onClick={() => setKind('tv')}>
              <Icon name="tv" size={14} /> {t('A show')}
            </button>
          </div>
          {source === 'here' && people.length > 1 && (
            <div className="room-row" role="group" aria-label={t("Who's watching")}>
              <span className="room-label">{t("Who's watching")}</span>
              {people.map((p) => (
                <button key={p.id} type="button" className={chip(room.includes(p.id))} aria-pressed={room.includes(p.id)} onClick={() => toggle(p.id)}>
                  {p.name}
                </button>
              ))}
            </div>
          )}
          <div className="room-row" role="group" aria-label={t('Mood')}>
            {moods().map((m) => (
              <button key={m.key} type="button" className={chip(mood === m.key)} aria-pressed={mood === m.key} onClick={() => setMood(m.key)}>
                {m.label}
              </button>
            ))}
          </div>
          {kind === 'movie' && (
            <div className="room-row" role="group" aria-label={t('How long')}>
              <span className="room-label">{t('How long')}</span>
              {MINUTES.map((m) => (
                <button key={m} type="button" className={chip(minutes === m)} aria-pressed={minutes === m} onClick={() => setMinutes(m)}>
                  {m ? t('Under {n} min', { n: m }) : t('Any length')}
                </button>
              ))}
            </div>
          )}
          {family !== 'off' && <p className="hint">{t('The family filter is on: only titles for up to {n}.', { n: family })}</p>}
        </div>

        <div className={`roulette-stage${landed ? ' landed' : ''}`} aria-live="polite">
          {face ? (
            landed && spin?.pick ? (
              <div className="roulette-pick">
                <DiscoverCards items={[spin.pick]} kind={kind} owned={owned} onAdd={setAdding} />
                <p className="hint">
                  {spin.runtime ? `${t('{n} min', { n: spin.runtime })} · ` : ''}
                  {t('chosen from {n}', { n: spin.pool })}
                </p>
              </div>
            ) : (
              <img className="roulette-face" src={face.posterUrl} alt="" />
            )
          ) : (
            <div className="roulette-empty">
              <Icon name="shuffle" size={40} />
              {spin?.note ? <p>{spin.note}</p> : <p className="hint">{t('Choose, then spin.')}</p>}
            </div>
          )}
        </div>
        {error && <p className="error-text">{error}</p>}
        <div className="roulette-nav">
          <button className="primary btn-with-icon" onClick={go} disabled={busy || (source === 'here' && room.length === 0)} autoFocus>
            <Icon name="shuffle" size={16} /> {busy ? t('Spinning…') : spin?.pick ? t('Spin again') : t('Spin')}
          </button>
        </div>
      </div>
      {adding && (
        <AddDialog
          target={adding}
          onClose={() => setAdding(null)}
          onAdded={() => {
            setAdding(null)
            owned.reload()
          }}
        />
      )}
    </div>
  )
}
