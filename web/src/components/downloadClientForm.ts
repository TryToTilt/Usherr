// What the download client card shows after the settings are read again.
//
// The card reloads when another card changes where torrents go. A reload must
// not throw away what the person has typed and not saved yet, so a field only
// follows the server while it is still what the last saved copy had.
//
// form is what the card shows now, was the saved copy it was last in step with
// (the blank form before the first load), and now what the server has.
export function reloadForm<T extends object>(form: T, was: T, now: T): T {
  const out = { ...now }
  for (const key of Object.keys(now) as (keyof T)[]) {
    if (form[key] !== was[key]) out[key] = form[key]
  }
  return out
}
