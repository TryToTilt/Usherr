// Usherr Broadcast: what a screen uses to hear the remote (remote.ts).
//
//   useRemote((action, target) => { if (action === 'playpause') { toggle(); return true } })
//   useRemoteOptions((target) => openMoreSheet(target))
//
// Return true from the handler when the screen handled the key: the browser's
// own behaviour (and the remote's, such as Back going back a page) is then skipped.
import { useEffect, useRef } from 'react'
import { REMOTE_EVENT, type RemoteAction, type RemoteDetail } from './remote'

export type RemoteHandler = (action: RemoteAction, target: Element | null) => boolean | void

/** Listens to every remote action while the component is shown. */
export function useRemote(handler: RemoteHandler, enabled = true) {
  const ref = useRef(handler)
  useEffect(() => {
    ref.current = handler
  })
  useEffect(() => {
    if (!enabled) return
    const on = (e: Event) => {
      const d = (e as CustomEvent<RemoteDetail>).detail
      if (ref.current(d.action, d.target) === true) e.preventDefault()
    }
    window.addEventListener(REMOTE_EVENT, on)
    return () => window.removeEventListener(REMOTE_EVENT, on)
  }, [enabled])
}

/** The Options key (ContextMenu, M): the "More" choices for what has focus. */
export function useRemoteOptions(handler: (target: Element | null) => boolean | void, enabled = true) {
  useRemote((action, target) => (action === 'options' ? handler(target) : undefined), enabled)
}
