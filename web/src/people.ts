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

// Usherr: sorting and narrowing a filmography (a person's page). The list
// comes most popular first from the server, so 'popular' keeps that order.
export type CreditSort = 'popular' | 'newest' | 'oldest' | 'rated' | 'az'
export type CreditHave = 'all' | 'have' | 'missing'

export interface CreditView {
  sort: CreditSort
  genre: string // '' for every genre
  query: string // part of a title
  have: CreditHave
}

interface Arrangeable {
  tmdbId: number
  title: string
  year?: number
  releaseDate?: string
  genres?: string[]
  rating?: number
  voteCount?: number
}

// A rating from a handful of votes says little: it sorts after the rest.
const FEW_VOTES = 25

function when(c: Arrangeable): string {
  return c.releaseDate || (c.year ? `${c.year}` : '')
}

// Lower case and without accents, so "pokemon" finds "Pokémon".
function plain(s: string): string {
  return s.normalize('NFD').replace(/[\u0300-\u036f]/g, '').toLowerCase()
}

export function arrangeCredits<T extends Arrangeable>(items: T[], v: CreditView, owned: (id: number) => boolean): T[] {
  const q = plain(v.query.trim())
  const out = items.filter(
    (c) => (!v.genre || (c.genres ?? []).includes(v.genre)) && (!q || plain(c.title).includes(q)) && (v.have === 'all' || owned(c.tmdbId) === (v.have === 'have')),
  )
  switch (v.sort) {
    case 'newest':
    case 'oldest': {
      // Undated titles (announced, not yet dated) go last either way.
      const dir = v.sort === 'newest' ? -1 : 1
      return out.sort((a, b) => {
        const x = when(a)
        const y = when(b)
        if (!x || !y) return x ? -1 : y ? 1 : 0
        return x < y ? -dir : x > y ? dir : 0
      })
    }
    case 'rated':
      return out.sort((a, b) => {
        const fa = (a.voteCount ?? 0) < FEW_VOTES
        const fb = (b.voteCount ?? 0) < FEW_VOTES
        if (fa !== fb) return fa ? 1 : -1
        return (b.rating ?? 0) - (a.rating ?? 0)
      })
    case 'az':
      return out.sort((a, b) => a.title.localeCompare(b.title, undefined, { sensitivity: 'base', numeric: true }))
    default:
      return out
  }
}

// The genres across a filmography, the most common first, for the Genre menu.
export function creditGenres(items: { genres?: string[] }[]): string[] {
  const count = new Map<string, number>()
  for (const c of items) for (const g of c.genres ?? []) count.set(g, (count.get(g) ?? 0) + 1)
  return [...count.entries()].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0])).map(([g]) => g)
}

export function creditSortFrom(v: string | null): CreditSort {
  return v === 'newest' || v === 'oldest' || v === 'rated' || v === 'az' ? v : 'popular'
}

export function creditHaveFrom(v: string | null): CreditHave {
  return v === 'have' || v === 'missing' ? v : 'all'
}
