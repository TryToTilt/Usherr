// Usherr Broadcast: the settings pages with a remote. An on/off switch (or a
// checkbox) turns on with Right and off with Left, the way the mock-up's rows
// change with Left and Right; when it is already that way the key moves on as
// usual (Left at the edge still opens the menu). Choices in a row of buttons
// are already one stop each: the arrows move along them and OK picks one.
// Classic and Neu are left alone.
import { useEffect } from 'react'
import { broadcastOn } from '../neu/look'
import { arrowDir, switchFlips } from './choice'

function onKey(e: KeyboardEvent) {
  if (e.defaultPrevented || e.altKey || e.ctrlKey || e.metaKey || e.shiftKey) return
  const d = arrowDir(e.key)
  if (!d) return
  const el = e.target
  if (!(el instanceof HTMLInputElement) || el.type !== 'checkbox' || el.disabled) return
  if (!switchFlips(el.checked, d)) return
  e.preventDefault() // the remote leaves a key that's been used
  el.click() // the page's own onChange runs, and saves as it always does
}

/** On the settings pages (SettingsLayout): Left and Right flip switches, in Broadcast only. */
export function useBroadcastSettingsKeys() {
  useEffect(() => {
    if (!broadcastOn()) return
    // on the document: before the remote, which listens on the window
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [])
}

// "Finish on your phone": on a TV-size screen SettingsLayout offers
// PhoneHandoff, which opens the same settings page on a signed-in phone for
// the fields that need typing (addresses, keys, folders, names).
