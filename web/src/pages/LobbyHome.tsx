import { useCallback, useEffect, useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { api, displayName, isAdmin, type CalendarEntry, type DashboardData, type DashRecent } from '../api'
import { useAuth } from '../AuthContext'
import Icon from '../components/Icon'
import Loading from '../components/Loading'
import PosterCard from '../components/PosterCard'
import { mangaContinueKey, mangaContinueLink, mangaContinueMeta, useMangaContinue } from '../manga/useMangaContinue'
import { useNextShowtime } from '../home/ShowtimePill'
import { inLabel, whenLabel } from '../home/showtimes'
import { locale, plural, t } from '../i18n'
import { useLive } from '../useLive'
import '../neu/lobbyHome.css'

// Usherr: Home in the new layout (the "lobby" look). It is for watching: the
// next showtime, what you stopped part way through, what just arrived and
// what's on its way. No download counts and no setup warnings here; an
// administrator gets one line that says how many things on the Server need
// them. The old dashboard is still there for them, under Server.

type ContinueItem = { item: string; kind: 'movie' | 'tv'; title: string; episode?: string; posterUrl?: string; link: string; position: number; duration: number }

function recentLink(r: DashRecent): string {
  if (r.kind === 'movie') return `/title/${r.tmdbId}`
  if (r.kind === 'album') return `/music/album/${r.albumId ?? r.id}`
  if (r.kind === 'book') return `/book/${r.bookId ?? r.id}`
  return `/series/${r.seriesId}`
}
const cardKind = (r: DashRecent) => (r.kind === 'movie' ? 'movie' : r.kind === 'album' ? 'music' : r.kind === 'book' ? 'book' : 'tv')

function greeting(): string {
  const h = new Date().getHours()
  return h < 5 ? t('Good evening') : h < 12 ? t('Good morning') : h < 17 ? t('Good afternoon') : t('Good evening')
}

// A row of posters that scrolls sideways, with its heading and a "See all".
function Row({ title, more, children }: { title: string; more?: { to: string; label: string }; children: ReactNode }) {
  return (
    <section className="lobby-section">
      <div className="lobby-section-head">
        <h2>{title}</h2>
        {more && (
          <Link to={more.to} className="lobby-more">
            {more.label}
            <Icon name="chevron-right" size={15} />
          </Link>
        )}
      </div>
      <div className="lobby-row">{children}</div>
    </section>
  )
}

function NextShowtime({ fallback }: { fallback?: DashRecent }) {
  const { next, now } = useNextShowtime()
  if (next) {
    const art = next.posterUrl ? { ['--art' as string]: `url("${next.posterUrl}")` } : undefined
    return (
      <section className="lobby-hero" style={art}>
        {next.posterUrl && <img className="lobby-hero-poster" src={next.posterUrl} alt="" />}
        <div className="lobby-hero-text">
          <span className="lobby-eyebrow">
            {t('Next showtime')} · {whenLabel(next.atMs)} · {inLabel(next.atMs, now)}
          </span>
          <h2 className="lobby-hero-title">{next.title}</h2>
          <p>
            {next.mine ? t('You booked it.') : t('Someone else in the house booked it.')}{' '}
            {next.preshowMin > 0 ? plural(next.preshowMin, 'Short trailers play for {n} minute first, then the film starts by itself.', 'Short trailers play for {n} minutes first, then the film starts by itself.') : t('The film starts by itself at showtime.')}
          </p>
          <div className="lobby-hero-btns">
            <Link to="/showtime" className="lobby-btn primary">
              <Icon name="clock" size={16} /> {t('Movie night')}
            </Link>
            <Link to={`/title/${next.tmdbId}`} className="lobby-btn">
              {t('About the film')}
            </Link>
          </div>
        </div>
      </section>
    )
  }
  if (!fallback) return null
  const art = fallback.posterUrl ? { ['--art' as string]: `url("${fallback.posterUrl}")` } : undefined
  return (
    <section className="lobby-hero" style={art}>
      {fallback.posterUrl && <img className="lobby-hero-poster" src={fallback.posterUrl} alt="" />}
      <div className="lobby-hero-text">
        <span className="lobby-eyebrow">{t('Just added')}</span>
        <h2 className="lobby-hero-title">{fallback.title}</h2>
        <p>{fallback.year ? `${fallback.year} · ` : ''}{t('New in the house. Open it to play, or book it for a movie night.')}</p>
        <div className="lobby-hero-btns">
          <Link to={recentLink(fallback)} className="lobby-btn primary">
            <Icon name="play" size={16} /> {t('Open')}
          </Link>
          <Link to="/showtime" className="lobby-btn">
            {t('Plan a movie night')}
          </Link>
        </div>
      </div>
    </section>
  )
}

function ContinueRow() {
  const [items, setItems] = useState<ContinueItem[]>([])
  useEffect(() => {
    fetch('/api/home/continue', { credentials: 'include', cache: 'no-store' })
      .then((r) => (r.ok ? r.json() : []))
      .then((x) => setItems(Array.isArray(x) ? (x as ContinueItem[]) : []))
      .catch(() => undefined)
  }, [])
  if (items.length === 0) return null
  const left = (c: ContinueItem) => plural(Math.max(1, Math.round((c.duration - c.position) / 60)), '{n} min left', '{n} min left')
  return (
    <Row title={t('Continue watching')}>
      {items.map((c) => (
        <PosterCard
          key={c.item}
          to={c.link}
          kind={c.kind}
          poster={c.posterUrl}
          title={c.title}
          meta={c.episode ? `${c.episode.split(' · ')[0]} · ${left(c)}` : left(c)}
          progress={c.duration > 0 ? { pct: Math.round((c.position / c.duration) * 100) } : undefined}
        />
      ))}
    </Row>
  )
}

// Usherr: where you got to in your manga.
function MangaContinueRow() {
  const items = useMangaContinue()
  if (items.length === 0) return null
  return (
    <Row title={t('Continue reading')} more={{ to: '/manga', label: t('Manga') }}>
      {items.map((c) => (
        <PosterCard
          key={mangaContinueKey(c)}
          to={mangaContinueLink(c)}
          kind="book"
          poster={c.coverUrl}
          title={c.series}
          meta={mangaContinueMeta(c)}
          progress={c.pages > 0 ? { pct: Math.round((c.page / c.pages) * 100) } : undefined}
        />
      ))}
    </Row>
  )
}

function dayOf(e: CalendarEntry): string {
  const d = new Date(e.airAt ?? `${e.releaseDate}T12:00:00`)
  return Number.isNaN(d.getTime()) ? '' : d.toLocaleDateString(locale(), { weekday: 'short', month: 'short', day: 'numeric' })
}

export default function LobbyHome() {
  const { user } = useAuth()
  const admin = isAdmin(user)
  const [data, setData] = useState<DashboardData | null>(null)
  const [error, setError] = useState('')
  const load = useCallback(() => {
    api
      .dashboard()
      .then((d) => {
        setData(d)
        setError('')
      })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
  }, [])
  useEffect(load, [load])
  useLive(load, 60_000)

  if (error && !data) return <p className="error-text">{error}</p>
  if (!data) {
    return (
      <Loading height={260}>
        <div className="skeleton" style={{ height: 260, borderRadius: 26 }} />
      </Loading>
    )
  }
  const arrived = data.recentInLibrary ?? data.recentlyAdded.filter((r) => r.state === 'downloaded' || r.state === 'partial')
  const coming = data.recentToCome ?? []
  const needs = data.health.filter((h) => h.level !== 'info').length
  const upcoming = data.upcoming.slice(0, 8)
  return (
    <div className="lobby-home">
      <h1 className="lobby-greeting">
        {greeting()}, {displayName(user)}
      </h1>
      {admin && needs > 0 && (
        <Link to="/server" className="lobby-needs">
          <Icon name="warning" size={16} />
          {plural(needs, 'Server: {n} thing needs you', 'Server: {n} things need you')}
          <Icon name="chevron-right" size={15} />
        </Link>
      )}
      <NextShowtime fallback={arrived[0]} />
      <ContinueRow />
      <MangaContinueRow />
      {arrived.length > 0 && (
        <Row title={t('Just added')} more={{ to: '/library', label: t('Library') }}>
          {arrived.map((r) => (
            <PosterCard key={`${r.kind}-${r.id}`} to={recentLink(r)} kind={cardKind(r)} poster={r.posterUrl} title={r.title} meta={r.year ? String(r.year) : undefined} />
          ))}
        </Row>
      )}
      {coming.length > 0 && (
        <Row title={t('Coming soon')}>
          {coming.map((r) => (
            <PosterCard key={`${r.kind}-${r.id}`} to={recentLink(r)} kind={cardKind(r)} poster={r.posterUrl} title={r.title} meta={r.state === 'downloading' ? t('On its way') : t('Coming soon')} />
          ))}
        </Row>
      )}
      {upcoming.length > 0 && (
        <section className="lobby-section">
          <div className="lobby-section-head">
            <h2>{t('New episodes and films this week')}</h2>
            <Link to="/calendar" className="lobby-more">
              {t('Coming up')}
              <Icon name="chevron-right" size={15} />
            </Link>
          </div>
          <ul className="lobby-list">
            {upcoming.map((e) => (
              <li key={`${e.kind}-${e.id}`}>
                <Link to={e.kind === 'movie' ? `/title/${e.tmdbId}` : e.seriesId ? `/series/${e.seriesId}` : '/calendar'}>
                  <span className="lobby-list-title">{e.title}</span>
                  {e.subtitle && <span className="lobby-list-sub">{e.subtitle}</span>}
                  <span className="lobby-list-day">{dayOf(e)}</span>
                </Link>
              </li>
            ))}
          </ul>
        </section>
      )}
      {arrived.length === 0 && coming.length === 0 && (
        <section className="lobby-empty">
          <h2>{t('Nothing here yet')}</h2>
          <p>{t('Search for a film or show to ask for it. It shows up here once it’s in the house.')}</p>
          <Link to="/search" className="lobby-btn primary">
            <Icon name="search" size={16} /> {t('Search')}
          </Link>
        </section>
      )}
    </div>
  )
}
