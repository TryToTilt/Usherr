// Usherr Broadcast: the plain decisions behind Home, a title's page and the
// player's controls (broadcast-plan.md, phase 4a). No React and no imports
// here, so node can test it on its own (logic.test.ts).

// ---- The player --------------------------------------------------------------

/** Where a seek lands: never before the start, never past the end (when the end is known). */
export function seekTarget(at: number, by: number, duration: number): number {
  const to = at + by
  const end = Number.isFinite(duration) && duration > 0 ? duration : Infinity
  return Math.max(0, Math.min(end, to))
}

/** 1:02:05, or 42:10 under an hour. */
export function playClock(secs: number): string {
  const s = Math.max(0, Math.floor(Number.isFinite(secs) ? secs : 0))
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  const ss = String(s % 60).padStart(2, '0')
  return h ? `${h}:${String(m).padStart(2, '0')}:${ss}` : `${m}:${ss}`
}

/** How far through, 0 to 100. */
export function percentOf(at: number, duration: number): number {
  if (!Number.isFinite(duration) || duration <= 0) return 0
  return Math.max(0, Math.min(100, (at / duration) * 100))
}

/** The seconds a remote key moves the picture: Left and Right 10, the FF and RW keys 30. */
export const SEEK_STEP = 10
export const SEEK_JUMP = 30
/** The controls go away after this long with no key or touch. */
export const CONTROLS_FOR_MS = 4000

/**
 * What Back does in the player: hides the controls when they show, else
 * leaves the player. (A choice list open over them closes first.)
 */
export function playerBack(state: { listOpen: boolean; controls: boolean }): 'close-list' | 'hide' | 'leave' {
  if (state.listOpen) return 'close-list'
  return state.controls ? 'hide' : 'leave'
}

// ---- Home ----------------------------------------------------------------------

/** A Continue watching item, as /api/home/continue sends it (home_continue.go). */
export interface ContinueLike {
  item: string
  kind: 'movie' | 'tv'
  title: string
  episode?: string
  link: string
  position: number
  duration: number
}

/** What Resume plays: a library film or episode by its id; a streamed title has no id, so it opens its page. */
export function resumeTarget(c: Pick<ContinueLike, 'item'>): { kind: 'movie' | 'episode'; id: number } | null {
  const m = /^(movie|episode):(\d+)$/.exec(c.item)
  return m ? { kind: m[1] as 'movie' | 'episode', id: Number(m[2]) } : null
}

/** The minutes left, at least one. */
export function minutesLeft(c: Pick<ContinueLike, 'position' | 'duration'>): number {
  return Math.max(1, Math.round((c.duration - c.position) / 60))
}

/** The hero: the thing you were last part way through that has a known length. */
export function pickHero<T extends ContinueLike>(items: T[]): T | null {
  return items.find((c) => c.duration > 0 && c.position < c.duration) ?? null
}

/** A Live TV channel, as /api/tv/channels sends it. */
export interface ChannelLike {
  id: number
  number: number
  name: string
  now?: { title: string; subtitle?: string; start: string; end: string }
}

/** The channels with something on right now, with how far through it is (0-100), at most `max`. */
export function onNow<T extends ChannelLike>(channels: T[], nowMs: number, max = 8): (T & { pct: number })[] {
  const out: (T & { pct: number })[] = []
  for (const c of channels) {
    if (!c.now) continue
    const start = Date.parse(c.now.start)
    const end = Date.parse(c.now.end)
    if (!Number.isFinite(start) || !Number.isFinite(end) || end <= start || nowMs < start || nowMs >= end) continue
    out.push({ ...c, pct: Math.round(((nowMs - start) / (end - start)) * 100) })
    if (out.length >= max) break
  }
  return out
}

/** A crew vote (crewsApi.ts) still open: not closed and not past its closing time. */
export function openVote<T extends { state: string; closes: number }>(votes: T[] | undefined, nowMs: number): T | null {
  return (votes ?? []).filter((v) => v.state === 'open' && v.closes > nowMs).sort((a, b) => a.closes - b.closes)[0] ?? null
}

/** The first game today (local day) that has not finished long ago: from 3 hours ago on. */
export function gameToday<T extends { when: string }>(games: T[], nowMs: number): T | null {
  const today = new Date(nowMs).toDateString()
  return (
    games
      .filter((g) => {
        const at = Date.parse(g.when)
        return Number.isFinite(at) && new Date(at).toDateString() === today && at > nowMs - 3 * 3600_000
      })
      .sort((a, b) => Date.parse(a.when) - Date.parse(b.when))[0] ?? null
  )
}

/** A manga volume waiting to be started: Continue reading's item with no page read yet. */
export function newVolume<T extends { volumeId?: number; chapterId?: number; page: number }>(items: T[]): T | null {
  return items.find((c) => !!c.volumeId && !c.chapterId && c.page === 0) ?? null
}

// ---- Posters and titles --------------------------------------------------------

/** The title a poster's link opens, so its Options key can open that title's More sheet. */
export type PosterTitle = { kind: 'movie'; tmdbId: number } | { kind: 'show'; tmdbId: number } | { kind: 'series'; id: number }

export function posterTitle(href: string | null | undefined): PosterTitle | null {
  if (!href) return null
  let path = href
  try {
    path = new URL(href, 'http://x').pathname
  } catch {
    return null
  }
  // under a base path (/usherr/title/1) too
  const m = /\/(title|show|series)\/(\d+)\/?$/.exec(path)
  if (!m) return null
  const n = Number(m[2])
  if (!n) return null
  return m[1] === 'title' ? { kind: 'movie', tmdbId: n } : m[1] === 'show' ? { kind: 'show', tmdbId: n } : { kind: 'series', id: n }
}

/** An episode as a show's page has it. */
export interface EpisodeLike {
  id: number
  season: number
  episode: number
  status?: string
}

/**
 * What a show's Play plays when nothing is part way through: the first
 * downloaded episode of the first real season (specials, season 0, last).
 */
export function firstPlayable<T extends EpisodeLike>(episodes: T[]): T | null {
  const have = episodes.filter((e) => e.status === 'downloaded')
  const order = (e: T) => (e.season === 0 ? 1e6 : e.season) * 10000 + e.episode
  return have.sort((a, b) => order(a) - order(b))[0] ?? null
}

/** S1E4 */
export const shortCode = (season: number, episode: number) => `S${season}E${episode}`
