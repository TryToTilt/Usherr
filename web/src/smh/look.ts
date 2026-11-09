// Usherr Broadcast: Show Me How in each look. Broadcast has its own menu,
// settings pages and title pages (the More sheet), so a step can be for one
// look only (look: 'broadcast' | 'other'), and its words can differ (bc).
// Classic and Neu play every walkthrough exactly as before: steps marked
// 'broadcast' are left out there, nothing else changes.
//
// With the remote likely in hand, a step that waits for a press says which
// button: OK, Options for the More sheet, Left for the menu.
//
// No React and no page here, so it can be tested on its own (look.test.ts).
import { t } from '../i18n.ts'
import { extrasMovedTo } from '../onemenu/broadcastNav.ts'
import type { SmhCtx, SmhLook, SmhStep, SmhText, Walkthrough } from './types.ts'

export const lookOf = (broadcast: boolean | undefined): SmhLook => (broadcast ? 'broadcast' : 'other')

// The steps (or walkthroughs) for this look: those without a look, and those for it.
export function forLook<T extends { look?: SmhLook }>(list: readonly T[], broadcast: boolean | undefined): T[] {
  const look = lookOf(broadcast)
  return list.filter((x) => !x.look || x.look === look)
}

// Whether a walkthrough is listed in this look.
export const walkInLook = (w: Pick<Walkthrough, 'look'>, broadcast: boolean | undefined) => !w.look || w.look === lookOf(broadcast)

const say = (x: SmhText, c: SmhCtx) => (typeof x === 'function' ? x(c) : x)

// Words that differ by look: Broadcast's, then Classic and Neu's (as before).
export const bc =
  (broadcast: SmhText, other: SmhText): SmhText =>
  (c) =>
    say(c.broadcast ? broadcast : other, c)

// A page that differs by look (a settings page that moved in Broadcast).
export const bcTo =
  (broadcast: string, other: string) =>
  (c: SmhCtx): string =>
    c.broadcast ? broadcast : other

// Which remote button a step wants, or '' when it wants none (or the
// remote isn't in hand). A step's own remote words win; otherwise a step
// that waits for something to be pressed says OK. Nothing while Usherr
// presses it itself ("Watch me do it").
export function remoteHint(step: SmhStep, c: SmhCtx): string {
  if (!c.remote) return ''
  if (step.remote !== undefined) return say(step.remote, c)
  if (step.kind === 'point') {
    if (c.watch && step.act) return ''
    const until = step.until ?? 'click'
    return until === 'next' ? '' : t('Press OK on it')
  }
  if (step.kind === 'go' && step.via && !c.watch) return t('Press OK on it')
  return ''
}

// The remote's words, for the steps that use them.
export const PRESS = {
  ok: () => t('Press OK on it'),
  more: () => t('Press Options (☰) for More'),
  menu: () => t('Press Left for the menu'),
  back: () => t('Press Back to close it'),
}

// A Doctor fix (or any link) on Settings > Extras goes straight to its card's
// own page in Broadcast (the server names the Extras card; other looks keep
// Extras as it is).
export function fixPath(path: string, broadcast: boolean | undefined): string {
  if (!broadcast || !path.startsWith('/settings/extras')) return path
  const at = path.indexOf('#')
  return extrasMovedTo(at < 0 ? '' : path.slice(at))
}
