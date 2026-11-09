// Usherr Broadcast: moving through the Broadcast guide with a remote
// (TVBroadcastGuide.tsx). Up and Down land on the programme on at the same
// time in the next channel, Left and Right go along a channel, and the
// window of time on screen follows the focus at its edges.

export interface Span {
  start: number // ms
  end: number // ms
}

export const MIN = 60_000
export const SLOT = 30 * MIN // each column of the time header

/** ms rounded down to a whole step (30 minutes by default). */
export const floorTo = (ms: number, step = SLOT) => Math.floor(ms / step) * step

/** The time the focus stands at: the programme's start, or the window's start for one that began before it. */
export const focusTime = (p: Span, winStart: number) => Math.max(p.start, winStart)

/**
 * The programme in a channel's row on at `at`; when nothing is (a gap), the
 * next one to start, else the last. -1 for an empty row.
 */
export function sameTimeIndex(row: readonly Span[], at: number): number {
  if (row.length === 0) return -1
  const on = row.findIndex((p) => p.start <= at && p.end > at)
  if (on >= 0) return on
  const next = row.findIndex((p) => p.start > at)
  return next >= 0 ? next : row.length - 1
}

/**
 * The row Up or Down lands on: the next one that way with something in it,
 * or -1 past the first or last row (the focus then leaves the guide).
 */
export function nextRow(rows: readonly (readonly Span[])[], from: number, by: 1 | -1): number {
  for (let r = from + by; r >= 0 && r < rows.length; r += by) if (rows[r].length > 0) return r
  return -1
}

/**
 * Where the window starts so programme p is on screen. A programme already
 * showing (any part of it) keeps the window; one past the right edge brings
 * its start into the window's last column but one; one off the left edge
 * starts the window on it. Never before `min` or after `max`.
 */
export function windowFor(winStart: number, winLen: number, p: Span, min: number, max: number): number {
  const clamp = (v: number) => Math.max(min, Math.min(max, v))
  const winEnd = winStart + winLen
  if (p.start < winEnd && p.end > winStart) return clamp(winStart)
  if (p.start >= winEnd) return clamp(floorTo(p.start) - Math.max(0, winLen - 2 * SLOT))
  return clamp(floorTo(p.start))
}

/** Where a time sits across the window, 0 to 100 (clipped). */
export function pct(ms: number, winStart: number, winLen: number): number {
  return Math.max(0, Math.min(100, ((ms - winStart) / winLen) * 100))
}
