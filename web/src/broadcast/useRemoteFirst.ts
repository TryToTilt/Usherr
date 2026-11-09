// Usherr Broadcast: like useRemote (remote/useRemote.ts), but heard before
// every other listener and kept from them once handled. For what sits on top
// of a page (a reader, a sheet), so the page behind doesn't act on the same
// Back or Options press.
import { useEffect, useRef } from 'react'
import { REMOTE_EVENT, type RemoteAction, type RemoteDetail } from '../remote'

export function useRemoteFirst(handler: (action: RemoteAction, target: Element | null) => boolean | void, enabled = true) {
  const ref = useRef(handler)
  useEffect(() => {
    ref.current = handler
  })
  useEffect(() => {
    if (!enabled) return
    const on = (e: Event) => {
      const d = (e as CustomEvent<RemoteDetail>).detail
      if (ref.current(d.action, d.target) === true) {
        e.preventDefault()
        e.stopImmediatePropagation()
      }
    }
    // capture: on the window itself, capturing listeners are called before the others
    window.addEventListener(REMOTE_EVENT, on, true)
    return () => window.removeEventListener(REMOTE_EVENT, on, true)
  }, [enabled])
}
