// Small pure helpers for the person page and the cast rows, kept apart so they can be tested.
import { locale as appLocale } from './i18n.ts'

export type RoleFilter = 'all' | 'acting' | 'directing' | 'writing'

// How many people the cast row shows before "Show all", and how many titles a
// filmography shows before "Show more".
export const CAST_FIRST = 12
export const CREDITS_FIRST = 60

export function personPath(id: number | undefined): string | undefined {
  return id && id > 0 ? `/person/${id}` : undefined
}

// The titles for one kind of work; "all" keeps everything, including the odd
// producing and camera credits that have no filter of their own.
export function filterByRole<T extends { roles: string[] }>(items: T[], role: RoleFilter): T[] {
  return role === 'all' ? items : items.filter((it) => it.roles.includes(role))
}

// The filter is only worth showing when there is something to choose between.
export function showRoleFilter(workKinds: string[]): boolean {
  return workKinds.length >= 2
}

// A role filter that the person has no work for (a stale address) falls back to everything.
export function roleFilterFrom(value: string | null, workKinds: string[]): RoleFilter {
  return value === 'acting' || value === 'directing' || value === 'writing' ? (workKinds.includes(value) ? value : 'all') : 'all'
}

// The first `shown` of a list, and how many are left behind "Show more".
export function firstOf<T>(items: T[], shown: number): { visible: T[]; hidden: number } {
  const visible = items.slice(0, Math.max(0, shown))
  return { visible, hidden: items.length - visible.length }
}

// The directors, writers or creators out of a title's crew list, without anyone twice.
export function crewWith<T extends { name: string; job: string }>(crew: T[] | undefined, job: string): T[] {
  const seen = new Set<string>()
  return (crew ?? []).filter((c) => {
    if (c.job !== job || seen.has(c.name)) return false
    seen.add(c.name)
    return true
  })
}

// A biography long enough to fold up behind "Show more".
export function bioIsLong(bio: string | undefined, limit = 420): boolean {
  return (bio?.length ?? 0) > limit
}

// "2 September 1964" in the language of the app; the plain text when it isn't a date.
export function longDate(iso: string | undefined, locale = appLocale()): string {
  if (!iso) return ''
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(iso)
  if (!m) return iso
  const d = new Date(Date.UTC(Number(m[1]), Number(m[2]) - 1, Number(m[3])))
  if (Number.isNaN(d.getTime())) return iso
  return new Intl.DateTimeFormat(locale, { day: 'numeric', month: 'long', year: 'numeric', timeZone: 'UTC' }).format(d)
}

// Their age, or the age they died at; undefined when the birthday is unknown or the dates make no sense.
export function ageOf(birthday: string | undefined, deathday: string | undefined, today = new Date()): number | undefined {
  const b = /^(\d{4})-(\d{2})-(\d{2})$/.exec(birthday ?? '')
  if (!b) return undefined
  const end = /^(\d{4})-(\d{2})-(\d{2})$/.exec(deathday ?? '')
  const [ey, em, ed] = end ? [Number(end[1]), Number(end[2]), Number(end[3])] : [today.getFullYear(), today.getMonth() + 1, today.getDate()]
  let age = ey - Number(b[1])
  if (em < Number(b[2]) || (em === Number(b[2]) && ed < Number(b[3]))) age--
  return age >= 0 && age < 130 ? age : undefined
}
