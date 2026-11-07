// Usherr: how a page turns: Curl, Slide or None. One choice per device, kept with
// the book reader's so both readers follow it (bookshelf/Reader.tsx writes the same
// place). A device asking for less motion gets None, whatever was chosen.
import { reducedMotion } from '../bookshelf/turn'

export type TurnEffect = 'curl' | 'slide' | 'none'

const PREFS_KEY = 'mediarium.reader'

export function storedTurnEffect(): TurnEffect {
  try {
    const p = JSON.parse(localStorage.getItem(PREFS_KEY) ?? '{}') as { effect?: string; turn?: string }
    if (p.effect === 'curl' || p.effect === 'slide' || p.effect === 'none') return p.effect
    if (reducedMotion() || p.turn === 'none' || localStorage.getItem('mediarium.reader.pageturn.off') === '1') return 'none'
  } catch {
    // no storage
  }
  return 'curl'
}

export function saveTurnEffect(effect: TurnEffect) {
  try {
    const p = JSON.parse(localStorage.getItem(PREFS_KEY) ?? '{}') as Record<string, unknown>
    const turn = effect === 'curl' ? (p.turn === 'none' ? 'slide' : p.turn) : effect
    localStorage.setItem(PREFS_KEY, JSON.stringify({ ...p, effect, turn }))
  } catch {
    // no storage: it lasts until the reader closes
  }
}
