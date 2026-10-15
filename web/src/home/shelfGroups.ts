// Home fork: how the Library's shelves are cut, for movies, shows and anime.
import { t } from '../i18n'
import type { Shelf } from './Shelves'
import { compareTitles, sortTitle } from './sortTitle'

export type RowsBy = 'smart' | 'genre' | 'decade' | 'mood' | 'collection' | 'status' | 'az' | 'added'

export interface ShelfTitle {
  key: string
  id: number
  tmdbId: number
  title: string
  year: number
  genres: string[]
  quality?: string
  status: string // missing, downloading, downloaded, partial
  have: number
  total: number
  streamedId?: number
  showStatus?: string
}

export const MOODS = ['feel-good', 'edge-of-seat', 'mind-bending', 'dark', 'laugh', 'family', 'comfort', 'tearjerker', 'romantic', 'epic', 'scary', 'true-story'] as const
export type Mood = (typeof MOODS)[number]

export function moodName(m: string): string {
  switch (m) {
    case 'feel-good':
      return t('Feel-good')
    case 'edge-of-seat':
      return t('Edge of your seat')
    case 'mind-bending':
      return t('Mind-bending')
    case 'dark':
      return t('Dark & gritty')
    case 'laugh':
      return t('Laugh out loud')
    case 'family':
      return t('Family night')
    case 'comfort':
      return t('Comfort rewatch')
    case 'tearjerker':
      return t('Tearjerker')
    case 'romantic':
      return t('Romantic')
    case 'epic':
      return t('Epic')
    case 'scary':
      return t('Scary')
    default:
      return t('True story')
  }
}

export function rowsByOptions(kind: 'movie' | 'tv'): { value: RowsBy; label: string }[] {
  const base: { value: RowsBy; label: string }[] = [
    { value: 'smart', label: t('Smart') },
    { value: 'genre', label: t('Genre') },
    { value: 'decade', label: t('Decade') },
    { value: 'mood', label: t('Mood') },
  ]
  if (kind === 'movie') base.push({ value: 'collection', label: t('Collection') })
  else base.push({ value: 'status', label: t('Still running / ended') })
  base.push({ value: 'az', label: t('A–Z') }, { value: 'added', label: t('Recently added') })
  return base
}

const LETTER_GROUPS: [string, RegExp][] = [
  ['A–B', /^[ab]/i],
  ['C–D', /^[cd]/i],
  ['E–G', /^[e-g]/i],
  ['H–K', /^[h-k]/i],
  ['L–M', /^[lm]/i],
  ['N–P', /^[n-p]/i],
  ['Q–S', /^[q-s]/i],
  ['T', /^t/i],
  ['U–Z', /^[u-z]/i],
]

const byTitle = <T extends ShelfTitle>(a: T, b: T) => compareTitles(a.title, b.title)
const newestFirst = <T extends ShelfTitle>(a: T, b: T) => b.year - a.year || byTitle(a, b)
// Recently added: the library's own ids grow as titles are added; streamed
// titles (negative ids) come after.
const addedFirst = <T extends ShelfTitle>(a: T, b: T) => (b.streamedId ? -1 : b.id) - (a.streamedId ? -1 : a.id) || (b.streamedId ?? 0) - (a.streamedId ?? 0)

function genreRows<T extends ShelfTitle>(items: T[], seeAll?: (g: string) => void, limit = 0): Shelf<T>[] {
  const by = new Map<string, T[]>()
  for (const i of items) for (const g of i.genres) by.set(g, [...(by.get(g) ?? []), i])
  const rows = [...by.entries()].sort((a, b) => b[1].length - a[1].length)
  return (limit ? rows.slice(0, limit) : rows).map(([g, list]) => ({ key: `genre:${g}`, title: g, items: list.sort(newestFirst), seeAll: seeAll ? () => seeAll(g) : undefined }))
}

export interface ShelfHelpers {
  seeGenre?: (g: string) => void
  seeDecade?: (d: number) => void
  seeCollection?: (id: number) => void
  collections?: { id: number; name: string; movieIds: number[] }[]
  moods?: Record<number, string[]> // TMDB id -> moods
}

export function buildShelves<T extends ShelfTitle>(items: T[], kind: 'movie' | 'tv', by: RowsBy, h: ShelfHelpers): Shelf<T>[] {
  const nonEmpty = (rows: Shelf<T>[]) => rows.filter((r) => r.items.length > 0)
  switch (by) {
    case 'genre':
      return genreRows(items, h.seeGenre)
    case 'decade': {
      const by = new Map<number, T[]>()
      for (const i of items) {
        const d = i.year > 0 ? Math.floor(i.year / 10) * 10 : 0
        by.set(d, [...(by.get(d) ?? []), i])
      }
      return [...by.entries()]
        .sort((a, b) => (a[0] === 0 ? 1 : b[0] === 0 ? -1 : b[0] - a[0]))
        .map(([d, list]) => ({ key: `decade:${d}`, title: d ? t('{decade}s', { decade: String(d) }) : t('Year unknown'), items: list.sort(newestFirst), seeAll: d && h.seeDecade ? () => h.seeDecade!(d) : undefined }))
    }
    case 'az': {
      const rows: Shelf<T>[] = [{ key: 'az:#', title: '#', items: [] }, ...LETTER_GROUPS.map(([l]) => ({ key: `az:${l}`, title: l, items: [] as T[] }))]
      for (const i of [...items].sort(byTitle)) {
        const s = sortTitle(i.title)
        const n = LETTER_GROUPS.findIndex(([, re]) => re.test(s))
        rows[n + 1].items.push(i)
      }
      return nonEmpty(rows)
    }
    case 'added':
      return nonEmpty([{ key: 'added', title: t('Recently added'), items: [...items].sort(addedFirst) }])
    case 'collection': {
      const have = new Map(items.map((i) => [i.id, i]))
      return (h.collections ?? [])
        .map((c) => ({ c, list: c.movieIds.map((id) => have.get(id)).filter((x): x is T => !!x) }))
        .filter((x) => x.list.length >= 2)
        .sort((a, b) => b.list.length - a.list.length || a.c.name.localeCompare(b.c.name))
        .map(({ c, list }) => ({ key: `col:${c.id}`, title: c.name, items: list.sort((a, b) => a.year - b.year), seeAll: h.seeCollection ? () => h.seeCollection!(c.id) : undefined }))
    }
    case 'status':
      return nonEmpty([
        { key: 'st:continuing', title: t('Still running'), items: items.filter((i) => i.showStatus === 'continuing').sort(byTitle) },
        { key: 'st:upcoming', title: t('Coming soon'), items: items.filter((i) => i.showStatus === 'upcoming').sort(byTitle) },
        { key: 'st:ended', title: t('Ended'), items: items.filter((i) => i.showStatus === 'ended').sort(byTitle) },
        { key: 'st:unknown', title: t('Not known yet'), items: items.filter((i) => !i.showStatus).sort(byTitle) },
      ])
    case 'mood': {
      const moods = h.moods ?? {}
      return nonEmpty(MOODS.map((m) => ({ key: `mood:${m}`, title: moodName(m), items: items.filter((i) => (moods[i.tmdbId] ?? []).includes(m)).sort(newestFirst) })))
    }
    default: {
      // Smart: the rows usually worth a look.
      const year = new Date().getFullYear()
      const rows: Shelf<T>[] = [
        { key: 'smart:added', title: t('Recently added'), items: [...items].sort(addedFirst).slice(0, 60) },
        { key: 'smart:new', title: kind === 'movie' ? t('New releases') : t('New shows'), items: items.filter((i) => i.year >= year - 1).sort(newestFirst) },
        { key: 'smart:coming', title: t('On the way'), items: items.filter((i) => !i.streamedId && (i.status === 'downloading' || i.status === 'missing')).sort(byTitle) },
      ]
      if (kind === 'tv') {
        rows.push({ key: 'smart:running', title: t('Still running'), items: items.filter((i) => i.showStatus === 'continuing').sort(byTitle) })
        rows.push({ key: 'smart:partial', title: t('Partly downloaded'), items: items.filter((i) => i.total > 0 && i.have > 0 && i.have < i.total).sort(byTitle) })
      } else {
        rows.push({ key: 'smart:4k', title: t('4K'), items: items.filter((i) => /2160|4k/i.test(i.quality ?? '')).sort(newestFirst) })
      }
      rows.push({ key: 'smart:streams', title: t('Streams'), hint: t('not on disk, plays from Usenet'), items: items.filter((i) => !!i.streamedId).sort(byTitle) })
      rows.push(...genreRows(items, h.seeGenre, 5))
      rows.push({ key: 'smart:classics', title: t('Classics'), hint: t('before 1980'), items: items.filter((i) => i.year > 0 && i.year < 1980).sort(newestFirst) })
      return nonEmpty(rows)
    }
  }
}
