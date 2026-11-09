// Usherr Neu: the app's own soft look (web/src/neu/), or Usherr Classic. Kept
// per browser like light or dark; main.tsx sets it before the first paint, and
// a change reloads the page so every icon and picture follows.
// Usherr: "lobby" is Neu exactly, with the new layout around it (lobby.css):
// a sidebar of libraries, tabs along the bottom of a phone, the Server area
// for administrators and a bigger scale on a TV.
// Usherr: "broadcast" is the Broadcast look (broadcast.css), the default for a
// browser that never picked one. It has its own layout marker
// (data-layout="broadcast"); Neu, Classic and "lobby" stay exactly as they were.
export type Look = 'broadcast' | 'classic' | 'neu' | 'lobby'

export const LOOK_KEY = 'usherr-look'

/** What a saved value means; nothing saved (or something unknown) is Broadcast. Kept in step with main.tsx. */
export function lookFrom(saved: string | null): Look {
  return saved === 'neu' || saved === 'lobby' || saved === 'classic' || saved === 'broadcast' ? saved : 'broadcast'
}

export function savedLook(): Look {
  try {
    return lookFrom(localStorage.getItem(LOOK_KEY))
  } catch {
    return 'broadcast'
  }
}

export function neuOn(): boolean {
  return document.documentElement.dataset.look === 'neu'
}

/** The Broadcast look (and its layout) is on in this browser. */
export function broadcastOn(): boolean {
  return typeof document !== 'undefined' && document.documentElement.dataset.look === 'broadcast'
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

/** The data-look and data-layout a look puts on <html>. */
export function lookAttrs(look: Look): { look?: string; layout?: string } {
  if (look === 'classic') return {}
  if (look === 'neu') return { look: 'neu' }
  if (look === 'lobby') return { look: 'neu', layout: 'lobby' }
  return { look: 'broadcast', layout: 'broadcast' }
}

export function applyLook(look: Look) {
  const root = document.documentElement
  const a = lookAttrs(look)
  if (a.look) root.dataset.look = a.look
  else delete root.dataset.look
  if (a.layout) root.dataset.layout = a.layout
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
