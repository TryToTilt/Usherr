// Usherr Broadcast: the remote. Arrows move focus to the nearest thing that
// way (spatial.ts), OK presses it, Back closes what is open or goes back a
// page, the Options key (ContextMenu or M) and the media keys become events a
// screen can listen for (useRemote.ts). It only acts on keys: mouse and touch
// are never touched, and nothing is taken from a field being typed in.
//
// On everywhere, it only moves focus. What a TV needs on top (Back as the
// browser's back, Left at the edge for the menu) is Broadcast's and a TV
// browser's only, so Neu and Classic keep working as they always have.

import { fieldKind, keyIntent, pickFirst, pickNext, remoteTakesArrow, type Dir, type Rect } from './spatial'

export type RemoteAction = 'back' | 'options' | 'menu' | 'playpause' | 'play' | 'pause' | 'fastforward' | 'rewind' | 'next' | 'previous' | 'stop'

export interface RemoteDetail {
  action: RemoteAction
  /** What had focus when the key was pressed. */
  target: Element | null
}

/** The event every remote action is sent as, on window. Cancel it (preventDefault) to say "handled". */
export const REMOTE_EVENT = 'usherr-remote'

const FOCUSABLE = [
  'a[href]',
  'button',
  'input:not([type="hidden"])',
  'select',
  'textarea',
  'summary',
  '[tabindex]',
  '[contenteditable=""]',
  '[contenteditable="true"]',
  'audio[controls]',
  'video[controls]',
].join(',')

// What counts as something open over the page: Back closes it, arrows stay in it.
const OVERLAY = '[aria-modal="true"], dialog[open]'
// Smaller things that keep the arrows while focus is in them.
const INNER_SCOPE = '[data-remote-scope], [role="menu"], [role="listbox"], .menu-pop'

let started = false
let grabbed: HTMLInputElement | null = null

const root = () => document.documentElement
/** A TV's own browser (main.tsx marks it before the first paint). */
export const tvBrowser = () => root().dataset.tvBrowser === '1'
const broadcast = () => root().dataset.look === 'broadcast'
/** The TV extras (Back goes back, Left at the edge asks for the menu) are on. */
const tvExtras = () => broadcast() || tvBrowser()

/** The remote (or a keyboard's arrows) is being used: focus is drawn for it. */
export function remoteOn(): boolean {
  return root().dataset.remote === '1'
}

function setRemote(on: boolean) {
  if (on) root().dataset.remote = '1'
  else delete root().dataset.remote
}

/** Sends a remote action to whoever listens; true when a listener handled it. */
export function sendRemote(action: RemoteAction, target: Element | null = document.activeElement): boolean {
  const ev = new CustomEvent<RemoteDetail>(REMOTE_EVENT, { detail: { action, target }, cancelable: true })
  return !window.dispatchEvent(ev)
}

function rectOf(el: Element): Rect {
  const r = el.getBoundingClientRect()
  return { left: r.left, top: r.top, right: r.right, bottom: r.bottom }
}

function visible(el: HTMLElement): boolean {
  if (el.closest('[inert], [aria-hidden="true"], [data-remote-skip]')) return false
  const r = el.getBoundingClientRect()
  if (r.width < 1 || r.height < 1) return false
  const cs = getComputedStyle(el)
  if (cs.visibility === 'hidden' || cs.pointerEvents === 'none') return false
  // off the side of the screen: only when a row that scrolls sideways holds it (a drawer slid away does not count)
  if (r.right <= 0 || r.left >= window.innerWidth) {
    for (let p = el.parentElement; p && p !== document.body; p = p.parentElement) {
      const ox = getComputedStyle(p).overflowX
      if (ox === 'auto' || ox === 'scroll' || ox === 'hidden') {
        const pr = p.getBoundingClientRect()
        return pr.right > 0 && pr.left < window.innerWidth
      }
    }
    return false
  }
  return true
}

function focusable(el: Element): el is HTMLElement {
  if (!(el instanceof HTMLElement)) return false
  if ((el as HTMLButtonElement).disabled) return false
  if (el.tabIndex < 0) return false
  return visible(el)
}

/** The open thing on top, if any. */
function topOverlay(): HTMLElement | null {
  const all = Array.from(document.querySelectorAll<HTMLElement>(OVERLAY)).filter((d) => {
    const r = d.getBoundingClientRect()
    return r.width > 0 && r.height > 0 && getComputedStyle(d).visibility !== 'hidden'
  })
  return all.length ? all[all.length - 1] : null
}

/** The menu a button has just opened (aria-expanded), so the arrows go into it. */
function openedBy(active: Element | null): HTMLElement | null {
  if (!active || active.getAttribute('aria-expanded') !== 'true') return null
  const id = active.getAttribute('aria-controls')
  const byId = id ? document.getElementById(id) : null
  const near = byId ?? active.parentElement?.querySelector<HTMLElement>(INNER_SCOPE) ?? null
  if (!near) return null
  const r = near.getBoundingClientRect()
  return r.width > 0 && r.height > 0 ? near : null
}

/** Where the arrows may go: the open dialog or menu holding focus, else the page. */
function scopeFor(active: Element | null): HTMLElement {
  const overlay = topOverlay()
  const inner = active?.closest<HTMLElement>(INNER_SCOPE) ?? openedBy(active)
  if (inner && (!overlay || overlay.contains(inner))) return inner
  return overlay ?? document.body
}

// A card whose picture and title both link to the same page is one stop, not two.
const CARD = '.pcard, .cx-card, .sc-card, [data-remote-card]'

function repeatLink(el: Element): boolean {
  if (!(el instanceof HTMLAnchorElement)) return false
  const card = el.closest(CARD)
  if (!card) return false
  const first = Array.from(card.querySelectorAll<HTMLAnchorElement>('a[href]')).find((a) => a.href === el.href)
  return !!first && first !== el
}

function candidates(scope: HTMLElement, active: Element | null): HTMLElement[] {
  return Array.from(scope.querySelectorAll(FOCUSABLE)).filter(
    (el): el is HTMLElement => el !== active && focusable(el) && !repeatLink(el) && !(active && (el.contains(active) || active.contains(el))),
  )
}

function focusEl(el: HTMLElement) {
  el.focus({ preventScroll: true })
  el.scrollIntoView({ block: 'nearest', inline: 'nearest' })
}

/** Moves focus one step `dir`; false when there is nothing that way. */
export function moveFocus(dir: Dir): boolean {
  const active = document.activeElement && document.activeElement !== document.body ? document.activeElement : null
  const scope = scopeFor(active)
  // focus outside the open dialog (it just opened): start inside it
  if (!active || !scope.contains(active)) {
    const list = candidates(scope, null)
    const auto = list.find((el) => el.hasAttribute('autofocus') || el.hasAttribute('data-autofocus'))
    const view = { left: 0, top: 0, right: window.innerWidth, bottom: window.innerHeight }
    // on the page, start in its content rather than the top bar (a screen can say where with data-autofocus)
    const main = scope === document.body ? list.filter((el) => el.closest('main, [role="main"], .content')) : []
    const from = main.length ? main : list
    const first = auto ?? from[pickFirst(from.map(rectOf), view)] ?? list[0]
    if (!first) return false
    focusEl(first)
    return true
  }
  const list = candidates(scope, active)
  const i = pickNext(rectOf(active), list.map(rectOf), dir)
  if (i < 0) return false
  focusEl(list[i])
  return true
}

function release() {
  if (!grabbed) return
  delete grabbed.dataset.remoteGrab
  grabbed = null
}

/** A slider takes Left and Right only after OK grabs it; OK or Back lets go. */
export function sliderGrabbed(): HTMLInputElement | null {
  return grabbed
}

const NATIVE_PRESS = new Set(['A', 'BUTTON', 'INPUT', 'SELECT', 'TEXTAREA', 'SUMMARY', 'AUDIO', 'VIDEO'])
const PRESS_ROLES = new Set(['button', 'link', 'menuitem', 'menuitemradio', 'menuitemcheckbox', 'option', 'tab', 'checkbox', 'switch', 'radio', 'gridcell'])

function onKey(e: KeyboardEvent) {
  if (e.isComposing) return
  const intent = keyIntent(e)
  if (intent.kind === 'none') return
  const active = document.activeElement instanceof HTMLElement && document.activeElement !== document.body ? document.activeElement : null
  const kind = active ? fieldKind(active.tagName, active.getAttribute('type'), active.isContentEditable) : 'other'
  const typing = kind === 'typing'

  switch (intent.kind) {
    case 'move': {
      if (e.defaultPrevented || active?.closest('[data-remote-own-arrows]')) return
      if (grabbed && grabbed === active) return // the slider has the arrows
      if (active && !remoteTakesArrow(kind, intent.dir, { grabbed: false, tv: tvBrowser(), multiline: active.tagName === 'TEXTAREA' || active.isContentEditable })) return
      // Neu and Classic on a computer: arrows still scroll a page where nothing has focus
      if (!active && !tvExtras() && !topOverlay()) return
      setRemote(true)
      if (moveFocus(intent.dir)) e.preventDefault()
      else if (intent.dir === 'left' && tvExtras() && sendRemote('menu', active)) e.preventDefault()
      return
    }
    case 'ok': {
      if (!active || e.defaultPrevented) return
      if (kind === 'slider') {
        if (grabbed === active) release()
        else {
          release()
          grabbed = active as HTMLInputElement
          grabbed.dataset.remoteGrab = '1'
        }
        e.preventDefault()
        return
      }
      // a thing made pressable by its role (a div with tabindex) gets the press a button would
      const role = active.getAttribute('role')
      if (role && PRESS_ROLES.has(role) && !NATIVE_PRESS.has(active.tagName)) {
        e.preventDefault()
        active.click()
      }
      return
    }
    case 'escape': {
      if (grabbed) {
        release()
        e.preventDefault()
        return
      }
      // a TV: Back (Escape) leaves the field, so the remote is not stuck in it
      if (typing && tvBrowser() && !e.defaultPrevented && !topOverlay()) active?.blur()
      else if (remoteOn() && (topOverlay() || active?.closest(INNER_SCOPE) || openedBy(active))) refocusOpener()
      return
    }
    case 'back': {
      if (typing || e.defaultPrevented) return
      if (grabbed) {
        release()
        e.preventDefault()
        return
      }
      if (!tvExtras()) return
      if (sendRemote('back', active)) {
        e.preventDefault()
        return
      }
      const overlay = topOverlay() ?? active?.closest<HTMLElement>('[role="menu"], .menu-pop') ?? openedBy(active)
      if (overlay) {
        // close it the way it already closes: an Escape from inside it
        e.preventDefault()
        const from = active && overlay.contains(active) ? active : overlay
        from.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', code: 'Escape', bubbles: true, cancelable: true }))
        refocusOpener()
        return
      }
      if (!intent.native) {
        e.preventDefault()
        history.back()
      }
      return
    }
    case 'options': {
      if (typing || e.defaultPrevented) return
      if (sendRemote('options', active)) e.preventDefault()
      return
    }
    case 'media': {
      if (e.defaultPrevented) return
      if (sendRemote(intent.action, active)) e.preventDefault()
      return
    }
  }
}

function onPointer(e: PointerEvent) {
  release()
  // the mouse or a finger is back: focus goes back to being drawn the browser's way (a TV stays in remote mode)
  if (e.pointerType && !tvBrowser()) setRemote(false)
}

function onFocusOut(e: FocusEvent) {
  if (grabbed && e.target === grabbed) release()
}

// What had focus before it went into an open dialog or menu, to go back to when that closes.
let opener: HTMLElement | null = null

function onFocusIn(e: FocusEvent) {
  const t = e.target instanceof Element ? e.target : null
  const box = t?.closest(`${OVERLAY}, ${INNER_SCOPE}`)
  const from = e.relatedTarget
  if (box && from instanceof HTMLElement && !box.contains(from)) opener = from
}

/** After Back or Escape closed something: if focus fell to the page, put it back where it was. */
function refocusOpener() {
  const back = opener
  if (!back) return
  setTimeout(() => {
    const now = document.activeElement
    if ((!now || now === document.body) && back.isConnected && focusable(back)) back.focus({ preventScroll: true })
    opener = null
  }, 60)
}

/** Starts the remote once for the page. */
export function startRemote() {
  if (started || typeof window === 'undefined') return
  started = true
  if (tvBrowser()) setRemote(true)
  // bubbling, on window: the page's own handlers run first and can keep a key (preventDefault)
  window.addEventListener('keydown', onKey)
  window.addEventListener('pointerdown', onPointer, { capture: true, passive: true })
  window.addEventListener('focusout', onFocusOut, true)
  window.addEventListener('focusin', onFocusIn, true)
}
