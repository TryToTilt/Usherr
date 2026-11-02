// Usherr: Community, step 7. You → Ticket stubs: one for each watch party you
// went to, handed out "at the door" when its film started, and stamped if
// you stayed to the end (the button in the party's thread at the credits).
// A friend's party joined with its showtime code gets a stub here too, kept
// by this Usherr: stamped when its player reaches the credits, or with Stamp
// it for half an hour after (home_community_guest.go).
import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
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
  if (!d) return null
  const list = d.stubs ?? []
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
                <span className="stub-who">{s.here ? t('A friend’s watch party') : s.seat === 0 ? t('You hosted') : t('Seat {n} · hosted by {host}', { n: s.seat, host: s.host })}</span>
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
              </div>
            </li>
          )
        })}
      </ul>
    </div>
  )
}
