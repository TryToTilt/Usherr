// Usherr: the calendar's search, shared by the top bar (where you type it on
// the Calendar) and the Calendar page (which narrows to what it finds).
import { useSyncExternalStore } from 'react'

export interface CalendarPick {
  answer: string
  keys: ReadonlySet<string>
}

interface State {
  q: string
  pick: CalendarPick | null
}

let state: State = { q: '', pick: null }
const listeners = new Set<() => void>()

export function setCalendarSearch(next: Partial<State>): void {
  state = { ...state, ...next }
  listeners.forEach((fn) => fn())
}

export function useCalendarSearch(): State {
  return useSyncExternalStore(
    (fn) => {
      listeners.add(fn)
      return () => listeners.delete(fn)
    },
    () => state,
  )
}
