// Usherr SMH: nudges, small reminder tips ("Have you voted lately?"). Each
// checks the person's own data first, so it only shows when it's true for
// them, and leads to the walkthrough that helps. At most one a day; Not now
// waits a week, Don't remind me is for good, and the menu has a switch for
// all of them. Kept in this browser, per account.
import { api } from '../api'
import { t } from '../i18n'
import { listShowtimes, showtimeEnds } from '../home/showtimes'

export interface Nudge {
  id: string
  title: string
  why: string
  walk: string // the walkthrough Show me how starts
}

const DAY = 24 * 60 * 60_000
const KEY = (user: number | string) => `usherr.nudge.${user}`

interface NudgeState {
  lastShown?: number
  snooze?: Record<string, number> // nudge → shown again after this time
  never?: string[]
  off?: boolean
}

export function nudgeState(user: number | string): NudgeState {
  try {
    return (JSON.parse(localStorage.getItem(KEY(user)) || '{}') as NudgeState) ?? {}
  } catch {
    return {}
  }
}

export function saveNudgeState(user: number | string, s: NudgeState) {
  try {
    localStorage.setItem(KEY(user), JSON.stringify(s))
  } catch {
    // a private window: the tips just come back
  }
}

// What each nudge checks. Cheap checks first; each one that can't tell
// (an error, no data) says no.
const checks: { id: string; make: () => Promise<Nudge | null> }[] = [
  {
    id: 'vote',
    make: async () => {
      const r = await fetch('/api/home/votes', { credentials: 'include' })
      if (!r.ok) return null
      const d = (await r.json()) as { votes: { updatedAt?: string }[] }
      const last = Math.max(0, ...d.votes.map((v) => (v.updatedAt ? Date.parse(v.updatedAt) : 0)))
      if (Date.now() - last < 14 * DAY) return null
      return {
        id: 'vote',
        title: d.votes.length ? t('Have you voted lately?') : t('Teach Usherr your taste?'),
        why: t('Each thumb up or down makes your For you suggestions, Tonight’s picks and Roulette fit you better.'),
        walk: 'vote',
      }
    },
  },
  {
    id: 'showtime',
    make: async () => {
      const now = new Date()
      const day = now.getDay()
      if (!(day === 5 || day === 6) || now.getHours() < 12) return null
      const d = await listShowtimes()
      if (d.showtimes.some((s) => s.state !== 'done' && showtimeEnds(s) > d.nowMs)) return null
      return { id: 'showtime', title: t('Book a showtime this weekend?'), why: t('Pick a film and a time: Usherr plays the trailers and the pre-show first, like at the cinema.'), walk: 'book-showtime' }
    },
  },
  {
    id: 'roulette',
    make: async () => {
      const h = new Date().getHours()
      if (h < 17 || h > 22) return null
      return { id: 'roulette', title: t('Can’t decide what to watch?'), why: t('Roulette spins for one title that fits the people watching. Spin again until it feels right.'), walk: 'roulette' }
    },
  },
  {
    id: 'comics-panels',
    make: async () => {
      if (window.innerWidth >= 760) return null
      let reads = 0
      let view = ''
      try {
        reads = Object.keys(localStorage).filter((k) => k.startsWith('comic-done-')).length
        view = localStorage.getItem('comic-view') || ''
      } catch {
        return null
      }
      if (reads === 0 || view === 'panels') return null
      return { id: 'comics-panels', title: t('Try reading comics panel by panel?'), why: t('On a phone, Panels zooms to each panel in reading order, so the words are big enough to read.'), walk: 'read-comic' }
    },
  },
  {
    id: 'collection',
    make: async () => {
      const list = await api.collections()
      const part = list.filter((c) => c.movieIds.length > 0)
      for (const c of part.slice(0, 4)) {
        const info = await api.collection(c.id)
        const out = info.parts.filter((p) => p.released)
        const missing = out.filter((p) => !p.libraryId)
        if (missing.length > 0 && missing.length < out.length) {
          return {
            id: 'collection',
            title: t('Complete the {name}?', { name: info.name }),
            why: t('You have {n} of its {total} films. Usherr can get the rest in one go.', { n: out.length - missing.length, total: out.length }),
            walk: 'complete-collection',
          }
        }
      }
      return null
    },
  },
  {
    id: 'stack',
    make: async () => {
      const r = await fetch('/api/home/stack', { credentials: 'include' })
      if (!r.ok) return null
      const d = (await r.json()) as { items?: unknown[] } | unknown[]
      const n = Array.isArray(d) ? d.length : (d.items?.length ?? 0)
      if (n < 3) return null
      return { id: 'stack', title: t('Your stack is waiting'), why: t('{n} films and shows you set aside to watch. Swipe through it in the menu and pick one.', { n }), walk: 'stack' }
    },
  },
]

// nextNudge: the nudge to show now, or null (shown today already, all off,
// or nothing true right now).
export async function nextNudge(user: number | string, admin: boolean): Promise<Nudge | null> {
  const s = nudgeState(user)
  if (s.off) return null
  const today = new Date()
  today.setHours(0, 0, 0, 0)
  if ((s.lastShown ?? 0) >= today.getTime()) return null
  for (const c of checks) {
    if (s.never?.includes(c.id)) continue
    if ((s.snooze?.[c.id] ?? 0) > Date.now()) continue
    if (c.id === 'collection' && !admin) continue // only administrators add collections
    const n = await c.make().catch(() => null)
    if (n) return n
  }
  return null
}
