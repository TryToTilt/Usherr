import { api, ApiError, type DiscoverMovie } from './api'
import { locale, t } from './i18n.ts'

export type Kind = 'movie' | 'tv'
// watched, collections and season are the home fork's rails (home/DiscoverExtras.tsx).
export type ListName = 'trending' | 'popular' | 'upcoming' | 'similar' | 'watched' | 'collections' | 'season' | 'anime-airing' | 'anime-popular' | 'anime-top' | 'anime-new' | 'recipe' | 'foryou' // anime-*, recipe, foryou: home fork

// Where a Discover rail or page gets its titles: one of TMDB's lists, or a
// filtered browse (genre, years, order).
export interface Source {
  kind: Kind
  list: ListName | 'browse'
  genre?: string
  yearFrom?: string
  yearTo?: string
  sort?: string
  // "More like your library" only: also list titles from before the last 15 years.
  older?: boolean
  // "Because you watched" only (home fork): whose watching, a media server user id.
  who?: string
  // The season's rail only (home fork): which season, for its title.
  season?: string
  // A recipe (home fork): its name and its filters, as JSON.
  name?: string
  filters?: string
}

export interface Page {
  results: DiscoverMovie[]
  totalPages: number
  totalResults?: number
}

// The lists older servers already had, used when /discover/list is missing.
async function legacyList(kind: Kind, list: ListName): Promise<DiscoverMovie[]> {
  if (list === 'trending') return kind === 'movie' ? api.discoverTrending() : api.discoverTrendingTV()
  if (list === 'popular') return kind === 'movie' ? api.discoverPopular() : api.discoverPopularTV()
  return []
}

export async function fetchPage(src: Source, page: number): Promise<Page> {
  if (src.list === 'recipe') {
    // home fork: a Discover recipe
    const r = await fetch(`/api/home/recipes/titles?${new URLSearchParams({ kind: src.kind, filters: src.filters ?? '{}', page: String(page) })}`, { credentials: 'include' })
    if (!r.ok) throw new ApiError(r.status, ((await r.json().catch(() => ({}))) as { error?: string }).error ?? r.statusText)
    return (await r.json()) as Page
  }
  if (src.list === 'browse') {
    return api.discoverBrowse({ kind: src.kind, genre: src.genre, yearFrom: src.yearFrom, yearTo: src.yearTo, sort: src.sort, page })
  }
  try {
    return await api.discoverList(src.kind, src.list, page, src.older, src.who)
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) {
      return { results: page === 1 ? await legacyList(src.kind, src.list) : [], totalPages: 1 }
    }
    throw e
  }
}

export function sourceToQuery(src: Source): string {
  const q = new URLSearchParams()
  for (const [k, v] of Object.entries(src)) if (v) q.set(k, String(v))
  return q.toString()
}

export function sourceFromQuery(q: URLSearchParams): Source {
  const kind = q.get('kind') === 'tv' ? 'tv' : 'movie'
  const list = (['trending', 'popular', 'upcoming', 'similar', 'watched', 'collections', 'season', 'anime-airing', 'anime-popular', 'anime-top', 'anime-new', 'recipe', 'foryou', 'browse'] as const).find((l) => l === q.get('list')) ?? 'popular'
  return {
    kind,
    list,
    genre: q.get('genre') ?? undefined,
    yearFrom: q.get('yearFrom') ?? undefined,
    yearTo: q.get('yearTo') ?? undefined,
    sort: q.get('sort') ?? undefined,
    older: q.get('older') === 'true' || undefined,
    who: q.get('who') ?? undefined,
    season: q.get('season') ?? undefined,
    name: q.get('name') ?? undefined,
    filters: q.get('filters') ?? undefined,
  }
}

const LIST_TITLE: Record<ListName, [string, string]> = {
  trending: [t('Trending movies'), t('Trending shows')],
  popular: [t('Popular movies'), t('Popular shows')],
  upcoming: [t('Coming soon: movies'), t('Coming soon: shows')],
  similar: [t('More movies like your library'), t('More shows like your library')],
  watched: [t('Because you watched: movies'), t('Because you watched: shows')],
  collections: [t('Complete the collection'), t('Complete the collection')],
  season: [t('For the season: movies'), t('For the season: shows')],
  'anime-airing': [t('Anime airing now'), t('Anime airing now')],
  'anime-popular': [t('Popular anime'), t('Popular anime')],
  'anime-top': [t('Top-rated anime'), t('Top-rated anime')],
  'anime-new': [t('New anime series'), t('New anime series')],
  recipe: [t('Recipe'), t('Recipe')],
  foryou: [t('For you: movies'), t('For you: shows')],
}

// The season's rail is named for the season (home fork).
const SEASON_TITLE: Record<string, [string, string]> = {
  halloween: [t('Spooky season: movies'), t('Spooky season: shows')],
  holidays: [t('Holiday movies'), t('Holiday shows')],
  valentine: [t('For Valentine’s: movies'), t('For Valentine’s: shows')],
  summer: [t('Summer movies'), t('Summer shows')],
}

export function sourceTitle(src: Source, genreName?: string): string {
  if (src.list === 'recipe' && src.name) return src.name // home fork
  if (src.list === 'season' && src.season && SEASON_TITLE[src.season]) return SEASON_TITLE[src.season][src.kind === 'movie' ? 0 : 1]
  if (src.list !== 'browse') return LIST_TITLE[src.list][src.kind === 'movie' ? 0 : 1]
  const movie = src.kind === 'movie'
  const what = genreName ? (movie ? t('{genre} movies', { genre: genreName }) : t('{genre} shows', { genre: genreName })) : movie ? t('Movies') : t('Shows')
  if (src.yearFrom && src.yearTo) return t('{what} from {from} to {to}', { what, from: src.yearFrom, to: src.yearTo })
  if (src.yearFrom) return t('{what} from {from}', { what, from: src.yearFrom })
  if (src.yearTo) return t('{what} up to {to}', { what, to: src.yearTo })
  return what
}

// "Coming 12 Oct" for a date still ahead, otherwise nothing.
export function comingLabel(date?: string): string | undefined {
  if (!date) return undefined
  const d = new Date(`${date}T00:00:00`)
  if (Number.isNaN(d.getTime()) || d.getTime() <= Date.now()) return undefined
  const sameYear = d.getFullYear() === new Date().getFullYear()
  return t('Coming {date}', { date: d.toLocaleDateString(locale(), { day: 'numeric', month: 'short', ...(sameYear ? {} : { year: 'numeric' }) }) })
}
