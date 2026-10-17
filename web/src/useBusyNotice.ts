import { useEffect, useState } from 'react'
import { useBusy } from './busy'
import { INITIAL_NOTICE, noticeWait, stepNotice } from './busyNotice'

// Whether to show the "slow to answer" notice: the app has to be slow for a few
// seconds before it shows, and answer normally for a moment before it goes away
// (see busyNotice.ts).
export function useBusyNotice(): boolean {
  const busy = useBusy()
  const [state, setState] = useState(INITIAL_NOTICE)
  useEffect(() => {
    const next = stepNotice(state, busy, Date.now())
    if (next !== state) {
      setState(next)
      return
    }
    const wait = noticeWait(state, busy, Date.now())
    if (wait === null) return
    // A timer can fire a hair early, so look a little after the time is up.
    const timer = setTimeout(() => setState((cur) => stepNotice(cur, busy, Date.now())), wait + 25)
    return () => clearTimeout(timer)
  }, [busy, state])
  return state.shown
}
