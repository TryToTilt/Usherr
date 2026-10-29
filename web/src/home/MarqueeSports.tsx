// Usherr: Sports on the Marquee — the live scoreboard and tonight's games
// (scenes), the score ticker along the bottom and the kick-off countdown
// band. A screen that doesn't show scores gets none from the server.
import { locale, t } from '../i18n'
import type { Data, MarqueeGame, Playing } from './marqueeData'
import { sportsImage } from './sportsApi'
import './marqueeSports.css'

const clockOf = (iso: string) => new Date(iso).toLocaleTimeString(locale(), { hour: 'numeric', minute: '2-digit' })

// A game with no known start comes at midnight UTC.
const timeKnown = (iso: string) => {
  const d = new Date(iso)
  return !(d.getUTCHours() === 0 && d.getUTCMinutes() === 0)
}

function Crest({ src, name }: { src?: string; name: string }) {
  return src ? <img className="mqs-crest" src={sportsImage(src)} alt="" /> : <span className="mqs-crest mqs-crest-none">{name.slice(0, 1)}</span>
}

// sides are a game's two teams, or its title when it has none (a race, a card).
function sides(g: MarqueeGame): [string, string] | null {
  return g.home && g.away ? [g.home, g.away] : null
}

const scoreOf = (g: MarqueeGame) => (g.homeScore != null && g.awayScore != null ? `${g.homeScore} – ${g.awayScore}` : '')

// ---- The live scoreboard: games under way, then the day's results. ----
export function ScoresView({ data, header }: { data: Data; header: boolean }) {
  const games = (data.sports?.games ?? []).filter((g) => g.state === 'live' || (data.sports?.scores && g.state === 'final')).slice(0, 8)
  return (
    <div className="lk lk-x mqs">
      {header && <h2 className="mqs-head">{t('Scoreboard')}</h2>}
      <ul className="mqs-board">
        {games.map((g) => {
          const two = sides(g)
          return (
            <li key={g.id} className={`mqs-game ${g.state}${g.followed ? ' mine' : ''}`}>
              <span className="mqs-league">{g.leagueBadge ? <img src={sportsImage(g.leagueBadge)} alt="" /> : null}</span>
              {two ? (
                <span className="mqs-teams">
                  <span className="mqs-team">
                    <Crest src={g.homeBadge} name={two[0]} />
                    <b>{two[0]}</b>
                  </span>
                  <span className="mqs-team">
                    <Crest src={g.awayBadge} name={two[1]} />
                    <b>{two[1]}</b>
                  </span>
                </span>
              ) : (
                <span className="mqs-teams">
                  <b>{g.title}</b>
                </span>
              )}
              <span className="mqs-score">
                {scoreOf(g) && <strong>{scoreOf(g)}</strong>}
                <small>{g.state === 'final' ? t('Final') : g.clock || t('Live')}</small>
              </span>
            </li>
          )
        })}
      </ul>
    </div>
  )
}

// ---- Tonight's games: the day's schedule with kick-off times and channels. ----
export function SportsTonightView({ data, header }: { data: Data; header: boolean }) {
  const today = new Date().toDateString()
  const games = (data.sports?.games ?? []).filter((g) => g.state === 'live' || (g.state === 'upcoming' && new Date(g.when).toDateString() === today)).sort((a, b) => +new Date(a.when) - +new Date(b.when)).slice(0, 10)
  return (
    <div className="lk lk-x mqs">
      {header && <h2 className="mqs-head">{t('Tonight’s games')}</h2>}
      <ul className="mqs-board">
        {games.map((g) => {
          const two = sides(g)
          return (
            <li key={g.id} className={`mqs-game ${g.state}${g.followed ? ' mine' : ''}`}>
              <span className="mqs-time">{g.state === 'live' ? <b className="mqs-live">{t('Live')}</b> : timeKnown(g.when) ? clockOf(g.when) : t('Today')}</span>
              <span className="mqs-teams">
                {two ? (
                  <>
                    <span className="mqs-team">
                      <Crest src={g.homeBadge} name={two[0]} />
                      <b>{two[0]}</b>
                    </span>
                    <span className="mqs-vs">{t('vs')}</span>
                    <span className="mqs-team">
                      <Crest src={g.awayBadge} name={two[1]} />
                      <b>{two[1]}</b>
                    </span>
                  </>
                ) : (
                  <b>{g.title}</b>
                )}
              </span>
              <span className="mqs-where">
                {g.league && <small>{g.league}</small>}
                {g.channel && <small>{g.channel}</small>}
              </span>
            </li>
          )
        })}
      </ul>
    </div>
  )
}

// hasScores: the scoreboard has something to show.
export const hasScores = (d: Data) => (d.sports?.games ?? []).some((g) => g.state === 'live' || (d.sports?.scores && g.state === 'final'))

// hasTonight: there are games left today (or under way).
export const hasTonight = (d: Data) => {
  const today = new Date().toDateString()
  return (d.sports?.games ?? []).some((g) => g.state === 'live' || (g.state === 'upcoming' && new Date(g.when).toDateString() === today))
}

// ---- The ticker: the day's games along the bottom, like a sports channel. ----
export function SportsTicker({ data }: { data: Data }) {
  const games = (data.sports?.games ?? []).slice(0, 24)
  if (!games.length) return null
  const line = games.map((g) => {
    const name = g.home && g.away ? `${g.home} ${scoreOf(g) || t('vs')} ${g.away}` : g.title
    const when = g.state === 'live' ? g.clock || t('Live') : g.state === 'final' ? t('Final') : timeKnown(g.when) ? clockOf(g.when) : ''
    return { key: g.id, league: g.league, name, when, live: g.state === 'live', mine: g.followed }
  })
  const items = (copy: number) =>
    line.map((x) => (
      <span key={`${copy}-${x.key}`} className={`mqs-tick${x.live ? ' live' : ''}${x.mine ? ' mine' : ''}`}>
        {x.league && <em>{x.league}</em>}
        <b>{x.name}</b>
        {x.when && <small>{x.when}</small>}
      </span>
    ))
  // Two copies: the line scrolls round without a gap. Its speed follows its length.
  return (
    <div className="mqs-ticker" role="marquee" aria-label={t('Sports scores')}>
      <div className="mqs-ticker-run" style={{ animationDuration: `${Math.max(30, line.length * 7)}s` }}>
        {items(0)}
        {items(1)}
      </div>
    </div>
  )
}

// ---- The countdown: the next kick-off in the next three hours, a followed team's first. ----
export function SportsCountdown({ data, now }: { data: Data; now: number }) {
  const soon = (data.sports?.games ?? []).filter((g) => g.state === 'upcoming' && timeKnown(g.when) && +new Date(g.when) - now <= 3 * 3600_000 && +new Date(g.when) > now - 60_000)
  if (!soon.length) return null
  const next = soon.find((g) => g.followed) ?? soon.sort((a, b) => +new Date(a.when) - +new Date(b.when))[0]
  const mins = Math.max(0, Math.round((+new Date(next.when) - now) / 60_000))
  const name = next.home && next.away ? `${next.home} ${t('vs')} ${next.away}` : next.title
  return (
    <div className={`mq-showtime-band mqs-countdown${mins <= 10 ? ' soon' : ''}`} role="status">
      <b>{t('Kick-off {time}', { time: clockOf(next.when) })}</b>
      <span>{name}</span>
      <span>{mins <= 0 ? t('starting now') : mins < 60 ? t('in {n} min', { n: mins }) : t('in {h} h {m} min', { h: Math.floor(mins / 60), m: mins % 60 })}</span>
      {next.channel && <span>{next.channel}</span>}
    </div>
  )
}

// ---- Now showing: the game being watched, as a matchup card. ----
export function GameNow({ p, g }: { p: Playing; g: MarqueeGame }) {
  const two = sides(g)
  const score = scoreOf(g)
  const facts = [g.round ? t('Round {round}', { round: g.round }) : '', g.venue, g.channel].filter(Boolean)
  return (
    <div className="mq-scene mqs-now">
      {g.image && <img className="mqs-now-bg" src={sportsImage(g.image)} alt="" />}
      <div className="mqs-now-card">
        {g.leagueBadge ? <img className="mqs-now-league" src={sportsImage(g.leagueBadge)} alt={g.league ?? ''} /> : g.league && <span className="mqs-now-leaguename">{g.league}</span>}
        {two ? (
          <div className="mqs-now-match">
            <div className="mqs-now-team">
              <Crest src={g.homeBadge} name={two[0]} />
              <b>{two[0]}</b>
            </div>
            <div className="mqs-now-mid">
              {score ? <strong>{score}</strong> : <span className="mqs-vs">{t('vs')}</span>}
              {g.state === 'live' && <small className="mqs-live">{g.clock || t('Live')}</small>}
            </div>
            <div className="mqs-now-team">
              <Crest src={g.awayBadge} name={two[1]} />
              <b>{two[1]}</b>
            </div>
          </div>
        ) : (
          <h2 className="mqs-now-title">{g.title}</h2>
        )}
        {facts.length > 0 && <p className="mqs-now-facts">{facts.join(' · ')}</p>}
        <div className="mq-progress" aria-label={t('Progress')}>
          <span style={{ width: `${Math.min(100, Math.round(p.progress * 100))}%` }} />
        </div>
        <p className="mq-meta mq-dim">
          {p.paused ? t('Paused') : t('Playing')}
          {p.device ? ` · ${p.device}` : ''}
        </p>
      </div>
    </div>
  )
}
