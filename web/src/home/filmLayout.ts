// Usherr: how a film's page is laid out, in this browser (like the colours).
// Three layouts arrange the same pieces: Cinema (the hero, then tabs), Side
// by side (the film on the left, you and the community on the right) and
// Streaming (a big backdrop, then rows; everything to manage behind one
// button). Automatic picks by the screen: Streaming on a TV-sized screen,
// Side by side on a wide one, Cinema on a phone or a laptop. A few switches
// say what the page shows, in every layout.
import { useEffect, useState } from 'react'
import { broadcastOn, lobbyOn } from '../neu/look'

// Usherr: 'lobby' is the new layout's own page (neu/look.ts), never a choice
// of its own: with that look on it is always used.
// Usherr Broadcast: 'broadcast' is Broadcast's own page in the same way.
export type FilmLayout = 'cinema' | 'side' | 'stream' | 'lobby' | 'broadcast'
export type FilmLayoutChoice = FilmLayout | 'auto'

export interface FilmPageShows {
  scores: boolean // the score line (IMDb, Rotten Tomatoes, …)
  streaming: boolean // where it's streaming
  you: boolean // your watches
  reviews: boolean // From the community
  cast: boolean
  similar: boolean // More like this
  fullPlot: boolean // the whole plot, not three lines
  libraryOpen: boolean // administrators: the library details shown, not folded
}

export interface FilmPagePrefs {
  layout: FilmLayoutChoice
  show: FilmPageShows
}

export const DEFAULT_SHOWS: FilmPageShows = { scores: true, streaming: true, you: true, reviews: true, cast: true, similar: true, fullPlot: false, libraryOpen: false }

const KEY = 'usherr-film-page'
const EVENT = 'usherr-film-page'

export function loadFilmPage(): FilmPagePrefs {
  try {
    const v = JSON.parse(localStorage.getItem(KEY) ?? 'null') as Partial<FilmPagePrefs> | null
    const layout = v?.layout && ['auto', 'cinema', 'side', 'stream'].includes(v.layout) ? v.layout : 'auto'
    return { layout, show: { ...DEFAULT_SHOWS, ...(v?.show ?? {}) } }
  } catch {
    return { layout: 'auto', show: { ...DEFAULT_SHOWS } }
  }
}

export function saveFilmPage(p: FilmPagePrefs) {
  try {
    localStorage.setItem(KEY, JSON.stringify(p))
  } catch {
    // a blocked localStorage just means the choice will not survive a reload
  }
  window.dispatchEvent(new Event(EVENT))
}

// autoLayout is Automatic's pick for a screen this wide.
export function autoLayout(width: number): FilmLayout {
  if (width >= 1800) return 'stream' // a TV, or a very big screen
  if (width >= 1280) return 'side'
  return 'cinema'
}

// useFilmPage is the saved choice, with Automatic worked out, following
// changes (from Settings, or the window being resized).
export function useFilmPage(): FilmPagePrefs & { resolved: FilmLayout } {
  const [prefs, setPrefs] = useState(loadFilmPage)
  const [width, setWidth] = useState(() => window.innerWidth)
  useEffect(() => {
    const onPrefs = () => setPrefs(loadFilmPage())
    const onResize = () => setWidth(window.innerWidth)
    window.addEventListener(EVENT, onPrefs)
    window.addEventListener('storage', onPrefs)
    window.addEventListener('resize', onResize)
    return () => {
      window.removeEventListener(EVENT, onPrefs)
      window.removeEventListener('storage', onPrefs)
      window.removeEventListener('resize', onResize)
    }
  }, [])
  return { ...prefs, resolved: broadcastOn() ? 'broadcast' : lobbyOn() ? 'lobby' : prefs.layout === 'auto' ? autoLayout(width) : prefs.layout }
}
