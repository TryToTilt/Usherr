// Usherr: Community, step 7. You → Ticket stubs: one for each watch party you
// went to, handed out "at the door" when its film started, and stamped if
// you stayed to the end (the button in the party's thread at the credits).
import { useEffect, useState } from 'react'
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
}

export default function Stubs() {
  const [d, setD] = useState<{ stubs: Stub[] | null; linked: boolean } | null>(null)
  useEffect(() => {
    fetch('/api/home/stubs', { credentials: 'include', cache: 'no-store' })
      .then((r) => (r.ok ? r.json() : null))
      .then((x) => x && setD(x as { stubs: Stub[] | null; linked: boolean }))
      .catch(() => undefined)
  }, [])
  if (!d) return null
  if (!d.linked)
    return (
      <p className="hint">
        {t('Ticket stubs come from watch parties in your Discord server. Link your Discord account first: ')}
        <Link to="/settings/services#discord">{t('Linked services')}</Link>
      </p>
    )
  const list = d.stubs ?? []
  if (list.length === 0) return <p className="hint">{t('No ticket stubs yet. Press I’m in on a watch party in Discord: when its film starts, you get one at the door, and it’s stamped if you stay to the end.')}</p>
  const stamped = list.filter((s) => s.stamped).length
  return (
    <div className="stub-page" data-walk="ticket-stubs">
      <p className="hint">
        {plural(list.length, 'One ticket stub', '{n} ticket stubs')} · {t('{n} stamped (stayed to the end)', { n: stamped })}
      </p>
      <ul className="stub-list">
        {list.map((s) => {
          const when = new Date(s.atMs)
          return (
            <li key={s.showtimeId} className={`stub${s.stamped ? ' stub-stamped' : ''}`}>
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
                <span className="stub-who">{s.seat === 0 ? t('You hosted') : t('Seat {n} · hosted by {host}', { n: s.seat, host: s.host })}</span>
                {(s.guests ?? []).length > 0 && <span className="stub-with">{t('With {names}', { names: (s.guests ?? []).join(', ') })}</span>}
              </div>
              <div className="stub-tear" aria-hidden="true" />
              <div className="stub-end">
                <span className="stub-no">No. {String(s.showtimeId).padStart(4, '0')}</span>
                {s.stamped && <span className="stub-stamp">{t('Stayed to the end')}</span>}
              </div>
            </li>
          )
        })}
      </ul>
    </div>
  )
}
