// Usherr Neu: the app's own soft look (web/src/neu/), or Usherr Classic. Kept
// per browser like light or dark; main.tsx sets it before the first paint, and
// a change reloads the page so every icon and picture follows.
// Usherr: "lobby" is Neu exactly, with the new layout around it (lobby.css):
// a sidebar of libraries, tabs along the bottom of a phone, the Server area
// for administrators and a bigger scale on a TV.
export type Look = 'classic' | 'neu' | 'lobby'

export const LOOK_KEY = 'usherr-look'

export function savedLook(): Look {
  try {
    const l = localStorage.getItem(LOOK_KEY)
    return l === 'neu' || l === 'lobby' ? l : 'classic'
  } catch {
    return 'classic'
  }
}

export function neuOn(): boolean {
  return document.documentElement.dataset.look === 'neu'
}

/** The new layout (the "lobby" look) is on in this browser. */
export function lobbyOn(): boolean {
  return typeof document !== 'undefined' && document.documentElement.dataset.layout === 'lobby'
}

// Everything bigger on a TV, in the new layout: picked here, or a TV's own
// browser (main.tsx). Kept in this browser; null means "decide by the browser".
export const TV_KEY = 'usherr-tv'

export function tvSizeOn(): boolean {
  return document.documentElement.dataset.tv === '1'
}

export function chooseTvSize(on: boolean) {
  try {
    localStorage.setItem(TV_KEY, on ? '1' : '0')
  } catch {
    // not kept, but it still applies until the page is reloaded
  }
  if (on) document.documentElement.dataset.tv = '1'
  else delete document.documentElement.dataset.tv
}

export function applyLook(look: Look) {
  const root = document.documentElement
  if (look === 'classic') delete root.dataset.look
  else root.dataset.look = 'neu'
  if (look === 'lobby') root.dataset.layout = 'lobby'
  else delete root.dataset.layout
}

export function chooseLook(look: Look) {
  try {
    localStorage.setItem(LOOK_KEY, look)
  } catch {
    // a blocked localStorage just means the choice will not survive a reload
  }
  applyLook(look)
  window.location.reload()
}
