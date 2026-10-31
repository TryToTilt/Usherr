// Home fork: Wrapped, on the You page. Your year in watching, from your
// Diary: how much and when, the shows you couldn't stop, the films you rated
// best, and what your year was made of.
import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import Icon from '../components/Icon'
import { currentLanguage, plural, t } from '../i18n'
import Stars from './Stars'
import './news.css'

interface Title {
  kind: 'movie' | 'tv'
  tmdbId: number
  title: string
  image?: string
  episodes?: number
  stars?: number
  day?: string
  times?: number
}
interface Count {
  name: string
  count: number
}
interface Year {
  year: number
  years: number[]
  films: number
  newFilms: number
  rewatches: number
  episodes: number
  shows: number
  minutes: number
  about: boolean
  days: number
  streak: number
  months: number[]
  weekday: number
  topShows: Title[]
  topRated: Title[]
  mostSeen: Title | null
  first: Title | null
  last: Title | null
  avgStars: number
  genres: Count[]
  people: Count[]
  subjects: Count[]
  empty: boolean
  fromServer: boolean
}

const to = (x: Title) => (x.kind === 'tv' ? `/show/${x.tmdbId}` : `/title/${x.tmdbId}`)

function Poster({ x, children }: { x: Title; children?: React.ReactNode }) {
  return (
    <Link to={to(x)} className="wrapped-poster">
      {x.image ? (
        <img src={x.image} alt="" loading="lazy" />
      ) : (
        <span className="you-noimage">
          <Icon name={x.kind === 'tv' ? 'tv' : 'film'} size={22} />
        </span>
      )}
      <span className="you-vote-title">{x.title}</span>
      {children}
    </Link>
  )
}

function Tile({ n, label }: { n: string | number; label: string }) {
  return (
    <div className="wrapped-tile">
      <strong>{n}</strong>
      <span>{label}</span>
    </div>
  )
}

export default function Wrapped() {
  const [year, setYear] = useState(0)
  const [data, setData] = useState<Year | null>(null)
  const [error, setError] = useState('')
  useEffect(() => {
    setData(null)
    fetch(`/api/home/wrapped${year ? `?year=${year}` : ''}`, { credentials: 'include' })
      .then(async (r) => {
        const d = await r.json().catch(() => ({}))
        if (!r.ok) throw new Error((d as { error?: string }).error || r.statusText)
        return d as Year
      })
      .then(setData)
      .catch((e: Error) => setError(e.message))
  }, [year])

  if (error) return <p className="error-text">{error}</p>
  if (!data) return <p className="hint">{t('Adding up your year…')}</p>

  const hours = Math.round(data.minutes / 60)
  const peak = Math.max(1, ...data.months)
  const lang = currentLanguage()
  const monthName = (i: number) => new Date(2000, i, 1).toLocaleDateString(lang, { month: 'short' })
  const weekdayName = new Date(2000, 0, 2 + data.weekday).toLocaleDateString(lang, { weekday: 'long' }) // 2 Jan 2000 was a Sunday
  const busiest = data.months.indexOf(Math.max(...data.months))

  return (
    <div className="wrapped">
      {data.years.length > 1 && (
        <div className="room-row" role="group" aria-label={t('Year')}>
          {data.years.map((y) => (
            <button key={y} type="button" className={`room-chip${y === data.year ? ' on' : ''}`} aria-pressed={y === data.year} onClick={() => setYear(y)}>
              {y}
            </button>
          ))}
        </div>
      )}
      {data.empty ? (
        <section className="card you-card diary-empty">
          <Icon name="sparkles" size={32} />
          <p>
            {data.fromServer
              ? t('Nothing in your Diary for {year} yet. What you finish in Jellyfin counts by itself.', { year: data.year })
              : t('Nothing in your Diary for {year} yet. Log what you watch, or import your history, on the Diary tab.', { year: data.year })}
          </p>
        </section>
      ) : (
        <>
          <section className="card wrapped-hero" data-walk="wrapped-hero">
            <span className="wrapped-year">{data.year}</span>
            <h2>{data.about ? t('About {n} hours of watching', { n: hours.toLocaleString(lang) }) : plural(hours, 'One hour of watching', '{n} hours of watching')}</h2>
            <p>
              {plural(data.films, 'One film', '{n} films')} · {plural(data.episodes, 'one episode', '{n} episodes')} · {plural(data.shows, 'one show', '{n} shows')}
            </p>
          </section>

          <div className="wrapped-tiles">
            <Tile n={data.newFilms} label={t('films new to you')} />
            <Tile n={data.rewatches} label={t('films watched again')} />
            <Tile n={data.days} label={t('days with something on')} />
            <Tile n={data.streak} label={t('days in a row, at most')} />
            <Tile n={weekdayName} label={t('your busiest day of the week')} />
            {data.avgStars > 0 && <Tile n={(data.avgStars / 2).toLocaleString(lang, { maximumFractionDigits: 1 })} label={t('stars, on average')} />}
          </div>

          <section className="card you-card">
            <h2>{t('Month by month')}</h2>
            <p className="hint">{t('{month} was the busiest.', { month: new Date(2000, busiest, 1).toLocaleDateString(lang, { month: 'long' }) })}</p>
            <div className="wrapped-months" role="img" aria-label={t('Hours watched each month')}>
              {data.months.map((m, i) => (
                <div key={i} className="wrapped-month" title={plural(Math.round(m / 60), 'One hour', '{n} hours')}>
                  <span style={{ height: `${Math.round((m / peak) * 100)}%` }} />
                  <small>{monthName(i)}</small>
                </div>
              ))}
            </div>
          </section>

          {(data.genres.length > 0 || data.people.length > 0) && (
            <section className="card you-card">
              <h2>{t('Your year was made of')}</h2>
              {[
                { label: t('Genres'), list: data.genres },
                { label: t('People'), list: data.people },
                { label: t('Subjects'), list: data.subjects },
              ]
                .filter((g) => g.list.length > 0)
                .map((g) => (
                  <div key={g.label} className="you-group">
                    <span className="room-label">{g.label}</span>
                    <div className="chip-row">
                      {g.list.map((c) => (
                        <span key={c.name} className="chip">
                          {c.name}
                        </span>
                      ))}
                    </div>
                  </div>
                ))}
            </section>
          )}

          <div className="you-grid">
            {data.topShows.length > 0 && (
              <section className="card you-card">
                <h2>{t('The shows you couldn’t stop')}</h2>
                <div className="wrapped-posters">
                  {data.topShows.map((x) => (
                    <Poster key={x.tmdbId} x={x}>
                      <span className="hint">{plural(x.episodes ?? 0, 'One episode', '{n} episodes')}</span>
                    </Poster>
                  ))}
                </div>
              </section>
            )}
            {data.topRated.length > 0 && (
              <section className="card you-card">
                <h2>{t('Your best-rated films')}</h2>
                <div className="wrapped-posters">
                  {data.topRated.map((x) => (
                    <Poster key={x.tmdbId} x={x}>
                      <Stars value={x.stars ?? 0} size={12} />
                    </Poster>
                  ))}
                </div>
              </section>
            )}
          </div>

          {(data.first || data.mostSeen) && (
            <div className="wrapped-tiles wrapped-moments">
              {data.first && (
                <div className="wrapped-moment">
                  <span className="room-label">{t('The year started with')}</span>
                  <Poster x={data.first} />
                </div>
              )}
              {data.mostSeen && (
                <div className="wrapped-moment">
                  <span className="room-label">{t('Watched most')}</span>
                  <Poster x={data.mostSeen}>
                    <span className="hint">{plural(data.mostSeen.times ?? 0, 'once', '{n} times')}</span>
                  </Poster>
                </div>
              )}
              {data.last && data.last.tmdbId !== data.first?.tmdbId && (
                <div className="wrapped-moment">
                  <span className="room-label">{t('Most recently')}</span>
                  <Poster x={data.last} />
                </div>
              )}
            </div>
          )}
          {data.about && <p className="hint">{t('Running times Mediarium doesn’t know are counted as a typical film or episode.')}</p>}
        </>
      )}
    </div>
  )
}
