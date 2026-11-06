// Usherr: linked screenings, step 3. How a player keeps in step with the
// host (the same numbers as internal/link/screening/playback.go).

export type RoomState = 'waiting' | 'preshow' | 'film' | 'paused' | 'ended'

// Where the film is at now (ms, the clock atMs is on): a playing film or
// pre-show moves on with the clock, a paused or ended one stays. A pre-show
// that has run out is the film.
export function project(state: RoomState, position: number, atMs: number, now: number): { state: RoomState; position: number } {
  if (state === 'film' || state === 'preshow') {
    const pos = position + (now - atMs) / 1000
    return { state: state === 'preshow' && pos >= 0 ? 'film' : state, position: pos }
  }
  return { state, position }
}

export const IN_STEP = 0.1 // seconds: closer than this, nothing to do
export const SEEK_OVER = 1.0 // seconds: further than this, seek
export const RATE_GAIN = 0.08 // speed change for each second off
export const MAX_NUDGE = 0.06 // at most 6% faster or slower
export const SEEK_AHEAD = 0.25 // a seek lands this far ahead (a seek takes a moment)

// How a follower catches up: drift is how far behind it is (seconds;
// negative when ahead). Close: normal speed. A little off: slightly faster
// or slower until it's back. Far off: seek.
export function correct(drift: number): { rate: number; seek: boolean } {
  const a = Math.abs(drift)
  if (Number.isNaN(drift) || a < IN_STEP) return { rate: 1, seek: false }
  if (a > SEEK_OVER) return { rate: 1, seek: true }
  const nudge = Math.max(-MAX_NUDGE, Math.min(MAX_NUDGE, drift * RATE_GAIN))
  return { rate: Math.round((1 + nudge) * 1000) / 1000, seek: false }
}

// The server's clock minus this one, from a request's round trip: the
// answer's time is taken as the middle of the trip. Of several samples, the
// one with the shortest trip is the best.
export function clockOffset(samples: { sent: number; got: number; server: number }[]): number {
  let best: { sent: number; got: number; server: number } | null = null
  for (const s of samples) if (!best || s.got - s.sent < best.got - best.sent) best = s
  if (!best) return 0
  return best.server - (best.sent + best.got) / 2
}

// What a room says (internal/api/home_link_room.go), as far as the clock
// goes: where the film was at now.atMs (the server's clock), or, with
// nothing said yet (seq 0), the schedule.
export type RoomClock = {
  atMs: number // the film starts
  preshowSeconds: number
  runtime?: number // minutes
  now: { state: RoomState; position: number; atMs: number; seq: number }
}

// Where the film should be at now (the server's clock).
export function roomAt(r: RoomClock, now: number): { state: RoomState; position: number } {
  let at: { state: RoomState; position: number }
  if (r.now.seq === 0) {
    const position = (now - r.atMs) / 1000
    at = { state: position < 0 ? 'preshow' : 'film', position }
  } else at = project(r.now.state, r.now.position, r.now.atMs, now)
  if (at.state === 'preshow' && at.position < -r.preshowSeconds - 0.5) at = { ...at, state: 'waiting' }
  const end = r.runtime && r.runtime > 0 ? r.runtime * 60 + 30 : 0
  if (at.state === 'film' && end && at.position >= end) at = { state: 'ended', position: end }
  return at
}

// "in step", "0.4 s behind", "1.2 s ahead": the drift, said gently.
export function driftWords(drift: number): { key: 'in' | 'behind' | 'ahead'; secs: string } {
  if (Math.abs(drift) < 0.15) return { key: 'in', secs: '0' }
  return { key: drift > 0 ? 'behind' : 'ahead', secs: Math.abs(drift).toFixed(1) }
}
