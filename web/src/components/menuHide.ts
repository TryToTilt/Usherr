// Usherr: how long the open menu waits, once the mouse has left it, before it
// slides away (wider screens): 0 (right away) to 10 seconds. Kept per
// browser; the app shell hears a change at once.
export const STORAGE_KEY = 'usherr-menu-hide'
export const MENU_HIDE_EVENT = 'usherr-menu-hide'
const DEFAULT_S = 3

export function menuHideSeconds(): number {
  try {
    const v = Number(localStorage.getItem(STORAGE_KEY))
    return localStorage.getItem(STORAGE_KEY) !== null && Number.isFinite(v) ? Math.min(10, Math.max(0, Math.round(v))) : DEFAULT_S
  } catch {
    return DEFAULT_S
  }
}

