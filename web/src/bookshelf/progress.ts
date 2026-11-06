import { withBase } from '../base'
import type { BookFormat } from '../api'
import { updateStored } from './offline'
import { addPending, newerOnServer, type Pending } from './offlineMath'

export interface ProgressSave {
  format: BookFormat
  position: string
  percent: number
  finished?: boolean
}

const PENDING_KEY = 'usherr.books.pending'

function readPending(): Record<string, Pending> {
  try {
    const v = JSON.parse(localStorage.getItem(PENDING_KEY) ?? '{}') as Record<string, Pending>
    return v && typeof v === 'object' ? v : {}
  } catch {
    return {}
  }
}

function writePending(all: Record<string, Pending>) {
  try {
    if (Object.keys(all).length === 0) localStorage.removeItem(PENDING_KEY)
    else localStorage.setItem(PENDING_KEY, JSON.stringify(all))
  } catch {
    // storage full or blocked: the place is lost only if the page closes offline
  }
}

function remember(p: Pending) {
  writePending(addPending(readPending(), p))
}

async function put(url: string, body: unknown, keepalive: boolean): Promise<boolean> {
  try {
    const res = await fetch(url, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body), keepalive, credentials: 'same-origin' })
    // A proxy in front of a stopped server answers 502-504, and a session that
    // ended answers 401: try again later (signed in again).
    return !(res.status === 401 || res.status === 502 || res.status === 503 || res.status === 504)
  } catch {
    return false
  }
}

// saveProgress keeps a person's place in a book on the server, or in this
// browser until the server can be reached. The stored copy of a kept book's
// place follows too, so it opens there again offline.
export function saveProgress(id: number, p: ProgressSave, keepalive = false) {
  const at = Date.now()
  void updateStored(withBase(`/api/books/${id}/progress?format=${p.format}`), { bookId: id, ...p, finished: !!p.finished, updatedAt: new Date(at).toISOString() })
  const pending: Pending = { kind: 'book', id, format: p.format, body: p, at }
  if (!navigator.onLine) {
    remember(pending)
    return
  }
  void put(withBase(`/api/books/${id}/progress`), p, keepalive).then((ok) => {
    if (!ok) remember(pending)
  })
}

// saveComicPlace is the same for an issue of the Comics library (pages).
export function saveComicPlace(id: number, page: number, total: number, keepalive = false) {
  const body = { item: `comic:${id}`, position: page, duration: total }
  const at = Date.now()
  void updateStored(`/api/home/position?item=comic:${id}`, { position: page, duration: total })
  const pending: Pending = { kind: 'comic', id, body, at }
  if (!navigator.onLine) {
    remember(pending)
    return
  }
  void put('/api/home/position', body, keepalive).then((ok) => {
    if (!ok) remember(pending)
  })
}

let flushing = false

// sendPending sends the places read offline, once the server is back. A book
// read since on another device keeps that newer place.
export async function sendPending(): Promise<void> {
  if (flushing || !navigator.onLine) return
  const all = readPending()
  if (Object.keys(all).length === 0) return
  flushing = true
  try {
    const left = { ...all }
    for (const [k, p] of Object.entries(all)) {
      if (p.kind === 'book') {
        const res = await fetch(withBase(`/api/books/${p.id}/progress?format=${p.format}`), { credentials: 'same-origin', cache: 'no-store' }).catch(() => null)
        if (!res) break // still offline
        if (res.status === 404) {
          delete left[k] // the book is gone
          continue
        }
        if (!res.ok) break
        const server = (await res.json().catch(() => null)) as { updatedAt?: string } | null
        if (!newerOnServer(server?.updatedAt, p.at) && !(await put(withBase(`/api/books/${p.id}/progress`), p.body, false))) break
      } else if (!(await put('/api/home/position', p.body, false))) {
        break
      }
      delete left[k]
    }
    // Places read again here while sending stay for next time.
    const now = readPending()
    for (const [k, p] of Object.entries(now)) if (!all[k] || all[k].at !== p.at) left[k] = p
    writePending(left)
  } finally {
    flushing = false
  }
}

// watchConnection sends what is waiting now, and whenever the connection
// comes back.
export function watchConnection(): () => void {
  const go = () => void sendPending()
  go()
  window.addEventListener('online', go)
  return () => window.removeEventListener('online', go)
}
