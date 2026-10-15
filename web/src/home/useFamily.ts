// Home fork: Discover's family filter. Each person picks the oldest age a
// title may need (remembered in this browser); titles that need more, or
// whose age isn't known, are left out. Ages come from /api/home/family
// (Common Sense Media, the US rating, or an AI estimate), asked for in
// batches and kept for the page's life.
import { useCallback, useEffect, useSyncExternalStore } from 'react'

export type FamilyLevel = 'off' | '7' | '10' | '13'
const KEY = 'mediarium.discover.family'

function readLevel(): FamilyLevel {
  try {
    const v = localStorage.getItem(KEY)
    return v === '7' || v === '10' || v === '13' ? v : 'off'
  } catch {
    return 'off'
  }
}

let level = readLevel()
const listeners = new Set<() => void>()
const notify = () => listeners.forEach((fn) => fn())
const subscribe = (fn: () => void) => {
  listeners.add(fn)
  return () => listeners.delete(fn)
}

export function setFamilyLevel(next: FamilyLevel): void {
  level = next
  try {
    localStorage.setItem(KEY, next)
  } catch {
    // Private windows can refuse storage: the choice then lasts until the page closes.
  }
  notify()
}

export function useFamilyLevel(): [FamilyLevel, (next: FamilyLevel) => void] {
  return [useSyncExternalStore(subscribe, () => level), setFamilyLevel]
}

// ---- ages, asked for in batches ----

type Kind = 'movie' | 'tv'
interface Item {
  tmdbId: number
  title: string
  year?: number
  genres?: string[]
  overview?: string
}

const ages = new Map<string, number>() // "movie:603" -> 13; -1 not known
const tries = new Map<string, number>()
const queued = new Map<string, { kind: Kind; item: Item }>()
let version = 0
let timer: number | undefined
const MAX_TRIES = 30 // about two minutes at one ask every 4 seconds: a local AI model can take a minute to wake up

function flush(): void {
  timer = undefined
  const byKind: Record<Kind, Item[]> = { movie: [], tv: [] }
  for (const { kind, item } of queued.values()) if (byKind[kind].length < 80) byKind[kind].push(item)
  for (const kind of ['movie', 'tv'] as Kind[]) {
    const list = byKind[kind]
    if (!list.length) continue
    for (const it of list) queued.delete(`${kind}:${it.tmdbId}`)
    fetch('/api/home/family', {
      method: 'POST',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ kind, items: list.map((i) => ({ id: i.tmdbId, title: i.title, year: i.year ?? 0, genres: i.genres, overview: i.overview?.slice(0, 240) })) }),
    })
      .then((r) => (r.ok ? r.json() : null))
      .then((d: { ages: Record<string, number>; pending: number[] } | null) => {
        if (d) for (const [id, age] of Object.entries(d.ages)) ages.set(`${kind}:${id}`, age)
        const left = d ? new Set(d.pending) : new Set(list.map((i) => i.tmdbId))
        for (const it of list) {
          if (!left.has(it.tmdbId)) continue
          const k = `${kind}:${it.tmdbId}`
          const n = (tries.get(k) ?? 0) + 1
          tries.set(k, n)
          if (n >= MAX_TRIES) ages.set(k, -1) // give up for now: not known
          else queued.set(k, { kind, item: it })
        }
        version++
        notify()
        if (queued.size) timer = window.setTimeout(flush, 4000)
      })
      .catch(() => undefined)
  }
}

function want(kind: Kind, items: Item[]): void {
  let added = false
  for (const it of items) {
    const k = `${kind}:${it.tmdbId}`
    if (ages.has(k) || queued.has(k)) continue
    queued.set(k, { kind, item: it })
    added = true
  }
  if (added && timer === undefined) timer = window.setTimeout(flush, 150)
}

// Whether a title may be shown with the family filter as chosen. While a
// title's age is being worked out it's left out.
export function useFamilyOk(kind: Kind, items: Item[]): { on: boolean; ok: (tmdbId: number) => boolean } {
  const [lvl] = useFamilyLevel()
  const on = lvl !== 'off'
  const v = useSyncExternalStore(subscribe, () => version)
  useEffect(() => {
    if (on && items.length) want(kind, items)
  }, [on, kind, items])
  const max = Number(lvl)
  const ok = useCallback(
    (id: number) => {
      if (!on) return true
      void v // a new answer arrived: a new function, so lists filter again
      const age = ages.get(`${kind}:${id}`)
      return age !== undefined && age >= 0 && age <= max
    },
    [on, kind, max, v],
  )
  return { on, ok }
}
