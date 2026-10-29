// Usherr: Sports — what the Sports pages ask the server (home_sports.go).
import { t } from '../i18n'
import type { StreamOption } from './streamApi'

export interface SportLeague {
  id?: string
  externalId: string
  name: string
  sport: string
  country?: string
  description?: string
  badge?: string
  poster?: string
  banner?: string
  inLibrary: boolean
  monitored: boolean
  events?: number
  files?: number
}

export interface SportTeam {
  id?: string
  externalId: string
  name: string
  sport: string
  country?: string
  badge?: string
  followed: boolean
}

export interface SportGame {
  id: string
  title: string
  sport: string
  leagueId?: string
  league?: string
  leagueBadge?: string
  home?: string
  away?: string
  season?: string
  round?: string
  when: string
  venue?: string
  broadcast?: string
  image?: string
  monitored: boolean
  hasFile: boolean
  quality?: string
  status?: string
  homeScore?: number
  awayScore?: number
}

export interface SportsHome {
  on: boolean
  backend?: string
  problem?: string
  canManage: boolean
  canStream: boolean
  canGet: boolean
  leagues: SportLeague[]
  teams: SportTeam[]
  games: SportGame[]
}

export interface SportsSettings {
  mode: '' | 'sportarr' | 'usherr'
  sportarrUrl: string
  sportarrKeySet: boolean
  tsdbKeySet: boolean
  ready: boolean
  problem?: string
}

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch('/api/home/sports' + path, {
    method,
    credentials: 'include',
    cache: 'no-store',
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const text = await res.text()
  const data = text ? JSON.parse(text) : null
  if (!res.ok) throw new Error(data?.error ?? t('Something went wrong (error {status}).', { status: res.status }))
  return data as T
}

export const sportsApi = {
  home: () => call<SportsHome>('GET', ''),
  search: (q: string) => call<{ leagues: SportLeague[]; teams: SportTeam[] }>('GET', '/search?q=' + encodeURIComponent(q)),
  league: (id: string, season = '') =>
    call<{ league: SportLeague; seasons: string[]; season: string; games: SportGame[]; canManage: boolean; canStream: boolean; canGet: boolean }>('GET', '/leagues/' + encodeURIComponent(id) + (season ? '?season=' + encodeURIComponent(season) : '')),
  addLeague: (l: SportLeague, monitor: boolean) => call<{ message: string }>('POST', '/leagues', { ...l, monitor }),
  followTeam: (team: SportTeam) => call<{ message: string }>('POST', '/teams', team),
  unfollowTeam: (id: string) => call<null>('DELETE', '/teams/' + encodeURIComponent(id)),
  getGame: (id: string) => call<{ message: string }>('POST', '/events/' + encodeURIComponent(id) + '/get'),
  stream: (id: string, quality: string) => call<{ profile: string; options: StreamOption[] }>('POST', '/events/' + encodeURIComponent(id) + '/stream', { quality }),
  streamed: (id: string, title: string, league?: string) => call<null>('POST', '/events/' + encodeURIComponent(id) + '/streamed', { title, league: league ?? '' }),
  settings: () => call<SportsSettings>('GET', '/settings'),
  saveSettings: (b: Partial<{ mode: string; sportarrUrl: string; sportarrKey: string; removeSportarrKey: boolean; tsdbKey: string; removeTsdbKey: boolean }>) => call<SportsSettings>('PUT', '/settings', b),
  test: () => call<{ message: string }>('POST', '/test'),
}

// sportsImage shows sports artwork through Usherr (pages load images only
// from Usherr).
export const sportsImage = (u?: string) => (u ? '/api/home/sports/image?u=' + encodeURIComponent(u) : '')

// streamGame plays a game through Usherr's streaming engine: the release the
// sports backend matched to it that opens first, at this device's quality.
export async function streamGame(g: SportGame) {
  const { rememberedQuality, streamApi } = await import('./streamApi')
  const { playStream } = await import('./Player')
  const { nextExtras } = await import('./StreamButton')
  const status = await streamApi.status()
  const { options } = await sportsApi.stream(g.id, rememberedQuality(status.qualities ?? []))
  if (!options.length) throw new Error(t('No release of {title} was found.', { title: g.title }))
  const st = await streamApi.start({ option: options[0].id, title: g.title })
  playStream(st, g.title, undefined, { ...nextExtras(st, g.title, undefined, undefined, g.id), sport: g.id }) // "Try another release", as for a film; the Marquee knows the game
  void sportsApi.streamed(g.id, g.title, g.league).catch(() => undefined)
}

// gameState is a game's place: in the library, wanted, or neither.
export function gameState(g: SportGame): { label: string; cls: string } | null {
  if (g.hasFile) return { label: g.quality ? t('Downloaded · {quality}', { quality: g.quality }) : t('Downloaded'), cls: 'downloaded' }
  if (g.monitored) return { label: t('Wanted'), cls: 'missing' }
  return null
}
