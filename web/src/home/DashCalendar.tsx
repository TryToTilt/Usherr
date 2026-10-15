// Home fork: the Dashboard's calendar, folded away until you open it (it
// starts closed on phones and tablets, and remembers what you chose). Its
// view button turns between Month, Week and Day; Month is the usual grid,
// Week and Day a short list of what comes out.
import { useState } from 'react'
import { Link } from 'react-router-dom'
import type { CalendarEntry } from '../api'
import CalendarGrid from '../components/CalendarGrid'
import Icon from '../components/Icon'
import { locale, plural, t } from '../i18n'
import './news.css'

type Span = 'month' | 'week' | 'day'
const OPEN_KEY = 'dash-calendar-open'
const SPAN_KEY = 'dash-calendar-span'

function read<T extends string>(key: string, ok: readonly T[], fallback: T): T {
  try {
    const v = localStorage.getItem(key) as T | null
    return v && ok.includes(v) ? v : fallback
  } catch {
    return fallback
  }
}

function save(key: string, v: string) {
  try {
    localStorage.setItem(key, v)
  } catch {
    // remembering is optional
  }
}

const day = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`

function linkOf(e: CalendarEntry): string {
  switch (e.kind) {
    case 'movie':
      return `/title/${e.tmdbId}`
    case 'episode':
      return `/series/${e.seriesId}`
    case 'album':
      return `/music/artist/${e.artistId}`
    case 'comic':
      return `/comics/${e.comicSeriesId}`
    default:
      return `/book/${e.bookId}`
  }
}

function statusText(s: CalendarEntry['status']): string {
  return s === 'downloaded' ? t('Have it') : s === 'downloading' ? t('Downloading') : t('Waiting')
}

export default function DashCalendar({ entries }: { entries: CalendarEntry[] }) {
  const small = typeof window !== 'undefined' && window.innerWidth < 1100
  const [open, setOpen] = useState(() => read(OPEN_KEY, ['1', '0'] as const, small ? '0' : '1') === '1')
  const [span, setSpan] = useState<Span>(() => read(SPAN_KEY, ['month', 'week', 'day'] as const, 'week'))
  const [at, setAt] = useState(() => new Date())

  const toggle = () => {
    setOpen((o) => {
      save(OPEN_KEY, o ? '0' : '1')
      return !o
    })
  }
  const nextSpan = () => {
    const n: Span = span === 'month' ? 'week' : span === 'week' ? 'day' : 'month'
    setSpan(n)
    setAt(new Date())
    save(SPAN_KEY, n)
  }
  const spanName = span === 'month' ? t('Month') : span === 'week' ? t('Week') : t('Day')

  // The days the list covers: the week from Monday, or the one day.
  const start = new Date(at)
  if (span === 'week') start.setDate(start.getDate() - ((start.getDay() + 6) % 7))
  const days = span === 'week' ? 7 : 1
  const end = new Date(start)
  end.setDate(end.getDate() + days - 1)
  const from = day(start)
  const to = day(end)
  const inSpan = entries.filter((e) => e.releaseDate >= from && e.releaseDate <= to).sort((a, b) => a.releaseDate.localeCompare(b.releaseDate))
  const move = (dir: number) => {
    const d = new Date(at)
    d.setDate(d.getDate() + dir * days)
    setAt(d)
  }
  const thisWeek = (() => {
    const s = new Date()
    s.setDate(s.getDate() - ((s.getDay() + 6) % 7))
    const e = new Date(s)
    e.setDate(e.getDate() + 6)
    return entries.filter((x) => x.releaseDate >= day(s) && x.releaseDate <= day(e)).length
  })()
  const rangeLabel =
    span === 'day'
      ? start.toLocaleDateString(locale(), { weekday: 'long', day: 'numeric', month: 'long' })
      : `${start.toLocaleDateString(locale(), { day: 'numeric', month: 'short' })} – ${end.toLocaleDateString(locale(), { day: 'numeric', month: 'short' })}`

  return (
    <section className="tile tone-calendar wide-tile dash-cal">
      <div className="tile-head">
        <button className="dash-cal-toggle" onClick={toggle} aria-expanded={open}>
          <span className="tile-ico">
            <Icon name="calendar" size={18} />
          </span>
          <h2>{t('Calendar')}</h2>
          {!open && <span className="hint">{plural(thisWeek, '{n} release this week', '{n} releases this week')}</span>}
          <Icon name={open ? 'chevron-up' : 'chevron-down'} size={16} />
        </button>
        {open && (
          <button className="room-chip on dash-cal-span" onClick={nextSpan} title={t('Switch between month, week and day')}>
            {spanName} <Icon name="refresh" size={12} />
          </button>
        )}
        <Link className="tile-link" to="/calendar">
          {t('Full page')} <Icon name="open" size={13} />
        </Link>
      </div>
      {open && span === 'month' && <CalendarGrid entries={entries} compact />}
      {open && span !== 'month' && (
        <div className="dash-cal-list">
          <div className="dash-cal-nav">
            <button className="icon-btn" onClick={() => move(-1)} aria-label={span === 'day' ? t('The day before') : t('The week before')}>
              <Icon name="chevron-left" size={16} />
            </button>
            <b>{rangeLabel}</b>
            <button className="icon-btn" onClick={() => move(1)} aria-label={span === 'day' ? t('The next day') : t('The next week')}>
              <Icon name="chevron-right" size={16} />
            </button>
          </div>
          {inSpan.length === 0 ? (
            <p className="hint">{span === 'day' ? t('Nothing comes out that day.') : t('Nothing comes out that week.')}</p>
          ) : (
            <ul className="mini-list compact">
              {inSpan.map((e) => (
                <li key={`${e.kind}-${e.id}`}>
                  <Link className="mini-link" to={linkOf(e)}>
                    <span className="date-chip">
                      <b>{new Date(`${e.releaseDate}T00:00:00`).getDate()}</b>
                      {new Date(`${e.releaseDate}T00:00:00`).toLocaleString(locale(), { weekday: 'short' })}
                    </span>
                    {e.posterUrl && (
                      <div className="mini-thumb">
                        <img src={e.posterUrl} alt="" loading="lazy" decoding="async" />
                      </div>
                    )}
                    <div className="mini-main">
                      <strong>{e.title}</strong>
                      {e.subtitle && <small className="qwhen">{e.subtitle.split(' · ')[0]}</small>}
                    </div>
                    <span className={`badge ${e.status}`}>{statusText(e.status)}</span>
                  </Link>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </section>
  )
}
