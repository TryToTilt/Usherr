// Home fork: the week's new comics (after Pullbox's "What's New"), from
// ComicVine's store dates. Pick the week, keep to the publishers you follow,
// and Pull: the series joins the library (new issues come in by themselves)
// and this issue is fetched.
import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { useRowMemory } from './rowMemory'
import { Link } from 'react-router-dom'
import Icon from '../components/Icon'
import { comicsApi, type ComicWeek, type ComicWeekIssue } from './comicsApi'
import { locale, t } from '../i18n'
import './comics.css'
import { useAuth } from '../AuthContext'
import { can } from '../api'

const PUB_KEY = 'comics-week-publishers'

function savedPublishers(): string[] {
  try {
    return JSON.parse(localStorage.getItem(PUB_KEY) ?? '[]') as string[]
  } catch {
    return []
  }
}

function addDays(iso: string, n: number) {
  const d = new Date(iso + 'T12:00:00Z')
  d.setUTCDate(d.getUTCDate() + n)
  return d.toISOString().slice(0, 10)
}

// mondayOf is the Monday of the week holding iso (or today), as YYYY-MM-DD.
function mondayOf(iso: string): string {
  const d = iso ? new Date(iso + 'T12:00:00Z') : new Date()
  const day = iso ? d.getUTCDay() : d.getDay()
  const base = iso ? iso : `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
  return addDays(base, -((day + 6) % 7))
}

// pubColor gives each publisher a colour of its own, the same every time.
function pubColor(name: string): string {
  let h = 0
  for (const ch of name) h = (h * 31 + ch.charCodeAt(0)) % 360
  return `hsl(${h} 70% 62%)`
}

const nice = (iso: string) => new Date(iso + 'T12:00:00Z').toLocaleDateString(locale(), { month: 'short', day: 'numeric', timeZone: 'UTC' })

// A publisher's sideways row, which remembers how far it was swiped.
function Strip({ id, children }: { id: string; children: ReactNode }) {
  const el = useRef<HTMLUListElement>(null)
  useRowMemory(el, `comicweek:${id}`)
  return (
    <ul ref={el} className="cx-week-strip">
      {children}
    </ul>
  )
}

export default function ComicWeekView({ admin }: { admin: boolean }) {
  const { user } = useAuth()
  const canPull = admin || can(user, 'comics') // home fork: or the comics switch
  const [date, setDate] = useState('')
  const [week, setWeek] = useState<ComicWeek | null>(null)
  const [error, setError] = useState('')
  const [pubs, setPubs] = useState<string[]>(savedPublishers)
  const [mine, setMine] = useState(false)
  const [q, setQ] = useState('')
  const [busy, setBusy] = useState(0)
  const [note, setNote] = useState('')

  useEffect(() => {
    setWeek(null)
    setError('')
    comicsApi
      .week(date)
      .then(setWeek)
      .catch((e: Error) => setError(e.message))
  }, [date])

  const publishers = useMemo(() => {
    const n = new Map<string, number>()
    for (const i of week?.issues ?? []) if (i.publisher) n.set(i.publisher, (n.get(i.publisher) ?? 0) + 1)
    return [...n.entries()].sort((a, b) => b[1] - a[1]).map(([p]) => p)
  }, [week])

  const shown = useMemo(() => {
    const needle = q.trim().toLowerCase()
    return (week?.issues ?? [])
      .filter((i) => (pubs.length === 0 || pubs.includes(i.publisher ?? '')) && (!mine || i.seriesId) && (!needle || i.series.toLowerCase().includes(needle)))
      .sort((a, b) => a.series.localeCompare(b.series) || a.number.localeCompare(b.number, undefined, { numeric: true }))
  }, [week, pubs, mine, q])

  // Rows: the series you follow first, then each publisher (biggest first).
  const groups = useMemo(() => {
    const out: { name: string; color: string; items: ComicWeekIssue[] }[] = []
    const yours = shown.filter((i) => i.seriesId)
    if (yours.length > 0 && !mine) out.push({ name: t('Your series'), color: 'var(--sec)', items: yours })
    const by = new Map<string, ComicWeekIssue[]>()
    for (const i of shown) {
      const k = i.publisher || t('Other publishers')
      by.set(k, [...(by.get(k) ?? []), i])
    }
    for (const [name, items] of [...by.entries()].sort((a, b) => b[1].length - a[1].length)) out.push({ name, color: pubColor(name), items })
    return out
  }, [shown, mine])

  const choosePub = (p: string) => {
    const next = p ? [p] : []
    setPubs(next)
    try {
      localStorage.setItem(PUB_KEY, JSON.stringify(next))
    } catch {
      /* storage may be off */
    }
  }

  const pull = (i: ComicWeekIssue) => {
    setBusy(i.cvId)
    setNote('')
    comicsApi
      .pull(i.cvId, i.volumeCvId)
      .then((r) => {
        setNote(`${i.series} #${i.number}: ${r.message}`)
        setWeek((w) => (w ? { ...w, issues: w.issues.map((x) => (x.cvId === i.cvId ? { ...x, seriesId: r.seriesId, issueId: r.issueId, wanted: true, status: x.status ?? 'missing' } : x)) } : w))
      })
      .catch((e: Error) => setError(e.message))
      .finally(() => setBusy(0))
  }

  // The week asked for, known before ComicVine answers (so the arrows always work).
  const from = week?.from ?? mondayOf(date)
  const to = week?.to ?? addDays(from, 6)
  const isThisWeek = from === mondayOf('')
  return (
    <div className="cx-week">
      <div className="cx-week-nav" data-walk="cx-week-nav">
        <button className="btn-sm" onClick={() => setDate(addDays(from, -7))} aria-label={t('Week before')}>
          ‹
        </button>
        <strong>{`${nice(from)} – ${nice(to)}`}</strong>
        <button className="btn-sm" onClick={() => setDate(addDays(from, 7))} aria-label={t('Week after')}>
          ›
        </button>
        {date && (
          <button className="btn-sm" onClick={() => setDate('')}>
            {t('This week')}
          </button>
        )}
        <input className="cx-week-find" value={q} onChange={(e) => setQ(e.target.value)} placeholder={t('Find a series')} />
        <label className="cx-check" data-walk="cx-week-mine">
          <input type="checkbox" checked={mine} onChange={(e) => setMine(e.target.checked)} /> {t('Only my series')}
        </label>
      </div>
      {publishers.length > 1 && (
        <select className="disc-who cx-week-pub" data-walk="cx-week-pub" value={pubs[0] ?? ''} onChange={(e) => choosePub(e.target.value)} aria-label={t('Publishers')}>
          <option value="">{t('All publishers')}</option>
          {publishers.map((p) => (
            <option key={p} value={p}>
              {p} ({week?.issues.filter((i) => i.publisher === p).length})
            </option>
          ))}
        </select>
      )}
      {note &&<p className="notice notice-info cx-note">{note}</p>}
      {error && <p className="error-text">{error}</p>}
      {!week && !error && <p className="hint">{t('Asking ComicVine for {from} – {to}… (a week not seen before can take up to half a minute)', { from: nice(from), to: nice(to) })}</p>}
      {week && (
        <p className="hint">
          {week.issues.length === 0
            ? isThisWeek || from > mondayOf('')
              ? t('No comics dated this week yet.')
              : t('ComicVine has no comics dated that week.')
            : t('{shown} of {total} issues.', { shown: shown.length, total: week.issues.length })}
        </p>
      )}
      {week && (isThisWeek || from > mondayOf('')) && week.issues.length < 40 && (
        // Why this week looks thin: comics are dated around their release day.
        <div className="cx-week-info">
          <Icon name="info" size={15} />
          <p>
            {t('New comics come out on Wednesdays in the US (manga magazines through the week in Japan). ComicVine adds each comic around its release day, so early in the week this list is short or empty. It fills in by itself; Mediarium checks again every couple of hours.')}
          </p>
        </div>
      )}
      {week && week.issues.length < 40 && (isThisWeek || from > mondayOf('')) && (
        <button className="btn-sm btn-with-icon" onClick={() => setDate(addDays(from, -7))}>
          <Icon name="chevron-left" size={14} /> {t('Show the week before')}
        </button>
      )}
      {groups.map((g) => (
        <section key={g.name} className="cx-week-row" style={{ ['--pub' as string]: g.color }}>
          <h3 className="cx-week-row-head">
            <span className="cx-week-pub-dot" aria-hidden="true" />
            {g.name} <small className="hint">{g.items.length}</small>
          </h3>
          <Strip id={`${date}:${g.name}`}>
            {g.items.map((i) => (
      <li key={i.cvId}>
        <div className="cx-week-cover">
          {i.imageUrl ? <img src={i.imageUrl} alt="" loading="lazy" /> : <Icon name="book" size={28} />}
          {i.status === 'downloaded' ? (
            <span className="cx-week-badge have">{t('Have')}</span>
          ) : i.wanted ? (
            <span className="cx-week-badge">{i.status === 'downloading' ? t('Downloading') : t('Wanted')}</span>
          ) : null}
        </div>
        <div className="cx-week-text">
          {i.seriesId ? (
            <Link to={`/comics/${i.seriesId}`}>
              {i.series} #{i.number}
            </Link>
          ) : (
            <span>
              {i.series} #{i.number}
            </span>
          )}
          <small className="hint">
            {i.publisher}
            {i.startYear ? ` · ${i.startYear}` : ''}
          </small>
        </div>
        {canPull && i.status !== 'downloaded' && !i.wanted && (
          <button className="btn-sm primary" data-walk="cx-week-pull" disabled={busy === i.cvId} onClick={() => pull(i)} title={i.seriesId ? t('Get this issue') : t('Follow the series and get this issue')}>
            <Icon name="plus" size={13} /> {busy === i.cvId ? t('Pulling…') : t('Pull')}
          </button>
        )}
      </li>
            ))}
          </Strip>
        </section>
      ))}
    </div>
  )
}
