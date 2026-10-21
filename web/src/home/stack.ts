// Usherr: your Stack, the films and shows you pulled out to watch next. One
// copy for the whole page (the sidebar, every card's Stack button and title
// pages read the same list), kept on the server so your phone has it too.
import { useSyncExternalStore } from 'react'
import { t } from '../i18n'
import type { CardAction } from '../components/PosterCard'

export interface StackItem {
  kind: 'movie' | 'tv'
  tmdbId: number
  title: string
  year?: number
  posterUrl?: string
  libraryId?: number
  downloaded?: boolean
}

let items: StackItem[] | null = null
let loading: Promise<void> | null = null
const listeners = new Set<() => void>()
const emit = () => listeners.forEach((l) => l())

async function call(method: string, path: string, body?: unknown): Promise<void> {
  const r = await fetch(path, {
    method,
    credentials: 'include',
    cache: 'no-store',
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const d = (await r.json().catch(() => ({}))) as { items?: StackItem[]; error?: string }
  if (!r.ok) throw new Error(d.error ?? t('Something went wrong (error {status}).', { status: r.status }))
  items = d.items ?? []
  emit()
}

export function loadStack(): Promise<void> {
  if (!loading) loading = call('GET', '/api/home/stack').catch(() => undefined)
  return loading
}

export const stackKey = (kind: string, tmdbId: number) => `${kind}:${tmdbId}`

export function useStack(): StackItem[] | null {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l)
      void loadStack()
      return () => listeners.delete(l)
    },
    () => items,
  )
}

export function onStack(kind: string, tmdbId: number): boolean {
  return !!items?.some((i) => i.kind === kind && i.tmdbId === tmdbId)
}

export function addToStack(it: Omit<StackItem, 'libraryId' | 'downloaded'>): Promise<void> {
  return call('POST', '/api/home/stack', it)
}

export function removeFromStack(kind: string, tmdbId: number): Promise<void> {
  return call('DELETE', `/api/home/stack/${kind}/${tmdbId}`)
}

// moveInStack moves the title at index by one place (-1 is up, towards the top).
export function moveInStack(index: number, by: -1 | 1): Promise<void> {
  if (!items) return Promise.resolve()
  const order = items.map((i) => stackKey(i.kind, i.tmdbId))
  const j = index + by
  if (j < 0 || j >= order.length) return Promise.resolve()
  ;[order[index], order[j]] = [order[j], order[index]]
  // Seen at once; the server's answer follows.
  const moved = [...items]
  ;[moved[index], moved[j]] = [moved[j], moved[index]]
  items = moved
  emit()
  return call('PUT', '/api/home/stack/order', { order })
}

// The page each title opens.
export function stackPath(it: Pick<StackItem, 'kind' | 'tmdbId' | 'libraryId'>): string {
  if (it.kind === 'movie') return `/title/${it.tmdbId}`
  return it.libraryId ? `/series/${it.libraryId}` : `/show/${it.tmdbId}`
}

// stackAction is a card's Stack button: on, or off again.
export function stackAction(stack: StackItem[] | null, it: Omit<StackItem, 'libraryId' | 'downloaded'>, onError?: (e: unknown) => void): CardAction {
  const on = !!stack?.some((i) => i.kind === it.kind && i.tmdbId === it.tmdbId)
  return {
    icon: 'stack',
    label: on ? t('On your stack (tap to take it off)') : t('Put it on your stack'),
    active: on,
    onClick: () => void (on ? removeFromStack(it.kind, it.tmdbId) : addToStack(it)).catch((e: unknown) => onError?.(e)),
  }
}
