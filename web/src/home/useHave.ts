// Home fork: what "Hide what I have" hides. A title can be had on disk,
// streamed (StreamNZB, nzbdav's Stream tier) or wanted (in Mediarium, not
// downloaded yet); each person picks which of those to hide, remembered in
// this browser. The household's titles come from /api/home/have (the media
// server, Mediarium and its streamed titles) and Mediarium's own library,
// which is kept live by useOwned.
import { useCallback, useEffect, useSyncExternalStore } from 'react'
import type { Owned } from '../useOwned'

export interface HideWhat {
  disk: boolean
  streamed: boolean
  wanted: boolean
}

const KEY = 'mediarium.discover.hideWhat'
const ALL: HideWhat = { disk: true, streamed: true, wanted: true }

function read(): HideWhat {
  try {
    const v = JSON.parse(localStorage.getItem(KEY) ?? 'null') as Partial<HideWhat> | null
    return v ? { ...ALL, ...v } : ALL
  } catch {
    return ALL
  }
}

let what = read()
const whatListeners = new Set<() => void>()

export function setHideWhat(next: HideWhat): void {
  what = next
  try {
    localStorage.setItem(KEY, JSON.stringify(next))
  } catch {
    // Private windows can refuse storage: the choice then lasts until the page closes.
  }
  whatListeners.forEach((fn) => fn())
}

export function useHideWhat(): [HideWhat, (next: HideWhat) => void] {
  const value = useSyncExternalStore(
    (fn) => {
      whatListeners.add(fn)
      return () => whatListeners.delete(fn)
    },
    () => what,
  )
  return [value, setHideWhat]
}

interface Sets {
  disk: Set<number>
  streamed: Set<number>
  wanted: Set<number>
}
interface Have {
  movies: Sets
  shows: Sets
}

// One copy for the whole page, read again after a few minutes.
let have: Have | null = null
let haveAt = 0
let loading: Promise<void> | null = null
const haveListeners = new Set<() => void>()

function loadHave(): void {
  if (loading || (have && Date.now() - haveAt < 5 * 60_000)) return
  const sets = (s?: { disk?: number[]; streamed?: number[]; wanted?: number[] }): Sets => ({ disk: new Set(s?.disk ?? []), streamed: new Set(s?.streamed ?? []), wanted: new Set(s?.wanted ?? []) })
  loading = fetch('/api/home/have', { credentials: 'include' })
    .then((r) => (r.ok ? r.json() : null))
    .then((d) => {
      if (d) {
        have = { movies: sets(d.movies), shows: sets(d.shows) }
        haveAt = Date.now()
        haveListeners.forEach((fn) => fn())
      }
    })
    .catch(() => undefined)
    .finally(() => {
      loading = null
    })
}

function useHaveSets(): Have | null {
  useEffect(loadHave, [])
  return useSyncExternalStore(
    (fn) => {
      haveListeners.add(fn)
      return () => haveListeners.delete(fn)
    },
    () => have,
  )
}

const ON_DISK = new Set(['downloaded', 'partial'])

// Whether "Hide what I have" leaves a title out, for movies or shows.
export function useIsHad(kind: 'movie' | 'tv', owned: Owned): (tmdbId: number) => boolean {
  const [w] = useHideWhat()
  const sets = useHaveSets()
  const mine = kind === 'movie' ? owned.movies : owned.shows
  const s = sets ? (kind === 'movie' ? sets.movies : sets.shows) : null
  return useCallback(
    (id: number) => {
      const st = mine.get(id)?.state.key
      if (w.disk && ((st && ON_DISK.has(st)) || s?.disk.has(id))) return true
      if (w.streamed && s?.streamed.has(id)) return true
      if (w.wanted && ((st && !ON_DISK.has(st)) || s?.wanted.has(id))) return true
      return false
    },
    [w, mine, s],
  )
}
