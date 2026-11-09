// Usherr Broadcast: a reader with a remote (Broadcast only; Classic and Neu
// read exactly as before). The reader itself takes focus when it opens and
// keeps the arrows (data-remote-own-arrows): Left and Right turn the page the
// way the book reads, Fast forward and Rewind too, Down goes on, Up shows the
// bar and moves to it (Down comes back to the page), OK shows or hides the
// bar, Options opens the reader settings sheet, Back closes the sheet, leaves
// the bar, then closes the reader.
//
//   const remote = useBroadcastReader({ turn, rtl, vertical, bar, setBar, sheet, setSheet, close })
//   <div className="cx-reader" {...remote.rootProps}> … <div data-reader-bar> … </div> … </div>
//
// A reader's own key handlers start with `if (remoteHasReaderKey(e)) return`.
import { useCallback, useEffect, useMemo, useRef, type KeyboardEvent } from 'react'
import { broadcastOn } from '../neu/look'
import { useRemoteFirst } from '../broadcast/useRemoteFirst'
import { moveFocus, type Dir } from '../remote'
import { readerKey, remoteTurn } from './remoteReading'
import '../broadcast/screens.css'

export interface BroadcastReaderOptions {
  /** Turn a page (or a screen, in a long scroll): 1 on in the book, -1 back. */
  turn: (d: 1 | -1) => void
  rtl: boolean
  vertical?: boolean
  /** Whether the bar along the top shows, and how to change that (left out: it always shows). */
  bar?: boolean
  setBar?: (on: boolean) => void
  /** The reader settings sheet. */
  sheet: boolean
  setSheet: (on: boolean) => void
  /** Closes the reader's own innermost open thing (a footnote, a panel); false when nothing was open. */
  dismiss?: () => boolean
  close: () => void
}

/** True when Broadcast's remote has (or will have) this key, so a reader's own handler leaves it. */
export function remoteHasReaderKey(e: Event): boolean {
  if (!broadcastOn()) return false
  if (e.defaultPrevented) return true
  const el = e.target instanceof Element ? e.target : null
  return !!el?.closest('[data-reader-root], [data-bsheet]')
}

const ARROWS: Record<string, Dir> = { ArrowUp: 'up', ArrowDown: 'down', ArrowLeft: 'left', ArrowRight: 'right' }
// a reader's open panels (settings, bookmarks, contents), which the arrows go into from the bar
const PANELS = '.cx-marks-panel, .rdr-panel'
const FOCUSABLE = 'button:not(:disabled), a[href], select, input:not([type="hidden"]), [tabindex]:not([tabindex="-1"])'

export function useBroadcastReader(o: BroadcastReaderOptions) {
  const on = useMemo(() => broadcastOn(), []) // the look never changes without a reload
  const live = useRef(o)
  live.current = o
  const root = useRef<HTMLDivElement | null>(null)

  const toPage = useCallback(() => root.current?.focus({ preventScroll: true }), [])
  const toBar = useCallback(() => {
    if (live.current.setBar && !live.current.bar) live.current.setBar(true)
    // after the bar has drawn
    window.setTimeout(() => root.current?.querySelector<HTMLElement>('[data-reader-bar]')?.querySelector<HTMLElement>(FOCUSABLE)?.focus({ preventScroll: true }), 30)
  }, [])
  const inside = () => {
    const a = document.activeElement
    return !!a && a !== root.current && !!root.current?.contains(a)
  }
  // Back: the sheet first, then what the reader has open, then out of the bar, then the reader.
  const back = useCallback(() => {
    const l = live.current
    if (l.sheet) l.setSheet(false)
    else if (l.dismiss?.()) toPage()
    else if (inside()) toPage()
    else l.close()
  }, [toPage])

  // the reader has focus as it opens (see the ref below), and again when its sheet closes
  const hadSheet = useRef(false)
  useEffect(() => {
    if (on && hadSheet.current && !o.sheet) toPage()
    hadSheet.current = o.sheet
  }, [on, o.sheet, toPage])

  useRemoteFirst((action) => {
    const l = live.current
    if (action === 'back') {
      back()
      return true
    }
    if (action === 'options') {
      l.setSheet(!l.sheet)
      return true
    }
    const d = remoteTurn(action)
    if (d) {
      if (!l.sheet) l.turn(d)
      return true
    }
    if (action === 'menu') return true // Left at the edge of the bar: the menu stays shut while reading
    return undefined
  }, on)

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (!on || e.altKey || e.ctrlKey || e.metaKey) return
    const target = e.target as Element
    if (target.closest('[data-bsheet]')) return // the sheet has its own keys
    const done = () => {
      e.preventDefault()
      e.stopPropagation()
    }
    if (e.key === 'Escape') {
      done()
      back()
      return
    }
    if (target !== root.current) {
      // A button in the bar or a panel: the arrows move between them (the
      // reader owns its arrows, so the remote leaves them to it). Down from the
      // bar, or past the last one, goes back to the page. Fields keep their keys.
      const dir = ARROWS[e.key]
      if (!dir || target.closest('input, select, textarea, [contenteditable="true"]')) return
      done()
      if (dir === 'down' && target.closest('[data-reader-bar]') && !root.current?.querySelector(PANELS)) toPage()
      else if (!moveFocus(dir) && dir === 'down') toPage()
      return
    }
    const l = live.current
    const m = readerKey(e.key, { rtl: l.rtl, vertical: !!l.vertical })
    if (!m) return
    done()
    if (m.kind === 'turn') l.turn(m.d)
    else if (m.kind === 'bar') toBar()
    else if (m.kind === 'toggle') {
      if (l.setBar) l.setBar(!l.bar)
      else toBar()
    } else back()
  }

  // When the reader is drawn (it may say "Opening…" first) it takes focus, unless something in it already has.
  const attach = useCallback((el: HTMLDivElement | null) => {
    root.current = el
    if (el) window.setTimeout(() => !el.contains(document.activeElement) && el.focus({ preventScroll: true }), 0)
  }, [])

  const rootProps = on
    ? {
        ref: attach,
        tabIndex: -1,
        'data-reader-root': '',
        'data-remote-own-arrows': '',
        'aria-modal': true as const,
        onKeyDown,
      }
    : {}
  return { on, rootProps, openSheet: () => o.setSheet(true) }
}
