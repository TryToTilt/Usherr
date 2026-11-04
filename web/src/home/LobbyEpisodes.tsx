import { useEffect, useState, type ReactNode } from 'react'
import type { Episode } from '../api'
import Icon from '../components/Icon'
import { t } from '../i18n'
import { lobbyOn } from '../neu/look'

// Usherr: a show's episodes in the new layout (neu/look.ts), for watching.
// One season at a time, and each episode says in plain words where it
// stands: Play, Resume with the time left, On its way, Not here yet, or the
// day it arrives. Quality, files and monitoring stay with Episode tools.

export interface KeptPlace {
  item: string // episode:<id> or movie:<id>
  position: number
  duration: number
}

// useKeptPlaces is what this person is part way through in Usherr's own
// player (GET /api/home/continue), in the new layout only.
export function useKeptPlaces(): KeptPlace[] {
  const [places, setPlaces] = useState<KeptPlace[]>([])
  useEffect(() => {
    if (!lobbyOn()) return
    fetch('/api/home/continue', { credentials: 'include', cache: 'no-store' })
      .then((r) => (r.ok ? r.json() : []))
      .then((v: KeptPlace[]) => setPlaces(Array.isArray(v) ? v : []))
      .catch(() => undefined)
  }, [])
  return places
}

export function minutesLeft(p: KeptPlace): number {
  return Math.max(1, Math.round((p.duration - p.position) / 60))
}

export const epCode = (season: number, episode: number) => `S${season} E${episode}`

export default function LobbyEpisodes({
  seasons,
  start,
  today,
  resume,
  canPlay,
  onPlay,
  stream,
  tools,
}: {
  seasons: [number, Episode[]][]
  start: number // the season shown first
  today: string
  resume?: { episode: Episode; place: KeptPlace } | null
  canPlay: boolean
  onPlay: (e: Episode) => void
  stream: (e: Episode) => ReactNode // Watch it now, for an aired episode that isn't here
  tools?: ReactNode // Episode tools, for those who manage the show
}) {
  const [season, setSeason] = useState(start)
  const shown = seasons.find(([s]) => s === season) ?? seasons[seasons.length - 1]
  if (!shown) return null
  const [current, eps] = shown
  return (
    <section className="lobby-eps" data-walk="title-episodes" aria-label={t('Episodes')}>
      <div className="lobby-eps-head">
        <h2>{t('Episodes')}</h2>
        <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 10 }}>
          {seasons.length > 1 && (
            <div className="seg" role="radiogroup" aria-label={t('Season')}>
              {seasons.map(([s]) => (
                <button key={s} type="button" role="radio" aria-checked={s === current} className={s === current ? 'active' : ''} onClick={() => setSeason(s)}>
                  {s === 0 ? t('Specials') : t('Season {n}', { n: s })}
                </button>
              ))}
            </div>
          )}
          {tools}
        </div>
      </div>
      <ul className="lobby-eps-list">
        {eps.map((e) => {
          const aired = !!e.airDate && e.airDate <= today
          const here = e.status === 'downloaded'
          const now = resume?.episode.id === e.id ? resume.place : null
          let state: ReactNode
          if (now) state = <span className="lobby-ep-state left">{t('{n} min left', { n: minutesLeft(now) })}</span>
          else if (e.status === 'downloading') state = <span className="lobby-ep-state coming">{t('On its way')}</span>
          else if (!here && !aired) state = e.airDate ? t('Arrives {day}', { day: dayName(e.airDate, today) }) : t('Date not known yet')
          else if (!here) state = <span className="lobby-ep-state coming">{t('Not here yet')}</span>
          return (
            <li key={e.id} className={`lobby-ep${now ? ' now' : ''}`}>
              <span className="lobby-ep-num" aria-hidden="true">
                {e.episode}
              </span>
              <span className="lobby-ep-text">
                <b>
                  <span className="sr-only">{t('Episode {n}', { n: e.episode })}: </span>
                  {e.title || t('TBA')}
                </b>
                <small>
                  {aired && e.airDate ? shortDate(e.airDate) : null}
                  {state && aired && e.airDate ? ' · ' : null}
                  {state}
                </small>
                {now && (
                  <span className="lobby-ep-bar" aria-hidden="true">
                    <span style={{ width: `${Math.min(100, Math.round((now.position / now.duration) * 100))}%` }} />
                  </span>
                )}
                {aired && e.overview && <p>{e.overview}</p>}
              </span>
              <span className="lobby-ep-acts">
                {here && canPlay && (
                  <button type="button" className={`btn-with-icon${now ? ' primary' : ''}`} onClick={() => onPlay(e)} aria-label={(now ? t('Resume') : t('Play')) + ' ' + epCode(current, e.episode)}>
                    <Icon name="play" size={15} /> {now ? t('Resume') : t('Play')}
                  </button>
                )}
                {!here && aired && current > 0 && e.status !== 'downloading' && stream(e)}
              </span>
            </li>
          )
        })}
      </ul>
    </section>
  )
}

function shortDate(day: string): string {
  const d = new Date(`${day}T12:00:00`)
  return Number.isNaN(d.getTime()) ? day : d.toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: day.slice(0, 4) === String(new Date().getFullYear()) ? undefined : 'numeric' })
}

// dayName says when an episode arrives: a weekday within the week, a date after.
function dayName(day: string, today: string): string {
  const d = new Date(`${day}T12:00:00`)
  const days = Math.round((d.getTime() - new Date(`${today}T12:00:00`).getTime()) / 86400000)
  if (Number.isNaN(days)) return day
  if (days <= 0) return t('today')
  if (days === 1) return t('tomorrow')
  if (days < 7) return d.toLocaleDateString(undefined, { weekday: 'long' })
  return shortDate(day)
}
