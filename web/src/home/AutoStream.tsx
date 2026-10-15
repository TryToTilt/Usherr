// Home fork: Streams for you, on the You page. Switch it on and the films
// that fit your taste best go into the streamed library by themselves (a few
// a week, very strong matches only), with nothing downloaded. Lists what it
// added, each with a Remove button; nothing is ever removed by itself. An
// administrator allows it for the household first.
import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import Icon from '../components/Icon'
import Switch from '../components/Switch'
import { currentLanguage, t } from '../i18n'
import './news.css'

interface Added {
  id: number
  tmdbId: number
  title: string
  year?: number
  image?: string
  match: number
  because?: string
  addedAt: string
  removed?: boolean
}
interface State {
  allowed: boolean
  ready: boolean
  admin: boolean
  on: boolean
  thisWeek: number
  perWeek: number
  floor: number
  added: Added[]
}

async function send<T>(path: string, init?: RequestInit): Promise<T> {
  const r = await fetch(path, { credentials: 'include', ...init })
  const d = await r.json().catch(() => ({}))
  if (!r.ok) throw new Error((d as { error?: string }).error || r.statusText)
  return d as T
}
const put = (body: unknown): RequestInit => ({ method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })

export default function AutoStream() {
  const [st, setSt] = useState<State | null>(null)
  const [busy, setBusy] = useState(false)
  const [note, setNote] = useState('')
  const [error, setError] = useState('')
  const load = () =>
    send<State>('/api/home/autostream')
      .then(setSt)
      .catch(() => setSt(null))
  useEffect(() => void load(), [])
  if (!st || !st.ready || (!st.allowed && !st.admin)) return null

  const household = (on: boolean) => {
    setError('')
    send('/api/home/autostream/household', put({ on }))
      .then(load)
      .catch((e: Error) => setError(e.message))
  }
  const mine = (on: boolean) => {
    setBusy(true)
    setError('')
    setNote('')
    send<{ added?: Added | null }>('/api/home/autostream', put({ on }))
      .then((d) => {
        if (on) setNote(d.added ? t('Added {title} to get you started.', { title: d.added.title }) : t('Nothing fits well enough right now. It looks again a few times a day.'))
        return load()
      })
      .catch((e: Error) => setError(e.message))
      .finally(() => setBusy(false))
  }
  const remove = (a: Added) => {
    setSt({ ...st, added: st.added.map((x) => (x.id === a.id ? { ...x, removed: true } : x)) })
    send(`/api/home/autostream/${a.id}`, { method: 'DELETE' }).catch(load)
  }
  const day = (iso: string) => new Date(iso).toLocaleDateString(currentLanguage(), { day: 'numeric', month: 'short' })

  return (
    <section className="card you-card autostream">
      <h2>
        <Icon name="play" size={18} /> {t('Streams for you')}
      </h2>
      <p className="hint">
        {t(
          'Films that fit your taste best go into the streamed library by themselves: up to {n} a week, only {floor}% matches or better, never something the household already has. Nothing is downloaded: StreamNZB finds a release when someone presses play. Nothing is ever removed by itself.',
          { n: st.perWeek, floor: st.floor },
        )}
      </p>
      {st.admin && <Switch checked={st.allowed} onChange={household} label={t('Allow Streams for you in this household')} />}
      {st.allowed ? <Switch checked={st.on} onChange={(v) => !busy && mine(v)} label={t('Add films that fit my taste')} /> : <p className="hint">{t('Off for everyone until it is allowed here.')}</p>}
      {st.on && <p className="hint">{t('{n} of {max} this week.', { n: st.thisWeek, max: st.perWeek })}</p>}
      {busy && <p className="hint">{t('Looking for a first film…')}</p>}
      {note && <p>{note}</p>}
      {error && <p className="error-text">{error}</p>}
      {st.added.length > 0 && (
        <ul className="autostream-list">
          {st.added.map((a) => (
            <li key={a.id} className={a.removed ? 'removed' : ''}>
              <Link to={`/title/${a.tmdbId}`} className="diary-poster" tabIndex={-1} aria-hidden="true">
                {a.image ? <img src={a.image} alt="" loading="lazy" /> : <Icon name="film" size={18} />}
              </Link>
              <div>
                <Link to={`/title/${a.tmdbId}`} className="autostream-title">
                  {a.title}
                </Link>
                {a.year ? <span className="hint"> {a.year}</span> : null}
                <div className="hint">
                  {t('{n}% match', { n: a.match })}
                  {a.because ? ` · ${t('like {title}', { title: a.because })}` : ''} · {t('added {day}', { day: day(a.addedAt) })}
                </div>
              </div>
              {a.removed ? (
                <span className="hint">{t('Taken out')}</span>
              ) : (
                <button className="btn-sm" onClick={() => remove(a)}>
                  {t('Remove')}
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
