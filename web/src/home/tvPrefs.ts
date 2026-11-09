// Usherr: how each person likes Live TV, kept in this browser: the guide's
// look, its scale and how far ahead it goes, favourite and hidden channels,
// and the static between channels.
import { useSyncExternalStore } from 'react'
import { broadcastOn } from '../neu/look' // Usherr Broadcast

// Usherr Broadcast: 'broadcast' is the Broadcast look's own grid (TVBroadcastGuide.tsx).
export type GuideStyle = 'modern' | 'rounded' | 'prevue' | 'cable' | 'listings' | 'nowon' | 'broadcast'

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
  bstyle?: GuideStyle // Usherr Broadcast: the guide style picked while the look is Broadcast
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

// Usherr Broadcast: the guide style in use. In the Broadcast look it is its
// own choice (the Broadcast grid until another is picked), so a style picked
// in Classic or Neu is kept for them; the Broadcast grid never shows in them.
export function guideStyleOf(p: TVPrefs): GuideStyle {
  if (broadcastOn()) return p.bstyle ?? 'broadcast'
  return p.style === 'broadcast' ? 'modern' : p.style
}

export function chooseGuideStyle(style: GuideStyle): void {
  if (broadcastOn()) setTVPrefs({ bstyle: style })
  else setTVPrefs({ style })
}
