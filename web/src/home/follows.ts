// Home fork: following people (actors, directors, writers). Shared by person
// pages, the Discover row and the Following tab.
import type { DiscoverMovie } from '../api'

export interface Followed {
  id: number
  name: string
  image: string
  known: string
}

export interface FollowRow extends Followed {
  movies: DiscoverMovie[]
  shows: DiscoverMovie[]
  fresh: number
}

export interface FollowWork {
  movies: DiscoverMovie[]
  shows: DiscoverMovie[]
  who: Record<string, string> // "movie:123" -> "Christian Bale"
  people: FollowRow[]
}

async function send<T>(method: string, path: string, body?: unknown): Promise<T> {
  const r = await fetch(`/api/home/follows${path}`, {
    method,
    credentials: 'include',
    headers: body ? { 'Content-Type': 'application/json' } : undefined,
    body: body ? JSON.stringify(body) : undefined,
  })
  const out = await r.json().catch(() => ({}))
  if (!r.ok) throw new Error(out.error ?? r.statusText)
  return out as T
}

export const listFollows = () => send<{ people: Followed[] }>('GET', '').then((d) => d.people)
export const followWork = () => send<FollowWork>('GET', '/work')
export const follow = (p: Followed) => send<{ people: Followed[] }>('PUT', `/${p.id}`, { name: p.name, image: p.image, known: p.known }).then((d) => d.people)
export const unfollow = (id: number) => send<{ people: Followed[] }>('DELETE', `/${id}`).then((d) => d.people)

// The line under each title: who it's from.
export function whoFor(work: FollowWork | null, kind: 'movie' | 'tv'): Record<number, string> {
  const out: Record<number, string> = {}
  for (const [key, names] of Object.entries(work?.who ?? {})) {
    const [k, id] = key.split(':')
    if (k === kind) out[Number(id)] = names
  }
  return out
}
