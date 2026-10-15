// Home fork: your thumbs up and down, for every kind of media. Loaded once per
// page, saved as you tap (shown at once, put back if saving fails). Votes are
// your own: nobody else sees them.
import { useCallback, useEffect, useSyncExternalStore } from 'react'

export type VoteKind = 'movie' | 'tv' | 'artist' | 'album' | 'book' | 'comic'
export interface VoteMeta {
  title?: string
  year?: number
  image?: string
}

const votes = new Map<string, number>() // "movie:603" -> 1 | -1
let loaded = false
let loading = false
let version = 0
const listeners = new Set<() => void>()
const notify = () => {
  version++
  listeners.forEach((fn) => fn())
}
const key = (kind: VoteKind, ref: string | number) => `${kind}:${ref}`

function load(): void {
  if (loaded || loading) return
  loading = true
  fetch('/api/home/votes', { credentials: 'include' })
    .then((r) => (r.ok ? r.json() : { votes: [] }))
    .then((d: { votes: { kind: VoteKind; ref: string; vote: number }[] }) => {
      for (const v of d.votes) votes.set(key(v.kind, v.ref), v.vote)
      loaded = true
      notify()
    })
    .catch(() => undefined)
    .finally(() => {
      loading = false
    })
}

export function setVote(kind: VoteKind, ref: string | number, vote: 1 | -1 | 0, meta: VoteMeta = {}): void {
  const k = key(kind, ref)
  const before = votes.get(k) ?? 0
  if (vote === 0) votes.delete(k)
  else votes.set(k, vote)
  notify()
  fetch('/api/home/votes', {
    method: 'PUT',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      kind,
      ref: String(ref),
      vote,
      title: meta.title,
      year: meta.year,
      image: meta.image,
    }),
  })
    .then((r) => {
      if (!r.ok) throw new Error(String(r.status))
    })
    .catch(() => {
      if (before) votes.set(k, before)
      else votes.delete(k)
      notify()
    })
}

export interface Votes {
  get: (kind: VoteKind, ref: string | number) => number
  toggle: (kind: VoteKind, ref: string | number, vote: 1 | -1, meta?: VoteMeta) => void
}

export function useVotes(): Votes {
  useEffect(load, [])
  const v = useSyncExternalStore(
    (fn) => {
      listeners.add(fn)
      return () => listeners.delete(fn)
    },
    () => version,
  )
  const get = useCallback(
    (kind: VoteKind, ref: string | number) => {
      void v // a new vote: a new function, so lists filter again
      return votes.get(key(kind, ref)) ?? 0
    },
    [v],
  )
  // Tapping the same thumb again takes the vote back.
  const toggle = useCallback((kind: VoteKind, ref: string | number, vote: 1 | -1, meta?: VoteMeta) => {
    setVote(kind, ref, (votes.get(key(kind, ref)) ?? 0) === vote ? 0 : vote, meta)
  }, [])
  return { get, toggle }
}
