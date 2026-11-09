// Usherr Broadcast: Home (broadcast-plan.md, phase 4a; the mock-up's Home).
// Made for the sofa: what you were watching up top (Resume has the remote's
// first focus), then Tonight (cards that only show when they have something:
// the next showtime, a crew vote, a new manga volume, a game today, and for
// administrators the Server needing them), On now (Live TV), Recently added
// and For you. Everything comes from the APIs the other Homes already read
// (LobbyHome, the Dashboard, the showtime pill, crews, manga, sports). The
// lobby layout keeps LobbyHome; Classic keeps the Dashboard.
import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api, isAdmin, type DashboardData, type DashRecent, type DiscoverMovie } from '../api'
import { useAuth } from '../AuthContext'
import Icon, { type IconName } from '../components/Icon'
import PosterCard from '../components/PosterCard'
import { useNextShowtime } from '../home/ShowtimePill'
import { clockLabel, whenLabel } from '../home/showtimes'
import { crewsApi } from '../home/crewsApi'
import { sportsApi } from '../home/sportsApi'
import { playChannel, playTitle } from '../home/Player'
import type { SurfChannel } from '../home/TVSurf'
import { mangaContinueLink, mangaContinueKey, mangaContinueMeta, useMangaContinue } from '../manga/useMangaContinue'
import { plural, t } from '../i18n'
import { remoteOn } from '../remote'
import { useLive } from '../useLive'
import { gameToday, minutesLeft, newVolume, onNow, openVote, pickHero, resumeTarget, type ContinueLike } from './logic'
import './home.css'

type ContinueItem = ContinueLike & { posterUrl?: string; year?: number }

function recentLink(r: DashRecent): string {
  if (r.kind === 'movie') return `/title/${r.tmdbId}`
  if (r.kind === 'album') return `/music/album/${r.albumId ?? r.id}`
  if (r.kind === 'book') return `/book/${r.bookId ?? r.id}`
  return `/series/${r.seriesId}`
}
const cardKind = (r: DashRecent) => (r.kind === 'movie' ? 'movie' : r.kind === 'album' ? 'music' : r.kind === 'book' ? 'book' : 'tv')

// A list from the server (an empty one when it can't be read), read again every `every` ms when given.
function useList<T>(url: string, every = 0): T[] {
  const [v, setV] = useState<T[]>([])
  const load = useCallback(() => {
    fetch(url, { credentials: 'include', cache: 'no-store' })
      .then((r) => (r.ok ? r.json() : null))
      .then((d) => Array.isArray(d) && setV(d as T[]))
      .catch(() => undefined)
  }, [url])
  useEffect(load, [load])
  useLive(load, every || 60_000, every > 0)
  return v
}

// A row that scrolls sideways, with its heading.
function Row({ title, note, more, children }: { title: string; note?: string; more?: { to: string; label: string }; children: ReactNode }) {
  return (
    <section className="bh-section">
      <div className="bh-head">
        <h2>
          {title}
          {note && <span className="bh-note">{note}</span>}
        </h2>
        {more && (
          <Link to={more.to} className="bh-more">
            {more.label}
            <Icon name="chevron-right" size={16} />
          </Link>
        )}
      </div>
      <div className="bh-row">{children}</div>
    </section>
  )
}

// Resume (or the hero's first button) takes the remote's first focus.
function useFirstFocus(ref: React.RefObject<HTMLElement | null>, ready: boolean) {
  const done = useRef(false)
  useEffect(() => {
    if (!ready || done.current) return
    done.current = true
    const el = ref.current
    const now = document.activeElement
    if (el && remoteOn() && (!now || now === document.body)) el.focus({ preventScroll: true })
  }, [ref, ready])
}

function Hero({ item, fallback }: { item: ContinueItem | null; fallback?: DashRecent }) {
  const first = useRef<HTMLButtonElement & HTMLAnchorElement>(null)
  const navigate = useNavigate()
  useFirstFocus(first, !!(item || fallback))
  if (item) {
    const go = resumeTarget(item)
    const pct = item.duration > 0 ? Math.round((item.position / item.duration) * 100) : 0
    const resume = () => {
      if (!go) return navigate(item.link)
      playTitle(go.kind, go.id, item.episode ? `${item.title} · ${item.episode}` : item.title)
    }
    return (
      <section className="bh-hero" style={item.posterUrl ? { ['--art' as string]: `url("${item.posterUrl}")` } : undefined}>
        <div className="bh-hero-text">
          <span className="bh-eyebrow">{t('Continue watching')}</span>
          <h1 className="bh-hero-title">{item.title}</h1>
          <span className="bh-hero-meta">
            {item.episode ? `${item.episode.split(' · ')[0]} · ` : ''}
            {plural(minutesLeft(item), '{n} min left', '{n} min left')}
          </span>
          <span className="bh-track" aria-hidden="true">
            <span style={{ width: `${pct}%` }} />
          </span>
          <div className="bh-hero-btns">
            <button ref={first} type="button" className="primary btn-with-icon big" data-autofocus="" onClick={resume}>
              <Icon name="play" size={20} /> {t('Resume')}
            </button>
            <Link to={item.link} className="btn btn-with-icon big bh-btn">
              {t('Details')}
            </Link>
          </div>
        </div>
      </section>
    )
  }
  if (!fallback) return null
  return (
    <section className="bh-hero" style={fallback.posterUrl ? { ['--art' as string]: `url("${fallback.posterUrl}")` } : undefined}>
      <div className="bh-hero-text">
        <span className="bh-eyebrow">{t('Just added')}</span>
        <h1 className="bh-hero-title">{fallback.title}</h1>
        {fallback.year ? <span className="bh-hero-meta">{fallback.year}</span> : null}
        <div className="bh-hero-btns">
          <Link ref={first} to={recentLink(fallback)} className="btn primary btn-with-icon big" data-autofocus="">
            <Icon name="play" size={20} /> {t('Open')}
          </Link>
        </div>
      </div>
    </section>
  )
}

type TonightCard = { key: string; icon: IconName; title: string; sub: string; to: string; warn?: boolean }

function TonightCardView({ c }: { c: TonightCard }) {
  return (
    <Link to={c.to} className={`bh-card${c.warn ? ' bh-card-warn' : ''}`}>
      <span className="bh-card-icon" aria-hidden="true">
        <Icon name={c.icon} size={24} />
      </span>
      <span className="bh-card-text">
        <span className="bh-card-title">{c.title}</span>
        <span className="bh-card-sub">{c.sub}</span>
      </span>
    </Link>
  )
}

// Tonight: each card only when it has something to say.
function Tonight({ needs, admin }: { needs: number; admin: boolean }) {
  const { next, now } = useNextShowtime()
  const manga = useMangaContinue()
  const [vote, setVote] = useState<TonightCard | null>(null)
  const [game, setGame] = useState<TonightCard | null>(null)
  useEffect(() => {
    let gone = false
    // the crews you joined, and the first vote still open in one of them
    crewsApi
      .list()
      .then(async (l) => {
        if (!l.on) return
        for (const c of (l.crews ?? []).filter((x) => x.joined).slice(0, 4)) {
          const space = await crewsApi.space(c.id).catch(() => null)
          const v = openVote(space?.votes, Date.now())
          if (v && !gone) {
            setVote({ key: 'vote', icon: 'thumb-up', title: v.myVote ? t('Your crew is voting') : t('Your crew wants your vote'), sub: t('{crew} · closes {time}', { crew: c.name, time: clockLabel(v.closes) }), to: `/crews/${c.id}` })
            return
          }
        }
      })
      .catch(() => undefined)
    fetch('/api/home/sports/on', { credentials: 'include' })
      .then((r) => (r.ok ? r.json() : { on: false }))
      .then((d: { on: boolean }) => (d.on ? sportsApi.home() : null))
      .then((h) => {
        const g = h ? gameToday(h.games ?? [], Date.now()) : null
        if (g && !gone) setGame({ key: 'game', icon: 'trophy', title: `${g.home && g.away ? `${g.home} – ${g.away}` : g.title}`, sub: [clockLabel(Date.parse(g.when)), g.league].filter(Boolean).join(' · '), to: '/sports' })
      })
      .catch(() => undefined)
    return () => {
      gone = true
    }
  }, [])
  const vol = newVolume(manga)
  const cards: TonightCard[] = []
  if (next) {
    const today = new Date(next.atMs).toDateString() === new Date(now).toDateString()
    cards.push({ key: 'showtime', icon: 'clock', title: t('Showtime · {time}', { time: today ? clockLabel(next.atMs) : whenLabel(next.atMs) }), sub: next.title, to: '/showtime' })
  }
  if (vote) cards.push(vote)
  if (vol) cards.push({ key: 'volume', icon: 'book', title: t('New volume'), sub: t('{series} {n} · read now', { series: vol.series, n: vol.number }), to: mangaContinueLink(vol) })
  if (game) cards.push(game)
  if (admin && needs > 0) cards.push({ key: 'needs', icon: 'warning', title: t('The Server needs you'), sub: plural(needs, '{n} thing to look at', '{n} things to look at'), to: '/server', warn: true })
  if (cards.length === 0) return null
  return (
    <section className="bh-section">
      <div className="bh-head">
        <h2>{t('Tonight')}</h2>
      </div>
      <div className="bh-row bh-cards">
        {cards.map((c) => (
          <TonightCardView key={c.key} c={c} />
        ))}
      </div>
    </section>
  )
}

// On now: Live TV channels with something airing; OK tunes in.
function OnNow() {
  const channels = useList<SurfChannel>('/api/tv/channels', 60_000)
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 30_000)
    return () => window.clearInterval(id)
  }, [])
  const list = onNow(
    channels.filter((c) => c.enabled),
    now,
  )
  if (list.length === 0) return null
  return (
    <Row title={t('On now')} note={t('Live TV')} more={{ to: '/tv', label: t('Guide') }}>
      {list.map((c) => (
        <button key={c.id} type="button" className="bh-live" onClick={() => playChannel(c.id, c.name)} aria-label={t('Tune in to {channel}: {title}', { channel: c.name, title: c.now?.title ?? '' })}>
          <span className="bh-live-num">{c.number}</span>
          {c.logoUrl && <img className="bh-live-logo" src={c.logoUrl} alt="" loading="lazy" />}
          <span className="bh-live-text">
            <span className="bh-live-title">{c.now?.title}</span>
            <span className="bh-live-sub">{c.now?.subtitle || c.name}</span>
            <span className="bh-track" aria-hidden="true">
              <span style={{ width: `${c.pct}%` }} />
            </span>
          </span>
        </button>
      ))}
    </Row>
  )
}

export default function BroadcastHome() {
  const { user } = useAuth()
  const admin = isAdmin(user)
  const [data, setData] = useState<DashboardData | null>(null)
  const load = useCallback(() => {
    api
      .dashboard()
      .then(setData)
      .catch(() => undefined)
  }, [])
  useEffect(load, [load])
  useLive(load, 60_000)
  const continuing = useList<ContinueItem>('/api/home/continue')
  const manga = useMangaContinue()
  const [forYou, setForYou] = useState<DiscoverMovie[]>([])
  useEffect(() => {
    api
      .discoverForYou()
      .then((l) => setForYou(Array.isArray(l) ? l.slice(0, 20) : []))
      .catch(() => undefined)
  }, [])

  const hero = pickHero(continuing)
  const rest = continuing.filter((c) => c !== hero)
  const arrived = data ? (data.recentInLibrary ?? data.recentlyAdded.filter((r) => r.state === 'downloaded' || r.state === 'partial')) : []
  const needs = data ? data.health.filter((h) => h.level !== 'info').length : 0
  const left = (c: ContinueItem) => plural(minutesLeft(c), '{n} min left', '{n} min left')
  const empty = data && !hero && arrived.length === 0 && rest.length === 0 && manga.length === 0

  return (
    <div className="bh-home">
      <Hero item={hero} fallback={hero ? undefined : arrived[0]} />
      <Tonight needs={needs} admin={admin} />
      {(rest.length > 0 || manga.length > 0) && (
        <Row title={t('Continue')}>
          {rest.map((c) => (
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
          {manga.map((c) => (
            <PosterCard key={mangaContinueKey(c)} to={mangaContinueLink(c)} kind="book" poster={c.coverUrl} title={c.series} meta={mangaContinueMeta(c)} progress={c.pages > 0 ? { pct: Math.round((c.page / c.pages) * 100) } : undefined} />
          ))}
        </Row>
      )}
      <OnNow />
      {arrived.length > 0 && (
        <Row title={t('Recently added')} more={{ to: '/library', label: t('Library') }}>
          {arrived.map((r) => (
            <PosterCard key={`${r.kind}-${r.id}`} to={recentLink(r)} kind={cardKind(r)} poster={r.posterUrl} title={r.title} meta={r.year ? String(r.year) : undefined} />
          ))}
        </Row>
      )}
      {forYou.length > 0 && (
        <Row title={t('For you')} more={{ to: '/find', label: t('Find') }}>
          {forYou.map((m) => (
            <PosterCard key={m.tmdbId} to={m.mediaType === 'tv' ? `/show/${m.tmdbId}` : `/title/${m.tmdbId}`} kind={m.mediaType === 'tv' ? 'tv' : 'movie'} poster={m.posterUrl} title={m.title} meta={m.year ? String(m.year) : undefined} />
          ))}
        </Row>
      )}
      {empty && (
        <section className="bh-empty">
          <h2>{t('Nothing here yet')}</h2>
          <p>{t('Find a film or show to ask for it. It shows up here once it’s in the house.')}</p>
          <Link to="/find" className="btn primary btn-with-icon big" data-autofocus="">
            <Icon name="search" size={18} /> {t('Find')}
          </Link>
        </section>
      )}
    </div>
  )
}
