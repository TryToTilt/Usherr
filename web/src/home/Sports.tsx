// Usherr: Sports (/sports). The games of the coming two weeks, the leagues in
// the library and the teams followed; search to add a league or follow a
// team. A league's page (/sports/league/:id) lists its games by season.
// Everyone can look; adding, following and getting a game need the
// "manage" permission.
import { useCallback, useEffect, useMemo, useState, type FormEvent } from 'react'
import { Link, useParams } from 'react-router-dom'
import HelpLink from '../components/HelpLink'
import Icon from '../components/Icon'
import { useToast } from '../components/Toast'
import { useAuth } from '../AuthContext'
import { isAdmin } from '../api'
import { useDocumentTitle } from '../documentTitle'
import { plural, t } from '../i18n'
import { gameState, sportsApi, sportsImage, streamGame, type SportGame, type SportLeague, type SportsHome, type SportTeam } from './sportsApi'
import './sports.css'

const dayKey = (iso: string) => new Date(iso).toDateString()
function dayLabel(iso: string): string {
  const d = new Date(iso)
  const today = new Date()
  const tomorrow = new Date(today.getTime() + 86_400_000)
  const yesterday = new Date(today.getTime() - 86_400_000)
  if (d.toDateString() === today.toDateString()) return t('Today')
  if (d.toDateString() === tomorrow.toDateString()) return t('Tomorrow')
  if (d.toDateString() === yesterday.toDateString()) return t('Yesterday')
  return d.toLocaleDateString(undefined, { weekday: 'long', month: 'long', day: 'numeric' })
}
// Scores spoil games watched later: hidden unless asked, remembered on this
// device.
const SCORES_KEY = 'usherr.sports.scores'
function useShowScores(): [boolean, (v: boolean) => void] {
  const [on, setOn] = useState(() => {
    try {
      return localStorage.getItem(SCORES_KEY) === '1'
    } catch {
      return false
    }
  })
  const set = (v: boolean) => {
    setOn(v)
    try {
      localStorage.setItem(SCORES_KEY, v ? '1' : '0')
    } catch {
      // remembering is optional
    }
  }
  return [on, set]
}

function ScoresSwitch({ on, set }: { on: boolean; set: (v: boolean) => void }) {
  return (
    <label className="sp-scores" data-walk="sp-scores">
      <input type="checkbox" checked={on} onChange={(e) => set(e.target.checked)} /> {t('Show scores')}
    </label>
  )
}

const timeLabel = (iso: string) => {
  const d = new Date(iso)
  // Games with no known start time come at midnight UTC.
  if (d.getUTCHours() === 0 && d.getUTCMinutes() === 0) return ''
  return d.toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' })
}

function Badge({ src, alt, size = 40 }: { src?: string; alt: string; size?: number }) {
  return src ? <img className="sp-badge" src={sportsImage(src)} alt={alt} width={size} height={size} loading="lazy" /> : <span className="sp-badge sp-badge-none" style={{ width: size, height: size }} aria-hidden="true" />
}

function GameRow({ g, canGet, canStream, showLeague, scores, onGot }: { g: SportGame; canGet: boolean; canStream: boolean; showLeague: boolean; scores: boolean; onGot: () => void }) {
  const toast = useToast()
  const [busy, setBusy] = useState(false)
  const [streaming, setStreaming] = useState(false)
  const state = gameState(g)
  const score = scores && g.homeScore != null && g.awayScore != null ? `${g.homeScore} – ${g.awayScore}` : ''
  const past = new Date(g.when).getTime() < Date.now()
  return (
    <li className="sp-game">
      {showLeague && <Badge src={g.leagueBadge} alt={g.league ?? ''} size={32} />}
      <div className="sp-game-main">
        <strong>{g.title}</strong>
        <span>{[timeLabel(g.when), showLeague ? g.league : '', g.round && Number(g.round) > 0 && Number(g.round) < 100 ? t('Round {round}', { round: g.round }) : '', g.venue, g.broadcast].filter(Boolean).join(' · ')}</span>
      </div>
      {score && <span className="sp-score">{score}</span>}
      {state && <span className={`badge ${state.cls}`}>{state.label}</span>}
      {canStream && past && (
        <button
          className="btn-sm primary btn-with-icon"
          disabled={streaming}
          title={t('Watch it now, without downloading')}
          onClick={() => {
            setStreaming(true)
            streamGame(g)
              .catch((e: Error) => toast.error(e.message))
              .finally(() => setStreaming(false))
          }}
        >
          <Icon name="play" /> {streaming ? t('Finding…') : t('Stream')}
        </button>
      )}
      {canGet && !g.hasFile && !g.monitored && (
        <button
          className="btn-sm btn-with-icon"
          disabled={busy}
          title={past ? t('Search for this game now') : t('Get this game once it’s out')}
          onClick={() => {
            setBusy(true)
            sportsApi
              .getGame(g.id)
              .then((r) => (toast.success(r.message), onGot()))
              .catch((e: Error) => toast.error(e.message))
              .finally(() => setBusy(false))
          }}
        >
          <Icon name="download" /> {t('Get')}
        </button>
      )}
    </li>
  )
}

function GameDays({ games, canGet, canStream, showLeague, onGot }: { games: SportGame[]; canGet: boolean; canStream: boolean; showLeague: boolean; onGot: () => void }) {
  const [scores, setScores] = useShowScores()
  const days = useMemo(() => {
    const out: { key: string; label: string; games: SportGame[] }[] = []
    for (const g of games) {
      const k = dayKey(g.when)
      if (out.length === 0 || out[out.length - 1].key !== k) out.push({ key: k, label: dayLabel(g.when), games: [] })
      out[out.length - 1].games.push(g)
    }
    return out
  }, [games])
  return (
    <div className="sp-days">
      {games.some((g) => g.homeScore != null) && <ScoresSwitch on={scores} set={setScores} />}
      {days.map((d) => (
        <section key={d.key}>
          <h3>{d.label}</h3>
          <ul className="sp-games" data-walk="sp-games">
            {d.games.map((g) => (
              <GameRow key={g.id} g={g} canGet={canGet} canStream={canStream} showLeague={showLeague} scores={scores} onGot={onGot} />
            ))}
          </ul>
        </section>
      ))}
    </div>
  )
}

// byRelevance puts the closest names first: the name typed, then names
// starting with it, then names with it as a whole word; the rest after.
function byRelevance<T extends { name: string }>(list: T[], q: string): T[] {
  const s = q.trim().toLowerCase()
  const rank = (name: string) => {
    const n = name.toLowerCase()
    if (n === s) return 0
    if (n.startsWith(s)) return 1
    if (` ${n} `.includes(` ${s} `)) return 2
    return 3
  }
  return list.map((x, i) => ({ x, i, r: rank(x.name) })).sort((a, b) => a.r - b.r || a.i - b.i).map((v) => v.x)
}

const FIRST = 12 // results shown before "Show all"

function SearchResults({ q, canManage, canGet, onChange }: { q: string; canManage: boolean; canGet: boolean; onChange: () => void }) {
  const toast = useToast()
  const [res, setRes] = useState<{ leagues: SportLeague[]; teams: SportTeam[] } | null>(null)
  const [all, setAll] = useState(false)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState('')
  const run = useCallback(() => {
    setRes(null)
    setError('')
    sportsApi
      .search(q)
      .then((r) => setRes({ leagues: byRelevance(r.leagues, q), teams: byRelevance(r.teams, q) }))
      .catch((e: Error) => setError(e.message))
  }, [q])
  useEffect(run, [run])
  const act = (key: string, p: Promise<{ message: string }>) => {
    setBusy(key)
    p.then((r) => (toast.success(r.message), run(), onChange()))
      .catch((e: Error) => toast.error(e.message))
      .finally(() => setBusy(''))
  }
  if (error) return <p className="error-text">{error}</p>
  if (!res) return <p className="sp-dim">{t('Searching…')}</p>
  if (!res.leagues.length && !res.teams.length) return <p className="sp-dim">{t('Nothing found for “{q}”.', { q })}</p>
  const more = !all && (res.leagues.length > FIRST || res.teams.length > FIRST)
  return (
    <div className="sp-results">
      {res.leagues.length > 0 && (
        <section className="card">
          <h2>{t('Leagues')}</h2>
          <ul className="sp-list" data-walk="sp-results">
            {(all ? res.leagues : res.leagues.slice(0, FIRST)).map((l) => (
              <li key={l.externalId}>
                <Badge src={l.badge} alt={l.name} />
                <div className="sp-game-main">
                  <strong>{l.name}</strong>
                  <span>{[l.sport, l.country].filter(Boolean).join(' · ')}</span>
                </div>
                {l.inLibrary && l.id ? (
                  <Link className="btn-sm btn-with-icon" to={`/sports/league/${l.id}`}>
                    {t('Open')}
                  </Link>
                ) : (
                  canManage && (
                    <span className="sp-btns">
                      <button className="btn-sm btn-with-icon" disabled={busy !== ''} onClick={() => act('l' + l.externalId, sportsApi.addLeague(l, false))}>
                        <Icon name="plus" /> {t('Add')}
                      </button>
                      {canGet && (
                        <button className="btn-sm primary btn-with-icon" disabled={busy !== ''} title={t('Add it and get its games from now on')} onClick={() => act('l' + l.externalId, sportsApi.addLeague(l, true))}>
                          <Icon name="download" /> {t('Add and get new games')}
                        </button>
                      )}
                    </span>
                  )
                )}
              </li>
            ))}
          </ul>
        </section>
      )}
      {res.teams.length > 0 && (
        <section className="card">
          <h2>{t('Teams')}</h2>
          <ul className="sp-list" data-walk="sp-results">
            {(all ? res.teams : res.teams.slice(0, FIRST)).map((tm) => (
              <li key={tm.externalId}>
                <Badge src={tm.badge} alt={tm.name} />
                <div className="sp-game-main">
                  <strong>{tm.name}</strong>
                  <span>{[tm.sport, tm.country].filter(Boolean).join(' · ')}</span>
                </div>
                {tm.followed ? (
                  <span className="badge downloaded">{t('Following')}</span>
                ) : (
                  canManage && (
                    <button className="btn-sm btn-with-icon" disabled={busy !== ''} onClick={() => act('t' + tm.externalId, sportsApi.followTeam(tm))}>
                      <Icon name="heart" /> {t('Follow')}
                    </button>
                  )
                )}
              </li>
            ))}
          </ul>
        </section>
      )}
      {more && (
        <button className="sp-more" onClick={() => setAll(true)}>
          {t('Show all {n} results', { n: res.leagues.length + res.teams.length })}
        </button>
      )}
    </div>
  )
}

function NotOn({ home }: { home: SportsHome }) {
  const admin = isAdmin(useAuth().user)
  return (
    <section className="card sp-off">
      <Icon name="trophy" />
      <h2>{t('Sports isn’t set up yet')}</h2>
      {home.problem && <p className="error-text">{home.problem}</p>}
      <p className="sp-dim">{t('Sports shows leagues, teams and games, and streams or gets the games you want. It runs on Sportarr or on Usherr’s own engine.')}</p>
      {admin ? (
        <Link className="btn-with-icon" to="/settings/extras#sports">
          {t('Set up Sports')}
        </Link>
      ) : (
        <p className="sp-dim">{t('Ask an administrator to set it up.')}</p>
      )}
    </section>
  )
}

export function SportsLeaguePage() {
  const { id = '' } = useParams()
  const [d, setD] = useState<Awaited<ReturnType<typeof sportsApi.league>> | null>(null)
  const [error, setError] = useState('')
  const [season, setSeason] = useState('') // '' = the newest
  const [when, setWhen] = useState<'next' | 'past' | 'all'>('next')
  useDocumentTitle(d?.league.name ?? t('Sports'))
  const load = useCallback(() => {
    sportsApi
      .league(id, season)
      .then((r) => {
        setD(r)
        setError('')
      })
      .catch((e: Error) => setError(e.message))
  }, [id, season])
  useEffect(load, [load])
  const { shown, upcoming } = useMemo(() => {
    const now = Date.now() - 3 * 3600_000 // a game that started in the last three hours is still "coming up"
    const games = d?.games ?? []
    const next = games.filter((g) => new Date(g.when).getTime() >= now)
    const view = when === 'next' && next.length === 0 ? 'past' : when
    if (view === 'next') return { shown: next.slice(0, 200), upcoming: next.length }
    if (view === 'past') return { shown: games.filter((g) => new Date(g.when).getTime() < now).reverse().slice(0, 200), upcoming: next.length }
    return { shown: games, upcoming: next.length }
  }, [d, when])
  if (error) return <p className="error-text">{error}</p>
  if (!d) return <p>{t('Loading…')}</p>
  const l = d.league
  return (
    <div className="sports-page">
      <header className="sp-league-head" style={l.banner ? { backgroundImage: `linear-gradient(90deg, var(--bg) 25%, transparent), url("${sportsImage(l.banner)}")` } : undefined}>
        <Link to="/sports" className="btn-sm btn-with-icon">
          <Icon name="chevron-left" /> {t('Sports')}
        </Link>
        <div className="sp-league-title">
          <Badge src={l.badge} alt={l.name} size={72} />
          <div>
            <h1>{l.name}</h1>
            <p className="sp-dim">
              {[l.sport, l.country, plural(l.files ?? 0, '{n} game downloaded', '{n} games downloaded'), l.monitored ? t('getting new games') : ''].filter(Boolean).join(' · ')}
            </p>
          </div>
        </div>
      </header>
      <div className="sp-filters">
        {d.seasons.length > 0 && (
          <select value={season || d.season} onChange={(e) => setSeason(e.target.value)} aria-label={t('Season')}>
            {d.seasons.map((s) => (
              <option key={s} value={s}>
                {s}
              </option>
            ))}
          </select>
        )}
        <div className="seg">
          <button className={when === 'next' && upcoming > 0 ? 'active' : ''} disabled={upcoming === 0} onClick={() => setWhen('next')}>
            {t('Coming up')}
          </button>
          <button className={when === 'past' || (when === 'next' && upcoming === 0) ? 'active' : ''} onClick={() => setWhen('past')}>
            {t('Played')}
          </button>
          <button className={when === 'all' ? 'active' : ''} onClick={() => setWhen('all')}>
            {t('Whole season')}
          </button>
        </div>
      </div>
      {shown.length ? <GameDays games={shown} canGet={d.canGet} canStream={d.canStream} showLeague={false} onGot={load} /> : <p className="sp-dim">{t('No games here yet. New leagues take a minute to fill.')}</p>}
    </div>
  )
}

export default function SportsPage() {
  useDocumentTitle(t('Sports'))
  const toast = useToast()
  const [home, setHome] = useState<SportsHome | null>(null)
  const [error, setError] = useState('')
  const [text, setText] = useState('')
  const [q, setQ] = useState('')
  const load = useCallback(() => {
    sportsApi
      .home()
      .then((h) => {
        setHome(h)
        setError('')
      })
      .catch((e: Error) => setError(e.message))
  }, [])
  useEffect(load, [load])
  if (error) return <p className="error-text">{error}</p>
  if (!home) return <p>{t('Loading…')}</p>
  if (!home.on) return <NotOn home={home} />
  const submit = (e: FormEvent) => {
    e.preventDefault()
    setQ(text.trim().length >= 2 ? text.trim() : '')
  }
  return (
    <div className="sports-page">
      <div className="sp-top">
        <h1>
          {t('Sports')} <HelpLink topic="home-sports" />
        </h1>
        <form className="sp-search" data-walk="sp-search" onSubmit={submit} role="search">
          <input value={text} onChange={(e) => setText(e.target.value)} placeholder={t('Find a league or team…')} aria-label={t('Find a league or team')} />
          <button type="submit" className="btn-sm">
            <Icon name="search" />
          </button>
          {q && (
            <button type="button" className="btn-sm" onClick={() => (setQ(''), setText(''))}>
              {t('Close')}
            </button>
          )}
        </form>
      </div>
      {home.problem && <p className="notice notice-warn">{home.problem}</p>}
      {q && <SearchResults q={q} canManage={home.canManage} canGet={home.canGet} onChange={load} />}

      {home.leagues.length > 0 && (
        <section>
          <h2>{t('Leagues')}</h2>
          <div className="sp-leagues">
            {home.leagues.map((l) => (
              <Link key={l.id} to={`/sports/league/${l.id}`} className="sp-league">
                <Badge src={l.badge} alt={l.name} size={56} />
                <strong>{l.name}</strong>
                <span>{plural(l.files ?? 0, '{n} downloaded', '{n} downloaded')}</span>
              </Link>
            ))}
          </div>
        </section>
      )}

      {home.teams.length > 0 && (
        <section>
          <h2>{t('Teams you follow')}</h2>
          <ul className="sp-teams">
            {home.teams.map((tm) => (
              <li key={tm.id}>
                <Badge src={tm.badge} alt={tm.name} size={28} />
                <span>{tm.name}</span>
                {home.canManage && tm.id && (
                  <button
                    className="icon-btn"
                    title={t('Stop following (its games stay)')}
                    aria-label={t('Stop following {name}', { name: tm.name })}
                    onClick={() =>
                      sportsApi
                        .unfollowTeam(tm.id!)
                        .then(load)
                        .catch((e: Error) => toast.error(e.message))
                    }
                  >
                    <Icon name="x" />
                  </button>
                )}
              </li>
            ))}
          </ul>
        </section>
      )}

      <section>
        <h2>{t('Games')}</h2>
        {home.games.length ? (
          <GameDays games={home.games} canGet={home.canGet} canStream={home.canStream} showLeague onGot={load} />
        ) : (
          <p className="sp-dim">{home.leagues.length ? t('No games in the next two weeks.') : t('Find a league or team above to begin.')}</p>
        )}
      </section>
    </div>
  )
}
