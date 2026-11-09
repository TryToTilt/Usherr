// Usherr Broadcast: how the one menu sits on a computer or a TV, kept per
// browser (like menuHide.ts). "icons" is a narrow rail of icons that opens
// when it's hovered or focused; "open" keeps the whole menu in sight; "hidden"
// keeps it out of the way until the menu button or the left edge opens it.
// Phones always get the bar along the bottom and the menu as a drawer.
//
// No React in this file, so it can be tested on its own.

export type MenuMode = 'open' | 'icons' | 'hidden'

export const MENU_MODE_KEY = 'usherr-menu-mode'
export const MENU_MODE_EVENT = 'usherr-menu-mode'
export const DEFAULT_MENU_MODE: MenuMode = 'icons'

export function asMenuMode(v: string | null | undefined): MenuMode {
  return v === 'open' || v === 'hidden' || v === 'icons' ? v : DEFAULT_MENU_MODE
}

export function menuMode(): MenuMode {
  try {
    return asMenuMode(localStorage.getItem(MENU_MODE_KEY))
  } catch {
    return DEFAULT_MENU_MODE
  }
}

export function chooseMenuMode(mode: MenuMode) {
  try {
    localStorage.setItem(MENU_MODE_KEY, mode)
  } catch {
    // a blocked localStorage just means the choice will not survive a reload
  }
  window.dispatchEvent(new Event(MENU_MODE_EVENT))
}
