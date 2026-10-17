// Usherr: how each person likes Live TV, kept in this browser: the guide's
// look, its scale and how far ahead it goes, favourite and hidden channels,
// and the static between channels.
import { useSyncExternalStore } from 'react'

export type GuideStyle = 'modern' | 'prevue' | 'cable' | 'listings' | 'nowon'

export interface TVPrefs {
  style: GuideStyle
  scale: 15 | 30 | 60 // minutes a guide column covers
  hours: 6 | 12 | 24
  favs: number[] // channel ids
  favsOnly: boolean
  hidden: number[]
  staticOn: boolean
  staticSound: boolean
  crt: boolean // a CRT look on the picture
}

const KEY = 'usherr-tv-prefs'
const DEFAULTS: TVPrefs = { style: 'modern', scale: 30, hours: 24, favs: [], favsOnly: false, hidden: [], staticOn: true, staticSound: false, crt: false }

function read(): TVPrefs {
  try {
    return { ...DEFAULTS, ...(JSON.parse(localStorage.getItem(KEY) ?? '{}') as Partial<TVPrefs>) }
  } catch {
    return DEFAULTS
  }
}

let prefs = read()
const listeners = new Set<() => void>()

export function setTVPrefs(patch: Partial<TVPrefs>): void {
  prefs = { ...prefs, ...patch }
  try {
    localStorage.setItem(KEY, JSON.stringify(prefs))
  } catch {
    // remembering is optional
  }
  listeners.forEach((fn) => fn())
}

export function useTVPrefs(): TVPrefs {
  return useSyncExternalStore(
    (fn) => {
      listeners.add(fn)
      return () => listeners.delete(fn)
    },
    () => prefs,
  )
}

export function tvPrefsNow(): TVPrefs {
  return prefs
}

export const toggleIn = (list: number[], id: number) => (list.includes(id) ? list.filter((x) => x !== id) : [...list, id])
