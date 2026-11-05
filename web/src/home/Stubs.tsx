// Usherr: Community, step 7. You → Ticket stubs: one for each watch party you
// went to, handed out "at the door" when its film started, and stamped if
// you stayed to the end (the button in the party's thread at the credits).
// A friend's party joined with its showtime code gets a stub here too, kept
// by this Usherr: stamped when its player reaches the credits, or with Stamp
// it for half an hour after (home_community_guest.go). Found in the host's
// server (its bot added there), it says who hosted and who else came.
// Remove takes a stub off (asked twice); it's kept hidden, so Undo brings
// it back.
import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { useConfirm } from '../components/ConfirmProvider'
import { locale, plural, t } from '../i18n'
import './stubs.css'

interface Stub {
  showtimeId: number
  tmdbId: number
  title: string
  year?: number
  poster?: string
  host: string
  seat: number // 0: you hosted
  guests: string[] | null
  atMs: number
  stamped: boolean
  here?: boolean // a friend's party, joined with its code
  stampUntil?: number // can be stamped by hand until then
}

export default function Stubs() {
  const [d, setD] = useState<{ stubs: Stub[] | null; linked: boolean } | null>(null)
  const [error, setError] = useState('')
  const [arm, setArm] = useState('') // the stub whose Remove was pressed once
  const [removed, setRemoved] = useState<{ key: string; title: string } | null>(null)
  const confirm = useConfirm()
  const load = useCallback(() => {
    fetch('/api/home/stubs', { credentials: 'include', cache: 'no-store' })
      .then((r) => (r.ok ? r.json() : null))
      .then((x) => x && setD(x as { stubs: Stub[] | null; linked: boolean }))
      .catch(() => undefined)
  }, [])
  useEffect(load, [load])
  async function stamp(id: number) {
    setError('')
    const r = await fetch(`/api/home/stubs/${id}/stamp`, { method: 'POST', credentials: 'include' }).catch(() => null)
    if (r && !r.ok) setError(((await r.json().catch(() => null)) as { error?: string } | null)?.error ?? t('Couldn’t stamp it.'))
    load()
  }
  const keyOf = (s: Stub) => `${s.here ? 'here' : 'discord'}/${s.showtimeId}`
  async function remove(s: Stub) {
    setArm('')
    const ok = await confirm({
      title: t('Remove your {title} stub?', { title: s.title }),
      body: t('It goes from your ticket stubs here and from /stubs in Discord. You can bring it back with Undo straight after.'),
      confirmLabel: t('Remove it'),
      cancelLabel: t('Keep it'),
      danger: true,
    })
    if (!ok) return
    setError('')
    const r = await fetch(`/api/home/stubs/${keyOf(s)}`, { method: 'DELETE', credentials: 'include' }).catch(() => null)
    if (!r || !r.ok) {
      setError(((await r?.json().catch(() => null)) as { error?: string } | null)?.error ?? t('Couldn’t remove it.'))
      return
    }
    setRemoved({ key: keyOf(s), title: s.title })
    load()
  }
  async function undo() {
    if (!removed) return
    const r = await fetch(`/api/home/stubs/${removed.key}/restore`, { method: 'POST', credentials: 'include' }).catch(() => null)
    if (r && r.ok) setRemoved(null)
    else setError(t('Couldn’t bring it back.'))
    load()
  }
  if (!d) return null
  const list = d.stubs ?? []
  const undoBar = removed && (
    <p className="notice stub-undo" role="status">
      {t('Removed the {title} stub.', { title: removed.title })}{' '}
      <button type="button" className="btn-sm" onClick={() => void undo()}>
        {t('Undo')}
      </button>
    </p>
  )
  if (list.length === 0 && removed) return <div className="stub-page">{undoBar}</div>
  if (list.length === 0)
    return (
      <div className="hint stub-none">
        <p>{t('No ticket stubs yet. You get one at the door of each watch party you go to, and it’s stamped if you stay to the end.')}</p>
        <p>{t('A friend’s party: paste its showtime code under Showtime, Join a watch party, and your stub shows here when the film starts. The stubs a friend’s Discord bot keeps for you are under /stubs in their server.')}</p>
        {!d.linked && (
          <p>
            {t('Parties from this Usherr’s own Discord server need your Discord account linked: ')}
            <Link to="/settings/services#discord">{t('Linked services')}</Link>
          </p>
        )}
      </div>
    )
  const stamped = list.filter((s) => s.stamped).length
  return (
    <div className="stub-page" data-walk="ticket-stubs">
      <p className="hint">
        {plural(list.length, 'One ticket stub', '{n} ticket stubs')} · {t('{n} stamped (stayed to the end)', { n: stamped })}
      </p>
      {error && <p className="error-text">{error}</p>}
      {undoBar}
      <ul className="stub-list">
        {list.map((s) => {
          const when = new Date(s.atMs)
          return (
            <li key={`${s.here ? 'here' : 'discord'}-${s.showtimeId}`} className={`stub${s.stamped ? ' stub-stamped' : ''}`}>
              <Link to={`/title/${s.tmdbId}`} className="stub-poster" tabIndex={-1} aria-hidden="true">
                {s.poster ? <img src={s.poster} alt="" loading="lazy" /> : <span />}
              </Link>
              <div className="stub-body">
                <span className="stub-admit">{t('ADMIT ONE')}</span>
                <Link to={`/title/${s.tmdbId}`} className="stub-title">
                  {s.title}
                  {s.year ? ` (${s.year})` : ''}
                </Link>
                <span className="stub-when">
                  {when.toLocaleDateString(locale(), { weekday: 'short', month: 'short', day: 'numeric', year: 'numeric' })} · {when.toLocaleTimeString(locale(), { hour: 'numeric', minute: '2-digit' })}
                </span>
                <span className="stub-who">{s.here && !s.host ? t('A friend’s watch party') : s.seat === 0 ? t('You hosted') : t('Seat {n} · hosted by {host}', { n: s.seat, host: s.host })}</span>
                {(s.guests ?? []).length > 0 && <span className="stub-with">{t('With {names}', { names: (s.guests ?? []).join(', ') })}</span>}
              </div>
              <div className="stub-tear" aria-hidden="true" />
              <div className="stub-end">
                <span className="stub-no">No. {String(s.showtimeId).padStart(4, '0')}</span>
                {s.stamped && <span className="stub-stamp">{t('Stayed to the end')}</span>}
                {!s.stamped && s.stampUntil && (
                  <button type="button" className="btn-sm" onClick={() => void stamp(s.showtimeId)} title={t('Stayed to the end? Stamp it in the half hour after the credits.')}>
                    {t('Stamp it')}
                  </button>
                )}
                {arm === keyOf(s) ? (
                  <span className="stub-remove">
                    <button type="button" className="btn-sm danger" onClick={() => void remove(s)}>
                      {t('Remove?')}
                    </button>
                    <button type="button" className="btn-sm" onClick={() => setArm('')}>
                      {t('Keep')}
                    </button>
                  </span>
                ) : (
                  <button type="button" className="stub-remove-btn" onClick={() => setArm(keyOf(s))} aria-label={t('Remove the {title} stub', { title: s.title })}>
                    {t('Remove')}
                  </button>
                )}
              </div>
            </li>
          )
        })}
      </ul>
    </div>
  )
}
