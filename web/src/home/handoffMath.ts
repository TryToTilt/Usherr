// Usherr: hanging out in a crew when a showtime starts. The crew screen
// moves everyone to Showtime by itself when the pre-show begins (or the film,
// when there's no pre-show), and says so a few minutes before.
import type { Showtime } from './showtimes.ts'

export const HANDOFF_SOON_MS = 10 * 60_000 // the heads-up shows this long before
export const HANDOFF_LATE_MS = 5 * 60_000 // after the film started this long ago, nobody is moved

export interface Handoff {
  s: Showtime
  goAt: number // when the move happens: the pre-show's start, else showtime
  go: boolean // now
}

// handoffFor picks the showtime this person watches here that's starting
// soon (or now), leaving out ones they said no to or were already moved to.
export function handoffFor(list: Showtime[], now: number, skip: ReadonlySet<number>): Handoff | null {
  let best: Handoff | null = null
  for (const s of list) {
    if (s.state === 'done' || skip.has(s.id)) continue
    if (!(s.playOn === 'here' || s.linked || s.hosted)) continue
    if (s.linked?.mine === 'not') continue
    const goAt = s.atMs - Math.max(0, s.preshowMin) * 60_000
    if (now < goAt - HANDOFF_SOON_MS || now > s.atMs + HANDOFF_LATE_MS) continue
    if (!best || goAt < best.goAt) best = { s, goAt, go: now >= goAt }
  }
  return best
}
