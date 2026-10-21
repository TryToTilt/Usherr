import { compareVersions } from './whatsNew.ts'

// Error texts that a later version already fixed. Where an error is shown, a
// short line tells people on an older version to update first. Add a row
// here when a fix ships for an error people can see.
export interface KnownFix {
  pattern: RegExp
  fixedIn: string
  note: string // for maintainers: what was fixed
}

export const KNOWN_FIXES: KnownFix[] = [
  {
    pattern: /wants to connect to .+ which is not one of the site'?s own addresses/i,
    fixedIn: '3.2.1',
    note: 'Sites without a login no longer fail on an address outside their own list.',
  },
  {
    pattern: /more than 32 filters/i,
    fixedIn: '3.2.1',
    note: 'Site definitions with a long filter chain are supported.',
  },
  {
    pattern: /XML syntax error on line \d+:?.*unexpected end element <\/link>/i,
    fixedIn: '3.2.1',
    note: 'Feeds with an unclosed link tag are read.',
  },
]

// The version that fixed this error text, when the running version is older
// than it. Empty when nothing is known or the fix is already installed.
export function fixedInNewerVersion(text: string | undefined | null, running: string, table: KnownFix[] = KNOWN_FIXES): string {
  if (!text || !running) return ''
  for (const row of table) {
    if (row.pattern.test(text) && compareVersions(running, row.fixedIn) < 0) return row.fixedIn
  }
  return ''
}
