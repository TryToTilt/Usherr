// Usherr Broadcast: rows a remote changes with Left and Right (Your settings,
// the reader settings sheet, the switches on settings pages). Only the pure
// part lives here, so it can be tested on its own (choice.test.ts).

export interface Choice<T extends string = string> {
  id: T
  label: string
  disabled?: boolean
}

/** The choice `dir` steps from `current`, going round at either end and skipping disabled ones. */
export function cycleChoice<T extends string>(list: readonly Choice<T>[], current: T, dir: 1 | -1): T {
  const n = list.length
  if (n === 0) return current
  const found = list.findIndex((c) => c.id === current)
  const at = found >= 0 ? found : dir > 0 ? -1 : n // an unknown value starts at the first (or, going back, the last)
  for (let step = 1; step <= n; step++) {
    const i = (((at + dir * step) % n) + n) % n
    if (!list[i].disabled) return list[i].id
  }
  return current
}

/** Left (-1) or Right (+1) for a key; 0 for any other key. */
export function arrowDir(key: string): 1 | -1 | 0 {
  if (key === 'ArrowRight' || key === 'Right') return 1
  if (key === 'ArrowLeft' || key === 'Left') return -1
  return 0
}

/**
 * Whether Left or Right should flip an on/off switch: Right turns it on, Left
 * turns it off. When it is already that way, the key is left to the remote, so
 * it still moves on (or opens the menu at the left edge).
 */
export function switchFlips(checked: boolean, dir: 1 | -1): boolean {
  return dir > 0 ? !checked : checked
}
