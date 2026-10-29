// Usherr Neu: the app's own soft look (web/src/neu/), or Usherr Classic. Kept
// per browser like light or dark; main.tsx sets it before the first paint, and
// a change reloads the page so every icon and picture follows.
export type Look = 'classic' | 'neu'

export const LOOK_KEY = 'usherr-look'

export function savedLook(): Look {
  try {
    return localStorage.getItem(LOOK_KEY) === 'neu' ? 'neu' : 'classic'
  } catch {
    return 'classic'
  }
}

export function neuOn(): boolean {
  return document.documentElement.dataset.look === 'neu'
}

export function applyLook(look: Look) {
  if (look === 'neu') document.documentElement.dataset.look = 'neu'
  else delete document.documentElement.dataset.look
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
