// Usherr: demo mode's watch history: made-up people, players and titles, so
// Settings > System > Watching can be looked at without a media server.
import type { WatchCount, WatchSession, WatchStatsData } from './api'

const PEOPLE = ['Sam', 'Robin', 'Alex', 'Jo']
const PLAYERS: [device: string, client: string, location: 'lan' | 'remote'][] = [
  ['Living Room TV', 'Jellyfin Android TV', 'lan'],
  ['Bedroom Shield', 'Plex for Android (TV)', 'lan'],
  ['Pixel 9', 'Findroid', 'remote'],
  ['Firefox', 'Jellyfin Web', 'lan'],
  ['iPad', 'Infuse', 'remote'],
]
const MOVIES: [title: string, year: number, tmdbId: number, minutes: number][] = [
  ['Saltwind: Part Two', 2024, 9001, 166],
  ['The Long Equation', 2023, 9002, 118],
  ['The Marigold Lodge', 2015, 9003, 104],
  ['Neon Meridian', 2022, 9004, 131],
  ['Northbound', 2014, 9405, 97],
]
const SHOWS: [series: string, libraryId: number, tmdbId: number, episodes: string[]][] = [
  ['The Clean Floor', 101, 8801, ['Orientation', 'Floor Nine', 'Hollow', 'The Quiet Room']],
  ['Salt & Static', 102, 8802, ['Low Tide', 'Signal', 'The Pier']],
  ['Harbour Street', 103, 8803, ['Number Four', 'Night Shift']],
]
const REASONS = [['Video codec not supported'], ['Audio codec not supported'], ['Container not supported'], ['Video bitrate not supported', 'Audio codec not supported']]

// A small seeded random, so the demo looks the same on every visit.
function rng(seed: number) {
  let s = seed
  return () => {
    s = (s * 1103515245 + 12345) % 2147483648
    return s / 2147483648
  }
}

let cached: WatchSession[] | null = null

function sessions(): WatchSession[] {
  if (cached) return cached
  const r = rng(42)
  const out: WatchSession[] = []
  const now = Date.now()
  let id = 1
  for (let day = 0; day < 120; day++) {
    const plays = Math.floor(r() * 4) + (day % 7 === 5 || day % 7 === 6 ? 2 : 0)
    for (let k = 0; k < plays; k++) {
      const hour = [12, 18, 19, 20, 20, 21, 21, 22, 23][Math.floor(r() * 9)]
      const start = new Date(now - day * 86400e3)
      start.setHours(hour, Math.floor(r() * 60), 0, 0)
      if (start.getTime() > now) continue
      const [device, client, location] = PLAYERS[Math.floor(r() * PLAYERS.length)]
      const user = PEOPLE[Math.floor(r() * r() * PEOPLE.length)]
      const pick = r()
      const method = pick < 0.62 ? 'DirectPlay' : pick < 0.8 ? 'DirectStream' : 'Transcode'
      const base: WatchSession = {
        id: id++,
        serverId: location === 'lan' && client.startsWith('Plex') ? 2 : 1,
        server: client.startsWith('Plex') ? 'Plex' : 'Jellyfin',
        serverKind: client.startsWith('Plex') ? 'plex' : 'jellyfin',
        user,
        kind: 'movie',
        title: '',
        client,
        device,
        playMethod: method,
        transcodeReasons: method === 'Transcode' ? REASONS[Math.floor(r() * REASONS.length)] : undefined,
        videoIn: 'hevc',
        videoOut: method === 'Transcode' ? 'h264' : 'hevc',
        audioIn: r() < 0.5 ? 'eac3' : 'truehd',
        audioOut: method === 'DirectPlay' ? undefined : 'aac',
        resolutionIn: r() < 0.4 ? '4K' : '1080p',
        resolutionOut: method === 'Transcode' ? '1080p' : undefined,
        bandwidthKbps: method === 'Transcode' ? 8000 : 22000,
        location,
        startedAt: start.toISOString(),
        watchedSec: 0,
        progress: 0,
        paused: false,
        completed: false,
      }
      if (r() < 0.45) {
        const [title, year, tmdbId, minutes] = MOVIES[Math.floor(r() * MOVIES.length)]
        const progress = r() < 0.7 ? 0.95 + r() * 0.05 : 0.2 + r() * 0.6
        Object.assign(base, { kind: 'movie', title, year, tmdbId, libraryId: tmdbId - 9000, runtimeSec: minutes * 60, progress, watchedSec: Math.round(minutes * 60 * progress * 0.98) })
      } else {
        const [series, libraryId, tmdbId, eps] = SHOWS[Math.floor(r() * SHOWS.length)]
        const n = Math.floor(r() * eps.length)
        const progress = r() < 0.8 ? 0.93 + r() * 0.07 : 0.3 + r() * 0.5
        Object.assign(base, { kind: 'episode', series, season: 1 + (n % 2), episode: n + 1, title: eps[n], tmdbId, libraryId, runtimeSec: 2900, progress, watchedSec: Math.round(2900 * progress) })
      }
      base.completed = base.progress >= 0.9
      base.endedAt = new Date(start.getTime() + base.watchedSec * 1000 + 300e3).toISOString()
      out.push(base)
    }
  }
  out.sort((a, b) => b.startedAt.localeCompare(a.startedAt))
  // The two newest are still playing.
  const live: WatchSession[] = []
  const now2: [number, Partial<WatchSession>][] = [
    [0, { device: 'Living Room TV', client: 'Jellyfin Android TV', user: 'Sam', playMethod: 'DirectPlay', transcodeReasons: undefined }],
    [1, { device: 'Pixel 9', client: 'Findroid', user: 'Robin', playMethod: 'Transcode', transcodeReasons: ['Video bitrate not supported'], location: 'remote' }],
  ]
  for (const [i, s] of now2) {
    const base = out[i]
    live.push({ ...base, ...s, id: 9000 + i, startedAt: new Date(now - (i + 1) * 23 * 60e3).toISOString(), endedAt: undefined, watchedSec: (i + 1) * 21 * 60, progress: i === 0 ? 0.41 : 0.72, paused: i === 1, completed: false })
  }
  cached = [...live, ...out]
  return cached
}

function top(list: WatchSession[], key: (s: WatchSession) => string | undefined, n: number, extra?: (c: WatchCount, s: WatchSession) => void): WatchCount[] {
  const m = new Map<string, WatchCount>()
  for (const s of list) {
    const k = key(s)
    if (!k) continue
    const c = m.get(k) ?? { name: k, plays: 0, hours: 0 }
    c.plays++
    c.hours += s.watchedSec / 3600
    extra?.(c, s)
    m.set(k, c)
  }
  return [...m.values()]
    .map((c) => ({ ...c, hours: Math.round(c.hours * 10) / 10 }))
    .sort((a, b) => b.hours - a.hours || b.plays - a.plays)
    .slice(0, n)
}

function stats(days: number, user: string): WatchStatsData {
  const from = new Date()
  from.setHours(0, 0, 0, 0)
  from.setDate(from.getDate() - (days - 1))
  const list = sessions().filter((s) => new Date(s.startedAt) >= from && (!user || s.user === user))
  const perDay = new Map<string, number>()
  const hourOfDay = Array(24).fill(0) as number[]
  const dayOfWeek = Array(7).fill(0) as number[]
  for (const s of list) {
    const d = new Date(s.startedAt)
    const key = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
    perDay.set(key, (perDay.get(key) ?? 0) + s.watchedSec / 3600)
    hourOfDay[d.getHours()] += s.watchedSec / 3600
    dayOfWeek[(d.getDay() + 6) % 7] += s.watchedSec / 3600
  }
  const days_: { day: string; hours: number }[] = []
  for (let i = 0; i < days; i++) {
    const d = new Date(from)
    d.setDate(from.getDate() + i)
    const key = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
    days_.push({ day: key, hours: Math.round((perDay.get(key) ?? 0) * 10) / 10 })
  }
  const round = (v: number) => Math.round(v * 10) / 10
  const eps = new Map<string, Set<string>>()
  return {
    days,
    plays: list.length,
    hours: round(list.reduce((a, s) => a + s.watchedSec / 3600, 0)),
    people: new Set(list.map((s) => s.user)).size,
    titles: new Set(list.map((s) => (s.kind === 'episode' ? s.series : s.title))).size,
    finished: list.filter((s) => s.completed).length,
    perDay: days_,
    users: top(list, (s) => s.user, 50),
    movies: top(list, (s) => (s.kind === 'movie' ? s.title : undefined), 10, (c, s) => {
      c.tmdbId = s.tmdbId
      c.libraryId = s.libraryId
    }),
    shows: top(list, (s) => (s.kind === 'episode' ? s.series : undefined), 10, (c, s) => {
      c.tmdbId = s.tmdbId
      c.libraryId = s.libraryId
      const set = eps.get(c.name) ?? new Set<string>()
      set.add(`${s.season}x${s.episode}`)
      eps.set(c.name, set)
      c.episodes = set.size
    }),
    clients: top(list, (s) => s.client, 10),
    devices: top(list, (s) => s.device, 10),
    methods: {
      direct: list.filter((s) => s.playMethod === 'DirectPlay').length,
      stream: list.filter((s) => s.playMethod === 'DirectStream').length,
      transcode: list.filter((s) => s.playMethod === 'Transcode').length,
    },
    reasons: top(
      list.flatMap((s) => (s.transcodeReasons ?? []).map((r) => ({ ...s, client: r }))),
      (s) => s.client,
      10,
    ),
    hourOfDay: hourOfDay.map(round),
    dayOfWeek: dayOfWeek.map(round),
    locations: top(list, (s) => s.location, 2),
  }
}

export function demoWatch(path: string, q: URLSearchParams): unknown {
  const all = sessions()
  switch (path) {
    case '/api/watch/now':
      return { playing: all.filter((s) => !s.endedAt), everyone: true, recording: true }
    case '/api/watch/stats':
      return { stats: stats(Number(q.get('days')) || 30, q.get('user') ?? ''), everyone: true }
    case '/api/watch/title': {
      const id = Number(q.get('id'))
      const kind = q.get('kind') === 'tv' ? 'episode' : 'movie'
      const list = all.filter((s) => s.kind === kind && s.libraryId === id)
      return { plays: list.length, finished: list.filter((s) => s.completed).length, people: [...new Set(list.map((s) => s.user))].slice(0, 6), last: list[0]?.startedAt }
    }
  }
  const text = (q.get('q') ?? '').toLowerCase()
  const device = (q.get('device') ?? '').toLowerCase()
  const method = { direct: 'DirectPlay', stream: 'DirectStream', transcode: 'Transcode' }[q.get('method') ?? ''] ?? ''
  const from = q.get('from') ? new Date(`${q.get('from')}T00:00:00`) : null
  const to = q.get('to') ? new Date(`${q.get('to')}T23:59:59`) : null
  const list = all.filter(
    (s) =>
      (!q.get('user') || s.user === q.get('user')) &&
      (!text || `${s.title} ${s.series ?? ''}`.toLowerCase().includes(text)) &&
      (!device || `${s.device} ${s.client}`.toLowerCase().includes(device)) &&
      (!method || s.playMethod === method) &&
      (!Number(q.get('server')) || s.serverId === Number(q.get('server'))) &&
      (!from || new Date(s.startedAt) >= from) &&
      (!to || new Date(s.startedAt) <= to),
  )
  const size = Number(q.get('size')) || 25
  const page = Math.max(1, Number(q.get('page')) || 1)
  return {
    items: list.slice((page - 1) * size, page * size),
    total: list.length,
    page,
    size,
    everyone: true,
    choices: { users: [...PEOPLE].sort(), servers: [{ id: 1, name: 'Jellyfin' }, { id: 2, name: 'Plex' }] },
  }
}
