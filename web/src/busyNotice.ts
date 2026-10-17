// When the "slow to answer" notice shows and hides, kept apart from busyState.ts
// (which only says whether the app is slow right now). The raw state flips on
// and off with every read that finishes, so showing it as it is made the notice
// flash. Here it has to hold for a few seconds before it appears, and the app
// has to answer normally for a moment before it goes away.
//
// No React in this file, so it can be tested on its own (see useBusyNotice.ts for the hook).

// The app must have been slow this long before the notice shows.
export const SHOW_AFTER_MS = 3000
// It must have answered normally this long before the notice goes away.
export const HIDE_AFTER_MS = 2500

export interface NoticeState {
  // Whether the notice is on screen.
  shown: boolean
  // When the real state first differed from what is on screen; null while they agree.
  since: number | null
}

export const INITIAL_NOTICE: NoticeState = { shown: false, since: null }

// The next state, given whether the app is slow right now and what time it is.
// Returns the same object when nothing changed.
export function stepNotice(state: NoticeState, busy: boolean, now: number): NoticeState {
  if (busy === state.shown) return state.since === null ? state : { shown: state.shown, since: null }
  const since = state.since ?? now
  if (now - since >= (busy ? SHOW_AFTER_MS : HIDE_AFTER_MS)) return { shown: busy, since: null }
  return state.since === since ? state : { shown: state.shown, since }
}

// How long to wait before asking again, or null when nothing is pending.
export function noticeWait(state: NoticeState, busy: boolean, now: number): number | null {
  if (state.since === null || busy === state.shown) return null
  return Math.max(0, state.since + (busy ? SHOW_AFTER_MS : HIDE_AFTER_MS) - now)
}
