// Usherr: this fork's name and links. Upstream's text says "Mediarium"; it is
// shown as Usherr where it's displayed (i18n.ts runs every phrase through
// brandText), so upstream's sentences and translations merge unchanged. An
// address ("github.com/rdborg/Mediarium") keeps the name, and so does the
// credit to the project Usherr is built on.

export const BRAND = 'Usherr'
export const UPSTREAM = 'Mediarium'
export const UPSTREAM_URL = 'https://github.com/rdborg/Mediarium'
export const REPO_URL = 'https://github.com/TryToTilt/Usherr'
export const ISSUES_URL = `${REPO_URL}/issues`
export const DISCUSSIONS_URL = `${REPO_URL}/discussions`

// Upstream's private reports and anonymous usage numbers go to mediarium.app;
// Usherr sends neither (the server refuses them too).
export const PRIVATE_REPORTS = false
export const USAGE_STATS = false

// "Mediarium" as a word, not inside an address or a longer name.
const NAME = /(?<![\w/.@-])Mediarium(?![\w/-])/g

export function brandText(text: string): string {
  return text.includes(UPSTREAM) ? text.replace(NAME, BRAND) : text
}
