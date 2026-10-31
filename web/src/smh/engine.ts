// Usherr: Show Me How's running state: which walkthrough, which step, the
// answers given. Kept in this browser per account, so closing the app and
// coming back offers to carry on. Small on purpose: the walkthroughs and the
// coach card load only when one runs.
import { useSyncExternalStore } from 'react'
import type { SmhMode } from './types'

export interface SmhRun {
  id: string
  mode: SmhMode
  i: number
  answers: Record<string, unknown>
  watch: boolean
  paused?: boolean
}

const KEY = (user: number | string) => `usherr.smh.${user}`
const DONE = (user: number | string) => `usherr.smh.done.${user}`

let who: number | string | null = null
let run: SmhRun | null = null
const listeners = new Set<() => void>()
let menuOpen = false

// Keys and passwords typed into a walkthrough stay in memory only, never in
// the browser's storage; they are gone when the walkthrough ends or the page
// reloads (a step that needs one asks again).
export const smhSecrets = new Map<string, string>()

function emit() {
  listeners.forEach((f) => f())
}

function store() {
  if (who == null) return
  try {
    if (run) localStorage.setItem(KEY(who), JSON.stringify(run))
    else localStorage.removeItem(KEY(who))
  } catch {
    // A private window can refuse storage: the walkthrough just won't resume.
  }
}

// smhUser loads the signed-in account's walkthrough, if one was left running.
export function smhUser(id: number | string | null) {
  if (id === who) return
  who = id
  run = null
  if (id != null) {
    try {
      const s = JSON.parse(localStorage.getItem(KEY(id)) || 'null') as SmhRun | null
      if (s && typeof s.id === 'string' && typeof s.i === 'number') run = { ...s, paused: true }
    } catch {
      run = null
    }
  }
  emit()
}

export function smhRun(): SmhRun | null {
  return run
}

export function setSmhRun(next: SmhRun | null) {
  run = next
  store()
  emit()
}

// startSmh begins a walkthrough. Simple starts in "Watch me do it".
export function startSmh(id: string, mode: SmhMode) {
  menuOpen = false
  setSmhRun({ id, mode, i: 0, answers: {}, watch: mode === 'simple' })
}

export function stopSmh() {
  smhSecrets.clear()
  setSmhRun(null)
}

export function markSmhDone(id: string) {
  if (who == null) return
  try {
    const list = new Set<string>(JSON.parse(localStorage.getItem(DONE(who)) || '[]'))
    list.add(id)
    localStorage.setItem(DONE(who), JSON.stringify([...list]))
  } catch {
    // only a tick in the menu
  }
}

export function smhDoneList(): Set<string> {
  if (who == null) return new Set()
  try {
    return new Set<string>(JSON.parse(localStorage.getItem(DONE(who)) || '[]'))
  } catch {
    return new Set()
  }
}

export function openSmhMenu() {
  menuOpen = true
  emit()
}

export function closeSmhMenu() {
  menuOpen = false
  emit()
}

const subscribe = (f: () => void) => {
  listeners.add(f)
  return () => listeners.delete(f)
}

export function useSmh(): { run: SmhRun | null; menuOpen: boolean } {
  const r = useSyncExternalStore(subscribe, () => run)
  const m = useSyncExternalStore(subscribe, () => menuOpen)
  return { run: r, menuOpen: m }
}
