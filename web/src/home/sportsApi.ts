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
  missing?: number
  // Usherr's own engine: what the league gets.
  profileId?: number
  parts?: string[] // 'main' | 'prelims' | 'early' (fight cards), 'race' | 'qualifying' | 'sprint' | 'practice' (race weekends); none = all
  teamsOnly?: boolean
  kind?: SportKind
}

export type SportKind = 'team' | 'fight' | 'motor' | 'wrestling' | 'other'

export interface SportTeam {
  id?: string
  externalId: string
  name: string
  sport: string
  country?: string
  badge?: string
  followed: boolean
  monitored?: boolean // Usherr's own engine: its games are got
  profileId?: number
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
  // Usherr's own engine.
  downloading?: boolean
  kind?: SportKind
  parts?: string[] // a fight card's parts that are got
  have?: string[] // the parts it has
}

// SportRelease is one release found for a game (Pick a release).
export interface SportRelease {
  title: string
  indexerName: string
  protocol: string
  downloadUrl: string
  sizeBytes: number
  publishDate?: string
  matches: boolean
  accepted: boolean
  score: number
  quality: string
  part?: string
  blocklisted?: boolean
  rejections?: string[]
}

export interface SportWanted extends SportGame {
  missing: string[]
  searching: boolean
  searched?: string
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
  // Usherr's own engine gets games.
  path: string
  defaultPath: string
  profileId: number
  autoSearch: boolean
  searchDays: number
}

type SettingsBody = Partial<{
  mode: string
  sportarrUrl: string
  sportarrKey: string
  removeSportarrKey: boolean
  tsdbKey: string
  removeTsdbKey: boolean
  path: string
  profileId: number
  autoSearch: boolean
  searchDays: number
}>

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
    call<{ league: SportLeague; seasons: string[]; season: string; games: SportGame[]; canManage: boolean; canStream: boolean; canGet: boolean; backend?: string }>('GET', '/leagues/' + encodeURIComponent(id) + (season ? '?season=' + encodeURIComponent(season) : '')),
  addLeague: (l: SportLeague, monitor: boolean) => call<{ message: string }>('POST', '/leagues', { ...l, monitor }),
  followTeam: (team: SportTeam) => call<{ message: string }>('POST', '/teams', team),
  unfollowTeam: (id: string) => call<null>('DELETE', '/teams/' + encodeURIComponent(id)),
  getGame: (id: string) => call<{ message: string }>('POST', '/events/' + encodeURIComponent(id) + '/get'),
  stream: (id: string, quality: string) => call<{ profile: string; options: StreamOption[] }>('POST', '/events/' + encodeURIComponent(id) + '/stream', { quality }),
  streamed: (id: string, title: string, league?: string) => call<null>('POST', '/events/' + encodeURIComponent(id) + '/streamed', { title, league: league ?? '' }),
  settings: () => call<SportsSettings>('GET', '/settings'),
  saveSettings: (b: SettingsBody) => call<SportsSettings>('PUT', '/settings', b),
  test: () => call<{ message: string }>('POST', '/test'),
  // Usherr's own engine gets games (home_sports_get.go).
  leagueGet: (id: string, b: Partial<{ monitored: boolean; parts: string[]; teamsOnly: boolean; profileId: number }>) => call<{ message: string }>('PUT', '/leagues/' + encodeURIComponent(id) + '/get', b),
  teamGet: (id: string, b: Partial<{ monitored: boolean; profileId: number }>) => call<{ message: string }>('PUT', '/teams/' + encodeURIComponent(id) + '/get', b),
  setWanted: (id: string, wanted: boolean) => call<null>('PUT', '/events/' + encodeURIComponent(id) + '/wanted', { wanted }),
  searchNow: (id: string) => call<{ grabbed: number; message: string }>('POST', '/events/' + encodeURIComponent(id) + '/search'),
  releases: (id: string, part = '', q = '') =>
    call<{ releases: SportRelease[]; searched: string[]; parts: string[] }>('GET', '/events/' + encodeURIComponent(id) + '/releases?' + new URLSearchParams({ ...(part ? { part } : {}), ...(q ? { q } : {}) }).toString()),
  grab: (id: string, r: SportRelease, part = '') =>
    call<{ queueId: number }>('POST', '/events/' + encodeURIComponent(id) + '/grab', { title: r.title, downloadUrl: r.downloadUrl, sizeBytes: r.sizeBytes, protocol: r.protocol, part }),
  wanted: () => call<{ on: boolean; canGet: boolean; games: SportWanted[] }>('GET', '/wanted'),
  importSportarr: () => call<{ leagues: number; teams: number; already: number; failed: string[]; message: string }>('POST', '/import-sportarr'),
}

// partLabel is a fight card's part in words ('' is the game itself).
export const partLabel = (p: string) => (p === 'Early Prelims' ? t('Early prelims') : p === 'Prelims' ? t('Prelims') : p === 'Main Card' ? t('Main card') : t('The game'))

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
  if (g.downloading) return { label: t('Downloading'), cls: 'downloading' }
  if (g.hasFile && g.parts && g.parts.length > (g.have?.length ?? 0) && g.monitored) return { label: t('{n} of {total} parts', { n: g.have?.length ?? 0, total: g.parts.length }), cls: 'missing' }
  if (g.hasFile) return { label: g.quality ? t('Downloaded · {quality}', { quality: g.quality }) : t('Downloaded'), cls: 'downloaded' }
  if (g.monitored) return { label: t('Wanted'), cls: 'missing' }
  return null
}
