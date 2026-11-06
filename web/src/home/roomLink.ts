// Usherr: linked screenings, step 3 (internal/api/home_link_room.go). A
// linked screening's room in this browser: where the film should be, by
// this Usherr's clock (measured from each answer's round trip), kept fresh
// by the room's live events. The player and the Showtime page share one per
// screening.
import { useEffect, useState } from 'react'
import { t } from '../i18n'
import { clockOffset, roomAt, type RoomClock, type RoomState } from './syncMath'

export interface Room extends RoomClock {
  role: 'host' | 'guest'
  showtimeId: number
  title: string
  openMs: number // "Join the screening" from then
  anyonePause: boolean
  canControl: boolean // moves the film for everyone
  canPause: boolean // may pause (or ask the host to)
  host: string // the Usherr hosting
  hostName?: string
  following: number // host: linked Usherrs at the screening
  relay: string // connected, connecting, error, off
  heardMs?: number // guest: when the host last said where the film is
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

type Sample = { sent: number; got: number; server: number }

// RoomLink is one screening's room, while anything here uses it.
export class RoomLink {
  readonly id: number
  room: Room | null = null
  error = ''
  private offset = 0
  private samples: Sample[] = []
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

  // retain starts it (when it isn't yet); the function it gives back lets go.
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
      this.events = new EventSource(`/api/home/showtimes/${this.id}/room/events`, { withCredentials: true })
      this.events.onmessage = () => void this.load()
      this.events.onopen = () => void this.load() // back after a break: the latest
    } catch {
      this.events = null
    }
    this.timer = window.setInterval(() => void this.load(), 10_000)
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

  private emit() {
    for (const fn of this.listeners) fn()
  }

  private take(r: Room, sent: number, got: number) {
    this.samples = [...this.samples.slice(-9), { sent, got, server: r.nowMs }]
    this.offset = clockOffset(this.samples)
    this.room = r
    this.error = ''
    this.emit()
  }

  async load() {
    if (this.loading) {
      this.again = true
      return
    }
    this.loading = true
    try {
      const sent = Date.now()
      const r = await call<Room>('GET', `/${this.id}/room`)
      this.take(r, sent, Date.now())
    } catch (e) {
      this.error = e instanceof Error ? e.message : String(e)
      this.emit()
    } finally {
      this.loading = false
      if (this.again) {
        this.again = false
        void this.load()
      }
    }
  }

  // The server's clock, now.
  serverNow(): number {
    return Date.now() + this.offset
  }

  // Where the film should be now.
  at(): { state: RoomState; position: number } | null {
    return this.room ? roomAt(this.room, this.serverNow()) : null
  }

  // The host plays, pauses, seeks or ends it, for everyone.
  async control(state: 'film' | 'paused' | 'ended', position?: number) {
    // Here at once (the player doesn't wait for the answer to stop or go);
    // the answer puts it right.
    if (this.room && position !== undefined) {
      this.room = { ...this.room, now: { state, position: Math.max(0, position), atMs: this.serverNow(), seq: this.room.now.seq + 1 } }
      this.emit()
    }
    const sent = Date.now()
    try {
      const r = await call<Room>('POST', `/${this.id}/room/playback`, position === undefined ? { state } : { state, position: Math.max(0, position) })
      this.take(r, sent, Date.now())
    } catch (e) {
      void this.load() // not taken: back to what the room says
      throw e
    }
  }

  // Pause: at the host, for everyone; at a guest, the host is asked.
  async pause() {
    await call<unknown>('POST', `/${this.id}/room/pause`)
    void this.load()
  }

  async setAnyonePause(on: boolean) {
    const sent = Date.now()
    const r = await call<Room>('PUT', `/${this.id}/room/settings`, { anyonePause: on })
    this.take(r, sent, Date.now())
  }
}

const links = new Map<number, RoomLink>()

export function roomLink(id: number): RoomLink {
  let l = links.get(id)
  if (!l) {
    l = new RoomLink(id)
    links.set(id, l)
  }
  return l
}

// useRoomLink keeps a screening's room going while the component is shown,
// and shows it again on every change (and every second, for the clock).
export function useRoomLink(id: number | null): { link: RoomLink | null; room: Room | null } {
  const [, setTick] = useState(0)
  const link = id ? roomLink(id) : null
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
  return { link, room: link?.room ?? null }
}
