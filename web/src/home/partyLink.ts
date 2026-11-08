// Usherr: the party at a showtime (internal/api/home_link_party.go): the
// lobby's seats and pre-show, the intermission, the verdict at the credits.
// One per showtime in this browser, kept fresh by its live events.
import { useEffect, useState } from 'react'
import { t } from '../i18n'

export type PartySeat = { seat: number; name?: string; house?: string; seated: boolean; me?: boolean }
export type PreshowItem = { kind: 'card' | 'clip' | 'trailer'; card?: string; title?: string; seconds: number }
export type PartyStatus = { name: string; house?: string; status: string }
export type Shared = { name: string; house?: string; stars: number }
export type TopMoment = { from: number; to: number; peak: number; total: number; words: Record<string, number>; top: string }

export interface Party {
  role: 'host' | 'guest' | 'local'
  showtimeId: number
  atMs: number
  canHost: boolean // runs the intermission and the credits
  lobby: { seats: PartySeat[]; canSit: boolean; seated: boolean; preshow: PreshowItem[] }
  intermission: { on: boolean; untilMs?: number; statuses: PartyStatus[]; mine?: string }
  verdict: {
    open: boolean
    mine: { stars: number; line?: string; share: boolean } | null
    count: number
    average: number // out of 5
    shared: Shared[]
    top: TopMoment | null
    stub: boolean
    canReview: boolean
  }
  statusChoices: string[]
  nowMs: number
}

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(`/api/home/showtimes${path}`, {
    method,
    credentials: 'include',
    cache: 'no-store',
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const data = (await res.json().catch(() => null)) as (T & { error?: string }) | null
  if (!res.ok) throw new Error(data?.error ?? t('Something went wrong (error {status}).', { status: res.status }))
  return data as T
}

// PartyLink is one showtime's party, while anything here shows it.
export class PartyLink {
  readonly id: number
  party: Party | null = null
  error = ''
  offset = 0 // the server's clock minus this one
  private listeners = new Set<() => void>()
  private refs = 0
  private events: EventSource | null = null
  private timer = 0
  private stopTimer = 0
  private loading = false
  private again = false

  constructor(id: number) {
    this.id = id
  }

  retain(): () => void {
    window.clearTimeout(this.stopTimer)
    if (this.refs++ === 0 && !this.events) this.start()
    let done = false
    return () => {
      if (done) return
      done = true
      if (--this.refs === 0) this.stopTimer = window.setTimeout(() => this.refs === 0 && this.stop(), 20_000)
    }
  }

  private start() {
    void this.load()
    try {
      this.events = new EventSource(`/api/home/showtimes/${this.id}/party/events`, { withCredentials: true })
      this.events.onmessage = () => void this.load()
      this.events.onopen = () => void this.load()
    } catch {
      this.events = null
    }
    this.timer = window.setInterval(() => void this.load(), 15_000)
  }

  private stop() {
    this.events?.close()
    this.events = null
    window.clearInterval(this.timer)
  }

  subscribe(fn: () => void): () => void {
    this.listeners.add(fn)
    return () => this.listeners.delete(fn)
  }

  private take(p: Party, sent: number, got: number) {
    this.offset = p.nowMs - (sent + got) / 2
    this.party = p
    this.error = ''
    for (const fn of this.listeners) fn()
  }

  async load() {
    if (this.loading) {
      this.again = true
      return
    }
    this.loading = true
    try {
      const sent = Date.now()
      const p = await call<Party>('GET', `/${this.id}/party`)
      this.take(p, sent, Date.now())
    } catch (e) {
      this.error = e instanceof Error ? e.message : String(e)
      for (const fn of this.listeners) fn()
    } finally {
      this.loading = false
      if (this.again) {
        this.again = false
        void this.load()
      }
    }
  }

  serverNow(): number {
    return Date.now() + this.offset
  }

  private async act(method: string, path: string, body?: unknown) {
    const sent = Date.now()
    const p = await call<Party>(method, `/${this.id}/party${path}`, body)
    this.take(p, sent, Date.now())
  }

  seat(seated: boolean) {
    return this.act('POST', '/seat', { seated })
  }
  intermission(action: 'start' | 'more' | 'end') {
    return this.act('POST', '/intermission', { action })
  }
  status(status: string) {
    return this.act('PUT', '/status', { status })
  }
  credits() {
    return this.act('POST', '/credits')
  }
  // verdict gives the film your stars; what it says back worth knowing
  // (it wasn't posted as a review, the host was out of reach).
  async verdict(v: { stars: number; line: string; share: boolean; review: boolean }): Promise<string[]> {
    const sent = Date.now()
    const d = await call<{ party: Party; notes: string[] }>('POST', `/${this.id}/party/verdict`, v)
    this.take(d.party, sent, Date.now())
    return d.notes ?? []
  }
}

const links = new Map<number, PartyLink>()

export function partyLink(id: number): PartyLink {
  let l = links.get(id)
  if (!l) {
    l = new PartyLink(id)
    links.set(id, l)
  }
  return l
}

// useParty keeps a showtime's party going while the component is shown,
// and shows it again on every change (and every second, for the clocks).
export function useParty(id: number | null): { link: PartyLink | null; party: Party | null } {
  const [, setTick] = useState(0)
  const link = id ? partyLink(id) : null
  useEffect(() => {
    if (!link) return
    const release = link.retain()
    const off = link.subscribe(() => setTick((n) => n + 1))
    const tm = window.setInterval(() => setTick((n) => n + 1), 1000)
    return () => {
      off()
      release()
      window.clearInterval(tm)
    }
  }, [link])
  return { link, party: link?.party ?? null }
}

// The statuses at the intermission, as people read them.
export const STATUS_LABEL: Record<string, () => string> = {
  snacks: () => t('Getting snacks'),
  drinks: () => t('Refilling drinks'),
  break: () => t('Quick break'),
  back: () => t('Back in my seat'),
}

// clockTo: "4:12" or "1:04:12" until a moment (never below zero).
export function clockTo(ms: number): string {
  const s = Math.max(0, Math.ceil(ms / 1000))
  const h = Math.floor(s / 3600)
  const m = Math.floor(s / 60) % 60
  const ss = String(s % 60).padStart(2, '0')
  return h > 0 ? `${h}:${String(m).padStart(2, '0')}:${ss}` : `${m}:${ss}`
}

// initials: "Sam" → "S", "Ana Maria" → "AM".
export function initials(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean)
  if (parts.length === 0) return '?'
  return (parts.length === 1 ? parts[0].slice(0, 1) : parts[0].slice(0, 1) + parts[parts.length - 1].slice(0, 1)).toUpperCase()
}

// hue: a steady colour for a name.
export function hue(name: string): number {
  let h = 0
  for (const c of name) h = (h * 31 + c.charCodeAt(0)) % 360
  return h
}

// preshowNow: which piece of the pre-show is on, seconds into it, and how
// far through the whole it is (0 to 1).
export function preshowNow(items: PreshowItem[], elapsed: number): { index: number; progress: number } {
  const total = items.reduce((n, it) => n + it.seconds, 0)
  if (!items.length || total <= 0) return { index: -1, progress: 0 }
  let at = 0
  for (let i = 0; i < items.length; i++) {
    if (elapsed < at + items[i].seconds) return { index: i, progress: Math.max(0, elapsed) / total }
    at += items[i].seconds
  }
  return { index: items.length - 1, progress: 1 }
}
